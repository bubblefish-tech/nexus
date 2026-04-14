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

// Retrieval profile names. Every query resolves to exactly one of these.
//
// Reference: Tech Spec Section 3.5.
const (
	ProfileFast         = "fast"
	ProfileBalanced     = "balanced"
	ProfileDeep         = "deep"
	ProfileClusterAware = "cluster-aware"
	ProfileWake         = "wake"
)

// WakeDefaultLimit is the default top_k for the wake profile. Tuned to
// produce ~170 tokens of critical context after projection.
const WakeDefaultLimit = 20

// profileStages maps each profile to its set of enabled cascade stages.
//
//	fast:     0, 1, 3          — no vector search, lowest latency
//	balanced: 0, 1, 2, 3, 4, 5 — general-purpose with semantic + decay
//	deep:     0, 2, 3, 4, 5    — skip exact cache for fresh results
//
// Reference: Tech Spec Section 3.5.
var profileStages = map[string]map[int]bool{
	ProfileFast: {
		0: true,
		1: true,
		3: true,
	},
	ProfileBalanced: {
		0: true,
		1: true,
		2: true,
		3: true,
		4: true,
		5: true,
	},
	ProfileDeep: {
		0: true,
		2: true,
		3: true,
		4: true,
		5: true,
	},
	// cluster-aware inherits balanced stages plus cluster expansion.
	// The cluster expansion happens as a post-retrieval step, not a separate stage.
	// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.4.
	ProfileClusterAware: {
		0: true,
		1: true,
		2: true,
		3: true,
		4: true,
		5: true,
	},
	// wake is an alias for fast, tuned for "load my critical context in one call."
	// Same stages as fast; the caller applies WakeDefaultLimit if no explicit limit is set.
	ProfileWake: {
		0: true,
		1: true,
		3: true,
	},
}

// ProfileEnabled reports whether the given cascade stage is enabled for the
// given retrieval profile. Unknown profiles are treated as balanced.
//
// This function is the single authority for stage/profile gating. It is checked
// before each stage in the cascade.
//
// Reference: Tech Spec Section 3.5.
func ProfileEnabled(stage int, profile string) bool {
	stages, ok := profileStages[profile]
	if !ok {
		stages = profileStages[ProfileBalanced]
	}
	return stages[stage]
}

// ValidProfile reports whether p is a recognised retrieval profile name.
func ValidProfile(p string) bool {
	switch p {
	case ProfileFast, ProfileBalanced, ProfileDeep, ProfileClusterAware, ProfileWake:
		return true
	}
	return false
}

// ProfileDecayEnabled reports whether temporal decay reranking is active for
// the given profile. Fast profiles disable decay entirely.
//
// Reference: Tech Spec Section 3.5 (Temporal Decay column).
func ProfileDecayEnabled(profile string) bool {
	switch profile {
	case ProfileFast, ProfileWake:
		return false
	default:
		return true
	}
}
