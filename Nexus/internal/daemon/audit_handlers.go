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
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/bubblefish-tech/nexus/internal/audit"
)

// ---------------------------------------------------------------------------
// Audit Query API — GET /api/audit/log
// ---------------------------------------------------------------------------

// auditLogResponse matches the Tech Spec Addendum Section A2.5 response format.
type auditLogResponse struct {
	Records       []audit.InteractionRecord `json:"records"`
	TotalMatching int                       `json:"total_matching"`
	Limit         int                       `json:"limit"`
	Offset        int                       `json:"offset"`
	HasMore       bool                      `json:"has_more"`
}

// handleAuditLog serves GET /api/audit/log — queries the interaction log
// with filtering and pagination. Admin token required.
//
// Query parameters: source, actor_type, actor_id, operation, policy_decision,
// subject, destination, after, before, limit (1–1000, default 100), offset.
//
// Rate limited at admin_rate_limit_per_minute (default 60).
//
// Reference: Tech Spec Addendum Section A2.5, A6.
func (d *Daemon) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/audit/log").Inc()

	if d.auditReader == nil {
		d.writeJSON(w, http.StatusOK, auditLogResponse{
			Records: []audit.InteractionRecord{},
		})
		return
	}

	// Rate limit: admin_rate_limit_per_minute.
	cfg := d.getConfig()
	rpm := cfg.Daemon.Audit.AdminRateLimitPerMin
	if rpm <= 0 {
		rpm = 60
	}
	if allowed, retryAfter := d.auditRateLimiter.Allow("_audit_admin", rpm); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		d.writeErrorResponse(w, r, http.StatusTooManyRequests, "rate_limit_exceeded",
			"audit query rate limit exceeded; back off and retry", retryAfter)
		return
	}

	queryStart := time.Now()

	filter := audit.AuditFilter{
		Source:         r.URL.Query().Get("source"),
		ActorType:      r.URL.Query().Get("actor_type"),
		ActorID:        r.URL.Query().Get("actor_id"),
		Operation:      r.URL.Query().Get("operation"),
		PolicyDecision: r.URL.Query().Get("policy_decision"),
		Subject:        r.URL.Query().Get("subject"),
		Destination:    r.URL.Query().Get("destination"),
	}

	// Parse time range filters.
	if afterStr := r.URL.Query().Get("after"); afterStr != "" {
		t, err := time.Parse(time.RFC3339, afterStr)
		if err != nil {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_after",
				"after must be RFC3339 format", 0)
			return
		}
		filter.After = t
	}
	if beforeStr := r.URL.Query().Get("before"); beforeStr != "" {
		t, err := time.Parse(time.RFC3339, beforeStr)
		if err != nil {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_before",
				"before must be RFC3339 format", 0)
			return
		}
		filter.Before = t
	}

	// Parse limit (1–1000, default 100).
	filter.Limit = 100
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil {
			filter.Limit = n
		}
	}
	if filter.Limit < 1 {
		filter.Limit = 1
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	// Parse offset.
	if os := r.URL.Query().Get("offset"); os != "" {
		if n, err := strconv.Atoi(os); err == nil && n >= 0 {
			filter.Offset = n
		}
	}

	result, err := d.auditReader.Query(filter)
	if err != nil {
		d.logger.Error("daemon: audit query failed",
			"component", "daemon",
			"error", err,
			"request_id", middleware.GetReqID(r.Context()),
		)
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"audit query failed", 0)
		return
	}

	d.metrics.AuditQueryLatency.Observe(time.Since(queryStart).Seconds())

	d.writeJSON(w, http.StatusOK, auditLogResponse{
		Records:       result.Records,
		TotalMatching: result.TotalMatching,
		Limit:         result.Limit,
		Offset:        result.Offset,
		HasMore:       result.HasMore,
	})
}

// ---------------------------------------------------------------------------
// Audit Status — GET /api/audit/status
// ---------------------------------------------------------------------------

// auditStatusResponse is returned by GET /api/audit/status.
type auditStatusResponse struct {
	TotalRecords int  `json:"total_records"`
	AuditEnabled bool `json:"audit_enabled"`
}

