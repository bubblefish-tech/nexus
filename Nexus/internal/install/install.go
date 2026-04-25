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

// Package install provides pure config-generation helpers used by both the
// CLI `nexus install` command and the TUI setup wizard.
package install

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bubblefish-tech/nexus/internal/version"
)

// ToolSelection describes a single AI tool the user selected during setup.
type ToolSelection struct {
	Name           string
	ConnectionType string
	Endpoint       string
}

// Options describes what the wizard collects before calling Install.
type Options struct {
	ConfigDir      string
	Mode           string // simple | balanced | safe | import
	DestType       string // sqlite | postgres | mysql | cockroachdb | mongodb | firestore | tidb | turso
	DSN            string // connection string for non-sqlite backends
	EncryptionPass string // empty = no encryption
	Force          bool

	Features      map[string]bool  // feature name → enabled
	SelectedTools []ToolSelection  // tools the user chose to connect
	TunnelEnabled  bool
	TunnelProvider string // cloudflare | ngrok | tailscale | bore | custom
	TunnelEndpoint string
}

// InstallResult carries data generated during installation for display to the user.
type InstallResult struct {
	ConfigDir string
	AdminKey  string
	SourceKey string
	MCPKey    string
	BindAddr  string
}

// Install creates the Nexus config directory tree from wizard options.
// It is idempotent when Force is true.
func Install(opts Options) (*InstallResult, error) {
	configDir := opts.configDir()

	dirs := []string{
		configDir,
		filepath.Join(configDir, "sources"),
		filepath.Join(configDir, "destinations"),
		filepath.Join(configDir, "compiled"),
		filepath.Join(configDir, "wal"),
		filepath.Join(configDir, "logs"),
		filepath.Join(configDir, "keys"),
		filepath.Join(configDir, "discovery"),
		filepath.Join(configDir, "tools"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, fmt.Errorf("create dir %q: %w", d, err)
		}
	}

	// Fsync sanity audit: verify the data directory honors fsync.
	FsyncAudit(configDir)

	adminKey := GenerateKey("bfn_admin_")
	sourceKey := GenerateKey("bfn_data_")
	mcpKey := GenerateKey("bfn_mcp_")

	mode := opts.Mode
	if mode == "" {
		mode = "simple"
	}
	destType := opts.DestType
	if destType == "" {
		destType = "sqlite"
	}

	daemonTOML, bindAddr := BuildDaemonTOML(configDir, mode, adminKey, mcpKey, "", opts.Features)
	daemonPath := filepath.Join(configDir, "daemon.toml")
	if err := WriteConfigFile(daemonPath, daemonTOML, opts.Force); err != nil {
		return nil, fmt.Errorf("write daemon.toml: %w", err)
	}

	if err := WriteDestination(configDir, destType, opts.DSN, opts.Force); err != nil {
		return nil, fmt.Errorf("write destination: %w", err)
	}

	if err := WriteDefaultSource(configDir, mode, destType, sourceKey, opts.Force); err != nil {
		return nil, fmt.Errorf("write source: %w", err)
	}

	if opts.TunnelEnabled && opts.TunnelProvider != "" {
		if err := WriteTunnelConfig(configDir, opts.TunnelProvider, opts.TunnelEndpoint, opts.Force); err != nil {
			return nil, fmt.Errorf("write tunnel config: %w", err)
		}
	}

	if err := WriteToolConfigs(configDir, opts.SelectedTools, opts.Force); err != nil {
		return nil, fmt.Errorf("write tool configs: %w", err)
	}

	return &InstallResult{
		ConfigDir: configDir,
		AdminKey:  adminKey,
		SourceKey: sourceKey,
		MCPKey:    mcpKey,
		BindAddr:  bindAddr,
	}, nil
}

func (o Options) configDir() string {
	if o.ConfigDir != "" {
		return o.ConfigDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "BubbleFish", "Nexus")
}

