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

// Package lint validates BubbleFish Nexus configuration and warns about
// dangerous or suboptimal settings. It implements the `bubblefish lint` CLI
// command and the /api/lint admin endpoint.
//
// Reference: Tech Spec Section 6.7.
package lint

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/config"
)

// Severity indicates whether a finding is a warning or an error.
// Errors cause exit code 1; warnings do not.
type Severity string

const (
	Warn  Severity = "warn"
	Error Severity = "error"
)

// Finding is a single lint diagnostic.
type Finding struct {
	Severity Severity `json:"severity"`
	Check    string   `json:"check"`
	Message  string   `json:"message"`
}

// Result holds all findings from a lint run.
type Result struct {
	Findings []Finding `json:"findings"`
}

// HasErrors returns true if any finding has error severity.
func (r *Result) HasErrors() bool {
	for _, f := range r.Findings {
		if f.Severity == Error {
			return true
		}
	}
	return false
}

// WarningCount returns the total number of findings (both warn and error).
func (r *Result) WarningCount() int {
	return len(r.Findings)
}

// Run executes all lint checks against the loaded configuration.
// The configDir is used for file-existence checks (compiled sigs, etc.).
//
// Reference: Tech Spec Section 6.7.
func Run(cfg *config.Config, configDir string) *Result {
	r := &Result{}

	checkDangerousBind(cfg, r)
	checkSafeModeTLSDisabled(cfg, r)
	checkMissingIdempotency(cfg, r)
	checkUnboundedRateLimits(cfg, r)
	checkLiteralKeys(cfg, r)
	checkMissingKeyfiles(cfg, r)
	checkTLSCertsMissing(cfg, r)
	checkUnsafeProxyCIDRs(cfg, r)
	checkUnknownDestinations(cfg, r)
	checkDuplicateKeys(cfg, r)
	checkUnsignedConfigs(cfg, configDir, r)
	checkEventSinksNoRetry(cfg, r)
	checkAuditSharedKeys(cfg, r)

	return r
}

// checkDangerousBind warns when bind is 0.0.0.0 without TLS.
func checkDangerousBind(cfg *config.Config, r *Result) {
	if cfg.Daemon.Bind == "0.0.0.0" && !cfg.Daemon.TLS.Enabled {
		r.Findings = append(r.Findings, Finding{
			Severity: Warn,
			Check:    "dangerous_bind",
			Message:  "bind address is 0.0.0.0 without TLS enabled; traffic is unencrypted on all interfaces",
		})
	}
}

// checkSafeModeTLSDisabled warns when deployment mode is "safe" but TLS is
// not enabled. In safe mode the mode overlay enables TLS by default, so if
// TLS is disabled the user explicitly turned it off — which contradicts the
// security intent of safe mode.
//
// Reference: Tech Spec Section 6.7, Phase R-13 Behavioral Contract item 5.
func checkSafeModeTLSDisabled(cfg *config.Config, r *Result) {
	if !strings.EqualFold(cfg.Daemon.Mode, "safe") {
		return
	}
	if !cfg.Daemon.TLS.Enabled {
		r.Findings = append(r.Findings, Finding{
			Severity: Warn,
			Check:    "safe_mode_tls_disabled",
			Message:  "deployment mode is \"safe\" but TLS is disabled; safe mode expects TLS to be enabled",
		})
	}
}

// checkMissingIdempotency warns for sources without idempotency enabled.
func checkMissingIdempotency(cfg *config.Config, r *Result) {
	for _, src := range cfg.Sources {
		if !src.Idempotency.Enabled {
			r.Findings = append(r.Findings, Finding{
				Severity: Warn,
				Check:    "missing_idempotency",
				Message:  fmt.Sprintf("source %q has idempotency disabled; duplicate writes will not be detected", src.Name),
			})
		}
	}
}

// checkUnboundedRateLimits warns when global or per-source rate limits are zero or negative.
func checkUnboundedRateLimits(cfg *config.Config, r *Result) {
	if cfg.Daemon.RateLimit.GlobalRequestsPerMinute <= 0 {
		r.Findings = append(r.Findings, Finding{
			Severity: Warn,
			Check:    "unbounded_rate_limit",
			Message:  "global rate limit is zero or negative; requests are unbounded",
		})
	}
	for _, src := range cfg.Sources {
		if src.RateLimit.RequestsPerMinute <= 0 {
			r.Findings = append(r.Findings, Finding{
				Severity: Warn,
				Check:    "unbounded_rate_limit",
				Message:  fmt.Sprintf("source %q rate limit is zero or negative; requests are unbounded", src.Name),
			})
		}
	}
}

