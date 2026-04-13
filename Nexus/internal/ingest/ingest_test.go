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
	"log/slog"
	"testing"
)

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
