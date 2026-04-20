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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// writeTestDaemonTOML writes a minimal valid daemon.toml into dir.
func writeTestDaemonTOML(t *testing.T, dir string) {
	t.Helper()

	type walCfg struct {
		Path string `toml:"path"`
	}
	type daemonCfg struct {
		Port       int    `toml:"port"`
		Bind       string `toml:"bind"`
		AdminToken string `toml:"admin_token"`
		Mode       string `toml:"mode"`
		WAL        walCfg `toml:"wal"`
	}
	type securityCfg struct {
		Enabled bool   `toml:"enabled"`
		LogFile string `toml:"log_file"`
	}
	type daemonFile struct {
		Daemon         daemonCfg   `toml:"daemon"`
		SecurityEvents securityCfg `toml:"security_events"`
	}

	cfg := daemonFile{
		Daemon: daemonCfg{
			Port:       8080,
			Bind:       "127.0.0.1",
			AdminToken: "bfn_admin_testtoken0000000000000000000000000000000000000000000000",
			Mode:       "simple",
			WAL: walCfg{
				Path: filepath.Join(dir, "wal"),
			},
		},
		SecurityEvents: securityCfg{
			Enabled: true,
			LogFile: filepath.Join(dir, "security.log"),
		},
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		t.Fatalf("encode daemon.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.toml"), buf.Bytes(), 0600); err != nil {
		t.Fatalf("write daemon.toml: %v", err)
	}
}

// writeTestSQLiteDest writes a minimal sqlite destination TOML into dir/destinations/.
func writeTestSQLiteDest(t *testing.T, dir string) {
	t.Helper()

	type destBody struct {
		Name   string `toml:"name"`
		Type   string `toml:"type"`
		DBPath string `toml:"db_path"`
	}
	type destFile struct {
		Destination destBody `toml:"destination"`
	}

	destDir := filepath.Join(dir, "destinations")
	if err := os.MkdirAll(destDir, 0700); err != nil {
		t.Fatalf("mkdir destinations: %v", err)
	}

	cfg := destFile{
		Destination: destBody{
			Name:   "sqlite",
			Type:   "sqlite",
			DBPath: filepath.Join(dir, "memories.db"),
		},
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		t.Fatalf("encode sqlite.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "sqlite.toml"), buf.Bytes(), 0600); err != nil {
		t.Fatalf("write sqlite.toml: %v", err)
	}
}

// writeTestSource writes a minimal source TOML into dir/sources/.
func writeTestSource(t *testing.T, dir string) {
	t.Helper()

	srcDir := filepath.Join(dir, "sources")
	if err := os.MkdirAll(srcDir, 0700); err != nil {
		t.Fatalf("mkdir sources: %v", err)
	}

	type srcBody struct {
		Name   string `toml:"name"`
		APIKey string `toml:"api_key"`
	}
	type srcFile struct {
		Source srcBody `toml:"source"`
	}

	cfg := srcFile{
		Source: srcBody{
			Name:   "default",
			APIKey: "bfn_data_testkey00000000000000000000000000000000000000000000000000",
		},
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		t.Fatalf("encode default.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "default.toml"), buf.Bytes(), 0600); err != nil {
		t.Fatalf("write default.toml: %v", err)
	}
}

func TestStatusPaths_BasicOutput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BUBBLEFISH_HOME", dir)

	writeTestDaemonTOML(t, dir)
	writeTestSQLiteDest(t, dir)
	writeTestSource(t, dir)

	var stdout bytes.Buffer
	opts := statusOptions{
		configDir: dir,
		stdout:    &stdout,
		stderr:    &bytes.Buffer{},
	}

	if err := doStatusPaths(opts); err != nil {
		t.Fatalf("doStatusPaths: %v", err)
	}

	out := stdout.String()

	// Must contain expected labels.
	for _, label := range []string{
		"config directory:",
		"daemon config:",
		"wal:",
		"sqlite database:",
	} {
		if !strings.Contains(out, label) {
			t.Errorf("output missing label %q", label)
		}
	}

	// Must contain the temp directory path.
	if !strings.Contains(out, dir) {
		t.Errorf("output missing config dir path %q", dir)
	}

	// daemon.toml exists, so should show (exists).
	daemonLine := findLine(out, "daemon config:")
	if !strings.Contains(daemonLine, "(exists)") {
		t.Errorf("daemon config line should show (exists), got: %s", daemonLine)
	}

	// WAL file does not exist yet (daemon hasn't run).
	walLine := findLine(out, "wal:")
	if !strings.Contains(walLine, "(does not exist)") {
		t.Errorf("wal line should show (does not exist), got: %s", walLine)
	}
}

func TestStatusPaths_RespectsBubblefishHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BUBBLEFISH_HOME", dir)

	writeTestDaemonTOML(t, dir)
	writeTestSQLiteDest(t, dir)
	writeTestSource(t, dir)

	// Run with BUBBLEFISH_HOME set.
	var stdout bytes.Buffer
	opts := statusOptions{
		configDir: dir,
		stdout:    &stdout,
		stderr:    &bytes.Buffer{},
	}

	if err := doStatusPaths(opts); err != nil {
		t.Fatalf("doStatusPaths: %v", err)
	}

	out := stdout.String()
	homeLine := findLine(out, "BUBBLEFISH_HOME:")
	if !strings.Contains(homeLine, dir) {
		t.Errorf("BUBBLEFISH_HOME line should contain %q, got: %s", dir, homeLine)
	}

	// Run with BUBBLEFISH_HOME unset.
	t.Setenv("BUBBLEFISH_HOME", "")

	stdout.Reset()
	if err := doStatusPaths(opts); err != nil {
		t.Fatalf("doStatusPaths (unset): %v", err)
	}

	out = stdout.String()
	homeLine = findLine(out, "BUBBLEFISH_HOME:")
	if !strings.Contains(homeLine, "(unset)") {
		t.Errorf("BUBBLEFISH_HOME line should show (unset), got: %s", homeLine)
	}
}

