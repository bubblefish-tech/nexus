# 🐟 BubbleFish Nexus v2 — Build Guide

**Prerequisite:** Post Phase 7 guide complete. v1.0.1 tagged and pushed. OpenBrain connected and verified.

**Total estimated time:** 10-12 weeks of focused development across 9 phases.

---

## V2 Goals

1. Full 6-stage retrieval cascade (policy gate → exact cache → semantic cache → structured lookup → semantic retrieval → hybrid merge + projection)
2. MCP server interface for Claude Desktop integration
3. Embedding provider support (OpenAI, Ollama, any OpenAI-compatible endpoint)
4. Streaming read support (SSE)
5. OpenAI-compatible write endpoint (`/v1/memories`)
6. Policy model in TOML with compiled policy artifacts
7. Lightweight subject namespacing via source config and optional X-Subject header
8. System tray launcher for Windows
9. One-command install and start
10. Formalized Projection Engine

---

## Phase Map

| Phase | Name | Duration | Key Output |
|-------|------|----------|------------|
| Phase 0 | V2 Scaffold + Bug Fixes | 2 days | WAL DELIVERED fix, new dirs, v2 types |
| Phase 1 | Policy Model + Compilation | 3-4 days | [source.policy] TOML, compiled policies.json |
| Phase 2 | Projection Engine | 2-3 days | internal/projection, byte budget, field policy |
| Phase 3 | Stage 0+3 Cascade (Policy Gate + Structured Lookup) | 3-4 days | Filter model, cascade planner skeleton |
| Phase 4 | Stage 1 Exact Cache | 3-4 days | internal/cache/exact.go, invalidation hooks |
| Phase 5 | Embedding Provider + Stage 4 Semantic Retrieval | 5-6 days | internal/embedding, vector search |
| Phase 6 | Stage 2 Semantic Cache + Stage 5 Hybrid Merge | 4-5 days | Full 6-stage cascade complete |
| Phase 7 | MCP Server + OpenAI Endpoint + Streaming | 4-5 days | Claude Desktop integration |
| Phase 8 | System Tray + One-Command Launch + Install | 3-4 days | bubblefish install, bubblefish start, tray icon |
| Phase 9 | Testing + Hardening + Ship | 3-4 days | Integration tests, v2.0.0 tag |

**Total: ~32-38 working days (8-10 weeks)**

---

## Prerequisites — Install Before Phase 0

```powershell
Set-Location D:\Bubblefish\Projects\Nexus

# Verify current state
go build ./...
go vet ./...
.\bubblefish.exe version
# Expected: BubbleFish Nexus 1.0.1

# Install new v2 dependencies
go get github.com/getlantern/systray@latest
go get github.com/mark3labs/mcp-go@latest
go get go.etcd.io/bbolt@latest
go mod tidy

Write-Host "V2 dependencies installed"
```

---

## Phase 0: V2 Scaffold + Critical Bug Fixes

**Duration:** 2 days
**Depends on:** Post Phase 7 guide complete

### Step 0.1 — Fix WAL DELIVERED Marking (Critical Bug)

This must be fixed first. Currently workers never mark WAL entries DELIVERED, causing every restart to re-enqueue all payloads.

The fix requires passing a WAL reference and a status-update function into the queue worker. Feed this prompt to your AI coding session:

