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

// Package api provides HTTP client and types for the Nexus admin API.
package api

import "time"

// StatusResponse is the shape of GET /api/status.
type StatusResponse struct {
	Status           string  `json:"status"`
	Version          string  `json:"version"`
	QueueDepth       int     `json:"queue_depth"`
	ConsistencyScore float64 `json:"consistency_score"`
}

// HealthResponse is the shape of GET /health.
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// LintFinding is a single lint finding.
type LintFinding struct {
	Severity string `json:"severity"`
	Check    string `json:"check"`
	Message  string `json:"message"`
}

// LintResponse is the shape of GET /api/lint.
type LintResponse struct {
	Findings []LintFinding `json:"findings"`
}

// SecurityEvent is a single security event.
type SecurityEvent struct {
	EventType string            `json:"event_type"`
	IP        string            `json:"ip"`
	Endpoint  string            `json:"endpoint"`
	Timestamp time.Time         `json:"timestamp"`
	Details   map[string]string `json:"details"`
}

// SecurityEventsResponse is the shape of GET /api/security/events.
type SecurityEventsResponse struct {
	Events []SecurityEvent `json:"events"`
}

// SecuritySummaryResponse is the shape of GET /api/security/summary.
type SecuritySummaryResponse struct {
	AuthFailures              int            `json:"auth_failures"`
	PolicyDenials             int            `json:"policy_denials"`
	RateLimitHits             int            `json:"rate_limit_hits"`
	WALTamperDetected         int            `json:"wal_tamper_detected"`
	ConfigSignatureInvalid    int            `json:"config_signature_invalid"`
	AdminAccess               int            `json:"admin_access"`
	RetrievalFirewallFiltered int            `json:"retrieval_firewall_filtered"`
	RetrievalFirewallDenied   int            `json:"retrieval_firewall_denied"`
	BySource                  map[string]any `json:"by_source"`
}

// ConflictEntry is a single conflict group.
type ConflictEntry struct {
	Subject           string    `json:"subject"`
	EntityKey         string    `json:"entity_key"`
	ConflictingValues []string  `json:"conflicting_values"`
	Sources           []string  `json:"sources"`
	Timestamps        []string  `json:"timestamps"`
	Count             int       `json:"count"`
}

// ConflictsResponse is the shape of GET /api/conflicts.
type ConflictsResponse struct {
	Conflicts []ConflictEntry `json:"conflicts"`
	Count     int             `json:"count"`
}

// ConflictOpts are query parameters for conflicts.
type ConflictOpts struct {
	Limit int
}

// TimeTravelRecord is a single time-travel record.
type TimeTravelRecord struct {
	PayloadID    string `json:"payload_id"`
	Source       string `json:"source"`
	Subject      string `json:"subject"`
	Namespace    string `json:"namespace"`
	Content      string `json:"content"`
	Model        string `json:"model"`
	Role         string `json:"role"`
	Timestamp    string `json:"timestamp"`
	ActorType    string `json:"actor_type"`
	ActorID      string `json:"actor_id"`
	Destination  string `json:"destination"`
}

// TimeTravelResponse is the shape of GET /api/timetravel.
type TimeTravelResponse struct {
	AsOf    string             `json:"as_of"`
	HasMore bool               `json:"has_more"`
	Records []TimeTravelRecord `json:"records"`
}

// TimeTravelOpts are query parameters for time-travel.
type TimeTravelOpts struct {
	AsOf    string
	Subject string
	Limit   int
}

// AuditRecord is a single audit log entry.
type AuditRecord struct {
	Timestamp string `json:"timestamp"`
	Source     string `json:"source"`
	Action    string `json:"action"`
	Subject   string `json:"subject"`
	Namespace string `json:"namespace"`
	Endpoint  string `json:"endpoint"`
	Status    int    `json:"status"`
	Latency   string `json:"latency_ms"`
	Error     string `json:"error_code"`
	ActorType string `json:"actor_type"`
}

// AuditResponse is the shape of GET /api/audit/log.
type AuditResponse struct {
	Records       []AuditRecord `json:"records"`
	TotalMatching int           `json:"total_matching"`
	Limit         int           `json:"limit"`
	Offset        int           `json:"offset"`
	HasMore       bool          `json:"has_more"`
}
