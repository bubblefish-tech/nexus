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

package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	// defaultPort is used when daemon.toml omits the port field.
	defaultPort = 8080

	// defaultQueueSize is the in-memory queue channel length.
	defaultQueueSize = 10_000

	// defaultMaxSegmentSizeMB is the WAL rotation threshold.
	defaultMaxSegmentSizeMB = 50

	// defaultDrainTimeoutSeconds is the graceful shutdown drain window.
	defaultDrainTimeoutSeconds = 30

	// defaultRequestsPerMinute is the per-source fallback rate limit.
	defaultRequestsPerMinute = 2_000

	// defaultMaxBytes is the per-source payload size limit when unset.
	defaultMaxBytes int64 = 10 * 1024 * 1024 // 10 MiB
)

// ConfigDir returns the canonical configuration directory for BubbleFish Nexus.
// If the BUBBLEFISH_HOME environment variable is set and non-empty, its value is
// used (resolved to an absolute path). Otherwise falls back to ~/.bubblefish/Nexus.
// Returns an error if path resolution fails; callers must treat this as fatal.
func ConfigDir() (string, error) {
	if env := os.Getenv("BUBBLEFISH_HOME"); env != "" {
		return filepath.Abs(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".bubblefish", "Nexus"), nil
}

// Load reads daemon.toml, sources/*.toml, and destinations/*.toml from
// configDir, validates and resolves all secret references, then returns the
// fully initialised Config.
//
// Validation failures are returned as errors with the "SCHEMA_ERROR:" prefix
// so callers can format them consistently. Any SCHEMA_ERROR means the daemon
// must not start.
//
// Reference: Tech Spec Section 9, Section 6.1.
func Load(configDir string, logger *slog.Logger) (*Config, error) {
	cfg, err := loadDaemonTOML(filepath.Join(configDir, "daemon.toml"), logger)
	if err != nil {
		return nil, err
	}

	sources, err := loadSources(filepath.Join(configDir, "sources"), logger)
	if err != nil {
		return nil, err
	}
	cfg.Sources = sources

	dests, err := loadDestinations(filepath.Join(configDir, "destinations"), logger)
	if err != nil {
		return nil, err
	}
	cfg.Destinations = dests

	if err := resolveAndValidate(cfg, configDir, logger); err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadDaemonTOML decodes daemon.toml and applies defaults.
func loadDaemonTOML(path string, logger *slog.Logger) (*Config, error) {
	// daemonTOML mirrors the file layout for BurntSushi/toml decoding.
	type daemonTOML struct {
		Daemon         DaemonConfig         `toml:"daemon"`
		Retrieval      RetrievalConfig      `toml:"retrieval"`
		Consistency    ConsistencyConfig    `toml:"consistency"`
		SecurityEvents SecurityEventsConfig `toml:"security_events"`
		Credentials    CredentialsConfig    `toml:"credentials"`
		A2A            A2AConfig            `toml:"a2a"`
	}

	var raw daemonTOML
	meta, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return nil, fmt.Errorf("config: decode %q: %w", path, err)
	}

	// Apply deployment mode overlay before general defaults. Mode presets
	// only fill fields the user did NOT explicitly set in the TOML file.
	// Reference: Tech Spec Section 2.2.3.
	if err := applyMode(&raw.Daemon, meta); err != nil {
		return nil, err
	}

	// Apply defaults for zero-value fields.
	if raw.Daemon.Port == 0 {
		raw.Daemon.Port = defaultPort
	}
	if raw.Daemon.QueueSize <= 0 {
		raw.Daemon.QueueSize = defaultQueueSize
	}
	if raw.Daemon.WAL.MaxSegmentSizeMB <= 0 {
		raw.Daemon.WAL.MaxSegmentSizeMB = defaultMaxSegmentSizeMB
	}
	if raw.Daemon.Shutdown.DrainTimeoutSeconds <= 0 {
		raw.Daemon.Shutdown.DrainTimeoutSeconds = defaultDrainTimeoutSeconds
	}
	if raw.Daemon.RateLimit.GlobalRequestsPerMinute <= 0 {
		raw.Daemon.RateLimit.GlobalRequestsPerMinute = defaultRequestsPerMinute
	}
	if raw.Daemon.WAL.Integrity.Mode == "" {
		raw.Daemon.WAL.Integrity.Mode = "crc32"
	}
	if raw.Retrieval.DefaultProfile == "" {
		raw.Retrieval.DefaultProfile = "balanced"
	}
	if raw.Retrieval.HalfLifeDays == 0 {
		raw.Retrieval.HalfLifeDays = 7
	}
	if raw.Retrieval.OverSampleFactor == 0 {
		raw.Retrieval.OverSampleFactor = 100
	}
	if raw.Retrieval.DecayMode == "" {
		raw.Retrieval.DecayMode = "exponential"
	}

	// Audit defaults. Reference: Tech Spec Addendum Section A4.1.
	if raw.Daemon.Audit.MaxFileSizeMB <= 0 {
		raw.Daemon.Audit.MaxFileSizeMB = 100
	}
	if raw.Daemon.Audit.AdminRateLimitPerMin <= 0 {
		raw.Daemon.Audit.AdminRateLimitPerMin = 60
	}
	if raw.Daemon.Audit.LogFile == "" {
		raw.Daemon.Audit.LogFile = filepath.Join(filepath.Dir(path), "logs", "interactions.jsonl")
	}

	// Retrieval firewall defaults. Reference: Tech Spec Addendum Section A4.1.
	if len(raw.Daemon.RetrievalFirewall.TierOrder) == 0 {
		raw.Daemon.RetrievalFirewall.TierOrder = []string{"public", "internal", "confidential", "restricted"}
	}
	if raw.Daemon.RetrievalFirewall.DefaultTier == "" {
		raw.Daemon.RetrievalFirewall.DefaultTier = "public"
	}

	cfg := &Config{
		Daemon:         raw.Daemon,
		Retrieval:      raw.Retrieval,
		Consistency:    raw.Consistency,
		SecurityEvents: raw.SecurityEvents,
		Credentials:    raw.Credentials,
		A2A:            raw.A2A,
	}

	if logger != nil {
		logger.Debug("config: daemon.toml loaded",
			"component", "config",
			"path", path,
		)
	}

	return cfg, nil
}

// loadSources discovers and decodes all *.toml files under sourcesDir.
// Returns an empty slice if the directory does not exist.
func loadSources(sourcesDir string, logger *slog.Logger) ([]*Source, error) {
	pattern := filepath.Join(sourcesDir, "*.toml")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("config: glob sources %q: %w", pattern, err)
	}

	sources := make([]*Source, 0, len(paths))
	for _, p := range paths {
		src, err := loadSourceFile(p, logger)
		if err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}
	return sources, nil
}