```
You are helping build BubbleFish Nexus — a single-binary Go AI routing daemon.

TASK: Fix WAL DELIVERED marking

Problem: internal/queue/worker.go delivers payloads to destinations but never
updates the WAL entry status to DELIVERED. On every daemon restart, all WAL
entries are re-enqueued because they are still PENDING.

Fix:
1. Add a WALUpdater interface to internal/queue/queue.go:
   type WALUpdater interface {
       MarkDelivered(payloadID string) error
   }

2. Add walUpdater WALUpdater field to Queue struct.

3. Add SetWALUpdater(w WALUpdater) method to Queue.

4. In internal/wal/wal.go, add MarkDelivered(payloadID string) error method
   that scans the active WAL segment and rewrites the matching entry's status
   to DELIVERED. Use a temp file + atomic rename to avoid corruption.

5. In internal/queue/worker.go, after successful plugin.Write(), call
   q.walUpdater.MarkDelivered(payload.PayloadID) if walUpdater is set.
   Log WARN on failure but do not fail the delivery.

6. In internal/daemon/daemon.go, after creating each queue, call
   q.SetWALUpdater(d.wal).

Files to modify: internal/wal/wal.go, internal/queue/queue.go,
internal/queue/worker.go, internal/daemon/daemon.go

Requirements:
- WAL marking failure must not fail the delivery
- go build ./... and go vet ./... must pass
- Existing tests must not break
```

### Step 0.2 — Create V2 Directory Structure

```powershell
Set-Location D:\Bubblefish\Projects\Nexus

New-Item -ItemType Directory -Force -Path @(
    'internal\identity',
    'internal\authz',
    'internal\policy',
    'internal\query',
    'internal\cache',
    'internal\projection',
    'internal\metrics',
    'internal\embedding',
    'internal\mcp',
    'internal\tray'
)

Write-Host "V2 directories created"
Get-ChildItem internal\ -Directory | Select-Object Name
```

### Step 0.3 — Update Version to 2.0.0-alpha

```powershell
(Get-Content D:\Bubblefish\Projects\Nexus\internal\version\version.go -Raw) `
    -replace '"1.0.0"','"2.0.0-alpha"' `
    | Set-Content D:\Bubblefish\Projects\Nexus\internal\version\version.go -NoNewline
```

### Step 0.4 — Quality Gates

```powershell
go build ./...
go vet ./...
go test ./... -count=1
.\bubblefish.exe version
# Expected: BubbleFish Nexus 2.0.0-alpha
```

### Phase 0 Acceptance Criteria
- WAL DELIVERED fix implemented and tested
- All new directories created
- Version updated to 2.0.0-alpha
- All existing tests still pass
- go build and go vet clean

---

## Phase 1: Policy Model + Compilation

**Duration:** 3-4 days
**Depends on:** Phase 0

### AI Prompt for Phase 1

```
You are helping build BubbleFish Nexus v2 — a single-binary Go AI routing daemon.
Reference: BubbleFish Nexus Spec v2 (attached).

TASK: Phase 1 — Policy Model and Compilation

Files that already exist (DO NOT recreate):
- All Phase 0 files including WAL fix

Implement:
- internal/policy/types.go — PolicyConfig, ScopeRule, OperationPolicy,
  FieldVisibilityRule, CachePolicy structs
- internal/policy/compile.go — compile source TOMLs into PolicyContext,
  generate deterministic policy hash
- internal/policy/validate.go — detect invalid operations, unknown destinations,
  conflicting rules
- internal/config/build.go — extend to load policies/ dir and emit
  compiled/policies.json
- internal/config/templates/claude.toml — add [source.policy] block
- Update cmd/bubblefish/main.go to add 'policy' subcommand stub

Key TOML structure to support:
[source.policy]
allowed_destinations = ["openbrain", "sqlite"]
allowed_operations = ["write", "read", "search"]
allowed_retrieval_modes = ["exact", "structured", "semantic"]
max_results = 50
max_response_bytes = 65536

[source.policy.field_visibility]
include_fields = ["content", "source", "role", "timestamp"]
strip_metadata = true

[source.policy.cache]
read_from_cache = true
write_to_cache = true
max_ttl_seconds = 300

Requirements:
- Build fails if source references unknown destination
- Build fails if unknown operation specified
- Compiled policies.json generated deterministically
- Existing source configs without [source.policy] blocks use safe defaults
- go build ./... and go vet ./... pass
```

### Phase 1 Verification

