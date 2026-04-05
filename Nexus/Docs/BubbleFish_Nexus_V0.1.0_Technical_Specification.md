# BubbleFish Nexus v0.1.0 — Technical Specification
# Authoritative behavioral contract for all Nexus code
# Internal development version: 2.2 | Public version: v0.1.0

> **VERSION NOTE:** All internal references to "v2.2" or "2.2.0" refer to the internal
> development version. The PUBLIC version for GitHub, README, CLI, and all user-facing
> surfaces is **v0.1.0** (pre-1.0, API subject to change).

---

## Quick Lookup Index

| Topic | Section |
|-------|---------|
| Write path operation order | 3.2 |
| Read path operation order | 3.3 |
| 6-stage retrieval cascade | 3.4 |
| Retrieval profiles (fast/balanced/deep) | 3.5 |
| Temporal decay algorithm | 3.6 |
| Semantic short-circuit and fast paths | 3.7 |
| Cursor-based pagination | 3.8 |
| WAL record structure | 4.1 |
| WAL segment rotation | 4.2 |
| MarkDelivered semantics | 4.3 |
| WAL health watchdog | 4.4 |
| Crash and replay semantics | 4.5 |
| Queue design | 5 |
| Authentication and authorization | 6.1 |
| TLS/mTLS configuration | 6.2 |
| Trusted proxies | 6.3 |
| WAL integrity and encryption | 6.4 |
| Config signing | 6.5 |
| OAuth edge integration | 6.6 |
| Config lint | 6.7 |
| Threat model | 6.8 |
| Canonical write envelope (TranslatedPayload) | 7.1 |
| _nexus response metadata | 7.2 |
| _nexus.debug payload | 7.3 |
| Error response format and codes | 7.4 |
| Write path failure contract | 8.1 |
| Read path failure contract | 8.2 |
| Directory structure | 9.1 |
| daemon.toml complete reference | 9.2 |
| Source TOML complete reference | 9.3 |
| Event sink (webhooks) | 10 |
| Structured logging fields | 11.1 |
| Structured security events | 11.2 |
| Prometheus metrics list | 11.3 |
| Health endpoints | 11.4 |
| Consistency assertions | 11.5 |
| HTTP API endpoints | 12 |
| CLI commands | 13.1 |
| Dashboard features | 13.2 |
| Reliability demo | 13.3 |
| bubblefish bench | 13.4 |
| Blessed integration configs | 13.5 |
| Reference architectures | 13.6 |
| Hot reload | 14.1 |
| Shutdown design | 14.2 |
| MCP server | 14.3 |
| Embedding provider | 14.4 |
| Backup and restore | 14.5 |
| V2.2 feature set (complete table) | 15 |
| Out of scope for V2.2 | 15.1 |
| Validation plan (all tests) | 16 |
| Constraints and tradeoffs | 17 |
| V3 architecture readiness | 18 |
| Glossary | 19 |

---

## Section 1 — Executive Summary

<!-- Section 1 — Executive Summary -->

BubbleFish Nexus v2.2 is a gateway-first AI memory daemon. It sits between multiple AI applications and one or more shared memory backends. Its job is to authenticate sources, normalize writes, protect storage, retrieve memories intelligently using the cheapest safe path first, shape responses to minimize token waste, and provide durable crash recovery.

Nexus v2.2 is not an AI system. It is a memory gateway, retrieval proxy, policy layer, and observability layer.

**Core Value Proposition:** One daemon. Many AI clients. One protected memory backend. Intelligent retrieval. Zero duplicate writes. Minimal tokens. Crash-safe. Policy-aware. Retrieval-optimized.

### 1.1 What Changed from V2.1

<!-- Section 1.1 — What Changed from V2.1 -->

V2.1 closed every correctness, security, and quality gap identified in two exhaustive code reviews. V2.2 adds community-driven enhancements, deeper durability guarantees, temporal-semantic intelligence, comprehensive failure contracts, security hardening, admin UX features, and frictionless install profiles:

-   CRC32 checksums on all WAL entries with crash-mid-write corruption detection on replay

-   Optional WAL-at-rest encryption with AES-256-GCM (keyfile reference, nonce-per-entry, CRC over ciphertext)

-   Optional HMAC-SHA256 WAL integrity mode for tamper detection without encryption

-   Config signing via bubblefish sign-config for signed-mode deployments

-   Temporal decay reranking with configurable exponential half-life model

-   Tiered, policy-aware temporal decay: per-destination and per-collection decay settings with exponential and step modes

-   Retrieval profiles: fast, balanced, deep with per-source stage toggles

-   Semantic short-circuit threshold and exact-subject last-N fast path

-   Zero-dependency LRU cache using Go generics (map + container/list)

-   WAL segment rotation crash safety with dual-segment replay

-   WAL health watchdog: background goroutine monitoring disk space, permissions, and append latency

-   Consistency assertions: periodic background check tying WAL and destination state with a consistency score

-   Explicit failure contracts for writes and reads with machine-readable error codes

-   Config lint command (bubblefish lint) checking dangerous binds, missing idempotency, unbounded limits

-   Threat model documentation covering local attackers, network eavesdroppers, and disk theft

-   Provenance fields: actor_type and actor_id on canonical write envelope for user/agent/system memory classification

-   TLS/mTLS support with configurable cert, key, client CA, and min/max TLS version

-   Trusted proxy CIDR list with forwarded header parsing for effective client IP derivation

-   Admin vs data token strict separation: admin tokens rejected on data endpoints and vice versa

-   Simple Mode install (bubblefish install --mode simple): zero-friction, zero-config, SQLite-only setup

-   bubblefish dev command: same daemon with dev-friendly defaults (debug logging, auto-reload, config path printing)

-   Open WebUI install profile (--profile openwebui): pre-built source config for Open WebUI payload shape

-   Postgres+pgvector and Supabase/OpenBrain install profiles with guided setup and doctor connectivity checks

-   bubblefish backup create / restore: config, compiled files, WAL, and SQLite snapshot tooling

-   Optional event sink (webhooks): separate goroutine pipeline tailing WAL, never blocks write path

-   OAuth edge integration templates: reverse proxy pattern and JWT header mapping with CLI template generators

-   Live pipeline visualization via lossy event channel (never blocks write/read hot paths)

-   Black Box Mode: dashboard toggle showing queries traversing cascade stages in real time

-   Conflict inspector: read-only introspection detecting contradictory memories across sources and timestamps

-   Time-travel view: query state as-of a specific timestamp without altering correctness

-   bubblefish bench command: throughput, latency, and retrieval evaluation benchmarks

-   Reliability demo: golden crash-recovery scenario exposed via CLI and dashboard button

-   Debug stages hook: optional _nexus.debug payload with per-stage candidates, latencies, cache hit/miss

-   Security-focused Prometheus metrics: auth failures, policy denials, rate limit hits, admin calls per endpoint

-   Structured security events: dedicated log stream for SIEM integration (Filebeat, fluentd, Loki)

-   Security tab on web dashboard: source policies, failed auth history, config lint warnings

-   Blessed integration configs: pre-built, security-reviewed templates for Claude Desktop, Claude Code, and agents

-   Reference architectures: documented deployment patterns for dev laptop, home lab, and air-gapped environments

-   Architecture diagram (Mermaid) included in README and documentation

-   Docker Compose reference with correct volume mounts, Docker Secrets, and read-only filesystem support

-   Ollama-specific diagnostics in bubblefish doctor

-   Phase CR split into 10 sub-phases to prevent regression cascades

-   DrainWithContext goroutine lifecycle explicitly documented

-   encoding/json + sync.Pool replaces third-party JSON parsers

### 1.2 Design Philosophy (Unchanged)

<!-- Section 1.2 — Design Philosophy (Unchanged) -->

-   Reliability first. Speed second. Features third.

-   Every payload is persisted to the WAL before it touches the queue or database.

-   The database is never written to directly — always through the queue.

-   Duplicate writes are prevented by idempotency tracking.

-   The AI client never receives raw database rows — only stripped, semantic content.

-   The system fails closed, not open.

-   Every failure mode is classified, logged, and handled — never silently dropped.

-   Nexus is a gateway. It routes data. It does not generate content or embeddings independently.

-   Advanced features are optional and disabled by default. Simple mode gets users running in under 60 seconds.

## Section 2 — Environment and Deployment

<!-- Section 2 — Environment and Deployment -->

### 2.1 Target Deployment Model

<!-- Section 2.1 — Target Deployment Model -->

-   Single-machine local deployment: developer laptop, home server, or VPS.

-   Single binary. No container orchestration required.

-   Optionally exposed via Cloudflare Tunnel for remote AI clients.

-   System tray support on Windows for persistent background operation.

-   Headless Linux: tray gracefully skipped when \$DISPLAY is empty (log INFO, not error).

-   One-command install: bubblefish install (defaults to SQLite, zero cloud accounts needed).

-   Simple Mode: bubblefish install --mode simple for zero-friction setup (single source, SQLite, no embedding keys).

-   Install profiles: --profile openwebui, --dest sqlite (default), --dest postgres, --dest openbrain.

-   Pre-built binaries for Windows, Linux, macOS (amd64 + arm64). No Go required.

-   MCP verification: bubblefish mcp test (self-test before Claude Desktop configuration).

-   Docker Compose reference deployment with Docker Secrets and read-only filesystem.

-   Three deployment modes: safe, balanced, fast for security/performance tradeoffs.

### 2.2 Install Profiles and Modes

<!-- Section 2.2 — Install Profiles and Modes -->

#### 2.2.1 Simple Mode

<!-- Section 2.2.1 — Simple Mode -->

For users who want zero-friction setup with no configuration decisions:

bubblefish install --mode simple

Simple Mode creates:

-   SQLite destination with default settings (WAL journal mode, busy_timeout=5000).

-   A single default source named 'default' with can_read=true, can_write=true, relaxed rate limits, minimal policy (no field visibility complexity).

-   Embeddings disabled (no external API keys required). Semantic search stages bypassed gracefully.

-   Binds to 127.0.0.1, no TLS, no encryption. Everything local.

After install, Simple Mode prints exactly three next steps:

1.  bubblefish start

2.  curl -X POST http://localhost:8080/inbound/default -H 'Authorization: Bearer YOUR_KEY' -H 'Content-Type: application/json' -d '{"message":{"content":"Hello","role":"user"},"model":"test"}'

3.  (Optional) Configure Open WebUI or Claude Desktop with the generated API key.

#### 2.2.2 Stack-Specific Install Profiles

<!-- Section 2.2.2 — Stack-Specific Install Profiles -->

+----------------------------------------------+-------------------------------------------------+---------------------------------------------------------------------------------------------+
| **Command**                                  | **Creates**                                     | **Notes**                                                                                   |
+==============================================+=================================================+=============================================================================================+
| bubblefish install --dest sqlite            | sqlite.toml destination                         | Default. Zero external dependencies.                                                        |
+----------------------------------------------+-------------------------------------------------+---------------------------------------------------------------------------------------------+
| bubblefish install --dest postgres          | postgres.toml destination                       | Prompts for connection string. Runs doctor connectivity check.                              |
+----------------------------------------------+-------------------------------------------------+---------------------------------------------------------------------------------------------+
| bubblefish install --dest openbrain         | openbrain.toml destination                      | Prompts for Supabase URL and service role key.                                              |
+----------------------------------------------+-------------------------------------------------+---------------------------------------------------------------------------------------------+
| bubblefish install --profile openwebui      | ollama-openwebui.toml source + sqlite.toml dest | Source config tuned for Open WebUI payload shape. Drops example provider JSON to examples/. |
+----------------------------------------------+-------------------------------------------------+---------------------------------------------------------------------------------------------+
| bubblefish install --oauth-template caddy   | examples/oauth/Caddyfile                        | Example Caddy reverse-proxy config wiring OIDC to Nexus.                                    |
+----------------------------------------------+-------------------------------------------------+---------------------------------------------------------------------------------------------+
| bubblefish install --oauth-template traefik | examples/oauth/traefik.yml                      | Example Traefik reverse-proxy config with OIDC middleware.                                  |
+----------------------------------------------+-------------------------------------------------+---------------------------------------------------------------------------------------------+

