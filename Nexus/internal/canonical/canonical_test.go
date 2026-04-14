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

package canonical

import (
	"errors"
	"testing"
)

func TestDefaultConfigIsDisabled(t *testing.T) {
	t.Helper()
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Fatal("expected disabled by default")
	}
	if cfg.CanonicalDim != 1024 {
		t.Fatalf("expected canonical_dim=1024, got %d", cfg.CanonicalDim)
	}
}

func TestManagerNilSafe(t *testing.T) {
	t.Helper()
	var m *Manager
	if m.Enabled() {
		t.Fatal("nil manager should report disabled")
	}
	_, _, err := m.Canonicalize([]float64{1, 2, 3}, "test")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
	_, _, err = m.CanonicalizeQuery([]float64{1, 2, 3}, "test")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
	if err := m.Shutdown(); err != nil {
		t.Fatalf("nil shutdown should not error: %v", err)
	}
}

func TestManagerDisabledReturnsNil(t *testing.T) {
	t.Helper()
	cfg := DefaultConfig()
	m := NewManager(cfg)
	if m != nil {
		t.Fatal("disabled config should return nil manager")
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
			modify: func(c *Config) { c.Enabled = false; c.CanonicalDim = 3 },
			want:   nil,
		},
		{
			name:   "dim too small",
			modify: func(c *Config) { c.Enabled = true; c.CanonicalDim = 32 },
			want:   ErrInvalidCanonicalDim,
		},
		{
			name:   "dim too large",
			modify: func(c *Config) { c.Enabled = true; c.CanonicalDim = 16384 },
			want:   ErrInvalidCanonicalDim,
		},
		{
			name:   "dim not power of 2",
			modify: func(c *Config) { c.Enabled = true; c.CanonicalDim = 1000 },
			want:   ErrCanonicalDimNotPowerOfTwo,
		},
		{
			name:   "whitening warmup too small",
			modify: func(c *Config) { c.Enabled = true; c.WhiteningWarmup = 50 },
			want:   ErrWhiteningWarmupTooSmall,
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