```powershell
.\bubblefish.exe build
# Expected: includes "X policy(s) compiled"

Get-Content C:\Users\shawn\.bubblefish\Nexus\compiled\policies.json | ConvertFrom-Json | ConvertTo-Json -Depth 5

.\bubblefish.exe policy
# Expected: lists compiled policies and hashes
```

---

## Phase 2: Projection Engine

**Duration:** 2-3 days
**Depends on:** Phase 1

### AI Prompt for Phase 2

```
You are helping build BubbleFish Nexus v2 — a single-binary Go AI routing daemon.

TASK: Phase 2 — Projection Engine

Files that already exist: all Phase 0-1 files

Implement:
- internal/projection/project.go — ProjectionEngine struct, Project() method
  Input: []map[string]interface{}, PolicyContext
  Output: []map[string]interface{} with only allowed fields
  Behavior: field allowlist from policy, strip_metadata option,
  max_response_bytes enforcement (truncate text fields to fit budget),
  deterministic field ordering for consistent hashing

- internal/projection/truncate.go — TruncateText(s string, maxBytes int) string
  Truncates at word boundary, appends "[truncated]" marker

- internal/projection/budget.go — ResponseBudget struct
  Tracks bytes consumed across result set, enforces max_response_bytes cap

Replace the existing applyResponseFilter function in internal/daemon/presenter.go
with a call to the new ProjectionEngine. The ProjectionEngine should be
initialized once on daemon start and stored on the Daemon struct.

Requirements:
- Same input always produces same output (deterministic)
- Oversized responses truncated with logged warning
- Existing response filter tests still pass with updated implementation
- go build ./... and go vet ./... pass
```

---

## Phase 3: Cascade Skeleton + Structured Lookup (Stages 0 + 3)

**Duration:** 3-4 days
**Depends on:** Phase 2

### AI Prompt for Phase 3

```
You are helping build BubbleFish Nexus v2 — a single-binary Go AI routing daemon.

TASK: Phase 3 — Retrieval Cascade Skeleton + Structured Lookup

Files that already exist: all Phase 0-2 files

Implement:
- internal/query/cascade.go — Cascade struct with Execute() method
  Stages are independently skippable. Unimplemented stages (1, 2, 4, 5) are
  stubs that return (nil, false) immediately. The cascade calls:
  stage0PolicyGate → stage1ExactCache(stub) → stage2SemanticCache(stub) →
  stage3StructuredLookup → stage4SemanticRetrieval(stub) →
  stage5HybridMerge(stub) → stage6ProjectAndReturn

- internal/query/normalize.go — NormalizeQuery(r *http.Request, src SourceConfig)
  CanonicalQuery builds from URL params including:
  ?query=text  ?source=claude  ?role=user  ?limit=20
  ?timestamp_after=2026-01-01  ?timestamp_before=2026-12-31
  ?mode=exact|structured|semantic|hybrid

- internal/query/structured.go — StructuredLookup(req CanonicalQuery, dest DestinationPlugin)
  Translates CanonicalQuery.StructuredFilters to destination-specific queries.
  SQLite: builds parameterized WHERE clause
  Supabase: builds PostgREST filter query string (?column=eq.value)
  Returns ([]map[string]interface{}, error)

- Update internal/destination/plugin.go — add to interface:
  CanStructuredLookup() bool
  StructuredRead(filters map[string]string, limit int) ([]map[string]interface{}, error)

- Update internal/destination/sqlite.go — implement CanStructuredLookup() and
  StructuredRead() with parameterized WHERE clauses

- Update internal/destination/openbrain.go — implement CanStructuredLookup() and
  StructuredRead() with PostgREST filter syntax

- Wire cascade into internal/daemon/handlers.go handleQuery — replace direct
  plugin.Read() call with cascade.Execute()

Requirements:
- Existing query behavior unchanged when no structured filters present
- ?source=claude filter returns only claude records
- ?role=user&timestamp_after=2026-01-01 filters work correctly
- Cascade stage logged in response headers (X-Nexus-Stage: structured)
- go build ./... and go vet ./... pass
```

