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
// authentication middleware, and request handlers.
//
// Lifecycle:
//
//	New()   — validates dependencies, wires components
//	Start() — opens WAL and destination, starts HTTP server, runs forever
//	Stop()  — graceful shutdown: stop accepting new requests, drain queue,
//	          close destination and WAL
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
	"github.com/BubbleFish-Nexus/internal/idempotency"
	"github.com/BubbleFish-Nexus/internal/queue"
	"github.com/BubbleFish-Nexus/internal/wal"
)

// Daemon is the central BubbleFish Nexus gateway daemon. All state is held in
// struct fields; there are no package-level variables.
type Daemon struct {
	cfg    *config.Config
	logger *slog.Logger

	wal     *wal.WAL
	queue   *queue.Queue
	idem    *idempotency.Store
	dest    destination.DestinationWriter
	querier destination.Querier
	server  *http.Server
	rl      *rateLimiter

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
		rl:      newRateLimiter(),
		stopped: make(chan struct{}),
	}
}

// Start opens the WAL, opens the destination, replays pending WAL entries,
// starts the queue workers, and starts the HTTP server. It blocks until the
// HTTP server returns (i.e. until Stop is called or the listener fails).
//
// Start is not safe to call concurrently. Call it once per Daemon.
func (d *Daemon) Start() error {
	// Open WAL.
	walPath, err := d.resolveWALPath()
	if err != nil {
		return fmt.Errorf("daemon: resolve WAL path: %w", err)
	}

	d.logger.Info("daemon: opening WAL",
		"component", "daemon",
		"path", walPath,
	)

	w, err := wal.Open(walPath, d.cfg.Daemon.WAL.MaxSegmentSizeMB, d.logger)
	if err != nil {
		return fmt.Errorf("daemon: open WAL: %w", err)
	}
	d.wal = w

	// Open SQLite destination.
	// For Phase 0C, we use the first destination of type "sqlite", or a
	// default path if none is configured.
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

	// Create queue.
	d.queue = queue.New(
		queue.Config{Size: d.cfg.Daemon.QueueSize},
		d.logger,
		d.dest,
		d.wal,
	)

	// Replay WAL: re-register idempotency keys and re-enqueue PENDING entries.
	if err := d.replayWAL(); err != nil {
		return fmt.Errorf("daemon: WAL replay: %w", err)
	}

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

// Stop gracefully shuts down the daemon. It is safe to call multiple times;
// only the first call has any effect (sync.Once).
//
// Shutdown order:
//  1. Stop accepting new HTTP requests (http.Server.Shutdown with timeout).
//  2. Drain queue workers (allow in-flight writes to complete).
//  3. Close destination and WAL.
func (d *Daemon) Stop() error {
	var firstErr error

	d.stopOnce.Do(func() {
		defer close(d.stopped)

		drainTimeout := time.Duration(d.cfg.Daemon.Shutdown.DrainTimeoutSeconds) * time.Second
		if drainTimeout <= 0 {
			drainTimeout = 30 * time.Second
		}

		d.logger.Info("daemon: shutting down",
			"component", "daemon",
			"drain_timeout", drainTimeout,
		)

		// Step 1: Stop accepting new HTTP requests.
		if d.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
			defer cancel()
			if err := d.server.Shutdown(ctx); err != nil {
				d.logger.Error("daemon: HTTP server shutdown error",
					"component", "daemon",
					"error", err,
				)
				if firstErr == nil {
					firstErr = err
				}
			}
		}

		// Step 2: Drain queue workers.
		if d.queue != nil {
			ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
			defer cancel()
			if !d.queue.DrainWithContext(ctx) {
				d.logger.Warn("daemon: queue drain timed out — some entries may be replayed on restart",
					"component", "daemon",
				)
			}
		}

		// Step 3: Close destination.
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

		d.logger.Info("daemon: stopped",
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
// WAL replay
// ---------------------------------------------------------------------------

// replayWAL scans the WAL for PENDING entries, re-registers their idempotency
// keys, and re-enqueues them for delivery to the destination.
// DELIVERED and PERMANENT_FAILURE entries are skipped.
func (d *Daemon) replayWAL() error {
	pending := 0
	err := d.wal.Replay(func(entry wal.Entry) {
		// Re-register idempotency key from WAL.
		if entry.IdempotencyKey != "" {
			d.idem.Register(entry.IdempotencyKey, entry.PayloadID)
		}
		// Re-enqueue PENDING entries for delivery.
		if err := d.queue.Enqueue(entry); err != nil {
			d.logger.Warn("daemon: WAL replay: queue full during replay",
				"component", "daemon",
				"payload_id", entry.PayloadID,
			)
		}
		pending++
	})
	if err != nil {
		return err
	}

	d.logger.Info("daemon: WAL replay complete",
		"component", "daemon",
		"pending_entries", pending,
	)
	return nil
}

// ---------------------------------------------------------------------------
// Path resolution helpers
// ---------------------------------------------------------------------------

// resolveWALPath expands the configured WAL path (which may contain ~).
// os.UserHomeDir failure is fatal per Phase 0C Behavioral Contract item 17.
func (d *Daemon) resolveWALPath() (string, error) {
	return expandPath(d.cfg.Daemon.WAL.Path)
}

// resolveSQLitePath returns the SQLite database path.
// Checks configured destinations first, then falls back to the default.
func (d *Daemon) resolveSQLitePath() (string, error) {
	for _, dst := range d.cfg.Destinations {
		if dst.Type == "sqlite" && dst.DBPath != "" {
			return expandPath(dst.DBPath)
		}
	}
	// Default path.
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
