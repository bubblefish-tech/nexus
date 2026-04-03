🐟

BubbleFish Nexus v2

Product · Architecture · Design · Technical Specification

Version 2.0.0

| Author | Shawn Sammartano |
| Date | 2026 |
| Classification | CONFIDENTIAL |

---

SECTION 1 — EXECUTIVE SUMMARY

1. Executive Summary

BubbleFish Nexus v2 is a gateway-first AI memory daemon. It sits between multiple AI applications and one or more shared memory backends. Its job is to authenticate sources, normalize writes, protect storage, retrieve memories intelligently using the cheapest safe path first, shape responses to minimize token waste, and provide durable crash recovery.

Nexus v2 is not an AI system. It is a memory gateway, retrieval proxy, policy layer, and observability layer.

Core Value Proposition:
One daemon. Many AI clients. One protected memory backend.
Intelligent retrieval. Zero duplicate writes. Minimal tokens. Claude Desktop native.

1.1 What Changed from V1

V1 built the reliability foundation: WAL, queue, circuit breaker, idempotency, auth, rate limiting, response filtering. It works. It is crash-safe. It protects the database.

V2 adds the experience layer on top of that foundation:
- Full 6-stage retrieval cascade (cheapest safe path first)
- Embedding provider support for semantic search
- MCP server interface for Claude Desktop integration
- OpenAI-compatible write endpoint (/v1/memories)
- Streaming read support (SSE)
- Formal policy model with compiled policy artifacts
- Projection Engine replacing the simple response filter
- Lightweight subject namespacing for multi-user households and teams
- System tray launcher for Windows
- One-command install and start

1.2 Design Philosophy (Unchanged)

- Reliability first. Speed second. Features third.
- Every payload is persisted to the WAL before it touches the queue or database.
- The database is never written to directly — always through the queue.
- Duplicate writes are prevented by idempotency tracking.
- The AI client never receives raw database rows — only stripped, semantic content.
- The system fails closed, not open.
- Every failure mode is classified, logged, and handled — never silently dropped.
- Nexus is a gateway. It routes data. It does not generate content or embeddings independently.

---

SECTION 2 — ENVIRONMENT AND DEPLOYMENT

2. Environment Assumptions

2.1 Target Deployment Model

- Single-machine local deployment: developer laptop, home server, or VPS.
- Single binary. No container orchestration required.
- Optionally exposed via Cloudflare Tunnel for remote AI clients.
- System tray support on Windows for persistent background operation.
- One-command install: bubblefish install
- One-command start: bubblefish start

2.2 Trust Boundaries

| Boundary | Description |
|----------|-------------|
| AI Client → Nexus | Each AI client authenticates with a unique per-source API key. No shared keys. Key bound to source identity and policy at config time. |
| MCP Client → Nexus | MCP tools authenticate with a dedicated MCP API key. MCP source has its own policy scope. |
| Nexus → OpenBrain | Nexus authenticates to Supabase using a service role key stored in environment variables. Never passed to AI clients. |
| Nexus → Embedding Provider | Nexus calls embedding API with a provider API key. Key stored in env vars. Never logged or returned to clients. |
| Local → Remote | Cloudflare Tunnel is the only public-facing entry point. Nexus binds to localhost by default. |
| Admin → Nexus | Admin token is separate from source API keys. Admin queries bypass response filter and are always logged as WARN. |

---

SECTION 3 — ARCHITECTURE

3. Architecture

3.1 System Planes

| Plane | Responsibility |
|-------|---------------|
| Control Plane | Config loading, schema validation, policy compilation, embedding config, hot reload, cache rules. |
| Identity & Policy Plane | Source authentication, subject namespace resolution, scope checks, field-level projection rules, admin audit rules. |
| Data Plane | Write path normalization, WAL, queue, destination writes. Read path: retrieval cascade, response shaping, projection. |
| Cache Plane | Exact cache, semantic cache, invalidation rules, freshness watermarks. |
| Observation Plane | Structured logs, Prometheus metrics, replay state, queue depth, policy-denied events, cache hit ratios, destination health. |
| Administration Plane | CLI commands, doctor checks, TUI dashboard, web dashboard, replay controls, policy inspection, system tray. |

3.2 Write Path (Inbound) — Updated for V2

