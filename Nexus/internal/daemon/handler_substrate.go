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
	"encoding/json"
	"net/http"
)

// SubstrateStatusResponse is the JSON payload for GET /api/substrate/status.
type SubstrateStatusResponse struct {
	Enabled bool `json:"enabled"`

	// Only populated when enabled:
	RatchetStateID uint32  `json:"ratchet_state_id,omitempty"`
	SketchCount    int     `json:"sketch_count,omitempty"`
	CuckooCount    uint    `json:"cuckoo_count,omitempty"`
	CuckooCapacity uint    `json:"cuckoo_capacity,omitempty"`
	CuckooLoadFactor float64 `json:"cuckoo_load_factor,omitempty"`
}

// handleSubstrateStatus returns the current substrate status.
// Endpoint: GET /api/substrate/status
func (d *Daemon) handleSubstrateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	resp := SubstrateStatusResponse{
		Enabled: d.substrate != nil && d.substrate.Enabled(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleSubstrateRotateRatchet manually advances the ratchet.
// Endpoint: POST /api/substrate/rotate-ratchet
func (d *Daemon) handleSubstrateRotateRatchet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if d.substrate == nil || !d.substrate.Enabled() {
		http.Error(w, `{"error":"substrate_disabled","message":"substrate is not enabled"}`, http.StatusBadRequest)
		return
	}

	// Ratchet rotation will be wired when the Substrate coordinator exposes
	// the ratchet manager via a public method (future commit).
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "not_implemented",
		"message": "ratchet rotation via API will be available when substrate is fully wired",
	})
}

// handleSubstrateProveDeletion produces a deletion proof for a memory.
// Endpoint: POST /api/substrate/prove-deletion?memory_id=<id>
func (d *Daemon) handleSubstrateProveDeletion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if d.substrate == nil || !d.substrate.Enabled() {
		http.Error(w, `{"error":"substrate_disabled","message":"substrate is not enabled"}`, http.StatusBadRequest)
		return
	}

	memoryID := r.URL.Query().Get("memory_id")
	if memoryID == "" {
		http.Error(w, `{"error":"missing_parameter","message":"memory_id is required"}`, http.StatusBadRequest)
		return
	}

	// Deletion proof will be wired when the Substrate coordinator exposes
	// the required sub-components via public methods (future commit).
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "not_implemented",
		"memory_id": memoryID,
		"message":   "deletion proof via API will be available when substrate is fully wired",
	})
}