// GenerateKey returns a cryptographically random 32-byte hex-encoded key
// with the given prefix.
func GenerateKey(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("install: generate key: " + err.Error())
	}
	return prefix + hex.EncodeToString(b)
}

// WriteConfigFile writes content to path with 0600 permissions.
// When force is false it skips files that already exist.
func WriteConfigFile(path, content string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return nil // already exists, skip
		}
	}
	return os.WriteFile(path, []byte(content), 0600)
}

// ResolveInstallHome resolves the config directory using:
// homeFlag > BUBBLEFISH_HOME env var > ~/BubbleFish/Nexus
func ResolveInstallHome(homeFlag string) (string, error) {
	var dir string
	switch {
	case homeFlag != "":
		dir = homeFlag
	case os.Getenv("BUBBLEFISH_HOME") != "":
		dir = os.Getenv("BUBBLEFISH_HOME")
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, "BubbleFish", "Nexus")
	}
	return filepath.Abs(dir)
}

func featureEnabled(features map[string]bool, key string, fallback bool) bool {
	if features == nil {
		return fallback
	}
	if v, ok := features[key]; ok {
		return v
	}
	return fallback
}

// BuildDaemonTOML generates daemon.toml content for the given mode, keys, and
// feature toggles. Returns the TOML string and the resolved bind address.
func BuildDaemonTOML(configDir, mode, adminKey, mcpKey, oauthIssuer string, features map[string]bool) (string, string) {
	bind := "127.0.0.1"
	port := 8080
	logLevel := "info"
	logFormat := "json"
	queueSize := 10000
	walIntegrity := "crc32"
	walEncryption := false
	rateLimit := 2000
	webPort := 8081

	switch mode {
	case "simple":
		logLevel = "info"
		logFormat = "text"
		rateLimit = 5000
	case "safe":
		walIntegrity = "mac"
		walEncryption = true
		rateLimit = 500
	}

	embeddingEnabled := featureEnabled(features, "embedding", false)
	mcpEnabled := featureEnabled(features, "mcp", true)
	dashboardEnabled := featureEnabled(features, "dashboard", true)
	signingEnabled := featureEnabled(features, "signing", false)
	jwtEnabled := featureEnabled(features, "jwt", false)
	eventsEnabled := featureEnabled(features, "events", false)
	tlsEnabled := featureEnabled(features, "tls", false)
	auditEnabled := featureEnabled(features, "audit", true)
	consistencyEnabled := featureEnabled(features, "consistency", false)
	securityEventsEnabled := featureEnabled(features, "security_events", true)

	walPath := filepath.ToSlash(filepath.Join(configDir, "wal"))
	securityLogPath := filepath.ToSlash(filepath.Join(configDir, "security.log"))
	auditLogPath := filepath.ToSlash(filepath.Join(configDir, "logs", "interactions.jsonl"))

	t := fmt.Sprintf(`# BubbleFish Nexus -- daemon.toml
# Generated by nexus install (v%s)
# Mode: %s

[daemon]
port = %d
bind = "%s"
admin_token = "%s"
log_level = "%s"
log_format = "%s"
mode = "%s"
queue_size = %d

[daemon.shutdown]
drain_timeout_seconds = 30

[daemon.wal]
path = "%s"
max_segment_size_mb = 50

[daemon.wal.integrity]
mode = "%s"

[daemon.wal.encryption]
enabled = %t

[daemon.wal.watchdog]
interval_seconds = 30
min_disk_bytes = 104857600
max_append_latency_ms = 100

[daemon.rate_limit]
global_requests_per_minute = %d

[daemon.embedding]
enabled = %t

[daemon.mcp]
enabled = %t
port = 7474
bind = "127.0.0.1"
source_name = "default"
api_key = "%s"

[daemon.web]
enabled = %t
port = %d
require_auth = true

[daemon.tls]
enabled = %t

[daemon.trusted_proxies]
cidrs = []
forwarded_headers = []

[daemon.signing]
enabled = %t

[daemon.jwt]
enabled = %t

[daemon.events]
enabled = %t

[daemon.audit]
enabled = %t
log_file = "%s"

[retrieval]
time_decay = true
half_life_days = 7.0
decay_mode = "exponential"
over_sample_factor = 100
default_profile = "balanced"

[consistency]
enabled = %t
interval_seconds = 300
sample_size = 100

[security_events]
enabled = %t
log_file = "%s"
`, version.Version, mode, port, bind, adminKey, logLevel, logFormat, mode,
		queueSize, walPath, walIntegrity, walEncryption, rateLimit,
		embeddingEnabled, mcpEnabled, mcpKey, dashboardEnabled, webPort,
		tlsEnabled, signingEnabled, jwtEnabled, eventsEnabled,
		auditEnabled, auditLogPath,
		consistencyEnabled, securityEventsEnabled, securityLogPath)

	if oauthIssuer != "" || featureEnabled(features, "oauth", false) {
		if oauthIssuer == "" {
			oauthIssuer = "https://localhost:8080"
		}
		keyPath := filepath.ToSlash(filepath.Join(configDir, "oauth_private.key"))
		t += fmt.Sprintf(`
[daemon.oauth]
enabled = true
issuer_url = "%s"
private_key_file = "file:%s"
access_token_ttl_seconds = 3600
auth_code_ttl_seconds = 300

[[daemon.oauth.clients]]
client_id = "chatgpt"
client_name = "ChatGPT"
redirect_uris = ["https://chatgpt.com/aip/g/CHANGE_ME/oauth/callback"]
oauth_source_name = "default"
allowed_scopes = ["openid", "mcp"]
`, oauthIssuer, keyPath)
	}

	return t, fmt.Sprintf("%s:%d", bind, port)
}

