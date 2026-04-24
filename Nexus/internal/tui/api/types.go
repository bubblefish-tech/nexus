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
	Status              string                      `json:"status"`
	InstanceName        string                      `json:"instance_name"`
	Version             string                      `json:"version"`
	QueueDepth          int                         `json:"queue_depth"`
	ConsistencyScore    float64                     `json:"consistency_score"`
	MemoriesTotal       int                         `json:"memories_total"`
	SourcesTotal        int                         `json:"sources_total"`
	UptimeSeconds       int                         `json:"uptime_seconds"`
	Goroutines          int                         `json:"goroutines"`
	MemoryResidentBytes int64                       `json:"memory_resident_bytes"`
	PID                 int                         `json:"pid"`
	Bind                string                      `json:"bind"`
	WebPort             int                         `json:"web_port"`
	Cache               StatusCache                 `json:"cache"`
	WAL                 StatusWAL                   `json:"wal"`
	Destinations        []StatusDest                `json:"destinations"`
	WritesTotal         int64                       `json:"writes_total"`
	Writes1m            int                         `json:"writes_1m"`
	ReadsTotal          int64                       `json:"reads_total"`
	Reads1m             int                         `json:"reads_1m"`
	Errors1m            int                         `json:"errors_1m"`
	ImmuneScans         int64                       `json:"immune_scans"`
	QuarantineTotal     int64                       `json:"quarantine_total"`
	AuditEnabled        bool                        `json:"audit_enabled"`
	CascadeStages       map[string]StageMetricEntry `json:"cascade_stages"`
	WriteStages         map[string]StageMetricEntry `json:"write_stages"`
	SourceHealth        []SourceHealthEntry         `json:"source_health"`
}

type StageMetricEntry struct {
	Status string  `json:"status"`
	AvgMs  float64 `json:"avg_ms"`
	Hits   int64   `json:"hits"`
}

type SourceHealthEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type StatusCache struct {
	HitRate      float64 `json:"hit_rate"`
	ExactRate    float64 `json:"exact_rate"`
	SemanticRate float64 `json:"semantic_rate"`
}

type StatusWAL struct {
	Healthy               bool   `json:"healthy"`
	CurrentSegment        string `json:"current_segment"`
	IntegrityMode         string `json:"integrity_mode"`
	PendingEntries        int    `json:"pending_entries"`
	LastCheckpointSecsAgo int    `json:"last_checkpoint_seconds_ago"`
}