1. AI client sends POST /inbound/{source} with source API key.
2. Nexus validates API key. Rejects 401 immediately on failure. No WAL write.
3. Subject namespace is resolved: source name is the default namespace. X-Subject header overrides if present.
4. Policy gate: check source policy — can this source write to this destination, namespace, collection?
5. Payload size checked. Reject 413 if over limit.
6. Rate limit checked per source. Reject 429 + Retry-After if over limit.
7. Idempotency key checked. If seen within window, return 200 immediately without writing.
8. Field mapping applied via gjson dot-path extraction.
9. Transforms applied: trim, concat, coalesce, conditionals.
10. Optional embedding field extracted from payload if AI client sent it.
11. Canonical Write Envelope constructed with namespace and subject.
12. WAL append + fsync. Only after WAL succeeds does next step proceed.
13. Idempotency key stored.
14. Payload enqueued. Return 429 if queue full.
15. Return 200 + payload_id.
16. Worker writes to destination. On success: WAL entry marked DELIVERED, cache invalidated, watermark incremented.

3.3 Read Path (Outbound) — Updated for V2

1. AI client sends GET /query/{destination} with credentials and query params.
2. API key validated. Subject namespace resolved.
3. Policy gate: check allowed operations, retrieval modes, max results, max response bytes.
4. Query normalized into CanonicalQuery with structured filters, retrieval mode, and subject scope.
5. Retrieval cascade executed (see Section 3.4).
6. Result set shaped through Projection Engine: field allowlist, metadata strip, byte budget enforcement.
7. X-Nexus-Stage header added to response indicating which cascade stage served the request.
8. Filtered JSON returned.

3.4 The 6-Stage Retrieval Cascade

The cascade ensures Nexus uses the cheapest safe retrieval path first. Every stage is independently skippable based on policy, destination capability, and configuration.

STAGE 0: Policy Gate
Before any retrieval work, determine what this source is allowed to ask for.
Checks: allowed operations, max results, max response bytes, allowed retrieval modes.
On denial: return 403 with specific reason.
Always runs.

STAGE 1: Exact Cache Lookup
Build a deterministic cache key: SHA256(source_scope + destination + normalized_params + policy_hash).
If a policy-compatible fresh hit exists and watermark is current: return immediately.
Cache is partitioned by policy scope — source A cannot see source B's cache entries.
Activates when: policy.read_from_cache = true.

STAGE 2: Semantic Cache Lookup
Compare the normalized query embedding against recent cache entries in the same policy scope.
If a sufficiently similar result (cosine similarity >= threshold, default 0.92) exists and watermark is current: return cached projected response.
Activates when: embedding provider configured AND policy allows semantic cache.

STAGE 3: Structured Lookup
If the request has exact identifiers, metadata filters, or deterministic conditions, use indexed retrieval.
Translates CanonicalQuery.StructuredFilters to destination-specific queries:
  SQLite/PostgreSQL: parameterized WHERE clauses
  Supabase: PostgREST filter query string
Returns early if results meet the limit and policy disallows semantic.

STAGE 4: Semantic Retrieval
If structured lookup is insufficient and policy permits semantic mode:
  1. Generate embedding for query text using configured provider.
  2. Call destination's semantic search capability:
     Supabase: POST /rest/v1/rpc/match_documents with query embedding
     PostgreSQL: SELECT ... ORDER BY embedding <-> $1 LIMIT $2
     SQLite: not supported (CanSemanticSearch() = false)
Activates when: embedding provider configured AND destination.CanSemanticSearch() AND policy allows semantic.

STAGE 5: Hybrid Merge
If both structured and semantic paths ran:
  Deduplicate by payload_id.
  Semantic results first (preserve similarity score order).
  Append structured results not already in semantic set.
  Trim to max_results budget.

STAGE 6: Projection and Return
All results pass through the Projection Engine regardless of which stage served them:
  Field allowlist enforcement.
  Metadata stripping.
  Byte budget enforcement with word-boundary truncation.
  Deterministic field ordering.
Store result in exact cache (if policy.write_to_cache = true).
Store embedding + result in semantic cache (if embedding available).
Return shaped result with X-Nexus-Stage header.

3.5 Embedding Provider Design

Nexus supports any OpenAI-compatible embedding endpoint. It does not bundle or run embedding models itself.

Supported providers:
- openai: api.openai.com, any OpenAI-compatible URL
- ollama: localhost:11434, uses /api/embeddings endpoint
- compatible: any endpoint implementing POST /v1/embeddings

