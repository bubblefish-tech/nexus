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
	"fmt"
	"sync"
	"testing"

	"github.com/BubbleFish-Nexus/internal/cache"
	"github.com/BubbleFish-Nexus/internal/destination"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeEntry(id, content string) cache.CacheEntry {
	t := t_helper_zero()
	_ = t
	return cache.CacheEntry{
		Records: []destination.TranslatedPayload{
			{PayloadID: id, Content: content},
		},
	}
}

// t_helper_zero exists only so makeEntry compiles without a *testing.T.
func t_helper_zero() struct{} { return struct{}{} }

func newCache(maxBytes int64) *cache.ExactCache {
	return cache.NewExactCache(maxBytes, nil) // nil stats — no registry needed in tests
}

func key(scope, dest, query string) [32]byte {
	return cache.BuildKey(scope, dest, "balanced", "ns", "", query, 20, 0, "ph")
}

// ---------------------------------------------------------------------------
// LRU tests
// ---------------------------------------------------------------------------

func TestLRU_BasicAddAndGet(t *testing.T) {
	l := cache.NewLRU[string, int](1024)
	l.Add("a", 1, 10)
	l.Add("b", 2, 10)

	v, ok := l.Get("a")
	if !ok {
		t.Fatal("Get(a): expected hit, got miss")
	}
	if v != 1 {
		t.Errorf("Get(a) = %d; want 1", v)
	}
}

func TestLRU_MissingKey_ReturnsFalse(t *testing.T) {
	l := cache.NewLRU[string, int](1024)
	_, ok := l.Get("missing")
	if ok {
		t.Fatal("Get(missing): expected miss, got hit")
	}
}

func TestLRU_UpdateExisting_DoesNotGrowLen(t *testing.T) {
	l := cache.NewLRU[string, int](1024)
	l.Add("k", 1, 10)
	l.Add("k", 2, 10) // update same key

	if l.Len() != 1 {
		t.Errorf("Len = %d after update; want 1", l.Len())
	}
	v, _ := l.Get("k")
	if v != 2 {
		t.Errorf("updated value = %d; want 2", v)
	}
}

func TestLRU_Remove(t *testing.T) {
	l := cache.NewLRU[string, int](1024)
	l.Add("x", 99, 10)
	l.Remove("x")

	_, ok := l.Get("x")
	if ok {
		t.Fatal("Get after Remove: expected miss, got hit")
	}
	if l.Len() != 0 {
		t.Errorf("Len = %d after Remove; want 0", l.Len())
	}
}

func TestLRU_LRUEviction_OldestEvictedFirst(t *testing.T) {
	// Capacity for exactly 2 entries of 10 bytes each.
	l := cache.NewLRU[string, string](20)
	l.Add("first", "A", 10)
	l.Add("second", "B", 10)

	// Access "first" to make it most-recently-used; "second" becomes LRU.
	l.Get("first")

	// Adding a third entry must evict "second" (LRU), not "first".
	l.Add("third", "C", 10)

	if _, ok := l.Get("second"); ok {
		t.Error("second should have been evicted but was found")
	}
	if _, ok := l.Get("first"); !ok {
		t.Error("first should be retained but was evicted")
	}
	if _, ok := l.Get("third"); !ok {
		t.Error("third should be present but was evicted")
	}
}

func TestLRU_BytesUsed_TracksCorrectly(t *testing.T) {
	l := cache.NewLRU[string, int](1024)
	l.Add("a", 1, 100)
	l.Add("b", 2, 200)
	if l.BytesUsed() != 300 {
		t.Errorf("BytesUsed = %d; want 300", l.BytesUsed())
	}
	l.Remove("a")
	if l.BytesUsed() != 200 {
		t.Errorf("BytesUsed after Remove = %d; want 200", l.BytesUsed())
	}
}

