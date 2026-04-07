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
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/BubbleFish-Nexus/internal/version"
)

// promptFunc reads a line of user input from the given reader. The prompt
// string is written to w before reading. Tests replace this with canned input.
type promptFunc func(w io.Writer, r io.Reader, prompt string) (string, error)

// stdinPrompt is the default promptFunc that reads from stdin.
func stdinPrompt(w io.Writer, r io.Reader, prompt string) (string, error) {
	_, _ = fmt.Fprint(w, prompt)
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text()), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", io.EOF
}

// installOptions collects all resolved install parameters so the core logic
// (doInstall) can be tested without os.Exit or real stdin.
type installOptions struct {
	dest          string
	mode          string
	profile       string
	oauthTemplate string
	force         bool
	configDir     string
	prompt        promptFunc
	stdin         io.Reader
	stdout        io.Writer
	stderr        io.Writer
}

// runInstall executes the `bubblefish install` command.
//
// It creates the config directory tree (~/.bubblefish/Nexus/) with daemon.toml,
// sources/, and destinations/ populated according to the selected --dest and
// --mode flags. It NEVER completes silently -- always prints the config path
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
	homeFlag := fs.String("home", "", "override config directory location (also configurable via BUBBLEFISH_HOME)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish install: %v\n", err)
		os.Exit(1)
	}

	configDir, err := resolveInstallHome(*homeFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish install: %v\n", err)
		os.Exit(1)
	}

	opts := installOptions{
		dest:          *dest,
		mode:          *mode,
		profile:       *profile,
		oauthTemplate: *oauthTemplate,
		force:         *force,
		configDir:     configDir,
		prompt:        stdinPrompt,
		stdin:         os.Stdin,
		stdout:        os.Stdout,
		stderr:        os.Stderr,
	}

	if err := doInstall(opts); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish install: %v\n", err)
		os.Exit(1)
	}
}

// resolveInstallHome determines the config directory using the resolution order:
// --home flag > BUBBLEFISH_HOME env var > default (~/.bubblefish/Nexus).
func resolveInstallHome(homeFlag string) (string, error) {
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
		dir = filepath.Join(home, ".bubblefish", "Nexus")
	}
	return filepath.Abs(dir)
}

