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

package lint

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
)

// baseConfig returns a minimal valid config with no lint issues.
func baseConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Daemon: config.DaemonConfig{
			Bind:       "127.0.0.1",
			AdminToken: "env:NEXUS_ADMIN_TOKEN",
			RateLimit: config.GlobalRateLimitConfig{
				GlobalRequestsPerMinute: 2000,
			},
		},
		Sources: []*config.Source{
			{
				Name:       "test-source",
				APIKey:     "env:TEST_SOURCE_KEY",
				TargetDest: "local",
				Idempotency: config.IdempotencyConfig{
					Enabled:            true,
					DedupWindowSeconds: 300,
				},
				RateLimit: config.SourceRateLimitConfig{
					RequestsPerMinute: 1000,
				},
			},
		},
		Destinations: []*config.Destination{
			{Name: "local", Type: "sqlite"},
		},
	}
}

func TestRun_CleanConfig(t *testing.T) {
	cfg := baseConfig(t)
	result := Run(cfg, t.TempDir())
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %d: %+v", len(result.Findings), result.Findings)
	}
	if result.HasErrors() {
		t.Fatal("HasErrors should be false for clean config")
	}
}

// ── Verification Gate: 0.0.0.0 bind + no TLS → warning ──────────────────────

func TestDangerousBind_WarnsWithoutTLS(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Bind = "0.0.0.0"
	cfg.Daemon.TLS.Enabled = false

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "dangerous_bind")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
	if result.HasErrors() {
		t.Fatal("dangerous_bind is a warning, should not cause HasErrors")
	}
}

func TestDangerousBind_NoWarningWithTLS(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Bind = "0.0.0.0"
	cfg.Daemon.TLS.Enabled = true
	cfg.Daemon.TLS.CertFile = "file:/etc/cert.pem"
	cfg.Daemon.TLS.KeyFile = "file:/etc/key.pem"

	result := Run(cfg, t.TempDir())
	assertNoCheck(t, result, "dangerous_bind")
}

// ── Verification Gate: empty api_key → error, exit code 1 ───────────────────

func TestEmptyAPIKey_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Sources[0].APIKey = ""

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "empty_key")
	if found.Severity != Error {
		t.Fatalf("expected severity error, got %s", found.Severity)
	}
	if !result.HasErrors() {
		t.Fatal("empty api_key should cause HasErrors")
	}
}

func TestEmptyAdminToken_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.AdminToken = ""

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "empty_key")
	if found.Severity != Error {
		t.Fatalf("expected severity error, got %s", found.Severity)
	}
}

// ── Missing Idempotency ─────────────────────────────────────────────────────

func TestMissingIdempotency_Warns(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Sources[0].Idempotency.Enabled = false

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "missing_idempotency")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

// ── Unbounded Rate Limits ───────────────────────────────────────────────────

func TestUnboundedGlobalRateLimit_Warns(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.RateLimit.GlobalRequestsPerMinute = 0

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "unbounded_rate_limit")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

func TestUnboundedSourceRateLimit_Warns(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Sources[0].RateLimit.RequestsPerMinute = 0

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "unbounded_rate_limit")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

// ── Literal Keys ────────────────────────────────────────────────────────────

func TestLiteralKey_WarnsInNonSimpleMode(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Mode = "balanced"
	cfg.Sources[0].APIKey = "my-literal-key"

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "literal_key")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

func TestLiteralKey_NoWarningInSimpleMode(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Mode = "simple"
	cfg.Daemon.AdminToken = "literal-admin-token"
	cfg.Sources[0].APIKey = "my-literal-key"

	result := Run(cfg, t.TempDir())
	assertNoCheck(t, result, "literal_key")
}

// ── Missing Keyfiles ────────────────────────────────────────────────────────

func TestMissingEncryptionKeyfile_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.WAL.Encryption.Enabled = true
	cfg.Daemon.WAL.Encryption.KeyFile = ""

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "missing_keyfile")
	if found.Severity != Error {
		t.Fatalf("expected severity error, got %s", found.Severity)
	}
}

func TestMissingMACKeyfile_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.WAL.Integrity.Mode = "mac"
	cfg.Daemon.WAL.Integrity.MacKeyFile = ""

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "missing_keyfile")
	if found.Severity != Error {
		t.Fatalf("expected severity error, got %s", found.Severity)
	}
}

// ── TLS Missing Certs ───────────────────────────────────────────────────────

