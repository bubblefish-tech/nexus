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
// BubbleFish Nexus. All structs model the TOML files in ~/.nexus/Nexus/.
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
	Ingest         IngestConfig         `toml:"ingest"`
	Credentials    CredentialsConfig    `toml:"credentials"`
	A2A            A2AConfig            `toml:"a2a"`

	// Substrate holds the [substrate] section for BF-Sketch.
	// Reference: v0.1.3 BF-Sketch Substrate Build Plan.
	Substrate SubstrateConfig `toml:"substrate"`

	// Canonical holds the [canonical] section for embedding canonicalization.
	// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.2.
	Canonical CanonicalConfig `toml:"canonical"`

	// Control holds the [control] section for the Nexus-native policy engine.
	// Reference: v0.1.3 Moat-Takeover Build Plan, MT.3.
	Control ControlConfig `toml:"control"`

	// Tunnels holds [[tunnels]] sections for external tunnel providers.
	// Reference: Tech Spec WIRE.7.
	Tunnels []TunnelConfig `toml:"tunnels"`

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

	// ResolvedReviewListKey is the resolved bfn_review_list_ token bytes.
	// Nil if not configured. NEVER log.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
	ResolvedReviewListKey []byte

	// ResolvedReviewReadKey is the resolved bfn_review_read_ token bytes.
	// Nil if not configured. NEVER log.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
	ResolvedReviewReadKey []byte

	// ResolvedA2ARegToken is the resolved a2a.registration_token bytes.
	// Nil when self-registration is disabled (empty or not configured). NEVER log.
	ResolvedA2ARegToken []byte
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
	// Review token configuration for the governance review UI (Phase 5).
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
	Review         ReviewTokensConfig   `toml:"review"`
	// Tier-level rate limit overrides. Key is tier level (0-3).
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.4.
	Tiers          []TierRateLimitConfig `toml:"tiers"`
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

// A2AConfig models the top-level [a2a] section. Defaults to disabled.
type A2AConfig struct {
	Enabled           bool   `toml:"enabled"`
	RegistrationToken string `toml:"registration_token"` // env:/file:/literal; empty = self-registration disabled
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
	// CompressEnabled enables zstd compression for new WAL entries. 3-5x size
	// reduction, fewer bytes to fsync, smaller backups. Replay auto-detects
	// compressed entries regardless of this flag, so mixed segments work.
	// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.10.
	CompressEnabled bool `toml:"compress_enabled"`
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

	// TLS support (CU.0.7). Default off; set tls_enabled = true to enable.
	// When operator cert/key are absent the daemon auto-generates ~/.nexus/keys/tls.crt.
	TLSEnabled  bool   `toml:"tls_enabled"`
	TLSCertFile string `toml:"tls_cert_file"` // env:/file:/literal reference
	TLSKeyFile  string `toml:"tls_key_file"`  // env:/file:/literal reference
}

