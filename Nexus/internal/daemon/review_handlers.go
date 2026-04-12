// Copyright © 2026 Shawn Sammartano. All rights reserved.
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

	"github.com/go-chi/chi/v5"
)

// handleReviewList returns a list of quarantined memory IDs.
//
// Requires bfn_review_list_ token. Returns 200 with a JSON array of IDs.
// Quarantine state is stored in WAL entries of type EntryTypeQuarantine and
// tracked in the embedding validator (Phase 2.5). This handler reads the
// current quarantine state from the daemon's embedding validator.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
func (d *Daemon) handleReviewList(w http.ResponseWriter, r *http.Request) {
	ids := d.getQuarantinedIDs()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	type response struct {
		QuarantinedIDs []string `json:"quarantined_ids"`
		Count          int      `json:"count"`
	}
	resp := response{
		QuarantinedIDs: ids,
		Count:          len(ids),
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// handleReviewRead returns the quarantine record for a specific memory ID.
//
// Requires bfn_review_read_ (or bfn_review_list_) token.
// The memory ID is extracted from the chi URL param {id}.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
func (d *Daemon) handleReviewRead(w http.ResponseWriter, r *http.Request) {
	memoryID := chi.URLParam(r, "id")
	if memoryID == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "bad_request", "missing memory id", 0)
		return
	}

	record, ok := d.getQuarantineRecord(memoryID)
	if !ok {
		d.writeErrorResponse(w, r, http.StatusNotFound, "not_found", "memory id not in quarantine", 0)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(record)
}

// getQuarantinedIDs returns all quarantined memory IDs from the embedding
// validator. Returns an empty slice if no validator is configured.
func (d *Daemon) getQuarantinedIDs() []string {
	if d.embeddingValidator == nil {
		return []string{}
	}
	return d.embeddingValidator.QuarantinedIDs()
}

// getQuarantineRecord returns the quarantine record for a specific memory ID.
func (d *Daemon) getQuarantineRecord(memoryID string) (interface{}, bool) {
	if d.embeddingValidator == nil {
		return nil, false
	}
	return d.embeddingValidator.QuarantineRecord(memoryID)
}
