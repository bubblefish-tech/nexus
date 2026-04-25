# Changelog

All notable changes to BubbleFish Nexus are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## v0.1.3 — Memory Operating System (2026-04-25)

One memory pool for every AI app you use. 97 packages, 2,900+ tests,
zero data loss under kill -9. This release adds a governed agent control
plane, agent-to-agent communication, hybrid BM25 search, a full terminal
UI, encryption at rest, and a built-in embedding provider so there is
nothing to configure on first install.

### Governed Agent Control Plane

A complete grant/approval/task governance layer for AI agents:

- **Agent registration** -- `agents` SQLite table, UUID-based identity, `nexus agent register|list|suspend|retire|show` CLI
- **Capability grants** -- 17 reserved capability prefixes (`nexus_write`, `nexus_delete`, `nexus_search`, ...) with configurable default policies (auto-allow, approve-once-per-scope, always-approve). Glob patterns for flexible matching. Deny always wins
- **Approval workflow** -- agents request capabilities they lack, admins approve or deny via CLI or web UI. Expired and revoked grants enforced at runtime
- **Task lifecycle** -- agents create tasks referencing their grants. Tasks track state (pending, running, completed, failed) with full input/output audit
- **Policy engine** -- deterministic policy resolution, destructive-skill escalation, compiled zero-allocation runtime lookup
- **Per-agent rate limiting and quotas** -- requests/min, bytes/sec, writes/day, tool_calls/day per agent TOML. Quota state persisted hourly, resets at UTC midnight
- **Agent activity telemetry** -- WAL-backed activity log, `GET /api/agents/{id}/activity`, dashboard Agents tab, 7-day retention with background pruning
- **Agent health and lifecycle** -- heartbeat tracking, stale/inactive/dormant state transitions, `nexus agent health` CLI, dashboard color-coded status
- **Credential gateway** -- synthetic API keys (`bfn_sk_`) route to real provider keys. OpenAI-compatible `/v1/chat/completions` and Anthropic-compatible `/v1/messages` proxy endpoints. Model allowlist, per-key rate limiting, streaming passthrough. Real keys never in logs
- **Tool-use policy enforcement** -- per-agent tool allowlist/denylist in agent TOML, parameter limits, hot-reloadable
- **Lineage endpoint** -- `GET /api/control/lineage/{task_id}` returns the full decision chain (grants, approvals, actions) for any task

### A2A Protocol (Agent-to-Agent Communication)

Bidirectional agent communication, wire-compatible with the public A2A v1.0 specification:

- **Register once, invoke from anywhere** -- register an agent with Nexus and any MCP-compatible AI assistant (Claude Desktop, ChatGPT, Perplexity, LM Studio, Open WebUI) can invoke it with zero code changes
- **Four physical transports** -- local subprocess (stdio), direct HTTP, Cloudflare-style tunnel, Windows-host-to-WSL2 loopback
- **MCP-to-NA2A bridge** -- nine MCP tools (`a2a_list_agents`, `a2a_send_to_agent`, `a2a_stream_to_agent`, ...) bridging MCP clients to A2A agents
- **Governance engine** -- SQLite-backed grant store, deterministic policy resolution, destructive skill escalation
- **Agent registry** -- TOML hot-reload, Ed25519 Agent Card signing, periodic health checks via `agent/ping`
- **NA2A client** -- connection pool with lazy creation, `input-required` resumption, cancel/resubscribe support
- **Bidirectional `agent/invoke`** -- chain-depth limiting (max 4) prevents infinite callback loops
- **`nexus a2a` CLI** -- `agent add|list|show|test|suspend|retire|pin`, `grant add|list|revoke|elevate`, `task get|cancel|list`, `audit tail|verify`
- **Web UI** -- A2A Permissions page (registered agents, editable grant table, live pending approvals) and Agent Control page (connection panel, skill catalog, channel-aware grants)
- **Chaos and soak tests** -- 20 chaos tests, 4 soak tests (24-hour sustained load, burst recovery, memory stability), cross-platform CI (Ubuntu, Windows, macOS)
- Disabled by default. Enable with `[a2a] enabled = true` in `daemon.toml`