### Phase 3 Verification

```powershell
# Start daemon
.\bubblefish.exe daemon

# In second window — test structured filters
$h = @{"Authorization"="Bearer nexus-key-claude-001";"Content-Type"="application/json"}
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/query/sqlite?role=user&limit=5" -Headers $h
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/query/sqlite?source=claude&limit=5" -Headers $h
```

---

## Phase 4: Exact Cache (Stage 1)

**Duration:** 3-4 days
**Depends on:** Phase 3

### AI Prompt for Phase 4

```
You are helping build BubbleFish Nexus v2 — a single-binary Go AI routing daemon.

TASK: Phase 4 — Exact Cache

Files that already exist: all Phase 0-3 files

Implement:
- internal/cache/exact.go — ExactCache struct
  Key: SHA256 hash of (source_name + destination + normalized_query_params + policy_hash)
  Value: cached []map[string]interface{} + expiry time
  Methods: Lookup(req CanonicalQuery, policy PolicyContext) ([]map[string]interface{}, bool)
           Store(req CanonicalQuery, policy PolicyContext, rows []map[string]interface{})
           Invalidate(destination string, namespace string)
           Stats() CacheStats

- internal/cache/watermark.go — WriteWatermark struct
  Monotonic counter per destination+namespace incremented on every successful write.
  Cache entries store the watermark at creation. On lookup, if current watermark
  > entry watermark, entry is stale and must be refreshed.
  Methods: Increment(dest string) uint64
           Current(dest string) uint64

- internal/cache/stats.go — CacheStats struct
  ExactHits, ExactMisses, Invalidations, EntriesCount

- Wire into cascade.go stage1ExactCache — replace stub with real implementation

- Wire invalidation into internal/queue/worker.go — after successful Write(),
  call cache.Invalidate(payload.Destination, payload.Namespace)
  Also call watermark.Increment(payload.Destination)

- Add GET /api/cache endpoint to internal/daemon/handlers.go
  Returns CacheStats as JSON (no auth required, same as /api/status)

Requirements:
- Cache partitioned by policy scope (source A cannot see source B's cache)
- Cache entries expire per policy max_ttl_seconds
- Watermark invalidation works correctly
- Repeated identical requests hit cache on second call
- Cache stats visible at /api/cache
- go build ./... and go vet ./... pass
```

### Phase 4 Verification

```powershell
# Start daemon, make two identical requests, confirm cache hit
$h = @{"Authorization"="Bearer nexus-key-claude-001";"Content-Type"="application/json"}
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/query/sqlite?limit=5" -Headers $h
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/query/sqlite?limit=5" -Headers $h

# Check cache stats
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/api/cache"
# Expected: ExactHits: 1, ExactMisses: 1
```

---

## Phase 5: Embedding Provider + Semantic Retrieval (Stage 4)

**Duration:** 5-6 days
**Depends on:** Phase 4

### Add Embedding Config to daemon.toml Template

```toml
[daemon.embedding]
enabled = false
provider = "openai"              # openai | ollama | compatible
url = "env:EMBEDDING_URL"        # https://api.openai.com or http://localhost:11434
api_key = "env:EMBEDDING_API_KEY"
model = "text-embedding-3-small" # or "nomic-embed-text" for Ollama
dimensions = 1536                # 1536 for OpenAI, 768 for nomic-embed-text
timeout_seconds = 10
```

### AI Prompt for Phase 5

