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

	"github.com/bubblefish-tech/nexus/internal/destination"
)

const (
	// DefaultMaxBytes is the default maximum byte capacity for the exact cache
	// (50 MiB). Configurable via daemon.toml in later phases.
	DefaultMaxBytes = 50 * 1024 * 1024
)

// CacheEntry is the value stored in the exact cache for a single query result.
// Watermark records the destination's write watermark at insertion time; an
// entry is stale when the destination's current watermark exceeds this value.
type CacheEntry struct {
	Records    []destination.TranslatedPayload
	NextCursor string
	HasMore    bool
	// Watermark is the destination's monotonic write counter at Put time.
	// A value of 0 is valid for destinations that have never been written to.
	Watermark uint64
}

// ExactCache is the Stage 1 exact cache. It provides:
//
//   - Scope isolation: cache keys encode the source identity so source A cannot
//     retrieve source B's cached results even for identical queries.
//   - Watermark invalidation: entries become stale when the destination watermark
//     advances (i.e., a new write was delivered).
//   - LRU eviction: bounded by maxBytes; oldest entries evicted when full.
//   - Prometheus counters: hit and miss events are reported via Stats.
//
// All methods are safe for concurrent use by multiple goroutines.
//
// Reference: Tech Spec Section 3.4 — Stage 1.
type ExactCache struct {
	lru        *LRU[[32]byte, CacheEntry]
	watermarks *WatermarkStore
	stats      *Stats
}

// NewExactCache creates an ExactCache with the given byte capacity and
// Prometheus registry. Pass DefaultMaxBytes for the 50 MiB default.
// stats may be nil (counters are skipped) — useful in tests that don't set up
// a Prometheus registry.
func NewExactCache(maxBytes int64, stats *Stats) *ExactCache {
	return &ExactCache{
		lru:        NewLRU[[32]byte, CacheEntry](maxBytes),
		watermarks: NewWatermarkStore(),
		stats:      stats,
	}
}

// BuildKey derives the 32-byte cache key for a query.
//
// Key = SHA256(sourceScope | dest | profile | namespace | subject | q | limit | offset | policyHash)
//
// sourceScope is the source's unique identity (typically source.Name). Including
// it in the key enforces scope isolation: source A and source B derive different
// keys for identical query parameters.
//
// policyHash is a caller-provided digest of the policy fields that affect result
// shape (max_results, field_visibility, etc.). A policy change produces a
// different key, preventing stale policy-shaped results from being served.
//
// Reference: Tech Spec Section 3.4 — Stage 1 (cache key derivation).
func BuildKey(sourceScope, dest, profile, namespace, subject, q string, limit, offset int, policyHash string) [32]byte {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%s",
		sourceScope, dest, profile, namespace, subject, q, limit, offset, policyHash)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Get retrieves a cached result for key. It returns the entry and true on a
// hit, or a zero CacheEntry and false on:
//   - key not present (miss)
//   - entry watermark < current dest watermark (stale miss)
//
// On a miss the miss counter is incremented. On a hit the hit counter is
// incremented and the entry is promoted to most-recently-used.
//
// Reference: Tech Spec Section 3.4 — Stage 1 (watermark freshness check,
// semantic short-circuit).
func (c *ExactCache) Get(key [32]byte, dest string) (CacheEntry, bool) {
	entry, ok := c.lru.Get(key)
	if !ok {
		if c.stats != nil {
			c.stats.Miss()
		}
		return CacheEntry{}, false
	}

	// Watermark check: entry is stale when a write was delivered after it was
	// cached. Stale entries are not removed from the LRU here — they are
	// naturally displaced by newer entries or by LRU eviction.
	if entry.Watermark < c.watermarks.Current(dest) {
		if c.stats != nil {
			c.stats.Miss()
		}
		return CacheEntry{}, false
	}

	if c.stats != nil {
		c.stats.Hit()
	}
	return entry, true
}

// Put stores a cache entry for key. The entry's Watermark is set to the
// current watermark for dest so that future writes invalidate it automatically.
// Size is estimated from the entry's record fields to enforce the byte cap.
func (c *ExactCache) Put(key [32]byte, dest string, entry CacheEntry) {
	entry.Watermark = c.watermarks.Current(dest)
	size := estimateSize(entry)
	c.lru.Add(key, entry, size)
}

// InvalidateDest advances the watermark for dest, instantly staling all
// existing cache entries for that destination. Called by the write path after
// a payload is successfully delivered.
//
// Reference: Tech Spec Section 3.4 (watermark freshness check), Section 3.7.
func (c *ExactCache) InvalidateDest(dest string) {
	c.watermarks.Advance(dest)
}

// Len returns the number of entries currently in the LRU.
func (c *ExactCache) Len() int { return c.lru.Len() }

// BytesUsed returns the total estimated byte footprint of cached entries.
func (c *ExactCache) BytesUsed() int64 { return c.lru.BytesUsed() }

// Evictions returns the total number of LRU evictions since creation.
func (c *ExactCache) Evictions() int64 { return c.lru.Evictions() }

// estimateSize approximates the heap footprint of a CacheEntry in bytes.
// It sums the string lengths of all record fields plus per-record overhead.
// This avoids the cost of JSON serialisation on every Put.
func estimateSize(e CacheEntry) int64 {
	const perEntryOverhead = 256  // struct metadata, list/map pointers
	const perRecordOverhead = 128 // TranslatedPayload struct fields
	size := int64(perEntryOverhead + len(e.NextCursor))
	for i := range e.Records {
		r := &e.Records[i]
		size += int64(perRecordOverhead +
			len(r.PayloadID) + len(r.RequestID) + len(r.Source) +
			len(r.Subject) + len(r.Namespace) + len(r.Destination) +
			len(r.Collection) + len(r.Content) + len(r.Model) +
			len(r.Role) + len(r.ActorType) + len(r.ActorID) +
			len(r.IdempotencyKey) + len(r.TransformVersion))
		for k, v := range r.Metadata {
			size += int64(len(k) + len(v))
		}
		size += int64(len(r.Embedding) * 4) // float32 = 4 bytes each
	}
	if size < 1 {
		size = 1
	}
	return size
}
