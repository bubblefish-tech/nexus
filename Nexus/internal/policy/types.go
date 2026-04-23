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

// Package policy compiles source policy configurations into a versioned JSON
// artifact (compiled/policies.json) that the daemon loads at startup for fast,
// lock-free policy enforcement.
//
// Policy compilation is a build-time step run via `nexus build`. It
// validates all [source.policy] blocks against the known destination set and
// serialises the result atomically. The daemon never re-reads raw TOML on the
// hot path — only the compiled artifact.
package policy

import "time"

// CompiledPolicies is the root structure written to compiled/policies.json.
// It records the daemon version and timestamp so operators can audit when
// policies were last compiled.
type CompiledPolicies struct {
	Version    string        `json:"version"`
	CompiledAt time.Time     `json:"compiled_at"`
	Policies   []PolicyEntry `json:"policies"`
}

// PolicyEntry is the compiled policy for a single source. It mirrors all
// [source.policy] TOML fields verbatim so no TOML parsing is required at
// runtime.
//
// Reference: Tech Spec Section 9.3.
type PolicyEntry struct {
	Source                string               `json:"source"`
	AllowedDestinations   []string             `json:"allowed_destinations"`
	AllowedOperations     []string             `json:"allowed_operations"`
	AllowedRetrievalModes []string             `json:"allowed_retrieval_modes"`
	AllowedProfiles       []string             `json:"allowed_profiles"`
	MaxResults            int                  `json:"max_results"`
	MaxResponseBytes      int                  `json:"max_response_bytes"`
	FieldVisibility       FieldVisibilityEntry `json:"field_visibility"`
	Cache                 PolicyCacheEntry     `json:"cache"`
	Decay                 PolicyDecayEntry     `json:"decay"`
}

// FieldVisibilityEntry mirrors [source.policy.field_visibility].
type FieldVisibilityEntry struct {
	IncludeFields []string `json:"include_fields"`
	StripMetadata bool     `json:"strip_metadata"`
}

// PolicyCacheEntry mirrors [source.policy.cache].
type PolicyCacheEntry struct {
	ReadFromCache               bool    `json:"read_from_cache"`
	WriteToCache                bool    `json:"write_to_cache"`
	MaxTTLSeconds               int     `json:"max_ttl_seconds"`
	SemanticSimilarityThreshold float64 `json:"semantic_similarity_threshold"`
}

// PolicyDecayEntry mirrors [source.policy.decay] (per-source override).
// All fields are optional; zero values mean "use the daemon-level default".
type PolicyDecayEntry struct {
	HalfLifeDays      float64 `json:"half_life_days"`
	DecayMode         string  `json:"decay_mode"`
	StepThresholdDays float64 `json:"step_threshold_days"`
}
