# Changelog

All notable changes to BubbleFish Nexus are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## v0.1.3 — OAuth 2.1 Hardening

### Added
- `internal/oauth/cors.go` — shared CORS helper for all OAuth endpoints,
  enabling browser-based OAuth clients (Claude Web UI, SPAs).
- `docs/OAUTH_KNOWN_LIMITATIONS.md` — honest documentation of per-client
  source mapping deferral, JWT revocation window, and single-tenant
  assumption.
- `scopes_supported` field in OAuth server metadata response.
- `OPTIONS` preflight handling on all `/oauth/*` and
  `/.well-known/oauth-*` endpoints.

### Changed
- `internal/oauth/authorize.go` — consent page rendering migrated from
  `fmt.Fprintf` with raw `%s` to `html/template`, HTML-escaping all
  untrusted query parameters. Consent page footer now reads the version
  from `internal/version` instead of a hardcoded string.
- `handleAllow` and `handleDeny` now strictly validate `state` and
  `code_challenge` presence, matching `handleAuthorize` strictness.

### Security
- **HIGH — Consent page XSS fixed.** Untrusted OAuth query parameters
  (`state`, `redirect_uri`, `code_challenge`, `scope`) were rendered into
  the consent page via `fmt.Fprintf` with no HTML escaping. A malicious
  OAuth flow with a crafted `state` parameter could execute arbitrary
  JavaScript in the context of the consent page origin. Fixed by
  migrating to `html/template`.
- **MEDIUM — CORS on OAuth endpoints.** OAuth endpoints did not emit
  `Access-Control-Allow-Origin` headers, silently blocking browser-based
  OAuth clients. Fixed with a shared CORS helper applied to all
  handlers.

### Unchanged
- `bfn_mcp_` static bearer auth — byte-identical behavior.
- `?key=` query parameter fallback — byte-identical behavior.
- MCP JSON-RPC protocol, tools, CORS on `/mcp`, SSE transport, stdio
  bridge — all unchanged.

## [0.1.2] — 2026-04-07

### Added

- **OAuth 2.1 Authorization Server** — full RFC 8414 compliant OAuth server enabling ChatGPT and other OAuth-only MCP clients to connect to Nexus
- **4 new HTTP endpoints** — `/.well-known/oauth-authorization-server`, `/oauth/authorize`, `/oauth/token`, `/oauth/jwks`
- **RSA-2048 key management** — auto-generated on first start, PEM storage with 0600 permissions
- **JWT access tokens** — RS256 signed, 1hr TTL, `bfn_source` claim for source mapping
- **PKCE (S256)** — proof key for code exchange; `plain` method rejected
- **Self-contained consent page** — branded HTML with no external dependencies
- **In-memory auth code store** — thread-safe, 5-minute TTL, single-use, automatic purge
- **MCP authenticate() extension** — JWT acceptance alongside existing `bfn_mcp_` static keys (backward compatible)
- **`[daemon.oauth]` config block** — full configuration in daemon.toml with client registration
- **`bubblefish install --oauth-issuer`** — install flag for OAuth setup
- **Doctor OAuth checks** — issuer_url, key file, client registration, HTTPS validation

### Security

- PKCE verification uses `subtle.ConstantTimeCompare` — never `==`
- Auth codes are single-use — deleted before JWT issuance
- `redirect_uri` mismatch returns 400, never redirects
- `private_key_file` as plain literal refused at startup (`SCHEMA_ERROR`)
- Private key never appears in logs, responses, or error messages

## [0.1.0] — 2026-04-06

Initial public release.

### Pre-Launch Polish (v0.1.0)

**Test reliability:**
- Replaced timing-based constant-time auth tests with structural verification (eliminated 3 flaky tests sensitive to OS scheduler noise)
- Converted firewall benchmark assertion to a proper Go benchmark with separate correctness test
- Made TestThroughputStability opt-in via NEXUS_RUN_FLAKY=1 environment variable

**Reliability fixes:**
- Windows WAL rename race in MarkDelivered was already fixed via fsutil.RobustRename (retry-on-sharing-violation logic with exponential backoff)

**Performance:**
- Added SQLite indexes for namespace/destination/timestamp and subject/timestamp query patterns
- Cached firewall string sets at config-load time, removing per-request allocation from the read hot path

