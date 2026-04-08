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

package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
)

// toTOMLPath converts a filesystem path to a forward-slash path safe for
// embedding in TOML string literals. On Windows, backslash path separators are
// misinterpreted as TOML escape sequences (e.g. \U is a Unicode escape).
func toTOMLPath(p string) string {
	return filepath.ToSlash(p)
}

// writeTOML creates a file at path with the given content, creating parent
// directories as needed.
func writeTOML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("writeTOML: MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeTOML: WriteFile: %v", err)
	}
}

// minimalDaemonTOML returns a minimal valid daemon.toml content.
func minimalDaemonTOML(adminToken string) string {
	return `[daemon]
port = 8080
bind = "127.0.0.1"
admin_token = "` + adminToken + `"
log_level = "info"
log_format = "json"
`
}

// minimalSourceTOML returns a minimal valid source TOML content.
func minimalSourceTOML(name, apiKey, dest string) string {
	return `[source]
name = "` + name + `"
api_key = "` + apiKey + `"
namespace = "` + name + `"
can_read = true
can_write = true
target_destination = "` + dest + `"
`
}

// minimalDestTOML returns a minimal valid destination TOML content.
func minimalDestTOML(name, typ string) string {
	return `[destination]
name = "` + name + `"
type = "` + typ + `"
db_path = "/tmp/test.db"
`
}

// setupValidConfig creates a complete valid config tree in a temp directory.
func setupValidConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML("super-secret-admin"))
	writeTOML(t, filepath.Join(dir, "sources", "claude.toml"), minimalSourceTOML("claude", "src-key-abc123", "sqlite"))
	writeTOML(t, filepath.Join(dir, "destinations", "sqlite.toml"), minimalDestTOML("sqlite", "sqlite"))
	return dir
}

func TestLoad_ValidConfig(t *testing.T) {
	dir := setupValidConfig(t)
	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if cfg.Daemon.Port != 8080 {
		t.Errorf("Daemon.Port = %d; want 8080", cfg.Daemon.Port)
	}
	if len(cfg.Sources) != 1 {
		t.Fatalf("len(Sources) = %d; want 1", len(cfg.Sources))
	}
	if cfg.Sources[0].Name != "claude" {
		t.Errorf("Sources[0].Name = %q; want %q", cfg.Sources[0].Name, "claude")
	}
	if len(cfg.ResolvedSourceKeys) != 1 {
		t.Errorf("len(ResolvedSourceKeys) = %d; want 1", len(cfg.ResolvedSourceKeys))
	}
	key, ok := cfg.ResolvedSourceKeys["claude"]
	if !ok || string(key) != "src-key-abc123" {
		t.Errorf("ResolvedSourceKeys[claude] = %q; want %q", key, "src-key-abc123")
	}
	if string(cfg.ResolvedAdminKey) != "super-secret-admin" {
		t.Errorf("ResolvedAdminKey = %q; want %q", cfg.ResolvedAdminKey, "super-secret-admin")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	// daemon.toml without optional fields — defaults should be applied.
	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML("admin-key"))
	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	if cfg.Daemon.QueueSize != 10_000 {
		t.Errorf("QueueSize = %d; want 10000", cfg.Daemon.QueueSize)
	}
	if cfg.Daemon.WAL.MaxSegmentSizeMB != 50 {
		t.Errorf("WAL.MaxSegmentSizeMB = %d; want 50", cfg.Daemon.WAL.MaxSegmentSizeMB)
	}
	if cfg.Daemon.Shutdown.DrainTimeoutSeconds != 30 {
		t.Errorf("DrainTimeoutSeconds = %d; want 30", cfg.Daemon.Shutdown.DrainTimeoutSeconds)
	}
	if cfg.Retrieval.DefaultProfile != "balanced" {
		t.Errorf("Retrieval.DefaultProfile = %q; want balanced", cfg.Retrieval.DefaultProfile)
	}
}

func TestLoad_MissingDaemonTOML(t *testing.T) {
	dir := t.TempDir() // no daemon.toml
	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected error for missing daemon.toml, got nil")
	}
}

func TestLoad_EmptyAdminToken(t *testing.T) {
	dir := t.TempDir()
	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML(""))
	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected SCHEMA_ERROR for empty admin_token, got nil")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Errorf("error %q should contain SCHEMA_ERROR", err.Error())
	}
}

func TestLoad_EmptySourceAPIKey(t *testing.T) {
	dir := t.TempDir()
	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML("admin-key"))
	writeTOML(t, filepath.Join(dir, "sources", "bad.toml"), minimalSourceTOML("bad", "", "sqlite"))
	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected SCHEMA_ERROR for empty source api_key, got nil")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Errorf("error %q should contain SCHEMA_ERROR", err.Error())
	}
}

