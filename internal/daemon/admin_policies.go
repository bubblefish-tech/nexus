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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
)

// policySourceEntry is the per-source entry in the /api/policies response.
// Reference: dashboard-contract.md GET /api/policies.
type policySourceEntry struct {
	Source              string   `json:"source"`
	CanRead             bool     `json:"can_read"`
	CanWrite            bool     `json:"can_write"`
	AllowedDestinations []string `json:"allowed_destinations"`
	MaxResults          int      `json:"max_results"`
	MaxResponseBytes    int      `json:"max_response_bytes"`
	RateLimitPerMin     int      `json:"rate_limit_per_min"`
	PolicyHash          string   `json:"policy_hash"`
}

// handleAdminPolicies returns the compiled policy summary per source matching
// the dashboard contract shape exactly.
// Reference: dashboard-contract.md GET /api/policies.
func (d *Daemon) handleAdminPolicies(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/policies").Inc()

	cfg := d.getConfig()

	entries := make([]policySourceEntry, 0, len(cfg.Sources))
	for _, src := range cfg.Sources {
		dests := src.Policy.AllowedDestinations
		if dests == nil {
			dests = []string{}
		}

		// Compute policy hash: SHA-256 of the JSON-marshaled policy, first 8 hex chars.
		policyJSON, _ := json.Marshal(src.Policy)
		hash := sha256.Sum256(policyJSON)
		policyHash := hex.EncodeToString(hash[:])[:8]

		entries = append(entries, policySourceEntry{
			Source:              src.Name,
			CanRead:             src.CanRead,
			CanWrite:            src.CanWrite,
			AllowedDestinations: dests,
			MaxResults:          src.Policy.MaxResults,
			MaxResponseBytes:    src.Policy.MaxResponseBytes,
			RateLimitPerMin:     src.RateLimit.RequestsPerMinute,
			PolicyHash:          policyHash,
		})
	}

	// Sort alphabetically by source name for stable rendering.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Source < entries[j].Source
	})

	d.writeJSON(w, http.StatusOK, map[string]interface{}{
		"sources": entries,
	})
}