The embedding provider is optional. When not configured:
- Stage 2 (Semantic Cache) is bypassed.
- Stage 4 (Semantic Retrieval) is bypassed.
- All other stages operate normally.
- The cascade degrades gracefully to Stage 1 (Exact Cache) → Stage 3 (Structured) → Full scan.

Write path embedding: AI clients may include an "embedding" field in their payload JSON. If present, Nexus routes it to the destination. If absent, the record is written without an embedding (semantic search will not find it). Nexus does not generate embeddings on the write path — this keeps write latency low.

3.6 MCP Server Interface

Nexus v2 exposes a Model Context Protocol server on a configurable port (default 8082). This enables Claude Desktop, Cursor, and other MCP-compatible clients to use Nexus as a native memory tool.

MCP tools:
- nexus_write: persist a memory to the configured destination
- nexus_search: retrieve memories using the full retrieval cascade
- nexus_status: get daemon health, queue stats, and cache stats

MCP calls route directly through the internal pipeline — not via HTTP round-trip. The MCP source has its own policy scope and API key.

Claude Desktop config:
{
  "mcpServers": {
    "memory": {
      "command": "bubblefish",
      "args": ["mcp"]
    }
  }
}

3.7 Subject Namespacing

Nexus uses source-level namespacing to support multi-user and multi-context deployments.

Default behavior: namespace = source name. All writes from "claude" source go to namespace "claude".

Per-source namespace override in TOML:
[source]
namespace = "shawn"

Optional X-Subject header for per-request context:
X-Subject: work        → namespace "shawn/work"
X-Subject: personal    → namespace "shawn/personal"

Multi-user household setup: each person gets their own source TOML and API key with their name as namespace. No identity inference — only what is explicitly configured or passed.

3.8 WAL Design (Updated)

V2 adds DELIVERED status marking. After a successful destination write, the worker calls wal.MarkDelivered(payloadID). On startup, only PENDING entries are replayed. DELIVERED entries are not re-enqueued. This eliminates the "re-enqueuing N pending payloads on every restart" problem from v1.

All other WAL guarantees from v1 remain unchanged: append-only JSONL, fsync on write, size-based auto-rotation, collision-safe filenames, schema version migration registry.

3.9 Graceful Shutdown (Updated)

On SIGTERM: stop HTTP server → stop MCP server → drain queues → fsync WAL → close cache → close destinations → close tray → exit.

---

SECTION 4 — CONFIGURATION

4. Configuration Design

4.1 Directory Structure (Updated)

~/.bubblefish/Nexus/
  daemon.toml              ← global daemon settings
  sources/
    claude.toml
    perplexity.toml
  destinations/
    openbrain.toml
    sqlite.toml
  policies/                ← NEW in v2
    claude_policy.toml     ← optional — inline [source.policy] is preferred
  caches/                  ← NEW in v2
    cache_rules.toml       ← optional cache rule overrides
  compiled/
    sources.json
    destinations.json
    policies.json          ← NEW in v2
    cache_rules.json       ← NEW in v2
  wal/
    wal-current.jsonl
  logs/
    bubblefish.log

4.2 daemon.toml (V2)

[daemon]
port = 8080
bind = "127.0.0.1"
admin_token = "env:ADMIN_TOKEN"
log_level = "info"
log_format = "json"

[daemon.shutdown]
drain_timeout_seconds = 30

[daemon.wal]
path = "~/.bubblefish/Nexus/wal"
max_segment_size_mb = 50

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
source_name = "mcp"
api_key = "env:MCP_API_KEY"

[daemon.web]
port = 8081
require_auth = false

4.3 Source TOML with Policy (V2)

[source]
name = "claude"
api_key = "env:CLAUDE_SOURCE_KEY"
namespace = "claude"
can_read = true
can_write = true
target_destination = "openbrain"

[source.rate_limit]
requests_per_minute = 2000

[source.payload_limits]
max_bytes = 10485760

[source.mapping]
content = "message.content"
model   = "model"
role    = "message.role"

[source.transform]
content = ["trim"]
model   = ["coalesce:unknown"]

[source.idempotency]
enabled = true
dedup_window_seconds = 300

[source.policy]
allowed_destinations = ["openbrain", "sqlite"]
allowed_operations = ["write", "read", "search"]
allowed_retrieval_modes = ["exact", "structured", "semantic", "hybrid"]
max_results = 50
max_response_bytes = 65536