```
You are helping build BubbleFish Nexus v2 — a single-binary Go AI routing daemon.

TASK: Phase 5 — Embedding Provider + Semantic Retrieval

Files that already exist: all Phase 0-4 files

Implement:
- internal/embedding/client.go — EmbeddingClient interface
  Methods: Embed(text string) ([]float32, error)
           Dimensions() int
           Provider() string

- internal/embedding/openai.go — OpenAIEmbedder implementing EmbeddingClient
  Calls POST /v1/embeddings with model and input text
  Parses response.data[0].embedding as []float32
  Supports any OpenAI-compatible URL (works with Ollama, LM Studio, etc.)
  Connection timeout from config

- internal/embedding/factory.go — NewEmbedder(cfg EmbeddingConfig) (EmbeddingClient, error)
  Returns nil embedder if embedding.enabled = false (graceful disable)

- internal/config/types.go — Add EmbeddingConfig struct to DaemonConfig

- Update internal/destination/plugin.go — add to interface:
  CanSemanticSearch() bool
  SemanticRead(queryEmbedding []float32, limit int) ([]map[string]interface{}, error)

- Update internal/destination/openbrain.go — implement CanSemanticSearch() true
  SemanticRead calls POST /rest/v1/rpc/match_documents with:
  { query_embedding: [...], match_count: limit }
  This calls the Supabase pgvector similarity function

- Update internal/destination/sqlite.go — implement CanSemanticSearch() false
  SQLite without sqlite-vec does not support semantic search

- Update internal/destination/postgres.go — implement CanSemanticSearch() true
  SemanticRead uses: SELECT ... ORDER BY embedding <-> $1 LIMIT $2

- Update TranslatedPayload in internal/config/types.go — add:
  Embedding []float32 `json:"embedding,omitempty"`
  If the AI client sends "embedding" in the payload JSON, extract and route it.
  If absent, write without embedding (no semantic search possible for that record).

- Wire cascade.go stage4SemanticRetrieval — replace stub:
  If embedder != nil AND dest.CanSemanticSearch() AND policy allows semantic:
    embedding := embedder.Embed(req.QueryText)
    rows := dest.SemanticRead(embedding, req.Limit)

Requirements:
- Embedding client gracefully disabled when embedding.enabled = false
- Destinations that don't support semantic search skip stage 4
- Embedding timeout does not block other cascade stages
- Write path accepts optional embedding field from AI clients
- go build ./... and go vet ./... pass
```

### Phase 5 Verification

```powershell
# Set embedding env vars (use Ollama for free local testing)
$env:EMBEDDING_URL="http://localhost:11434"
$env:EMBEDDING_API_KEY="ollama"

# Update daemon.toml to enable embedding
# [daemon.embedding]
# enabled = true
# provider = "ollama"
# url = "env:EMBEDDING_URL"
# model = "nomic-embed-text"
# dimensions = 768

.\bubblefish.exe build
.\bubblefish.exe daemon

# Test semantic search
$h = @{"Authorization"="Bearer nexus-key-claude-001";"Content-Type"="application/json"}
Invoke-RestMethod -Method GET -Uri "http://localhost:8080/query/openbrain?mode=semantic&query=what+did+I+say+about+the+API+project" -Headers $h
```

---

## Phase 6: Semantic Cache + Hybrid Merge (Stages 2 + 5)

**Duration:** 4-5 days
**Depends on:** Phase 5

### AI Prompt for Phase 6

```
You are helping build BubbleFish Nexus v2 — a single-binary Go AI routing daemon.

TASK: Phase 6 — Semantic Cache + Hybrid Merge

Files that already exist: all Phase 0-5 files

Implement:
- internal/cache/semantic.go — SemanticCache struct
  Stores (embedding_vector, projected_result, watermark, expiry, scope_hash)
  Lookup: compute cosine similarity between query embedding and stored embeddings
  Return hit if similarity >= threshold (default 0.92) AND watermark is fresh
  AND scope hashes match
  Methods: Lookup(embedding []float32, policy PolicyContext, watermark uint64)
           Store(embedding []float32, rows []map[string]interface{},
                 policy PolicyContext, watermark uint64)

- internal/cache/fingerprint.go — CosineSimilarity(a, b []float32) float32
  Standard cosine similarity implementation

- internal/query/hybrid.go — HybridMerge function
  Input: structuredRows []map[string]interface{}, semanticRows []map[string]interface{},
         limit int
  Output: deduplicated, merged []map[string]interface{}
  Strategy: semantic results first (preserve similarity order),
  append structured results not already in semantic set,
  trim to limit

- Wire cascade.go stage2SemanticCache — replace stub with SemanticCache.Lookup()
- Wire cascade.go stage5HybridMerge — replace stub with HybridMerge()

- Update /api/cache endpoint to include semantic cache stats:
  SemanticHits, SemanticMisses, SimilarityThreshold

Requirements:
- Semantic cache never shares entries across different policy scopes
- Stale entries (watermark mismatch) are not served
- Hybrid merge never returns duplicate payload_ids
- Similarity threshold configurable per source policy
- go build ./... and go vet ./... pass
```

