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

package cache_test

import (
	"sync"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/cache"
	"github.com/bubblefish-tech/nexus/internal/destination"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newSemanticCache(maxEntries int) *cache.SemanticCache {
	return cache.NewSemanticCache(maxEntries, nil) // nil stats — no registry in tests
}

func makeSemanticEntry(id, content string) cache.SemanticCacheEntry {
	return cache.SemanticCacheEntry{
		Records: []destination.TranslatedPayload{
			{PayloadID: id, Content: content},
		},
	}
}

func scopeKey(source, dest, profile, namespace string) [32]byte {
	return cache.SemanticScopeKey(source, dest, profile, namespace)
}

// ---------------------------------------------------------------------------
// SemanticCache — basic hit/miss
// ---------------------------------------------------------------------------

func TestSemanticCache_Hit_ExactVector(t *testing.T) {
	sc := newSemanticCache(10)

	vec := []float32{1, 0, 0}
	scope := scopeKey("source-a", "sqlite", "balanced", "ns")
	entry := makeSemanticEntry("id-1", "memory content")

	sc.Put(scope, vec, "sqlite", entry)

	// Exact same vector should hit.
	got, ok := sc.Get(scope, vec, "sqlite", 0.92)
	if !ok {
		t.Fatal("expected cache hit for exact vector, got miss")
	}
	if len(got.Records) == 0 || got.Records[0].PayloadID != "id-1" {
		t.Errorf("got records %v; want PayloadID = id-1", got.Records)
	}
}

func TestSemanticCache_Hit_SimilarVector(t *testing.T) {
	sc := newSemanticCache(10)

	// Store vector along the X axis.
	stored := []float32{1, 0, 0}
	scope := scopeKey("source-a", "sqlite", "balanced", "ns")
	sc.Put(scope, stored, "sqlite", makeSemanticEntry("id-1", "content"))

	// Query with a nearby vector (cos ~ 0.9998 — above threshold).
	query := []float32{0.9999, 0.01, 0}
	_, ok := sc.Get(scope, query, "sqlite", 0.92)
	if !ok {
		t.Error("expected hit for near-identical vector, got miss")
	}
}

func TestSemanticCache_Miss_OrthogonalVector(t *testing.T) {
	sc := newSemanticCache(10)

	stored := []float32{1, 0, 0}
	scope := scopeKey("source-a", "sqlite", "balanced", "ns")
	sc.Put(scope, stored, "sqlite", makeSemanticEntry("id-1", "content"))

	// Orthogonal vector: cosine similarity = 0, far below threshold.
	query := []float32{0, 1, 0}
	_, ok := sc.Get(scope, query, "sqlite", 0.92)
	if ok {
		t.Error("expected miss for orthogonal vector, got hit")
	}
}

func TestSemanticCache_Miss_EmptyCache(t *testing.T) {
	sc := newSemanticCache(10)
	scope := scopeKey("source-a", "sqlite", "balanced", "ns")
	_, ok := sc.Get(scope, []float32{1, 0, 0}, "sqlite", 0.92)
	if ok {
		t.Error("expected miss on empty cache, got hit")
	}
}

// ---------------------------------------------------------------------------
// SemanticCache — scope isolation
// ---------------------------------------------------------------------------

// TestSemanticCache_ScopeIsolation verifies that source A's entry is not
// accessible to source B, even for an identical query vector.
func TestSemanticCache_ScopeIsolation(t *testing.T) {
	sc := newSemanticCache(10)

	vec := []float32{1, 0, 0}
	scopeA := scopeKey("source-a", "sqlite", "balanced", "ns")
	scopeB := scopeKey("source-b", "sqlite", "balanced", "ns")

	sc.Put(scopeA, vec, "sqlite", makeSemanticEntry("A-id", "source A memory"))

	// Source A should hit.
	if _, ok := sc.Get(scopeA, vec, "sqlite", 0.92); !ok {
		t.Error("source A: expected hit, got miss")
	}

	// Source B must NOT see source A's entry.
	if _, ok := sc.Get(scopeB, vec, "sqlite", 0.92); ok {
		t.Error("scope isolation FAIL: source B retrieved source A's cached entry")
	}
}

// TestSemanticCache_ScopeIsolation_DifferentDest verifies that the same source
// with a different destination name produces distinct scope keys.
func TestSemanticCache_ScopeIsolation_DifferentDest(t *testing.T) {
	sc := newSemanticCache(10)
	vec := []float32{1, 0, 0}

	scopeSQLite := scopeKey("src", "sqlite", "balanced", "ns")
	scopePostgres := scopeKey("src", "postgres", "balanced", "ns")

	sc.Put(scopeSQLite, vec, "sqlite", makeSemanticEntry("sqlite-id", "content"))

	if _, ok := sc.Get(scopeSQLite, vec, "sqlite", 0.92); !ok {
		t.Error("sqlite scope: expected hit, got miss")
	}
	if _, ok := sc.Get(scopePostgres, vec, "postgres", 0.92); ok {
		t.Error("scope isolation FAIL: postgres scope hit sqlite entry")
	}
}

// ---------------------------------------------------------------------------
// SemanticCache — watermark invalidation
// ---------------------------------------------------------------------------

func TestSemanticCache_WatermarkInvalidation(t *testing.T) {
	sc := newSemanticCache(10)

	vec := []float32{1, 0, 0}
	scope := scopeKey("src", "sqlite", "balanced", "ns")
	sc.Put(scope, vec, "sqlite", makeSemanticEntry("id-1", "content"))

	// Entry should hit before invalidation.
	if _, ok := sc.Get(scope, vec, "sqlite", 0.92); !ok {
		t.Fatal("expected hit before invalidation")
	}

	// Advance watermark (simulates a write delivered to the destination).
	sc.InvalidateDest("sqlite")

	// Entry should now miss (stale watermark).
	if _, ok := sc.Get(scope, vec, "sqlite", 0.92); ok {
		t.Error("expected miss after watermark advance, got hit")
	}
}

func TestSemanticCache_NewEntryAfterInvalidation(t *testing.T) {
	sc := newSemanticCache(10)

	vec := []float32{1, 0, 0}
	scope := scopeKey("src", "sqlite", "balanced", "ns")

	sc.Put(scope, vec, "sqlite", makeSemanticEntry("old-id", "old"))
	sc.InvalidateDest("sqlite")
	sc.Put(scope, vec, "sqlite", makeSemanticEntry("new-id", "new"))

	got, ok := sc.Get(scope, vec, "sqlite", 0.92)
	if !ok {
		t.Fatal("expected hit for new post-invalidation entry, got miss")
	}
	if got.Records[0].PayloadID != "new-id" {
		t.Errorf("got PayloadID = %q; want new-id", got.Records[0].PayloadID)
	}
}

// ---------------------------------------------------------------------------
// SemanticCache — FIFO eviction
// ---------------------------------------------------------------------------

func TestSemanticCache_Eviction_FIFO(t *testing.T) {
	sc := newSemanticCache(2) // capacity = 2 entries

	scope := scopeKey("src", "sqlite", "balanced", "ns")

	// Put 2 entries at different vectors so they don't collide on similarity.
	vecA := []float32{1, 0, 0}
	vecB := []float32{0, 1, 0}
	vecC := []float32{0, 0, 1}

	sc.Put(scope, vecA, "sqlite", makeSemanticEntry("A", "A content"))
	sc.Put(scope, vecB, "sqlite", makeSemanticEntry("B", "B content"))

	// Both A and B present.
	if _, ok := sc.Get(scope, vecA, "sqlite", 0.99); !ok {
		t.Error("A should be present before eviction")
	}
	if _, ok := sc.Get(scope, vecB, "sqlite", 0.99); !ok {
		t.Error("B should be present before eviction")
	}

	// Add C — evicts A (oldest).
	sc.Put(scope, vecC, "sqlite", makeSemanticEntry("C", "C content"))

	if sc.Len() != 2 {
		t.Errorf("Len = %d; want 2 after eviction", sc.Len())
	}

	// A should be evicted.
	if _, ok := sc.Get(scope, vecA, "sqlite", 0.99); ok {
		t.Error("A should have been evicted (FIFO), got hit")
	}
	// B and C should still be present.
	if _, ok := sc.Get(scope, vecB, "sqlite", 0.99); !ok {
		t.Error("B should still be present")
	}
	if _, ok := sc.Get(scope, vecC, "sqlite", 0.99); !ok {
		t.Error("C should be present after insertion")
	}
}

// ---------------------------------------------------------------------------
// SemanticCache — concurrency
// ---------------------------------------------------------------------------

func TestSemanticCache_Concurrency_NoRaceConditions(t *testing.T) {
	sc := newSemanticCache(100)
	scope := scopeKey("src", "sqlite", "balanced", "ns")

	const goroutines = 50
	const ops = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			vec := []float32{float32(g % 10), float32((g + 1) % 10), 0}
			for i := 0; i < ops; i++ {
				if i%2 == 0 {
					sc.Put(scope, vec, "sqlite", makeSemanticEntry("id", "content"))
				} else {
					sc.Get(scope, vec, "sqlite", 0.92)
				}
			}
		}()
	}
	wg.Wait()
	// No race detector reports = pass.
}

// ---------------------------------------------------------------------------
// SemanticScopeKey — uniqueness
// ---------------------------------------------------------------------------

func TestSemanticScopeKey_DifferentInputsProduceDifferentKeys(t *testing.T) {
	cases := []struct {
		a, b [4]string
	}{
		{[4]string{"src-a", "sqlite", "balanced", "ns"}, [4]string{"src-b", "sqlite", "balanced", "ns"}},
		{[4]string{"src", "sqlite", "balanced", "ns"}, [4]string{"src", "postgres", "balanced", "ns"}},
		{[4]string{"src", "sqlite", "balanced", "ns"}, [4]string{"src", "sqlite", "deep", "ns"}},
		{[4]string{"src", "sqlite", "balanced", "ns"}, [4]string{"src", "sqlite", "balanced", "other-ns"}},
	}
	for _, tc := range cases {
		ka := cache.SemanticScopeKey(tc.a[0], tc.a[1], tc.a[2], tc.a[3])
		kb := cache.SemanticScopeKey(tc.b[0], tc.b[1], tc.b[2], tc.b[3])
		if ka == kb {
			t.Errorf("scope key collision: %v and %v produced the same key", tc.a, tc.b)
		}
	}
}