[source.policy.field_visibility]
include_fields = ["content", "source", "role", "timestamp", "model"]
strip_metadata = true

[source.policy.cache]
read_from_cache = true
write_to_cache = true
max_ttl_seconds = 300
semantic_similarity_threshold = 0.92

4.4 Destination TOML (V2 — Unchanged from V1)

[destination]
name = "openbrain"
type = "supabase"
url  = "env:SUPABASE_URL"
key  = "env:SUPABASE_SERVICE_ROLE_KEY"
table = "thoughts"
schema_version = 1
schema = ["content", "model", "role", "source", "timestamp", "idempotency_key",
          "schema_version", "metadata", "embedding", "namespace"]

[destination.queue]
depth = 1000
workers = 2
load_shed_threshold = 800

[destination.circuit_breaker]
failure_threshold = 5
recovery_interval_seconds = 30

[destination.retry]
max_attempts = 3
initial_backoff_ms = 500
max_backoff_ms = 30000
backoff_multiplier = 2.0

---

SECTION 5 — FEATURES

5. Feature Set

5.1 V2 Features (New)

| Feature | Description | Priority |
|---------|-------------|----------|
| 6-Stage Retrieval Cascade | Policy gate → exact cache → semantic cache → structured → semantic → hybrid merge + projection | P0 — Required |
| MCP Server Interface | Native Claude Desktop integration via Model Context Protocol. nexus_write, nexus_search, nexus_status tools. | P0 — Required |
| Embedding Provider | OpenAI-compatible embedding endpoint config. Optional — gracefully disabled when not configured. | P0 — Required |
| Semantic Retrieval | Stage 4 of cascade. Vector similarity search against pgvector destinations. | P0 — Required |
| Exact Cache | Stage 1 of cascade. In-memory cache with policy-scoped keys, TTL, and write invalidation. | P0 — Required |
| Semantic Cache | Stage 2 of cascade. Near-duplicate query reuse via cosine similarity. | P0 — Required |
| Hybrid Merge | Stage 5 of cascade. Dedup and rerank structured + semantic results. | P0 — Required |
| Projection Engine | Formal replacement for response filter. Field allowlist, byte budget, deterministic serialization. | P0 — Required |
| Policy Model in TOML | [source.policy] block. Compiled to policies.json at build time. | P0 — Required |
| WAL DELIVERED Marking | Workers mark WAL entries DELIVERED after successful write. Eliminates phantom replay on restart. | P0 — Required |
| OpenAI-Compatible Endpoint | POST /v1/memories — accepts OpenAI chat message format. | P1 — High |
| Streaming Reads | SSE support on GET /query/{dest} when Accept: text/event-stream. | P1 — High |
| Subject Namespacing | Namespace per source config, optional X-Subject header override. | P1 — High |
| System Tray | Windows tray icon with status indicator and quick actions. | P1 — High |
| bubblefish install | One-command wizard: init + edit prompt + build + doctor. | P1 — High |
| bubblefish start | One-command launcher: daemon + MCP + web + tray. | P1 — High |
| Cache Stats Endpoint | GET /api/cache — exact hits, semantic hits, invalidations. | P2 — Medium |
| Policy Inspection CLI | bubblefish policy — prints compiled policies and hashes. | P2 — Medium |
| Raised Rate Limits | Default 2000/min per source. Clear 429 error messages with fix instructions. | P1 — High |

5.2 V1 Features Retained (All of them)

WAL + crash recovery, queue + circuit breaker, idempotency, auth, rate limiting, SQLite destination, OpenBrain/Supabase destination, PostgreSQL destination, Prometheus metrics, TUI dashboard, web dashboard, hot-reload, Bubble Tea TUI, bubblefish doctor, cross-platform binaries, Dockerfile, Helm chart, systemd unit, launchd plist.

5.3 Explicitly Out of Scope for V2

- Raft clustering or multi-node HA (Enterprise tier)
- RBAC with roles and groups (Enterprise tier)
- Encrypted WAL at rest (Community relies on OS disk encryption)
- Complex multi-step DAG agentic chaining (Enterprise tier)
- Forward proxy with TLS inspection (separate product variant)
- Built-in ONNX model inference (V3 stretch goal)
- OAuth delegated identity from AI clients (no current clients support this)
- Cloud IAM, SSO/SAML (Enterprise tier)

