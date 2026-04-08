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

package cache

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/prometheus/client_golang/prometheus"
)

// SemanticStats holds Prometheus counters for Stage 2 semantic-cache events.
// Register via NewSemanticStats on the daemon's private registry.
//
// Metric names:
//   - bubblefish_cache_semantic_hits_total
//   - bubblefish_cache_semantic_misses_total
//
// Reference: Tech Spec Section 11.3.
type SemanticStats struct {
	hits   prometheus.Counter
	misses prometheus.Counter

	// hitCount and missCount are atomic counters mirroring the Prometheus
	// counters for direct read access by admin endpoints.
	hitCount  atomic.Int64
	missCount atomic.Int64
}

// NewSemanticStats creates and registers the two semantic-cache counters on reg.
// Panics only on programming errors (duplicate names, impossible with a fresh
// private registry).
func NewSemanticStats(reg prometheus.Registerer) *SemanticStats {
	hits := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bubblefish_cache_semantic_hits_total",
		Help: "Total number of Stage 2 semantic-cache hits.",
	})
	misses := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bubblefish_cache_semantic_misses_total",
		Help: "Total number of Stage 2 semantic-cache misses.",
	})
	reg.MustRegister(hits, misses)
	return &SemanticStats{hits: hits, misses: misses}
}

// Hit increments the semantic cache hit counter. Safe to call on a nil receiver.
func (s *SemanticStats) Hit() {
	if s != nil {
		s.hits.Inc()
		s.hitCount.Add(1)
	}
}

// Miss increments the semantic cache miss counter. Safe to call on a nil receiver.
func (s *SemanticStats) Miss() {
	if s != nil {
		s.misses.Inc()
		s.missCount.Add(1)
	}
}

// HitCount returns the total number of semantic cache hits since startup.
// Safe to call on a nil receiver (returns 0).
func (s *SemanticStats) HitCount() int64 {
	if s != nil {
		return s.hitCount.Load()
	}
	return 0
}

// MissCount returns the total number of semantic cache misses since startup.
// Safe to call on a nil receiver (returns 0).
func (s *SemanticStats) MissCount() int64 {
	if s != nil {
		return s.missCount.Load()
	}
	return 0
}

// DefaultSemanticMaxEntries is the default maximum number of entries in the
// semantic cache. Configurable via daemon.toml in later phases.
const DefaultSemanticMaxEntries = 1000

// semanticEntry is a single cached query result keyed by embedding vector.
// The scope hash enforces source isolation; a query from source B cannot
// retrieve an entry written by source A even for identical query text.
type semanticEntry struct {
	// scope is SHA256(sourceName + "\x00" + dest + "\x00" + profile + "\x00" + namespace).
	// Entries with different scopes are never compared.
	scope [32]byte
	// dest is the destination name, used for watermark freshness checks.
	dest string
	// queryVec is the embedding vector of the query that produced these results.
	// A new query hits this entry when cosine(newQueryVec, queryVec) >= threshold.
	queryVec []float32
	// watermark is the destination write counter at Put time. An entry is stale
	// when the destination's current watermark exceeds this value.
	watermark uint64
	// records, nextCursor, hasMore are the cached result page.
	records    []destination.TranslatedPayload
	nextCursor string
	hasMore    bool
}

// SemanticCacheEntry is the value returned on a Stage 2 cache hit.
type SemanticCacheEntry struct {
	Records    []destination.TranslatedPayload
	NextCursor string
	HasMore    bool
}

// SemanticCache is the Stage 2 semantic cache. It stores recent query results
// keyed by embedding vector. A new query hits the cache when its embedding
// is within cosine-similarity threshold of a stored query vector.
//
// Scope isolation: entries carry a scope hash derived from source + dest +
// profile + namespace. Source A cannot retrieve source B's cached results even
// for identical query text.
//
// Watermark invalidation: entries become stale when a write is delivered to
// the destination, matching the ExactCache invalidation strategy.
//
// Eviction: bounded by maxEntries using FIFO eviction (oldest entry removed
// when the cache is full).
//
// All methods are safe for concurrent use by multiple goroutines.
//
// Reference: Tech Spec Section 3.4 — Stage 2.
type SemanticCache struct {
	mu         sync.Mutex
	entries    []*semanticEntry // FIFO: index 0 is oldest
	maxEntries int
	watermarks *WatermarkStore
	stats      *SemanticStats
}

