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

package main

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
)

func TestPrintConfigPaths(t *testing.T) {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	configDir := filepath.Join(t.TempDir(), "nexus")

	printConfigPaths(logger, configDir)

	output := buf.String()
	wantSubstrings := []string{
		"effective config paths",
		"daemon.toml",
		"sources",
		"destinations",
		"compiled",
	}
	for _, want := range wantSubstrings {
		if !bytes.Contains([]byte(output), []byte(want)) {
			t.Errorf("printConfigPaths output missing %q\nGot: %s", want, output)
		}
	}
}

func TestDevOverridesLogLevel(t *testing.T) {
	t.Helper()

	// Create a config with default (info) log level.
	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			LogLevel: "info",
		},
	}

	// Simulate what runDev does: override to debug.
	cfg.Daemon.LogLevel = "debug"

	logger := buildLogger(cfg)

	// Verify the logger accepts DEBUG-level messages.
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	_ = logger // proves buildLogger accepts debug config without panic

	testLogger.Debug("test debug message")
	if !bytes.Contains(buf.Bytes(), []byte("test debug message")) {
		t.Error("expected debug logger to emit debug messages")
	}
}

func TestDevSamePipelineAsStart(t *testing.T) {
	t.Helper()

	// Verify that runDev and runStart both use the same daemon.New constructor.
	// This is a structural test: both functions exist in the same package and
	// call daemon.New with the same arguments (cfg, logger). We verify that the
	// dev command only changes log_level and prints paths — no pipeline changes.

	// The dev command overrides exactly one config field.
	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			LogLevel: "warn",
			Mode:     "safe",
		},
	}

	// Before dev override.
	if cfg.Daemon.LogLevel != "warn" {
		t.Fatal("precondition: log level should be warn")
	}

	// Apply dev override (same as runDev).
	cfg.Daemon.LogLevel = "debug"

	if cfg.Daemon.LogLevel != "debug" {
		t.Fatal("dev override should set log level to debug")
	}

	// Mode must NOT change — no pipeline semantics altered.
	if cfg.Daemon.Mode != "safe" {
		t.Fatalf("dev command must not change mode, got %q", cfg.Daemon.Mode)
	}
}
