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
	"path/filepath"
	"strings"
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
)

// setupModeConfig creates a config directory with the given daemon.toml body,
// a minimal source, and a minimal destination.
func setupModeConfig(t *testing.T, daemonTOML string) string {
	t.Helper()
	dir := t.TempDir()
	writeTOML(t, filepath.Join(dir, "daemon.toml"), daemonTOML)
	writeTOML(t, filepath.Join(dir, "sources", "src.toml"),
		minimalSourceTOML("src", "key-abc", "sqlite"))
	writeTOML(t, filepath.Join(dir, "destinations", "sqlite.toml"),
		minimalDestTOML("sqlite", "sqlite"))
	return dir
}

// TestModeSafe_DefaultsApplied verifies that mode=safe applies TLS, encryption,
// MAC integrity, and 500/min rate limit when those fields are not explicitly set.
func TestModeSafe_DefaultsApplied(t *testing.T) {
	dir := setupModeConfig(t, `[daemon]
admin_token = "admin-key"
mode = "safe"
`)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if !cfg.Daemon.TLS.Enabled {
		t.Error("mode=safe: TLS.Enabled should be true")
	}
	if !cfg.Daemon.WAL.Encryption.Enabled {
		t.Error("mode=safe: WAL.Encryption.Enabled should be true")
	}
	if cfg.Daemon.WAL.Integrity.Mode != "mac" {
		t.Errorf("mode=safe: WAL.Integrity.Mode = %q; want \"mac\"", cfg.Daemon.WAL.Integrity.Mode)
	}
	if cfg.Daemon.RateLimit.GlobalRequestsPerMinute != 500 {
		t.Errorf("mode=safe: RateLimit = %d; want 500", cfg.Daemon.RateLimit.GlobalRequestsPerMinute)
	}
}

// TestModeBalanced_DefaultsApplied verifies mode=balanced overlay values.
func TestModeBalanced_DefaultsApplied(t *testing.T) {
	dir := setupModeConfig(t, `[daemon]
admin_token = "admin-key"
mode = "balanced"
`)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if cfg.Daemon.TLS.Enabled {
		t.Error("mode=balanced: TLS.Enabled should be false")
	}
	if cfg.Daemon.WAL.Encryption.Enabled {
		t.Error("mode=balanced: WAL.Encryption.Enabled should be false")
	}
	if cfg.Daemon.WAL.Integrity.Mode != "crc32" {
		t.Errorf("mode=balanced: WAL.Integrity.Mode = %q; want \"crc32\"", cfg.Daemon.WAL.Integrity.Mode)
	}
	if cfg.Daemon.RateLimit.GlobalRequestsPerMinute != 2000 {
		t.Errorf("mode=balanced: RateLimit = %d; want 2000", cfg.Daemon.RateLimit.GlobalRequestsPerMinute)
	}
}

// TestModeFast_DefaultsApplied verifies mode=fast overlay values.
func TestModeFast_DefaultsApplied(t *testing.T) {
	dir := setupModeConfig(t, `[daemon]
admin_token = "admin-key"
mode = "fast"
`)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if cfg.Daemon.TLS.Enabled {
		t.Error("mode=fast: TLS.Enabled should be false")
	}
	if cfg.Daemon.WAL.Encryption.Enabled {
		t.Error("mode=fast: WAL.Encryption.Enabled should be false")
	}
	if cfg.Daemon.WAL.Integrity.Mode != "crc32" {
		t.Errorf("mode=fast: WAL.Integrity.Mode = %q; want \"crc32\"", cfg.Daemon.WAL.Integrity.Mode)
	}
	if cfg.Daemon.RateLimit.GlobalRequestsPerMinute != 10000 {
		t.Errorf("mode=fast: RateLimit = %d; want 10000", cfg.Daemon.RateLimit.GlobalRequestsPerMinute)
	}
}

// TestModeSafe_UserOverrides verifies that explicit user values take precedence
// over mode defaults. User sets mode=safe but explicitly disables TLS and sets
// a custom rate limit.
func TestModeSafe_UserOverrides(t *testing.T) {
	dir := setupModeConfig(t, `[daemon]
admin_token = "admin-key"
mode = "safe"

[daemon.tls]
enabled = false

[daemon.rate_limit]
global_requests_per_minute = 999

[daemon.wal.integrity]
mode = "crc32"

[daemon.wal.encryption]
enabled = false
`)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	// All four fields were explicitly set by the user — mode overlay must not override.
	if cfg.Daemon.TLS.Enabled {
		t.Error("user explicitly set tls.enabled=false; mode should not override")
	}
	if cfg.Daemon.WAL.Encryption.Enabled {
		t.Error("user explicitly set encryption.enabled=false; mode should not override")
	}
	if cfg.Daemon.WAL.Integrity.Mode != "crc32" {
		t.Errorf("user explicitly set integrity.mode=crc32; got %q", cfg.Daemon.WAL.Integrity.Mode)
	}
	if cfg.Daemon.RateLimit.GlobalRequestsPerMinute != 999 {
		t.Errorf("user explicitly set rate_limit=999; got %d", cfg.Daemon.RateLimit.GlobalRequestsPerMinute)
	}
}