func TestLoad_DuplicateSourceKeys(t *testing.T) {
	dir := t.TempDir()
	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML("admin-key"))
	// Two sources with the same resolved key.
	writeTOML(t, filepath.Join(dir, "sources", "src1.toml"), minimalSourceTOML("src1", "shared-key", "sqlite"))
	writeTOML(t, filepath.Join(dir, "sources", "src2.toml"), minimalSourceTOML("src2", "shared-key", "sqlite"))
	writeTOML(t, filepath.Join(dir, "destinations", "sqlite.toml"), minimalDestTOML("sqlite", "sqlite"))
	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected SCHEMA_ERROR for duplicate source keys, got nil")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Errorf("error %q should contain SCHEMA_ERROR", err.Error())
	}
}

func TestLoad_EnvResolution(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_SOURCE_KEY", "env-resolved-key")
	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML("admin-key"))
	writeTOML(t, filepath.Join(dir, "sources", "src.toml"), minimalSourceTOML("src", "env:TEST_SOURCE_KEY", "sqlite"))
	writeTOML(t, filepath.Join(dir, "destinations", "sqlite.toml"), minimalDestTOML("sqlite", "sqlite"))
	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	key := cfg.ResolvedSourceKeys["src"]
	if string(key) != "env-resolved-key" {
		t.Errorf("resolved key = %q; want %q", key, "env-resolved-key")
	}
}

func TestLoad_EnvResolution_NonExistent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Unsetenv("NONEXISTENT_KEY_XYZ"); err != nil {
		t.Fatalf("Unsetenv: %v", err)
	}
	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML("admin-key"))
	writeTOML(t, filepath.Join(dir, "sources", "src.toml"), minimalSourceTOML("src", "env:NONEXISTENT_KEY_XYZ", "sqlite"))
	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected SCHEMA_ERROR for non-existent env var, got nil")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Errorf("error %q should contain SCHEMA_ERROR", err.Error())
	}
}

func TestLoad_FileResolution(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "secret.key")
	if err := os.WriteFile(keyFile, []byte("  file-resolved-key  \n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML("admin-key"))
	writeTOML(t, filepath.Join(dir, "sources", "src.toml"),
		minimalSourceTOML("src", "file:"+toTOMLPath(keyFile), "sqlite"))
	writeTOML(t, filepath.Join(dir, "destinations", "sqlite.toml"), minimalDestTOML("sqlite", "sqlite"))

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
	key := cfg.ResolvedSourceKeys["src"]
	if string(key) != "file-resolved-key" {
		t.Errorf("resolved key = %q; want %q (whitespace trimmed)", key, "file-resolved-key")
	}
}

func TestLoad_FileResolution_NonExistent(t *testing.T) {
	dir := t.TempDir()
	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML("admin-key"))
	writeTOML(t, filepath.Join(dir, "sources", "src.toml"),
		minimalSourceTOML("src", "file:/nonexistent/path/to/secret", "sqlite"))
	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected error for non-existent file reference, got nil")
	}
}

func TestLoad_UnknownDestination(t *testing.T) {
	dir := t.TempDir()
	writeTOML(t, filepath.Join(dir, "daemon.toml"), minimalDaemonTOML("admin-key"))
	writeTOML(t, filepath.Join(dir, "sources", "src.toml"),
		minimalSourceTOML("src", "key-abc", "nonexistent"))
	// No destinations/ directory.
	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected SCHEMA_ERROR for unknown destination, got nil")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Errorf("error %q should contain SCHEMA_ERROR", err.Error())
	}
}

func TestResolveEnv_Literal(t *testing.T) {
	val, err := config.ResolveEnv("literal-value", nil)
	if err != nil {
		t.Fatalf("ResolveEnv: unexpected error: %v", err)
	}
	if val != "literal-value" {
		t.Errorf("got %q; want %q", val, "literal-value")
	}
}

func TestResolveEnv_Env(t *testing.T) {
	t.Setenv("RESOLVE_TEST_VAR", "hello")
	val, err := config.ResolveEnv("env:RESOLVE_TEST_VAR", nil)
	if err != nil {
		t.Fatalf("ResolveEnv: unexpected error: %v", err)
	}
	if val != "hello" {
		t.Errorf("got %q; want %q", val, "hello")
	}
}

func TestResolveEnv_File(t *testing.T) {
	f, err := os.CreateTemp("", "nexus-resolve-test-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil {
			t.Logf("remove: %v", err)
		}
	}()
	if _, err := f.WriteString("  file-secret\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	val, err := config.ResolveEnv("file:"+f.Name(), nil)
	if err != nil {
		t.Fatalf("ResolveEnv: unexpected error: %v", err)
	}
	if val != "file-secret" {
		t.Errorf("got %q; want %q", val, "file-secret")
	}
}

