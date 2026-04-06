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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	t.Helper()
	key := generateKey()
	if len(key) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64 hex chars, got %d", len(key))
	}
	// Keys must be unique.
	key2 := generateKey()
	if key == key2 {
		t.Fatal("two generated keys should not be identical")
	}
}

func TestBuildDaemonTOML(t *testing.T) {
	t.Helper()
	tests := []struct {
		name string
		mode string
		want []string // substrings that must appear
	}{
		{
			name: "simple mode",
			mode: "simple",
			want: []string{`mode = "simple"`, `log_format = "text"`, `global_requests_per_minute = 5000`},
		},
		{
			name: "balanced mode",
			mode: "balanced",
			want: []string{`mode = "balanced"`, `log_format = "json"`, `mode = "crc32"`},
		},
		{
			name: "safe mode",
			mode: "safe",
			want: []string{`mode = "safe"`, `mode = "mac"`, `enabled = true`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildDaemonTOML(tt.mode, "test-admin-key")
			for _, w := range tt.want {
				if !strings.Contains(result, w) {
					t.Errorf("mode=%s: expected %q in output", tt.mode, w)
				}
			}
		})
	}
}

func TestWriteConfigFile(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")

	// First write should succeed.
	if err := writeConfigFile(path, "hello", false); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "hello" {
		t.Fatalf("expected %q, got %q", "hello", string(data))
	}

	// Second write without force should be a no-op (skip existing).
	if err := writeConfigFile(path, "world", false); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "hello" {
		t.Fatalf("expected file to remain %q, got %q", "hello", string(data))
	}

	// Write with force should overwrite.
	if err := writeConfigFile(path, "forced", true); err != nil {
		t.Fatalf("force write failed: %v", err)
	}
	data, _ = os.ReadFile(path)
	if string(data) != "forced" {
		t.Fatalf("expected %q after force, got %q", "forced", string(data))
	}
}

func TestWriteDestination(t *testing.T) {
	t.Helper()
	tests := []struct {
		destType string
		filename string
	}{
		{"sqlite", "sqlite.toml"},
		{"postgres", "postgres.toml"},
		{"openbrain", "openbrain.toml"},
	}

	for _, tt := range tests {
		t.Run(tt.destType, func(t *testing.T) {
			dir := t.TempDir()
			destDir := filepath.Join(dir, "destinations")
			if err := os.MkdirAll(destDir, 0700); err != nil {
				t.Fatal(err)
			}

			if err := writeDestination(dir, tt.destType, false); err != nil {
				t.Fatalf("writeDestination(%q) failed: %v", tt.destType, err)
			}

			path := filepath.Join(destDir, tt.filename)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("expected file %q to exist", path)
			}
		})
	}
}

func TestWriteDestinationUnknown(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	destDir := filepath.Join(dir, "destinations")
	os.MkdirAll(destDir, 0700)

	err := writeDestination(dir, "badtype", false)
	if err == nil {
		t.Fatal("expected error for unknown destination type")
	}
}