// handleAuditStatus serves GET /api/audit/status — returns the total number
// of audit records, used by the WebUI to show audit chain length.
func (d *Daemon) handleAuditStatus(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/audit/status").Inc()

	if d.auditReader == nil {
		d.writeJSON(w, http.StatusOK, auditStatusResponse{AuditEnabled: false})
		return
	}

	result, err := d.auditReader.Query(audit.AuditFilter{Limit: 1})
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"audit status query failed", 0)
		return
	}
	d.writeJSON(w, http.StatusOK, auditStatusResponse{
		TotalRecords: result.TotalMatching,
		AuditEnabled: true,
	})
}

// ---------------------------------------------------------------------------
// Audit Stats — GET /api/audit/stats
// ---------------------------------------------------------------------------

// auditStatsResponse holds summary statistics for the interaction log.
type auditStatsResponse struct {
	TotalRecords      int                `json:"total_records"`
	InteractionsPerHr map[string]int     `json:"interactions_per_hour"` // write, query, admin
	DenialRate        float64            `json:"denial_rate"`
	TopSources        map[string]int     `json:"top_sources"`
	TopActors         map[string]int     `json:"top_actors"`
	ByOperation       map[string]int     `json:"by_operation"`
	ByDecision        map[string]int     `json:"by_decision"`
}

// handleAuditStats serves GET /api/audit/stats — returns summary statistics
// for the interaction log.
//
// Reference: Tech Spec Addendum Section A6.
func (d *Daemon) handleAuditStats(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/audit/stats").Inc()

	if d.auditReader == nil {
		d.writeJSON(w, http.StatusOK, auditStatsResponse{
			InteractionsPerHr: map[string]int{},
			TopSources:        map[string]int{},
			TopActors:         map[string]int{},
			ByOperation:       map[string]int{},
			ByDecision:        map[string]int{},
		})
		return
	}

	// Rate limit: admin_rate_limit_per_minute.
	cfg := d.getConfig()
	rpm := cfg.Daemon.Audit.AdminRateLimitPerMin
	if rpm <= 0 {
		rpm = 60
	}
	if allowed, retryAfter := d.auditRateLimiter.Allow("_audit_admin", rpm); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		d.writeErrorResponse(w, r, http.StatusTooManyRequests, "rate_limit_exceeded",
			"audit query rate limit exceeded; back off and retry", retryAfter)
		return
	}

	// Read all records (no filter, high limit) — acceptable for v0.1.0 per spec.
	result, err := d.auditReader.Query(audit.AuditFilter{Limit: 1000})
	if err != nil {
		d.logger.Error("daemon: audit stats query failed",
			"component", "daemon",
			"error", err,
		)
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"audit stats query failed", 0)
		return
	}

	stats := auditStatsResponse{
		TotalRecords:      result.TotalMatching,
		InteractionsPerHr: make(map[string]int),
		TopSources:        make(map[string]int),
		TopActors:         make(map[string]int),
		ByOperation:       make(map[string]int),
		ByDecision:        make(map[string]int),
	}

	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour)
	var denied int

	for _, rec := range result.Records {
		stats.ByOperation[rec.OperationType]++
		stats.ByDecision[rec.PolicyDecision]++
		stats.TopSources[rec.Source]++
		if rec.ActorID != "" {
			stats.TopActors[rec.ActorID]++
		}
		if rec.PolicyDecision == "denied" {
			denied++
		}
		if rec.Timestamp.After(oneHourAgo) {
			stats.InteractionsPerHr[rec.OperationType]++
		}
	}

	if stats.TotalRecords > 0 {
		stats.DenialRate = float64(denied) / float64(stats.TotalRecords)
	}

	d.writeJSON(w, http.StatusOK, stats)
}

// ---------------------------------------------------------------------------
// Audit Export — GET /api/audit/export
// ---------------------------------------------------------------------------