// WriteTunnelConfig creates tunnel.toml when a tunnel provider is selected.
func WriteTunnelConfig(configDir, provider, endpoint string, force bool) error {
	content := fmt.Sprintf(`# BubbleFish Nexus -- Tunnel Configuration
[tunnel]
enabled = true
provider = "%s"
endpoint = "%s"
mcp_port = 7474
`, provider, endpoint)
	return WriteConfigFile(filepath.Join(configDir, "tunnel.toml"), content, force)
}

// WriteToolConfigs creates a TOML config file per selected tool in the tools/ directory.
func WriteToolConfigs(configDir string, tools []ToolSelection, force bool) error {
	toolsDir := filepath.Join(configDir, "tools")
	for _, t := range tools {
		content := fmt.Sprintf(`# BubbleFish Nexus -- Tool: %s
[tool]
name = "%s"
connection_type = "%s"
endpoint = "%s"
enabled = true
`, t.Name, t.Name, t.ConnectionType, t.Endpoint)
		safeName := sanitizeFileName(t.Name)
		path := filepath.Join(toolsDir, safeName+".toml")
		if err := WriteConfigFile(path, content, force); err != nil {
			return fmt.Errorf("write tool %q: %w", t.Name, err)
		}
	}
	return nil
}

func sanitizeFileName(name string) string {
	var b []byte
	for _, c := range []byte(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
			b = append(b, c)
		case c >= 'A' && c <= 'Z':
			b = append(b, c+'a'-'A')
		case c == ' ':
			b = append(b, '_')
		}
	}
	if len(b) == 0 {
		return "tool"
	}
	return string(b)
}

