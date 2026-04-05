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
	"log/slog"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
)

// PolicyDenial is returned by Stage 0 when a query request is blocked by
// policy. The caller (typically the HTTP handler) should translate Code into
// an appropriate HTTP 403 response.
//
// Reference: Tech Spec Section 3.4 — Stage 0.
type PolicyDenial struct {
	// Code is the machine-readable error code sent to the client.
	Code string
	// Reason is the human-readable explanation sent to the client.
	Reason string
}

// Error implements the error interface so PolicyDenial can be used as an error.
func (d *PolicyDenial) Error() string {
	return d.Code + ": " + d.Reason
}

// CascadeResult holds the output of a completed cascade run. When Denial is
// non-nil the caller must return HTTP 403 — all other fields are zero-valued.
//
// Reference: Tech Spec Section 3.4.
type CascadeResult struct {
	// Records is the page of memories returned by the winning stage.
	Records []destination.TranslatedPayload
	// NextCursor is the opaque base64 cursor for the next page. Empty when
	// HasMore is false.
	NextCursor string
	// HasMore is true when additional pages are available.
	HasMore bool
	// Profile is the retrieval profile that was used.
	Profile string
	// RetrievalStage is the numeric stage number that produced results (3 in
	// this phase). Zero when Denial is set.
	RetrievalStage int
	// Denial is non-nil when Stage 0 blocked the request. The caller must
	// return HTTP 403 with Denial.Code as the error body.
	Denial *PolicyDenial
}

// CascadeRunner executes the 6-stage retrieval cascade. All state is held in
// struct fields; there are no package-level variables.
//
// Reference: Tech Spec Section 3.4.
type CascadeRunner struct {
	querier destination.Querier
	logger  *slog.Logger
}

// New creates a CascadeRunner backed by the provided querier. If logger is nil
// the default slog logger is used.
func New(querier destination.Querier, logger *slog.Logger) *CascadeRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &CascadeRunner{
		querier: querier,
		logger:  logger,
	}
}

// Run executes the 6-stage retrieval cascade for the given source policy and
// canonical query. Stages execute strictly in order 0 → 5. Each stage may
// produce results and short-circuit, pass through to the next stage, or block
// the request entirely (Stage 0 only).
//
// Stages 1, 2, 4, and 5 are stub pass-throughs in this phase. They are
// implemented in Phases 4, 5, and 6 respectively.
//
// Reference: Tech Spec Section 3.4.
func (cr *CascadeRunner) Run(ctx context.Context, src *config.Source, q CanonicalQuery) (CascadeResult, error) {
	// ── Stage 0: Policy Gate — always runs ──────────────────────────────────
	// Returns HTTP 403 with a specific denial reason when blocked.
	// Reference: Tech Spec Section 3.4 — Stage 0.
	if denial := runStage0(src, q); denial != nil {
		cr.logger.Warn("query: cascade Stage 0 denied request",
			"component", "cascade",
			"source", src.Name,
			"destination", q.Destination,
			"code", denial.Code,
		)
		return CascadeResult{Denial: denial, Profile: q.Profile}, nil
	}

	// ── Stage 1: Exact Cache — stub (Phase 4) ───────────────────────────────
	// Active when: policy.read_from_cache = true AND profile != deep.
	// When implemented: zero-dep LRU keyed by SHA256(scope+dest+params+policy).

	// ── Stage 2: Semantic Cache — stub (Phase 6) ────────────────────────────
	// Active when: embedding configured + policy allows + profile != fast.
	// When implemented: cosine similarity >= threshold (default 0.92).

	// ── Stage 3: Structured Lookup ──────────────────────────────────────────
	// Active when: metadata filters present OR exact-subject fast path.
	// Uses parameterized WHERE clauses — no SQL string concatenation ever.
	// Reference: Tech Spec Section 3.4 — Stage 3.
	records, nextCursor, hasMore, err := runStage3(ctx, cr.querier, q)
	if err != nil {
		return CascadeResult{}, err
	}

	// ── Stage 4: Semantic Retrieval — stub (Phase 5) ────────────────────────
	// Active when: embedding configured + dest.CanSemanticSearch() + profile != fast.
	// When implemented: sqlite-vec, pgvector, or Supabase RPC.

	// ── Stage 5: Hybrid Merge + Temporal Decay — stub (Phase 6) ─────────────
	// Active when: Stages 3 AND 4 both produced results.
	// When implemented: dedup by payload_id, temporal decay rerank, projection.

	return CascadeResult{
		Records:        records,
		NextCursor:     nextCursor,
		HasMore:        hasMore,
		Profile:        q.Profile,
		RetrievalStage: 3,
	}, nil
}

// runStage0 enforces the policy gate. It returns a *PolicyDenial when the
// request must be blocked, or nil to allow it to proceed to Stage 1.
//
// Checks (applied in this order):
//  1. src.CanRead must be true.
//  2. If AllowedDestinations is non-empty, q.Destination must be in the list.
//  3. If AllowedOperations is non-empty, "read" must be in the list.
//  4. If AllowedRetrievalModes is non-empty, q.Profile must be in the list.
//
// Reference: Tech Spec Section 3.4 — Stage 0.
func runStage0(src *config.Source, q CanonicalQuery) *PolicyDenial {
	if !src.CanRead {
		return &PolicyDenial{
			Code:   "source_not_permitted_to_read",
			Reason: "this source does not have read permission",
		}
	}
	if len(src.Policy.AllowedDestinations) > 0 && !containsString(src.Policy.AllowedDestinations, q.Destination) {
		return &PolicyDenial{
			Code:   "destination_not_allowed",
			Reason: "destination not permitted for this source",
		}
	}
	if len(src.Policy.AllowedOperations) > 0 && !containsString(src.Policy.AllowedOperations, "read") {
		return &PolicyDenial{
			Code:   "operation_not_allowed",
			Reason: "read operation not permitted for this source",
		}
	}
	if len(src.Policy.AllowedRetrievalModes) > 0 && !containsString(src.Policy.AllowedRetrievalModes, q.Profile) {
		return &PolicyDenial{
			Code:   "retrieval_mode_not_allowed",
			Reason: "retrieval profile not permitted for this source",
		}
	}
	return nil
}

// containsString reports whether s appears in slice. Linear scan — policy
// lists are short (typically < 10 items) so no map overhead is warranted.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