// handleAuditExport serves GET /api/audit/export — exports interaction records
// as JSON array or CSV based on Accept header.
//
// Query parameters: same as /api/audit/log (source, actor_type, etc.)
// Accept: application/json (default) or text/csv.
//
// Reference: Tech Spec Addendum Section A6.
func (d *Daemon) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/audit/export").Inc()

	if d.auditReader == nil {
		d.writeJSON(w, http.StatusOK, []audit.InteractionRecord{})
		return
	}

	// Rate limit: admin_rate_limit_per_minute.
	cfg := d.getConfig()
	rpm := cfg.Daemon.Audit.AdminRateLimitPerMin
	if rpm <= 0 {
		rpm = 60
	}
	if allowed, retryAfter := d.auditRateLimiter.Allow("_audit_admin", rpm); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		d.writeErrorResponse(w, r, http.StatusTooManyRequests, "rate_limit_exceeded",
			"audit query rate limit exceeded; back off and retry", retryAfter)
		return
	}

	filter := audit.AuditFilter{
		Source:         r.URL.Query().Get("source"),
		ActorType:      r.URL.Query().Get("actor_type"),
		ActorID:        r.URL.Query().Get("actor_id"),
		Operation:      r.URL.Query().Get("operation"),
		PolicyDecision: r.URL.Query().Get("policy_decision"),
		Subject:        r.URL.Query().Get("subject"),
		Destination:    r.URL.Query().Get("destination"),
		Limit:          1000, // Hard cap per spec
	}

	if afterStr := r.URL.Query().Get("after"); afterStr != "" {
		if t, err := time.Parse(time.RFC3339, afterStr); err == nil {
			filter.After = t
		}
	}
	if beforeStr := r.URL.Query().Get("before"); beforeStr != "" {
		if t, err := time.Parse(time.RFC3339, beforeStr); err == nil {
			filter.Before = t
		}
	}

	result, err := d.auditReader.Query(filter)
	if err != nil {
		d.logger.Error("daemon: audit export failed",
			"component", "daemon",
			"error", err,
		)
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"audit export failed", 0)
		return
	}

	// CSV export when Accept header requests it.
	if strings.Contains(r.Header.Get("Accept"), "text/csv") {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=audit_export.csv")
		w.WriteHeader(http.StatusOK)

		cw := csv.NewWriter(w)
		// Header row.
		_ = cw.Write([]string{
			"record_id", "request_id", "timestamp", "source", "actor_type",
			"actor_id", "effective_ip", "operation_type", "endpoint",
			"http_method", "http_status_code", "payload_id", "destination",
			"subject", "policy_decision", "policy_reason", "latency_ms",
		})
		for _, rec := range result.Records {
			_ = cw.Write([]string{
				rec.RecordID,
				rec.RequestID,
				rec.Timestamp.Format(time.RFC3339Nano),
				rec.Source,
				rec.ActorType,
				rec.ActorID,
				rec.EffectiveIP,
				rec.OperationType,
				rec.Endpoint,
				rec.HTTPMethod,
				strconv.Itoa(rec.HTTPStatusCode),
				rec.PayloadID,
				rec.Destination,
				rec.Subject,
				rec.PolicyDecision,
				rec.PolicyReason,
				fmt.Sprintf("%.3f", rec.LatencyMs),
			})
		}
		cw.Flush()
		return
	}

	// Default: JSON array.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	if err := enc.Encode(result.Records); err != nil {
		d.logger.Error("daemon: audit export JSON encode failed",
			"component", "daemon",
			"error", err,
		)
	}
}

// ---------------------------------------------------------------------------
// Audit record emission helper
// ---------------------------------------------------------------------------

// emitAuditRecord appends an interaction record to both the WAL (for
// kill-9 durability) and the JSONL audit log (for SIEM compatibility).
// Failure MUST NOT cause request failure — logs WARN and increments metric.
//
// Reference: Tech Spec Addendum Section A2.4.
func (d *Daemon) emitAuditRecord(rec audit.InteractionRecord) {
	if d.auditLogger == nil {
		return
	}
	d.metrics.AuditRecordsTotal.WithLabelValues(rec.OperationType, rec.PolicyDecision).Inc()

	// Write to WAL first (durability source of truth).
	if d.auditWAL != nil {
		if err := d.auditWAL.Submit(rec); err != nil {
			d.logger.Warn("daemon: audit WAL write failed",
				"component", "daemon",
				"error", err,
				"record_id", rec.RecordID,
			)
		}
	}

	// Write to JSONL (tail-follower for SIEM integrations).
	if err := d.auditLogger.Log(rec); err != nil {
		d.logger.Warn("daemon: audit log write failed",
			"component", "daemon",
			"error", err,
			"record_id", rec.RecordID,
		)
		d.metrics.AuditLogErrorsTotal.WithLabelValues("primary").Inc()
	}
}
