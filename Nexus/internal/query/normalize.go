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

// Package query implements the 6-stage retrieval cascade for BubbleFish Nexus.
// It provides query normalization (CanonicalQuery), the cascade orchestrator, and
// Stage 3 structured lookup. Stages 1, 2, 4, and 5 are stub pass-throughs
// pending later phases.
//
// Reference: Tech Spec Section 3.4 — The 6-Stage Retrieval Cascade.
package query

import (
	"errors"

	"github.com/BubbleFish-Nexus/internal/destination"
)

// CanonicalQuery is the normalised, validated form of a query request. It is
// produced from raw QueryParams by Normalize and is the single input type for
// every cascade stage.
//
// Limit is clamped to [1, destination.MaxQueryLimit]. CursorOffset is the
// decoded integer from the opaque base64 cursor. Profile defaults to "balanced"
// when the caller does not specify one.
type CanonicalQuery struct {
	// Destination is the target destination name.
	Destination string
	// Namespace restricts results to a specific source namespace.
	Namespace string
	// Subject is a subject filter. Empty means all subjects.
	Subject string
	// Q is a free-text content filter. Empty means no filter.
	Q string
	// Limit is the page size, clamped to [1, destination.MaxQueryLimit].
	Limit int
	// CursorOffset is the decoded integer offset from the opaque base64 cursor.
	// Zero means "first page".
	CursorOffset int
	// RawCursor is the original opaque cursor as provided by the client.
	// Stored so Stage 3 can pass it through to the underlying querier unchanged.
	RawCursor string
	// Profile is the retrieval profile: "fast", "balanced", or "deep".
	// Defaults to "balanced" when the caller does not specify one.
	Profile string
	// ActorType filters results by provenance (user, agent, system). Empty
	// means no filter. Forwarded to the querier in later phases.
	ActorType string
	// Collection is an optional collection name within the destination. When
	// set, per-collection decay overrides are resolved from
	// [destination.decay.collections.<name>].
	//
	// Reference: Tech Spec Section 3.6.
	Collection string
}

// Normalize converts raw QueryParams into a CanonicalQuery, applying defaults
// and enforcing invariants:
//
//   - Limit is clamped via destination.ClampLimit (0 → 20, >200 → 200).
//   - Cursor is decoded; a decode error is returned to the caller.
//   - Profile defaults to "balanced" when empty.
//
// Normalize does NOT perform policy checks — that is Stage 0's responsibility.
//
// Reference: Tech Spec Section 3.8.
func Normalize(p destination.QueryParams) (CanonicalQuery, error) {
	limit := destination.ClampLimit(p.Limit)

	offset, err := destination.DecodeCursor(p.Cursor)
	if err != nil {
		return CanonicalQuery{}, err
	}

	profile := p.Profile
	if profile == "" {
		profile = ProfileBalanced
	}
	if !ValidProfile(profile) {
		return CanonicalQuery{}, errors.New("invalid retrieval profile: must be fast, balanced, or deep")
	}

	return CanonicalQuery{
		Destination:  p.Destination,
		Namespace:    p.Namespace,
		Subject:      p.Subject,
		Q:            p.Q,
		Limit:        limit,
		CursorOffset: offset,
		RawCursor:    p.Cursor,
		Profile:      profile,
		ActorType:    p.ActorType,
	}, nil
}
