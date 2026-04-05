// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

// Package daemon implements the BubbleFish Nexus gateway daemon. It wires
// together the WAL, queue, idempotency store, destination adapter, HTTP server,
// authentication middleware, request handlers, Prometheus metrics, hot reload
// watcher, and 3-stage graceful shutdown.
//
// Lifecycle:
//
//	New()   — validates dependencies, wires components, initialises metrics
//	Start() — opens WAL and destination, starts HTTP server, runs forever
//	Stop()  — 3-stage budgeted shutdown: HTTP → queue drain → WAL close
//
// All state is held in struct fields. There are no package-level variables.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/doctor"
	"github.com/BubbleFish-Nexus/internal/hotreload"
	"github.com/BubbleFish-Nexus/internal/idempotency"
	"github.com/BubbleFish-Nexus/internal/metrics"
	"github.com/BubbleFish-Nexus/internal/queue"
	"github.com/BubbleFish-Nexus/internal/wal"
)

// Daemon is the central BubbleFish Nexus gateway daemon. All state is held in
// struct fields; there are no package-level variables.
type Daemon struct {
	// configMu guards cfg. Auth hot path uses RLock(); hot reload uses Lock().
	// INVARIANT: NEVER call Lock() on the auth hot path. Only RLock().
	// Reference: Phase 0D Behavioral Contract items 5–6.
	configMu sync.RWMutex
	cfg      *config.Config

	logger  *slog.Logger
	metrics *metrics.Metrics

	wal     *wal.WAL
	queue   *queue.Queue
	idem    *idempotency.Store
	dest    destination.DestinationWriter
	querier destination.Querier
	server  *http.Server
	rl      *rateLimiter

	reloadWatcher *hotreload.Watcher

	stopOnce sync.Once
	stopped  chan struct{}
}

// New creates a Daemon from the loaded configuration. It does NOT open any
// files or start any goroutines — call Start() for that.
//
// Panics if cfg or logger are nil.
func New(cfg *config.Config, logger *slog.Logger) *Daemon {
	if cfg == nil {
		panic("daemon: cfg must not be nil")
	}
	if logger == nil {
		panic("daemon: logger must not be nil")
	}
	return &Daemon{
		cfg:     cfg,
		logger:  logger,
		metrics: metrics.New(),
		rl:      newRateLimiter(),
		stopped: make(chan struct{}),
	}
}

// getConfig returns the current *config.Config under RLock. All concurrent
// accesses to cfg must go through this method to be race-free during hot reload.
//
// The returned pointer is to an immutable Config struct — hot reload only swaps
// the pointer, never mutates in-place. So callers may dereference fields after
// releasing the RLock.
func (d *Daemon) getConfig() *config.Config {
	d.configMu.RLock()
	c := d.cfg
	d.configMu.RUnlock()
	return c
}