// NewSemanticCache creates a SemanticCache with the given maximum entry count.
// stats may be nil — counters are silently skipped. This is safe for tests
// that do not set up a Prometheus registry.
func NewSemanticCache(maxEntries int, stats *SemanticStats) *SemanticCache {
	if maxEntries <= 0 {
		maxEntries = DefaultSemanticMaxEntries
	}
	return &SemanticCache{
		entries:    make([]*semanticEntry, 0, maxEntries),
		maxEntries: maxEntries,
		watermarks: NewWatermarkStore(),
		stats:      stats,
	}
}

// SemanticScopeKey derives the scope hash for semantic cache entries.
//
// Key = SHA256(sourceName | "\x00" | dest | "\x00" | profile | "\x00" | namespace)
//
// Including the source name enforces scope isolation: source A and source B
// derive different scope keys even for identical query parameters.
func SemanticScopeKey(sourceName, dest, profile, namespace string) [32]byte {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s", sourceName, dest, profile, namespace)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Get performs a semantic cache lookup.
//
// It scans entries in reverse insertion order (newest first) for entries with
// a matching scope that are fresh (watermark check) and within cosine-similarity
// threshold of queryVec. The first matching entry is returned.
//
// Returns the cached entry and true on a hit. Returns a zero-value and false
// on a miss (no matching entry, stale entry, or no entry within threshold).
//
// Hit increments the hit counter; miss (including after scanning all entries
// without a match) increments the miss counter.
//
// Reference: Tech Spec Section 3.4 — Stage 2.
func (sc *SemanticCache) Get(scope [32]byte, queryVec []float32, dest string, threshold float64) (SemanticCacheEntry, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	currentWatermark := sc.watermarks.Current(dest)

	// Scan newest-first for fastest hit on repeated queries.
	for i := len(sc.entries) - 1; i >= 0; i-- {
		e := sc.entries[i]
		if e.scope != scope {
			continue
		}
		// Watermark freshness: stale if a write was delivered after insertion.
		if e.watermark < currentWatermark {
			continue
		}
		// Dimension mismatch: skip (different embedding model or dimensions).
		if len(e.queryVec) != len(queryVec) {
			continue
		}
		sim := cosineSimilarity(queryVec, e.queryVec)
		if sim >= threshold {
			sc.stats.Hit()
			return SemanticCacheEntry{
				Records:    e.records,
				NextCursor: e.nextCursor,
				HasMore:    e.hasMore,
			}, true
		}
	}

	sc.stats.Miss()
	return SemanticCacheEntry{}, false
}

// Put stores a semantic cache entry. The watermark is snapshotted at Put time;
// a subsequent write to the destination will make this entry stale.
//
// When the cache is at capacity, the oldest entry is evicted (FIFO).
func (sc *SemanticCache) Put(scope [32]byte, queryVec []float32, dest string, entry SemanticCacheEntry) {
	if len(queryVec) == 0 {
		return // do not cache empty vectors
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Evict oldest entry when at capacity.
	if len(sc.entries) >= sc.maxEntries {
		sc.entries = sc.entries[1:]
	}

	// Copy queryVec to prevent the caller from mutating the cached slice.
	vec := make([]float32, len(queryVec))
	copy(vec, queryVec)

	sc.entries = append(sc.entries, &semanticEntry{
		scope:      scope,
		dest:       dest,
		queryVec:   vec,
		watermark:  sc.watermarks.Current(dest),
		records:    entry.Records,
		nextCursor: entry.NextCursor,
		hasMore:    entry.HasMore,
	})
}

// InvalidateDest advances the watermark for dest, instantly staling all
// existing semantic cache entries for that destination. Called by the write
// path after a payload is delivered.
//
// Reference: Tech Spec Section 3.4 (watermark freshness check).
func (sc *SemanticCache) InvalidateDest(dest string) {
	sc.watermarks.Advance(dest)
}

// Len returns the number of entries currently in the cache.
func (sc *SemanticCache) Len() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return len(sc.entries)
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0 if either vector has zero magnitude. Both vectors must have the
// same length (callers must check before calling).
//
// The result is clamped to [0, 1] to account for floating-point imprecision.
func cosineSimilarity(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		na += ai * ai
		nb += bi * bi
	}
	if na == 0 || nb == 0 {
		return 0
	}
	sim := dot / (math.Sqrt(na) * math.Sqrt(nb))
	if sim < 0 {
		return 0
	}
	if sim > 1 {
		return 1
	}
	return sim
}
