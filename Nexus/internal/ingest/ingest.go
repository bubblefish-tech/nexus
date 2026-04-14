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

// Package ingest implements proactive filesystem-based ingestion of AI
// client conversations. It watches well-known data directories (Claude Code,
// Cursor, generic JSONL, and scaffolded stubs for 5 more clients) and writes
// new conversation content into Nexus as memories via the existing source
// pipeline — with full cryptographic provenance from Phase 4.
//
// Ingest is a new kind of source. Every parser writes through the same
// path a manual POST /inbound takes, using reserved synthetic source names
// (ingest.claude_code, ingest.cursor, ingest.generic_jsonl). From the
// write path's perspective nothing is new — all existing per-source policy,
// rate limiting, byte budgets, WAL, and audit work unchanged.
package ingest

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Manager owns the Ingest lifecycle: config, watchers, file state store,
// fsnotify watcher, debouncer, and parse worker pool.
type Manager struct {
	cfg      Config
	watchers []Watcher
	state    *FileStateStore
	writer   IngestWriter
	logger   *slog.Logger

	// pathToWatcher maps watched directory prefixes to their owning watcher.
	pathToWatcher map[string]Watcher

	metrics   *IngestMetrics
	fsWatcher *fsnotify.Watcher
	debouncer *Debouncer
	pool      *WorkerPool
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	mu       sync.Mutex
	started  bool
	shutdown bool
}

// New creates a Manager. If Ingest is disabled via kill switch or config,
// returns a no-op Manager that is safe to call Start/Shutdown/Status on.
// metrics may be nil (testing/disabled mode).
func New(cfg Config, state *FileStateStore, writer IngestWriter, logger *slog.Logger, metrics *IngestMetrics) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if metrics == nil {
		metrics = NewIngestMetrics(nil)
	}
	return &Manager{
		cfg:           cfg,
		state:         state,
		writer:        writer,
		logger:        logger,
		metrics:       metrics,
		pathToWatcher: make(map[string]Watcher),
	}, nil
}

// Start initializes and starts all enabled watchers. If Ingest is disabled,
// this is a no-op that returns nil.
func (m *Manager) Start(ctx context.Context) error {
	if m.cfg.IsDisabled() {
		m.logger.Info("ingest: disabled, skipping start",
			"component", "ingest",
			"kill_switch", m.cfg.KillSwitch,
			"enabled", m.cfg.Enabled,
		)
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	m.started = true

	m.logger.Info("ingest: starting",
		"component", "ingest",
		"parse_concurrency", m.cfg.ParseConcurrency,
		"debounce", m.cfg.DebounceDuration,
	)

	// Build watchers from config.
	m.buildWatchers()

	// Detect which watchers have their target directories present.
	for _, w := range m.watchers {
		detected, path, err := w.Detect(ctx)
		if err != nil {
			w.SetState(StateError)
			m.logger.Warn("ingest: watcher detection failed",
				"component", "ingest", "watcher", w.Name(), "error", err)
			continue
		}
		if !detected {
			w.SetState(StateNotDetected)
			m.logger.Info("ingest: watcher target not detected",
				"component", "ingest", "watcher", w.Name())
			continue
		}
		w.SetState(StateActive)
		m.pathToWatcher[path] = w
		m.logger.Info("ingest: watcher activated",
			"component", "ingest", "watcher", w.Name(), "path", path)
	}

	// Start fsnotify.
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("ingest: fsnotify create: %w", err)
	}
	m.fsWatcher = fsw

	for path, w := range m.pathToWatcher {
		if err := m.walkAndAdd(fsw, path); err != nil {
			m.logger.Warn("ingest: failed to add path to fsnotify",
				"component", "ingest", "watcher", w.Name(), "path", path, "error", err)
			w.SetState(StateError)
		}
	}

	// Start debouncer and worker pool.
	m.debouncer = NewDebouncer(m.cfg.DebounceDuration)
	m.pool = NewWorkerPool(m.cfg.ParseConcurrency)

	// Start event loop.
	loopCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.wg.Add(1)
	go m.eventLoop(loopCtx)

	return nil
}

// Shutdown gracefully stops all watchers and releases resources.
// Safe to call multiple times.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.shutdown {
		return nil
	}
	m.shutdown = true

	m.logger.Info("ingest: shutting down", "component", "ingest")

	if m.cancel != nil {
		m.cancel()
	}
	if m.debouncer != nil {
		m.debouncer.Stop()
	}
	if m.fsWatcher != nil {
		m.fsWatcher.Close()
	}

	// Wait for event loop to exit before shutting down the pool,
	// so no new tasks are submitted after pool.Shutdown().
	m.wg.Wait()

	if m.pool != nil {
		m.pool.Shutdown()
	}

	return nil
}

// Status returns the current state of all registered watchers.
func (m *Manager) Status() []WatcherStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]WatcherStatus, 0, len(m.watchers))
	for _, w := range m.watchers {
		out = append(out, WatcherStatus{
			Name:       w.Name(),
			SourceName: w.SourceName(),
			State:      w.State(),
		})
	}
	return out
}

// IsEnabled returns true if Ingest is configured to run.
func (m *Manager) IsEnabled() bool {
	return !m.cfg.IsDisabled()
}

// buildWatchers creates the watcher instances based on config toggles.
func (m *Manager) buildWatchers() {
	if m.cfg.ClaudeCodeEnabled {
		m.watchers = append(m.watchers, NewClaudeCodeWatcher(m.cfg, m.logger))
	}
	if m.cfg.CursorEnabled {
		m.watchers = append(m.watchers, NewCursorWatcher(m.cfg, m.logger))
	}
	if m.cfg.GenericJSONLEnabled && len(m.cfg.GenericJSONLPaths) > 0 {
		m.watchers = append(m.watchers, NewGenericJSONLWatcher(m.cfg, m.logger))
	}
}

