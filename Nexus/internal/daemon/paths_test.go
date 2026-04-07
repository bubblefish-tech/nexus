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

package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/metrics"
)

func TestDaemon_SQLitePathFallsBackToConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BUBBLEFISH_HOME", dir)

	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port:       8080,
			Bind:       "127.0.0.1",
			AdminToken: "test-token",
		},
		// No destinations configured — should fall back to configDir.
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	d := &Daemon{
		cfg:     cfg,
		logger:  logger,
		metrics: metrics.New(),
	}

	got, err := d.resolveSQLitePath()
	if err != nil {
		t.Fatalf("resolveSQLitePath: %v", err)
	}

	want := filepath.Join(dir, "memories.db")
	if got != want {
		t.Errorf("resolveSQLitePath() = %q, want %q", got, want)
	}
	if strings.Contains(got, ".bubblefish/Nexus") && !strings.Contains(dir, ".bubblefish/Nexus") {
		t.Error("SQLite fallback path should not contain hardcoded .bubblefish/Nexus")
	}
}

func TestDaemon_AuditLogFailsClosedOnEmptyPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BUBBLEFISH_HOME", dir)

	// Create minimal WAL directory so Start() gets past WAL init.
	walDir := filepath.Join(dir, "wal")
	if err := os.MkdirAll(walDir, 0700); err != nil {
		t.Fatalf("mkdir wal: %v", err)
	}

	// Create minimal SQLite destination so Start() gets past destination init.
	dbPath := filepath.Join(dir, "memories.db")

	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port:       8080,
			Bind:       "127.0.0.1",
			AdminToken: "test-token",
			Mode:       "simple",
			QueueSize:  100,
			Audit: config.AuditConfig{
				Enabled: true,
				LogFile: "", // Empty — should fail closed.
			},
			WAL: config.WALDaemonConfig{
				Path:             walDir,
				MaxSegmentSizeMB: 50,
				Integrity: config.WALIntegrityConfig{
					Mode: "crc32",
				},
			},
			Shutdown: config.ShutdownConfig{
				DrainTimeoutSeconds: 5,
			},
		},
		Destinations: []*config.Destination{
			{Name: "sqlite", Type: "sqlite", DBPath: dbPath},
		},
		SecurityEvents: config.SecurityEventsConfig{
			Enabled: false,
		},
		ResolvedAdminKey:   []byte("test-token"),
		ResolvedSourceKeys: map[string][]byte{},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	d := New(cfg, logger)

	err := d.Start()
	// Clean up any resources Start() may have partially initialized.
	_ = d.Stop()

	if err == nil {
		t.Fatal("expected error when audit log_file is empty, got nil")
	}
	if !strings.Contains(err.Error(), "audit enabled but log_file is empty") {
		t.Errorf("error should mention empty log_file, got: %v", err)
	}
}
