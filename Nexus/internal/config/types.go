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

// Package config provides config loading, resolution, and validation for
// BubbleFish Nexus. All structs model the TOML files in ~/.bubblefish/Nexus/.
//
// Config is loaded once at startup and treated as immutable. Hot-reload
// (Phase 0D) replaces the pointer atomically; in-flight requests always
// finish with the config they started with.
package config

// Config is the fully loaded and resolved runtime configuration.
// After a successful Load, ResolvedSourceKeys and ResolvedAdminKey are
// populated and safe to use on the hot path without any os.Getenv calls.
type Config struct {
	Daemon         DaemonConfig         `toml:"daemon"`
	Retrieval      RetrievalConfig      `toml:"retrieval"`
	Consistency    ConsistencyConfig    `toml:"consistency"`
	SecurityEvents SecurityEventsConfig `toml:"security_events"`

	// Sources and Destinations are populated by scanning the sources/ and
	// destinations/ sub-directories. Not decoded from daemon.toml itself.
	Sources      []*Source
	Destinations []*Destination

	// ResolvedSourceKeys maps source name → resolved API key bytes.
	// Pre-computed at startup; never mutated after Load returns.
	// NEVER log these values.
	ResolvedSourceKeys map[string][]byte

	// ResolvedAdminKey is the resolved admin_token bytes.
	// NEVER log this value.
	ResolvedAdminKey []byte

	// ResolvedMCPKey is the resolved MCP api_key bytes.
	// May be nil if MCP is disabled or api_key is empty.
	// NEVER log this value.
	ResolvedMCPKey []byte
}

