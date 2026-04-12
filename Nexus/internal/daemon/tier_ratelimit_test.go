// Copyright © 2026 Shawn Sammartano. All rights reserved.
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

package daemon_test

import (
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/daemon"
)

// TestEffectiveRPM verifies the tier rate limit precedence chain:
// source config → tier config → global config.
func TestEffectiveRPM(t *testing.T) {
	t.Helper()
	tests := []struct {
		name      string
		srcRPM    int
		tierLevel int
		tierRPM   int
		globalRPM int
		wantRPM   int
	}{
		{
			name:    "source_overrides_tier_and_global",
			srcRPM:  200, tierLevel: 1, tierRPM: 500, globalRPM: 1000,
			wantRPM: 200,
		},
		{
			name:    "tier_overrides_global_when_source_zero",
			srcRPM:  0, tierLevel: 2, tierRPM: 500, globalRPM: 1000,
			wantRPM: 500,
		},
		{
			name:    "global_fallback_when_source_and_tier_zero",
			srcRPM:  0, tierLevel: 1, tierRPM: 0, globalRPM: 1000,
			wantRPM: 1000,
		},
		{
			name:    "tier_config_for_wrong_level_not_applied",
			srcRPM:  0, tierLevel: 3, tierRPM: 500, globalRPM: 1000,
			wantRPM: 1000, // tier config is for level 2, not 3
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tierLevel := 3
			if tc.name == "tier_config_for_wrong_level_not_applied" {
				tierLevel = 2 // intentionally wrong level
			} else {
				tierLevel = tc.tierLevel
			}

			cfg := &config.Config{
				Daemon: config.DaemonConfig{
					RateLimit: config.GlobalRateLimitConfig{
						GlobalRequestsPerMinute: tc.globalRPM,
					},
					Tiers: []config.TierRateLimitConfig{
						{Level: tierLevel, RequestsPerMinute: tc.tierRPM},
					},
				},
			}
			src := &config.Source{
				Name:  "test",
				Tier:  tc.tierLevel,
				RateLimit: config.SourceRateLimitConfig{
					RequestsPerMinute: tc.srcRPM,
				},
			}
			got := daemon.EffectiveRPM(cfg, src)
			if got != tc.wantRPM {
				t.Errorf("EffectiveRPM = %d, want %d", got, tc.wantRPM)
			}
		})
	}
}

// TestEffectiveBPS verifies the tier bytes-per-second precedence chain.
func TestEffectiveBPS(t *testing.T) {
	t.Helper()
	tests := []struct {
		name      string
		srcBPS    int64
		tierBPS   int64
		wantBPS   int64
	}{
		{"source_overrides_tier", 1000, 5000, 1000},
		{"tier_applies_when_source_zero", 0, 5000, 5000},
		{"zero_means_unlimited", 0, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Daemon: config.DaemonConfig{
					Tiers: []config.TierRateLimitConfig{
						{Level: 1, BytesPerSecond: tc.tierBPS},
					},
				},
			}
			src := &config.Source{
				Name: "test",
				Tier: 1,
				RateLimit: config.SourceRateLimitConfig{
					BytesPerSecond: tc.srcBPS,
				},
			}
			got := daemon.EffectiveBPS(cfg, src)
			if got != tc.wantBPS {
				t.Errorf("EffectiveBPS = %d, want %d", got, tc.wantBPS)
			}
		})
	}
}
