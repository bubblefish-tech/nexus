# Reference Architecture: Air-Gapped Environment

Maximum security posture for environments with no outbound network access.
Signed configs, MAC integrity, no embedding calls, no event sinks.

**Reference:** Tech Spec Section 13.6.

## Overview

| Component         | Choice                                |
|-------------------|---------------------------------------|
| Destination       | SQLite (local file)                   |
| TLS               | Disabled (localhost only, no network) |
| WAL Encryption    | AES-256-GCM (required)               |
| WAL Integrity     | HMAC-SHA256 (`mac` mode)              |
| Embedding         | Disabled (no outbound calls)          |
| Metrics           | Built-in dashboard only (local)       |
| Event Sinks       | Disabled                              |
| Config Signing    | Enabled                               |
| Mode              | `safe`                                |

## Quick Start

```bash
nexus install --dest sqlite --mode safe
# Generate signing and encryption keys:
nexus keygen --out ~/.nexus/Nexus/secrets/signing.key
nexus keygen --out ~/.nexus/Nexus/secrets/wal.key
nexus keygen --out ~/.nexus/Nexus/secrets/mac.key
# Sign the compiled config:
nexus sign-config
nexus start
```

## Sample Config

```toml
[daemon]
port       = 8000
bind       = "127.0.0.1"
admin_token = "file:~/.nexus/Nexus/secrets/admin.key"
log_level  = "info"
log_format = "text"
mode       = "safe"
queue_size = 128

[daemon.shutdown]
drain_timeout_seconds = 30

[daemon.wal]
max_segment_size_mb = 10

[daemon.wal.integrity]
mode         = "mac"
mac_key_file = "file:~/.nexus/Nexus/secrets/mac.key"

[daemon.wal.encryption]
enabled  = true
key_file = "file:~/.nexus/Nexus/secrets/wal.key"

[daemon.wal.watchdog]
interval_seconds      = 30
min_disk_bytes        = 104857600
max_append_latency_ms = 500

[daemon.rate_limit]
global_requests_per_minute = 60

[daemon.signing]
enabled  = true
key_file = "file:~/.nexus/Nexus/secrets/signing.key"

[daemon.web]
port         = 8081
require_auth = true
```

## Key Management

All keys are stored in `~/.nexus/Nexus/secrets/` with mode 0600.
The secrets directory itself is mode 0700.

| Key File       | Purpose                          |
|----------------|----------------------------------|
| `admin.key`    | Admin API authentication         |
| `wal.key`      | AES-256-GCM WAL encryption       |
| `mac.key`      | HMAC-SHA256 WAL integrity         |
| `signing.key`  | Config signature verification     |

Keys must be generated on the air-gapped machine itself. Never transfer
private key material across network boundaries.

## Config Signing

When `daemon.signing.enabled = true`, Nexus verifies compiled config
signatures at startup and on hot reload. Modified configs are rejected
with a `config_signature_invalid` security event.

```bash
# After any config change:
nexus sign-config
nexus start   # or send SIGHUP to trigger hot reload
```

## When to Use

- Classified or regulated environments.
- Machines with no internet access.
- Environments where data exfiltration is the primary threat.
- Compliance requirements mandate integrity verification.

## Security Notes

- No outbound network calls of any kind.
- WAL entries are encrypted (AES-256-GCM) and integrity-checked (HMAC).
- Tampered WAL entries are detected and skipped with a WARN log.
- Config changes require re-signing; unsigned changes are rejected.
- All file permissions enforced: 0600 for files, 0700 for directories.
- Embedding is disabled — no data leaves the machine for vectorization.
