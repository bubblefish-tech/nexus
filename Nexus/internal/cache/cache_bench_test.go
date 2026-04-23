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
	"strconv"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// BenchmarkCache_Hit measures the cost of a cache hit on the ExactCache.
func BenchmarkCache_Hit(b *testing.B) {
	b.ReportAllocs()
	c := NewExactCache(1<<20, nil) // 1 MB, no Prometheus stats
	k := BuildKey("bench", "sqlite", "balanced", "ns", "", "test-query", 20, 0, "ph")
	entry := CacheEntry{
		Records: []destination.TranslatedPayload{
			{PayloadID: "hit-1", Content: "cached content"},
		},
	}
	c.Put(k, "sqlite", entry)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := c.Get(k, "sqlite")
		if !ok {
			b.Fatal("expected cache hit")
		}
	}
}

// BenchmarkCache_Miss measures the cost of a cache miss on the ExactCache.
func BenchmarkCache_Miss(b *testing.B) {
	b.ReportAllocs()
	c := NewExactCache(1<<20, nil)
	k := BuildKey("bench", "sqlite", "balanced", "ns", "", "nonexistent", 20, 0, "ph")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := c.Get(k, "sqlite")
		if ok {
			b.Fatal("expected cache miss")
		}
	}
}

// BenchmarkCache_Set measures the cost of inserting a new key per iteration.
func BenchmarkCache_Set(b *testing.B) {
	b.ReportAllocs()
	c := NewExactCache(1<<30, nil) // 1 GB — avoid eviction during bench
	entry := CacheEntry{
		Records: []destination.TranslatedPayload{
			{PayloadID: "set-1", Content: "new content"},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := BuildKey("bench", "sqlite", "balanced", "ns", "", strconv.Itoa(i), 20, 0, "ph")
		c.Put(k, "sqlite", entry)
	}
}
