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
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

// Stats holds the Prometheus counters for exact-cache hit and miss events.
// Register counters on the daemon's private registry by passing it to NewStats.
//
// Metric names:
//   - bubblefish_cache_exact_hits_total
//   - bubblefish_cache_exact_misses_total
//
// Reference: Tech Spec Section 11.3.
type Stats struct {
	hits   prometheus.Counter
	misses prometheus.Counter

	// hitCount and missCount are atomic counters mirroring the Prometheus
	// counters for direct read access by admin endpoints without proto parsing.
	hitCount  atomic.Int64
	missCount atomic.Int64
}

// NewStats creates and registers the two exact-cache counters on reg. Panics
// only if reg already contains counters with the same names, which is a
// programming error that cannot occur with the daemon's private registry.
func NewStats(reg prometheus.Registerer) *Stats {
	hits := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bubblefish_cache_exact_hits_total",
		Help: "Total number of Stage 1 exact-cache hits.",
	})
	misses := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "bubblefish_cache_exact_misses_total",
		Help: "Total number of Stage 1 exact-cache misses.",
	})
	reg.MustRegister(hits, misses)
	return &Stats{hits: hits, misses: misses}
}

// Hit increments the cache hit counter.
func (s *Stats) Hit() {
	s.hits.Inc()
	s.hitCount.Add(1)
}

// Miss increments the cache miss counter.
func (s *Stats) Miss() {
	s.misses.Inc()
	s.missCount.Add(1)
}

// HitCount returns the total number of cache hits since startup.
func (s *Stats) HitCount() int64 { return s.hitCount.Load() }

// MissCount returns the total number of cache misses since startup.
func (s *Stats) MissCount() int64 { return s.missCount.Load() }