// doInstall is the testable core of runInstall. It returns an error instead
// of calling os.Exit.
func doInstall(opts installOptions) error {
	configDir := opts.configDir
	w := opts.stdout

	// Refuse if config already exists unless --force.
	daemonPath := filepath.Join(configDir, "daemon.toml")
	if !opts.force {
		if _, err := os.Stat(daemonPath); err == nil {
			return fmt.Errorf("config already exists at %s -- use --force to overwrite", configDir)
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
			return fmt.Errorf("create directory %q: %v", d, err)
		}
	}

	// Generate random API keys for admin, default source, and MCP.
	adminKey := generateKey("bfn_admin_")
	sourceKey := generateKey("bfn_data_")
	mcpKey := generateKey("bfn_mcp_")

	// Write daemon.toml based on mode.
	// MCP source_name is always "default" so it routes through the default
	// source config created below. The generated mcpKey is written into
	// daemon.toml and printed in the install summary.
	daemonTOML, bindAddr := buildDaemonTOML(configDir, opts.mode, adminKey, mcpKey)
	if err := writeConfigFile(daemonPath, daemonTOML, opts.force); err != nil {
		return fmt.Errorf("write daemon.toml: %v", err)
	}

	// For postgres and openbrain, prompt for connection details and write
	// the destination config with user-provided values.
	destType := opts.dest
	switch destType {
	case "postgres":
		dsn, err := opts.prompt(w, opts.stdin, "PostgreSQL connection string (e.g. postgres://user:pass@host:5432/db): ")
		if err != nil {
			return fmt.Errorf("read postgres DSN: %v", err)
		}
		if dsn == "" {
			dsn = "env:NEXUS_POSTGRES_DSN"
		}
		if err := writePostgresDestination(configDir, dsn, opts.force); err != nil {
			return fmt.Errorf("write destination: %v", err)
		}
		// Run doctor connectivity check.
		if !strings.HasPrefix(dsn, "env:") && !strings.HasPrefix(dsn, "file:") {
			checkPostgresConnectivity(w, dsn)
		}

	case "openbrain":
		url, err := opts.prompt(w, opts.stdin, "Supabase project URL (e.g. https://xyzproject.supabase.co): ")
		if err != nil {
			return fmt.Errorf("read openbrain URL: %v", err)
		}
		if url == "" {
			url = "env:NEXUS_OPENBRAIN_URL"
		}
		key, err := opts.prompt(w, opts.stdin, "Supabase service role key: ")
		if err != nil {
			return fmt.Errorf("read openbrain key: %v", err)
		}
		if key == "" {
			key = "env:NEXUS_OPENBRAIN_KEY"
		}
		if err := writeOpenBrainDestination(configDir, url, key, opts.force); err != nil {
			return fmt.Errorf("write destination: %v", err)
		}
		// Run doctor connectivity check.
		if !strings.HasPrefix(url, "env:") && !strings.HasPrefix(url, "file:") &&
			!strings.HasPrefix(key, "env:") && !strings.HasPrefix(key, "file:") {
			checkOpenBrainConnectivity(w, url, key)
		}

	default:
		if err := writeDestination(configDir, destType, opts.force); err != nil {
			return fmt.Errorf("write destination: %v", err)
		}
	}

	// Write default source config based on mode.
	if err := writeDefaultSource(configDir, opts.mode, destType, sourceKey, opts.force); err != nil {
		return fmt.Errorf("write source: %v", err)
	}

	// Handle --profile flag.
	if opts.profile == "openwebui" {
		if err := writeOpenWebUIProfile(configDir, opts.force); err != nil {
			return fmt.Errorf("write openwebui profile: %v", err)
		}
		if err := writeOpenWebUIProviderExample(configDir, opts.force); err != nil {
			return fmt.Errorf("write openwebui provider example: %v", err)
		}
	}

	// Handle --oauth-template flag.
	if opts.oauthTemplate != "" {
		if err := writeOAuthTemplate(configDir, opts.oauthTemplate, opts.force); err != nil {
			return fmt.Errorf("write oauth template: %v", err)
		}
	}

	// Print results -- NEVER silent.
	_, _ = fmt.Fprintf(w, "bubblefish install: ok -- v%s\n", version.Version)
	_, _ = fmt.Fprintf(w, "  config directory: %s\n", configDir)
	_, _ = fmt.Fprintf(w, "  mode:            %s\n", opts.mode)
	_, _ = fmt.Fprintf(w, "  destination:     %s\n", destType)
	_, _ = fmt.Fprintf(w, "  bind address:    %s\n", bindAddr)
	_, _ = fmt.Fprintf(w, "  admin token:     %s\n", adminKey)
	_, _ = fmt.Fprintf(w, "  source API key:  %s\n", sourceKey)
	_, _ = fmt.Fprintf(w, "  MCP API key:     %s\n", mcpKey)
	_, _ = fmt.Fprintln(w)

	// Print next steps -- Simple Mode walks through start -> write -> read -> integrate.
	if opts.mode == "simple" {
		_, _ = fmt.Fprintln(w, "Next steps:")
		_, _ = fmt.Fprintln(w, "  1. Start the daemon:")
		_, _ = fmt.Fprintln(w, "     bubblefish start")
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "  2. Write your first memory:")
		_, _ = fmt.Fprintf(w, "     curl.exe -X POST http://%s/inbound/default \\\n", bindAddr)
		_, _ = fmt.Fprintf(w, "       -H \"Authorization: Bearer %s\" \\\n", sourceKey)
		_, _ = fmt.Fprintln(w, "       -H \"Content-Type: application/json\" \\")
		_, _ = fmt.Fprintln(w, "       -d \"{\\\"message\\\":{\\\"content\\\":\\\"My first BubbleFish memory\\\",\\\"role\\\":\\\"user\\\"},\\\"model\\\":\\\"test\\\"}\"")
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "  3. Read it back:")
		_, _ = fmt.Fprintf(w, "     curl.exe http://%s/query/%s \\\n", bindAddr, destType)
		_, _ = fmt.Fprintf(w, "       -H \"Authorization: Bearer %s\"\n", sourceKey)
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "  4. Connect Claude Desktop via the BubbleFish Nexus extension.")
		_, _ = fmt.Fprintf(w, "     MCP API key: %s\n", mcpKey)
	} else {
		_, _ = fmt.Fprintln(w, "Next steps:")
		_, _ = fmt.Fprintln(w, "  1. Review config:  cat "+daemonPath)
		_, _ = fmt.Fprintln(w, "  2. Build config:   bubblefish build")
		_, _ = fmt.Fprintln(w, "  3. Start daemon:   bubblefish start")
		_, _ = fmt.Fprintln(w, "  4. Health check:   bubblefish doctor")
	}
	return nil
}

