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

package projection

import (
	"encoding/json"

	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/policy"
)

// Response is the final JSON-serializable query response. It contains the
// projected record maps and, when strip_metadata is false, the _nexus block.
//
// Records is a slice of maps rather than typed structs so that the field
// allowlist can be applied by key exclusion without reflection on every call.
type Response struct {
	Records []map[string]any `json:"records"`
	Nexus   *NexusMetadata   `json:"_nexus,omitempty"`
}

// payloadToMap serialises p to JSON and unmarshals it back into a
// map[string]any. This gives a key-addressed view of every exported field,
// including the Metadata map's entries, without bespoke reflection.
//
// The result uses the JSON tag names (e.g. "payload_id", "actor_type") which
// are the same names used in policy field_visibility.include_fields.
func payloadToMap(p destination.TranslatedPayload) (map[string]any, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// applyAllowlist filters m so that only keys listed in includeFields survive.
// If includeFields is empty or nil all fields are retained (open allowlist).
func applyAllowlist(m map[string]any, includeFields []string) map[string]any {
	if len(includeFields) == 0 {
		return m
	}
	allowed := make(map[string]struct{}, len(includeFields))
	for _, f := range includeFields {
		allowed[f] = struct{}{}
	}
	result := make(map[string]any, len(includeFields))
	for k, v := range m {
		if _, ok := allowed[k]; ok {
			result[k] = v
		}
	}
	return result
}

// Apply projects records through the policy field allowlist, enforces the byte
// budget, and assembles the final Response with optional _nexus metadata.
//
// Steps:
//  1. Each TranslatedPayload is converted to a map[string]any keyed by JSON
//     tag name.
//  2. The policy field_visibility.include_fields allowlist is applied; unlisted
//     fields are dropped. An empty allowlist retains all fields.
//  3. The "content" fields are truncated on word boundaries if the serialized
//     records exceed pol.MaxResponseBytes (0 = unlimited).
//  4. meta.Truncated is set to true if any truncation occurred.
//  5. meta.ResultCount is set to len(records).
//  6. If pol.FieldVisibility.StripMetadata is true the _nexus block is
//     omitted from the response. Otherwise it is included.
//
// Reference: Tech Spec Section 9.3, Phase 2 Behavioral Contract items 1–4.
func Apply(
	records []destination.TranslatedPayload,
	pol policy.PolicyEntry,
	meta NexusMetadata,
) Response {
	projected := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		m, err := payloadToMap(rec)
		if err != nil {
			// Serialization failure is not expected for a well-formed
			// TranslatedPayload. Skip the offending record rather than
			// crashing — the caller will see a shorter result set.
			continue
		}
		projected = append(projected, applyAllowlist(m, pol.FieldVisibility.IncludeFields))
	}

	// Apply byte-budget truncation.
	var truncated bool
	projected, truncated = FitBudget(projected, pol.MaxResponseBytes)
	if truncated {
		meta.Truncated = true
	}

	meta.ResultCount = len(projected)

	resp := Response{Records: projected}
	if !pol.FieldVisibility.StripMetadata {
		resp.Nexus = &meta
	}
	return resp
}