#### 2.2.3 Deployment Modes

<!-- Section 2.2.3 — Deployment Modes -->

+----------+----------------------+----------------------+-------------------+------------------------+-----------+-------------------------------------------+
| **Mode** | **TLS**              | **WAL Encryption**   | **WAL Integrity** | **Rate Limits**        | **Bind**  | **Use Case**                              |
+==========+======================+======================+===================+========================+===========+===========================================+
| safe     | Enabled (required)   | Enabled (required)   | MAC (HMAC-SHA256) | Conservative (500/min) | 127.0.0.1 | Production, remote access, sensitive data |
+----------+----------------------+----------------------+-------------------+------------------------+-----------+-------------------------------------------+
| balanced | Off (easy to enable) | Off (easy to enable) | CRC32 (default)   | Default (2000/min)     | 127.0.0.1 | Personal use, home server, development    |
+----------+----------------------+----------------------+-------------------+------------------------+-----------+-------------------------------------------+
| fast     | Off                  | Off                  | CRC32             | Relaxed (10000/min)    | 127.0.0.1 | Local-only structured-first, benchmarking |
+----------+----------------------+----------------------+-------------------+------------------------+-----------+-------------------------------------------+

### 2.3 Trust Boundaries

<!-- Section 2.3 — Trust Boundaries -->

+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| **Boundary**               | **Description**                                                                                                                  |
+============================+==================================================================================================================================+
| AI Client → Nexus          | Per-source API key (data-plane token). Constant-time comparison. No shared keys. Admin tokens rejected on data endpoints.        |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| MCP Client → Nexus         | Dedicated MCP API key. Binds 127.0.0.1 exclusively. Same pipeline as HTTP.                                                       |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| Nexus → OpenBrain/Supabase | Service role key in env vars or file references. Never logged.                                                                   |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| Nexus → Embedding Provider | Provider API key. Stored in env vars or file references. Never logged.                                                           |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| Nexus → Event Sinks        | Webhook URLs with per-sink auth tokens. Separate goroutine pipeline. Never blocks write path.                                    |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| Local → Remote             | Cloudflare Tunnel or TLS/mTLS for direct exposure. Nexus binds localhost by default.                                             |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| Admin → Nexus              | Separate admin token. Cannot be used on data endpoints. Data keys cannot be used on admin endpoints.                             |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| Web Dashboard              | Requires admin_token by default. Security tab shows source policies and failed auth history.                                     |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| Trusted Proxies            | Configurable CIDR list. Forwarded headers honored only from trusted proxy IPs.                                                   |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| Secret Storage             | Supports env:VAR_NAME, file:/path (Docker Secrets, Kubernetes Secrets), and plain literals (dev only).                           |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+
| Config Signing             | Optional signed-mode: compiled configs signed by bubblefish sign-config. Daemon refuses to load unsigned configs in signed mode. |
+----------------------------+----------------------------------------------------------------------------------------------------------------------------------+

## Section 3 — Architecture

<!-- Section 3 — Architecture -->

### 3.1 System Planes

<!-- Section 3.1 — System Planes -->

+--------------------+-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| **Plane**          | **Responsibility**                                                                                                                                                                                                                                                                                                                        |
+====================+===========================================================================================================================================================================================================================================================================================================================================+
| Control            | Config loading, schema validation, policy compilation, embedding config, hot reload (source-only), cache rules, secret resolution at startup, config lint, deployment mode application, TLS certificate loading, config signing verification.                                                                                             |
+--------------------+-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| Identity & Policy  | Source authentication (constant-time), resolved key cache, admin vs data token separation, subject namespace resolution, scope checks, field-level projection rules, CanRead/CanWrite enforcement, MCP key validation, trusted proxy verification, OAuth JWT mapping (advanced).                                                          |
+--------------------+-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| Data — Ingestion | Write path normalization, WAL (10MB scanner, CRC32, optional HMAC, optional encryption, atomic DELIVERED), queue (non-blocking send, sync.Once drain), destination writes, provenance tracking (actor_type, actor_id), event sink emission.                                                                                               |
+--------------------+-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| Data — Retrieval | 6-stage retrieval cascade, retrieval profiles (fast/balanced/deep), temporal decay reranking (tiered, policy-aware), semantic short-circuit, exact-subject fast path, cursor pagination, projection, debug stages.                                                                                                                        |
+--------------------+-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| Cache              | Exact cache (zero-dep LRU), semantic cache, invalidation rules, freshness watermarks, scope isolation, configurable size limits, safe eviction.                                                                                                                                                                                           |
+--------------------+-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| Observation        | Structured slog JSON logs, Prometheus metrics (all recorded, private registry), WAL health watchdog, consistency assertions and score, replay state, queue depth, cache hit ratios, visualization event channel, security-focused metrics, structured security events, SIEM integration hooks.                                            |
+--------------------+-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| Admin UX           | CLI commands (install, start, build, doctor, policy, mcp test, lint, bench, demo, dev, backup, sign-config), TUI dashboard, web dashboard (requires admin auth, security tab), live pipeline visualization, Black Box Mode, conflict inspector, time-travel view, reliability demo, blessed integration configs, reference architectures. |
+--------------------+-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+

### 3.2 Write Path (Inbound) — V2.2 Corrected Operation Order

<!-- Section 3.2 — Write Path (Inbound) — V2.2 Corrected Operation Order -->

> **⚠ WARNING:** Operation order is critical for correctness and security. Steps must execute in exactly this sequence.

+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| **Step** | **Operation**                                              | **Notes**                                                                                 |
+==========+============================================================+===========================================================================================+
| 1        | AI client sends POST /inbound/{source}                     | chi.URLParam(r, 'source'). MCP writes enter at step 3 via internal pipeline.            |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 2        | API key validated (constant-time)                          | Resolved key from startup cache. Admin tokens rejected (wrong_token_class).               |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 3        | CanWrite check                                             | 403 source_not_permitted_to_write if source.CanWrite = false.                             |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 4        | Subject namespace resolved                                 | X-Subject header override or source namespace default.                                    |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 5        | Policy gate                                                | Allowed destination, operation, collection? 403 policy_denied if not.                     |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 6        | Payload size check (MaxBytesReader)                        | BEFORE reading any bytes. 413 payload_too_large if over limit.                            |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 7        | Idempotency check                                          | BEFORE rate limiting. If seen → 200 immediately with original payload_id.                 |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 8        | Rate limit check                                           | AFTER idempotency. 429 + Retry-After header.                                              |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 9        | Field mapping via gjson dot-path                           | Only mapped fields survive. Unmapped fields populate Metadata map.                        |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 10       | Transforms applied                                         | trim, concat, coalesce, conditionals.                                                     |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 11       | Embedding field extracted                                  | If AI client sent it. Not generated by Nexus.                                             |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 12       | Provenance fields set                                      | actor_type and actor_id from X-Actor-Type/X-Actor-ID headers or source config defaults.   |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 13       | Canonical Write Envelope built                             | TranslatedPayload with namespace, subject, metadata, provenance.                          |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 14       | WAL append + integrity check + optional encryption + fsync | CRC32 always. HMAC if integrity=mac. Encrypt if wal.encryption.enabled. 500 if WAL fails. |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 15       | Idempotency key stored                                     | In dedup map. Survives restart via WAL replay.                                            |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 16       | Non-blocking channel send to queue                         | select default: return 429 queue_full. WAL entry still durable.                           |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 17       | Return 200 + payload_id                                    | Client can proceed. Data is durable in WAL.                                               |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 18       | Event sink emission (async)                                | Summarized event payload sent to configured webhook sinks. Lossy channel. Never blocks.   |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 19       | Worker writes to destination                               | On success: MarkDelivered (atomic rename + CRC32), cache invalidated, watermark advanced. |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+
| 20       | Visualization event emitted (async)                        | Pipeline event for live dashboard. Lossy channel. Never blocks.                           |
+----------+------------------------------------------------------------+-------------------------------------------------------------------------------------------+

### 3.3 Read Path (Outbound) — V2.2 with Retrieval Profiles and Tiered Decay

<!-- Section 3.3 — Read Path (Outbound) — V2.2 with Retrieval Profiles and Tiered Decay -->

+----------+-----------------------------------------------------------------------------------------------+
| **Step** | **Operation**                                                                                 |
+==========+===============================================================================================+
| 1        | AI client sends GET /query/{destination} with credentials.                                    |
+----------+-----------------------------------------------------------------------------------------------+
| 2        | API key validated (constant-time). Data-plane token required. Admin tokens rejected.          |
+----------+-----------------------------------------------------------------------------------------------+
| 3        | CanRead checked — 403 if false.                                                             |
+----------+-----------------------------------------------------------------------------------------------+
| 4        | Rate limit checked per source (applies to reads).                                             |
+----------+-----------------------------------------------------------------------------------------------+
| 5        | Subject namespace resolved.                                                                   |
+----------+-----------------------------------------------------------------------------------------------+
| 6        | Policy gate: allowed operations, retrieval modes, max results.                                |
+----------+-----------------------------------------------------------------------------------------------+
| 7        | Query normalized. Cursor decoded if present.                                                  |
+----------+-----------------------------------------------------------------------------------------------+
| 8        | Retrieval profile resolved (fast/balanced/deep from ?profile= or source config default).      |
+----------+-----------------------------------------------------------------------------------------------+
| 9        | Retrieval cascade executed (6 stages, filtered by active profile).                            |
+----------+-----------------------------------------------------------------------------------------------+
| 10       | Temporal decay reranking applied (tiered: per-destination/collection settings if configured). |
+----------+-----------------------------------------------------------------------------------------------+
| 11       | Result set shaped through Projection Engine.                                                  |
+----------+-----------------------------------------------------------------------------------------------+
| 12       | _nexus metadata added. Strip if policy.strip_metadata = true.                                |
+----------+-----------------------------------------------------------------------------------------------+
| 13       | Optional _nexus.debug payload added if debug_stages=true and admin token provided.           |
+----------+-----------------------------------------------------------------------------------------------+
| 14       | Visualization event emitted (lossy channel, never blocks).                                    |
+----------+-----------------------------------------------------------------------------------------------+
| 15       | Filtered JSON returned. SSE if Accept: text/event-stream.                                     |
+----------+-----------------------------------------------------------------------------------------------+

### 3.4 The 6-Stage Retrieval Cascade

<!-- Section 3.4 — The 6-Stage Retrieval Cascade -->

The cascade ensures Nexus uses the cheapest safe retrieval path first. Every stage is independently skippable based on policy, destination capability, retrieval profile, and configuration.

