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

package substrate

import "time"

// Config holds the [substrate] TOML section configuration.
type Config struct {
	// Enabled controls whether the BF-Sketch substrate is active.
	// Default: false for v0.1.3.
	Enabled bool `toml:"enabled"`

	// SketchBits is the number of bits per coordinate in the sketch.
	// Only 1 is supported in v0.1.3. Default: 1.
	SketchBits int `toml:"sketch_bits"`

	// RatchetRotationPeriod is the time between automatic ratchet advances.
	// Parsed from a Go duration string (e.g. "24h"). Must be >= 1h.
	// Default: 24h.
	RatchetRotationPeriod time.Duration `toml:"-"`

	// RatchetRotationPeriodStr is the TOML-friendly string form.
	RatchetRotationPeriodStr string `toml:"ratchet_rotation_period"`

	// PrefilterThreshold is the minimum candidate count for Stage 3.5 to
	// activate. Below this, the sketch prefilter is skipped. Default: 200.
	PrefilterThreshold int `toml:"prefilter_threshold"`

	// PrefilterTopK is the number of candidates to keep after sketch
	// prefiltering. Default: 100.
	PrefilterTopK int `toml:"prefilter_top_k"`

	// CuckooCapacity is the expected number of live memories for the
	// cuckoo filter. The filter is sized at 2x this. Default: 1,000,000.
	CuckooCapacity uint `toml:"cuckoo_capacity"`

	// CuckooRebuildThreshold is the load factor above which the cuckoo
	// filter is rebuilt at larger capacity. Must be in (0.5, 1.0).
	// Default: 0.75.
	CuckooRebuildThreshold float64 `toml:"cuckoo_rebuild_threshold"`

	// EncryptionEnabled controls per-memory AES-256-GCM embedding
	// encryption. Default: true when substrate is enabled.
	EncryptionEnabled bool `toml:"encryption_enabled"`
}

// DefaultConfig returns a Config with all defaults applied.
// Substrate is disabled by default.
func DefaultConfig() Config {
	return Config{
		Enabled:                  false,
		SketchBits:               1,
		RatchetRotationPeriod:    24 * time.Hour,
		RatchetRotationPeriodStr: "24h",
		PrefilterThreshold:       200,
		PrefilterTopK:            100,
		CuckooCapacity:           1_000_000,
		CuckooRebuildThreshold:   0.75,
		EncryptionEnabled:        true,
	}
}

// Validate checks that all fields are within acceptable ranges.
// Returns nil if the config is valid or substrate is disabled.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.SketchBits != 1 {
		return ErrUnsupportedSketchBits
	}
	// Parse duration string if set; it takes precedence over the Duration field.
	if c.RatchetRotationPeriodStr != "" {
		d, err := time.ParseDuration(c.RatchetRotationPeriodStr)
		if err != nil {
			return ErrRatchetRotationTooFast
		}
		c.RatchetRotationPeriod = d
	}
	if c.RatchetRotationPeriod < time.Hour {
		return ErrRatchetRotationTooFast
	}
	if c.PrefilterThreshold < 50 {
		return ErrPrefilterThresholdTooSmall
	}
	if c.PrefilterTopK < 10 || c.PrefilterTopK > c.PrefilterThreshold {
		return ErrInvalidPrefilterTopK
	}
	if c.CuckooCapacity < 1000 {
		return ErrCuckooCapacityTooSmall
	}
	if c.CuckooRebuildThreshold <= 0.5 || c.CuckooRebuildThreshold >= 1.0 {
		return ErrInvalidRebuildThreshold
	}
	return nil
}