---

## Phase 7: MCP Server + OpenAI Endpoint + Streaming

**Duration:** 4-5 days
**Depends on:** Phase 6

### AI Prompt for Phase 7

```
You are helping build BubbleFish Nexus v2 — a single-binary Go AI routing daemon.

TASK: Phase 7 — MCP Server + OpenAI Endpoint + Streaming

Files that already exist: all Phase 0-6 files

Implement:

--- MCP Server ---
- internal/mcp/server.go — MCPServer struct
  Implements the Model Context Protocol JSON-RPC server on configurable port (default 8082)
  Exposes three tools:
  1. nexus_write: maps to internal write pipeline (bypasses HTTP, calls directly)
  2. nexus_search: maps to cascade.Execute() with CanonicalQuery
  3. nexus_status: returns daemon status JSON
  Source identity for MCP calls uses a dedicated mcp_key from daemon.toml

- internal/mcp/tools.go — tool definitions with JSON schemas

- Add to daemon.toml template:
  [daemon.mcp]
  enabled = true
  port = 8082
  source_name = "mcp"
  api_key = "env:MCP_API_KEY"

- Wire MCPServer into daemon startup in internal/daemon/daemon.go
  Start in goroutine alongside HTTP server

- Add 'mcp' subcommand to main.go that starts MCP server standalone

--- OpenAI-Compatible Endpoint ---
- Add POST /v1/memories to internal/daemon/handlers.go
  Accepts OpenAI chat message format:
  { "messages": [{"role": "user", "content": "..."}], "model": "..." }
  Translates to internal write pipeline
  Returns: { "id": "...", "object": "memory", "created": 1234567890 }

--- Streaming Reads ---
- Update GET /query/{destination} to detect Accept: text/event-stream header
  When detected: stream rows as Server-Sent Events
  Format: data: {json row}\n\n
  Terminate with: data: [DONE]\n\n
  Uses http.Flusher for progressive delivery

Requirements:
- MCP server disabled by default, enabled by daemon.toml config
- MCP tools call internal pipeline directly (not HTTP round-trip)
- OpenAI endpoint uses same auth and rate limiting as /inbound
- Streaming falls back to normal JSON if client does not send SSE header
- go build ./... and go vet ./... pass

Claude Desktop config to document in README:
{
  "mcpServers": {
    "bubblefish-nexus": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-sse"],
      "env": { "SSE_URL": "http://localhost:8082/sse" }
    }
  }
}
```

### Phase 7 Verification

```powershell
# Start daemon with MCP enabled
$env:MCP_API_KEY="nexus-mcp-key-001"
.\bubblefish.exe daemon

# Test MCP server is running
Invoke-RestMethod -Method GET -Uri http://localhost:8082/health

# Test OpenAI-compatible endpoint
$h = @{"Authorization"="Bearer nexus-key-claude-001";"Content-Type"="application/json"}
$b = '{"messages":[{"role":"user","content":"Testing OpenAI-compatible write endpoint"}],"model":"claude-opus-4-5"}'
Invoke-RestMethod -Method POST -Uri http://localhost:8080/v1/memories -Headers $h -Body $b

# Test streaming read
Invoke-WebRequest -Uri "http://localhost:8080/query/sqlite?limit=5" `
    -Headers @{"Authorization"="Bearer nexus-key-claude-001";"Accept"="text/event-stream"} `
    -UseBasicParsing
```