// WebConfig models [daemon.web].
type WebConfig struct {
	Port        int  `toml:"port"`
	RequireAuth bool `toml:"require_auth"`

	// TLS support (CU.0.7). Dashboard serves HTTPS by default.
	// Set tls_disabled = true to revert to HTTP.
	// When operator cert/key are absent the daemon auto-generates ~/.nexus/keys/tls.crt.
	TLSDisabled bool   `toml:"tls_disabled"`
	TLSCertFile string `toml:"tls_cert_file"` // env:/file:/literal reference
	TLSKeyFile  string `toml:"tls_key_file"`  // env:/file:/literal reference
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
	Name           string            `toml:"name"`
	Type           string            `toml:"type"` // "webhook", "syslog", "fluentd", "otlp"
	URL            string            `toml:"url"`
	TimeoutSeconds int               `toml:"timeout_seconds"`
	MaxRetries     int               `toml:"max_retries"`
	Content        string            `toml:"content"`  // "summary" or "full"
	Facility       string            `toml:"facility"` // syslog facility
	Tag            string            `toml:"tag"`      // syslog/fluentd tag
	Headers        map[string]string `toml:"headers"`  // OTLP custom headers
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

// CredentialsConfig models the [credentials] TOML section. Controls the
// Agent Gateway credential proxy that substitutes synthetic keys for real
// provider keys at upstream dispatch time.
// Reference: AG.3.
type CredentialsConfig struct {
	Enabled  bool                     `toml:"enabled"`
	Mappings []CredentialMappingConfig `toml:"mappings"`
}

// CredentialMappingConfig models [[credentials.mappings]].
// The real_key_ref uses the env:/file: reference scheme — the resolved value
// is NEVER stored in config structs or logged.
type CredentialMappingConfig struct {
	SyntheticPrefix string   `toml:"synthetic_prefix"`
	RealKeyRef      string   `toml:"real_key_ref"`
	Provider        string   `toml:"provider"` // "openai" or "anthropic"
	AllowedAgents   []string `toml:"allowed_agents"`
	AllowedModels   []string `toml:"allowed_models"`
	RateLimitRPM    int      `toml:"rate_limit_rpm"`
}

// IngestConfig models the [ingest] TOML section. Controls proactive
// filesystem-based ingestion of AI client conversations.
type IngestConfig struct {
	Enabled          bool     `toml:"enabled"`
	KillSwitch       bool     `toml:"kill_switch"`
	DebounceDuration int      `toml:"debounce_duration_ms"` // milliseconds; converted to time.Duration by caller
	ParseConcurrency int      `toml:"parse_concurrency"`
	MaxFileSize      int64    `toml:"max_file_size"`
	MaxLineLength    int      `toml:"max_line_length"`
	AllowlistPaths   []string `toml:"allowlist_paths"`

	ClaudeCodeEnabled      bool     `toml:"claude_code_enabled"`
	CursorEnabled          bool     `toml:"cursor_enabled"`
	GenericJSONLEnabled    bool     `toml:"generic_jsonl_enabled"`
	GenericJSONLPaths      []string `toml:"generic_jsonl_paths"`
	ChatGPTDesktopEnabled  bool     `toml:"chatgpt_desktop_enabled"`
	ClaudeDesktopEnabled   bool     `toml:"claude_desktop_enabled"`
	LMStudioEnabled        bool     `toml:"lm_studio_enabled"`
	OpenWebUIEnabled       bool     `toml:"open_webui_enabled"`
	PerplexityCometEnabled bool     `toml:"perplexity_comet_enabled"`
}

// ---------------------------------------------------------------------------
// Source TOML — ~/.nexus/Nexus/sources/*.toml
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
	Signing          SourceSigningConfig        `toml:"signing"`
}

