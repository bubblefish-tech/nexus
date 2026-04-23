# Reference Architecture: Home Lab

Multi-source setup with Postgres, observability, and TLS via reverse proxy.
Suitable for power users running multiple AI clients on a home network.

**Reference:** Tech Spec Section 13.6.

## Overview

| Component         | Choice                                 |
|-------------------|----------------------------------------|
| Destination       | Postgres + pgvector                    |
| TLS               | Caddy/nginx reverse proxy with TLS     |
| WAL Encryption    | Optional (recommended)                 |
| WAL Integrity     | CRC32                                  |
| Embedding         | Optional (local Ollama or remote API)  |
| Metrics           | Prometheus + Grafana                   |
| Event Sink        | Local webhook                          |
| Mode              | `balanced`                             |

## Quick Start

```bash
nexus install --dest postgres --mode balanced
# Edit ~/.nexus/Nexus/config.toml with your Postgres DSN.
nexus start
```

## Sample Config

```toml
[daemon]
port       = 8000
bind       = "127.0.0.1"
admin_token = "env:BUBBLEFISH_ADMIN_TOKEN"
log_level  = "info"
log_format = "json"
mode       = "balanced"
queue_size = 512

[daemon.shutdown]
drain_timeout_seconds = 30

[daemon.wal]
max_segment_size_mb = 10

[daemon.wal.integrity]
mode = "crc32"

[daemon.wal.encryption]
enabled  = true
key_file = "file:~/.nexus/Nexus/secrets/wal.key"

[daemon.rate_limit]
global_requests_per_minute = 300

[daemon.web]
port         = 8081
require_auth = true

[daemon.events]
enabled     = true
max_inflight = 10

[[daemon.events.sinks]]
name            = "homelab-webhook"
url             = "http://127.0.0.1:9090/hooks/nexus"
timeout_seconds = 5
max_retries     = 3
content         = "summary"
```

## Reverse Proxy (Caddy)

```
nexus.local {
    reverse_proxy 127.0.0.1:8000
    tls internal
}

nexus-dashboard.local {
    reverse_proxy 127.0.0.1:8081
    tls internal
}
```

Nexus binds to localhost only. The reverse proxy terminates TLS and
forwards traffic. This keeps Nexus simple while providing encrypted
transport for LAN clients.

## Postgres Destination

```toml
[destination]
name   = "postgres-local"
type   = "postgres"
dsn    = "env:BUBBLEFISH_POSTGRES_DSN"
```

Requires the `pgvector` extension for semantic search:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

## Per-Source Policies

Define separate source files in `~/.nexus/Nexus/sources/` for each
AI client. Each source gets its own API key, rate limits, and policy
constraints.

## Monitoring

Prometheus scrapes `/metrics` on the daemon port. Import the provided
Grafana dashboard JSON for queue depth, WAL pending, cache hit rates,
and auth failure counters.

## When to Use

- Multiple AI clients on a home network.
- Need for durable storage (Postgres replication/backups).
- Want observability (Grafana dashboards, alerting).
- Comfortable managing TLS certificates and reverse proxies.

## Security Notes

- Nexus binds to localhost; only the reverse proxy is exposed.
- Per-source API keys isolate clients.
- WAL encryption protects data at rest.
- Event sinks run over localhost — no secrets traverse the network.