// walkAndAdd recursively adds a directory and all its subdirectories to
// the fsnotify watcher. fsnotify does not recurse by default on Linux/macOS.
func (m *Manager) walkAndAdd(fsw *fsnotify.Watcher, root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		// Skip symlinks.
		if info.Mode()&os.ModeSymlink != 0 {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if err := fsw.Add(path); err != nil {
				m.logger.Warn("ingest: fsnotify add failed",
					"component", "ingest", "path", path, "error", err)
			}
		}
		return nil
	})
}

// watcherForPath finds the watcher that owns a given file path by matching
// against registered directory prefixes.
func (m *Manager) watcherForPath(path string) Watcher {
	cleanPath := filepath.Clean(path)
	for prefix, w := range m.pathToWatcher {
		cleanPrefix := filepath.Clean(prefix)
		if strings.HasPrefix(cleanPath, cleanPrefix+string(filepath.Separator)) || cleanPath == cleanPrefix {
			return w
		}
	}
	return nil
}

// eventLoop processes fsnotify events and debouncer readiness signals.
func (m *Manager) eventLoop(ctx context.Context) {
	defer m.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-m.fsWatcher.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			w := m.watcherForPath(ev.Name)
			if w == nil || w.State() != StateActive {
				continue
			}
			m.debouncer.Touch(ev.Name)
		case err, ok := <-m.fsWatcher.Errors:
			if !ok {
				return
			}
			m.logger.Warn("ingest: fsnotify error",
				"component", "ingest", "error", err)
		case path := <-m.debouncer.Ready():
			m.pool.Submit(func() {
				m.parseAndWrite(ctx, path)
			})
		}
	}
}

// parseAndWrite loads the file state, detects truncation, parses new content,
// writes memories through the pipeline, and persists the new offset.
func (m *Manager) parseAndWrite(ctx context.Context, path string) {
	w := m.watcherForPath(path)
	if w == nil {
		return
	}

	// Check path is allowed if allowlist is configured.
	if !m.pathAllowed(path) {
		m.logger.Warn("ingest: path not in allowlist",
			"component", "ingest", "path", path)
		return
	}

	var offset int64
	var prevHash [32]byte
	if m.state != nil {
		offset, prevHash, _ = m.state.Get(w.Name(), path)
	}

	// Detect truncation: if the file shrank or the hash at the old position
	// doesn't match, reset to offset 0.
	if offset > 0 {
		if truncated := m.detectTruncation(path, offset, prevHash); truncated {
			m.logger.Info("ingest: file truncated, resetting to offset 0",
				"component", "ingest", "watcher", w.Name(), "path", path)
			offset = 0
		}
	}

	parseStart := time.Now()
	result, err := w.Parse(ctx, path, offset)
	m.metrics.ParseDuration.WithLabelValues(w.Name()).Observe(time.Since(parseStart).Seconds())
	if err != nil {
		m.metrics.ParseErrors.WithLabelValues(w.Name()).Inc()
		m.logger.Warn("ingest: parse failed",
			"component", "ingest", "watcher", w.Name(), "path", path, "error", err)
		return
	}

	// Write each memory through the source pipeline.
	for _, mem := range result.Memories {
		if m.writer != nil {
			if err := m.writer.Write(ctx, w.SourceName(), mem); err != nil {
				m.logger.Warn("ingest: write failed",
					"component", "ingest", "watcher", w.Name(), "error", err)
				// Do NOT break — content hash idempotency means next run reprocesses safely.
			}
		}
	}

	// Persist new offset/hash.
	if m.state != nil {
		_ = m.state.Set(w.Name(), path, result.NewOffset, result.LastHash)
	}

	if len(result.Memories) > 0 {
		m.metrics.IngestionsTotal.WithLabelValues(w.Name()).Add(float64(len(result.Memories)))
		m.logger.Info("ingest: parsed and wrote memories",
			"component", "ingest",
			"watcher", w.Name(),
			"path", path,
			"count", len(result.Memories),
			"new_offset", result.NewOffset,
		)
	}
}

// detectTruncation checks whether the file has been truncated or replaced
// since the last parse. Returns true if the offset should be reset to 0.
func (m *Manager) detectTruncation(path string, offset int64, prevHash [32]byte) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true // file gone — reset
	}
	if info.Size() < offset {
		return true // file shrank
	}
	if prevHash == [32]byte{} {
		return false // no previous hash to compare
	}

	// Read the same region that was hashed last time and compare.
	hashStart := offset - 64
	if hashStart < 0 {
		hashStart = 0
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()

	buf := make([]byte, offset-hashStart)
	if _, err := f.ReadAt(buf, hashStart); err != nil && err != io.EOF {
		return true
	}
	currentHash := sha256.Sum256(buf)
	return currentHash != prevHash
}

// pathAllowed checks a path against the configured allowlist. If no
// allowlist is set, all paths are allowed.
func (m *Manager) pathAllowed(path string) bool {
	if len(m.cfg.AllowlistPaths) == 0 {
		return true
	}
	cp, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	cp = filepath.Clean(cp)
	for _, allow := range m.cfg.AllowlistPaths {
		ap, err := filepath.Abs(allow)
		if err != nil {
			continue
		}
		ap = filepath.Clean(ap)
		if strings.HasPrefix(cp+string(filepath.Separator), ap+string(filepath.Separator)) || cp == ap {
			return true
		}
	}
	return false
}
