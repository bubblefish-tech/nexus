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

package query_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/BubbleFish-Nexus/internal/cache"
	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/query"
)

// testPolicyHash replicates the sourcePolicyHash logic from cascade.go so
// tests can build cache keys that match what the cascade runner computes.
func testPolicyHash(p config.PolicyCacheConfig) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "rfc=%v\x00wtc=%v\x00ttl=%d\x00sst=%.6f",
		p.ReadFromCache, p.WriteToCache, p.MaxTTLSeconds, p.SemanticSimilarityThreshold)
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}

// ---------------------------------------------------------------------------
// Phase R-8 Verification Gate: Semantic Short-Circuit + Fast Path
// Reference: Tech Spec Section 3.7, Verification Guide Phase R-8.
// ---------------------------------------------------------------------------

// TestFastPath_SubjectAndLimit_UsesFastPath verifies that a query with only
// subject + limit (no Q, no actor_type, no cursor) triggers the fast path and
// reports RetrievalStage = StageFastPath with StageName "fast_path".
func TestFastPath_SubjectAndLimit_UsesFastPath(t *testing.T) {
	records := []destination.TranslatedPayload{
		makeRecord("fp-1", "fast path memory"),
		makeRecord("fp-2", "another memory"),
	}
	mq := &mockQuerier{
		result: destination.QueryResult{
			Records: records,
			HasMore: false,
		},
	}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: true}

	cq := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Subject:     "user:42",
		Profile:     "balanced",
		Limit:       20,
		// Q is empty, ActorType is empty, CursorOffset is 0 → fast path.
	}

	result, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Denial != nil {
		t.Fatalf("unexpected denial: %v", result.Denial)
	}

	// Verification: RetrievalStage must be StageFastPath.
	if result.RetrievalStage != query.StageFastPath {
		t.Errorf("RetrievalStage = %d; want %d (StageFastPath)", result.RetrievalStage, query.StageFastPath)
	}

	// Verification: StageName must be "fast_path".
	if name := query.StageName(result.RetrievalStage); name != "fast_path" {
		t.Errorf("StageName = %q; want %q", name, "fast_path")
	}

	// Results should be returned.
	if len(result.Records) != 2 {
		t.Fatalf("len(Records) = %d; want 2", len(result.Records))
	}
	if result.Records[0].PayloadID != "fp-1" {
		t.Errorf("Records[0].PayloadID = %q; want fp-1", result.Records[0].PayloadID)
	}

	// Querier should have been called with the subject filter.
	if mq.lastParams.Subject != "user:42" {
		t.Errorf("querier Subject = %q; want user:42", mq.lastParams.Subject)
	}
}

// TestFastPath_ZeroResults_ReturnsEmpty verifies that the fast path returning
// 0 results returns an empty response and does NOT fall through to the cascade.
func TestFastPath_ZeroResults_ReturnsEmpty(t *testing.T) {
	mq := &mockQuerier{
		result: destination.QueryResult{
			Records: []destination.TranslatedPayload{}, // empty
		},
	}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: true}

	cq := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Subject:     "nonexistent:99",
		Profile:     "balanced",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must be fast path — even with 0 results.
	if result.RetrievalStage != query.StageFastPath {
		t.Errorf("RetrievalStage = %d; want %d (StageFastPath) — 0 results must NOT fall through to cascade",
			result.RetrievalStage, query.StageFastPath)
	}

	// Must return empty, not nil.
	if result.Records == nil {
		t.Error("Records is nil; want empty slice")
	}
	if len(result.Records) != 0 {
		t.Errorf("len(Records) = %d; want 0", len(result.Records))
	}
}

// TestFastPath_QueryWithQ_DoesNotUseFastPath verifies that a query with a
// free-text Q field does NOT trigger the fast path, even if subject is present.
func TestFastPath_QueryWithQ_DoesNotUseFastPath(t *testing.T) {
	records := []destination.TranslatedPayload{makeRecord("id-1", "some content")}
	mq := &mockQuerier{
		result: destination.QueryResult{Records: records},
	}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: true}

	cq := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Subject:     "user:42",
		Q:           "search text", // Q is non-empty → NOT fast path
		Profile:     "fast",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RetrievalStage == query.StageFastPath {
		t.Error("query with Q field should NOT use fast path")
	}
	// Should fall through to Stage 3 (structured lookup) for profile=fast.
	if result.RetrievalStage != 3 {
		t.Errorf("RetrievalStage = %d; want 3", result.RetrievalStage)
	}
}

