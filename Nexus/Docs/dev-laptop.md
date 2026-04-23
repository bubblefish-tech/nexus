# Reference Architecture: Single Developer Laptop

Minimal setup for personal use on a single machine. No network exposure,
no encryption overhead, no external dependencies.

**Reference:** Tech Spec Section 13.6.

## Overview

| Component         | Choice                        |
|-------------------|-------------------------------|
| Destination       | SQLite (local file)           |
| TLS               | Disabled (localhost only)     |
| WAL Encryption    | Disabled                      |
| WAL Integrity     | CRC32                         |
| Embedding         | Disabled                      |
| Metrics           | Built-in dashboard only       |
| Mode              | `simple`                      |

## Quick Start

```bash
nexus install --mode simple
nexus start
```

This creates a default config at `~/.nexus/Nexus/` with:

- SQLite destination at `~/.nexus/Nexus/data/nexus.db`
- WAL at `~/.nexus/Nexus/wal/`
- Daemon on `127.0.0.1:8000`
- Dashboard on `127.0.0.1:8081`
- Auto-generated admin token (printed once at install)

## Sample Config

```toml
[daemon]
port       = 8000
bind       = "127.0.0.1"
admin_token = "file:~/.nexus/Nexus/secrets/admin.key"
log_level  = "info"
mode       = "simple"
queue_size = 128

[daemon.wal]
max_segment_size_mb = 10

[daemon.wal.integrity]
mode = "crc32"

[daemon.rate_limit]
global_requests_per_minute = 120

[daemon.web]
port         = 8081
require_auth = true
```

## When to Use

- Personal projects, learning, experimentation.
- Single AI client (Claude Desktop, Claude Code, etc.).
- No need for TLS (traffic never leaves localhost).
- Acceptable to lose data if the disk fails (no replication).

## Security Notes

- Bind is `127.0.0.1` — never exposed to the network.
- File permissions are 0600/0700 by default.
- API keys stored via `file:` references, not literals.
- No embedding calls leave the machine.