### BM25 Hybrid Search + Temporal Bins

Full-text search alongside embedding-based semantic search:

- **BM25 sparse retrieval** -- FTS5 virtual table with porter stemming, auto-sync triggers, integrated as Stage 3.75 in the 6-stage retrieval cascade
- **Reciprocal Rank Fusion (RRF)** -- merges BM25 and dense embedding results with k=60, wired into Stage 5
- **Temporal bins** -- every memory tagged with a temporal bin (today, yesterday, this week, this month, ...). `temporal_bin`, `temporal_label`, `age_human` metadata in query responses
- **Temporal query hints** -- natural language like "what did I say yesterday" automatically filters to the correct bin (11 bin patterns recognized)
- **Hourly bin refresh** -- background goroutine keeps bin assignments current
- Zero new dependencies (FTS5 built into modernc.org/sqlite)

### Built-in Embedding Provider

Zero-config embeddings out of the box:

- **nomic-embed-text-v1.5** (Q4_K_S GGUF, 75 MB, Apache 2.0) managed as a subprocess via llama-server
- **Auto-download at install time** -- `EnsureModelDownloaded` / `EnsureServerDownloaded` with progress callbacks
- **OpenAI-compatible API** -- speaks `/v1/embeddings` on a random localhost port
- **Health check polling** -- 60s startup timeout, 250ms interval
- **Auto-restart on crash** -- max 3 retries, exponential backoff (2s to 30s)
- **Batch embedding** -- native batch via array input for OpenAI-compatible providers; sequential loop for Ollama
- **Hedged embedding** -- optional fallback provider via cristalhq/hedgedhttp for latency-sensitive deployments
- Default in new installs: `embedding.provider = "builtin"`. No API key, no external service, no configuration

### TUI (Bubble Tea Terminal Interface)

Full-featured terminal dashboard built on the Charm ecosystem:

- **9-page state machine** -- Dashboard, Memory Browser, Retrieval Theater, Audit Walker, Agents, Crypto Vault, Governance, Immune Theater, and Splash
- **Dashboard** -- 6-stat-card grid (memories, audit events, agents, health, quarantine, WAL lag) with gradient styling
- **Memory Browser** -- list + search + detail panel with score display
- **Retrieval Theater** -- live waterfall visualization of the 6-stage retrieval cascade, SQL preview with keyword highlighting
- **Audit Walker** -- entry card with prev_hash/content/hash/signature flow, Merkle proof tree
- **Crypto Vault** -- three-state signing panel (enabled/awaiting config/error), master key status, ratchet status
- **Governance** -- grants, approvals, and tasks panels with context-aware empty states
- **Immune Theater** -- quarantine list with pending/total counts, security event feed
- **Command palette** (Ctrl+K), help overlay, slash commands, four-theme switching via `/theme`
- **Bubble field physics background**, ANSI fish emblem, harmonica spring animations on splash
- **Demo mode** (D key) -- 9-step scripted walkthrough with narration panel
- **Kuramoto phase wheel** -- ODE-based phase synchronization visualization
- **Free energy gauge** -- wired to `/api/stats.free_energy_nats`
- `nexus tui` with `--api-url` and `--admin-token` flags. DEBUG env var enables `tea.LogToFile`

### Encryption at Rest

11-layer encryption with MasterKeyManager:

