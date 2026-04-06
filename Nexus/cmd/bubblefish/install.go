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

package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BubbleFish-Nexus/internal/version"
)

// runInstall executes the `bubblefish install` command.
//
// It creates the config directory tree (~/.bubblefish/Nexus/) with daemon.toml,
// sources/, and destinations/ populated according to the selected --dest and
// --mode flags. It NEVER completes silently — always prints the config path
// and next steps.
//
// Reference: Tech Spec Section 2.2, Section 13.1.
func runInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	dest := fs.String("dest", "sqlite", "destination type: sqlite, postgres, openbrain")
	mode := fs.String("mode", "balanced", "deployment mode: simple, balanced, safe")
	profile := fs.String("profile", "", "install profile: openwebui")
	oauthTemplate := fs.String("oauth-template", "", "generate OAuth template: caddy, traefik")
	force := fs.Bool("force", false, "overwrite existing config")
	fs.Parse(args)

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish install: %v\n", err)
		os.Exit(1)
	}
	configDir := filepath.Join(home, ".bubblefish", "Nexus")

	// Refuse if config already exists unless --force.
	daemonPath := filepath.Join(configDir, "daemon.toml")
	if !*force {
		if _, err := os.Stat(daemonPath); err == nil {
			fmt.Fprintf(os.Stderr, "bubblefish install: config already exists at %s — use --force to overwrite\n", configDir)
			os.Exit(1)
		}
	}

	// Create directory tree.
	dirs := []string{
		configDir,
		filepath.Join(configDir, "sources"),
		filepath.Join(configDir, "destinations"),
		filepath.Join(configDir, "compiled"),
		filepath.Join(configDir, "wal"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish install: create directory %q: %v\n", d, err)
			os.Exit(1)
		}
	}

	// Generate a random API key for the default source and admin token.
	adminKey := generateKey()
	sourceKey := generateKey()

	// Write daemon.toml based on mode.
	daemonTOML := buildDaemonTOML(*mode, adminKey)
	if err := writeConfigFile(daemonPath, daemonTOML, *force); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish install: write daemon.toml: %v\n", err)
		os.Exit(1)
	}

	// Write destination config.
	if err := writeDestination(configDir, *dest, *force); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish install: write destination: %v\n", err)
		os.Exit(1)
	}

	// Write default source config based on mode.
	if err := writeDefaultSource(configDir, *mode, *dest, sourceKey, *force); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish install: write source: %v\n", err)
		os.Exit(1)
	}

	// Handle --profile flag.
	if *profile == "openwebui" {
		if err := writeOpenWebUIProfile(configDir, *force); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish install: write openwebui profile: %v\n", err)
			os.Exit(1)
		}
	}

	// Handle --oauth-template flag.
	if *oauthTemplate != "" {
		if err := writeOAuthTemplate(configDir, *oauthTemplate, *force); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish install: write oauth template: %v\n", err)
			os.Exit(1)
		}
	}

	// Print results — NEVER silent.
	fmt.Printf("bubblefish install: ok — v%s\n", version.Version)
	fmt.Printf("  config directory: %s\n", configDir)
	fmt.Printf("  mode:            %s\n", *mode)
	fmt.Printf("  destination:     %s\n", *dest)
	fmt.Printf("  admin token:     %s\n", adminKey)
	fmt.Printf("  source API key:  %s\n", sourceKey)
	fmt.Println()

	// Print next steps — Simple Mode prints exactly three.
	if *mode == "simple" {
		fmt.Println("Next steps:")
		fmt.Println("  1. bubblefish start")
		fmt.Printf("  2. curl -X POST http://localhost:8080/inbound/default -H 'Authorization: Bearer %s' -H 'Content-Type: application/json' -d '{\"message\":{\"content\":\"Hello\",\"role\":\"user\"},\"model\":\"test\"}'\n", sourceKey)
		fmt.Println("  3. (Optional) Configure Open WebUI or Claude Desktop with the generated API key.")
	} else {
		fmt.Println("Next steps:")
		fmt.Println("  1. Review config:  cat " + daemonPath)
		fmt.Println("  2. Build config:   bubblefish build")
		fmt.Println("  3. Start daemon:   bubblefish start")
		fmt.Println("  4. Health check:   bubblefish doctor")
	}
}

// generateKey returns a cryptographically random 32-byte hex-encoded key.
func generateKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish install: generate key: %v\n", err)
		os.Exit(1)
	}
	return hex.EncodeToString(b)
}

// writeConfigFile writes content to path. When force is false it skips existing
// files; when true it overwrites them. Uses 0600 permissions for sensitive config.
func writeConfigFile(path, content string, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			fmt.Printf("  skip (exists): %s\n", path)
			return nil
		}
	}
	fmt.Printf("  create: %s\n", path)
	return os.WriteFile(path, []byte(content), 0600)
}

// buildDaemonTOML generates daemon.toml content based on the deployment mode.
func buildDaemonTOML(mode, adminKey string) string {
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

	t := fmt.Sprintf(`# BubbleFish Nexus — daemon.toml
# Generated by bubblefish install (v%s)
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
path = "~/.bubblefish/Nexus/wal"
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
source_name = "mcp"
api_key = ""

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
log_file = "~/.bubblefish/Nexus/security.log"
`, version.Version, mode, port, bind, adminKey, logLevel, logFormat, mode,
		queueSize, walIntegrity, walEncryption, rateLimit, embeddingEnabled, webPort)

	return t
}

