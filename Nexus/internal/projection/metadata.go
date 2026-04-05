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

// Package projection implements the response projection engine: field
// allowlist filtering, byte-budget truncation on word boundaries, _nexus
// metadata injection, and metadata stripping.
//
// Reference: Tech Spec Section 7.2, Section 9.3, Phase 2.
package projection

// NexusMetadata is the _nexus block appended to every query response. It
// carries pipeline diagnostics for the client without exposing internal
// routing details.
//
// Reference: Tech Spec Section 7.2.
type NexusMetadata struct {
	// Stage names the retrieval stage that produced the result set.
	// Examples: "structured", "exact_cache", "fast_path", "semantic".
	Stage string `json:"stage"`

	// SemanticUnavailable is true when the embedding provider is absent or
	// unreachable and Stages 2+4 were skipped.
	SemanticUnavailable bool `json:"semantic_unavailable"`

	// SemanticUnavailableReason explains why semantic retrieval was skipped.
	// Omitted when SemanticUnavailable is false.
	SemanticUnavailableReason string `json:"semantic_unavailable_reason,omitempty"`

	// ResultCount is the number of records returned in this response page.
	ResultCount int `json:"result_count"`

	// Truncated is true when one or more content fields were shortened to
	// satisfy the policy max_response_bytes budget.
	Truncated bool `json:"truncated"`

	// NextCursor is the opaque base64 cursor for the next result page.
	// Omitted when HasMore is false.
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates that additional result pages are available.
	HasMore bool `json:"has_more"`

	// TemporalDecayApplied is true when temporal decay reranking was applied
	// to the result set (Phase 6+).
	TemporalDecayApplied bool `json:"temporal_decay_applied"`

	// TemporalDecayMode is "exponential" or "step". Omitted when
	// TemporalDecayApplied is false.
	TemporalDecayMode string `json:"temporal_decay_mode,omitempty"`

	// ConsistencyScore is a 0.0–1.0 measure of write-to-read consistency for
	// the returned records. 0.0 means unverified; 1.0 means fully consistent.
	ConsistencyScore float64 `json:"consistency_score"`

	// Profile is the retrieval profile used: "fast", "balanced", or "deep".
	Profile string `json:"profile"`
}