func TestStatusPaths_NoConfigError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BUBBLEFISH_HOME", dir)

	opts := statusOptions{
		configDir: dir,
		stdout:    &bytes.Buffer{},
		stderr:    &bytes.Buffer{},
	}

	err := doStatusPaths(opts)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "no config found") {
		t.Errorf("error should contain 'no config found', got: %v", err)
	}
}

func TestStatusPaths_FileCounts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BUBBLEFISH_HOME", dir)

	writeTestDaemonTOML(t, dir)
	writeTestSQLiteDest(t, dir)

	// Write 3 source files.
	srcDir := filepath.Join(dir, "sources")
	if err := os.MkdirAll(srcDir, 0700); err != nil {
		t.Fatalf("mkdir sources: %v", err)
	}

	type srcBody struct {
		Name   string `toml:"name"`
		APIKey string `toml:"api_key"`
	}
	type srcFile struct {
		Source srcBody `toml:"source"`
	}

	for i, name := range []string{"default", "extra1", "extra2"} {
		cfg := srcFile{
			Source: srcBody{
				Name:   name,
				APIKey: "bfn_data_" + strings.Repeat("a", 55) + string(rune('0'+i)),
			},
		}
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
			t.Fatalf("encode %s.toml: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, name+".toml"), buf.Bytes(), 0600); err != nil {
			t.Fatalf("write %s.toml: %v", name, err)
		}
	}

	var stdout bytes.Buffer
	opts := statusOptions{
		configDir: dir,
		stdout:    &stdout,
		stderr:    &bytes.Buffer{},
	}

	if err := doStatusPaths(opts); err != nil {
		t.Fatalf("doStatusPaths: %v", err)
	}

	out := stdout.String()
	srcLine := findLine(out, "sources directory:")
	if !strings.Contains(srcLine, "(exists, 3 files)") {
		t.Errorf("sources line should show (exists, 3 files), got: %s", srcLine)
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("os.UserHomeDir() unavailable: %v", err)
	}

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "tilde_subdir", input: "~/subdir/file.txt", want: filepath.Join(home, "subdir/file.txt")},
		{name: "tilde_alone", input: "~", want: home},
		{name: "non_tilde", input: "/absolute/path", want: "/absolute/path"},
		{name: "empty", input: "", want: ""},
		{name: "relative", input: "relative/path", want: "relative/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := expandTilde(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expandTilde(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("expandTilde(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// findLine returns the first line in text that contains substr.
func findLine(text, substr string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return ""
}