// loadSourceFile decodes a single source TOML file.
func loadSourceFile(path string, logger *slog.Logger) (*Source, error) {
	var raw sourceFile
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, fmt.Errorf("config: decode source %q: %w", path, err)
	}
	b := raw.Source

	if b.Name == "" {
		return nil, fmt.Errorf("SCHEMA_ERROR: source file %q: name is required", path)
	}
	if b.APIKey == "" {
		return nil, fmt.Errorf("SCHEMA_ERROR: source %q: api_key is required", b.Name)
	}

	// Apply source defaults.
	if b.PayloadLimits.MaxBytes <= 0 {
		b.PayloadLimits.MaxBytes = defaultMaxBytes
	}
	if b.RateLimit.RequestsPerMinute <= 0 {
		b.RateLimit.RequestsPerMinute = defaultRequestsPerMinute
	}
	if b.DefaultProfile == "" {
		b.DefaultProfile = "balanced"
	}
	if b.DefaultActorType == "" {
		b.DefaultActorType = "user"
	}
	if b.Namespace == "" {
		b.Namespace = b.Name
	}

	// Apply retrieval firewall defaults.
	// Reference: Tech Spec Addendum Section A4.2.
	if b.Policy.RetrievalFirewall.MaxClassificationTier == "" {
		b.Policy.RetrievalFirewall.MaxClassificationTier = "restricted"
	}
	if b.Policy.RetrievalFirewall.DefaultClassificationTier == "" {
		b.Policy.RetrievalFirewall.DefaultClassificationTier = "public"
	}

	// Apply tier defaults.
	// Tier 3 = unrestricted read access (backward compat for sources that
	// predate tier partitioning). DefaultWriteTier 1 = "internal" sensitivity
	// for new memories. Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	if b.Tier == 0 {
		b.Tier = 3 // full clearance by default
	}
	if b.DefaultWriteTier == 0 {
		b.DefaultWriteTier = 1 // internal sensitivity by default
	}

	src := &Source{
		Name:             b.Name,
		APIKey:           b.APIKey,
		Namespace:        b.Namespace,
		CanRead:          b.CanRead,
		CanWrite:         b.CanWrite,
		TargetDest:       b.TargetDest,
		DefaultActorType: b.DefaultActorType,
		DefaultActorID:   b.DefaultActorID,
		DefaultProfile:   b.DefaultProfile,
		Tier:             b.Tier,
		DefaultWriteTier: b.DefaultWriteTier,
		RateLimit:        b.RateLimit,
		PayloadLimits:    b.PayloadLimits,
		Mapping:          b.Mapping,
		Transform:        b.Transform,
		Idempotency:      b.Idempotency,
		Policy:           b.Policy,
		Signing:          b.Signing,
	}

	if logger != nil {
		logger.Debug("config: source loaded",
			"component", "config",
			"source", src.Name,
			"path", path,
		)
	}

	return src, nil
}

