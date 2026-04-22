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

import (
	"context"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// runStage3 executes Stage 3 (Structured Lookup) of the retrieval cascade.
//
// It translates the CanonicalQuery into a destination.QueryParams and delegates
// to the querier. All SQL in the querier implementation uses parameterized
// statements — no string concatenation is performed at any layer.
//
// When the query shape is subject-only + limit (no free-text, no metadata
// filters), the querier may select the exact-subject fast path automatically
// (SELECT … WHERE subject = ? ORDER BY timestamp DESC LIMIT ?). The cascade
// reports this as retrieval stage 3 regardless.
//
// Reference: Tech Spec Section 3.4 — Stage 3, Section 3.7.
func runStage3(
	_ context.Context,
	q destination.Querier,
	cq CanonicalQuery,
) (records []destination.TranslatedPayload, nextCursor string, hasMore bool, err error) {
	// Re-encode the offset back to an opaque cursor so the querier receives the
	// same format it originally emitted. Offset 0 is represented as the empty
	// string (i.e., first page — no cursor).
	cursor := ""
	if cq.CursorOffset > 0 {
		cursor = destination.EncodeCursor(cq.CursorOffset)
	}

	params := destination.QueryParams{
		Destination: cq.Destination,
		Namespace:   cq.Namespace,
		Subject:     cq.Subject,
		Q:           cq.Q,
		Limit:       cq.Limit,
		Cursor:      cursor,
		Profile:     cq.Profile,
		ActorType:   cq.ActorType,
		TemporalBinFilter: cq.TemporalBin >= 0,
		TemporalBin:       cq.TemporalBin,
	}

	result, err := q.Query(params)
	if err != nil {
		return nil, "", false, err
	}
	return result.Records, result.NextCursor, result.HasMore, nil
}
