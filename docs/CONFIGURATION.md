# Configuration Reference

All Nexus configuration lives in TOML files under `~/.bubblefish/Nexus/` (or the path set by `BUBBLEFISH_HOME`). Source config changes are hot-reloaded. Destination changes require a restart.

---

## Directory layout

```
~/.bubblefish/Nexus/
  daemon.toml              # Main daemon config
  sources/
    default.toml           # One file per AI client
    claude.toml
    ollama.toml
  destinations/
    sqlite.toml            # One file per backend
    postgres.toml
  compiled/                # Auto-generated JSON (do not edit)
  wal/                     # Write-Ahead Log segments
  logs/
    bubblefish.log
    security-events.jsonl  # Security event log
  examples/
    integrations/          # Pre-built, security-reviewed configs
    oauth/                 # OAuth reverse proxy templates
  backups/                 # Backup snapshots
```

File permissions: all config files are 0600, all directories are 0700.

---

## daemon.toml — complete reference

```toml
# daemon.toml

[daemon]
# HTTP API port
port = 8080

# Bind address. Never set to 0.0.0.0 without TLS.
bind = "127.0.0.1"

# Admin API token. Use env: or file: reference.
admin_token = "env:ADMIN_TOKEN"

# Log level: debug, info, warn, error
log_level = "info"

# Log format: json or text
log_format = "json"

# Deployment mode: safe, balanced, or fast
# Sets defaults for TLS, encryption, rate limits. Explicit settings override.
mode = "balanced"

# Maximum queue depth. Exceeding returns 429 queue_full (data is still in WAL).
queue_size = 10000

# Total shutdown budget in seconds. Split across HTTP, queue drain, WAL close.
drain_timeout_seconds = 30


[daemon.wal]
# WAL directory path. Supports ~ and $BUBBLEFISH_HOME.
path = "~/.bubblefish/Nexus/wal"

# WAL segment size before rotation.
max_segment_size_mb = 50


[daemon.wal.integrity]
# Integrity mode: crc32 (default, detects accidental corruption)
# or mac (adds HMAC-SHA256 for tamper detection)
mode = "crc32"

# Required when mode = "mac". 32-byte key file.
# mac_key_file = "file:/path/to/mac.key"


[daemon.wal.encryption]
# AES-256-GCM encryption at rest for all WAL entries.
enabled = false

# 32-byte key file. Required when enabled = true.
# key_file = "file:/path/to/wal.key"


[daemon.mcp]
# MCP server for Claude Desktop, Cursor, and other MCP clients.
enabled = true
port = 7474
bind = "127.0.0.1"
source_name = "mcp"
api_key = "env:MCP_API_KEY"


[daemon.tls]
# Optional TLS for direct exposure. Recommended if not using a reverse proxy.
enabled = false
# cert_file = "file:/path/to/cert.pem"
# key_file = "file:/path/to/key.pem"
# min_version = "1.2"
# max_version = "1.3"
# client_ca_file = ""         # For mTLS
# client_auth = "no_client_cert"


[daemon.trusted_proxies]
# CIDR-based forwarded header parsing behind load balancers.
cidrs = ["127.0.0.1/32", "::1/128"]
forwarded_headers = ["X-Forwarded-For", "X-Real-IP"]
# Never add 0.0.0.0/0 — bubblefish lint warns about this.


[daemon.signing]
# Config signing. Daemon refuses to load unsigned/modified config files.
enabled = false
# key_file = "file:/path/to/signing.key"


[daemon.embedding]
# Embedding provider for semantic search (Stage 4).
# Without this, Nexus uses structured lookup (Stage 3) only.
enabled = false
# provider = "ollama"
# url = "http://localhost:11434"
# model = "nomic-embed-text"
# dimensions = 768


[daemon.events]
# Optional async webhook notifications. Never blocks the write path.
enabled = false
# [[daemon.events.sinks]]
# name = "audit"
# url = "https://your-siem.example.com/webhook"
# timeout_seconds = 5
# content = "summary"    # or "full" for complete memory content


[security_events]
# Dedicated security event log for SIEM integration.
enabled = true
log_file = "~/.bubblefish/Nexus/logs/security-events.jsonl"


[retrieval]
# Temporal decay reranking. Newer facts outrank older contradictions.
time_decay = true
half_life_days = 7
decay_mode = "exponential"  # or "step" for hard cutoffs
```

---

## Source configuration

Each source is a TOML file in `sources/`. One file per AI client.

```toml
# sources/claude.toml

[source]
name = "claude"
api_key = "env:CLAUDE_SOURCE_KEY"
can_read = true
can_write = true
target_destination = "sqlite"

# Rate limits
rate_limit_rpm = 2000
max_payload_bytes = 65536

# Provenance defaults (overridable per request via headers)
default_actor_type = "agent"
default_actor_id = "claude-desktop"

# Field visibility (what this source can see in responses)
# include_fields = ["content", "subject", "timestamp", "actor_type"]

# Allowed retrieval profiles
# allowed_profiles = ["fast", "balanced", "deep"]

[source.mapping]
# Field mapping from client payload to canonical format.
content = "message.content"
role = "message.role"
model = "model"
```

---

## Destination configuration

Each destination is a TOML file in `destinations/`.

```toml
# destinations/sqlite.toml

[destination]
name = "sqlite"
type = "sqlite"
path = "~/.bubblefish/Nexus/data/nexus.db"
```

```toml
# destinations/postgres.toml

[destination]
name = "postgres"
type = "postgres"
connection_string = "env:POSTGRES_URL"
# pgvector_enabled = true
```

---

## Secrets

Three ways to provide API keys and secrets:

```toml
api_key = "env:CLAUDE_KEY"              # Environment variable
api_key = "file:/run/secrets/claude"    # Docker Secrets / Kubernetes Secrets
api_key = "my-dev-key"                  # Literal (dev only)
```

Secret values are never logged at any level.

---

## Deployment modes

| Mode | TLS | WAL Encryption | Integrity | Rate Limits | Use Case |
|------|-----|---------------|-----------|-------------|----------|
| `balanced` | Off | Off | CRC32 | 2000/min | Default. Personal use, home server. |
| `safe` | Required | Required | HMAC-SHA256 | 500/min | Production, sensitive data, remote access. |
| `fast` | Off | Off | CRC32 | 10000/min | Local-only benchmarking, low latency. |

Explicit settings always override mode defaults.

---

## Validate your config

```bash
bubblefish lint    # Config lint: warns about dangerous binds, missing idempotency, etc.
bubblefish doctor  # Checks daemon, destinations, disk space, config validity
```

---

## Reload without restart

Source config changes are hot-reloaded automatically (file watcher). Destination changes require a full restart.

For manual reload:

```bash
# Reload via admin API
curl -X POST http://localhost:8080/api/reload \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Reload picks up changes to source configs, log level, and presenter settings. Port changes require a restart.

---

## Planned for v0.2

> **This section describes planned functionality. It is not implemented in v0.1.x.**

### Credential gateway

A `[credentials]` configuration section will allow Nexus to act as a credential proxy. AI clients will authenticate with synthetic `bfn_sk_` keys, and Nexus will substitute the real provider key on upstream calls.

```toml
# daemon.toml (planned — not yet implemented)
[credentials]
  [[credentials.mappings]]
  synthetic_prefix = "bfn_sk_"
  provider = "openai"
  real_key = "env:OPENAI_API_KEY"
  allowed_models = ["gpt-4o", "gpt-4o-mini"]
  rate_limit_rpm = 100
```

This will enable per-client rate limiting, model allowlists, and key isolation without exposing real provider credentials to AI clients.