+-----------+-------------------------------+-------------------------------------------------------------------+-----------------------------------------------------------------------------------------------------------------------------------+
| **Stage** | **Name**                      | **When Active**                                                   | **V2.2 Notes**                                                                                                                    |
+===========+===============================+===================================================================+===================================================================================================================================+
| 0         | Policy Gate                   | Always runs                                                       | Returns 403 with specific denial reason. Checks CanRead, allowed_operations, allowed_destinations, allowed_retrieval_modes.       |
+-----------+-------------------------------+-------------------------------------------------------------------+-----------------------------------------------------------------------------------------------------------------------------------+
| 1         | Exact Cache                   | policy.read_from_cache = true AND profile != deep                 | Zero-dep LRU. SHA256(scope_hash + dest + params + policy_hash). Scope-partitioned. Watermark freshness check.                     |
+-----------+-------------------------------+-------------------------------------------------------------------+-----------------------------------------------------------------------------------------------------------------------------------+
| 2         | Semantic Cache                | Embedding configured + policy allows + profile = balanced or deep | Cosine similarity >= threshold (default 0.92, configurable per source). Bypassed if embedding provider unreachable.              |
+-----------+-------------------------------+-------------------------------------------------------------------+-----------------------------------------------------------------------------------------------------------------------------------+
| 3         | Structured Lookup             | Metadata filters present OR exact-subject fast path               | Parameterized WHERE clauses. No SQL string concatenation. Exact-subject last-N fast path when query is subject + limit only.      |
+-----------+-------------------------------+-------------------------------------------------------------------+-----------------------------------------------------------------------------------------------------------------------------------+
| 4         | Semantic Retrieval            | Embedding configured + dest.CanSemanticSearch() + profile != fast | sqlite-vec, pgvector, or Supabase RPC. Over-sample factor from profile config.                                                    |
+-----------+-------------------------------+-------------------------------------------------------------------+-----------------------------------------------------------------------------------------------------------------------------------+
| 5         | Hybrid Merge + Temporal Decay | Stages 3 and 4 both produced results                              | Dedup by payload_id. Tiered temporal decay rerank (per-destination/collection settings). Trim to max_results. Projection applied. |
+-----------+-------------------------------+-------------------------------------------------------------------+-----------------------------------------------------------------------------------------------------------------------------------+

### 3.5 Retrieval Profiles

<!-- Section 3.5 — Retrieval Profiles -->

+--------------------+----------------------------------+-----------------+--------------------------+-----------------------------------------------------------------------------------+
| **Profile**        | **Stages Enabled**               | **Over-sample** | **Temporal Decay**       | **Use Case**                                                                      |
+====================+==================================+=================+==========================+===================================================================================+
| fast               | 0, 1, 3                          | N/A             | Off                      | Structured lookup. Lowest latency. Chat history, recent-context queries.          |
+--------------------+----------------------------------+-----------------+--------------------------+-----------------------------------------------------------------------------------+
| balanced (default) | 0, 1, 2, 3, 4, 5                 | 100             | On (half_life_days = 7)  | General-purpose. Semantic + structured with temporal decay.                       |
+--------------------+----------------------------------+-----------------+--------------------------+-----------------------------------------------------------------------------------+
| deep               | 0, 2, 3, 4, 5 (skip exact cache) | 500             | On (half_life_days = 30) | Research, thorough retrieval. Maximum recall. Skip exact cache for fresh results. |
+--------------------+----------------------------------+-----------------+--------------------------+-----------------------------------------------------------------------------------+

### 3.6 Temporal Decay Reranking — Tiered and Policy-Aware

<!-- Section 3.6 — Temporal Decay Reranking — Tiered and Policy-Aware -->

V2.2 extends temporal decay from a global setting to a tiered, policy-aware system. Decay settings can be configured at three levels, with the most specific taking precedence:

4.  Global: [retrieval] section in daemon.toml.

5.  Per-destination: [destination.decay] section in destination TOML.

6.  Per-collection: [destination.decay.collections.<name>] for collection-specific overrides.

#### Algorithm

```text
lambda = ln(2) / half_life_days
recency_weight = exp(-lambda * days_elapsed)
final_score = (cosine_similarity * 0.7) + (recency_weight * 0.3)
```

#### Decay Modes

+-----------------------+-----------------------------------------+--------------------------------------------------------------+
| **Mode**              | **Formula**                             | **Use Case**                                                 |
+=======================+=========================================+==============================================================+
| exponential (default) | exp(-lambda * days)                    | Smooth, continuous decay. General purpose.                   |
+-----------------------+-----------------------------------------+--------------------------------------------------------------+
| step                  | 1.0 if days < threshold, 0.1 otherwise | Hard cutoff. Chat history where old messages are irrelevant. |
+-----------------------+-----------------------------------------+--------------------------------------------------------------+

Scores are deterministic for a given configuration and data set. The same query against the same data with the same config always produces the same ranking.

### 3.7 Semantic Short-Circuit and Fast Paths

<!-- Section 3.7 — Semantic Short-Circuit and Fast Paths -->

#### Semantic Short-Circuit

When the exact cache (Stage 1) or semantic cache (Stage 2) returns a result with confidence above the configured threshold, remaining stages are skipped entirely. This prevents unnecessary database and vector queries for repeated or near-identical requests.

#### Exact-Subject Fast Path

When a query contains only a subject filter and a limit (no free-text search, no metadata filters, no embedding), Nexus bypasses the full cascade and executes a direct indexed query: SELECT * FROM memories WHERE subject = ? ORDER BY timestamp DESC LIMIT ?. This is the lowest-latency retrieval path. The retrieval stage is reported as 'fast_path' in _nexus metadata.

> **ℹ NOTE:** The fast path is automatically selected when the query shape matches. No explicit opt-in required.

### 3.8 Cursor-Based Pagination

<!-- Section 3.8 — Cursor-Based Pagination -->

+---------------+-------------+-------------+-------------------------------------------------------------------------------+
| **Parameter** | **Default** | **Maximum** | **Notes**                                                                     |
+===============+=============+=============+===============================================================================+
| limit         | 20          | 200         | ?limit=0 or omitted = 20. N>200 capped at 200.                               |
+---------------+-------------+-------------+-------------------------------------------------------------------------------+
| cursor        | (none)      | —         | Opaque base64 from _nexus.next_cursor.                                       |
+---------------+-------------+-------------+-------------------------------------------------------------------------------+
| profile       | balanced    | —         | fast, balanced, or deep. Source config default if omitted.                    |
+---------------+-------------+-------------+-------------------------------------------------------------------------------+
| debug_stages  | false       | —         | Requires admin token. Adds _nexus.debug payload.                             |
+---------------+-------------+-------------+-------------------------------------------------------------------------------+
| actor_type    | (none)      | —         | Filter results by provenance. user, agent, or system.                         |
+---------------+-------------+-------------+-------------------------------------------------------------------------------+
| as_of         | (none)      | —         | Time-travel: return state as-of this RFC3339 timestamp. Admin token required. |
+---------------+-------------+-------------+-------------------------------------------------------------------------------+

## Section 4 — WAL Design

<!-- Section 4 — WAL Design -->

### 4.1 WAL Record Structure

<!-- Section 4.1 — WAL Record Structure -->

+-----------------+-------------+------------------------------------------------+
| **Field**       | **Type**    | **Description**                                |
+=================+=============+================================================+
| version         | int         | WAL format version. Currently 2.               |
+-----------------+-------------+------------------------------------------------+
| payload_id      | string      | Unique identifier for the payload.             |
+-----------------+-------------+------------------------------------------------+
| idempotency_key | string      | Client-provided dedup key.                     |
+-----------------+-------------+------------------------------------------------+
| status          | string      | PENDING, DELIVERED, or PERMANENT_FAILURE.      |
+-----------------+-------------+------------------------------------------------+
| timestamp       | RFC3339     | When the entry was written.                    |
+-----------------+-------------+------------------------------------------------+
| source          | string      | Source name from config.                       |
+-----------------+-------------+------------------------------------------------+
| destination     | string      | Target destination name.                       |
+-----------------+-------------+------------------------------------------------+
| subject         | string      | Resolved subject namespace.                    |
+-----------------+-------------+------------------------------------------------+
| actor_type      | string      | user, agent, or system (provenance).           |
+-----------------+-------------+------------------------------------------------+
| actor_id        | string      | Identity of the actor (provenance).            |
+-----------------+-------------+------------------------------------------------+
| payload         | JSON object | The full TranslatedPayload.                    |
+-----------------+-------------+------------------------------------------------+
| crc32           | hex string  | CRC32 checksum (always present).               |
+-----------------+-------------+------------------------------------------------+
| hmac            | hex string  | HMAC-SHA256 (present only when integrity=mac). |
+-----------------+-------------+------------------------------------------------+

#### Unencrypted Layout (integrity=crc32, default)

Each line is: JSON_BYTES<TAB>CRC32_HEX<NEWLINE>. CRC32 computed over JSON bytes. Mismatch on replay: entry skipped with WARN.

#### Unencrypted Layout (integrity=mac)

Each line is: JSON_BYTES<TAB>CRC32_HEX<TAB>HMAC_HEX<NEWLINE>. CRC32 computed over JSON bytes. HMAC-SHA256 computed over JSON bytes using the MAC key loaded from mac_key_file. On replay: CRC32 checked first, then HMAC. Mismatch on either: entry skipped with WARN and bubblefish_wal_integrity_failures_total incremented. Invalid HMAC also triggers a structured security event (event_type: wal_tamper_detected).

#### Encrypted Layout

+---------------+----------+--------------------------------------------------------------------------------------+
| **Component** | **Size** | **Description**                                                                      |
+===============+==========+======================================================================================+
| version       | 1 byte   | WAL encryption format version (currently 1).                                         |
+---------------+----------+--------------------------------------------------------------------------------------+
| key_id        | 4 bytes  | Identifier for the encryption key.                                                   |
+---------------+----------+--------------------------------------------------------------------------------------+
| nonce         | 12 bytes | AES-256-GCM nonce. Unique per entry. Generated via crypto/rand.                      |
+---------------+----------+--------------------------------------------------------------------------------------+
| ciphertext    | variable | AES-256-GCM encrypted JSON payload with authentication tag.                          |
+---------------+----------+--------------------------------------------------------------------------------------+
| crc32         | 4 bytes  | CRC32 over (version + key_id + nonce + ciphertext). Written as 8-char hex after tab. |
+---------------+----------+--------------------------------------------------------------------------------------+

> **⚠ WARNING:** If WAL encryption is configured but the keyfile is missing, empty, or unreadable, Nexus MUST refuse to start.

> **INVARIANT:** Daemon must refuse to start under this condition. Violation is a bug.
>
> **⚠ WARNING:** If WAL integrity=mac is configured but mac_key_file is missing, Nexus MUST refuse to start.

> **INVARIANT:** Daemon must refuse to start under this condition. Violation is a bug.

### 4.2 Segment Rotation

<!-- Section 4.2 — Segment Rotation -->

WAL segments rotate when the current segment exceeds max_segment_size_mb (default 50MB). On crash during rotation, both segments exist on disk. On restart, both segments are replayed. Entries are deduplicated by idempotency key.

### 4.3 MarkDelivered Semantics

<!-- Section 4.3 — MarkDelivered Semantics -->

7.  Read the WAL segment containing the entry.

8.  Rewrite the entry with status = DELIVERED + new CRC32 (+ new HMAC if integrity=mac).

9.  Write to a temp file in filepath.Dir(wal.path) — same filesystem.

