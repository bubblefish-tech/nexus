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

package daemon

import (
	"fmt"
	"net/http"
)

// handleAdminCache returns cache statistics matching the dashboard contract
// shape exactly.
// Reference: dashboard-contract.md GET /api/cache.
func (d *Daemon) handleAdminCache(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/cache").Inc()

	// Read all counter values. The atomic reads are individually consistent;
	// slight cross-counter skew is acceptable for a dashboard endpoint.
	exactHits := int64(0)
	exactMisses := int64(0)
	semHits := int64(0)
	semMisses := int64(0)

	if d.exactStats != nil {
		exactHits = d.exactStats.HitCount()
		exactMisses = d.exactStats.MissCount()
	}
	if d.semanticStats != nil {
		semHits = d.semanticStats.HitCount()
		semMisses = d.semanticStats.MissCount()
	}

	exactTotal := exactHits + exactMisses
	semTotal := semHits + semMisses

	exactHitRate := 0.0
	if exactTotal > 0 {
		exactHitRate = float64(exactHits) / float64(exactTotal)
	}
	semHitRate := 0.0
	if semTotal > 0 {
		semHitRate = float64(semHits) / float64(semTotal)
	}

	// Misses that fell through both cache layers.
	missesTotal := exactMisses

	evictions := int64(0)
	used := 0
	if d.exactCache != nil {
		evictions = d.exactCache.Evictions()
		used = d.exactCache.Len()
	}

	// Capacity is not currently configurable; use a reasonable default.
	capacity := 10000

	d.writeJSON(w, http.StatusOK, map[string]interface{}{
		"exact": map[string]interface{}{
			"hits":     exactHits,
			"misses":   exactMisses,
			"hit_rate": exactHitRate,
		},
		"semantic": map[string]interface{}{
			"hits":     semHits,
			"misses":   semMisses,
			"hit_rate": semHitRate,
		},
		"misses_total":    missesTotal,
		"evictions_total": evictions,
		"capacity":        capacity,
		"used":            used,
		"watermark":       fmt.Sprintf("%d / %d", used, capacity),
	})
}
