# Security Policy

## Reporting a vulnerability

Do not open a public GitHub issue for security vulnerabilities.

Email **security@bubblefish.sh** with:
- A description of the vulnerability
- Steps to reproduce
- Your assessment of impact and exploitability
- Whether you want credit in the advisory (name or handle)

We respond within 48 hours. We'll confirm receipt, assess the report, and work with you on a disclosure timeline. For critical issues we aim to ship a fix within 7 days of confirmation.

We do not have a bug bounty program yet.

---

## Supported versions

| Version | Security support |
|---------|-----------------|
| 0.1.x   | Yes             |
| < 0.1   | No              |

---

## Security architecture

These are documented publicly so users can assess the model:

**Token class separation.** Three token namespaces: `bfn_mcp_` (MCP server), `bfn_data_` (HTTP data API), and admin tokens (admin endpoints). Cross-use returns 401. All comparisons use constant-time operations to prevent timing side-channels.

**MCP endpoint authentication.** The MCP server on port 7474 requires a `bfn_mcp_` bearer token on every request. There is no unauthenticated path to any tool endpoint. MCP binds to 127.0.0.1 only.

**WAL integrity.** CRC32 checksums on every WAL entry by default. Optional HMAC-SHA256 tamper detection. Optional AES-256-GCM encryption at rest. WAL files are written with restrictive permissions (0600).

**Config signing.** Optional HMAC-SHA256 signatures on compiled config files. When enabled, the daemon refuses to start or reload if any config file has been modified.

**File permissions.** All config files are 0600, all directories are 0700. Secret values are never logged at any level.

**Remote access.** For Cloudflare Tunnel deployments, Nexus validates the `bfn_mcp_` key at the application layer regardless of what Cloudflare passes through. The Cloudflare layer adds defense-in-depth, not the only layer.

**No shell execution.** Nexus does not execute external processes or shell commands at runtime. The attack surface is limited to the HTTP and MCP endpoints.

**Structured security events.** Auth failures, policy denials, rate limit hits, WAL tamper detection, and config signature failures are logged to a dedicated JSON Lines file for SIEM integration.

### Planned (v0.2)

**Credential gateway.** Planned for v0.2: Real provider keys will be stored in `daemon.toml` and never transmitted to clients. Clients will authenticate with synthetic `bfn_sk_` keys, and Nexus will substitute the real key on upstream calls. This feature is not implemented in v0.1.x.

---

## Optional hardening

| Feature | Config | What It Does |
|---------|--------|-------------|
| TLS/mTLS | `[daemon.tls]` | HTTPS for direct exposure. Client cert verification for mTLS. |
| WAL HMAC | `[daemon.wal.integrity] mode = "mac"` | Detects tampered WAL entries using HMAC-SHA256. |
| WAL Encryption | `[daemon.wal.encryption]` | AES-256-GCM encryption at rest for all WAL entries. |
| Config Signing | `[daemon.signing]` | Daemon refuses to load unsigned/modified config files. |
| Trusted Proxies | `[daemon.trusted_proxies]` | CIDR-based forwarded header parsing behind load balancers. |
| Deployment Modes | `mode = "safe"` | Preset requiring TLS, encryption, MAC integrity, lower rate limits. |

---

## Known limitations

- Single WAL encryption key. Key rotation ring is planned for a future version.
- No RBAC. Per-source API keys and policies provide isolation. Sufficient for personal and small-team use. RBAC is planned for Enterprise.
- The MCP server does not currently support mutual TLS (use a reverse proxy for mTLS).
- No clustering or HA. Single-node daemon. HA is planned for a future version.