10. fsync the temp file.

11. os.Rename(tmpPath, segmentPath) — atomic on same filesystem.

> **⚠ WARNING:** Temp file MUST be in filepath.Dir(wal.path). os.Rename() across filesystems fails with EXDEV.

### 4.4 WAL Health Watchdog

<!-- Section 4.4 — WAL Health Watchdog -->

A background goroutine runs every 30 seconds (configurable) and checks:

-   WAL directory exists and is writable (test write + delete of a probe file).

-   Available disk space exceeds threshold (default 100MB).

-   WAL append latency over the last interval is within bounds (default 100ms p99).

-   WAL pending entry count.

Results exposed via Prometheus metrics and /ready endpoint. If WAL directory becomes unwritable, /ready returns 503 and daemon logs ERROR.

### 4.5 Crash and Replay Semantics

<!-- Section 4.5 — Crash and Replay Semantics -->

+-----------------------------------------+------------------------------+---------------------------------------------------------------------+
| **Crash Point**                         | **Data State**               | **Recovery**                                                        |
+=========================================+==============================+=====================================================================+
| Before WAL append                       | Not persisted.               | Client retries. Same idempotency key = safe.                        |
+-----------------------------------------+------------------------------+---------------------------------------------------------------------+
| After WAL, before queue                 | In WAL (PENDING). Not in DB. | WAL replay. Re-enqueued. Worker writes to destination.              |
+-----------------------------------------+------------------------------+---------------------------------------------------------------------+
| After queue, before destination         | In WAL (PENDING). In queue.  | WAL replay. Re-enqueued. Worker writes.                             |
+-----------------------------------------+------------------------------+---------------------------------------------------------------------+
| After destination, before MarkDelivered | In WAL (PENDING). In DB.     | WAL replay re-enqueues. Destination idempotency prevents duplicate. |
+-----------------------------------------+------------------------------+---------------------------------------------------------------------+
| After MarkDelivered                     | In WAL (DELIVERED). In DB.   | No action. Skipped on replay.                                       |
+-----------------------------------------+------------------------------+---------------------------------------------------------------------+

On startup, replay proceeds: discover segments sorted oldest-first, scan with 10MB buffer, validate CRC32 (then HMAC if integrity=mac, then decrypt if encrypted), skip DELIVERED/PERMANENT_FAILURE entries, rebuild idempotency map from PENDING entries, enqueue PENDING entries for destination write.

> **⚠ WARNING:** In-memory idempotency maps are empty on restart. Rebuilt exclusively from WAL replay.

## Section 5 — Queue Design

<!-- Section 5 — Queue Design -->

+------------------+---------------------------------------------------------------------------------------------------------------+
| **Item**         | **V2.2 Behavior**                                                                                             |
+==================+===============================================================================================================+
| Enqueue()        | Non-blocking select. No mutex. If full: returns ErrLoadShed → 429 queue_full. WAL entry still durable.        |
+------------------+---------------------------------------------------------------------------------------------------------------+
| Drain()          | sync.Once wrapping close(q.done). Safe for multiple calls.                                                    |
+------------------+---------------------------------------------------------------------------------------------------------------+
| DrainWithContext | Respects context.Context for timeout. Returns bool.                                                           |
+------------------+---------------------------------------------------------------------------------------------------------------+
| Worker logging   | slog.Logger injected via Queue struct field. Never nil.                                                       |
+------------------+---------------------------------------------------------------------------------------------------------------+
| Worker delivery  | On success: MarkDelivered. On failure: classify TRANSIENT (backoff retry) or PERMANENT (mark WAL, log ERROR). |
+------------------+---------------------------------------------------------------------------------------------------------------+
| Queue size       | Configurable via daemon.toml queue_size (default 10000). Hard maximum at enqueue.                             |
+------------------+---------------------------------------------------------------------------------------------------------------+

## Section 6 — Security Specifications

<!-- Section 6 — Security Specifications -->

### 6.1 Authentication and Authorization

<!-- Section 6.1 — Authentication and Authorization -->

-   Per-source API keys bound to source identity at config time. No shared keys.

-   All resolved keys cached at startup. Zero os.Getenv() on auth hot path.

-   ALL token comparisons use subtle.ConstantTimeCompare.

-   **Admin vs data token separation:** Admin token accepted ONLY on admin endpoints. Data keys accepted ONLY on data endpoints. Cross-use returns 401 wrong_token_class.

-   MCP server authenticates via dedicated mcp_key. Binds 127.0.0.1 only.

-   Web dashboard requires admin_token by default.

-   CanRead and CanWrite flags enforced in handlers — return 403 with specific error.

-   Empty resolved API key fails build (SCHEMA_ERROR).

### 6.2 TLS/mTLS Configuration

<!-- Section 6.2 — TLS/mTLS Configuration -->

[daemon.tls]

enabled = false

cert_file = "file:/path/to/cert.pem"

key_file = "file:/path/to/key.pem"

min_version = "1.2"

max_version = "1.3"

client_ca_file = ""

client_auth = "no_client_cert"

> **⚠ WARNING:** If TLS enabled but cert_file or key_file missing/unreadable, Nexus MUST refuse to start.

> **INVARIANT:** Daemon must refuse to start under this condition. Violation is a bug.

### 6.3 Trusted Proxies

<!-- Section 6.3 — Trusted Proxies -->

[daemon.trusted_proxies]

cidrs = ["127.0.0.1/32", "::1/128"]

forwarded_headers = ["X-Forwarded-For", "X-Real-IP"]

Requests from trusted CIDRs: forwarded headers read for effective client IP. Non-trusted: TCP source IP used. Never add 0.0.0.0/0 (bubblefish lint warns).

### 6.4 WAL Integrity and Encryption

<!-- Section 6.4 — WAL Integrity and Encryption -->

#### 6.4.1 Integrity Modes

<!-- Section 6.4.1 — Integrity Modes -->

[daemon.wal.integrity]

mode = "crc32" \# crc32 (default) or mac

mac_key_file = "file:/path/to/mac.key" \# 32-byte HMAC-SHA256 key

CRC32 detects accidental corruption. MAC mode adds HMAC-SHA256 for tamper detection. Entries with invalid MAC on replay are skipped with WARN and a structured security event is emitted.

#### 6.4.2 Encryption

<!-- Section 6.4.2 — Encryption -->

[daemon.wal.encryption]

enabled = false

key_file = "file:/path/to/wal.key" \# 32-byte AES-256 key

When enabled, every WAL entry encrypted with AES-256-GCM using a unique 12-byte nonce per entry. CRC covers the encrypted form.

### 6.5 Config Signing

<!-- Section 6.5 — Config Signing -->

[daemon.signing]

enabled = false

key_file = "file:/path/to/signing.key"

When enabled, daemon verifies digital signatures on all compiled config files (sources.json, destinations.json, policies.json, cache_rules.json) at startup and on hot reload. Unsigned or invalid signatures cause the daemon to refuse to start (or refuse to reload, logging ERROR).

Signing is performed offline via:

bubblefish sign-config --keyref env:NEXUS_SIGNING_KEY

This command runs on a locked-down machine so signing keys do not need to reside on every host running Nexus.

### 6.6 OAuth Edge Integration

<!-- Section 6.6 — OAuth Edge Integration -->

Nexus does not implement OAuth internally. OAuth is handled at the edge (reverse proxy or API gateway). Two documented patterns:

#### Pattern A: Reverse Proxy (Recommended)

Caddy/Traefik/nginx verifies OAuth/OIDC tokens and injects an X-Nexus-Source header or maps token claims to a per-source API key. Nexus sees only its own API keys.

#### Pattern B: JWT Header Mapping (Advanced)

Nexus can be configured to accept a signed JWT in a header, validate it with a configured JWKS URI, and map a claim (e.g., sub or group) to a source/tenant. Requires careful key management.

[daemon.jwt]

enabled = false

jwks_url = ""

claim_to_source = "sub"

audience = ""

> **⚠ WARNING:** JWT mapping is marked advanced. Most deployments should use the reverse proxy pattern.

### 6.7 Config Lint (bubblefish lint)

<!-- Section 6.7 — Config Lint (bubblefish lint) -->

Validates configuration and warns about dangerous or suboptimal settings:

-   Missing idempotency configuration on any source.

-   Bind address set to 0.0.0.0 without TLS enabled.

-   Unbounded or disabled rate limits.

-   Empty or literal API keys in non-simple-mode config.

-   WAL encryption/integrity enabled but keyfile missing.

-   TLS enabled but cert/key files missing.

-   Trusted proxies containing 0.0.0.0/0 or ::/0.

-   Source referencing unknown destination.

-   Duplicate resolved API keys across sources.

-   Event sinks with no retry configuration.

-   Unsigned config files when signing mode is enabled.

### 6.8 Threat Model

<!-- Section 6.8 — Threat Model -->

#### In Scope

-   **Local network attackers:** Mitigated by localhost bindings, per-source keys, constant-time auth, optional TLS/mTLS.

-   **Network eavesdroppers:** Mitigated by TLS/mTLS when configured. Default localhost binding means traffic never leaves the machine.

-   **Disk theft:** Mitigated by WAL encryption, 0600/0700 permissions, OS disk encryption (recommended).

-   **WAL/config tampering:** Mitigated by HMAC integrity mode and config signing. Tampered entries detected and skipped.

-   **Accidental secret exposure:** Mitigated by never logging secret values, env:/file: references, config lint warnings.

#### Out of Scope

-   **Compromised host:** If an attacker has root, Nexus cannot protect data.

-   **Hostile hypervisor:** Side-channel attacks from the hypervisor layer are not mitigated.

-   **Supply chain attacks:** Nexus pins dependencies and audits licenses but does not defend against compromised Go modules.

-   **DDoS:** Rate limiting provides basic protection. Volumetric attacks require upstream mitigation.

## Section 7 — Data Contracts

<!-- Section 7 — Data Contracts -->

### 7.1 Canonical Write Envelope

<!-- Section 7.1 — Canonical Write Envelope -->

```go
type TranslatedPayload struct {

PayloadID string

RequestID string // UUID at HTTP ingress

Source string

Subject string // X-Subject header or namespace

Namespace string

Destination string

Collection string

Content string

Model string

Role string

Timestamp time.Time

IdempotencyKey string

SchemaVersion int

TransformVersion string

ActorType string // V2.2: user, agent, or system

ActorID string // V2.2: identity of the actor

Embedding []float32 // optional

Metadata map[string]string // extra mapping fields

}

#### Provenance Semantics

-   **user:** Memory from a human user's input or preferences.

-   **agent:** Memory from an AI agent's reasoning or actions.

-   **system:** Memory from system configuration, metadata sync, or automated processes.

Provenance fields are stored in WAL, written to destinations, and available as filters in structured lookup (Stage 3) and the conflict inspector.

### 7.2 _nexus Response Metadata

<!-- Section 7.2 — _nexus Response Metadata -->

```go
type NexusMetadata struct {

Stage string // e.g. 'structured', 'exact_cache', 'fast_path'

SemanticUnavailable bool

SemanticUnavailableReason string

ResultCount int

Truncated bool

NextCursor string

HasMore bool

TemporalDecayApplied bool

TemporalDecayMode string // 'exponential' or 'step'

ConsistencyScore float64 // 0.0-1.0

Profile string // fast, balanced, or deep

}

### 7.3 _nexus.debug Payload (Optional)