// checkLiteralKeys errors on empty keys and warns on literal (non env:/file:) keys
// when not in simple mode.
func checkLiteralKeys(cfg *config.Config, r *Result) {
	isSimple := strings.EqualFold(cfg.Daemon.Mode, "simple")

	// Admin token.
	if cfg.Daemon.AdminToken == "" {
		r.Findings = append(r.Findings, Finding{
			Severity: Error,
			Check:    "empty_key",
			Message:  "admin_token is empty",
		})
	} else if !isSimple && !isSecretRef(cfg.Daemon.AdminToken) {
		r.Findings = append(r.Findings, Finding{
			Severity: Warn,
			Check:    "literal_key",
			Message:  "admin_token is a literal value; use env: or file: reference for production",
		})
	}

	// Source API keys.
	for _, src := range cfg.Sources {
		if src.APIKey == "" {
			r.Findings = append(r.Findings, Finding{
				Severity: Error,
				Check:    "empty_key",
				Message:  fmt.Sprintf("source %q api_key is empty", src.Name),
			})
		} else if !isSimple && !isSecretRef(src.APIKey) {
			r.Findings = append(r.Findings, Finding{
				Severity: Warn,
				Check:    "literal_key",
				Message:  fmt.Sprintf("source %q api_key is a literal value; use env: or file: reference for production", src.Name),
			})
		}
	}
}

// checkMissingKeyfiles errors when WAL or audit encryption/MAC integrity is
// enabled but the corresponding key file is not configured.
func checkMissingKeyfiles(cfg *config.Config, r *Result) {
	if cfg.Daemon.WAL.Encryption.Enabled && cfg.Daemon.WAL.Encryption.KeyFile == "" {
		r.Findings = append(r.Findings, Finding{
			Severity: Error,
			Check:    "missing_keyfile",
			Message:  "WAL encryption enabled but key_file is empty",
		})
	}
	if strings.EqualFold(cfg.Daemon.WAL.Integrity.Mode, "mac") && cfg.Daemon.WAL.Integrity.MacKeyFile == "" {
		r.Findings = append(r.Findings, Finding{
			Severity: Error,
			Check:    "missing_keyfile",
			Message:  "WAL integrity mode is mac but mac_key_file is empty",
		})
	}
	// Audit key checks. Reference: Update U1.1, U1.2.
	if cfg.Daemon.Audit.Encryption.Enabled && cfg.Daemon.Audit.Encryption.KeyFile == "" {
		r.Findings = append(r.Findings, Finding{
			Severity: Error,
			Check:    "missing_keyfile",
			Message:  "audit encryption enabled but key_file is empty",
		})
	}
	if strings.EqualFold(cfg.Daemon.Audit.Integrity.Mode, "mac") && cfg.Daemon.Audit.Integrity.MacKeyFile == "" {
		r.Findings = append(r.Findings, Finding{
			Severity: Error,
			Check:    "missing_keyfile",
			Message:  "audit integrity mode is mac but mac_key_file is empty",
		})
	}
}

// checkTLSCertsMissing errors when TLS is enabled but cert or key file is not set.
func checkTLSCertsMissing(cfg *config.Config, r *Result) {
	if !cfg.Daemon.TLS.Enabled {
		return
	}
	if cfg.Daemon.TLS.CertFile == "" {
		r.Findings = append(r.Findings, Finding{
			Severity: Error,
			Check:    "tls_missing_cert",
			Message:  "TLS enabled but cert_file is empty",
		})
	}
	if cfg.Daemon.TLS.KeyFile == "" {
		r.Findings = append(r.Findings, Finding{
			Severity: Error,
			Check:    "tls_missing_key",
			Message:  "TLS enabled but key_file is empty",
		})
	}
}

// checkUnsafeProxyCIDRs warns when trusted proxy CIDRs include world-open ranges.
func checkUnsafeProxyCIDRs(cfg *config.Config, r *Result) {
	for _, cidr := range cfg.Daemon.TrustedProxies.CIDRs {
		normalized := strings.TrimSpace(cidr)
		if normalized == "0.0.0.0/0" || normalized == "::/0" {
			r.Findings = append(r.Findings, Finding{
				Severity: Warn,
				Check:    "unsafe_proxy_cidr",
				Message:  fmt.Sprintf("trusted proxy CIDR %q allows all addresses; any client can spoof forwarded headers", normalized),
			})
		}
	}
}