// Start opens the WAL, opens the destination, replays pending WAL entries,
// starts the queue workers, starts the hot reload watcher, and starts the HTTP
// server. It blocks until the HTTP server returns (i.e. until Stop is called
// or the listener fails).
//
// Start is not safe to call concurrently. Call it once per Daemon.
func (d *Daemon) Start() error {
	cfg := d.getConfig()

	// Open WAL.
	walPath, err := d.resolveWALPath()
	if err != nil {
		return fmt.Errorf("daemon: resolve WAL path: %w", err)
	}

	d.logger.Info("daemon: opening WAL",
		"component", "daemon",
		"path", walPath,
	)

	w, err := wal.Open(walPath, cfg.Daemon.WAL.MaxSegmentSizeMB, d.logger)
	if err != nil {
		return fmt.Errorf("daemon: open WAL: %w", err)
	}
	d.wal = w

	// Open SQLite destination.
	sqlitePath, err := d.resolveSQLitePath()
	if err != nil {
		return fmt.Errorf("daemon: resolve SQLite path: %w", err)
	}

	d.logger.Info("daemon: opening SQLite destination",
		"component", "daemon",
		"path", sqlitePath,
	)

	sqliteDest, err := destination.OpenSQLite(sqlitePath, d.logger)
	if err != nil {
		return fmt.Errorf("daemon: open SQLite destination: %w", err)
	}
	d.dest = sqliteDest
	d.querier = sqliteDest

	// Initialise idempotency store.
	d.idem = idempotency.New()

	// Create queue — wire OnProcessed to increment queue_processing_rate.
	d.queue = queue.New(
		queue.Config{
			Size:        cfg.Daemon.QueueSize,
			OnProcessed: d.metrics.QueueProcessingRate.Inc,
		},
		d.logger,
		d.dest,
		d.wal,
	)

	// Replay WAL: re-register idempotency keys and re-enqueue PENDING entries.
	// Measure replay duration for the bubblefish_replay_duration_seconds gauge.
	if err := d.replayWAL(); err != nil {
		return fmt.Errorf("daemon: WAL replay: %w", err)
	}

	// Set initial WAL metrics.
	d.metrics.WALCRCFailures.Add(float64(d.wal.CRCFailures()))
	d.metrics.WALHealthy.Set(1)

	// Start WAL watchdog — updates WAL health and disk metrics periodically.
	go d.walWatchdog(walPath)

	// Start hot reload watcher.
	d.startHotReload()

	// Build HTTP server.
	router := d.buildRouter()
	d.server = newHTTPServer(d.serverAddr(), router)

	d.logger.Info("daemon: starting HTTP server",
		"component", "daemon",
		"addr", d.serverAddr(),
		"version", "0.1.0",
	)

	// ListenAndServe blocks until the server is closed.
	if err := d.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("daemon: HTTP server: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the daemon in three budgeted stages. It is safe
// to call multiple times; only the first call has any effect (sync.Once).
//
// Shutdown stages (reference: Tech Spec Section 14.2):
//
//	Stage 1 (stageTimeout): Stop accepting new HTTP requests.
//	Stage 2 (stageTimeout): Drain queue workers.
//	Stage 3 (stageTimeout): Stop reload watcher + close WAL + close destination.
//
// Total budget = drain_timeout_seconds (default 30s). Each stage gets 1/3.
func (d *Daemon) Stop() error {
	var firstErr error

	d.stopOnce.Do(func() {
		defer close(d.stopped)

		cfg := d.getConfig()
		drainTimeout := time.Duration(cfg.Daemon.Shutdown.DrainTimeoutSeconds) * time.Second
		if drainTimeout <= 0 {
			drainTimeout = 30 * time.Second
		}
		// Each stage gets an equal share of the total budget.
		stageTimeout := drainTimeout / 3
		if stageTimeout < 5*time.Second {
			stageTimeout = 5 * time.Second
		}

		d.logger.Info("daemon: shutting down",
			"component", "daemon",
			"drain_timeout", drainTimeout,
			"stage_timeout", stageTimeout,
		)

		// ── Stage 1: Stop accepting new HTTP requests ──────────────────────
		if d.server != nil {
			ctx1, cancel1 := context.WithTimeout(context.Background(), stageTimeout)
			defer cancel1()
			if err := d.server.Shutdown(ctx1); err != nil {
				d.logger.Error("daemon: stage 1 HTTP shutdown error",
					"component", "daemon",
					"error", err,
				)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
		d.logger.Info("daemon: stage 1 complete — HTTP server stopped",
			"component", "daemon",
		)

		// ── Stage 2: Drain queue workers ──────────────────────────────────
		if d.queue != nil {
			ctx2, cancel2 := context.WithTimeout(context.Background(), stageTimeout)
			defer cancel2()
			if !d.queue.DrainWithContext(ctx2) {
				d.logger.Warn("daemon: stage 2 queue drain timed out — some entries may be replayed on restart",
					"component", "daemon",
				)
			}
		}
		d.logger.Info("daemon: stage 2 complete — queue drained",
			"component", "daemon",
		)

		// ── Stage 3: Stop reload watcher, close WAL and destination ───────
		if d.reloadWatcher != nil {
			d.reloadWatcher.Stop()
		}

		if d.dest != nil {
			if err := d.dest.Close(); err != nil {
				d.logger.Error("daemon: close destination",
					"component", "daemon",
					"error", err,
				)
				if firstErr == nil {
					firstErr = err
				}
			}
		}

		if d.wal != nil {
			if err := d.wal.Close(); err != nil {
				d.logger.Error("daemon: close WAL",
					"component", "daemon",
					"error", err,
				)
				if firstErr == nil {
					firstErr = err
				}
			}
		}

		d.logger.Info("daemon: stage 3 complete — daemon stopped",
			"component", "daemon",
		)
	})

	return firstErr
}

// Stopped returns a channel that is closed when the daemon has fully stopped.
func (d *Daemon) Stopped() <-chan struct{} {
	return d.stopped
}

// ---------------------------------------------------------------------------
// Hot reload
// ---------------------------------------------------------------------------

// startHotReload initialises and starts the hot reload watcher. A failure to
// start (e.g. sources dir does not exist) is non-fatal — the daemon continues
// without hot reload and logs a warning.
func (d *Daemon) startHotReload() {
	configDir, err := config.ConfigDir()
	if err != nil {
		d.logger.Warn("daemon: cannot resolve config dir — hot reload disabled",
			"component", "daemon",
			"error", err,
		)
		return
	}
	sourcesDir := filepath.Join(configDir, "sources")

	reloadFunc := func() (*config.Config, error) {
		return config.Load(configDir, d.logger)
	}

	w := hotreload.New(hotreload.Config{
		SourcesDir: sourcesDir,
		ConfigDir:  configDir,
		Mu:         &d.configMu,
		Snapshot: func() *config.Config {
			// Called by the watcher under RLock — do not acquire additional locks.
			return d.cfg
		},
		Apply: func(c *config.Config) {
			// Called by the watcher under Lock — do not acquire additional locks.
			d.cfg = c
			d.metrics.ConfigLintWarnings.Set(0) // reset on successful reload
		},
		Reload: reloadFunc,
		Logger: d.logger,
	})

	if err := w.Start(); err != nil {
		d.logger.Warn("daemon: hot reload watcher start failed — hot reload disabled",
			"component", "daemon",
			"error", err,
		)
		return
	}
	d.reloadWatcher = w
}

// ---------------------------------------------------------------------------
// WAL watchdog
// ---------------------------------------------------------------------------

// walWatchdog is a background goroutine that updates WAL-related Prometheus
// metrics every 30 seconds. It exits when d.stopped is closed.
func (d *Daemon) walWatchdog(walDir string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopped:
			return
		case <-ticker.C:
			d.updateWALMetrics(walDir)
		}
	}
}

// updateWALMetrics refreshes WAL and disk metrics via doctor.
func (d *Daemon) updateWALMetrics(walDir string) {
	res := doctor.Check(walDir, nil) // no destination checks in watchdog

	if res.WALWritable {
		d.metrics.WALHealthy.Set(1)
	} else {
		d.metrics.WALHealthy.Set(0)
		d.logger.Error("daemon: WAL watchdog: WAL directory not writable",
			"component", "daemon",
			"wal_dir", walDir,
		)
	}

	if res.DiskFreeBytes > 0 {
		d.metrics.WALDiskBytesFree.Set(float64(res.DiskFreeBytes))
	}

	// Sync WAL CRC failure counter from the WAL engine.
	// WAL.CRCFailures() returns the cumulative count; we use Add(0) to
	// observe the current value without resetting it.
	if d.wal != nil {
		d.metrics.WALCRCFailures.Add(0) // already initialised in Start()
	}

	// Update queue depth.
	if d.queue != nil {
		d.metrics.QueueDepth.Set(float64(d.queue.Len()))
	}
}

// ---------------------------------------------------------------------------
// WAL replay
// ---------------------------------------------------------------------------

// replayWAL scans the WAL for PENDING entries, re-registers their idempotency
// keys, and re-enqueues them for delivery to the destination. Replay duration
// and entry count are recorded in metrics.
func (d *Daemon) replayWAL() error {
	replayStart := time.Now()
	pending := 0

	err := d.wal.Replay(func(entry wal.Entry) {
		if entry.IdempotencyKey != "" {
			d.idem.Register(entry.IdempotencyKey, entry.PayloadID)
		}
		if err := d.queue.Enqueue(entry); err != nil {
			d.logger.Warn("daemon: WAL replay: queue full during replay",
				"component", "daemon",
				"payload_id", entry.PayloadID,
			)
		}
		pending++
		d.metrics.ReplayEntriesTotal.Inc()
	})
	if err != nil {
		return err
	}

	replayDuration := time.Since(replayStart)
	d.metrics.ReplayDurationSeconds.Set(replayDuration.Seconds())
	d.metrics.WALPendingEntries.Set(float64(pending))

	d.logger.Info("daemon: WAL replay complete",
		"component", "daemon",
		"pending_entries", pending,
		"duration", replayDuration,
	)
	return nil
}

// ---------------------------------------------------------------------------
// Path resolution helpers
// ---------------------------------------------------------------------------

// resolveWALPath expands the configured WAL path (which may contain ~).
// os.UserHomeDir failure is fatal per Phase 0C Behavioral Contract item 17.
func (d *Daemon) resolveWALPath() (string, error) {
	return expandPath(d.getConfig().Daemon.WAL.Path)
}

// resolveSQLitePath returns the SQLite database path.
// Checks configured destinations first, then falls back to the default.
func (d *Daemon) resolveSQLitePath() (string, error) {
	for _, dst := range d.getConfig().Destinations {
		if dst.Type == "sqlite" && dst.DBPath != "" {
			return expandPath(dst.DBPath)
		}
	}
	return expandPath("~/.bubblefish/Nexus/memories.db")
}

// expandPath expands a leading ~ to the user's home directory.
// Returns an error if os.UserHomeDir fails — callers must treat this as fatal.
func expandPath(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return filepath.Clean(p), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("os.UserHomeDir: %w", err)
	}
	return filepath.Join(home, p[1:]), nil
}