<!-- Section 7.3 — _nexus.debug Payload (Optional) -->

Available when debug_stages=true query param + admin token. Omitted silently for data-plane tokens.

```go
type NexusDebug struct {

StagesHit []string

CandidatesPerStage map[string]int

PerStageLatencyMs map[string]float64

CacheHit bool

CacheType string

TemporalDecayConfig struct {

Mode string

HalfLifeDays float64

OverSampleFactor int

}

TotalLatencyMs float64

}

### 7.4 Error Response Format

<!-- Section 7.4 — Error Response Format -->

{ "error": "rate_limit_exceeded", "message": "too many requests", "retry_after_seconds": 30, "details": {} }

+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| **Status** | **error**                     | **When**                                 | **Durable?** | **Client Action**          |
+============+===============================+==========================================+==============+============================+
| 200        | (success)                     | Write accepted or read returned          | Yes (WAL)    | Proceed.                   |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| 401        | unauthorized                  | Invalid/missing key or wrong_token_class | No           | Fix credentials.           |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| 403        | source_not_permitted_to_write | CanWrite = false                         | No           | Fix source config.         |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| 403        | source_not_permitted_to_read  | CanRead = false                          | No           | Fix source config.         |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| 403        | policy_denied                 | Policy denies operation                  | No           | Fix policy config.         |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| 413        | payload_too_large             | Exceeds max_bytes                        | No           | Reduce payload size.       |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| 429        | rate_limit_exceeded           | Rate limit hit                           | No           | Back off per Retry-After.  |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| 429        | queue_full                    | Load shed                                | Yes (WAL)    | Back off. WAL will replay. |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| 500        | internal_error                | WAL failure, unexpected error            | Depends      | Operator: check logs.      |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+
| 503        | destination_unavailable       | Circuit breaker open                     | Yes (WAL)    | Back off. Auto-heals.      |
+------------+-------------------------------+------------------------------------------+--------------+----------------------------+

## Section 8 — Failure Contracts

<!-- Section 8 — Failure Contracts -->

### 8.1 Write Path Failure Contract

<!-- Section 8.1 — Write Path Failure Contract -->

+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| **Failure**                  | **Inside Nexus**                  | **Client Sees**           | **Durable?**                         | **Client Action**            |
+==============================+===================================+===========================+======================================+==============================+
| Auth failure                 | Rejected. No WAL.                 | 401                       | No                                   | Fix credentials.             |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| Policy denial                | Rejected after auth.              | 403                       | No                                   | Fix config.                  |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| Payload too large            | Rejected before body read.        | 413                       | No                                   | Reduce size.                 |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| Duplicate (idempotent)       | Detected before rate limit.       | 200 + original payload_id | Already durable                      | Proceed (safe to retry).     |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| Rate limited                 | Rejected after idempotency.       | 429 + Retry-After         | No                                   | Back off, retry.             |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| WAL append fails             | Logged ERROR. Metric incremented. | 500                       | NOT durable                          | Retry. Operator: check disk. |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| Queue full                   | WAL written. Channel full.        | 429 queue_full            | DURABLE in WAL                       | Back off. Data safe.         |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| Dest write fails (transient) | Backoff retry.                    | 200 already returned      | DURABLE. Retrying.                   | Nothing.                     |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| Dest write fails (permanent) | WAL marked PERMANENT_FAILURE.     | 200 already returned      | DURABLE but not delivered            | Operator: investigate.       |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| Crash post-WAL               | In WAL (PENDING).                 | Connection dropped        | DURABLE. Replayed.                   | Retry (idempotent).          |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| MarkDelivered failure        | Logged WARN.                      | 200 already returned      | DURABLE and delivered                | Nothing.                     |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+
| Event sink failure           | Sink retries independently.       | 200 already returned      | DURABLE. Sink eventually consistent. | Nothing.                     |
+------------------------------+-----------------------------------+---------------------------+--------------------------------------+------------------------------+

### 8.2 Read Path Failure Contract

<!-- Section 8.2 — Read Path Failure Contract -->

+-------------------------+-----------------------+---------------------------------+-------------------------------+
| **Failure**             | **Inside Nexus**      | **Client Sees**                 | **Client Action**             |
+=========================+=======================+=================================+===============================+
| Auth failure            | Rejected.             | 401                             | Fix credentials.              |
+-------------------------+-----------------------+---------------------------------+-------------------------------+
| Policy denial           | Rejected.             | 403                             | Fix config.                   |
+-------------------------+-----------------------+---------------------------------+-------------------------------+
| Rate limited            | Rejected.             | 429 + Retry-After               | Back off.                     |
+-------------------------+-----------------------+---------------------------------+-------------------------------+
| Destination unreachable | Circuit breaker open. | 503                             | Back off. Auto-heals.         |
+-------------------------+-----------------------+---------------------------------+-------------------------------+
| Embedding unreachable   | Stages 2+4 bypassed.  | 200 + semantic_unavailable=true | Results may be less relevant. |
+-------------------------+-----------------------+---------------------------------+-------------------------------+
| Internal error          | Logged ERROR.         | 500                             | Retry. Operator: check logs.  |
+-------------------------+-----------------------+---------------------------------+-------------------------------+

## Section 9 — Configuration Design

<!-- Section 9 — Configuration Design -->

### 9.1 Directory Structure

<!-- Section 9.1 — Directory Structure -->

~/.bubblefish/Nexus/ (0700)

daemon.toml (0600)

sources/ (0700)

claude.toml (0600)

ollama.toml (0600)

default.toml (0600, Simple Mode)

destinations/ (0700)

sqlite.toml (0600)

openbrain.toml (0600, optional)

policies/ (0700, optional)

compiled/ (0700)

sources.json (0600)

destinations.json (0600)

policies.json (0600)

cache_rules.json (0600)

*.sig (0600, when signing enabled)

wal/ (0700)

wal-current.jsonl (0600)

backups/ (0700)

examples/ (0755)

oauth/ (example Caddy/Traefik configs)

openwebui-provider.json (example Open WebUI config)

blessed/ (pre-built integration configs)

logs/

bubblefish.log

security-events.jsonl (structured security events)

### 9.2 daemon.toml (V2.2 Complete)

<!-- Section 9.2 — daemon.toml (V2.2 Complete) -->

```toml
[daemon]

port = 8080

bind = "127.0.0.1"

admin_token = "env:ADMIN_TOKEN"

log_level = "info"

log_format = "json"

mode = "balanced" \# safe, balanced, or fast

queue_size = 10000

[daemon.shutdown]

drain_timeout_seconds = 30

[daemon.wal]

path = "~/.bubblefish/Nexus/wal"

max_segment_size_mb = 50

[daemon.wal.integrity]

mode = "crc32" \# crc32 or mac

mac_key_file = ""

[daemon.wal.encryption]

enabled = false

key_file = ""

[daemon.wal.watchdog]

interval_seconds = 30

min_disk_bytes = 104857600

max_append_latency_ms = 100

[daemon.rate_limit]

global_requests_per_minute = 2000

[daemon.embedding]

enabled = false

provider = "openai"

url = "env:EMBEDDING_URL"

api_key = "env:EMBEDDING_API_KEY"

model = "text-embedding-3-small"

dimensions = 1536

timeout_seconds = 10

[daemon.mcp]

enabled = true

port = 8082

bind = "127.0.0.1"

source_name = "mcp"

api_key = "env:MCP_API_KEY"

[daemon.web]

port = 8081

require_auth = true

[daemon.tls]

enabled = false

cert_file = ""

key_file = ""

min_version = "1.2"

max_version = "1.3"

client_ca_file = ""

client_auth = "no_client_cert"

[daemon.trusted_proxies]

cidrs = ["127.0.0.1/32", "::1/128"]

forwarded_headers = ["X-Forwarded-For", "X-Real-IP"]

[daemon.signing]

enabled = false

key_file = ""

[daemon.jwt]

enabled = false

jwks_url = ""

claim_to_source = "sub"

audience = ""

[daemon.events]

enabled = false

max_inflight = 1000

retry_backoff_seconds = [1, 5, 30, 300]

[[daemon.events.sinks]]

name = "audit"

url = "https://example.com/webhook/nexus"

timeout_seconds = 5

max_retries = 5

content = "summary" \# summary (IDs only) or full

[retrieval]

time_decay = true

half_life_days = 7

decay_mode = "exponential" \# exponential or step

over_sample_factor = 100

default_profile = "balanced"

[consistency]

enabled = true

interval_seconds = 300

sample_size = 100

[security_events]

enabled = true

log_file = "~/.bubblefish/Nexus/logs/security-events.jsonl"

```

### 9.3 Source TOML (V2.2 Complete)

<!-- Section 9.3 — Source TOML (V2.2 Complete) -->

```toml
[source]

name = "claude"

api_key = "env:CLAUDE_SOURCE_KEY"

namespace = "claude"

can_read = true

can_write = true

target_destination = "sqlite"

default_actor_type = "user"

default_actor_id = ""

default_profile = "balanced"

[source.rate_limit]

requests_per_minute = 2000

[source.payload_limits]

max_bytes = 10485760

[source.mapping]

content = "message.content"

model = "model"

role = "message.role"

[source.transform]

content = ["trim"]

model = ["coalesce:unknown"]

[source.idempotency]

enabled = true

dedup_window_seconds = 300

[source.policy]

allowed_destinations = ["sqlite", "openbrain"]

allowed_operations = ["write", "read", "search"]

allowed_retrieval_modes = ["exact", "structured", "semantic", "hybrid"]

allowed_profiles = ["fast", "balanced", "deep"]

max_results = 50

max_response_bytes = 65536

[source.policy.field_visibility]

include_fields = ["content", "source", "role", "timestamp", "model", "actor_type", "actor_id"]

strip_metadata = true

[source.policy.cache]

read_from_cache = true

write_to_cache = true

max_ttl_seconds = 300

semantic_similarity_threshold = 0.92

[source.policy.decay]

\# Per-source decay override (optional)

\# half_life_days = 14

\# decay_mode = "step"

\# step_threshold_days = 30