// TestModeSafe_PartialOverride verifies that user can override some fields
// while mode defaults fill in the rest.
func TestModeSafe_PartialOverride(t *testing.T) {
	dir := setupModeConfig(t, `[daemon]
admin_token = "admin-key"
mode = "safe"

[daemon.rate_limit]
global_requests_per_minute = 750
`)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	// Rate limit was explicitly set — user override.
	if cfg.Daemon.RateLimit.GlobalRequestsPerMinute != 750 {
		t.Errorf("user set rate_limit=750; got %d", cfg.Daemon.RateLimit.GlobalRequestsPerMinute)
	}
	// TLS, encryption, integrity were NOT set — mode defaults apply.
	if !cfg.Daemon.TLS.Enabled {
		t.Error("TLS.Enabled should be true from mode=safe default")
	}
	if !cfg.Daemon.WAL.Encryption.Enabled {
		t.Error("WAL.Encryption.Enabled should be true from mode=safe default")
	}
	if cfg.Daemon.WAL.Integrity.Mode != "mac" {
		t.Errorf("WAL.Integrity.Mode = %q; want \"mac\" from mode=safe default", cfg.Daemon.WAL.Integrity.Mode)
	}
}

// TestModeUnknown_Error verifies that an unknown mode value is rejected.
func TestModeUnknown_Error(t *testing.T) {
	dir := setupModeConfig(t, `[daemon]
admin_token = "admin-key"
mode = "turbo"
`)

	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("Load: expected error for unknown mode, got nil")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Errorf("error %q should contain SCHEMA_ERROR", err.Error())
	}
	if !strings.Contains(err.Error(), "turbo") {
		t.Errorf("error %q should mention the invalid mode name", err.Error())
	}
}

// TestModeEmpty_NoOverlay verifies that an empty mode field does not apply overlays.
func TestModeEmpty_NoOverlay(t *testing.T) {
	dir := setupModeConfig(t, `[daemon]
admin_token = "admin-key"
`)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	// No mode set — general defaults apply (crc32, 2000/min, no TLS, no encryption).
	if cfg.Daemon.TLS.Enabled {
		t.Error("no mode: TLS should not be enabled")
	}
	if cfg.Daemon.WAL.Encryption.Enabled {
		t.Error("no mode: encryption should not be enabled")
	}
	if cfg.Daemon.WAL.Integrity.Mode != "crc32" {
		t.Errorf("no mode: integrity = %q; want crc32", cfg.Daemon.WAL.Integrity.Mode)
	}
	if cfg.Daemon.RateLimit.GlobalRequestsPerMinute != 2000 {
		t.Errorf("no mode: rate_limit = %d; want 2000", cfg.Daemon.RateLimit.GlobalRequestsPerMinute)
	}
}

// TestModeSimple_NoOverlay verifies that mode=simple does not apply deployment overlays.
func TestModeSimple_NoOverlay(t *testing.T) {
	dir := setupModeConfig(t, `[daemon]
admin_token = "admin-key"
mode = "simple"
`)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	// simple mode is handled by the install command, not the overlay system.
	if cfg.Daemon.TLS.Enabled {
		t.Error("mode=simple: TLS should not be enabled by overlay")
	}
}

// TestValidModes verifies the exported helper.
func TestValidModes(t *testing.T) {
	modes := config.ValidModes()
	if len(modes) != 3 {
		t.Fatalf("ValidModes() returned %d modes; want 3", len(modes))
	}
	expected := map[string]bool{"safe": true, "balanced": true, "fast": true}
	for _, m := range modes {
		if !expected[m] {
			t.Errorf("unexpected mode %q in ValidModes()", m)
		}
	}
}

// TestModeCaseInsensitive verifies that mode matching is case-insensitive.
func TestModeCaseInsensitive(t *testing.T) {
	dir := setupModeConfig(t, `[daemon]
admin_token = "admin-key"
mode = "SAFE"
`)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if !cfg.Daemon.TLS.Enabled {
		t.Error("mode=SAFE (uppercase): TLS should be enabled")
	}
}

// TestModeFast_WriteTOMLAndLoad is an integration test that writes actual TOML
// files and verifies the full load path with mode=fast.
func TestModeFast_WriteTOMLAndLoad(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	writeTOML(t, filepath.Join(dir, "daemon.toml"), `[daemon]
port = 9090
admin_token = "admin-key"
mode = "fast"
log_level = "debug"
`)
	writeTOML(t, filepath.Join(dir, "sources", "bench.toml"), `[source]
name = "bench"
api_key = "bench-key"
can_read = true
can_write = true
target_destination = "sqlite"
`)
	writeTOML(t, filepath.Join(dir, "destinations", "sqlite.toml"), `[destination]
name = "sqlite"
type = "sqlite"
db_path = "`+toTOMLPath(dbPath)+`"
`)

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if cfg.Daemon.Port != 9090 {
		t.Errorf("Port = %d; want 9090", cfg.Daemon.Port)
	}
	if cfg.Daemon.RateLimit.GlobalRequestsPerMinute != 10000 {
		t.Errorf("mode=fast: RateLimit = %d; want 10000", cfg.Daemon.RateLimit.GlobalRequestsPerMinute)
	}
	if cfg.Daemon.WAL.Integrity.Mode != "crc32" {
		t.Errorf("mode=fast: Integrity = %q; want crc32", cfg.Daemon.WAL.Integrity.Mode)
	}
}