**Code clarity:**
- Documented the durability contract on WAL fsync (why batched fsyncs must not be used)
- Documented the bounded-key invariant on the rate limiter map
- Documented the O(N) audit reader characteristic and its bounds
- Documented the MarkDelivered hot-path warning to use MarkDeliveredBatch
- Documented the WAL 10MB scanner buffer allocation rationale

### Added

- **Core daemon** with 3-stage graceful shutdown (HTTP stop, queue drain, WAL close)
- **Write-Ahead Log (WAL)** with CRC32 checksums, fsync durability, and automatic rotation at 50 MB
- **WAL HMAC integrity** — optional HMAC-SHA256 tamper detection
- **WAL encryption** — optional AES-256-GCM with per-entry nonce
- **Non-blocking message queue** with configurable workers and exponential backoff retry
- **Idempotency store** — in-memory deduplication rebuilt from WAL on startup
- **6-stage retrieval cascade** — policy, exact cache, semantic cache, temporal decay, embedding search, projection
- **Retrieval profiles** — `fast`, `balanced`, `deep` with per-source stage toggles
- **Tiered temporal decay** — per-destination/collection exponential and step modes
- **Exact cache** — SHA256-keyed LRU with watermark invalidation
- **Semantic cache** — embedding-based similarity with configurable threshold
- **Zero-dependency LRU** — Go generics, `map` + `container/list`
- **Projection engine** — field allowlists, metadata stripping, pagination with cursors
- **Destination adapters** — SQLite, PostgreSQL, Supabase
- **Policy engine** — compiled policies with zero-allocation runtime lookup
- **Config signing** — HMAC-SHA256 signatures for signed-mode deployments (`bubblefish sign-config`)
- **Constant-time auth** — `subtle.ConstantTimeCompare` for all token validation
- **Admin vs data token separation** — wrong token class returns 401
- **JWT/JWKS authentication** — advanced auth pattern with claim mapping and audience validation
- **Provenance fields** — `actor_type` (user/agent/system) + `actor_id` on every write
- **MCP server** — JSON-RPC 2.0 (`nexus_write`, `nexus_search`, `nexus_status`) for Claude Desktop and Cursor
- **Web dashboard** — admin-authenticated UI with security tab, metrics, and pipeline visualization
- **Security tab** — source policies, auth failure history, lint findings in dashboard
- **Live pipeline visualization** — lossy event channel, never blocks hot paths
- **Structured security events** — dedicated security event log for SIEM integration
- **Security metrics** — auth failures, policy denials, rate limits, admin call counts
- **Prometheus metrics** — daemon up, queue depth, request duration, cache hit/miss rates
- **Health doctor** — disk space, database connectivity, embedding provider checks
- **Simple mode install** — `bubblefish install --mode simple` for zero-friction setup
- **Install profiles** — Open WebUI, PostgreSQL, OpenBrain starter configs
- **`bubblefish dev`** — daemon with debug logging and auto-reload
- **`bubblefish build`** — compile policies and validate configuration
- **`bubblefish lint`** — check configuration for dangerous or suboptimal settings
- **`bubblefish backup`** — create and restore backups of config, WAL, and database
- **`bubblefish bench`** — throughput, latency, and retrieval evaluation benchmarks
- **`bubblefish demo`** — reliability demo with 50-memory crash-recovery scenario
- **Hot reload** — source config changes applied without restart
- **Consistency assertions** — background WAL-to-destination consistency checks
- **WAL health watchdog** — background disk/permissions/latency monitoring
- **Blessed integration configs** — pre-built templates for Claude Code, Claude Desktop, Open WebUI, Perplexity
- **Reference architectures** — dev laptop, home lab, air-gapped deployment docs
- **TLS/mTLS support** — optional TLS with configurable cert, key, and client CA
- **Trusted proxies** — CIDR allowlist with forwarded header parsing
- **Event sink (webhooks)** — optional async webhook notifications from WAL
- **Debug stages** — optional `_nexus.debug` response block with admin auth
- **System tray** — Windows tray icon with status and dashboard launch (headless Linux: graceful skip)
- **Threat model** — documented in THREAT_MODEL.md

### Known Issues

- Go 1.26.1 race detector linker bug affects packages that import `modernc.org/sqlite` — see [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)
- SQLite enforces single-writer semantics; PostgreSQL recommended for high-throughput
- In-memory caches (exact + semantic) are lost on restart; persistent cache planned for v3
- Source config hot reload only; destination changes require restart
