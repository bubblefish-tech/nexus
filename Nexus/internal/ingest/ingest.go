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
	"log/slog"
	"sync"
)

// Manager owns the Ingest lifecycle. It holds the config, watchers, file
// state store, and (in SN.4) the fsnotify watcher, debouncer, and worker pool.
type Manager struct {
	cfg      Config
	watchers []Watcher
	state    *FileStateStore
	writer   IngestWriter
	logger   *slog.Logger

	mu       sync.Mutex
	started  bool
	shutdown bool
}

// New creates a Manager. If Ingest is disabled via kill switch or config,
// returns a no-op Manager that is safe to call Start/Shutdown/Status on.
func New(cfg Config, state *FileStateStore, writer IngestWriter, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		cfg:    cfg,
		state:  state,
		writer: writer,
		logger: logger,
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

	// Watcher registration, detection, fsnotify, debouncer, and worker pool
	// are wired in SN.4. For now, Manager starts successfully but does not
	// watch any files.

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
	// Watcher cleanup deferred to SN.4.
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
