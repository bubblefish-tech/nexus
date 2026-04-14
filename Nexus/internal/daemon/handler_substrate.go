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
	"fmt"
	"net/http"
)

// handleSubstrateStatus returns the current substrate status.
// Endpoint: GET /api/substrate/status
func (d *Daemon) handleSubstrateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if d.substrate == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"enabled": false})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(d.substrate.Status())
}

// handleSubstrateRotateRatchet manually advances the ratchet.
// Endpoint: POST /api/substrate/rotate-ratchet
func (d *Daemon) handleSubstrateRotateRatchet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if d.substrate == nil || !d.substrate.Enabled() {
		http.Error(w, `{"error":"substrate_disabled","message":"substrate is not enabled"}`, http.StatusServiceUnavailable)
		return
	}

	newState, err := d.substrate.RotateRatchet("manual")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"ratchet_error","message":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"new_state_id": newState.StateID,
		"created_at":   newState.CreatedAt,
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
		http.Error(w, `{"error":"substrate_disabled","message":"substrate is not enabled"}`, http.StatusServiceUnavailable)
		return
	}

	memoryID := r.URL.Query().Get("memory_id")
	if memoryID == "" {
		http.Error(w, `{"error":"missing_parameter","message":"memory_id is required"}`, http.StatusBadRequest)
		return
	}

	proof, err := d.substrate.ProveDeletion(memoryID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"prove_deletion_error","message":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(proof)
}

// handleSubstrateShred performs forward-secure deletion: shreds the memory's
// substrate key material and advances the ratchet.
// Endpoint: POST /api/substrate/shred?memory_id=<id>
func (d *Daemon) handleSubstrateShred(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method_not_allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if d.substrate == nil || !d.substrate.Enabled() {
		http.Error(w, `{"error":"substrate_disabled","message":"substrate is not enabled"}`, http.StatusServiceUnavailable)
		return
	}

	memoryID := r.URL.Query().Get("memory_id")
	if memoryID == "" {
		http.Error(w, `{"error":"missing_parameter","message":"memory_id is required"}`, http.StatusBadRequest)
		return
	}

	// Shred the memory's substrate data
	if err := d.substrate.ShredMemory(memoryID); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"shred_error","message":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Advance the ratchet to ensure forward security
	newState, err := d.substrate.RotateRatchet("shred-seed-" + memoryID)
	if err != nil {
		// Shred succeeded but ratchet advance failed — log but don't fail
		d.logger.Error("substrate: ratchet advance after shred failed",
			"component", "daemon", "memory_id", memoryID, "error", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "partial",
			"memory_id": memoryID,
			"message":   "memory shredded but ratchet advance failed",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "ok",
		"memory_id":      memoryID,
		"new_state_id":   newState.StateID,
		"message":        "memory shredded and ratchet advanced",
	})
}