// SourceByName returns the Source with the given name, or nil if not found.
func (c *Config) SourceByName(name string) *Source {
	for _, s := range c.Sources {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// DestinationByName returns the Destination with the given name, or nil.
func (c *Config) DestinationByName(name string) *Destination {
	for _, d := range c.Destinations {
		if d.Name == name {
			return d
		}
	}
	return nil
}

// DaemonConfig models the [daemon] section of daemon.toml.
type DaemonConfig struct {
	Port      int    `toml:"port"`
	Bind      string `toml:"bind"`
	AdminToken string `toml:"admin_token"` // env:/file:/literal reference
	LogLevel  string `toml:"log_level"`
	LogFormat string `toml:"log_format"`
	Mode      string `toml:"mode"` // safe, balanced, or fast
	QueueSize int    `toml:"queue_size"`

	Shutdown       ShutdownConfig       `toml:"shutdown"`
	WAL            WALDaemonConfig      `toml:"wal"`
	RateLimit      GlobalRateLimitConfig `toml:"rate_limit"`
	Embedding      EmbeddingConfig      `toml:"embedding"`
	MCP            MCPConfig            `toml:"mcp"`
	Web            WebConfig            `toml:"web"`
	TLS            TLSConfig            `toml:"tls"`
	TrustedProxies TrustedProxiesConfig `toml:"trusted_proxies"`
	Signing        SigningConfig        `toml:"signing"`
	JWT            JWTConfig            `toml:"jwt"`
	Events         EventsConfig         `toml:"events"`
	Audit          AuditConfig          `toml:"audit"`
	RetrievalFirewall DaemonRetrievalFirewallConfig `toml:"retrieval_firewall"`
	OAuth          OAuthDaemonConfig    `toml:"oauth"`
}

// OAuthDaemonConfig models [daemon.oauth].
// Reference: Post-Build Add-On Update Technical Specification Section 6.1.
type OAuthDaemonConfig struct {
	Enabled              bool                    `toml:"enabled"`
	IssuerURL            string                  `toml:"issuer_url"`
	PrivateKeyFile       string                  `toml:"private_key_file"`
	AccessTokenTTLSecs   int                     `toml:"access_token_ttl_seconds"`
	AuthCodeTTLSecs      int                     `toml:"auth_code_ttl_seconds"`
	Clients              []OAuthClientConfig     `toml:"clients"`
}

// OAuthClientConfig models [[daemon.oauth.clients]].
type OAuthClientConfig struct {
	ClientID        string   `toml:"client_id"`
	ClientName      string   `toml:"client_name"`
	RedirectURIs    []string `toml:"redirect_uris"`
	OAuthSourceName string   `toml:"oauth_source_name"`
	AllowedScopes   []string `toml:"allowed_scopes"`
}

// AuditConfig models [daemon.audit].
// Reference: Tech Spec Addendum Section A4.1, Update U1.6.
type AuditConfig struct {
	Enabled              bool                  `toml:"enabled"`
	LogFile              string                `toml:"log_file"`
	MaxFileSizeMB        int                   `toml:"max_file_size_mb"`
	AdminRateLimitPerMin int                   `toml:"admin_rate_limit_per_minute"`
	DualWrite            *bool                 `toml:"dual_write"` // Default true; pointer to distinguish unset from false
	Integrity            AuditIntegrityConfig  `toml:"integrity"`
	Encryption           AuditEncryptionConfig `toml:"encryption"`
}

// AuditDualWriteEnabled returns the effective dual_write setting (default true).
func (a *AuditConfig) AuditDualWriteEnabled() bool {
	if a.DualWrite == nil {
		return true
	}
	return *a.DualWrite
}

// AuditIntegrityConfig models [daemon.audit.integrity].
// SEPARATE from [daemon.wal.integrity] — independent HMAC key.
// Reference: Update U1.1.
type AuditIntegrityConfig struct {
	Mode       string `toml:"mode"`         // "crc32" (default) or "mac"
	MacKeyFile string `toml:"mac_key_file"` // Separate 32-byte HMAC-SHA256 key for interaction log
}

// AuditEncryptionConfig models [daemon.audit.encryption].
// SEPARATE from [daemon.wal.encryption] — independent AES-256 key.
// Reference: Update U1.2.
type AuditEncryptionConfig struct {
	Enabled bool   `toml:"enabled"`
	KeyFile string `toml:"key_file"` // Separate 32-byte AES-256 key for interaction log
}

// DaemonRetrievalFirewallConfig models [daemon.retrieval_firewall].
// Reference: Tech Spec Addendum Section A4.1.
type DaemonRetrievalFirewallConfig struct {
	Enabled     bool     `toml:"enabled"`
	TierOrder   []string `toml:"tier_order"`
	DefaultTier string   `toml:"default_tier"`
}

// ShutdownConfig models [daemon.shutdown].
type ShutdownConfig struct {
	DrainTimeoutSeconds int `toml:"drain_timeout_seconds"`
}

// WALDaemonConfig models [daemon.wal].
type WALDaemonConfig struct {
	Path             string              `toml:"path"`
	MaxSegmentSizeMB int64               `toml:"max_segment_size_mb"`
	Integrity        WALIntegrityConfig  `toml:"integrity"`
	Encryption       WALEncryptionConfig `toml:"encryption"`
	Watchdog         WALWatchdogConfig   `toml:"watchdog"`
	GroupCommit      WALGroupCommitConfig  `toml:"group_commit"`
	Checkpoint       WALCheckpointConfig   `toml:"checkpoint"`
}

// WALGroupCommitConfig models [daemon.wal.group_commit].
// When enabled, WAL writes are batched with a single fsync per batch.
// Disabled by default (preserves exact v0.1.2 behaviour on upgrade).
type WALGroupCommitConfig struct {
	Enabled    bool `toml:"enabled"`
	MaxBatch   int  `toml:"max_batch"`    // max entries per batch (default 256)
	MaxDelayUS int  `toml:"max_delay_us"` // max wait in microseconds (default 500)
}

// WALCheckpointConfig models [daemon.wal.checkpoint].
type WALCheckpointConfig struct {
	IntervalEntries int `toml:"interval_entries"` // checkpoint every N entries (default 10000)
}

// WALIntegrityConfig models [daemon.wal.integrity].
type WALIntegrityConfig struct {
	Mode       string `toml:"mode"`       // "crc32" or "mac"
	MacKeyFile string `toml:"mac_key_file"` // env:/file:/literal reference
}

// WALEncryptionConfig models [daemon.wal.encryption].
type WALEncryptionConfig struct {
	Enabled bool   `toml:"enabled"`
	KeyFile string `toml:"key_file"` // env:/file:/literal reference
}

// WALWatchdogConfig models [daemon.wal.watchdog].
type WALWatchdogConfig struct {
	IntervalSeconds     int   `toml:"interval_seconds"`
	MinDiskBytes        int64 `toml:"min_disk_bytes"`
	MaxAppendLatencyMS  int   `toml:"max_append_latency_ms"`
}

// GlobalRateLimitConfig models [daemon.rate_limit].
type GlobalRateLimitConfig struct {
	GlobalRequestsPerMinute int `toml:"global_requests_per_minute"`
}

// EmbeddingConfig models [daemon.embedding].
type EmbeddingConfig struct {
	Enabled        bool   `toml:"enabled"`
	Provider       string `toml:"provider"`
	URL            string `toml:"url"`    // env:/file:/literal reference
	APIKey         string `toml:"api_key"` // env:/file:/literal reference
	Model          string `toml:"model"`
	Dimensions     int    `toml:"dimensions"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
}

// MCPConfig models [daemon.mcp].
type MCPConfig struct {
	Enabled    bool   `toml:"enabled"`
	Port       int    `toml:"port"`
	Bind       string `toml:"bind"`
	SourceName string `toml:"source_name"`
	APIKey     string `toml:"api_key"` // env:/file:/literal reference
}

// WebConfig models [daemon.web].
type WebConfig struct {
	Port        int  `toml:"port"`
	RequireAuth bool `toml:"require_auth"`
}

// TLSConfig models [daemon.tls].
type TLSConfig struct {
	Enabled      bool   `toml:"enabled"`
	CertFile     string `toml:"cert_file"` // env:/file:/literal reference
	KeyFile      string `toml:"key_file"`  // env:/file:/literal reference
	MinVersion   string `toml:"min_version"`
	MaxVersion   string `toml:"max_version"`
	ClientCAFile string `toml:"client_ca_file"`
	ClientAuth   string `toml:"client_auth"`
}

// TrustedProxiesConfig models [daemon.trusted_proxies].
type TrustedProxiesConfig struct {
	CIDRs             []string `toml:"cidrs"`
	ForwardedHeaders  []string `toml:"forwarded_headers"`
}

// SigningConfig models [daemon.signing].
type SigningConfig struct {
	Enabled bool   `toml:"enabled"`
	KeyFile string `toml:"key_file"` // env:/file:/literal reference
}

// JWTConfig models [daemon.jwt].
type JWTConfig struct {
	Enabled       bool   `toml:"enabled"`
	JWKSUrl       string `toml:"jwks_url"`
	ClaimToSource string `toml:"claim_to_source"`
	Audience      string `toml:"audience"`
}

// EventsConfig models [daemon.events].
type EventsConfig struct {
	Enabled              bool           `toml:"enabled"`
	MaxInFlight          int            `toml:"max_inflight"`
	RetryBackoffSeconds  []int          `toml:"retry_backoff_seconds"`
	Sinks                []EventSink    `toml:"sinks"`
}

// EventSink models [[daemon.events.sinks]].
type EventSink struct {
	Name           string `toml:"name"`
	URL            string `toml:"url"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
	MaxRetries     int    `toml:"max_retries"`
	Content        string `toml:"content"` // "summary" or "full"
}

// RetrievalConfig models the top-level [retrieval] section.
type RetrievalConfig struct {
	TimeDecay         bool    `toml:"time_decay"`
	HalfLifeDays      float64 `toml:"half_life_days"`
	DecayMode         string  `toml:"decay_mode"` // "exponential" or "step"
	OverSampleFactor  int     `toml:"over_sample_factor"`
	DefaultProfile    string  `toml:"default_profile"` // fast, balanced, deep
}

// ConsistencyConfig models the [consistency] section.
type ConsistencyConfig struct {
	Enabled         bool `toml:"enabled"`
	IntervalSeconds int  `toml:"interval_seconds"`
	SampleSize      int  `toml:"sample_size"`
}

// SecurityEventsConfig models the [security_events] section.
type SecurityEventsConfig struct {
	Enabled bool   `toml:"enabled"`
	LogFile string `toml:"log_file"`
}

// ---------------------------------------------------------------------------
// Source TOML — ~/.bubblefish/Nexus/sources/*.toml
// ---------------------------------------------------------------------------

// sourceFile is used exclusively for TOML decoding of a source file.
// After decoding, the inner Source fields are promoted to a flat Source struct.
type sourceFile struct {
	Source sourceBody `toml:"source"`
}

// sourceBody models the [source] section in a source TOML file.
type sourceBody struct {
	Name             string                     `toml:"name"`
	APIKey           string                     `toml:"api_key"` // env:/file:/literal ref
	Namespace        string                     `toml:"namespace"`
	CanRead          bool                       `toml:"can_read"`
	CanWrite         bool                       `toml:"can_write"`
	TargetDest       string                     `toml:"target_destination"`
	DefaultActorType string                     `toml:"default_actor_type"`
	DefaultActorID   string                     `toml:"default_actor_id"`
	DefaultProfile   string                     `toml:"default_profile"`
	// Tier is the access level of this source (0-3). Sources can only read
	// memory entries whose tier is <= this value. Tier 3 = unrestricted.
	// Default 3 preserves backward compatibility with sources that predate
	// tier partitioning. Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	Tier             int                        `toml:"tier"`
	// DefaultWriteTier is the tier assigned to entries written by this source
	// when the request does not specify a tier. Default 1 (internal).
	DefaultWriteTier int                        `toml:"default_write_tier"`
	RateLimit        SourceRateLimitConfig      `toml:"rate_limit"`
	PayloadLimits    PayloadLimitsConfig        `toml:"payload_limits"`
	Mapping          map[string]string          `toml:"mapping"`
	Transform        map[string][]string        `toml:"transform"`
	Idempotency      IdempotencyConfig          `toml:"idempotency"`
	Policy           SourcePolicyConfig         `toml:"policy"`
}

// Source is the fully decoded, validated source configuration.
// Field names mirror sourceBody but are exported and used throughout the daemon.
type Source struct {
	Name             string
	APIKey           string // raw (unresolved) reference — NEVER log resolved value
	Namespace        string
	CanRead          bool
	CanWrite         bool
	TargetDest       string
	DefaultActorType string
	DefaultActorID   string
	DefaultProfile   string
	// Tier is the access level of this source (0-3). Sources can only read
	// entries at tier <= Tier. Default 3 = unrestricted (backward compat).
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	Tier             int
	// DefaultWriteTier is the tier stamped on entries written by this source
	// when the request does not specify a tier. Default 1 (internal).
	DefaultWriteTier int
	RateLimit        SourceRateLimitConfig
	PayloadLimits    PayloadLimitsConfig
	Mapping          map[string]string   // output field → gjson dot-path
	Transform        map[string][]string // output field → transform pipeline
	Idempotency      IdempotencyConfig
	Policy           SourcePolicyConfig
}

// SourceRateLimitConfig models [source.rate_limit].
type SourceRateLimitConfig struct {
	RequestsPerMinute int   `toml:"requests_per_minute"`
	BytesPerSecond    int64 `toml:"bytes_per_second"` // 0 = unlimited
}

// PayloadLimitsConfig models [source.payload_limits].
type PayloadLimitsConfig struct {
	MaxBytes int64 `toml:"max_bytes"`
}

// IdempotencyConfig models [source.idempotency].
type IdempotencyConfig struct {
	Enabled             bool `toml:"enabled"`
	DedupWindowSeconds  int  `toml:"dedup_window_seconds"`
}

// SourcePolicyConfig models [source.policy].
type SourcePolicyConfig struct {
	AllowedDestinations   []string             `toml:"allowed_destinations"`
	AllowedOperations     []string             `toml:"allowed_operations"`
	AllowedRetrievalModes []string             `toml:"allowed_retrieval_modes"`
	AllowedProfiles       []string             `toml:"allowed_profiles"`
	MaxResults            int                  `toml:"max_results"`
	MaxResponseBytes      int                  `toml:"max_response_bytes"`
	FieldVisibility       FieldVisibilityConfig `toml:"field_visibility"`
	Cache                 PolicyCacheConfig     `toml:"cache"`
	Decay                 PolicyDecayConfig     `toml:"decay"`
	RetrievalFirewall     SourceRetrievalFirewallConfig `toml:"retrieval_firewall"`
}

// SourceRetrievalFirewallConfig models [source.policy.retrieval_firewall].
// Reference: Tech Spec Addendum Section A4.2.
type SourceRetrievalFirewallConfig struct {
	BlockedLabels            []string `toml:"blocked_labels"`
	MaxClassificationTier    string   `toml:"max_classification_tier"`
	RequiredLabels           []string `toml:"required_labels"`
	DefaultClassificationTier string  `toml:"default_classification_tier"`
	VisibleNamespaces        []string `toml:"visible_namespaces"`
	CrossNamespaceRead       bool     `toml:"cross_namespace_read"`

	// Precomputed sets built at config-load time to avoid per-request
	// allocation in the PostFilter hot path. Rebuilt on every config load
	// (including hot-reload), so they are always fresh.
	BlockedLabelsSet    map[string]struct{} `toml:"-"`
	RequiredLabelsSet   map[string]struct{} `toml:"-"`
	VisibleNamespacesSet map[string]struct{} `toml:"-"`
}

// FieldVisibilityConfig models [source.policy.field_visibility].
type FieldVisibilityConfig struct {
	IncludeFields []string `toml:"include_fields"`
	StripMetadata bool     `toml:"strip_metadata"`
}

// PolicyCacheConfig models [source.policy.cache].
type PolicyCacheConfig struct {
	ReadFromCache              bool    `toml:"read_from_cache"`
	WriteToCache               bool    `toml:"write_to_cache"`
	MaxTTLSeconds              int     `toml:"max_ttl_seconds"`
	SemanticSimilarityThreshold float64 `toml:"semantic_similarity_threshold"`
}

// PolicyDecayConfig models [source.policy.decay] (per-source override).
type PolicyDecayConfig struct {
	HalfLifeDays      float64 `toml:"half_life_days"`
	DecayMode         string  `toml:"decay_mode"`
	StepThresholdDays float64 `toml:"step_threshold_days"`
}

// ---------------------------------------------------------------------------
// Destination TOML — ~/.bubblefish/Nexus/destinations/*.toml
// ---------------------------------------------------------------------------

// destinationFile is used exclusively for TOML decoding of a destination file.
type destinationFile struct {
	Destination destinationBody `toml:"destination"`
}

// destinationBody models the [destination] section in a destination TOML file.
type destinationBody struct {
	Name   string                    `toml:"name"`
	Type   string                    `toml:"type"` // "sqlite", "postgres", "openbrain"
	DBPath string                    `toml:"db_path"` // sqlite only; env:/file:/literal
	DSN    string                    `toml:"dsn"`     // postgres only; env:/file:/literal
	URL    string                    `toml:"url"`     // openbrain only; env:/file:/literal
	APIKey string                    `toml:"api_key"` // openbrain only; env:/file:/literal
	Decay  DestinationDecayConfig    `toml:"decay"`
}

// Destination is the fully decoded, validated destination configuration.
type Destination struct {
	Name   string
	Type   string
	DBPath string
	DSN    string
	URL    string
	APIKey string
	Decay  DestinationDecayConfig
}

// DestinationDecayConfig models [destination.decay].
type DestinationDecayConfig struct {
	HalfLifeDays      float64                              `toml:"half_life_days"`
	DecayMode         string                               `toml:"decay_mode"`
	StepThresholdDays float64                              `toml:"step_threshold_days"`
	Collections       map[string]CollectionDecayConfig      `toml:"collections"`
}

// CollectionDecayConfig models [destination.decay.collections.<name>].
// Per-collection overrides take highest precedence in the tiered decay system.
//
// Reference: Tech Spec Section 3.6.
type CollectionDecayConfig struct {
	HalfLifeDays      float64 `toml:"half_life_days"`
	DecayMode         string  `toml:"decay_mode"`
	StepThresholdDays float64 `toml:"step_threshold_days"`
}
