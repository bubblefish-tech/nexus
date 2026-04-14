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
	"log/slog"
	"testing"
	"time"
)

func TestSubstrateDisabledByDefault(t *testing.T) {
	t.Helper()
	cfg := DefaultConfig()
	s, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled() {
		t.Fatal("expected disabled by default")
	}
}

func TestSubstrateFailClosedOnEnable(t *testing.T) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	_, err := New(cfg, slog.Default())
	if err == nil {
		t.Fatal("expected error when enabling substrate at BS.1")
	}
}

func TestSubstrateNilSafe(t *testing.T) {
	t.Helper()
	var s *Substrate
	if s.Enabled() {
		t.Fatal("nil substrate should report disabled")
	}
	if err := s.Shutdown(); err != nil {
		t.Fatalf("nil shutdown should not error: %v", err)
	}
}

func TestSubstrateDisabledShutdown(t *testing.T) {
	t.Helper()
	cfg := DefaultConfig()
	s, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Shutdown(); err != nil {
		t.Fatalf("disabled shutdown should not error: %v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
		want   error
	}{
		{
			name:   "disabled config is always valid",
			modify: func(c *Config) { c.Enabled = false; c.SketchBits = 99 },
			want:   nil,
		},
		{
			name:   "sketch_bits != 1",
			modify: func(c *Config) { c.Enabled = true; c.SketchBits = 4 },
			want:   ErrUnsupportedSketchBits,
		},
		{
			name: "ratchet rotation too fast",
			modify: func(c *Config) {
				c.Enabled = true
				c.RatchetRotationPeriod = time.Minute
				c.RatchetRotationPeriodStr = "1m"
			},
			want: ErrRatchetRotationTooFast,
		},
		{
			name:   "prefilter threshold too small",
			modify: func(c *Config) { c.Enabled = true; c.PrefilterThreshold = 10 },
			want:   ErrPrefilterThresholdTooSmall,
		},
		{
			name:   "prefilter top_k too small",
			modify: func(c *Config) { c.Enabled = true; c.PrefilterTopK = 5 },
			want:   ErrInvalidPrefilterTopK,
		},
		{
			name:   "prefilter top_k > threshold",
			modify: func(c *Config) { c.Enabled = true; c.PrefilterTopK = 300 },
			want:   ErrInvalidPrefilterTopK,
		},
		{
			name:   "cuckoo capacity too small",
			modify: func(c *Config) { c.Enabled = true; c.CuckooCapacity = 500 },
			want:   ErrCuckooCapacityTooSmall,
		},
		{
			name:   "rebuild threshold too low",
			modify: func(c *Config) { c.Enabled = true; c.CuckooRebuildThreshold = 0.4 },
			want:   ErrInvalidRebuildThreshold,
		},
		{
			name:   "rebuild threshold too high",
			modify: func(c *Config) { c.Enabled = true; c.CuckooRebuildThreshold = 1.0 },
			want:   ErrInvalidRebuildThreshold,
		},
		{
			name: "ratchet rotation period from string",
			modify: func(c *Config) {
				c.Enabled = true
				c.RatchetRotationPeriod = 0
				c.RatchetRotationPeriodStr = "2h"
			},
			want: nil,
		},
		{
			name:   "valid enabled config",
			modify: func(c *Config) { c.Enabled = true },
			want:   nil,
		},
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

func TestDefaultConfigValues(t *testing.T) {
	t.Helper()
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Fatal("should be disabled by default")
	}
	if cfg.SketchBits != 1 {
		t.Fatalf("expected sketch_bits=1, got %d", cfg.SketchBits)
	}
	if cfg.RatchetRotationPeriod != 24*time.Hour {
		t.Fatalf("expected 24h rotation, got %v", cfg.RatchetRotationPeriod)
	}
	if cfg.PrefilterThreshold != 200 {
		t.Fatalf("expected prefilter_threshold=200, got %d", cfg.PrefilterThreshold)
	}
	if cfg.PrefilterTopK != 100 {
		t.Fatalf("expected prefilter_top_k=100, got %d", cfg.PrefilterTopK)
	}
	if cfg.CuckooCapacity != 1_000_000 {
		t.Fatalf("expected cuckoo_capacity=1000000, got %d", cfg.CuckooCapacity)
	}
}
