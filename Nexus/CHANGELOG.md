# Changelog

All notable changes to BubbleFish Nexus are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## v0.1.3 — Memory Operating System (2026-04-14)

One memory pool for all your AI apps. Proactive ingestion, cryptographic provenance, bulk import, four moat phases, and extreme durability hardening.

### Ingest (Proactive Ingestion)
- **Filesystem watcher framework** with fsnotify, per-file debouncing (500ms), bounded parse worker pool (4 goroutines)
- **Claude Code parser** — `~/.claude/projects/**/*.jsonl`, offset-based incremental parsing, handles string and array content formats
- **Cursor parser** — `~/.cursor/chat-history/*.json`, whole-file hash comparison for rewrite detection
- **Generic JSONL parser** — fallback for any `{role, content, timestamp}` JSONL file, user-configured paths
- **Scaffolded parsers** for ChatGPT Desktop, Claude Desktop, LM Studio, Open WebUI, Perplexity Comet (detection and interface only; real parsers in v0.1.4)
- **File position tracking** — `ingest_file_state` SQLite table persists (watcher, path, offset, hash) for crash-safe resume without re-ingesting
- **Truncation detection** — SHA-256 hash of last 64 bytes detects file replacement or truncation, triggers full re-parse
- **Path allowlist policy** — enterprise deployments can restrict which paths Ingest reads
- **Security** — symlinks never followed, MaxFileSize (100MB) and MaxLineLength (4MB) enforced, read-only file handles only
- **`bubblefish ingest status|pause|resume|reset`** CLI commands
- **Prometheus metrics** — `nexus_ingest_ingestions_total`, `nexus_ingest_parse_errors_total`, `nexus_ingest_parse_duration_seconds`, `nexus_ingest_active_files`, `nexus_ingest_watchers_total`
- **Config** — `[ingest]` TOML section with `enabled`, `kill_switch`, per-watcher toggles, `generic_jsonl_paths`

### Importer (Bulk Historical Ingest)
- **`bubblefish import <path>`** with auto-format detection
- Supports Claude export ZIP, ChatGPT export ZIP, Claude Code project directories, Cursor directories, generic JSONL
- Idempotent via existing content hash layer
- `--dry-run`, `--source-name`, `--format` flags
- Coming in v0.1.4: Slack exports, Codex CLI, LM Studio, Open WebUI bulk

### Phase 1 — Foundation Layer (Hardened)
- **Group commit ring buffer** — single-consumer goroutine batches WAL writes with one fsync per batch. Configurable max_batch (256) and max_delay (500us)
- **Dual integrity sentinels** — 8-byte start/end sentinels (BF/FB) on every WAL entry for torn-sector-write detection, in addition to CRC32. Backward compatible with v0.1.2 entries. Fail-closed on corrupt sentinel. `SentinelFailures()` Prometheus counter
- **Incremental replay with consistent checkpoints** — checkpoint validation (CRC32 + state_hash + applied_count); any failure triggers full genesis replay
- **Audit log as WAL entry type** — `EntryTypeAudit` with group commit durability. `bubblefish audit export --format jsonl --since --until`
- **Bytes/sec rate limiting** — per-source token bucket with distinct 429 code `bytes_rate_limit_exceeded`
- **fsync verification on startup** — write/fsync/read-back test detects broken fsync (network storage, consumer SSDs). `bubblefish doctor --fsync-test`
- **Disk-full pre-batch reservation** — verifies (batch_size x max_entry_size) bytes available before every group commit. Pre-allocates next segment at 80% fill
- **Goroutine heartbeat supervisor** — `internal/supervisor` package. 30s stall detection, stack dump to `logs/deadlock-*.log`, exit code 3. Graceful shutdown suppresses stall detection during drain
- **Monotonic sequence counter** — `internal/seq` package. Atomic int64 ordering independent of wall-clock time. Persisted to `$BUBBLEFISH_HOME/seq.state` on shutdown
- **WAL zstd compression** — 3-5x size reduction. Auto-detected on replay; mixed segments work. Config: `compress_enabled = true`
- **WAL watchdog heartbeat fix** — moved heartbeat inside ticker case to prevent false supervisor kill (`61d37bd`)

### Phase 2 — Trust Boundary Layer
- **Tier partitions** — `tier` column (0-3) with SQL-layer `AND tier <= ?` enforcement. Non-destructive migration. Admin tokens bypass; source tokens see only `tier <= source.Tier`
- **LSH tier-scoped buckets** — `internal/lsh` package. Per-tier seeds (32 bytes each, persisted at 0600), 16-hyperplane SimHash. Cross-tier collision impossible by construction
- **Review token classes** — `bfn_review_list_` and `bfn_review_read_` with constant-time comparison. `GET /api/review/quarantine` and `GET /api/review/quarantine/{id}`
- **Per-tier rate limiting** — `[[daemon.tiers]]` config blocks with `level`, `requests_per_minute`, `bytes_per_second`. Precedence: source > tier > global
- **Embedding validation envelope** — shape check, content-hash integrity, provider-identity stamping, 3-sigma Welford drift detection, fresh baseline rule (1000 warmup), quarantine state
- **Secrets directory** — `internal/secrets` package. `$BUBBLEFISH_HOME/secrets/` (0700), atomic temp-file + rename (0600), path traversal guard