func TestTLSMissingCert_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.TLS.Enabled = true
	cfg.Daemon.TLS.CertFile = ""
	cfg.Daemon.TLS.KeyFile = "file:/etc/key.pem"

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "tls_missing_cert")
	if found.Severity != Error {
		t.Fatalf("expected severity error, got %s", found.Severity)
	}
}

func TestTLSMissingKey_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.TLS.Enabled = true
	cfg.Daemon.TLS.CertFile = "file:/etc/cert.pem"
	cfg.Daemon.TLS.KeyFile = ""

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "tls_missing_key")
	if found.Severity != Error {
		t.Fatalf("expected severity error, got %s", found.Severity)
	}
}

// ── Unsafe Proxy CIDRs ─────────────────────────────────────────────────────

func TestUnsafeProxyCIDR_IPv4_Warns(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.TrustedProxies.CIDRs = []string{"0.0.0.0/0"}

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "unsafe_proxy_cidr")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

func TestUnsafeProxyCIDR_IPv6_Warns(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.TrustedProxies.CIDRs = []string{"::/0"}

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "unsafe_proxy_cidr")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

// ── Unknown Destination ─────────────────────────────────────────────────────

func TestUnknownDestination_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Sources[0].TargetDest = "nonexistent"

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "unknown_destination")
	if found.Severity != Error {
		t.Fatalf("expected severity error, got %s", found.Severity)
	}
}

// ── Duplicate Keys ──────────────────────────────────────────────────────────

func TestDuplicateKeys_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Sources = append(cfg.Sources, &config.Source{
		Name:   "second-source",
		APIKey: cfg.Sources[0].APIKey,
		Idempotency: config.IdempotencyConfig{
			Enabled: true,
		},
		RateLimit: config.SourceRateLimitConfig{
			RequestsPerMinute: 1000,
		},
	})

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "duplicate_key")
	if found.Severity != Error {
		t.Fatalf("expected severity error, got %s", found.Severity)
	}
}

// ── Unsigned Configs ────────────────────────────────────────────────────────

