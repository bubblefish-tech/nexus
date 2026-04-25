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
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/bubblefish-tech/nexus/internal/quarantine"
)

// quarantineRecordDTO is the JSON representation of a quarantine record.
type quarantineRecordDTO struct {
	ID                string  `json:"id"`
	OriginalPayloadID string  `json:"original_payload_id"`
	Content           string  `json:"content"`
	MetadataJSON      string  `json:"metadata_json"`
	SourceName        string  `json:"source_name"`
	AgentID           string  `json:"agent_id"`
	QuarantineReason  string  `json:"quarantine_reason"`
	RuleID            string  `json:"rule_id"`
	QuarantinedAtMs   int64   `json:"quarantined_at_ms"`
	ReviewedAtMs      *int64  `json:"reviewed_at_ms,omitempty"`
	ReviewAction      *string `json:"review_action,omitempty"`
	ReviewedBy        *string `json:"reviewed_by,omitempty"`
}

func quarantineRecordToDTO(r quarantine.Record) quarantineRecordDTO {
	return quarantineRecordDTO{
		ID:                r.ID,
		OriginalPayloadID: r.OriginalPayloadID,
		Content:           r.Content,
		MetadataJSON:      r.MetadataJSON,
		SourceName:        r.SourceName,
		AgentID:           r.AgentID,
		QuarantineReason:  r.QuarantineReason,
		RuleID:            r.RuleID,
		QuarantinedAtMs:   r.QuarantinedAtMs,
		ReviewedAtMs:      r.ReviewedAtMs,
		ReviewAction:      r.ReviewAction,
		ReviewedBy:        r.ReviewedBy,
	}
}

// handleQuarantineList returns quarantined records.
// GET /api/quarantine?source=<name>&include_reviewed=<bool>&limit=<n>
func (d *Daemon) handleQuarantineList(w http.ResponseWriter, r *http.Request) {
	filter := quarantine.ListFilter{
		SourceName: r.URL.Query().Get("source"),
	}
	if v := r.URL.Query().Get("include_reviewed"); v == "true" || v == "1" {
		filter.IncludeReviewed = true
	}
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 {
			filter.Limit = n
		}
	}

	recs, err := d.quarantineStore.List(filter)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"failed to list quarantine records", 0)
		return
	}

	dtos := make([]quarantineRecordDTO, len(recs))
	for i, rec := range recs {
		dtos[i] = quarantineRecordToDTO(rec)
	}

	total, pending := 0, 0
	if t, p, cErr := d.quarantineStore.Count(); cErr == nil {
		total, pending = t, p
	}

	type response struct {
		Records  []quarantineRecordDTO `json:"records"`
		Count    int                   `json:"count"`
		Total    int                   `json:"total"`
		Pending  int                   `json:"pending"`
	}
	d.writeJSON(w, http.StatusOK, response{Records: dtos, Count: len(dtos), Total: total, Pending: pending})
}

// handleQuarantineGet returns a single quarantine record by ID.
// GET /api/quarantine/{id}
func (d *Daemon) handleQuarantineGet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "bad_request", "missing quarantine id", 0)
		return
	}
	rec, err := d.quarantineStore.Get(id)
	if err == quarantine.ErrNotFound {
		d.writeErrorResponse(w, r, http.StatusNotFound, "not_found", "quarantine record not found", 0)
		return
	}
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"failed to fetch quarantine record", 0)
		return
	}
	d.writeJSON(w, http.StatusOK, quarantineRecordToDTO(rec))
}

// handleQuarantineApprove approves a quarantined record.
// POST /api/quarantine/{id}/approve
func (d *Daemon) handleQuarantineApprove(w http.ResponseWriter, r *http.Request) {
	d.decideQuarantineRecord(w, r, quarantine.ReviewActionApproved)
}

// handleQuarantineReject rejects a quarantined record.
// POST /api/quarantine/{id}/reject
func (d *Daemon) handleQuarantineReject(w http.ResponseWriter, r *http.Request) {
	d.decideQuarantineRecord(w, r, quarantine.ReviewActionRejected)
}

func (d *Daemon) decideQuarantineRecord(w http.ResponseWriter, r *http.Request, action string) {
	id := chi.URLParam(r, "id")
	if id == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "bad_request", "missing quarantine id", 0)
		return
	}

	var body struct {
		ReviewedBy string `json:"reviewed_by"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&body); err != nil {
		body.ReviewedBy = "admin"
	}
	if body.ReviewedBy == "" {
		body.ReviewedBy = "admin"
	}

	if err := d.quarantineStore.Decide(id, action, body.ReviewedBy); err == quarantine.ErrNotFound {
		d.writeErrorResponse(w, r, http.StatusNotFound, "not_found", "quarantine record not found", 0)
		return
	} else if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"failed to record decision", 0)
		return
	}

	d.emitControlEvent(
		"memory.quarantine_decided",
		body.ReviewedBy, id, "quarantine",
		"", "", action, "", nil,
	)

	type response struct {
		ID           string `json:"id"`
		ReviewAction string `json:"review_action"`
		ReviewedBy   string `json:"reviewed_by"`
		ReviewedAtMs int64  `json:"reviewed_at_ms"`
	}
	d.writeJSON(w, http.StatusOK, response{
		ID:           id,
		ReviewAction: action,
		ReviewedBy:   body.ReviewedBy,
		ReviewedAtMs: time.Now().UnixMilli(),
	})
}