- **MasterKeyManager** -- derives per-purpose keys via HKDF-SHA-256 (RFC 5869) from a single master secret (`NEXUS_PASSWORD` env var)
- **WAL encryption** -- AES-256-GCM with per-entry nonce
- **WAL HMAC integrity** -- optional HMAC-SHA256 tamper detection
- **Per-memory cryptographic erasure** -- every embedding encrypted at rest with AES-256-GCM, key derived from forward-secure ratchet state
- **Forward-secure deletion** -- HMAC-SHA-256 hash ratchet with seed shredding. `nexus substrate prove-deletion <id>` provides mathematical proof
- **FTS5 indexes plaintext before encryption** -- full-text search works even with encryption enabled
- **Agent registry encryption** -- AES-256-GCM on registry rows
- **Secrets directory** -- `$BUBBLEFISH_HOME/secrets/` (0700), atomic temp-file + rename (0600), path traversal guard
- **Config signing** -- HMAC-SHA256 signatures for signed-mode deployments

### WAL Crash Safety

Kill-9 survival verified across all platforms:

- **WAL-first architecture** -- WAL fsync, then queue, then database. Always in that order
- **Group commit ring buffer** -- single-consumer goroutine batches writes with one fsync per batch. Configurable max_batch (256) and max_delay (500us)
- **Dual integrity sentinels** -- 8-byte start/end sentinels (BF/FB) on every entry for torn-sector-write detection, plus CRC32 checksums
- **Incremental replay with consistent checkpoints** -- checkpoint validation (CRC32 + state_hash + applied_count); any failure triggers full genesis replay
- **fsync verification on startup** -- write/fsync/read-back test detects broken fsync (network storage, consumer SSDs). `nexus doctor --fsync-test`
- **Disk-full pre-batch reservation** -- verifies space available before every group commit. Pre-allocates next segment at 80% fill
- **WAL zstd compression** -- 3-5x size reduction. Auto-detected on replay; mixed segments work
- **Monotonic sequence counter** -- atomic int64 ordering independent of wall-clock time
- **Goroutine heartbeat supervisor** -- 120s stall detection, stack dump, exit code 3
- **Instance lock** -- gofrs/flock prevents two-daemon corruption
- **Backup-on-start** -- `.lastgood` snapshot gated on clean shutdown

### Worm Auto-Discovery

Automatic detection and connection of AI tools on your machine:

- **30-connector registry** -- Claude Desktop, Cursor, Windsurf, ChatGPT Desktop, Ollama, LM Studio, and 24 more
- **Digital twin environment model** -- tracks tool state (running/stopped/unknown), config paths, drift, health
- **Behavioral protocol fingerprinting** -- 6 probes identify OpenAI-compat, Ollama, TGI, KoboldCpp, Tabby endpoints
- **Convergence reconciliation loop** -- Kubernetes-style declarative control: detect drift, select known-issue recipe, execute transactional fix, learn from outcome
- **ACID transaction engine** -- journaled rollback, crash recovery via `RecoverIncomplete()`
- **Adaptive fix learning** -- decay-adjusted success rate (7-day half-life) prioritizes fixes that worked before
- **Network topology resolver** -- Docker networks, WSL2 bridge IP, proxy detection, port probing
- **Transparent AI API proxy** -- SSRF-safe allowlist (loopback only), `X-Nexus-Proxy` header injection
- **Worm auto-connect** -- generates source TOML for discovered tools after successful reconcile (gated behind `AutoConnect` config flag)
- `nexus maintain status|fix|watch|registry` CLI commands

### OAuth 2.1

Full RFC 8414 compliant OAuth server, verified with ChatGPT:

- **Authorization server** -- `/.well-known/oauth-authorization-server`, `/oauth/authorize`, `/oauth/token`, `/oauth/jwks`
- **RSA-2048 key management** -- auto-generated on first start, PEM storage with 0600 permissions
- **JWT access tokens** -- RS256 signed, 1hr TTL, `bfn_source` claim for source mapping
- **PKCE (S256)** -- `subtle.ConstantTimeCompare` verification, single-use auth codes
- **Self-contained consent page** -- branded HTML, XSS-safe via `html/template`
- **CORS on OAuth endpoints** -- localhost/127.0.0.1 allowed, external origins rejected
- OAuth routes unregistered when disabled -- zero disabled-code attack surface

### Web Dashboard

Admin-authenticated browser UI:

- **Security tab, metrics, pipeline visualization**
- **A2A Permissions page** -- registered agents, editable grant table, live pending approvals
- **Agent Control page** -- connection panel, skill catalog with grant state
- **Proofs tab** -- live chain status, verification, proof export
- **Memory graph** -- `GET /dashboard/memgraph` with D3 visualization
- **Memory health panel** -- continuity score, freshness, anomaly detection
- **Aggregated stats endpoint** -- `GET /api/stats` for dashboard stat cards

### Ingest (Proactive Ingestion)

- **Filesystem watcher framework** with fsnotify, per-file debouncing, bounded parse worker pool
- **6 fully implemented parsers** -- Claude Code, Cursor, Claude Desktop, ChatGPT Desktop, Open WebUI, Perplexity Comet
- **Generic JSONL parser** -- fallback for any `{role, content, timestamp}` JSONL file
- **Markdown diary importer** -- `nexus import` supports markdown diaries with heading-based segmentation
- **Bulk importer** -- `nexus import <path>` with auto-format detection (Claude ZIP, ChatGPT ZIP, Cursor dirs, generic JSONL). `--dry-run`, `--source-name`, `--format` flags
- **File position tracking** -- crash-safe resume without re-ingesting
- **Truncation detection** -- SHA-256 hash detects file replacement, triggers full re-parse

### Substrate (BF-Sketch, experimental)

- Sketch-based compact embedding representation (~160 bytes per memory)
- Embedding canonicalization pipeline (dimension normalization, L2 normalization, anisotropy correction)
- Cuckoo filter deletion oracle
- Forward-secure deletion via seed shredding with mathematical proof
- Behind `[substrate] enabled` feature flag

### Performance and Hardening

- **Projection engine** -- 9.9x faster (866us to 87us), 4.4x fewer allocations
- **JWT validation cache** -- 68x faster on cache hit (31us to 459ns)
- **SQLite tuning** -- synchronous=NORMAL, mmap 256MiB, cache 128MiB, prepared statements on hot paths
- **Circuit breakers** -- sony/gobreaker on destinations (5 failures trips, 10s open timeout)
- **sync.Pool** -- for JSON encode buffers and io.Copy buffers (oversized eviction)
- **Write deduplication** -- content-hash cache prevents identical writes within 24h
- **Panic recovery boundaries** -- safego.Go wrapper on 6 daemon goroutines
- **Structured error codes** -- 8 sentinel error types with IsInfrastructureError()
- **Log rotation** -- lumberjack (100MiB max, 5 backups, 30 day retention, compressed)
- **automaxprocs + GOMEMLIMIT** -- container/WSL2/cgroup aware
- **TLS cipher allowlist** -- ECDHE+AEAD only, TLS 1.2 minimum
- **nexus-supervisor** -- watchdog binary with exponential backoff (5s to 60s)
- **nexus doctor** -- 5 new checks (cloud-sync, disk space, ports, permissions, filesystem type), `--repair` flag, auto-run at startup
- **nexus self-test** -- non-destructive smoke test on live daemon
- **nexus trace** -- captures runtime/trace from pprof

### Testing Infrastructure

- **2,900+ tests** across 97 packages, all passing with `-race`
- **`nexus chaos`** -- concurrent writers + random faults, A+B cross-check verification
- **`nexus simulate`** -- FoundationDB-style deterministic testing with seeded faults
- **`nexus drift`** -- continuous drift detection with Prometheus metrics
- **20 A2A chaos tests** + 4 soak tests (24-hour sustained load)
- **Pluggable audit sinks** -- syslog (RFC 5424), Fluent Bit, OpenTelemetry
- **Cross-platform CI** -- GitHub Actions on Ubuntu, Windows, macOS

### Subscriptions

- **`nexus_subscribe` MCP tool** -- subscribe to topics with filter embeddings
- **`nexus_unsubscribe` / `nexus_subscriptions`** -- manage and list subscriptions
- **Subscription matcher** -- cached filter embeddings, similarity threshold
- **Search boost** -- subscribed content ranks higher in retrieval
- **Audit chain integration** -- subscription events logged in hash-chained audit