---

SECTION 6 — DATA CONTRACTS

6. Data Contracts

6.1 Canonical Write Envelope (V2)

type TranslatedPayload struct {
    PayloadID        string
    RequestID        string            // UUID generated at HTTP ingress
    Source           string
    Subject          string            // from X-Subject header or source namespace
    Namespace        string            // source namespace from config
    Destination      string
    Collection       string            // from X-Collection header or destination table
    Content          string
    Model            string
    Role             string
    Timestamp        time.Time
    IdempotencyKey   string
    SchemaVersion    int
    TransformVersion string
    Embedding        []float32         // optional, sent by AI client
    Metadata         map[string]string
}

6.2 Canonical Query Envelope (V2)

type CanonicalQuery struct {
    RequestID            string
    Source               string
    Subject              string
    Destination          string
    Namespace            string
    Collection           string
    Operation            string         // read | search | list
    QueryText            string
    StructuredFilters    map[string]string
    RequestedFields      []string
    RetrievalMode        string         // exact | structured | semantic | hybrid | auto
    Limit                int
    PolicyHash           string
}

6.3 Cache Record

type CacheRecord struct {
    CacheKey              string
    CacheType             string         // exact | semantic
    ScopeHash             string
    Destination           string
    Namespace             string
    PolicyHash            string
    BackendWatermark      uint64
    SimilarityFingerprint []float32      // nil for exact cache
    ResponsePayload       []byte
    CreatedAtUTC          time.Time
    ExpiresAtUTC          time.Time
}

6.4 HTTP API (V2 — Full)

| Endpoint | Method | Description |
|----------|--------|-------------|
| /inbound/{source} | POST | Write path. Full pipeline. Returns payload_id. |
| /v1/memories | POST | OpenAI-compatible write. Accepts messages array. |
| /query/{destination} | GET | Read path. Full 6-stage cascade. |
| /api/status | GET | Queue, destination, replay, cache stats. No auth required. |
| /api/cache | GET | Cache stats: hits, misses, invalidations, watermarks. |
| /api/policies | GET | Compiled policy hashes and summaries. Admin only. |
| /api/replay | POST | Trigger WAL segment replay. Admin only. |
| /metrics | GET | Prometheus metrics. |
| /health | GET | Liveness probe. |
| /ready | GET | Readiness: destinations reachable, queues below threshold. |

6.5 V2 Prometheus Metrics (Added)

- bubblefish_cache_exact_hits_total
- bubblefish_cache_exact_misses_total
- bubblefish_cache_semantic_hits_total
- bubblefish_cache_semantic_misses_total
- bubblefish_retrieval_stage{stage="exact_cache|structured|semantic|hybrid"}
- bubblefish_policy_denials_total{source, operation}
- bubblefish_embedding_latency_seconds
- bubblefish_projected_response_bytes{source}
- bubblefish_wal_pending_entries

---

SECTION 7 — FAILURE MODES (V2 Additions)

7. Failure Modes

| Failure Scenario | Behavior | Recovery |
|-----------------|----------|----------|
| Embedding provider unreachable | Stages 2 and 4 bypassed. Cascade continues without semantic. Logged as WARN. | Auto-recovers when provider returns. No data loss. |
| Semantic cache stale | Watermark check fails. Fall through to Stage 3. Fresh result served and cached. | Automatic. |
| Cache store full (in-memory) | LRU eviction. Oldest entries removed. No impact on correctness. | Automatic. |
| Policy compilation failure | bubblefish build fails. Daemon refuses to start with invalid policy. | Operator fixes TOML, re-runs build. |
| Subject namespace missing | Falls back to source name as namespace. No error. | By design — namespace is optional. |
| MCP server port conflict | MCP server fails to bind. Daemon continues without MCP. Error logged. | Operator changes MCP port in daemon.toml. |
| WAL DELIVERED mark failure | Logged as WARN. Delivery counted as success. On replay, idempotency layer prevents double-write. | Idempotency is the safety net. |

---

SECTION 8 — VALIDATION PLAN (V2)

8. Validation Plan