// TestFastPath_QueryWithActorType_DoesNotUseFastPath verifies that a query
// with an actor_type filter does NOT trigger the fast path.
func TestFastPath_QueryWithActorType_DoesNotUseFastPath(t *testing.T) {
	mq := &mockQuerier{
		result: destination.QueryResult{Records: []destination.TranslatedPayload{makeRecord("id-1", "c")}},
	}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: true}

	cq := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Subject:     "user:42",
		ActorType:   "agent", // actor_type present → NOT fast path
		Profile:     "fast",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RetrievalStage == query.StageFastPath {
		t.Error("query with ActorType should NOT use fast path")
	}
}

// TestFastPath_NoSubject_DoesNotUseFastPath verifies that a query without a
// subject field does NOT trigger the fast path.
func TestFastPath_NoSubject_DoesNotUseFastPath(t *testing.T) {
	mq := &mockQuerier{
		result: destination.QueryResult{Records: []destination.TranslatedPayload{}},
	}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: true}

	cq := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Profile:     "balanced",
		Limit:       20,
		// Subject is empty → NOT fast path.
	}

	result, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RetrievalStage == query.StageFastPath {
		t.Error("query without subject should NOT use fast path")
	}
}

// TestFastPath_SkipsCacheAndSemantic verifies that the fast path bypasses all
// cache stages and semantic retrieval entirely — no cache reads, no embedding.
func TestFastPath_SkipsCacheAndSemantic(t *testing.T) {
	records := []destination.TranslatedPayload{makeRecord("fp-1", "content")}
	embedder := &mockEmbedder{}
	sq := &mockSemanticQuerier{
		mockQuerier: mockQuerier{
			result: destination.QueryResult{Records: records},
		},
		semanticResult: []destination.ScoredRecord{
			{Payload: records[0], Score: 0.99},
		},
	}

	ec := cache.NewExactCache(1<<20, nil)
	sc := cache.NewSemanticCache(100, nil)

	runner := query.New(sq, nil).
		WithExactCache(ec).
		WithSemanticCache(sc).
		WithEmbeddingClient(embedder, nil).
		WithRetrievalConfig(config.RetrievalConfig{DefaultProfile: "balanced"})

	src := &config.Source{
		Name:      "s",
		Namespace: "ns",
		CanRead:   true,
		Policy: config.SourcePolicyConfig{
			Cache: config.PolicyCacheConfig{
				ReadFromCache:  true,
				WriteToCache:   true,
			},
		},
	}

	cq := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Subject:     "user:42",
		Profile:     "balanced",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RetrievalStage != query.StageFastPath {
		t.Errorf("RetrievalStage = %d; want %d (StageFastPath)", result.RetrievalStage, query.StageFastPath)
	}

	// Embedding should NOT have been called.
	if embedder.called {
		t.Error("embedding was called; fast path should bypass all semantic stages")
	}

	// Semantic search should NOT have been called.
	if sq.semanticCalled {
		t.Error("semantic search was called; fast path should bypass cascade entirely")
	}
}

// TestFastPath_Stage0DenialStillEnforced verifies that Stage 0 policy denial
// is enforced even for queries that would qualify for the fast path.
func TestFastPath_Stage0DenialStillEnforced(t *testing.T) {
	mq := &mockQuerier{result: destination.QueryResult{Records: []destination.TranslatedPayload{}}}
	runner := query.New(mq, nil)
	src := &config.Source{Name: "s", Namespace: "ns", CanRead: false} // denied

	cq := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Subject:     "user:42",
		Profile:     "balanced",
		Limit:       20,
	}

	result, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Denial == nil {
		t.Fatal("expected Stage 0 denial, got nil — policy must still be enforced on fast path")
	}
	if result.Denial.Code != "source_not_permitted_to_read" {
		t.Errorf("Denial.Code = %q; want source_not_permitted_to_read", result.Denial.Code)
	}
}