// loadDestinations discovers and decodes all *.toml files under destsDir.
// Returns an empty slice if the directory does not exist.
func loadDestinations(destsDir string, logger *slog.Logger) ([]*Destination, error) {
	pattern := filepath.Join(destsDir, "*.toml")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("config: glob destinations %q: %w", pattern, err)
	}

	dests := make([]*Destination, 0, len(paths))
	for _, p := range paths {
		dst, err := loadDestinationFile(p, logger)
		if err != nil {
			return nil, err
		}
		dests = append(dests, dst)
	}
	return dests, nil
}

// loadDestinationFile decodes a single destination TOML file.
func loadDestinationFile(path string, logger *slog.Logger) (*Destination, error) {
	var raw destinationFile
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, fmt.Errorf("config: decode destination %q: %w", path, err)
	}
	b := raw.Destination

	if b.Name == "" {
		return nil, fmt.Errorf("SCHEMA_ERROR: destination file %q: name is required", path)
	}
	if b.Type == "" {
		return nil, fmt.Errorf("SCHEMA_ERROR: destination %q: type is required", b.Name)
	}

	dst := &Destination{
		Name:             b.Name,
		Type:             b.Type,
		DBPath:           b.DBPath,
		DSN:              b.DSN,
		URL:              b.URL,
		APIKey:           b.APIKey,
		ConnectionString: b.ConnectionString,
		Decay:            b.Decay,
	}

	if logger != nil {
		logger.Debug("config: destination loaded",
			"component", "config",
			"destination", dst.Name,
			"type", dst.Type,
			"path", path,
		)
	}

	return dst, nil
}

