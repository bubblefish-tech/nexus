# Changelog

All notable changes to BubbleFish Nexus are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.1.0] — 2026-04-06

Initial public release.

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
