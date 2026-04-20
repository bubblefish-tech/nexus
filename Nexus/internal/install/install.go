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

// Options describes what the wizard collects before calling Install.
type Options struct {
	ConfigDir      string
	Mode           string // simple | balanced | safe | import
	DestType       string // sqlite | postgres | mysql | cockroachdb | mongodb | firestore | tidb | turso
	DSN            string // connection string for non-sqlite backends
	EncryptionPass string // empty = no encryption
	Force          bool
}

// Install creates the Nexus config directory tree from wizard options.
// It is idempotent when Force is true.
func Install(opts Options) error {
	configDir := opts.configDir()

	// Directory tree.
	dirs := []string{
		configDir,
		filepath.Join(configDir, "sources"),
		filepath.Join(configDir, "destinations"),
		filepath.Join(configDir, "compiled"),
		filepath.Join(configDir, "wal"),
		filepath.Join(configDir, "logs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return fmt.Errorf("create dir %q: %w", d, err)
		}
	}

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

	daemonTOML, _ := BuildDaemonTOML(configDir, mode, adminKey, mcpKey, "")
	daemonPath := filepath.Join(configDir, "daemon.toml")
	if err := WriteConfigFile(daemonPath, daemonTOML, opts.Force); err != nil {
		return fmt.Errorf("write daemon.toml: %w", err)
	}

	if err := WriteDestination(configDir, destType, opts.DSN, opts.Force); err != nil {
		return fmt.Errorf("write destination: %w", err)
	}

	if err := WriteDefaultSource(configDir, mode, destType, sourceKey, opts.Force); err != nil {
		return fmt.Errorf("write source: %w", err)
	}

	return nil
}

func (o Options) configDir() string {
	if o.ConfigDir != "" {
		return o.ConfigDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nexus", "nexus")
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
// homeFlag > BUBBLEFISH_HOME env var > ~/.nexus/nexus
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
		dir = filepath.Join(home, ".nexus", "nexus")
	}
	return filepath.Abs(dir)
}

// BuildDaemonTOML generates daemon.toml content for the given mode and keys.
// Returns the TOML string and the resolved bind address (e.g. "127.0.0.1:8080").
func BuildDaemonTOML(configDir, mode, adminKey, mcpKey, oauthIssuer string) (string, string) {
	bind := "127.0.0.1"
	port := 8080
	logLevel := "info"
	logFormat := "json"
	queueSize := 10000
	walIntegrity := "crc32"
	walEncryption := false
	rateLimit := 2000
	embeddingEnabled := false
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
enabled = true
port = 7474
bind = "127.0.0.1"
source_name = "default"
api_key = "%s"

[daemon.web]
port = %d
require_auth = true

[daemon.tls]
enabled = false

[daemon.trusted_proxies]
cidrs = []
forwarded_headers = []

[daemon.signing]
enabled = false

[daemon.jwt]
enabled = false

[daemon.events]
enabled = false

[daemon.audit]
log_file = "%s"

[retrieval]
time_decay = true
half_life_days = 7.0
decay_mode = "exponential"
over_sample_factor = 100
default_profile = "balanced"

[consistency]
enabled = false
interval_seconds = 300
sample_size = 100

[security_events]
enabled = true
log_file = "%s"
`, version.Version, mode, port, bind, adminKey, logLevel, logFormat, mode,
		queueSize, walPath, walIntegrity, walEncryption, rateLimit, embeddingEnabled, mcpKey, webPort,
		auditLogPath, securityLogPath)

	if oauthIssuer != "" {
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
content = "message.content"
role    = "message.role"
model   = "model"

[source.policy]
allowed_destinations = ["%s"]
max_results = 20
max_response_bytes = 16384
`, apiKey, targetDest, rateLimit, targetDest)

	return WriteConfigFile(filepath.Join(sourcesDir, "default.toml"), content, force)
}