| Guarantee | Validation Method | Pass Criteria |
|-----------|------------------|---------------|
| Exact cache hit | Send identical request twice. Check X-Nexus-Stage header. | Second request: Stage = exact_cache |
| Semantic cache hit | Send similar query twice with embedding enabled. | Second request: Stage = semantic_cache, similarity >= threshold |
| Structured filter | GET /query/sqlite?role=user. Check results. | Only role=user rows returned |
| Semantic retrieval | GET /query/openbrain?mode=semantic&query=... | Results ranked by semantic relevance |
| Policy denial | Request operation not in allowed_operations. | 403 with specific denial reason |
| Cache scope isolation | Two sources query same destination. | Source A never receives Source B's cached results |
| WAL DELIVERED marking | Send payload. Restart daemon. | Startup shows 0 pending payloads (not previous counts) |
| MCP write | Call nexus_write from Claude Desktop. | Payload appears in Supabase |
| MCP search | Call nexus_search from Claude Desktop. | Returns semantically relevant memories |
| OpenAI endpoint | POST /v1/memories. | Payload written, OpenAI-format response returned |
| SSE streaming | GET /query with Accept: text/event-stream. | Results streamed as SSE events |
| Namespace isolation | Two sources with different namespaces write to same dest. | ?namespace=shawn only returns shawn's records |

---

SECTION 9 — GLOSSARY (V2 Additions)

9. Glossary

| Term | Definition |
|------|------------|
| Retrieval Cascade | The 6-stage read-path pipeline that chooses the cheapest safe retrieval method first. |
| Exact Cache | Stage 1 cache. Returns byte-identical responses for normalized repeat requests. |
| Semantic Cache | Stage 2 cache. Returns near-duplicate responses based on embedding similarity within the same policy scope. |
| Projection Engine | The outbound shaping layer that enforces field visibility, byte budgets, and deterministic serialization. |
| Freshness Watermark | A monotonic counter per destination namespace. Cache entries store the watermark at creation. Stale when current watermark is higher. |
| Policy Gate | Stage 0. Evaluates source policy before any retrieval work begins. |
| Structured Lookup | Stage 3. Uses indexed metadata filters before semantic search. |
| Semantic Retrieval | Stage 4. Vector similarity search against pgvector-capable destinations. |
| Hybrid Merge | Stage 5. Deduplicates and reranks results from structured and semantic paths. |
| Subject | The acting identity within a source. Defaults to source name. Overridable via X-Subject header. |
| Namespace | A logical partition within a destination. Prevents data bleed between sources or users. |
| MCP Server | The Model Context Protocol server that exposes Nexus as native tools to Claude Desktop and other MCP clients. |
| Scope Hash | A deterministic hash of source + destination + namespace + policy. Used to partition cache entries. |
| Canonical Write Envelope | The normalized payload structure after field mapping, transforms, and namespace injection. |
| Canonical Query Envelope | The normalized read request after filter parsing, mode detection, and policy application. |
| Result Budget | The max_results + max_response_bytes limits enforced by the Projection Engine. |
| Embedding Provider | An external OpenAI-compatible endpoint that generates vector embeddings. Optional. |
| Operator Console | The web dashboard and CLI tools for inspecting Nexus state in production. |
| Source Profile | A source TOML file defining an AI client's identity, mapping, policy, and cache rules. |
| Destination Profile | A destination TOML file defining a storage backend's connection, schema, and queue settings. |

---

SECTION 10 — CONSTRAINTS AND TRADEOFFS (V2)

10. Constraints and Tradeoffs

| Tradeoff | Reasoning |
|----------|-----------|
| No built-in embedding model | Nexus is a gateway. Bundling ONNX inference is a v3 goal. For v2, any OpenAI-compatible URL works including free local Ollama. |
| Write-path embedding is client responsibility | Keeps write latency low. AI clients already have content and can generate embeddings. Nexus routes, not generates. |
| In-memory cache lost on restart | TTL-based invalidation and watermarks mean stale data is never served. Cache is a latency optimization, not a data store. bbolt persistence is a v3 option. |
| Semantic cache requires embedding provider | Feature gracefully disabled when not configured. No degradation in functionality. |
| Subject namespacing is opt-in | Simple source-per-person config is sufficient for most households. X-Subject is available for finer-grained control without requiring it. |
| MCP server is a separate port | Clean separation of concerns. MCP clients and HTTP clients do not share connection state. |
| WAL DELIVERED mark failure does not fail delivery | Idempotency is the safety net. A rare marking failure will cause a replay that is safely deduplicated. Correctness over completeness. |