### Memory Health

- **Memory health calculator** -- continuity, freshness, anomaly detection
- **`nexus doctor --memory-health`** CLI
- **`GET /api/health/memory`** endpoint + dashboard panel

### Other

- **`nexus_status` MCP tool** -- returns version, available tools, retrieval profiles, sources, ingest state, temporal awareness, search modes
- **`wake` retrieval profile** -- alias for `fast` with `top_k=20` (~170 tokens)
- **CORS middleware** -- localhost/127.0.0.1 on any port, no wildcard
- **System tray** -- Windows tray icon with status and dashboard launch
- **Release rehearsal scripts** -- `scripts/release/`
- **Blessed integration configs** -- pre-built templates for Claude Code, Claude Desktop, Open WebUI, Perplexity

### Known Issues

- Go 1.26.1 race detector linker bug affects packages that import `modernc.org/sqlite` -- see [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)
- SQLite enforces single-writer semantics; PostgreSQL recommended for high-throughput
- In-memory caches (exact + semantic) are lost on restart; persistent cache planned for future release
- Source config hot reload only; destination changes require restart

---

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
- **`nexus install --oauth-issuer`** — install flag for OAuth setup
- **Doctor OAuth checks** — issuer_url, key file, client registration, HTTPS validation

### Security

- PKCE verification uses `subtle.ConstantTimeCompare` — never `==`
- Auth codes are single-use — deleted before JWT issuance
- `redirect_uri` mismatch returns 400, never redirects
- `private_key_file` as plain literal refused at startup (`SCHEMA_ERROR`)
- Private key never appears in logs, responses, or error messages

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
- **Config signing** — HMAC-SHA256 signatures for signed-mode deployments (`nexus sign-config`)
- **Constant-time auth** — `subtle.ConstantTimeCompare` for all token validation
- **Admin vs data token separation** — wrong token class returns 401
- **JWT/JWKS authentication** — advanced auth pattern with claim mapping and audience validation
- **Provenance fields** — `actor_type` (user/agent/system) + `actor_id` on every write
- **MCP server** — JSON-RPC 2.0 (`nexus_write`, `nexus_search`, `nexus_status`) for Claude Desktop and Cursor
- **Web dashboard** — admin-authenticated UI with security tab, metrics, and pipeline visualization
- **Structured security events** — dedicated security event log for SIEM integration
- **Prometheus metrics** — daemon up, queue depth, request duration, cache hit/miss rates
- **Health doctor** — disk space, database connectivity, embedding provider checks
- **Simple mode install** — `nexus install --mode simple` for zero-friction setup
- **`nexus dev`** — daemon with debug logging and auto-reload
- **`nexus backup`** — create and restore backups of config, WAL, and database
- **`nexus bench`** — throughput, latency, and retrieval evaluation benchmarks
- **`nexus demo`** — reliability demo with 50-memory crash-recovery scenario
- **Hot reload** — source config changes applied without restart
- **Consistency assertions** — background WAL-to-destination consistency checks
- **WAL health watchdog** — background disk/permissions/latency monitoring
- **Blessed integration configs** — pre-built templates for Claude Code, Claude Desktop, Open WebUI, Perplexity
- **TLS/mTLS support** — optional TLS with configurable cert, key, and client CA
- **Event sink (webhooks)** — optional async webhook notifications from WAL
- **System tray** — Windows tray icon with status and dashboard launch

### Known Issues

- Go 1.26.1 race detector linker bug affects packages that import `modernc.org/sqlite` — see [KNOWN_LIMITATIONS.md](KNOWN_LIMITATIONS.md)
- SQLite enforces single-writer semantics; PostgreSQL recommended for high-throughput
- In-memory caches (exact + semantic) are lost on restart; persistent cache planned for v3
- Source config hot reload only; destination changes require restart