// ---------------------------------------------------------------------------
// Phase R-8 Verification Gate: Cache Hit Short-Circuit
// (Stages 1 and 2 already short-circuit; these tests confirm the contract.)
// ---------------------------------------------------------------------------

// TestCacheHit_Stage1_SkipsRemainingStages verifies that an exact cache hit
// (Stage 1) short-circuits the cascade — no Stage 3 query is executed.
func TestCacheHit_Stage1_SkipsRemainingStages(t *testing.T) {
	called := false
	inner := &mockQuerier{result: destination.QueryResult{}}
	detector := &callDetector{inner: inner, called: &called}

	ec := cache.NewExactCache(1<<20, nil)

	cq := query.CanonicalQuery{
		Destination: "mem",
		Namespace:   "ns",
		Q:           "test query", // Q present → not fast path
		Profile:     "balanced",
		Limit:       20,
	}

	policyCache := config.PolicyCacheConfig{ReadFromCache: true, WriteToCache: true}

	// Pre-populate cache with the correct policy hash.
	ph := testPolicyHash(policyCache)
	cacheKey := cache.BuildKey("s", cq.Destination, cq.Profile,
		cq.Namespace, cq.Subject, cq.Q, cq.Limit, cq.CursorOffset, ph)
	ec.Put(cacheKey, "mem", cache.CacheEntry{
		Records: []destination.TranslatedPayload{makeRecord("cached-1", "cached")},
	})

	runner := query.New(detector, nil).WithExactCache(ec)
	src := &config.Source{
		Name:      "s",
		Namespace: "ns",
		CanRead:   true,
		Policy: config.SourcePolicyConfig{
			Cache: policyCache,
		},
	}

	result, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.RetrievalStage != 1 {
		t.Errorf("RetrievalStage = %d; want 1 (exact cache hit)", result.RetrievalStage)
	}
	if called {
		t.Error("querier was called — Stage 1 cache hit should skip remaining stages")
	}
	if len(result.Records) != 1 || result.Records[0].PayloadID != "cached-1" {
		t.Errorf("unexpected records: %v", result.Records)
	}
}

// ---------------------------------------------------------------------------
// IsFastPath unit tests
// ---------------------------------------------------------------------------

func TestIsFastPath_TableDriven(t *testing.T) {
	cases := []struct {
		name string
		q    query.CanonicalQuery
		want bool
	}{
		{
			name: "subject only",
			q:    query.CanonicalQuery{Subject: "user:1", Limit: 10},
			want: true,
		},
		{
			name: "subject with Q",
			q:    query.CanonicalQuery{Subject: "user:1", Q: "hello", Limit: 10},
			want: false,
		},
		{
			name: "subject with actor_type",
			q:    query.CanonicalQuery{Subject: "user:1", ActorType: "agent", Limit: 10},
			want: false,
		},
		{
			name: "subject with cursor offset",
			q:    query.CanonicalQuery{Subject: "user:1", CursorOffset: 20, Limit: 10},
			want: false,
		},
		{
			name: "no subject",
			q:    query.CanonicalQuery{Limit: 10},
			want: false,
		},
		{
			name: "empty query",
			q:    query.CanonicalQuery{},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := query.IsFastPath(tc.q); got != tc.want {
				t.Errorf("IsFastPath = %v; want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StageName unit tests
// ---------------------------------------------------------------------------

func TestStageName(t *testing.T) {
	cases := []struct {
		stage int
		want  string
	}{
		{query.StageFastPath, "fast_path"},
		{1, "exact_cache"},
		{2, "semantic_cache"},
		{3, "structured"},
		{4, "semantic"},
		{5, "hybrid_merge"},
		{99, "unknown"},
	}
	for _, tc := range cases {
		if got := query.StageName(tc.stage); got != tc.want {
			t.Errorf("StageName(%d) = %q; want %q", tc.stage, got, tc.want)
		}
	}
}
