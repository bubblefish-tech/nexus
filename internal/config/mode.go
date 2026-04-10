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

package config

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// modePreset holds the overlay values for a deployment mode.
// Each field corresponds to a daemon.toml key that the mode controls.
//
// Reference: Tech Spec Section 2.2.3.
type modePreset struct {
	TLSEnabled        bool
	WALEncryption     bool
	WALIntegrityMode  string
	RateLimitPerMin   int
}

// modePresets maps deployment mode names to their default overlays.
//
// | Mode     | TLS     | Encryption | Integrity | Rate Limit |
// |----------|---------|------------|-----------|------------|
// | safe     | true    | true       | mac       | 500/min    |
// | balanced | false   | false      | crc32     | 2000/min   |
// | fast     | false   | false      | crc32     | 10000/min  |
var modePresets = map[string]modePreset{
	"safe": {
		TLSEnabled:       true,
		WALEncryption:    true,
		WALIntegrityMode: "mac",
		RateLimitPerMin:  500,
	},
	"balanced": {
		TLSEnabled:       false,
		WALEncryption:    false,
		WALIntegrityMode: "crc32",
		RateLimitPerMin:  2000,
	},
	"fast": {
		TLSEnabled:       false,
		WALEncryption:    false,
		WALIntegrityMode: "crc32",
		RateLimitPerMin:  10000,
	},
}

// ValidModes returns the set of recognised deployment mode names.
func ValidModes() []string {
	return []string{"safe", "balanced", "fast"}
}

// applyMode applies the deployment-mode overlay to the decoded daemon config.
// Only fields that the user did NOT explicitly set in daemon.toml are changed.
// meta is the TOML metadata from the decode step, used to detect explicit keys.
//
// Returns an error if the mode value is non-empty and not recognised.
func applyMode(d *DaemonConfig, meta toml.MetaData) error {
	mode := strings.ToLower(strings.TrimSpace(d.Mode))
	if mode == "" || mode == "simple" {
		return nil
	}

	preset, ok := modePresets[mode]
	if !ok {
		return fmt.Errorf("SCHEMA_ERROR: unknown deployment mode %q; valid modes: safe, balanced, fast", d.Mode)
	}

	// TLS enabled — only apply if user didn't explicitly set [daemon.tls].enabled.
	if !meta.IsDefined("daemon", "tls", "enabled") {
		d.TLS.Enabled = preset.TLSEnabled
	}

	// WAL encryption — only apply if user didn't explicitly set [daemon.wal.encryption].enabled.
	if !meta.IsDefined("daemon", "wal", "encryption", "enabled") {
		d.WAL.Encryption.Enabled = preset.WALEncryption
	}

	// WAL integrity mode — only apply if user didn't explicitly set [daemon.wal.integrity].mode.
	if !meta.IsDefined("daemon", "wal", "integrity", "mode") {
		d.WAL.Integrity.Mode = preset.WALIntegrityMode
	}

	// Global rate limit — only apply if user didn't explicitly set [daemon.rate_limit].global_requests_per_minute.
	if !meta.IsDefined("daemon", "rate_limit", "global_requests_per_minute") {
		d.RateLimit.GlobalRequestsPerMinute = preset.RateLimitPerMin
	}

	return nil
}