// writeDestination creates the appropriate destination TOML in destinations/.
func writeDestination(configDir, destType string, force bool) error {
	destDir := filepath.Join(configDir, "destinations")

	switch destType {
	case "sqlite":
		content := `# BubbleFish Nexus — SQLite destination
[destination]
name = "sqlite"
type = "sqlite"
db_path = "~/.bubblefish/Nexus/memories.db"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`
		return writeConfigFile(filepath.Join(destDir, "sqlite.toml"), content, force)

	case "postgres":
		content := `# BubbleFish Nexus — PostgreSQL destination
# Set NEXUS_POSTGRES_DSN or edit dsn below.
[destination]
name = "postgres"
type = "postgres"
dsn = "env:NEXUS_POSTGRES_DSN"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`
		return writeConfigFile(filepath.Join(destDir, "postgres.toml"), content, force)

	case "openbrain":
		content := `# BubbleFish Nexus — OpenBrain (Supabase) destination
# Set NEXUS_OPENBRAIN_URL and NEXUS_OPENBRAIN_KEY or edit below.
[destination]
name = "openbrain"
type = "openbrain"
url = "env:NEXUS_OPENBRAIN_URL"
api_key = "env:NEXUS_OPENBRAIN_KEY"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`
		return writeConfigFile(filepath.Join(destDir, "openbrain.toml"), content, force)

	default:
		return fmt.Errorf("unknown destination type %q (supported: sqlite, postgres, openbrain)", destType)
	}
}

// writeDefaultSource creates a default source TOML in sources/.
func writeDefaultSource(configDir, mode, destType, apiKey string, force bool) error {
	sourcesDir := filepath.Join(configDir, "sources")

	canRead := true
	canWrite := true
	rateLimit := 2000
	targetDest := "sqlite"

	switch destType {
	case "postgres":
		targetDest = "postgres"
	case "openbrain":
		targetDest = "openbrain"
	}

	switch mode {
	case "simple":
		rateLimit = 5000
	case "safe":
		rateLimit = 500
	}

	content := fmt.Sprintf(`# BubbleFish Nexus — default source
[source]
name = "default"
api_key = "%s"
namespace = "default"
can_read = %t
can_write = %t
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

[source.policy]
allowed_destinations = ["%s"]
max_results = 20
max_response_bytes = 16384
`, apiKey, canRead, canWrite, targetDest, rateLimit, targetDest)

	return writeConfigFile(filepath.Join(sourcesDir, "default.toml"), content, force)
}

// writeOpenWebUIProfile creates source config tuned for Open WebUI payload shape.
func writeOpenWebUIProfile(configDir string, force bool) error {
	sourcesDir := filepath.Join(configDir, "sources")
	content := `# BubbleFish Nexus — Open WebUI source profile
# Tuned for Open WebUI payload shape.
[source]
name = "openwebui"
api_key = "CHANGE_ME"
namespace = "openwebui"
can_read = true
can_write = true
target_destination = "sqlite"
default_actor_type = "user"
default_profile = "balanced"

[source.rate_limit]
requests_per_minute = 2000

[source.payload_limits]
max_bytes = 10485760

[source.mapping]
content = "messages.-1.content"
role = "messages.-1.role"
model = "model"

[source.idempotency]
enabled = true
dedup_window_seconds = 300

[source.policy]
allowed_destinations = ["sqlite"]
max_results = 20
max_response_bytes = 16384
`
	return writeConfigFile(filepath.Join(sourcesDir, "openwebui.toml"), content, force)
}

// writeOAuthTemplate generates example OAuth reverse-proxy configs.
func writeOAuthTemplate(configDir, template string, force bool) error {
	examplesDir := filepath.Join(configDir, "examples", "oauth")
	if err := os.MkdirAll(examplesDir, 0700); err != nil {
		return err
	}

	switch template {
	case "caddy":
		content := `# BubbleFish Nexus — Example Caddyfile with OIDC
# Replace YOUR_DOMAIN, YOUR_CLIENT_ID, YOUR_CLIENT_SECRET.

YOUR_DOMAIN {
    forward_auth * {
        uri /oauth2/auth
        header_up X-Forwarded-For {remote_host}
    }
    reverse_proxy localhost:8080
}
`
		return writeConfigFile(filepath.Join(examplesDir, "Caddyfile"), content, force)

	case "traefik":
		content := `# BubbleFish Nexus — Example Traefik config with OIDC
# Replace values as needed.

http:
  routers:
    nexus:
      rule: "Host(` + "`your.domain`" + `)"
      service: nexus
      middlewares:
        - oidc-auth

  services:
    nexus:
      loadBalancer:
        servers:
          - url: "http://localhost:8080"

  middlewares:
    oidc-auth:
      forwardAuth:
        address: "http://localhost:4181"
`
		return writeConfigFile(filepath.Join(examplesDir, "traefik.yml"), content, force)

	default:
		return fmt.Errorf("unknown oauth template %q (supported: caddy, traefik)", template)
	}
}