func TestResolveEnv_File_NotExist(t *testing.T) {
	_, err := config.ResolveEnv("file:/nonexistent/path/xyz", nil)
	if err == nil {
		t.Fatal("ResolveEnv: expected error for non-existent file, got nil")
	}
}

func TestConfigDir_HonorsEnvVar(t *testing.T) {
	t.Run("EnvVarSet", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("BUBBLEFISH_HOME", dir)

		got, err := config.ConfigDir()
		if err != nil {
			t.Fatalf("ConfigDir: %v", err)
		}
		if got != dir {
			t.Errorf("expected %q, got %q", dir, got)
		}
	})

	t.Run("EnvVarUnset", func(t *testing.T) {
		t.Setenv("BUBBLEFISH_HOME", "")

		got, err := config.ConfigDir()
		if err != nil {
			t.Fatalf("ConfigDir: %v", err)
		}
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".bubblefish", "Nexus")
		if got != want {
			t.Errorf("expected default %q, got %q", want, got)
		}
	})

	t.Run("EnvVarRelativePath", func(t *testing.T) {
		t.Setenv("BUBBLEFISH_HOME", "./test-sandbox")

		got, err := config.ConfigDir()
		if err != nil {
			t.Fatalf("ConfigDir: %v", err)
		}
		if !filepath.IsAbs(got) {
			t.Errorf("expected absolute path, got %q", got)
		}
	})
}

func TestLoader_WALDefaultsRelativeToConfigDir(t *testing.T) {
	dir := setupValidConfig(t)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := filepath.Join(dir, "wal")
	if cfg.Daemon.WAL.Path != want {
		t.Errorf("WAL.Path = %q, want %q", cfg.Daemon.WAL.Path, want)
	}
	if strings.Contains(cfg.Daemon.WAL.Path, ".bubblefish/Nexus") && !strings.Contains(dir, ".bubblefish/Nexus") {
		t.Error("WAL path should not contain hardcoded .bubblefish/Nexus when configDir differs")
	}
}

func TestOAuthConfigEnabledNoIssuer(t *testing.T) {
	dir := t.TempDir()
	daemonTOML := minimalDaemonTOML("admin-key") + `
[daemon.oauth]
enabled = true
issuer_url = ""
`
	writeTOML(t, filepath.Join(dir, "daemon.toml"), daemonTOML)
	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected SCHEMA_ERROR for enabled OAuth with empty issuer_url")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Errorf("error %q should contain SCHEMA_ERROR", err.Error())
	}
	if !strings.Contains(err.Error(), "issuer_url") {
		t.Errorf("error %q should mention issuer_url", err.Error())
	}
}

func TestOAuthConfigPlainLiteralKey(t *testing.T) {
	dir := t.TempDir()
	daemonTOML := minimalDaemonTOML("admin-key") + `
[daemon.oauth]
enabled = true
issuer_url = "https://example.com"
private_key_file = "-----BEGIN RSA PRIVATE KEY-----"
`
	writeTOML(t, filepath.Join(dir, "daemon.toml"), daemonTOML)
	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected SCHEMA_ERROR for plain literal private_key_file")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Errorf("error %q should contain SCHEMA_ERROR", err.Error())
	}
}

func TestOAuthConfigDisabled(t *testing.T) {
	dir := setupValidConfig(t)
	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Daemon.OAuth.Enabled {
		t.Error("OAuth should be disabled by default")
	}
}

func TestOAuthConfigNoClients(t *testing.T) {
	dir := t.TempDir()
	daemonTOML := minimalDaemonTOML("admin-key") + `
[daemon.oauth]
enabled = true
issuer_url = "https://example.com"
private_key_file = "file:/tmp/key.pem"
`
	writeTOML(t, filepath.Join(dir, "daemon.toml"), daemonTOML)
	// Should load successfully (warn only, not error).
	_, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}
}

func TestLoader_AuditLogDefaultsToConfigDir(t *testing.T) {
	dir := setupValidConfig(t)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := filepath.Join(dir, "logs", "interactions.jsonl")
	if cfg.Daemon.Audit.LogFile != want {
		t.Errorf("Audit.LogFile = %q, want %q", cfg.Daemon.Audit.LogFile, want)
	}
	if strings.Contains(cfg.Daemon.Audit.LogFile, ".bubblefish/Nexus") && !strings.Contains(dir, ".bubblefish/Nexus") {
		t.Error("Audit log path should not contain hardcoded .bubblefish/Nexus when configDir differs")
	}
}
