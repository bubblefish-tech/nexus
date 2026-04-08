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
// Matches securitylog.Event from the server.
type SecurityEvent struct {
	EventType string                 `json:"event_type"`
	Source    string                 `json:"source,omitempty"`
	Subject  string                 `json:"subject,omitempty"`
	IP       string                 `json:"ip,omitempty"`
	Endpoint string                 `json:"endpoint,omitempty"`
	Timestamp time.Time             `json:"timestamp"`
	Details  map[string]interface{} `json:"details,omitempty"`
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
	BySource                  map[string]int `json:"by_source"`
}

// ConflictEntry is a single conflict group.
// Matches destination.ConflictGroup from the server.
type ConflictEntry struct {
	Subject           string      `json:"subject"`
	EntityKey         string      `json:"entity_key"`
	ConflictingValues []string    `json:"conflicting_values"`
	Sources           []string    `json:"sources"`
	Timestamps        []time.Time `json:"timestamps"`
	Count             int         `json:"count"`
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
// Matches destination.TranslatedPayload from the server.
type TimeTravelRecord struct {
	PayloadID        string    `json:"payload_id"`
	RequestID        string    `json:"request_id"`
	Source           string    `json:"source"`
	Subject          string    `json:"subject"`
	Namespace        string    `json:"namespace"`
	Destination      string    `json:"destination"`
	Collection       string    `json:"collection"`
	Content          string    `json:"content"`
	Model            string    `json:"model"`
	Role             string    `json:"role"`
	Timestamp        time.Time `json:"timestamp"`
	IdempotencyKey   string    `json:"idempotency_key"`
	SchemaVersion    int       `json:"schema_version"`
	TransformVersion string    `json:"transform_version"`
	ActorType        string    `json:"actor_type"`
	ActorID          string    `json:"actor_id"`
}

// TimeTravelResponse is the shape of GET /api/timetravel.
type TimeTravelResponse struct {
	AsOf       string             `json:"as_of"`
	HasMore    bool               `json:"has_more"`
	NextCursor string             `json:"next_cursor"`
	Records    []TimeTravelRecord `json:"records"`
}

// TimeTravelOpts are query parameters for time-travel.
type TimeTravelOpts struct {
	AsOf    string
	Subject string
	Limit   int
}

// AuditRecord is a single audit log entry.
// Matches audit.InteractionRecord from the server.
type AuditRecord struct {
	RecordID       string    `json:"record_id"`
	RequestID      string    `json:"request_id"`
	Timestamp      time.Time `json:"timestamp"`
	Source         string    `json:"source"`
	ActorType      string    `json:"actor_type"`
	ActorID        string    `json:"actor_id"`
	EffectiveIP    string    `json:"effective_ip"`
	OperationType  string    `json:"operation_type"`
	Endpoint       string    `json:"endpoint"`
	HTTPMethod     string    `json:"http_method"`
	HTTPStatusCode int       `json:"http_status_code"`
	Subject        string    `json:"subject,omitempty"`
	Destination    string    `json:"destination,omitempty"`
	PolicyDecision string    `json:"policy_decision"`
	PolicyReason   string    `json:"policy_reason,omitempty"`
	LatencyMs      float64   `json:"latency_ms"`
}

// AuditResponse is the shape of GET /api/audit/log.
type AuditResponse struct {
	Records       []AuditRecord `json:"records"`
	TotalMatching int           `json:"total_matching"`
	Limit         int           `json:"limit"`
	Offset        int           `json:"offset"`
	HasMore       bool          `json:"has_more"`
}

// ConfigResponse is the shape of GET /api/config.
// Contains sanitized config — NEVER includes secrets.
type ConfigResponse struct {
	Daemon       ConfigDaemon       `json:"daemon"`
	WAL          ConfigWAL          `json:"wal"`
	MCP          ConfigMCP          `json:"mcp"`
	Web          ConfigWeb          `json:"web"`
	Embedding    ConfigEmbedding    `json:"embedding"`
	TLS          ConfigTLS          `json:"tls"`
	RateLimit    ConfigRateLimit    `json:"rate_limit"`
	Retrieval    ConfigRetrieval    `json:"retrieval"`
	Audit        ConfigAudit        `json:"audit"`
	Sources      []string           `json:"sources"`
	Destinations []string           `json:"destinations"`
}

// ConfigDaemon holds sanitized daemon settings.
type ConfigDaemon struct {
	Bind      string `json:"bind"`
	Port      int    `json:"port"`
	LogLevel  string `json:"log_level"`
	Mode      string `json:"mode"`
	QueueSize int    `json:"queue_size"`
}

// ConfigWAL holds sanitized WAL settings.
type ConfigWAL struct {
	Path               string `json:"path"`
	MaxSegmentSizeMB   int64  `json:"max_segment_size_mb"`
	IntegrityMode      string `json:"integrity_mode"`
	EncryptionEnabled  bool   `json:"encryption_enabled"`
	WatchdogIntervalS  int    `json:"watchdog_interval_s"`
	WatchdogMinDisk    int64  `json:"watchdog_min_disk"`
}

// ConfigMCP holds sanitized MCP settings.
type ConfigMCP struct {
	Enabled    bool   `json:"enabled"`
	Port       int    `json:"port"`
	Bind       string `json:"bind"`
	SourceName string `json:"source_name"`
}

// ConfigWeb holds sanitized web dashboard settings.
type ConfigWeb struct {
	Port        int  `json:"port"`
	RequireAuth bool `json:"require_auth"`
}

// ConfigEmbedding holds sanitized embedding settings.
type ConfigEmbedding struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
}

// ConfigTLS holds sanitized TLS settings.
type ConfigTLS struct {
	Enabled    bool   `json:"enabled"`
	MinVersion string `json:"min_version"`
}

// ConfigRateLimit holds sanitized rate limit settings.
type ConfigRateLimit struct {
	GlobalRPM int `json:"global_rpm"`
}

// ConfigRetrieval holds sanitized retrieval settings.
type ConfigRetrieval struct {
	TimeDecay      bool    `json:"time_decay"`
	HalfLifeDays   float64 `json:"half_life_days"`
	DefaultProfile string  `json:"default_profile"`
}

// ConfigAudit holds sanitized audit settings.
type ConfigAudit struct {
	Enabled   bool `json:"enabled"`
	DualWrite bool `json:"dual_write"`
}