// generateKey returns a cryptographically random 32-byte hex-encoded key
// prefixed with the given class identifier (e.g. "bfn_admin_", "bfn_data_").
func generateKey(prefix string) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish install: generate key: %v\n", err)
		os.Exit(1)
	}
	return prefix + hex.EncodeToString(b)
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
// It returns the TOML content and the resolved bind address (e.g. "127.0.0.1:8080").
// Paths inside the TOML (WAL, security log) are templated against configDir.
// mcpKey is the generated bfn_mcp_ token written into [daemon.mcp] api_key.
func buildDaemonTOML(configDir, mode, adminKey, mcpKey string) (string, string) {
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

	return t, fmt.Sprintf("%s:%d", bind, port)
}

// writeDestination creates the appropriate destination TOML in destinations/.
// For sqlite, it writes a self-contained config. For postgres and openbrain,
// use writePostgresDestination or writeOpenBrainDestination (which accept
// prompted values) instead.
func writeDestination(configDir, destType string, force bool) error {
	destDir := filepath.Join(configDir, "destinations")

	switch destType {
	case "sqlite":
		dbPath := filepath.ToSlash(filepath.Join(configDir, "memories.db"))
		content := fmt.Sprintf(`# BubbleFish Nexus -- SQLite destination
[destination]
name = "sqlite"
type = "sqlite"
db_path = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dbPath)
		return writeConfigFile(filepath.Join(destDir, "sqlite.toml"), content, force)

	default:
		return fmt.Errorf("unknown destination type %q (supported: sqlite, postgres, openbrain)", destType)
	}
}

// writePostgresDestination writes postgres.toml with the user-provided DSN.
// Reference: Tech Spec Section 2.2.2.
func writePostgresDestination(configDir, dsn string, force bool) error {
	destDir := filepath.Join(configDir, "destinations")
	content := fmt.Sprintf(`# BubbleFish Nexus -- PostgreSQL destination
[destination]
name = "postgres"
type = "postgres"
dsn = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, dsn)
	return writeConfigFile(filepath.Join(destDir, "postgres.toml"), content, force)
}

// writeOpenBrainDestination writes openbrain.toml with the user-provided URL
// and service role key.
// Reference: Tech Spec Section 2.2.2.
func writeOpenBrainDestination(configDir, url, apiKey string, force bool) error {
	destDir := filepath.Join(configDir, "destinations")
	content := fmt.Sprintf(`# BubbleFish Nexus -- OpenBrain (Supabase) destination
[destination]
name = "openbrain"
type = "openbrain"
url = "%s"
api_key = "%s"

[destination.decay]
half_life_days = 7.0
decay_mode = "exponential"
`, url, apiKey)
	return writeConfigFile(filepath.Join(destDir, "openbrain.toml"), content, force)
}

// checkPostgresConnectivity attempts to connect to a PostgreSQL database and
// reports success or failure. It never blocks install on failure -- only warns.
// Reference: Tech Spec Section 2.2.2 (doctor connectivity check).
func checkPostgresConnectivity(w io.Writer, dsn string) {
	_, _ = fmt.Fprintf(w, "  doctor: checking PostgreSQL connectivity...\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		_, _ = fmt.Fprintf(w, "  doctor: PostgreSQL UNREACHABLE -- %v\n", err)
		return
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Debug("doctor: close postgres db", "error", err)
		}
	}()

	if err := db.PingContext(ctx); err != nil {
		_, _ = fmt.Fprintf(w, "  doctor: PostgreSQL UNREACHABLE -- %v\n", err)
		return
	}
	_, _ = fmt.Fprintf(w, "  doctor: PostgreSQL OK\n")
}