func TestLRU_Concurrency_NoRaceConditions(t *testing.T) {
	l := cache.NewLRU[int, int](1024 * 1024)
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			l.Add(n, n*2, 10)
			l.Get(n)
			if n%10 == 0 {
				l.Remove(n)
			}
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Watermark tests
// ---------------------------------------------------------------------------

func TestWatermark_InitialValue_IsZero(t *testing.T) {
	w := cache.NewWatermarkStore()
	if w.Current("dest") != 0 {
		t.Errorf("Current(unknown dest) = %d; want 0", w.Current("dest"))
	}
}

func TestWatermark_Advance_MonotonicallyIncreases(t *testing.T) {
	w := cache.NewWatermarkStore()
	v1 := w.Advance("d")
	v2 := w.Advance("d")
	v3 := w.Advance("d")
	if v1 != 1 {
		t.Errorf("first Advance = %d; want 1", v1)
	}
	if v2 != 2 {
		t.Errorf("second Advance = %d; want 2", v2)
	}
	if v3 != 3 {
		t.Errorf("third Advance = %d; want 3", v3)
	}
}

func TestWatermark_IndependentPerDest(t *testing.T) {
	w := cache.NewWatermarkStore()
	w.Advance("alpha")
	w.Advance("alpha")
	w.Advance("beta")

	if w.Current("alpha") != 2 {
		t.Errorf("alpha watermark = %d; want 2", w.Current("alpha"))
	}
	if w.Current("beta") != 1 {
		t.Errorf("beta watermark = %d; want 1", w.Current("beta"))
	}
}

// ---------------------------------------------------------------------------
// ExactCache — scope isolation
// ---------------------------------------------------------------------------

// TestExactCache_ScopeIsolation verifies that source A's cached entry is not
// visible to source B even when the query parameters are identical.
//
// Reference: Tech Spec Section 3.4 — Stage 1 (scope isolation invariant).
func TestExactCache_ScopeIsolation(t *testing.T) {
	c := newCache(DefaultMaxBytes)

	keyA := key("source-A", "sqlite", "tell me about dogs")
	keyB := key("source-B", "sqlite", "tell me about dogs")

	// source-A writes a cache entry.
	c.Put(keyA, "sqlite", makeEntry("id-a", "dogs content from A"))

	// source-B must NOT see source-A's entry.
	if _, ok := c.Get(keyB, "sqlite"); ok {
		t.Fatal("scope isolation violated: source-B retrieved source-A's cache entry")
	}

	// source-A can still retrieve its own entry.
	if _, ok := c.Get(keyA, "sqlite"); !ok {
		t.Fatal("source-A should hit its own cache entry but got a miss")
	}
}

// ---------------------------------------------------------------------------
// ExactCache — watermark invalidation
// ---------------------------------------------------------------------------

// TestExactCache_WatermarkInvalidation verifies that advancing the watermark
// (simulating a write delivery) stales all existing entries for that destination.
//
// Reference: Tech Spec Section 3.4 — Stage 1 (watermark freshness check).
func TestExactCache_WatermarkInvalidation(t *testing.T) {
	c := newCache(DefaultMaxBytes)

	k := key("src", "sqlite", "dogs")
	c.Put(k, "sqlite", makeEntry("id-1", "initial content"))

	// Entry is present before invalidation.
	if _, ok := c.Get(k, "sqlite"); !ok {
		t.Fatal("expected cache hit before invalidation, got miss")
	}

	// Advance the watermark (simulates a write being delivered to "sqlite").
	c.InvalidateDest("sqlite")

	// Entry must now be stale — Get must return miss.
	if _, ok := c.Get(k, "sqlite"); ok {
		t.Fatal("watermark invalidation failed: stale entry still served after InvalidateDest")
	}
}

// TestExactCache_NewEntryAfterInvalidation verifies that a Put after
// InvalidateDest stores a fresh entry at the new watermark and is retrievable.
func TestExactCache_NewEntryAfterInvalidation(t *testing.T) {
	c := newCache(DefaultMaxBytes)
	k := key("src", "dest", "q")

	c.Put(k, "dest", makeEntry("old", "stale"))
	c.InvalidateDest("dest")
	c.Put(k, "dest", makeEntry("new", "fresh"))

	entry, ok := c.Get(k, "dest")
	if !ok {
		t.Fatal("expected hit for fresh entry, got miss")
	}
	if entry.Records[0].PayloadID != "new" {
		t.Errorf("PayloadID = %q; want new", entry.Records[0].PayloadID)
	}
}

// ---------------------------------------------------------------------------
// ExactCache — LRU eviction
// ---------------------------------------------------------------------------

// TestExactCache_LRUEviction verifies that adding entries beyond the byte cap
// evicts the least-recently-used entries.
//
// Reference: Tech Spec Section 3.4 — Stage 1 (LRU capped at configurable max).
func TestExactCache_LRUEviction(t *testing.T) {
	// Use a very small cache (1 byte) so every Put evicts all prior entries.
	c := newCache(1)

	k1 := key("src", "dest", "query-one")
	k2 := key("src", "dest", "query-two")

	c.Put(k1, "dest", makeEntry("id-1", "first"))
	c.Put(k2, "dest", makeEntry("id-2", "second")) // k1 must be evicted

	if _, ok := c.Get(k1, "dest"); ok {
		t.Error("k1 should have been evicted by LRU but is still present")
	}
	if _, ok := c.Get(k2, "dest"); !ok {
		t.Error("k2 should be present but was evicted")
	}
}

// ---------------------------------------------------------------------------
// ExactCache — basic hit/miss
// ---------------------------------------------------------------------------

func TestExactCache_Hit(t *testing.T) {
	c := newCache(DefaultMaxBytes)
	k := key("src", "dest", "q")
	want := makeEntry("id-42", "hello world")
	c.Put(k, "dest", want)

	got, ok := c.Get(k, "dest")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if len(got.Records) != 1 || got.Records[0].PayloadID != "id-42" {
		t.Errorf("got PayloadID %q; want id-42", got.Records[0].PayloadID)
	}
}

func TestExactCache_Miss_AbsentKey(t *testing.T) {
	c := newCache(DefaultMaxBytes)
	k := key("src", "dest", "never-stored")

	if _, ok := c.Get(k, "dest"); ok {
		t.Fatal("expected miss for absent key, got hit")
	}
}

// ---------------------------------------------------------------------------
// ExactCache — BuildKey scope isolation property
// ---------------------------------------------------------------------------

func TestBuildKey_DifferentScopes_ProduceDifferentKeys(t *testing.T) {
	kA := cache.BuildKey("A", "dest", "balanced", "ns", "subj", "q", 20, 0, "ph")
	kB := cache.BuildKey("B", "dest", "balanced", "ns", "subj", "q", 20, 0, "ph")
	if kA == kB {
		t.Fatal("different scopes produced the same cache key — scope isolation broken")
	}
}

func TestBuildKey_SameInputs_ProduceSameKey(t *testing.T) {
	k1 := cache.BuildKey("src", "dest", "balanced", "ns", "s", "q", 10, 0, "ph")
	k2 := cache.BuildKey("src", "dest", "balanced", "ns", "s", "q", 10, 0, "ph")
	if k1 != k2 {
		t.Fatal("same inputs produced different keys — key derivation is not deterministic")
	}
}

func TestBuildKey_DifferentPolicyHash_ProducesDifferentKey(t *testing.T) {
	k1 := cache.BuildKey("src", "dest", "balanced", "ns", "", "q", 20, 0, "policy-v1")
	k2 := cache.BuildKey("src", "dest", "balanced", "ns", "", "q", 20, 0, "policy-v2")
	if k1 == k2 {
		t.Fatal("different policy hashes produced the same key — policy changes will not invalidate cache")
	}
}

// ---------------------------------------------------------------------------
// ExactCache — concurrency
// ---------------------------------------------------------------------------

// TestExactCache_Concurrency_NoRaceConditions hammers the cache from 100
// goroutines simultaneously. The -race detector must report zero races.
//
// Reference: Phase 4 Verification Gate (100 goroutines: zero race reports).
func TestExactCache_Concurrency_NoRaceConditions(t *testing.T) {
	c := newCache(DefaultMaxBytes)

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			scope := fmt.Sprintf("source-%d", n%5)
			q := fmt.Sprintf("query-%d", n%10)
			dest := fmt.Sprintf("dest-%d", n%3)
			k := key(scope, dest, q)

			// Mix of Puts, Gets, and InvalidateDests.
			c.Put(k, dest, makeEntry(fmt.Sprintf("id-%d", n), "content"))
			c.Get(k, dest)
			if n%20 == 0 {
				c.InvalidateDest(dest)
			}
		}(i)
	}
	wg.Wait()
}

// DefaultMaxBytes re-exports the package constant for test readability.
const DefaultMaxBytes = cache.DefaultMaxBytes