```

## Section 10 — Event Sink (Optional Webhooks)

<!-- Section 10 — Event Sink (Optional Webhooks) -->

V2.2 adds an optional event sink system that tails the WAL after writes complete. This is a thin notification layer, not a full event bus. It never blocks the write path.

### 10.1 Architecture

<!-- Section 10.1 — Architecture -->

-   Separate goroutine pipeline running independently from the main write/read paths.

-   Reads from a lossy buffered channel (capacity = max_inflight, default 1000). If channel full, events are dropped and bubblefish_events_dropped_total metric incremented.

-   Each configured sink gets its own retry goroutine with exponential backoff.

-   Sinks receive summarized event payloads by default (payload_id, source, subject, destination, timestamp, actor_type). Full content only when content = 'full' is explicitly configured.

### 10.2 Event Payload

<!-- Section 10.2 — Event Payload -->

{

"event_type": "memory_written",

"payload_id": "abc123",

"source": "claude",

"subject": "user:shawn",

"destination": "sqlite",

"timestamp": "2026-04-01T12:00:00Z",

"actor_type": "user",

"actor_id": "shawn"

}

### 10.3 Failure Behavior

<!-- Section 10.3 — Failure Behavior -->

Sink failures do not affect the main pipeline. Failed deliveries are retried with exponential backoff (configurable). After max_retries, the event is dropped and logged as WARN. Sink health is exposed via Prometheus metrics.

> **⚠ WARNING:** Event sinks are eventually consistent. They are not a durability mechanism. The WAL and destination remain the sources of truth.

## Section 11 — Observability

<!-- Section 11 — Observability -->

### 11.1 Structured Logging

<!-- Section 11.1 — Structured Logging -->

+---------------------+-------------------------------------------------------------------------------+
| **Field**           | **Description**                                                               |
+=====================+===============================================================================+
| time                | RFC3339 timestamp.                                                            |
+---------------------+-------------------------------------------------------------------------------+
| level               | DEBUG, INFO, WARN, or ERROR.                                                  |
+---------------------+-------------------------------------------------------------------------------+
| msg                 | Human-readable message.                                                       |
+---------------------+-------------------------------------------------------------------------------+
| component           | Package/module name (wal, queue, daemon, cache, retrieval, events, security). |
+---------------------+-------------------------------------------------------------------------------+
| source              | Source name (when applicable).                                                |
+---------------------+-------------------------------------------------------------------------------+
| subject             | Subject namespace (when applicable).                                          |
+---------------------+-------------------------------------------------------------------------------+
| request_id          | UUID assigned at HTTP ingress.                                                |
+---------------------+-------------------------------------------------------------------------------+
| effective_client_ip | Derived from trusted proxy headers or TCP source.                             |
+---------------------+-------------------------------------------------------------------------------+
| latency_ms          | Request processing time.                                                      |
+---------------------+-------------------------------------------------------------------------------+
| status_code         | HTTP response status.                                                         |
+---------------------+-------------------------------------------------------------------------------+
| error_code          | Machine-readable error code.                                                  |
+---------------------+-------------------------------------------------------------------------------+

### 11.2 Structured Security Events

<!-- Section 11.2 — Structured Security Events -->

When security_events.enabled = true, Nexus writes a dedicated security event log (JSON lines) to the configured log_file. Events include:

-   auth_failure: Invalid API key or wrong token class. Fields: source, ip, token_class, endpoint.

-   policy_denied: Policy gate rejection. Fields: source, subject, operation, destination, reason.

-   rate_limit_hit: Rate limit exceeded. Fields: source, ip, requests_per_minute.

-   wal_tamper_detected: HMAC mismatch on WAL entry. Fields: line_number, segment_file.

-   config_signature_invalid: Config signature verification failed. Fields: file, expected_hash.

-   admin_access: Admin endpoint accessed. Fields: endpoint, ip, user_agent.

This log is designed for forwarding to external SIEM systems (Filebeat, fluentd, Loki, Splunk) for anomaly detection and audit trails.

### 11.3 Prometheus Metrics

<!-- Section 11.3 — Prometheus Metrics -->

+-----------------------------------------------+-----------+------------------------------------------------+
| **Metric**                                    | **Type**  | **Description**                                |
+===============================================+===========+================================================+
| bubblefish_payload_processing_latency_seconds | Histogram | Full write path latency by source.             |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_read_latency_seconds               | Histogram | Full read path latency by source and endpoint. |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_throughput_per_source_total        | Counter   | Successful writes by source.                   |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_errors_total                       | Counter   | Errors by type label.                          |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_queue_depth                        | Gauge     | Current queue depth.                           |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_queue_processing_rate              | Counter   | Payloads dequeued per second.                  |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_cache_exact_hits_total             | Counter   | Stage 1 cache hits.                            |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_cache_exact_misses_total           | Counter   | Stage 1 cache misses.                          |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_cache_semantic_hits_total          | Counter   | Stage 2 cache hits.                            |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_cache_semantic_misses_total        | Counter   | Stage 2 cache misses.                          |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_retrieval_stage_total              | Counter   | Requests served per cascade stage.             |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_embedding_latency_seconds          | Histogram | Embedding provider call duration.              |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_wal_pending_entries                | Gauge     | WAL entries not yet DELIVERED.                 |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_wal_disk_bytes_free                | Gauge     | Free disk on WAL partition.                    |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_wal_healthy                        | Gauge     | 1 if WAL watchdog healthy, 0 if not.           |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_wal_append_latency_seconds         | Histogram | WAL append + fsync latency.                    |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_wal_crc_failures_total             | Counter   | CRC32 mismatches on replay.                    |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_wal_integrity_failures_total       | Counter   | HMAC mismatches on replay (integrity=mac).     |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_temporal_decay_applied_total       | Counter   | Reranking applied in Stage 5.                  |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_consistency_score                  | Gauge     | Latest consistency assertion score (0.0-1.0).  |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_config_lint_warnings               | Gauge     | Number of config lint warnings.                |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_visualization_events_dropped_total | Counter   | Pipeline viz events dropped.                   |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_events_dropped_total               | Counter   | Event sink events dropped (channel full).      |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_events_delivered_total             | Counter   | Event sink events successfully delivered.      |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_events_failed_total                | Counter   | Event sink delivery failures.                  |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_replay_entries_total               | Counter   | WAL entries processed during replay.           |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_replay_duration_seconds            | Gauge     | Time spent on WAL replay at startup.           |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_auth_failures_total                | Counter   | Auth failures by source label.                 |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_policy_denials_total               | Counter   | Policy denials by source and reason labels.    |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_rate_limit_hits_total              | Counter   | Rate limit hits by source label.               |
+-----------------------------------------------+-----------+------------------------------------------------+
| bubblefish_admin_calls_total                  | Counter   | Admin endpoint calls by endpoint label.        |
+-----------------------------------------------+-----------+------------------------------------------------+

### 11.4 Health Endpoints

<!-- Section 11.4 — Health Endpoints -->

+--------------+-----------------------------------------------+--------------------------------------------------------------------------------------------------+
| **Endpoint** | **Semantics**                                 | **Unhealthy Conditions**                                                                         |
+==============+===============================================+==================================================================================================+
| /health      | Liveness. 200 if process running.             | Process crash.                                                                                   |
+--------------+-----------------------------------------------+--------------------------------------------------------------------------------------------------+
| /ready       | Readiness. 200 if all critical subsystems OK. | WAL unwritable. Destination unreachable (5s). WAL watchdog unhealthy. Severe config lint errors. |
+--------------+-----------------------------------------------+--------------------------------------------------------------------------------------------------+

### 11.5 Consistency Assertions

<!-- Section 11.5 — Consistency Assertions -->

Background goroutine every consistency.interval_seconds (default 300s):

12. Sample consistency.sample_size (default 100) DELIVERED WAL entries.

13. Query destination to verify each sampled payload exists.

14. Compute consistency_score = found / sampled.

15. Expose via Prometheus (bubblefish_consistency_score) and /api/status.

16. If score < 0.95: log WARN. If score < 0.80: log ERROR.

Consistency assertions are read-only. They never modify WAL or destination data.

## Section 12 — HTTP API

<!-- Section 12 — HTTP API -->

+-----------------------+------------+----------+-----------------------------------------------------------+
| **Endpoint**          | **Method** | **Auth** | **Description**                                           |
+=======================+============+==========+===========================================================+
| /inbound/{source}     | POST       | Data key | Write path. Full pipeline. Returns payload_id.            |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /v1/memories          | POST       | Data key | OpenAI-compatible write. Accepts messages array.          |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /query/{destination}  | GET        | Data key | Read path. 6-stage cascade. Cursor pagination. SSE.       |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/status           | GET        | Admin    | Queue, destination, replay, cache, consistency stats.     |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/cache            | GET        | Admin    | Cache stats: hits, misses, invalidations, watermarks.     |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/policies         | GET        | Admin    | Compiled policy hashes and summaries.                     |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/replay           | POST       | Admin    | Trigger WAL segment replay.                               |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/lint             | GET        | Admin    | Config lint results.                                      |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/viz/events       | GET (SSE)  | Admin    | Live pipeline visualization event stream.                 |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/conflicts        | GET        | Admin    | Conflict inspector: contradictory memories.               |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/timetravel       | GET        | Admin    | Time-travel: state as-of a timestamp (?as_of=RFC3339).    |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/demo/reliability | POST       | Admin    | Trigger reliability demo.                                 |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/security/events  | GET        | Admin    | Recent structured security events.                        |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /api/security/summary | GET        | Admin    | Auth failures, policy denials, rate limit hits by source. |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /metrics              | GET        | Admin    | Prometheus metrics (private registry).                    |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /health               | GET        | None     | Liveness probe.                                           |
+-----------------------+------------+----------+-----------------------------------------------------------+
| /ready                | GET        | None     | Readiness probe.                                          |
+-----------------------+------------+----------+-----------------------------------------------------------+

## Section 13 — Admin UX

<!-- Section 13 — Admin UX -->

### 13.1 CLI Commands

<!-- Section 13.1 — CLI Commands -->

+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| **Command**               | **Description**                                                                                                                                                              |
+===========================+==============================================================================================================================================================================+
| bubblefish install        | Install wizard. --mode simple, --dest sqlite/postgres/openbrain, --profile openwebui, --oauth-template caddy/traefik.                                                    |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish start          | Start daemon + MCP + web dashboard + tray.                                                                                                                                   |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish dev            | Same daemon as start with dev-friendly defaults: log_level=debug, auto-reload on source TOML changes, prints all effective config paths. Does not change pipeline semantics. |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish build          | Compile config to compiled/ JSON.                                                                                                                                            |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish doctor         | Check daemon, destinations, disk space, config. Ollama-specific diagnostics. Postgres/OpenBrain connectivity check.                                                          |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish lint           | Config lint: dangerous binds, missing idempotency, unbounded limits, unsigned configs in signed mode.                                                                        |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish mcp test       | MCP self-test: starts MCP, sends nexus_status, exits 0 on success.                                                                                                           |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish bench          | --mode throughput (writes), --mode latency (reads), --mode eval (retrieval quality against known-good results).                                                           |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish demo           | Reliability demo: writes 50 memories, kills daemon, restarts, verifies 50 present with 0 duplicates.                                                                         |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish backup create  | --dest /backups/nexus-YYYYMMDD. Snapshots config, compiled, WAL, optionally SQLite DB.                                                                                      |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish backup restore | --from /backups/nexus-YYYYMMDD. Restores config, compiled, WAL, and optionally DB.                                                                                          |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish sign-config    | --keyref env:NEXUS_SIGNING_KEY. Signs compiled config files for signed-mode deployments.                                                                                    |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish policy         | Display compiled policy summaries.                                                                                                                                           |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+
| bubblefish version        | Print version string.                                                                                                                                                        |
+---------------------------+------------------------------------------------------------------------------------------------------------------------------------------------------------------------------+

### 13.2 Web Dashboard

<!-- Section 13.2 — Web Dashboard -->

Runs on daemon.web.port (default 8081). Requires admin_token authentication.

#### Status Overview

Daemon health, queue depth, WAL pending, consistency score, destination status, cache hit ratios. Auto-refreshes.

#### Security Tab

Sources and their policies (can_read/can_write, allowed destinations, max_results/response_bytes). Last N failed auth attempts per source (401/403 breakdown). Config lint warnings with severity indicators. Read-only; config edits remain via TOML/CLI.

#### Live Pipeline Visualization

Real-time view of queries traversing the cascade. Lossy event channel (capacity 1000). If full, events dropped. Dashboard consumes via SSE. Pipeline boxes show each stage with timing and hit/miss.

#### Black Box Mode

Dashboard toggle showing only external behavior: request in, response out, status code, latency. Internal stages hidden.

#### Conflict Inspector

Read-only view identifying contradictory memories: multiple entries for the same subject + entity with different content. Groups by subject/entity, flags divergent content. Filterable by source, subject, time range, actor_type.

#### Time-Travel View

Query memories as-of a specific timestamp via WHERE timestamp <= ? ORDER BY timestamp DESC. Read-only.

### 13.3 Reliability Demo

<!-- Section 13.3 — Reliability Demo -->

Available via CLI (bubblefish demo) and dashboard (/api/demo/reliability):

17. Write 50 memories with unique idempotency keys.

18. Simulate crash (SIGKILL).

19. Restart (automatic WAL replay).

20. Query for all 50.

21. Assert: exactly 50 results, 0 duplicates, 0 data loss.

### 13.4 bubblefish bench

<!-- Section 13.4 — bubblefish bench -->

-   **--mode throughput:** N concurrent writes. Measures requests/sec, p50/p95/p99 latency, WAL append latency, queue depth.

-   **--mode latency:** N sequential reads. Per-stage latency breakdown, cache hit ratio.

-   **--mode eval:** Compares retrieval results against known-good JSON. Measures precision, recall, MRR, NDCG.

### 13.5 Blessed Integration Configs

<!-- Section 13.5 — Blessed Integration Configs -->

Pre-built, security-reviewed config templates shipped in examples/blessed/:

-   **Claude Desktop (MCP):** Minimal MCP scope, single source, conservative policies (max_results=20, max_response_bytes=16384), read-only variant available.

-   **Claude Code / Agents:** HTTP API usage patterns, safe defaults, recommended idempotency key format.

-   **Open WebUI:** Ollama source template with correct mapping paths for Open WebUI payload shape.

-   **Perplexity:** HTTP source template with appropriate rate limits and field mapping.

### 13.6 Reference Architectures

<!-- Section 13.6 — Reference Architectures -->

Documented deployment patterns with sample configs:

-   **Single Developer Laptop:** SQLite, no TLS, no encryption, minimal metrics, Simple Mode install. bubblefish install --mode simple.

-   **Home Lab:** Postgres+pgvector, Prometheus+Grafana, Caddy/nginx reverse proxy with TLS, per-source policies, event sink to local webhook. bubblefish install --dest postgres --mode balanced.

-   **Air-Gapped Environment:** No outbound embedding calls, embedding disabled, signed configs, MAC integrity mode, local-only metrics, no event sinks. bubblefish install --dest sqlite --mode safe.

## Section 14 — Hot Reload, Shutdown, MCP, and Embedding

<!-- Section 14 — Hot Reload, Shutdown, MCP, and Embedding -->

### 14.1 Hot Reload

<!-- Section 14.1 — Hot Reload -->

+----------------------------+-----------------------------------------------------------------------------------------------------------------------------------------------------------+
| **Scope**                  | **Behavior**                                                                                                                                              |
+============================+===========================================================================================================================================================+
| Source config changes      | Applied atomically via configMu.Lock(). Auth uses RLock(). In-flight handlers complete with old config. Config signatures re-verified if signing enabled. |
+----------------------------+-----------------------------------------------------------------------------------------------------------------------------------------------------------+
| Destination config changes | WARN logged. Old config remains. Restart required.                                                                                                        |
+----------------------------+-----------------------------------------------------------------------------------------------------------------------------------------------------------+
| Compiled JSON output       | Written atomically: temp file + fsync + os.Rename(). Signed if signing enabled.                                                                           |
+----------------------------+-----------------------------------------------------------------------------------------------------------------------------------------------------------+
| watchLoop shutdown         | stopReload channel closed during shutdown. Goroutine exits cleanly.                                                                                       |
+----------------------------+-----------------------------------------------------------------------------------------------------------------------------------------------------------+

### 14.2 Shutdown

<!-- Section 14.2 — Shutdown -->

totalTimeout := drain_timeout_seconds // e.g. 30s

Stage 1 (10s): HTTP server graceful shutdown

Stage 2 (10s): Queue drain via DrainWithContext

Stage 3 (10s): WAL close + event sink drain + plugin close

### 14.3 MCP Server

<!-- Section 14.3 — MCP Server -->

+-----------------+--------------------------------------------------------------------------------------------+
| **Item**        | **Specification**                                                                          |
+=================+============================================================================================+
| Bind            | 127.0.0.1 ONLY. Never 0.0.0.0.                                                             |
+-----------------+--------------------------------------------------------------------------------------------+
| Tools           | nexus_write, nexus_search, nexus_status                                                    |
+-----------------+--------------------------------------------------------------------------------------------+
| Auth            | Dedicated mcp_key. Constant-time comparison.                                               |
+-----------------+--------------------------------------------------------------------------------------------+
| Pipeline        | Internal pipeline — not HTTP round-trip. Same auth, policy, WAL, queue, cascade as HTTP. |
+-----------------+--------------------------------------------------------------------------------------------+
| Startup failure | Does NOT crash daemon. Log WARN. HTTP continues.                                           |
+-----------------+--------------------------------------------------------------------------------------------+
| Self-test       | bubblefish mcp test: exits 0 within 5 seconds.                                             |
+-----------------+--------------------------------------------------------------------------------------------+

### 14.4 Embedding Provider

<!-- Section 14.4 — Embedding Provider -->

Supports any OpenAI-compatible endpoint. Does not bundle embedding models. When not configured, Stages 2+4 bypassed with _nexus.semantic_unavailable = true.

Providers: openai, ollama (localhost:11434), compatible (any /v1/embeddings endpoint).

### 14.5 Backup and Restore

<!-- Section 14.5 — Backup and Restore -->

bubblefish backup create snapshots:

-   All TOML config files (daemon.toml, sources/, destinations/, policies/).

-   All compiled/ JSON files (and .sig files if signing enabled).

-   WAL directory (all segment files).

-   SQLite database file (using sqlite3 .backup API for crash-safe snapshot). Optional flag: --include-db.

For Postgres destinations: documented integration with pg_dump and pg_basebackup.

bubblefish backup restore reads the backup directory and restores all components to their expected locations, verifying file integrity.

## Section 15 — V2.2 Feature Set

<!-- Section 15 — V2.2 Feature Set -->

+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| **Feature**                    | **Description**                                                                                            | **Priority** |
+================================+============================================================================================================+==============+
| 6-Stage Retrieval Cascade      | Policy → exact cache → semantic cache → structured → semantic → hybrid merge + temporal decay + projection | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Retrieval Profiles             | fast, balanced, deep with per-source stage toggles                                                         | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Tiered Temporal Decay          | Per-destination/collection decay settings, exponential and step modes                                      | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| MCP Server                     | nexus_write, nexus_search, nexus_status. Claude Desktop/Cursor.                                            | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| WAL CRC32 Checksums            | 4-byte CRC32 on every entry                                                                                | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| WAL HMAC Integrity             | Optional HMAC-SHA256 for tamper detection                                                                  | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| WAL Encryption                 | Optional AES-256-GCM with per-entry nonce                                                                  | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Config Signing                 | bubblefish sign-config for signed-mode deployments                                                         | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Zero-Dep LRU Cache             | Go generics. map + container/list                                                                          | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Constant-Time Auth             | subtle.ConstantTimeCompare everywhere                                                                      | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Admin vs Data Token Separation | Wrong token class returns 401                                                                              | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Provenance Fields              | actor_type (user/agent/system) + actor_id                                                                  | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Non-Blocking Queue             | Non-blocking select. sync.Once drain                                                                       | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Simple Mode Install            | bubblefish install --mode simple for zero-friction setup                                                  | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| bubblefish dev                 | Same daemon with debug logging and auto-reload                                                             | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Install Profiles               | Open WebUI, Postgres, OpenBrain profiles                                                                   | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Backup and Restore             | bubblefish backup create/restore                                                                           | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Config Lint                    | bubblefish lint for dangerous config detection                                                             | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Consistency Assertions         | Background WAL-destination consistency checks                                                              | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| WAL Health Watchdog            | Background disk/permissions/latency monitoring                                                             | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| bubblefish bench               | Throughput, latency, and retrieval eval                                                                    | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Reliability Demo               | Golden crash-recovery via CLI and dashboard                                                                | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Structured Security Events     | Dedicated security event log for SIEM integration                                                          | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Security Metrics               | Auth failures, policy denials, rate limits, admin calls                                                    | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Blessed Integration Configs    | Pre-built templates for Claude, Open WebUI, etc.                                                           | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Reference Architectures        | Dev laptop, home lab, air-gapped deployment docs                                                           | P0           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| TLS/mTLS Support               | Optional TLS with configurable cert, key, client CA                                                        | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Trusted Proxies                | CIDR list with forwarded header parsing                                                                    | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Event Sink (Webhooks)          | Optional async webhook notifications from WAL                                                              | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| OAuth Edge Integration         | Reverse proxy and JWT mapping templates                                                                    | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Live Pipeline Visualization    | Lossy event channel. Never blocks hot paths                                                                | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Black Box Mode                 | Dashboard toggle hiding internal stages                                                                    | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Conflict Inspector             | Read-only contradictory memory detection                                                                   | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Time-Travel View               | Query state as-of a timestamp                                                                              | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Security Tab                   | Dashboard tab with source policies and auth failure history                                                | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Debug Stages                   | Optional _nexus.debug with admin auth                                                                     | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Deployment Modes               | safe, balanced, fast presets                                                                               | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Docker Compose Reference       | Correct volumes, Docker Secrets, read-only filesystem                                                      | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| System Tray                    | Windows tray. Headless Linux: graceful skip                                                                | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+
| Architecture Diagram           | Mermaid diagram in README and docs                                                                         | P1           |
+--------------------------------+------------------------------------------------------------------------------------------------------------+--------------+

### 15.1 Explicitly Out of Scope for V2.2

<!-- Section 15.1 — Explicitly Out of Scope for V2.2 -->

-   Raft clustering or multi-node HA (v3 / Enterprise tier)

-   RBAC with roles, groups, and tenant model (v3 / Enterprise tier)

-   External policy decision point (OPA/Rego sidecar) (v3 / Enterprise tier)

-   Built-in ONNX model inference or embedding sidecar (v3 stretch goal)

-   Persistent cache across restarts (v3 stretch goal)

-   OAuth delegated identity from AI clients (Nexus handles keys, not login flows)

-   Background summarization worker (v3 investigation)

-   Knowledge graph / entity resolution module (v3 investigation)

-   LibSQL BEGIN CONCURRENT (v3 investigation)

-   WAL key rotation ring (v3; v2.2 supports a single key)

-   Per-tenant storage quotas (v3 / Enterprise tier)

-   Immutable admin audit trail (v3 / Enterprise tier)

-   Retention policies and legal holds (v3 / Enterprise tier)

## Section 16 — Validation Plan

<!-- Section 16 — Validation Plan -->

+-----------------------------+---------------------------------------------------+-----------------------------------------+
| **Guarantee**               | **Validation Method**                             | **Pass Criteria**                       |
+=============================+===================================================+=========================================+
| No duplicate writes         | Same payload twice with identical key.            | Exactly 1 row.                          |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| WAL crash recovery          | Send payload, kill -9, restart, query.            | Payload present.                        |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| WAL DELIVERED marking       | Send payload, restart.                            | 0 pending payloads.                     |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| WAL CRC32 integrity         | Corrupt WAL entry. Restart.                       | Corrupt entry skipped. WARN logged.     |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| WAL HMAC integrity          | Modify WAL entry (integrity=mac). Restart.        | Entry skipped. security event emitted.  |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| WAL encryption              | Enable encryption, write, restart.                | All entries readable. CRC validated.    |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Config signing              | Modify signed config. Start daemon.               | Daemon refuses to start.                |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| WAL watchdog                | Fill disk, observe.                               | /ready 503. Metric unhealthy.           |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Non-blocking queue          | 100 goroutines, 2000 sends.                       | Some 429s. Zero hangs.                  |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Timing attack resistance    | 1000 samples wrong vs correct key.                | p99 diff < 1ms.                        |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Admin/data token separation | Admin token on /inbound. Data key on /api/status. | Both return 401.                        |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Simple Mode install         | Run bubblefish install --mode simple.            | Config created. bubblefish start works. |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Event sink delivery         | Configure sink. Write memory. Check webhook.      | Webhook receives event.                 |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Event sink non-blocking     | Configure sink with unreachable URL. Write 1000.  | All 1000 return 200. Events dropped.    |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Temporal decay              | Insert old + new contradictory. Query.            | New ranked higher.                      |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Tiered decay                | Configure per-collection decay. Query.            | Collection-specific decay applied.      |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Retrieval profiles          | Query profile=fast. Verify stages 2+4 skipped.    | Stage 3 only.                           |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Provenance filtering        | Write with different actor_types. Filter.         | Only matching returned.                 |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Conflict inspector          | Write contradictory memories. Check.              | Conflicts detected.                     |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Time-travel                 | Write at t1, update at t2. Query as_of=t1.        | Only t1 data.                           |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Backup/restore              | Create backup, delete config, restore.            | Config and WAL restored.                |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Bench stability             | Run bench --mode throughput twice.               | Results within 10%.                     |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Reliability demo            | Run bubblefish demo.                              | 50 present, 0 dups, exit 0.             |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Crash recovery (50)         | Send 50, kill -9, restart, query.                 | All 50. 0 duplicates.                   |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Config lint                 | Config with 0.0.0.0 bind, no idempotency.         | Lint reports warnings.                  |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| MCP self-test               | bubblefish mcp test.                              | Exits 0 within 5 seconds.               |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Security events             | Trigger auth failure. Check log.                  | Event written to security-events.jsonl. |
+-----------------------------+---------------------------------------------------+-----------------------------------------+
| Load test                   | 1000 concurrent, unique keys.                     | Zero data loss. Zero crashes.           |
+-----------------------------+---------------------------------------------------+-----------------------------------------+

## Section 17 — Constraints and Tradeoffs

<!-- Section 17 — Constraints and Tradeoffs -->

+-----------------------------------------------+-----------------------------------------------------------------------------+
| **Tradeoff**                                  | **Reasoning**                                                               |
+===============================================+=============================================================================+
| No built-in embedding model                   | Nexus is a gateway. Any OpenAI-compatible URL works. Built-in ONNX is v3.   |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| Write-path embedding is client responsibility | Keeps write latency low. Open WebUI: configure separate embedding provider. |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| In-memory cache lost on restart               | TTL + watermarks ensure no stale data. Persistent cache is v3.              |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| SQLite write serialization                    | Acceptable for personal use. PostgreSQL for teams.                          |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| No built-in TLS by default                    | Binds localhost. TLS available when needed. Remote via Cloudflare Tunnel.   |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| No RBAC                                       | Per-source policies provide isolation. Enterprise tier adds RBAC.           |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| Source-only hot reload                        | Destination changes require restart. We fail safe.                          |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| No background summarization                   | Nexus is a router, not an AI. v3 investigation.                             |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| Temporal decay is heuristic                   | Does not replace entity resolution. Solves ~90% of contradiction cases.    |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| Single WAL encryption key                     | Key rotation ring is v3.                                                    |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| Event sinks are eventually consistent         | Never block writes. Dropped events are acceptable.                          |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| Lossy pipeline visualization                  | Events dropped when dashboard slow. Never blocks hot paths.                 |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| Consistency assertions sample-based           | Not full reconciliation. Minimal performance impact.                        |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| Config signing is offline                     | Signing keys don't reside on every host. Security tradeoff.                |
+-----------------------------------------------+-----------------------------------------------------------------------------+
| JWT mapping is advanced                       | Most should use reverse proxy pattern. Documented as advanced.              |
+-----------------------------------------------+-----------------------------------------------------------------------------+

## Section 18 — V3 Architecture Readiness

<!-- Section 18 — V3 Architecture Readiness -->

The V2.2 architecture is designed to support the following features in v3 without requiring pipeline rewrites. These are explicitly not implemented in V2.2 but the data model, interfaces, and extension points are in place.

### 18.1 RBAC and Multi-Tenant Semantics

<!-- Section 18.1 — RBAC and Multi-Tenant Semantics -->

V2.2 has per-source policies with CanRead/CanWrite, subject namespaces, and provenance fields. V3 extends this to a full tenant model:

-   Tenant ID layered on top of sources/subjects. Roles: admin, writer, reader, auditor.

-   Policy engine extended to (tenant, source, subject) tuples.

-   Per-tenant rate limits, storage quotas, and metrics.

-   Namespace format: {tenant_id}/{logical_namespace}.

*V2.2 readiness:* Subject namespace and provenance fields provide the data model foundation. Policy engine is extensible.

### 18.2 External Policy Decision Point (OPA/Rego)

<!-- Section 18.2 — External Policy Decision Point (OPA/Rego) -->

V3 adds an optional external PDP:

-   Config block: policy.external.enabled, policy.external.url.

-   Before policy gate: Nexus POSTs decision input (tenant, source, op, target, metadata) to OPA.

-   Strict timeouts and circuit-breaker semantics.

*V2.2 readiness:* Policy gate step in the pipeline is a clear extension point. Circuit breaker pattern already implemented for destinations.

### 18.3 Cluster/HA

<!-- Section 18.3 — Cluster/HA -->

V3 introduces optional clustering in two phases:

-   **Phase 1 — Active/Passive:** Multiple Nexus nodes behind a load balancer, all talking to replicated Postgres. Raft-based Cluster Manager for leader election only. Non-leaders redirect writes to leader.

-   **Phase 2 — Raft Log Replication:** Raft-backed replicated log above the WAL. Each write becomes a Raft entry committed by majority before WAL append.

*V2.2 readiness:* WAL, queue, and destination are cleanly separated. Node logic is isolated from cluster concerns. bubblefish cluster init/join/status subcommands are reserved.

### 18.4 Knowledge Graph Module

<!-- Section 18.4 — Knowledge Graph Module -->

V3 adds an optional KG module:

-   Dedicated KG destination schema for entities derived from canonical payloads.

-   Optional KG builder job polling Nexus or tailing WAL.

-   Optional entity stage in retrieval cascade.

-   bubblefish kg build / reconcile subcommands.

*V2.2 readiness:* Retrieval cascade has named stages that are independently skippable. Destination interface is extensible. Provenance fields enable entity-aware queries.

### 18.5 Persistent Cache

<!-- Section 18.5 — Persistent Cache -->

V3 adds an optional persistent cache store (bbolt or separate SQLite):

-   Non-source-of-truth: if corrupted, dropped and rebuilt from WAL/destination.

-   Max-bytes and max-entries limits with LRU eviction and periodic compaction.

*V2.2 readiness:* Cache interface (ExactCache, SemanticCache) is abstracted. Swapping in-memory for persistent is an implementation change, not a pipeline change.

### 18.6 ONNX Embedding Sidecar

<!-- Section 18.6 — ONNX Embedding Sidecar -->

V3 adds bubblefish-embed binary:

-   Small HTTP API wrapping ONNX Runtime for local embedding models.

-   Configured as daemon.embedding.provider = 'local-onnx'.

-   Strict resource limits (concurrency caps, timeouts).

*V2.2 readiness:* Embedding client interface supports any OpenAI-compatible endpoint. Sidecar just needs to implement /v1/embeddings.

### 18.7 Compliance Features

<!-- Section 18.7 — Compliance Features -->

V3 adds enterprise compliance capabilities:

-   Immutable audit trail of admin operations (policy changes, destination changes).

-   Retention policies at per-collection/tenant level.

-   Legal hold markers exempt records from deletion.

-   Application-level encryption for content fields using KMS-managed keys.

*V2.2 readiness:* Structured security events provide the audit foundation. Provenance fields enable collection-level policy. Config signing demonstrates cryptographic operations.

## Section 19 — Glossary

<!-- Section 19 — Glossary -->

+----------------------------+------------------------------------------------------------------------------------------------------------------+
| **Term**                   | **Definition**                                                                                                   |
+============================+==================================================================================================================+
| WAL                        | Write-Ahead Log. Append-only JSONL. Scanner: 10MB. CRC32 always, optional HMAC, optional AES-256-GCM encryption. |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| DELIVERED                  | WAL entry status after successful destination write. Atomic rename, same filesystem.                             |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| CRC32                      | 4-byte checksum. Detects accidental corruption.                                                                  |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| HMAC                       | HMAC-SHA256 integrity check. Detects intentional tampering (integrity=mac mode).                                 |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| TranslatedPayload          | Canonical normalized payload with provenance fields.                                                             |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Projection Engine          | Outbound shaping: field visibility, byte budgets, _nexus metadata.                                              |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Temporal Decay             | Reranking algorithm: final_score = (cos_sim * 0.7) + (recency * 0.3). Tiered and policy-aware.                 |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Retrieval Cascade          | 6-stage read-path pipeline. Cheapest safe method first.                                                          |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Retrieval Profile          | fast, balanced, or deep. Controls stages and oversampling.                                                       |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Exact Cache                | Zero-dep LRU. Go generics. Scope-partitioned.                                                                    |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Fast Path                  | Exact-subject last-N query bypassing full cascade.                                                               |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Provenance                 | actor_type (user/agent/system) + actor_id identifying memory origin.                                             |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Consistency Score          | Ratio of sampled DELIVERED entries found in destination (0.0-1.0).                                               |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| WAL Watchdog               | Background goroutine monitoring WAL health.                                                                      |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Config Lint                | Validation for dangerous or suboptimal settings.                                                                 |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Config Signing             | Digital signatures on compiled config files. Verified at startup.                                                |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Deployment Mode            | safe, balanced, or fast. Preset for security/performance.                                                        |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Simple Mode                | Zero-friction install: SQLite, single source, no embedding keys.                                                 |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Event Sink                 | Optional async webhook notification pipeline tailing the WAL.                                                    |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Black Box Mode             | Dashboard toggle hiding internal cascade stages.                                                                 |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Conflict Inspector         | Read-only contradictory memory detection.                                                                        |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Time-Travel View           | Query state as-of a specific timestamp.                                                                          |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Blessed Configs            | Pre-built, security-reviewed integration templates.                                                              |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Reference Architecture     | Documented deployment pattern with sample configs.                                                               |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| Structured Security Events | Dedicated JSON event log for SIEM integration.                                                                   |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| configMu                   | sync.RWMutex protecting d.sources and d.dests from hot reload race.                                              |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| MCP Server                 | Model Context Protocol. Claude Desktop integration. 127.0.0.1 only.                                              |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| sqlite-vec                 | SQLite extension for local vector similarity search.                                                             |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
| file: reference            | api_key = 'file:/path'. Docker Secrets compatible.                                                             |
+----------------------------+------------------------------------------------------------------------------------------------------------------+
