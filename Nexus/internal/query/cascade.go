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
	"crypto/sha256"
	"fmt"
	"log/slog"

	"github.com/BubbleFish-Nexus/internal/cache"
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
	querier    destination.Querier
	logger     *slog.Logger
	exactCache *cache.ExactCache
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

// WithExactCache attaches an ExactCache to the runner, enabling Stage 1
// retrieval. Returns the runner for method chaining.
//
// Reference: Tech Spec Section 3.4 — Stage 1.
func (cr *CascadeRunner) WithExactCache(c *cache.ExactCache) *CascadeRunner {
	cr.exactCache = c
	return cr
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

	// ── Stage 1: Exact Cache ────────────────────────────────────────────────
	// Active when: policy.read_from_cache = true AND profile != "deep".
	// Key: SHA256(scope_hash + dest + params + policy_hash).
	// Scope isolation: source identity is embedded in the key so source A
	// cannot retrieve source B's cached entries.
	// Watermark check: entries are stale when a write was delivered after they
	// were cached; stale entries produce a miss.
	// Reference: Tech Spec Section 3.4 — Stage 1.
	var cacheKey [32]byte
	useCache := cr.exactCache != nil && src.Policy.Cache.ReadFromCache && q.Profile != "deep"
	if useCache {
		ph := sourcePolicyHash(src.Policy.Cache)
		cacheKey = cache.BuildKey(src.Name, q.Destination, q.Profile,
			q.Namespace, q.Subject, q.Q, q.Limit, q.CursorOffset, ph)
		if entry, ok := cr.exactCache.Get(cacheKey, q.Destination); ok {
			cr.logger.Debug("query: Stage 1 cache hit",
				"component", "cascade",
				"source", src.Name,
				"destination", q.Destination,
			)
			return CascadeResult{
				Records:        entry.Records,
				NextCursor:     entry.NextCursor,
				HasMore:        entry.HasMore,
				Profile:        q.Profile,
				RetrievalStage: 1,
			}, nil
		}
	}

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

	// ── Stage 1 write-back ───────────────────────────────────────────────────
	// Store Stage 3 results in the exact cache for future requests when the
	// source policy permits caching writes.
	if useCache && cr.exactCache != nil && src.Policy.Cache.WriteToCache {
		cr.exactCache.Put(cacheKey, q.Destination, cache.CacheEntry{
			Records:    records,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		})
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

// sourcePolicyHash derives a short digest of the policy fields that affect
// result shape. A change in any of these fields produces a different digest,
// causing cache misses for stale policy-shaped results.
//
// The hash covers fields from PolicyCacheConfig that influence what the cache
// serves: ReadFromCache, WriteToCache, MaxTTLSeconds, and the semantic
// similarity threshold used in Stage 2.
func sourcePolicyHash(p config.PolicyCacheConfig) string {
	h := sha256.New()
	fmt.Fprintf(h, "rfc=%v\x00wtc=%v\x00ttl=%d\x00sst=%.6f",
		p.ReadFromCache, p.WriteToCache, p.MaxTTLSeconds, p.SemanticSimilarityThreshold)
	return fmt.Sprintf("%x", h.Sum(nil))[:16] // first 8 bytes (16 hex chars) is sufficient
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