### Phase 3 — Cluster Mechanism
- **SimHash LSH prefilter** — 16 hyperplanes per tier, bucket ID as 16-bit integer
- **Cluster columns** — `cluster_id`, `cluster_role` (primary/member/superseded), `lsh_bucket` with indexes
- **Async cluster assignment** — cosine similarity >= 0.92, cluster cap 16, deterministic overflow by timestamp, never spans tiers
- **Cluster-aware retrieval** — `cluster-aware` profile with `_nexus.conflict` and `_nexus.cluster_expanded` metadata fields

### Phase 4 — Cryptographic Provenance
- **Per-source Ed25519 keys** — `[source.signing] mode = "local"`, key rotation chain. CLIs: `bubblefish source rotate-key`, `bubblefish source pubkey`
- **Signed write envelopes** — Ed25519 signature over `{source_name, timestamp, idempotency_key, content_hash}`. Daemon signs on write path
- **Hash-chained audit log** — genesis entry with daemon identity, `prev_audit_hash` chain, fail-closed on mismatch (exit code 2)
- **`bubblefish audit recover`** — forensic inspection of corrupted chain, truncate-or-abort operator choice
- **Automatic MCP idempotency** — `SHA-256(session_id || content || timestamp_second)` auto-generated for `nexus_write` calls without explicit key
- **Verify endpoint + CLI** — `GET /verify/{memory_id}` returns proof bundle. `bubblefish verify <proof.json>` with parallel chain verification
- **Python verifier** — `tools/verify-python/verify.py`, independent implementation proving the proof format is spec, not trick
- **Daily Merkle root** — midnight UTC computation, daemon-signed, persisted to `data/merkle-roots/`. `bubblefish anchor setup --gist` for external anchoring
- **Query attestation** — `POST /api/prove` returns daemon-signed proof of query result set
- **Timeline command** — `bubblefish timeline <memory_id>` for forensic audit history
- **Dashboard Proofs tab** — live chain status, verification, proof export
- **60-second cross-vendor demo** — `examples/cryptographic-provenance/` with demo.sh, demo.ps1, agent configs

### Agent Gateway (AG.1–AG.8)
- **Agent identity and registration** — `agents` SQLite table, UUID-based identity, `bubblefish agent register|list|suspend|retire|show` CLI
- **Agent session management** — in-memory session tracking per agent, idle timeout, `GET /api/agents/{id}/sessions`, Prometheus gauge
- **Credential gateway** — synthetic API keys (`bfn_sk_`) route to real provider keys. OpenAI-compatible `/v1/chat/completions` and Anthropic-compatible `/v1/messages` proxy endpoints. Model allowlist, per-key rate limiting, streaming passthrough. Real keys never in logs
- **Tool-use policy enforcement** — per-agent tool allowlist/denylist in agent TOML, parameter limits (max_content_bytes, max_limit, allowed_profiles), hot-reloadable
- **Agent-to-agent coordination** — `agent_broadcast`, `agent_pull_signals`, `agent_status_query` MCP tools. Ephemeral signal queue (max 1000) with optional persistent signals via WAL
- **Per-agent rate limiting and quotas** — requests/min, bytes/sec, writes/day, tool_calls/day per agent TOML. Quota state persisted hourly, resets at UTC midnight
- **Agent activity telemetry** — `EntryTypeAgentActivity` WAL entry, `GET /api/agents/{id}/activity`, dashboard Agents tab, 7-day retention with background pruning
- **Agent health and lifecycle** — heartbeat tracking (inferred from requests), stale/inactive/dormant state transitions, `bubblefish agent health` CLI, dashboard color-coded status

### Chaos A+B Verification
- Two complementary verification paths: direct SQLite DB read (ground truth) + admin API cursor walk
- New `GET /admin/memories` endpoint with stable `(created_at, payload_id)` tuple cursor
- Cross-check distinguishes durability bugs, read-path bugs, phantom data, cursor instability
- `waitForDrain()` polls queue depth before verification to prevent false positives
- Required `-db` flag pointing at memories.db; removes old `-destination` flag

### Testing Infrastructure
- **`bubblefish chaos`** — fault injection tool. Concurrent writers + random faults (network timeout, connection reset, write burst). Machine-readable JSON report with A+B cross-check
- **`bubblefish simulate`** — FoundationDB-style deterministic testing. Real WAL + real SQLite in temp dirs. Seeded fault injection. `--seed N` for reproduction
- **`bubblefish sentinel`** — continuous drift detection. Samples delivered entries, verifies existence in destination. Prometheus metrics
- **Pluggable audit sinks** — syslog (RFC 5424), Fluent Bit (JSON forward), OpenTelemetry (OTLP/HTTP JSON). No new dependencies
- **`bubblefish backup verify`** — full checksum verification against manifest
- **`bubblefish destination rebuild`** — replays WAL into fresh destination