func TestUnsignedConfig_WarnsWhenSigningEnabled(t *testing.T) {
	dir := t.TempDir()
	compiledDir := filepath.Join(dir, "compiled")
	if err := os.MkdirAll(compiledDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Create a compiled JSON file without a .sig sidecar.
	if err := os.WriteFile(filepath.Join(compiledDir, "policies.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := baseConfig(t)
	cfg.Daemon.Signing.Enabled = true

	result := Run(cfg, dir)
	found := findByCheck(t, result, "unsigned_config")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

func TestUnsignedConfig_NoWarningWhenSigExists(t *testing.T) {
	dir := t.TempDir()
	compiledDir := filepath.Join(dir, "compiled")
	if err := os.MkdirAll(compiledDir, 0700); err != nil {
		t.Fatal(err)
	}
	jsonFile := filepath.Join(compiledDir, "policies.json")
	if err := os.WriteFile(jsonFile, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jsonFile+".sig", []byte("abc123"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := baseConfig(t)
	cfg.Daemon.Signing.Enabled = true

	result := Run(cfg, dir)
	assertNoCheck(t, result, "unsigned_config")
}

// ── Event Sink No Retry ─────────────────────────────────────────────────────

func TestEventSinkNoRetry_Warns(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Events.Enabled = true
	cfg.Daemon.Events.Sinks = []config.EventSink{
		{Name: "webhook1", URL: "https://example.com/hook", MaxRetries: 0},
	}

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "event_sink_no_retry")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

func TestEventSinkWithRetry_NoWarning(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Events.Enabled = true
	cfg.Daemon.Events.Sinks = []config.EventSink{
		{Name: "webhook1", URL: "https://example.com/hook", MaxRetries: 3},
	}

	result := Run(cfg, t.TempDir())
	assertNoCheck(t, result, "event_sink_no_retry")
}

// ── Safe Mode TLS Disabled ──────────────────────────────────────────────────

func TestSafeModeTLSDisabled_Warns(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Mode = "safe"
	cfg.Daemon.TLS.Enabled = false

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "safe_mode_tls_disabled")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

func TestSafeModeTLSEnabled_NoWarning(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Mode = "safe"
	cfg.Daemon.TLS.Enabled = true
	cfg.Daemon.TLS.CertFile = "file:/etc/cert.pem"
	cfg.Daemon.TLS.KeyFile = "file:/etc/key.pem"

	result := Run(cfg, t.TempDir())
	assertNoCheck(t, result, "safe_mode_tls_disabled")
}

func TestBalancedMode_NoSafeModeWarning(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Mode = "balanced"
	cfg.Daemon.TLS.Enabled = false

	result := Run(cfg, t.TempDir())
	assertNoCheck(t, result, "safe_mode_tls_disabled")
}

// ── WarningCount ────────────────────────────────────────────────────────────

func TestWarningCount(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Bind = "0.0.0.0"          // +1 warning
	cfg.Sources[0].Idempotency.Enabled = false // +1 warning

	result := Run(cfg, t.TempDir())
	if result.WarningCount() < 2 {
		t.Fatalf("expected at least 2 findings, got %d", result.WarningCount())
	}
}

// ── Audit Shared Keys ──────────────────────────────────────────────────

func TestAuditSharedHMACKey_Warns(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.WAL.Integrity.Mode = "mac"
	cfg.Daemon.WAL.Integrity.MacKeyFile = "file:/etc/shared-hmac.key"
	cfg.Daemon.Audit.Integrity.Mode = "mac"
	cfg.Daemon.Audit.Integrity.MacKeyFile = "file:/etc/shared-hmac.key"

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "audit_shared_hmac_key")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

func TestAuditSharedHMACKey_NoWarningWhenDifferent(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.WAL.Integrity.Mode = "mac"
	cfg.Daemon.WAL.Integrity.MacKeyFile = "file:/etc/wal-hmac.key"
	cfg.Daemon.Audit.Integrity.Mode = "mac"
	cfg.Daemon.Audit.Integrity.MacKeyFile = "file:/etc/audit-hmac.key"

	result := Run(cfg, t.TempDir())
	assertNoCheck(t, result, "audit_shared_hmac_key")
}

func TestAuditSharedEncryptionKey_Warns(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.WAL.Encryption.Enabled = true
	cfg.Daemon.WAL.Encryption.KeyFile = "file:/etc/shared-enc.key"
	cfg.Daemon.Audit.Encryption.Enabled = true
	cfg.Daemon.Audit.Encryption.KeyFile = "file:/etc/shared-enc.key"

	result := Run(cfg, t.TempDir())
	found := findByCheck(t, result, "audit_shared_encryption_key")
	if found.Severity != Warn {
		t.Fatalf("expected severity warn, got %s", found.Severity)
	}
}

func TestAuditSharedEncryptionKey_NoWarningWhenDifferent(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.WAL.Encryption.Enabled = true
	cfg.Daemon.WAL.Encryption.KeyFile = "file:/etc/wal-enc.key"
	cfg.Daemon.Audit.Encryption.Enabled = true
	cfg.Daemon.Audit.Encryption.KeyFile = "file:/etc/audit-enc.key"

	result := Run(cfg, t.TempDir())
	assertNoCheck(t, result, "audit_shared_encryption_key")
}

func TestAuditMissingMACKeyfile_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Audit.Integrity.Mode = "mac"
	cfg.Daemon.Audit.Integrity.MacKeyFile = ""

	result := Run(cfg, t.TempDir())
	// Should find a missing_keyfile error for audit.
	found := false
	for _, f := range result.Findings {
		if f.Check == "missing_keyfile" && f.Severity == Error &&
			f.Message == "audit integrity mode is mac but mac_key_file is empty" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected missing_keyfile error for audit MAC mode")
	}
}

func TestAuditMissingEncryptionKeyfile_Error(t *testing.T) {
	cfg := baseConfig(t)
	cfg.Daemon.Audit.Encryption.Enabled = true
	cfg.Daemon.Audit.Encryption.KeyFile = ""

	result := Run(cfg, t.TempDir())
	found := false
	for _, f := range result.Findings {
		if f.Check == "missing_keyfile" && f.Severity == Error &&
			f.Message == "audit encryption enabled but key_file is empty" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected missing_keyfile error for audit encryption")
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func findByCheck(t *testing.T, r *Result, check string) Finding {
	t.Helper()
	for _, f := range r.Findings {
		if f.Check == check {
			return f
		}
	}
	t.Fatalf("no finding with check %q found in %+v", check, r.Findings)
	return Finding{}
}

func assertNoCheck(t *testing.T, r *Result, check string) {
	t.Helper()
	for _, f := range r.Findings {
		if f.Check == check {
			t.Fatalf("unexpected finding with check %q: %+v", check, f)
		}
	}
}