// checkUnknownDestinations errors when a source references a destination
// that does not exist.
func checkUnknownDestinations(cfg *config.Config, r *Result) {
	destNames := make(map[string]bool, len(cfg.Destinations))
	for _, d := range cfg.Destinations {
		destNames[d.Name] = true
	}
	for _, src := range cfg.Sources {
		if src.TargetDest != "" && !destNames[src.TargetDest] {
			r.Findings = append(r.Findings, Finding{
				Severity: Error,
				Check:    "unknown_destination",
				Message:  fmt.Sprintf("source %q references unknown destination %q", src.Name, src.TargetDest),
			})
		}
	}
}

// checkDuplicateKeys errors when two sources share the same raw API key reference.
// Note: resolveAndValidate in the loader already catches resolved duplicates,
// but lint catches raw-reference duplicates as an additional safety check.
func checkDuplicateKeys(cfg *config.Config, r *Result) {
	seen := make(map[string]string, len(cfg.Sources))
	for _, src := range cfg.Sources {
		if src.APIKey == "" {
			continue
		}
		if prev, dup := seen[src.APIKey]; dup {
			r.Findings = append(r.Findings, Finding{
				Severity: Error,
				Check:    "duplicate_key",
				Message:  fmt.Sprintf("sources %q and %q share the same api_key reference", prev, src.Name),
			})
		}
		seen[src.APIKey] = src.Name
	}
}

// checkUnsignedConfigs warns when signing is enabled but compiled config
// signature files are missing.
func checkUnsignedConfigs(cfg *config.Config, configDir string, r *Result) {
	if !cfg.Daemon.Signing.Enabled {
		return
	}
	compiledDir := filepath.Join(configDir, "compiled")
	jsonFiles, _ := filepath.Glob(filepath.Join(compiledDir, "*.json"))
	for _, f := range jsonFiles {
		sigPath := f + ".sig"
		if !fileExists(sigPath) {
			r.Findings = append(r.Findings, Finding{
				Severity: Warn,
				Check:    "unsigned_config",
				Message:  fmt.Sprintf("signing enabled but %q has no .sig file; run bubblefish sign-config", filepath.Base(f)),
			})
		}
	}
}

// checkEventSinksNoRetry warns when an event sink has no retry configuration.
func checkEventSinksNoRetry(cfg *config.Config, r *Result) {
	if !cfg.Daemon.Events.Enabled {
		return
	}
	for _, sink := range cfg.Daemon.Events.Sinks {
		if sink.MaxRetries <= 0 {
			r.Findings = append(r.Findings, Finding{
				Severity: Warn,
				Check:    "event_sink_no_retry",
				Message:  fmt.Sprintf("event sink %q has no retry configuration; failed deliveries will be dropped", sink.Name),
			})
		}
	}
}

// isSecretRef returns true if the value uses an env: or file: prefix.
func isSecretRef(val string) bool {
	return strings.HasPrefix(val, "env:") || strings.HasPrefix(val, "file:")
}

// checkAuditSharedKeys warns when the audit HMAC key path or encryption key path
// is the same as the WAL key path. Shared keys reduce security isolation.
//
// Reference: Update U1.1, U1.2.
func checkAuditSharedKeys(cfg *config.Config, r *Result) {
	// HMAC key path overlap.
	if cfg.Daemon.Audit.Integrity.MacKeyFile != "" &&
		cfg.Daemon.WAL.Integrity.MacKeyFile != "" &&
		cfg.Daemon.Audit.Integrity.MacKeyFile == cfg.Daemon.WAL.Integrity.MacKeyFile {
		r.Findings = append(r.Findings, Finding{
			Severity: Warn,
			Check:    "audit_shared_hmac_key",
			Message:  "audit HMAC key path is the same as WAL HMAC key path; use separate keys for security isolation",
		})
	}
	// Encryption key path overlap.
	if cfg.Daemon.Audit.Encryption.KeyFile != "" &&
		cfg.Daemon.WAL.Encryption.KeyFile != "" &&
		cfg.Daemon.Audit.Encryption.KeyFile == cfg.Daemon.WAL.Encryption.KeyFile {
		r.Findings = append(r.Findings, Finding{
			Severity: Warn,
			Check:    "audit_shared_encryption_key",
			Message:  "audit encryption key path is the same as WAL encryption key path; use separate keys for security isolation",
		})
	}
}

// fileExists returns true if the path exists and is not a directory.
func fileExists(path string) bool {
	info, err := statFile(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
