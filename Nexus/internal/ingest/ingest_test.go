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

package ingest

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// mockWriter collects all memories written through the pipeline.
type mockWriter struct {
	mu       sync.Mutex
	memories []Memory
}

func (w *mockWriter) Write(ctx context.Context, source string, memory Memory) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.memories = append(w.memories, memory)
	return nil
}

func (w *mockWriter) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.memories)
}

func TestNewManagerKillSwitch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KillSwitch = true
	m, err := New(cfg, nil, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected non-nil manager even when kill switched")
	}
	if m.IsEnabled() {
		t.Error("expected IsEnabled()=false when kill switch is on")
	}
}

func TestNewManagerDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	m, err := New(cfg, nil, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if m.IsEnabled() {
		t.Error("expected IsEnabled()=false when enabled=false")
	}
}

func TestNewManagerEnabled(t *testing.T) {
	cfg := DefaultConfig()
	m, err := New(cfg, nil, nil, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsEnabled() {
		t.Error("expected IsEnabled()=true with default config")
	}
}

func TestStartDisabledIsNoop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KillSwitch = true
	m, _ := New(cfg, nil, nil, slog.Default())
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start on disabled manager should succeed: %v", err)
	}
}

func TestShutdownIdempotent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KillSwitch = true
	m, _ := New(cfg, nil, nil, slog.Default())
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Shutdown(context.Background()); err != nil {
		t.Fatal("second Shutdown should succeed")
	}
}

func TestStatusEmptyWhenNoWatchers(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KillSwitch = true
	m, _ := New(cfg, nil, nil, slog.Default())
	status := m.Status()
	if len(status) != 0 {
		t.Errorf("expected 0 watchers, got %d", len(status))
	}
}

func TestDefaultConfigValues(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.Enabled {
		t.Error("default Enabled should be true")
	}
	if cfg.KillSwitch {
		t.Error("default KillSwitch should be false")
	}
	if !cfg.ClaudeCodeEnabled {
		t.Error("default ClaudeCodeEnabled should be true")
	}
	if !cfg.CursorEnabled {
		t.Error("default CursorEnabled should be true")
	}
	if !cfg.GenericJSONLEnabled {
		t.Error("default GenericJSONLEnabled should be true")
	}
	if cfg.ChatGPTDesktopEnabled {
		t.Error("default ChatGPTDesktopEnabled should be false")
	}
	if cfg.ParseConcurrency != 4 {
		t.Errorf("default ParseConcurrency = %d, want 4", cfg.ParseConcurrency)
	}
	if cfg.MaxFileSize != 100*1024*1024 {
		t.Errorf("default MaxFileSize = %d, want 100MB", cfg.MaxFileSize)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{name: "defaults valid", mutate: func(c *Config) {}, wantErr: false},
		{name: "negative debounce", mutate: func(c *Config) { c.DebounceDuration = -1 }, wantErr: true},
		{name: "zero concurrency", mutate: func(c *Config) { c.ParseConcurrency = 0 }, wantErr: true},
		{name: "zero file size", mutate: func(c *Config) { c.MaxFileSize = 0 }, wantErr: true},
		{name: "zero line length", mutate: func(c *Config) { c.MaxLineLength = 0 }, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestWatcherStateString(t *testing.T) {
	tests := []struct {
		state WatcherState
		want  string
	}{
		{StateDisabled, "disabled"},
		{StateNotDetected, "not_detected"},
		{StateDetectedPaused, "detected_paused"},
		{StateActive, "active"},
		{StateError, "error"},
		{WatcherState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("WatcherState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestPathAllowed(t *testing.T) {
	cfg := DefaultConfig()
	m, _ := New(cfg, nil, nil, slog.Default())

	// No allowlist — everything is allowed.
	if !m.pathAllowed("/any/path") {
		t.Error("expected all paths allowed when allowlist is empty")
	}

	// With allowlist.
	m.cfg.AllowlistPaths = []string{"/allowed/dir"}
	if !m.pathAllowed("/allowed/dir/file.jsonl") {
		t.Error("expected path under allowlist to be allowed")
	}
	if m.pathAllowed("/other/dir/file.jsonl") {
		t.Error("expected path outside allowlist to be denied")
	}
}

// TestIntegrationClaudeCodeEndToEnd is the critical integration test.
// It spins up a Manager with a real fsnotify watcher, writes a JSONL file,
// and verifies memories appear in the mock writer.
func TestIntegrationClaudeCodeEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := t.TempDir()
	projectDir := filepath.Join(dir, "projects", "test-project")
	if err := os.MkdirAll(projectDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Set up SQLite file state store.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := NewFileStateStore(db)
	if err != nil {
		t.Fatal(err)
	}

	writer := &mockWriter{}

	// Create a custom watcher that looks at our temp dir instead of ~/.claude.
	cfg := DefaultConfig()
	cfg.DebounceDuration = 100 * time.Millisecond
	cfg.ParseConcurrency = 2
	cfg.ClaudeCodeEnabled = false
	cfg.CursorEnabled = false
	cfg.GenericJSONLEnabled = true
	cfg.GenericJSONLPaths = []string{projectDir}

	m, err := New(cfg, store, writer, slog.Default())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Shutdown(context.Background())

	// Give fsnotify time to start watching.
	time.Sleep(200 * time.Millisecond)

	// Write a JSONL file into the watched directory.
	sessionFile := filepath.Join(projectDir, "session.jsonl")
	content := `{"role":"user","content":"Hello from integration test"}` + "\n" +
		`{"role":"assistant","content":"Hello back!"}` + "\n" +
		`{"role":"user","content":"Third message"}` + "\n"
	if err := os.WriteFile(sessionFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + parse + write (debounce=100ms, generous timeout).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if writer.count() >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if writer.count() < 3 {
		t.Fatalf("expected >= 3 memories, got %d", writer.count())
	}

	// Verify file state was persisted.
	offset, _, err := store.Get("generic_jsonl", sessionFile)
	if err != nil {
		t.Fatal(err)
	}
	if offset <= 0 {
		t.Errorf("expected positive persisted offset, got %d", offset)
	}

	// Append 2 more lines.
	f, err := os.OpenFile(sessionFile, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"role":"user","content":"Fourth message"}` + "\n")
	f.WriteString(`{"role":"assistant","content":"Fifth message"}` + "\n")
	f.Close()

	// Wait for the new messages.
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if writer.count() >= 5 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if writer.count() < 5 {
		t.Fatalf("after append: expected >= 5 memories, got %d", writer.count())
	}
}