---

## Phase 8: System Tray + One-Command Launch

**Duration:** 3-4 days
**Depends on:** Phase 7

### AI Prompt for Phase 8

```
You are helping build BubbleFish Nexus v2 — a single-binary Go AI routing daemon.

TASK: Phase 8 — System Tray + One-Command Launch

Files that already exist: all Phase 0-7 files

Implement:

--- bubblefish install command ---
- cmd/bubblefish/main.go — add 'install' subcommand
  Sequence:
  1. RunInit(baseDir) — create ~/.bubblefish/Nexus/ with templates
  2. Print "Edit your source and destination TOMLs, then press Enter to continue"
  3. Wait for Enter
  4. RunBuild(baseDir) — compile configs
  5. RunDoctor(cfg, sources, dests) — health checks
  6. If all pass: print "Nexus is ready! Run: bubblefish start"
  7. If any fail: print specific fix instructions

--- bubblefish start command ---
- cmd/bubblefish/main.go — add 'start' subcommand
  Launches in one command:
  1. Start HTTP daemon (port 8080)
  2. Start MCP server (port 8082) if enabled
  3. Start web dashboard (port 8081)
  4. Start system tray (if --tray flag or Windows)
  All as goroutines managed by a single WaitGroup
  Ctrl+C triggers graceful shutdown of all components

--- System Tray ---
- internal/tray/tray.go — Tray struct using github.com/getlantern/systray
  Icon states:
  - Green fish (running, all healthy)
  - Yellow fish (degraded — circuit open or queue backing up)
  - Red fish (daemon down)
  Right-click menu:
  - "BubbleFish Nexus v2.0.0" (disabled header)
  - Separator
  - "Open Web Dashboard" → opens http://localhost:8081 in default browser
  - "Open TUI" → launches bubblefish ui in new terminal
  - "View Logs" → opens log file in default editor
  - Separator
  - "Restart Daemon"
  - "Run Doctor"
  - Separator
  - "Quit"

  Tray polls GET /health every 5 seconds to update icon state

- Wire tray into 'start' subcommand as optional goroutine

Requirements:
- 'install' is idempotent — safe to run multiple times
- 'start' exits cleanly on Ctrl+C with graceful shutdown of all components
- Tray icon updates within 10 seconds of daemon state change
- Tray compiles and runs without errors on Windows
- If systray not available (headless Linux), tray goroutine is a no-op
- go build ./... and go vet ./... pass
```

### Phase 8 Verification

```powershell
# Full one-command install (first time user experience)
.\bubblefish.exe install

# One-command start
.\bubblefish.exe start

# Verify all ports
Invoke-RestMethod -Method GET -Uri http://localhost:8080/health
Invoke-RestMethod -Method GET -Uri http://localhost:8081/
Invoke-RestMethod -Method GET -Uri http://localhost:8082/health
```

---

## Phase 9: Testing + Hardening + Ship v2.0.0

**Duration:** 3-4 days
**Depends on:** Phase 8

