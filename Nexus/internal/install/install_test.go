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

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateKey_Prefix(t *testing.T) {
	t.Helper()
	key := GenerateKey("bfn_admin_")
	if !strings.HasPrefix(key, "bfn_admin_") {
		t.Fatalf("expected prefix bfn_admin_, got %s", key)
	}
}

func TestGenerateKey_Length(t *testing.T) {
	t.Helper()
	key := GenerateKey("bfn_admin_")
	// prefix(10) + 64 hex chars
	if len(key) != 10+64 {
		t.Fatalf("expected len %d, got %d", 10+64, len(key))
	}
}

func TestGenerateKey_Unique(t *testing.T) {
	t.Helper()
	a := GenerateKey("bfn_")
	b := GenerateKey("bfn_")
	if a == b {
		t.Fatal("expected unique keys, got identical")
	}
}

func TestBuildDaemonTOML_ContainsMode(t *testing.T) {
	t.Helper()
	toml, _ := BuildDaemonTOML("/tmp/nexus", "balanced", "adminkey", "mcpkey", "", nil)
	if !strings.Contains(toml, `mode = "balanced"`) {
		t.Fatal("expected mode=balanced in TOML")
	}
}

func TestBuildDaemonTOML_ContainsAdminKey(t *testing.T) {
	t.Helper()
	toml, _ := BuildDaemonTOML("/tmp/nexus", "simple", "myAdminKey", "myMCPKey", "", nil)
	if !strings.Contains(toml, "myAdminKey") {
		t.Fatal("expected admin key in TOML")
	}
}

func TestBuildDaemonTOML_BindAddress(t *testing.T) {
	t.Helper()
	_, addr := BuildDaemonTOML("/tmp/nexus", "simple", "k", "m", "", nil)
	if addr != "127.0.0.1:8080" {
		t.Fatalf("expected 127.0.0.1:8080, got %s", addr)
	}
}

func TestBuildDaemonTOML_SafeMode_WalMAC(t *testing.T) {
	t.Helper()
	toml, _ := BuildDaemonTOML("/tmp/nexus", "safe", "k", "m", "", nil)
	if !strings.Contains(toml, `mode = "mac"`) {
		t.Fatal("expected WAL integrity mode=mac in safe mode")
	}
}

func TestWriteConfigFile_Creates(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	if err := WriteConfigFile(path, "content=true\n", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "content=true\n" {
		t.Fatalf("unexpected content: %s", data)
	}
}

func TestWriteConfigFile_SkipsExisting(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	_ = os.WriteFile(path, []byte("original"), 0600)
	// force=false should skip.
	if err := WriteConfigFile(path, "new content", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Fatal("expected file to be unchanged")
	}
}

func TestWriteConfigFile_ForceOverwrites(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	_ = os.WriteFile(path, []byte("original"), 0600)
	if err := WriteConfigFile(path, "new content", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new content" {
		t.Fatal("expected file to be overwritten")
	}
}

func TestWriteDestination_SQLite(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "destinations"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := WriteDestination(dir, "sqlite", "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "destinations", "sqlite.toml"))
	if err != nil {
		t.Fatalf("read sqlite.toml: %v", err)
	}
	if !strings.Contains(string(data), `type = "sqlite"`) {
		t.Fatal("expected type=sqlite in TOML")
	}
}

func TestWriteDestination_Postgres(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "destinations"), 0700)
	if err := WriteDestination(dir, "postgres", "postgres://user:pass@host/db", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "destinations", "postgres.toml"))
	if !strings.Contains(string(data), "postgres://user:pass@host/db") {
		t.Fatal("expected DSN in TOML")
	}
}

func TestWriteDestination_UnknownType(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "destinations"), 0700)
	err := WriteDestination(dir, "unknowndb", "", false)
	if err == nil {
		t.Fatal("expected error for unknown dest type")
	}
}

func TestInstall_CreatesDirectories(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	opts := Options{
		ConfigDir: dir,
		Mode:      "simple",
		DestType:  "sqlite",
		Force:     true,
	}
	if _, err := Install(opts); err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, sub := range []string{"sources", "destinations", "compiled", "wal", "logs", "keys", "discovery", "tools"} {
		if _, err := os.Stat(filepath.Join(dir, sub)); err != nil {
			t.Errorf("expected directory %q to exist: %v", sub, err)
		}
	}
}

func TestInstall_WritesDaemonTOML(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	opts := Options{ConfigDir: dir, Mode: "balanced", DestType: "sqlite", Force: true}
	if _, err := Install(opts); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "daemon.toml"))
	if err != nil {
		t.Fatal("daemon.toml not created")
	}
	if !strings.Contains(string(data), `mode = "balanced"`) {
		t.Fatal("expected mode=balanced in daemon.toml")
	}
}