// resolveAndValidate resolves all secret references and validates the full
// configuration. Any empty resolved key or duplicate resolved key across
// sources is a SCHEMA_ERROR.
//
// Reference: Tech Spec Section 6.1, Phase 0C Behavioral Contract items 3–6.
func resolveAndValidate(cfg *Config, configDir string, logger *slog.Logger) error {
	// Resolve admin token.
	adminKey, err := ResolveEnv(cfg.Daemon.AdminToken, logger)
	if err != nil {
		return fmt.Errorf("SCHEMA_ERROR: admin_token: %w", err)
	}
	if adminKey == "" {
		return fmt.Errorf("SCHEMA_ERROR: admin_token resolved to empty string")
	}
	cfg.ResolvedAdminKey = []byte(adminKey)

	// Resolve source keys and check for empties and duplicates.
	// Resolved values are compared — NOT the raw references.
	cfg.ResolvedSourceKeys = make(map[string][]byte, len(cfg.Sources))
	// seen maps resolved key (as string) → source name, for duplicate detection.
	seen := make(map[string]string, len(cfg.Sources))

	for _, src := range cfg.Sources {
		resolved, err := ResolveEnv(src.APIKey, logger)
		if err != nil {
			return fmt.Errorf("SCHEMA_ERROR: source %q api_key: %w", src.Name, err)
		}
		if resolved == "" {
			return fmt.Errorf("SCHEMA_ERROR: source %q api_key resolved to empty string", src.Name)
		}
		if prev, dup := seen[resolved]; dup {
			return fmt.Errorf("SCHEMA_ERROR: sources %q and %q share the same resolved api_key", prev, src.Name)
		}
		seen[resolved] = src.Name
		cfg.ResolvedSourceKeys[src.Name] = []byte(resolved)
	}

	// Precompute retrieval firewall string sets for each source so PostFilter
	// does not allocate maps on every read request.
	for _, src := range cfg.Sources {
		fw := &src.Policy.RetrievalFirewall
		fw.BlockedLabelsSet = makeStringSet(fw.BlockedLabels)
		fw.RequiredLabelsSet = makeStringSet(fw.RequiredLabels)
		fw.VisibleNamespacesSet = makeStringSet(fw.VisibleNamespaces)
	}

	// Validate source→destination references.
	destNames := make(map[string]bool, len(cfg.Destinations))
	for _, d := range cfg.Destinations {
		destNames[d.Name] = true
	}
	for _, src := range cfg.Sources {
		if src.TargetDest != "" && !destNames[src.TargetDest] {
			return fmt.Errorf("SCHEMA_ERROR: source %q references unknown destination %q", src.Name, src.TargetDest)
		}
	}

	// Resolve MCP key if MCP is enabled and api_key is set.
	// The resolved key is stored for use by both the MCP server and the HTTP
	// auth path (to reject MCP tokens on HTTP endpoints).
	if cfg.Daemon.MCP.Enabled && cfg.Daemon.MCP.APIKey != "" {
		mcpKey, err := ResolveEnv(cfg.Daemon.MCP.APIKey, logger)
		if err != nil {
			return fmt.Errorf("SCHEMA_ERROR: mcp api_key: %w", err)
		}
		if mcpKey != "" {
			cfg.ResolvedMCPKey = []byte(mcpKey)
		}
	}

	// Resolve review tokens if configured.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
	if cfg.Daemon.Review.ListToken != "" {
		listKey, err := ResolveEnv(cfg.Daemon.Review.ListToken, logger)
		if err != nil {
			return fmt.Errorf("SCHEMA_ERROR: review.list_token: %w", err)
		}
		if listKey != "" {
			cfg.ResolvedReviewListKey = []byte(listKey)
		}
	}
	if cfg.Daemon.Review.ReadToken != "" {
		readKey, err := ResolveEnv(cfg.Daemon.Review.ReadToken, logger)
		if err != nil {
			return fmt.Errorf("SCHEMA_ERROR: review.read_token: %w", err)
		}
		if readKey != "" {
			cfg.ResolvedReviewReadKey = []byte(readKey)
		}
	}

	// Validate WAL path is set (default to config dir if empty).
	if cfg.Daemon.WAL.Path == "" {
		cfg.Daemon.WAL.Path = filepath.Join(configDir, "wal")
	}

	// Validate OAuth config if enabled.
	// Reference: Post-Build Add-On Update Technical Specification Section 6.2.
	if cfg.Daemon.OAuth.Enabled {
		if cfg.Daemon.OAuth.IssuerURL == "" {
			return fmt.Errorf("SCHEMA_ERROR: oauth.issuer_url is required when oauth is enabled")
		}
		// private_key_file MUST use file: reference. Plain literals are rejected.
		pkf := cfg.Daemon.OAuth.PrivateKeyFile
		if pkf != "" && !strings.HasPrefix(pkf, "file:") && !strings.HasPrefix(pkf, "env:") {
			return fmt.Errorf("SCHEMA_ERROR: oauth.private_key_file must use file: or env: reference, not a plain literal")
		}
		if len(cfg.Daemon.OAuth.Clients) == 0 {
			if logger != nil {
				logger.Warn("oauth enabled but no clients registered",
					"component", "config",
				)
			}
		}
		if !strings.HasPrefix(cfg.Daemon.OAuth.IssuerURL, "https://") {
			// Allow localhost for development.
			if !strings.Contains(cfg.Daemon.OAuth.IssuerURL, "localhost") &&
				!strings.Contains(cfg.Daemon.OAuth.IssuerURL, "127.0.0.1") {
				if logger != nil {
					logger.Warn("oauth issuer_url should use HTTPS",
						"component", "config",
						"issuer_url", cfg.Daemon.OAuth.IssuerURL,
					)
				}
			}
		}
	}

	return nil
}

// makeStringSet converts a string slice to a set (map[string]struct{}) for
// O(1) lookup. Used to precompute retrieval firewall sets at load time.
func makeStringSet(items []string) map[string]struct{} {
	s := make(map[string]struct{}, len(items))
	for _, item := range items {
		s[item] = struct{}{}
	}
	return s
}