// checkOpenBrainConnectivity attempts an HTTP HEAD against the Supabase REST
// endpoint and reports success or failure. Never blocks install on failure.
// Reference: Tech Spec Section 2.2.2 (doctor connectivity check).
func checkOpenBrainConnectivity(w io.Writer, baseURL, apiKey string) {
	_, _ = fmt.Fprintf(w, "  doctor: checking OpenBrain/Supabase connectivity...\n")

	client := &http.Client{Timeout: 5 * time.Second}
	endpoint := strings.TrimRight(baseURL, "/") + "/rest/v1/"

	req, err := http.NewRequest(http.MethodHead, endpoint, nil)
	if err != nil {
		_, _ = fmt.Fprintf(w, "  doctor: OpenBrain UNREACHABLE -- %v\n", err)
		return
	}
	req.Header.Set("apikey", apiKey)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		_, _ = fmt.Fprintf(w, "  doctor: OpenBrain UNREACHABLE -- %v\n", err)
		return
	}
	_ = resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		_, _ = fmt.Fprintf(w, "  doctor: OpenBrain OK (HTTP %d)\n", resp.StatusCode)
	} else {
		_, _ = fmt.Fprintf(w, "  doctor: OpenBrain WARNING -- HTTP %d (check URL and key)\n", resp.StatusCode)
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

	content := fmt.Sprintf(`# BubbleFish Nexus -- default source
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

[source.mapping]
content = "message.content"
role    = "message.role"
model   = "model"

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
	content := `# BubbleFish Nexus -- Open WebUI source profile
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

// writeOpenWebUIProviderExample drops an example provider JSON file into
// examples/ so users can see how Open WebUI configures its external memory
// provider. Reference: Tech Spec Section 2.2.2.
func writeOpenWebUIProviderExample(configDir string, force bool) error {
	examplesDir := filepath.Join(configDir, "examples")
	if err := os.MkdirAll(examplesDir, 0700); err != nil {
		return err
	}

	content := `{
  "name": "BubbleFish Nexus",
  "description": "AI memory gateway -- persists conversation memories via Nexus.",
  "base_url": "http://localhost:8080",
  "api_key": "CHANGE_ME",
  "endpoints": {
    "write": {
      "method": "POST",
      "path": "/inbound/openwebui",
      "content_type": "application/json"
    },
    "read": {
      "method": "POST",
      "path": "/retrieve",
      "content_type": "application/json"
    }
  },
  "notes": "Replace api_key with the openwebui source API key from sources/openwebui.toml."
}
`
	return writeConfigFile(filepath.Join(examplesDir, "openwebui-provider.json"), content, force)
}

// writeOAuthTemplate generates example OAuth reverse-proxy configs.
func writeOAuthTemplate(configDir, template string, force bool) error {
	examplesDir := filepath.Join(configDir, "examples", "oauth")
	if err := os.MkdirAll(examplesDir, 0700); err != nil {
		return err
	}

	switch template {
	case "caddy":
		content := `# BubbleFish Nexus -- Example Caddyfile with OIDC
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
		content := `# BubbleFish Nexus -- Example Traefik config with OIDC
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
