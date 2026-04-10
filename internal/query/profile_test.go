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

package query

import "testing"

func TestProfileEnabled(t *testing.T) {
	t.Helper()

	tests := []struct {
		name    string
		stage   int
		profile string
		want    bool
	}{
		// fast: stages 0, 1, 3 only
		{"fast_stage0", 0, ProfileFast, true},
		{"fast_stage1", 1, ProfileFast, true},
		{"fast_stage2", 2, ProfileFast, false},
		{"fast_stage3", 3, ProfileFast, true},
		{"fast_stage4", 4, ProfileFast, false},
		{"fast_stage5", 5, ProfileFast, false},

		// balanced: stages 0, 1, 2, 3, 4, 5
		{"balanced_stage0", 0, ProfileBalanced, true},
		{"balanced_stage1", 1, ProfileBalanced, true},
		{"balanced_stage2", 2, ProfileBalanced, true},
		{"balanced_stage3", 3, ProfileBalanced, true},
		{"balanced_stage4", 4, ProfileBalanced, true},
		{"balanced_stage5", 5, ProfileBalanced, true},

		// deep: stages 0, 2, 3, 4, 5 (skip exact cache)
		{"deep_stage0", 0, ProfileDeep, true},
		{"deep_stage1", 1, ProfileDeep, false},
		{"deep_stage2", 2, ProfileDeep, true},
		{"deep_stage3", 3, ProfileDeep, true},
		{"deep_stage4", 4, ProfileDeep, true},
		{"deep_stage5", 5, ProfileDeep, true},

		// unknown profile falls back to balanced
		{"unknown_stage0", 0, "unknown", true},
		{"unknown_stage1", 1, "unknown", true},
		{"unknown_stage2", 2, "unknown", true},
		{"unknown_stage4", 4, "unknown", true},

		// out-of-range stage
		{"fast_stage99", 99, ProfileFast, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProfileEnabled(tt.stage, tt.profile)
			if got != tt.want {
				t.Errorf("ProfileEnabled(%d, %q) = %v, want %v", tt.stage, tt.profile, got, tt.want)
			}
		})
	}
}

func TestValidProfile(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"fast", true},
		{"balanced", true},
		{"deep", true},
		{"", false},
		{"turbo", false},
		{"FAST", false},
	}

	for _, tt := range tests {
		name := tt.input
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			if got := ValidProfile(tt.input); got != tt.want {
				t.Errorf("ValidProfile(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestProfileDecayEnabled(t *testing.T) {
	tests := []struct {
		profile string
		want    bool
	}{
		{ProfileFast, false},
		{ProfileBalanced, true},
		{ProfileDeep, true},
	}

	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			if got := ProfileDecayEnabled(tt.profile); got != tt.want {
				t.Errorf("ProfileDecayEnabled(%q) = %v, want %v", tt.profile, got, tt.want)
			}
		})
	}
}
