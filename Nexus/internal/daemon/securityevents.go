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
	"strconv"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/BubbleFish-Nexus/internal/securitylog"
)

// emitSecurityEvent writes a structured security event to both the main
// logger and the dedicated security event log (when enabled).
// Reference: Tech Spec Section 11.2.
func (d *Daemon) emitSecurityEvent(e securitylog.Event) {
	if d.securityLog == nil {
		return
	}
	d.securityLog.Emit(e)
}

// handleSecurityEvents serves GET /api/security/events — returns the last N
// structured security events from the in-memory ring buffer.
// Query parameters:
//
//	?limit=N (default 100, max 1000)
//
// Reference: Tech Spec Section 12.
func (d *Daemon) handleSecurityEvents(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/security/events").Inc()

	// Emit admin_access event for this endpoint.
	d.emitSecurityEvent(securitylog.Event{
		EventType: "admin_access",
		IP:        effectiveClientIPFromContext(r.Context()),
		Endpoint:  "/api/security/events",
		Details: map[string]interface{}{
			"user_agent": r.UserAgent(),
		},
	})

	if d.securityLog == nil {
		d.writeJSON(w, http.StatusOK, map[string]interface{}{
			"events":  []securitylog.Event{},
			"message": "security events not enabled",
		})
		return
	}

	limit := 100
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	events := d.securityLog.Recent(limit)
	d.writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
	})
}

// handleSecuritySummary serves GET /api/security/summary — returns aggregated
// counts of security events by type.
// Reference: Tech Spec Section 12.
func (d *Daemon) handleSecuritySummary(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/security/summary").Inc()

	// Emit admin_access event for this endpoint.
	d.emitSecurityEvent(securitylog.Event{
		EventType: "admin_access",
		IP:        effectiveClientIPFromContext(r.Context()),
		Endpoint:  "/api/security/summary",
		Details: map[string]interface{}{
			"user_agent": r.UserAgent(),
		},
	})

	if d.securityLog == nil {
		d.writeJSON(w, http.StatusOK, securitylog.Summary{
			BySource: map[string]int{},
		})
		return
	}

	d.writeJSON(w, http.StatusOK, d.securityLog.Summarize())
}

// emitAuthFailure emits an auth_failure security event.
func (d *Daemon) emitAuthFailure(r *http.Request, tokenClass string) {
	d.emitSecurityEvent(securitylog.Event{
		EventType: "auth_failure",
		Source:    "unknown",
		IP:        effectiveClientIPFromContext(r.Context()),
		Endpoint:  r.URL.Path,
		Details: map[string]interface{}{
			"token_class": tokenClass,
			"request_id":  middleware.GetReqID(r.Context()),
		},
	})
}

// emitPolicyDenied emits a policy_denied security event and increments the
// bubblefish_policy_denials_total metric.
// Reference: Tech Spec Section 11.3.
func (d *Daemon) emitPolicyDenied(r *http.Request, source, subject, operation, dest, reason string) {
	d.metrics.PolicyDenialsTotal.WithLabelValues(source, reason).Inc()
	d.emitSecurityEvent(securitylog.Event{
		EventType: "policy_denied",
		Source:    source,
		Subject:   subject,
		IP:        effectiveClientIPFromContext(r.Context()),
		Endpoint:  r.URL.Path,
		Details: map[string]interface{}{
			"operation":   operation,
			"destination": dest,
			"reason":      reason,
			"request_id":  middleware.GetReqID(r.Context()),
		},
	})
}

// emitRateLimitHit emits a rate_limit_hit security event.
func (d *Daemon) emitRateLimitHit(r *http.Request, source string, rpm int) {
	d.emitSecurityEvent(securitylog.Event{
		EventType: "rate_limit_hit",
		Source:    source,
		IP:        effectiveClientIPFromContext(r.Context()),
		Endpoint:  r.URL.Path,
		Details: map[string]interface{}{
			"requests_per_minute": rpm,
			"request_id":         middleware.GetReqID(r.Context()),
		},
	})
}

// emitRetrievalFirewallFiltered emits a retrieval_firewall_filtered security
// event when the retrieval firewall removes memories from the result set.
// Reference: Tech Spec Addendum Section A3.7.
func (d *Daemon) emitRetrievalFirewallFiltered(r *http.Request, source, subject string, labelsBlocked []string, tierBlocked bool, countFiltered, countRemaining int) {
	d.emitSecurityEvent(securitylog.Event{
		EventType: "retrieval_firewall_filtered",
		Source:    source,
		Subject:   subject,
		IP:        effectiveClientIPFromContext(r.Context()),
		Endpoint:  r.URL.Path,
		Details: map[string]interface{}{
			"labels_blocked":  labelsBlocked,
			"tier_blocked":    tierBlocked,
			"count_filtered":  countFiltered,
			"count_remaining": countRemaining,
			"request_id":      middleware.GetReqID(r.Context()),
		},
	})
}

// emitRetrievalFirewallDenied emits a retrieval_firewall_denied security event
// when a query is fully denied by the retrieval firewall pre-query check.
// Reference: Tech Spec Addendum Section A3.7.
func (d *Daemon) emitRetrievalFirewallDenied(r *http.Request, source, subject, reason string) {
	d.emitSecurityEvent(securitylog.Event{
		EventType: "retrieval_firewall_denied",
		Source:    source,
		Subject:   subject,
		IP:        effectiveClientIPFromContext(r.Context()),
		Endpoint:  r.URL.Path,
		Details: map[string]interface{}{
			"reason":     reason,
			"request_id": middleware.GetReqID(r.Context()),
		},
	})
}

// emitAdminAccess emits an admin_access security event.
func (d *Daemon) emitAdminAccess(r *http.Request) {
	d.emitSecurityEvent(securitylog.Event{
		EventType: "admin_access",
		IP:        effectiveClientIPFromContext(r.Context()),
		Endpoint:  r.URL.Path,
		Details: map[string]interface{}{
			"user_agent": r.UserAgent(),
			"request_id": middleware.GetReqID(r.Context()),
		},
	})
}