```powershell
# Full test suite with race detector
$env:PATH += ";C:\msys64\mingw64\bin"
$env:CGO_ENABLED="1"
go test ./... -v -race -count=1

# Static analysis
go vet ./...
go install honnef.co/go/tools/cmd/staticcheck@latest
staticcheck ./...

# Load test — 1000 requests
$failed = 0
for ($i = 1; $i -le 1000; $i++) {
    $key = "v2-load-" + $i.ToString()
    $h = @{
        "Authorization" = "Bearer nexus-key-claude-001"
        "Content-Type" = "application/json"
        "X-Idempotency-Key" = $key
    }
    $b = '{"message":{"content":"V2 load test payload","role":"user"},"model":"claude-opus-4-5"}'
    try {
        Invoke-RestMethod -Method POST -Uri http://localhost:8080/inbound/claude -Headers $h -Body $b | Out-Null
    } catch { $failed++ }
}
Write-Host "Load test: Sent 1000. Failed: $failed"

# Tag and release
go build ./...
git add -A
git commit -m "Release v2.0.0 — BubbleFish Nexus"
git tag v2.0.0
git push origin master --tags

# Build release assets
New-Item -ItemType Directory -Force -Path dist
$env:GOOS="windows"; $env:GOARCH="amd64"
go build -ldflags "-X github.com/shawnsammartano-hub/bubblefish-nexus/internal/version.Version=2.0.0" -o dist\bubblefish-v2.0.0-windows-amd64.exe .\cmd\bubblefish\
$env:GOOS="linux"; $env:GOARCH="amd64"
go build -ldflags "-X github.com/shawnsammartano-hub/bubblefish-nexus/internal/version.Version=2.0.0" -o dist\bubblefish-v2.0.0-linux-amd64 .\cmd\bubblefish\
$env:GOOS="darwin"; $env:GOARCH="amd64"
go build -ldflags "-X github.com/shawnsammartano-hub/bubblefish-nexus/internal/version.Version=2.0.0" -o dist\bubblefish-v2.0.0-darwin-amd64 .\cmd\bubblefish\
$env:GOOS="darwin"; $env:GOARCH="arm64"
go build -ldflags "-X github.com/shawnsammartano-hub/bubblefish-nexus/internal/version.Version=2.0.0" -o dist\bubblefish-v2.0.0-darwin-arm64 .\cmd\bubblefish\
Remove-Item Env:\GOOS; Remove-Item Env:\GOARCH
Get-ChildItem dist\
```

---

## V2 Quick-Start (5 Minutes, No Book Required)

This goes in the README. The goal is that someone can be running Nexus in 5 minutes.

```powershell
# Step 1: Download binary from GitHub releases
# Go to: https://github.com/shawnsammartano-hub/BubbleFish-Nexus/releases
# Download bubblefish-v2.0.0-windows-amd64.exe
# Rename to bubblefish.exe and add to PATH

# Step 2: Run install wizard
bubblefish install
# Follow the prompts. Edit your source TOMLs with your API keys.
# Set SUPABASE_URL and SUPABASE_SERVICE_ROLE_KEY env vars.

# Step 3: Start everything
bubblefish start
# Opens web dashboard, starts MCP server, shows tray icon

# Step 4: Add to Claude Desktop
# Edit: C:\Users\{you}\AppData\Roaming\Claude\claude_desktop_config.json
# Add:
# {
#   "mcpServers": {
#     "memory": {
#       "command": "bubblefish",
#       "args": ["mcp"]
#     }
#   }
# }
# Restart Claude Desktop. Done.
```

---

## How to Use Each Phase Prompt

1. Start a fresh Claude conversation
2. Attach the Nexus Spec v2 document
3. Attach the Build Plan v2 document
4. Say: "The following files already exist:" and list files from prior phases
5. Paste the phase prompt
6. Run PowerShell commands as directed
7. All acceptance criteria must pass before next phase

---

## V2 Phase Summary

```
Phase 0  — V2 Scaffold + WAL fix           ✓ 2 days
Phase 1  — Policy Model                    ✓ 3-4 days
Phase 2  — Projection Engine               ✓ 2-3 days
Phase 3  — Stage 0+3 Cascade              ✓ 3-4 days
Phase 4  — Exact Cache                     ✓ 3-4 days
Phase 5  — Embedding + Semantic Retrieval  ✓ 5-6 days
Phase 6  — Semantic Cache + Hybrid Merge   ✓ 4-5 days
Phase 7  — MCP + OpenAI + Streaming        ✓ 4-5 days
Phase 8  — Tray + Install + Start          ✓ 3-4 days
Phase 9  — Testing + v2.0.0               ✓ 3-4 days
                                    Total: ~32-38 days
```