### Substrate (BF-Sketch, experimental, disabled by default)
- **Sketch-based compact embedding representation** alongside full-precision storage. Binary quantization with 1-bit signs and a small set of correction factors per sketch, producing approximately 160 bytes per memory at canonical_d=1024. Sketches participate in the retrieval cascade as a prefilter stage on corpora above 200 memories
- **Embedding canonicalization pipeline** — dimension normalization, L2 normalization, and per-source anisotropy correction. Consistent sketches regardless of embedding source (OpenAI, Cohere, BGE, Voyage, custom)
- **Per-memory cryptographic erasure** — every embedding encrypted at rest with AES-256-GCM. Per-memory encryption key derived via HKDF-SHA-256 (RFC 5869) from the current forward-secure ratchet state. When the ratchet is advanced past a state and the associated state is shredded, the key cannot be re-derived
- **Forward-secure deletion via seed shredding** — sketch projection seeded by a forward-secure HMAC-SHA-256 hash ratchet. When a memory is deleted with `--shred-seed`, the ratchet advances and the prior state is zeroed on disk and in memory. Sketch-based retrieval for memories under the shredded state becomes mathematically impossible
- **Cuckoo filter deletion oracle** — live memories tracked in a cuckoo filter for defense-in-depth set membership. Deletion removes entries in O(1)
- **Audit log composition** — every substrate operation (sketch write, ratchet advance, shred, cuckoo rebuild) logged via the hash-chained audit log
- `bubblefish substrate status`, `bubblefish substrate rotate-ratchet`, `bubblefish substrate prove-deletion <id>` CLI commands
- New SQLite columns: `sketch`, `embedding_ciphertext`, `embedding_nonce` on `memories` table
- New SQLite tables: `substrate_ratchet_states`, `substrate_memory_state`, `substrate_canonical_whitening`, `substrate_cuckoo_filter`
- All substrate functionality behind `[substrate] enabled` feature flag. When disabled, the daemon is bit-for-bit identical to a pre-substrate build

### Other
- **`nexus_status` MCP auto-teaching tool** — returns daemon version, available tools with examples, retrieval profiles, active sources, and ingest state in one call
- **`wake` retrieval profile** — alias for `fast` with `top_k=20`, tuned for low-latency critical-context loading (~170 tokens)
- **Release rehearsal scripts** — `scripts/release/rehearsal.ps1`, `capture_benchmark.ps1`, `capture_chaos.ps1`, `sign_artifacts.ps1`
- **OAuth routes unregistered when disabled** — zero disabled-code attack surface
- **Test deadline flush tolerance** — increased to 500ms for Windows scheduler (`ac991b6`)

### Security (OAuth 2.1 Hardening)
- **Consent page XSS fixed** — untrusted OAuth query parameters migrated from `fmt.Fprintf` to `html/template`. All user-supplied values HTML-escaped
- **CORS on OAuth endpoints** — `Access-Control-Allow-Origin` headers on all `/oauth/*` and `/.well-known/oauth-*` endpoints
- `scopes_supported` in OAuth server metadata, `OPTIONS` preflight on all OAuth endpoints
- `handleAllow` and `handleDeny` now strictly validate `state` and `code_challenge` presence

### Release (Shawn)
- **RL.2** — 24-hour chaos run with Ingest active (release gate, Sunday night)
- **SX.5** — Discord server live with #general, #install-help, #showcase, #bugs, #roadmap channels. Invite link in README
- **RL.3** — Tag v0.1.3, build binaries for Windows/macOS(Intel+ARM)/Linux, GitHub release page with SHA-256 checksums and Ed25519-signed artifacts
- **SX.4** — Python pipx launcher at `python/bubblefish_nexus/` (deferred until after RL.3 — wrapper needs release download URL)
- **RL.4** — HN post (Tuesday 7am Pacific), LinkedIn (Wednesday), r/LocalLLaMA and r/programming cross-posts

### Measured (fill in after release rehearsal)
- Writes: TBD/sec steady state, p99 TBD ms
- Queries: TBD/sec steady state, p99 TBD ms
- Resident: ~TBD MB after 10K writes + 10K reads
- Chaos: TBD kill-9 iterations, zero data loss

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
- **Structured security events** — dedicated security event log for SIEM integration
- **Prometheus metrics** — daemon up, queue depth, request duration, cache hit/miss rates
- **Health doctor** — disk space, database connectivity, embedding provider checks
- **Simple mode install** — `bubblefish install --mode simple` for zero-friction setup
- **`bubblefish dev`** — daemon with debug logging and auto-reload
- **`bubblefish backup`** — create and restore backups of config, WAL, and database
- **`bubblefish bench`** — throughput, latency, and retrieval evaluation benchmarks
- **`bubblefish demo`** — reliability demo with 50-memory crash-recovery scenario
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
