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
	"net/http"

	registry "github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/destination"
)

type aggregatedStatsDTO struct {
	MemoryCount     int64   `json:"memory_count"`
	SessionWrites   int64   `json:"session_writes"`
	AuditCount      int64   `json:"audit_count"`
	QuarantineTotal int64   `json:"quarantine_total"`
	AgentsConnected int     `json:"agents_connected"`
	AgentsKnown     int     `json:"agents_known"`
	WALLagMs        float64 `json:"wal_lag_ms"`
	WALFsyncOK      bool    `json:"wal_fsync_ok"`
	CacheHitRate    float64 `json:"cache_hit_rate"`
	Health          struct {
		State       string `json:"state"`
		ChainIntact bool   `json:"chain_intact"`
	} `json:"health"`
	FreeEnergyNats float64 `json:"free_energy_nats"`
}

// handleStats serves GET /api/stats — aggregated dashboard statistics.
func (d *Daemon) handleStats(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/stats").Inc()

	stats := aggregatedStatsDTO{
		SessionWrites:   d.pipeMetrics.writesTotal.Load(),
		AuditCount:      d.pipeMetrics.writesTotal.Load(),
		QuarantineTotal: d.pipeMetrics.quarantineTotal.Load(),
		WALFsyncOK:      true,
	}

	if mc, ok := d.dest.(destination.MemoryCounter); ok {
		if count, err := mc.MemoryCount(); err == nil {
			stats.MemoryCount = count
		}
	}

	if d.registryStore != nil {
		agents, err := d.registryStore.List(r.Context(), registry.ListFilter{})
		if err == nil {
			stats.AgentsKnown = len(agents)
			for _, a := range agents {
				if a.Status == "active" || a.Status == "online" {
					stats.AgentsConnected++
				}
			}
		}
	}

	stats.Health.State = "nominal"
	stats.Health.ChainIntact = d.chainState != nil
	if d.auditLogger == nil {
		stats.Health.State = "degraded"
	}

	d.writeJSON(w, http.StatusOK, stats)
}