// WriteDestination creates the appropriate destination TOML file.
// For backends with a connection string (postgres, mongodb, turso, etc.) the
// dsn parameter is written into the config; empty dsn is written as-is.
func WriteDestination(configDir, destType, dsn string, force bool) error {
	destDir := filepath.Join(configDir, "destinations")

	var content string
	switch destType {
	case "sqlite", "":
		dbPath := filepath.ToSlash(filepath.Join(configDir, "memories.db"))
		content = fmt.Sprintf(`# BubbleFish Nexus -- SQLite destination
[destination]
name = "sqlite"
type = "sqlite"
db_path = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dbPath)
		return WriteConfigFile(filepath.Join(destDir, "sqlite.toml"), content, force)

	case "postgres":
		content = fmt.Sprintf(`# BubbleFish Nexus -- PostgreSQL destination
[destination]
name = "postgres"
type = "postgres"
dsn = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dsn)
		return WriteConfigFile(filepath.Join(destDir, "postgres.toml"), content, force)

	case "mysql", "mariadb":
		content = fmt.Sprintf(`# BubbleFish Nexus -- MySQL/MariaDB destination
[destination]
name = "mysql"
type = "mysql"
dsn = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dsn)
		return WriteConfigFile(filepath.Join(destDir, "mysql.toml"), content, force)

	case "cockroachdb", "crdb":
		content = fmt.Sprintf(`# BubbleFish Nexus -- CockroachDB destination
[destination]
name = "cockroachdb"
type = "cockroachdb"
dsn = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dsn)
		return WriteConfigFile(filepath.Join(destDir, "cockroachdb.toml"), content, force)

	case "mongodb", "mongo":
		content = fmt.Sprintf(`# BubbleFish Nexus -- MongoDB destination
[destination]
name = "mongodb"
type = "mongodb"
connection_string = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dsn)
		return WriteConfigFile(filepath.Join(destDir, "mongodb.toml"), content, force)

	case "firestore":
		content = fmt.Sprintf(`# BubbleFish Nexus -- Firestore destination
[destination]
name = "firestore"
type = "firestore"
connection_string = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dsn)
		return WriteConfigFile(filepath.Join(destDir, "firestore.toml"), content, force)

	case "tidb":
		content = fmt.Sprintf(`# BubbleFish Nexus -- TiDB destination
[destination]
name = "tidb"
type = "tidb"
dsn = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dsn)
		return WriteConfigFile(filepath.Join(destDir, "tidb.toml"), content, force)

	case "turso", "libsql":
		content = fmt.Sprintf(`# BubbleFish Nexus -- Turso/libSQL destination
[destination]
name = "turso"
type = "turso"
connection_string = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dsn)
		return WriteConfigFile(filepath.Join(destDir, "turso.toml"), content, force)

	default:
		return fmt.Errorf("unknown destination type %q", destType)
	}
}

// WriteDefaultSource creates sources/default.toml.
func WriteDefaultSource(configDir, mode, destType, apiKey string, force bool) error {
	sourcesDir := filepath.Join(configDir, "sources")

	rateLimit := 2000
	targetDest := "sqlite"
	switch destType {
	case "postgres":
		targetDest = "postgres"
	case "mysql", "mariadb":
		targetDest = "mysql"
	case "cockroachdb", "crdb":
		targetDest = "cockroachdb"
	case "mongodb", "mongo":
		targetDest = "mongodb"
	case "firestore":
		targetDest = "firestore"
	case "tidb":
		targetDest = "tidb"
	case "turso", "libsql":
		targetDest = "turso"
	}

	switch mode {
	case "simple":
		rateLimit = 5000
	case "safe":
		rateLimit = 500
	}

	content := fmt.Sprintf(`# BubbleFish Nexus -- default source
[source]
name = "default"
api_key = "%s"
namespace = "default"
can_read = true
can_write = true
target_destination = "%s"
default_actor_type = "user"
default_profile = "balanced"

[source.rate_limit]
requests_per_minute = %d

[source.payload_limits]
max_bytes = 10485760

[source.idempotency]
enabled = true
dedup_window_seconds = 300

[source.mapping]
content = "content"
role    = "role"
model   = "model"

[source.policy]
allowed_destinations = ["%s"]
max_results = 20
max_response_bytes = 16384
`, apiKey, targetDest, rateLimit, targetDest)

	return WriteConfigFile(filepath.Join(sourcesDir, "default.toml"), content, force)
}