// SourceSigningConfig models [source.signing].
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.1.
type SourceSigningConfig struct {
	// Mode controls Ed25519 signing for writes from this source.
	// "local" = sign with per-source key in secrets/sources/<name>.ed25519.
	// "" (empty) = signing disabled (default, backward compatible).
	Mode string `toml:"mode"`
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
	// Signing holds per-source Ed25519 signing configuration.
	// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.1.
	Signing          SourceSigningConfig
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
// Destination TOML — ~/.nexus/Nexus/destinations/*.toml
// ---------------------------------------------------------------------------

// destinationFile is used exclusively for TOML decoding of a destination file.
type destinationFile struct {
	Destination destinationBody `toml:"destination"`
}

// destinationBody models the [destination] section in a destination TOML file.
type destinationBody struct {
	Name   string                    `toml:"name"`
	// Type selects the memory backend. One of:
	//   "sqlite", "postgres", "supabase",
	//   "mysql", "cockroachdb", "mongodb", "firestore", "tidb", "turso"
	Type   string                    `toml:"type"`
	DBPath           string          `toml:"db_path"`           // sqlite: env:/file:/literal path
	DSN              string          `toml:"dsn"`               // postgres/mysql/cockroachdb/tidb: env:/file:/literal
	URL              string          `toml:"url"`               // supabase base URL; env:/file:/literal
	APIKey           string          `toml:"api_key"`           // supabase/firestore credentials; env:/file:/literal
	ConnectionString string          `toml:"connection_string"` // mongodb URI, turso URL, firestore project ID; env:/file:/literal
	Decay  DestinationDecayConfig    `toml:"decay"`
}

// Destination is the fully decoded, validated destination configuration.
type Destination struct {
	Name             string
	Type             string
	DBPath           string
	DSN              string
	URL              string
	APIKey           string
	ConnectionString string
	Decay            DestinationDecayConfig
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

// ReviewTokensConfig models [daemon.review].
// Review tokens are for the Phase 5 governance UI. Two classes:
//   - bfn_review_list_ : list quarantined memory IDs (read-only)
//   - bfn_review_read_ : read the content of specific quarantined IDs (read-only)
//
// Both classes are constant-time compared. Both return 401 on any endpoint
// other than their designated review routes.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
type ReviewTokensConfig struct {
	// ListToken is the env:/file:/literal reference for the bfn_review_list_ token.
	ListToken string `toml:"list_token"`
	// ReadToken is the env:/file:/literal reference for the bfn_review_read_ token.
	ReadToken string `toml:"read_token"`
}

// TierRateLimitConfig models one [[daemon.tiers]] entry.
// Per-source overrides take precedence over tier-level limits.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.4.
type TierRateLimitConfig struct {
	// Level is the tier number this config applies to (0-3).
	Level             int   `toml:"level"`
	// RequestsPerMinute is the rate limit for this tier. 0 = unlimited.
	RequestsPerMinute int   `toml:"requests_per_minute"`
	// BytesPerSecond is the byte-rate limit for this tier. 0 = unlimited.
	BytesPerSecond    int64 `toml:"bytes_per_second"`
}

// SubstrateConfig holds the [substrate] TOML section for BF-Sketch.
// All fields default to safe values (disabled). When absent from daemon.toml,
// the substrate is completely invisible.
// Reference: v0.1.3 BF-Sketch Substrate Build Plan.
type SubstrateConfig struct {
	Enabled                bool    `toml:"enabled"`
	SketchBits             int     `toml:"sketch_bits"`
	RatchetRotationPeriod  string  `toml:"ratchet_rotation_period"`
	PrefilterThreshold     int     `toml:"prefilter_threshold"`
	PrefilterTopK          int     `toml:"prefilter_top_k"`
	CuckooCapacity         uint    `toml:"cuckoo_capacity"`
	CuckooRebuildThreshold float64 `toml:"cuckoo_rebuild_threshold"`
	EncryptionEnabled      bool    `toml:"encryption_enabled"`
}

// CanonicalConfig holds the [canonical] TOML section for embedding
// canonicalization. Required when substrate is enabled.
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.2.
type CanonicalConfig struct {
	Enabled              bool `toml:"enabled"`
	CanonicalDim         int  `toml:"canonical_dim"`
	WhiteningWarmup      int  `toml:"whitening_warmup"`
	QueryCacheTTLSeconds int  `toml:"query_cache_ttl_seconds"`
}

// ControlConfig models the [control] TOML section for the MT.3 policy engine.
// Defaults to disabled — set enabled = true to activate control-plane stores
// and policy evaluation.
type ControlConfig struct {
	Enabled      bool                      `toml:"enabled"`
	Capabilities ControlCapabilitiesConfig `toml:"capabilities"`
}

// ControlCapabilitiesConfig models [control.capabilities].
type ControlCapabilitiesConfig struct {
	// RequireApproval is a list of capability names that require an approved
	// approval request before the policy engine will allow the action.
	RequireApproval []string `toml:"require_approval"`
}

// TunnelConfig models one [[tunnels]] TOML entry.
//
// All providers share Provider, LocalPort, and Enabled. Additional fields are
// provider-specific (AuthToken for Cloudflare/ngrok, Hostname for Cloudflare,
// Domain for Tailscale, Address for Bore, Command for custom).
//
// Reference: Tech Spec WIRE.7.
type TunnelConfig struct {
	Provider  string `toml:"provider"`  // cloudflare, ngrok, tailscale, bore, custom
	LocalPort int    `toml:"local_port"` // local daemon port to tunnel
	Enabled   bool   `toml:"enabled"`

	// Cloudflare Tunnel fields.
	AuthToken string `toml:"auth_token"` // env:VAR or ENC:v1:... or plaintext (NEVER log)
	Hostname  string `toml:"hostname"`   // e.g. nexus.example.com

	// ngrok fields (reuse AuthToken; add Region).
	Region string `toml:"region"` // e.g. "us", "eu"

	// Tailscale Funnel fields.
	Domain string `toml:"domain"` // Tailscale domain for HTTPS Funnel

	// Bore fields.
	Address string `toml:"address"` // bore server address, e.g. bore.pub:7835

	// Custom tunnel — arbitrary shell command.
	// Placeholders: {port} is replaced with LocalPort.
	Command string `toml:"command"`
}