type StatusDest struct {
	Name      string  `json:"name"`
	Healthy   bool    `json:"healthy"`
	LastError *string `json:"last_error"`
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
	Subject   string                 `json:"subject,omitempty"`
	IP        string                 `json:"ip,omitempty"`
	Endpoint  string                 `json:"endpoint,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// SecurityEventsResponse is the shape of GET /api/security/events.
type SecurityEventsResponse struct {
	Events []SecurityEvent `json:"events"`
}

// SecuritySummaryResponse is the shape of GET /api/security/summary.
type SecuritySummaryResponse struct {
	AuthFailures              int            `json:"auth_failures_total"`
	PolicyDenials             int            `json:"policy_denials_total"`
	RateLimitHits             int            `json:"rate_limit_hits_total"`
	WALTamperDetected         int            `json:"wal_tamper_detected"`
	ConfigSignatureInvalid    int            `json:"config_signature_invalid"`
	AdminAccess               int            `json:"admin_calls_total"`
	RetrievalFirewallFiltered int            `json:"retrieval_firewall_filtered"`
	RetrievalFirewallDenied   int            `json:"retrieval_firewall_denied"`
	BySource                  map[string]int `json:"by_source"`
}

// ConflictEntry is a single conflict group.
type ConflictEntry struct {
	ID        string           `json:"id"`
	Subject   string           `json:"subject"`
	Entity    string           `json:"entity"`
	GroupSize int              `json:"group_size"`
	Memories  []ConflictMemory `json:"memories"`
}

type ConflictMemory struct {
	Source    string `json:"source"`
	ActorType string `json:"actor_type"`
	Ts        string `json:"ts"`
	Content   string `json:"content"`
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
	PrevHash       string    `json:"prev_hash,omitempty"`
	Hash           string    `json:"hash,omitempty"`
	Signature      string    `json:"signature,omitempty"`
	SignatureValid bool      `json:"signature_valid,omitempty"`
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
	Daemon       ConfigDaemon    `json:"daemon"`
	WAL          ConfigWAL       `json:"wal"`
	MCP          ConfigMCP       `json:"mcp"`
	Web          ConfigWeb       `json:"web"`
	Embedding    ConfigEmbedding `json:"embedding"`
	TLS          ConfigTLS       `json:"tls"`
	RateLimit    ConfigRateLimit `json:"rate_limit"`
	Retrieval    ConfigRetrieval `json:"retrieval"`
	Audit        ConfigAudit     `json:"audit"`
	Sources      []string        `json:"sources"`
	Destinations []string        `json:"destinations"`
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
	Path              string `json:"path"`
	MaxSegmentSizeMB  int64  `json:"max_segment_size_mb"`
	IntegrityMode     string `json:"integrity_mode"`
	EncryptionEnabled bool   `json:"encryption_enabled"`
	WatchdogIntervalS int    `json:"watchdog_interval_s"`
	WatchdogMinDisk   int64  `json:"watchdog_min_disk"`
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

// AgentSummary is a condensed view of a registered A2A agent.
type AgentSummary struct {
	AgentID     string    `json:"agent_id"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	TrustTier   int       `json:"trust_tier"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// AgentsResponse is the shape of GET /api/control/agents.
type AgentsResponse struct {
	Agents []AgentSummary `json:"agents"`
}

// StatusBroadcastMsg is sent by the root model to forward cached status to screens.
type StatusBroadcastMsg struct {
	Data *StatusResponse
}

// HealthBroadcastMsg is sent by the root model to forward health state to screens.
type HealthBroadcastMsg struct {
	OK bool
}

// QuarantineRecord is a quarantined memory record.
type QuarantineRecord struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Content    string    `json:"content"`
	Rule       string    `json:"rule"`
	Status     string    `json:"status"`
	ReviewedBy string    `json:"reviewed_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// QuarantineResponse is the unified shape of GET /api/quarantine.
// Contains records AND total/pending counts in a single payload.
type QuarantineResponse struct {
	Records []QuarantineRecord `json:"records"`
	Count   int                `json:"count"`
	Total   int                `json:"total"`
	Pending int                `json:"pending"`
}

// QuarantineCountResponse is the shape of GET /api/quarantine/count.
type QuarantineCountResponse struct {
	Total   int `json:"total"`
	Pending int `json:"pending"`
}

// Grant is a governance grant record.
type Grant struct {
	ID         string `json:"id"`
	AgentID    string `json:"agent_id"`
	Capability string `json:"capability"`
	Scope      string `json:"scope"`
	GrantedBy  string `json:"granted_by"`
	ExpiresAt  int64  `json:"expires_at_ms"`
}

// GrantsResponse is the shape of GET /api/control/grants.
type GrantsResponse struct {
	Grants []Grant `json:"grants"`
}

// Approval is a pending approval request.
type Approval struct {
	ID         string `json:"id"`
	AgentID    string `json:"agent_id"`
	Capability string `json:"capability"`
	Action     string `json:"action"`
	Decision   string `json:"decision"`
	Reason     string `json:"reason,omitempty"`
}

// ApprovalsResponse is the shape of GET /api/control/approvals.
type ApprovalsResponse struct {
	Approvals []Approval `json:"approvals"`
}

// Task is a governance task.
type Task struct {
	ID           string `json:"id"`
	AgentID      string `json:"agent_id"`
	ParentTaskID string `json:"parent_task_id,omitempty"`
	Capability   string `json:"capability"`
	Status       string `json:"status"`
	Input        string `json:"input,omitempty"`
	Output       string `json:"output,omitempty"`
}

// TasksResponse is the shape of GET /api/control/tasks.
type TasksResponse struct {
	Tasks []Task `json:"tasks"`
}

// Memory represents a single memory row returned by GET /api/memories.
type Memory struct {
	ID          string  `json:"payload_id"`
	Content     string  `json:"content,omitempty"`
	Source      string  `json:"source"`
	Actor       string  `json:"actor,omitempty"`
	ActorType   string  `json:"actor_type,omitempty"`
	Namespace   string  `json:"namespace,omitempty"`
	CreatedAt   string  `json:"created_at"`
	Destination string  `json:"destination,omitempty"`
	Preview     string  `json:"preview,omitempty"`
	Score       float64 `json:"score,omitempty"`
}

// MemoryDetail extends Memory with provenance and scoring details.
type MemoryDetail struct {
	Memory
	ProvenanceEntry int    `json:"provenance_entry"`
	PrevHash        string `json:"prev_hash"`
	Hash            string `json:"hash"`
	Signature       string `json:"signature"`
	MerkleRoot      string `json:"merkle_root"`
	Scores          struct {
		BM25  float64 `json:"bm25"`
		Dense float64 `json:"dense"`
		RRF   float64 `json:"rrf"`
		Decay float64 `json:"decay"`
	} `json:"scores"`
}

// MemoryListEnvelope is the pagination metadata from GET /api/memories.
type MemoryListEnvelope struct {
	ResultCount int    `json:"result_count"`
	HasMore     bool   `json:"has_more"`
	NextCursor  string `json:"next_cursor,omitempty"`
}

// MemoryListResponse is the shape of GET /api/memories.
type MemoryListResponse struct {
	Memories []Memory           `json:"memories"`
	Admin    MemoryListEnvelope `json:"_admin"`
}

// SigningStatus is the shape of GET /api/crypto/signing.
type SigningStatus struct {
	Enabled        bool   `json:"enabled"`
	Reason         string `json:"reason,omitempty"`
	ConfigHint     string `json:"config_hint,omitempty"`
	PublicKeyHash  string `json:"public_key_hash,omitempty"`
	SignedCount    int64  `json:"signed_count"`
	VerifyFailures int64  `json:"verify_failures"`
}

// CryptoProfile is the shape of GET /api/crypto/profile.
type CryptoProfile struct {
	Symmetric string `json:"symmetric"`
	Signing   string `json:"signing"`
	KDF       string `json:"kdf"`
	Hash      string `json:"hash"`
	Ratchet   string `json:"ratchet"`
}

// MasterKeyStatus is the shape of GET /api/crypto/master.
type MasterKeyStatus struct {
	Derived   bool   `json:"derived"`
	Algorithm string `json:"algorithm"`
	Reason    string `json:"reason,omitempty"`
}

// RatchetStatus is the shape of GET /api/crypto/ratchet.
type RatchetStatus struct {
	Position       int64  `json:"position"`
	DestroyedCount int64  `json:"destroyed_count"`
	Algorithm      string `json:"algorithm"`
}

// AggregatedStats is the shape of GET /api/stats.
type AggregatedStats struct {
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
