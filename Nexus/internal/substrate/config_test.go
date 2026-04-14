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

import (
	"errors"
	"testing"
	"time"
)

func TestConfigBoundaryValues(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
		want   error
	}{
		// SketchBits boundaries
		{"sketch_bits=0", func(c *Config) { c.Enabled = true; c.SketchBits = 0 }, ErrUnsupportedSketchBits},
		{"sketch_bits=1 (valid)", func(c *Config) { c.Enabled = true }, nil},
		{"sketch_bits=2", func(c *Config) { c.Enabled = true; c.SketchBits = 2 }, ErrUnsupportedSketchBits},

		// RatchetRotationPeriod boundaries
		{"ratchet=59m (too fast)", func(c *Config) {
			c.Enabled = true
			c.RatchetRotationPeriod = 59 * time.Minute
			c.RatchetRotationPeriodStr = "59m"
		}, ErrRatchetRotationTooFast},
		{"ratchet=1h (minimum valid)", func(c *Config) {
			c.Enabled = true
			c.RatchetRotationPeriod = time.Hour
			c.RatchetRotationPeriodStr = "1h"
		}, nil},
		{"ratchet=168h (valid)", func(c *Config) {
			c.Enabled = true
			c.RatchetRotationPeriodStr = "168h"
		}, nil},

		// PrefilterThreshold boundaries
		{"threshold=49 (too small)", func(c *Config) { c.Enabled = true; c.PrefilterThreshold = 49 }, ErrPrefilterThresholdTooSmall},
		{"threshold=50 (minimum valid)", func(c *Config) {
			c.Enabled = true
			c.PrefilterThreshold = 50
			c.PrefilterTopK = 50 // top_k must be <= threshold
		}, nil},
		{"threshold=10000 (large valid)", func(c *Config) { c.Enabled = true; c.PrefilterThreshold = 10000 }, nil},

		// PrefilterTopK boundaries
		{"top_k=9 (too small)", func(c *Config) { c.Enabled = true; c.PrefilterTopK = 9 }, ErrInvalidPrefilterTopK},
		{"top_k=10 (minimum valid)", func(c *Config) { c.Enabled = true; c.PrefilterTopK = 10 }, nil},
		{"top_k=threshold (exact match)", func(c *Config) {
			c.Enabled = true
			c.PrefilterThreshold = 200
			c.PrefilterTopK = 200
		}, nil},
		{"top_k=threshold+1 (too large)", func(c *Config) {
			c.Enabled = true
			c.PrefilterThreshold = 200
			c.PrefilterTopK = 201
		}, ErrInvalidPrefilterTopK},

		// CuckooCapacity boundaries
		{"capacity=999 (too small)", func(c *Config) { c.Enabled = true; c.CuckooCapacity = 999 }, ErrCuckooCapacityTooSmall},
		{"capacity=1000 (minimum valid)", func(c *Config) { c.Enabled = true; c.CuckooCapacity = 1000 }, nil},
		{"capacity=10M (large valid)", func(c *Config) { c.Enabled = true; c.CuckooCapacity = 10_000_000 }, nil},

		// CuckooRebuildThreshold boundaries
		{"rebuild=0.5 (exactly at lower bound, invalid)", func(c *Config) {
			c.Enabled = true
			c.CuckooRebuildThreshold = 0.5
		}, ErrInvalidRebuildThreshold},
		{"rebuild=0.50001 (just above lower bound)", func(c *Config) {
			c.Enabled = true
			c.CuckooRebuildThreshold = 0.50001
		}, nil},
		{"rebuild=0.75 (default, valid)", func(c *Config) { c.Enabled = true }, nil},
		{"rebuild=0.99999 (just below upper bound)", func(c *Config) {
			c.Enabled = true
			c.CuckooRebuildThreshold = 0.99999
		}, nil},
		{"rebuild=1.0 (exactly at upper bound, invalid)", func(c *Config) {
			c.Enabled = true
			c.CuckooRebuildThreshold = 1.0
		}, ErrInvalidRebuildThreshold},
		{"rebuild=0.0 (zero)", func(c *Config) {
			c.Enabled = true
			c.CuckooRebuildThreshold = 0.0
		}, ErrInvalidRebuildThreshold},
		{"rebuild=-0.5 (negative)", func(c *Config) {
			c.Enabled = true
			c.CuckooRebuildThreshold = -0.5
		}, ErrInvalidRebuildThreshold},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestConfigRatchetRotationStringPrecedence(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RatchetRotationPeriod = time.Minute // would be invalid
	cfg.RatchetRotationPeriodStr = "2h"     // should override
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("string '2h' should override Duration: got %v", err)
	}
	if cfg.RatchetRotationPeriod != 2*time.Hour {
		t.Fatalf("Duration should be updated to 2h, got %v", cfg.RatchetRotationPeriod)
	}
}

func TestConfigRatchetRotationInvalidString(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RatchetRotationPeriodStr = "not-a-duration"
	err := cfg.Validate()
	if !errors.Is(err, ErrRatchetRotationTooFast) {
		t.Fatalf("invalid duration string should error: got %v", err)
	}
}

func TestConfigRatchetRotationEmptyStringUsesDuration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.RatchetRotationPeriodStr = ""
	cfg.RatchetRotationPeriod = 2 * time.Hour
	err := cfg.Validate()
	if err != nil {
		t.Fatalf("empty string with valid Duration should pass: got %v", err)
	}
}

func TestDefaultConfigAllFields(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Fatal("should be disabled")
	}
	if cfg.SketchBits != 1 {
		t.Fatalf("sketch_bits: got %d, want 1", cfg.SketchBits)
	}
	if cfg.RatchetRotationPeriod != 24*time.Hour {
		t.Fatalf("ratchet_rotation: got %v, want 24h", cfg.RatchetRotationPeriod)
	}
	if cfg.RatchetRotationPeriodStr != "24h" {
		t.Fatalf("ratchet_rotation_str: got %q, want '24h'", cfg.RatchetRotationPeriodStr)
	}
	if cfg.PrefilterThreshold != 200 {
		t.Fatalf("prefilter_threshold: got %d, want 200", cfg.PrefilterThreshold)
	}
	if cfg.PrefilterTopK != 100 {
		t.Fatalf("prefilter_top_k: got %d, want 100", cfg.PrefilterTopK)
	}
	if cfg.CuckooCapacity != 1_000_000 {
		t.Fatalf("cuckoo_capacity: got %d, want 1000000", cfg.CuckooCapacity)
	}
	if cfg.CuckooRebuildThreshold != 0.75 {
		t.Fatalf("cuckoo_rebuild: got %v, want 0.75", cfg.CuckooRebuildThreshold)
	}
	if !cfg.EncryptionEnabled {
		t.Fatal("encryption_enabled should default to true")
	}
}
