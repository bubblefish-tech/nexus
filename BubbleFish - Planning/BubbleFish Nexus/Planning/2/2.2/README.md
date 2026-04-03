<p align="center">
  <h1 align="center">🐟 BubbleFish Nexus</h1>
  <p align="center"><strong>Your AI clients forget everything when the conversation ends. Nexus fixes that.</strong></p>
  <p align="center">Shared persistent memory across Claude, Perplexity, Ollama, and any AI client. Runs entirely on your machine. Crash-safe.</p>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> •
  <a href="#crash-recovery-demo">Crash Recovery</a> •
  <a href="#how-it-works">How It Works</a> •
  <a href="#ollama-setup">Ollama Setup</a> •
  <a href="#mcp-claude-desktop">MCP / Claude Desktop</a> •
  <a href="#configuration">Configuration</a> •
  <a href="#api">API</a> •
  <a href="#known-limitations">Known Limitations</a>
</p>

---

## The Problem

Every AI memory solution available today asks you to make the same trade: send your data to someone else's cloud, or glue together Python scripts that lose data when they crash.

Nexus takes a different position. Your agent's memory belongs on your disk, not on a VC-funded server. And it should survive a power failure.

## What Nexus Is

A single Go binary. A local proxy that sits between your AI clients and a memory database. It handles authentication, deduplication, crash-safe writes, intelligent retrieval, and response shaping. You configure it with TOML files. You observe it through structured logs, Prometheus metrics, and a live dashboard. You never have to think about data leaving your machine.

One daemon. Many AI clients. One protected memory backend. Zero duplicate writes. Minimal tokens.

## Crash Recovery Demo

This is the pitch. Everything else is details.

```bash
# 1. Send 50 memories through Nexus
for i in $(seq 1 50); do
  curl -s -X POST http://localhost:8080/inbound/claude \
    -H "Authorization: Bearer $KEY" \
    -H "Content-Type: application/json" \
    -H "X-Idempotency-Key: demo-$i" \
    -d "{\"message\":{\"content\":\"Memory $i\",\"role\":\"user\"},\"model\":\"test\"}"
done

# 2. Kill the daemon mid-flight
kill -9 $(pgrep bubblefish)

# 3. Restart
bubblefish start

# 4. Query — all 50 are there. Zero duplicates. Zero data loss.
curl http://localhost:8080/query/sqlite -H "Authorization: Bearer $KEY" | jq '.results | length'
# → 50
```

Every payload hits the Write-Ahead Log with a CRC32 checksum and an fsync before Nexus acknowledges receipt. The WAL uses atomic file renames on the same filesystem. Idempotency keys are rebuilt from the WAL on restart. There is no window where data is at risk.

Try that with a Python script.

## Quick Start

### Download

Grab the binary for your platform from [Releases](https://github.com/bubblefish-tech/nexus/releases). No Go installation required.

```bash
# Linux
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-linux-amd64 -o bubblefish
chmod +x bubblefish
sudo mv bubblefish /usr/local/bin/

# macOS (Apple Silicon)
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-darwin-arm64 -o bubblefish
chmod +x bubblefish && sudo mv bubblefish /usr/local/bin/
```

### Install and Run

```bash
bubblefish install --dest sqlite    # Creates config with SQLite. Zero cloud accounts needed.
bubblefish start                    # Daemon + MCP server + web dashboard. Done.
```

That's it. Nexus is running on `localhost:8080` with a SQLite backend. No Docker, no Python, no cloud signup.

### Write a Memory

```bash
curl -X POST http://localhost:8080/inbound/claude \
  -H "Authorization: Bearer YOUR_SOURCE_KEY" \
  -H "Content-Type: application/json" \
  -d '{"message":{"content":"User prefers dark mode","role":"user"},"model":"claude-4"}'
```

### Read It Back

```bash
curl http://localhost:8080/query/sqlite \
  -H "Authorization: Bearer YOUR_SOURCE_KEY"
```

### Check Health

```bash
bubblefish doctor    # Checks daemon, destinations, disk space, config validity
```

## How It Works

Nexus is a gateway. Data flows in from AI clients, gets persisted durably, then gets retrieved intelligently.

### Write Path

1. AI client sends a POST with an API key
2. Constant-time auth (no timing attacks)
3. Permission check (CanWrite enforced, not decorative)
4. Idempotency check *before* rate limiting (duplicates don't burn your rate budget)
5. WAL append + CRC32 + fsync (data is safe before anything else happens)
6. Non-blocking queue send (if queue is full, you get a 429, not a hung connection)
7. Worker writes to destination database
8. WAL entry marked DELIVERED with atomic rename

### Read Path — The 6-Stage Retrieval Cascade

Nexus picks the cheapest safe retrieval method first:

| Stage | What It Does | When It Runs |
|-------|-------------|--------------|
| 0 | Policy gate | Always. Checks permissions before any work. |
| 1 | Exact cache | Byte-identical repeat query? Return cached result. |
| 2 | Semantic cache | Near-duplicate query? Return if similarity >= 0.92. |
| 3 | Structured lookup | Metadata filters? Parameterized SQL (no string concatenation). |
| 4 | Semantic retrieval | Vector similarity via sqlite-vec, pgvector, or Supabase. |
| 5 | Hybrid merge + temporal decay | Deduplicate, rerank by recency, trim to limit. |

Every response includes `_nexus` metadata telling you which stage served it and why. If semantic search wasn't available, it tells you that too. No guessing.

### Temporal Decay

Standard vector search treats all memories equally. A fact from two years ago competes with one from yesterday. That causes contradiction loops.

Nexus fixes this by default. After retrieving the top 100 candidates by vector similarity, it applies a time-weighted rerank:

```
final_score = (cosine_similarity × 0.7) + (recency_weight × 0.3)
```

Newer facts automatically outrank older contradictions. This is configurable:

```toml
[retrieval]
time_decay = true       # turn off for raw vector distance
half_life_days = 7      # tune to your use case
```

## Ollama Setup

Nexus works with Ollama and Ollama-based frontends (Open WebUI, LM Studio).

### 1. Create a Source

```bash
# ~/.bubblefish/Nexus/sources/ollama.toml
[source]
name = "ollama"
api_key = "env:OLLAMA_SOURCE_KEY"
namespace = "ollama"
can_read = true
can_write = true
target_destination = "sqlite"

[source.mapping]
content = "message.content"
model   = "model"
role    = "message.role"
```

### 2. (Optional) Free Local Semantic Search

Use Ollama as your embedding provider. Zero cost, zero cloud:

```toml
# In daemon.toml
[daemon.embedding]
enabled = true
provider = "ollama"
url = "http://localhost:11434"
model = "nomic-embed-text"
dimensions = 768
```

```bash
ollama pull nomic-embed-text   # One-time download
```

### Open WebUI Note

Open WebUI doesn't generate embeddings by default. Without an embedding provider, Nexus uses structured lookup (Stage 3) instead of semantic search (Stage 4). Still useful, just not similarity-based. Configure the embedding provider above to unlock full semantic search.

## MCP / Claude Desktop

Nexus exposes an MCP (Model Context Protocol) server so Claude Desktop and Cursor can use it as a native memory tool.

### 1. Enable MCP

```toml
# In daemon.toml
[daemon.mcp]
enabled = true
port = 8082
bind = "127.0.0.1"    # Never 0.0.0.0. Enforced in code.
source_name = "mcp"
api_key = "env:MCP_API_KEY"
```

### 2. Verify It Works

```bash
bubblefish mcp test    # Starts MCP → sends nexus_status → exits 0 on success
```

Run this *before* touching Claude Desktop. If `mcp test` fails, Claude won't connect either.

### 3. Configure Claude Desktop

```json
{
  "mcpServers": {
    "memory": {
      "command": "bubblefish",
      "args": ["mcp"]
    }
  }
}
```

> **macOS PATH gotcha:** Claude Desktop uses the *system* PATH, not your shell PATH. Use the full path: `"command": "/usr/local/bin/bubblefish"`.

### MCP Tools

| Tool | What It Does |
|------|-------------|
| `nexus_write` | Persist a memory |
| `nexus_search` | Retrieve via the full 6-stage cascade |
| `nexus_status` | Daemon health check |

MCP calls go through the internal pipeline directly, not via HTTP. Lower latency than the REST API.

## Configuration

Nexus uses TOML files. Configuration is code. The web dashboard is read-only.

### Directory Layout

```
~/.bubblefish/Nexus/
  daemon.toml              # Main config
  sources/
    claude.toml            # One file per AI client
    ollama.toml
  destinations/
    sqlite.toml            # One file per backend
  compiled/                # Auto-generated, don't edit
  wal/                     # Write-Ahead Log
```

All config directories are `0700`. All files are `0600`. API keys are never world-readable.

### Secrets

Three ways to provide API keys:

```toml
api_key = "env:CLAUDE_KEY"              # Environment variable
api_key = "file:/run/secrets/claude"    # Docker Secrets / Kubernetes Secrets
api_key = "my-dev-key"                  # Literal (dev only)
```

Secret values are never logged at any level. Not at DEBUG. Not at TRACE. Never.

### Destinations

SQLite is the default. Zero configuration, zero cloud accounts. For teams or homelabs:

| Backend | When To Use |
|---------|------------|
| SQLite | Personal use. Single machine. Zero setup. |
| PostgreSQL (pgvector) | Teams, homelabs, multi-node. |
| Supabase (OpenBrain) | Cloud-hosted Postgres with built-in vector search. |

Swap backends by editing one line in your destination TOML and restarting. Core routing logic doesn't change.

## API

| Endpoint | Method | Description |
|----------|--------|------------|
| `/inbound/{source}` | POST | Write a memory. Returns `payload_id`. |
| `/v1/memories` | POST | OpenAI-compatible write format. |
| `/query/{destination}` | GET | Retrieve memories. Supports `?limit`, `?cursor`, SSE. |
| `/api/status` | GET | Daemon health (admin auth required). |
| `/metrics` | GET | Prometheus metrics. |
| `/health` | GET | Liveness probe. |
| `/ready` | GET | Readiness probe (checks destinations). |

### Error Format

Every error uses the same shape. No special-casing:

```json
{
  "error": "rate_limit_exceeded",
  "message": "too many requests",
  "retry_after_seconds": 30
}
```

### Pagination

```bash
# First page
curl "http://localhost:8080/query/sqlite?limit=20"

# Next page (use _nexus.next_cursor from previous response)
curl "http://localhost:8080/query/sqlite?limit=20&cursor=eyJ0aW1l..."
```

Cursors are stable across writes. New data doesn't shift existing pages.

## Docker

```yaml
# docker-compose.yml
version: '3.8'
services:
  nexus:
    image: bubblefish/nexus:latest
    ports:
      - "8080:8080"
      - "8081:8081"
      - "8082:8082"
    volumes:
      - nexus-wal:/data/wal
      - nexus-config:/data/config
      - nexus-compiled:/data/compiled
    secrets:
      - admin_token
      - claude_key
    environment:
      - ADMIN_TOKEN=file:/run/secrets/admin_token
      - CLAUDE_SOURCE_KEY=file:/run/secrets/claude_key
    read_only: true

volumes:
  nexus-wal:
  nexus-config:
  nexus-compiled:

secrets:
  admin_token:
    file: ./secrets/admin_token
  claude_key:
    file: ./secrets/claude_key
```

WAL, config, and compiled directories are writable volumes. Everything else is read-only.

## Headless Linux

Running on Unraid, TrueNAS, a Raspberry Pi, or any server without a display? Nexus detects `$DISPLAY` is empty and skips the system tray. No error. No crash. Just an INFO log:

```
INFO: running headless, system tray disabled
```

Everything else works normally.

## Observability

- **Structured JSON logs** via Go's `log/slog`. Configurable level and format in `daemon.toml`.
- **Prometheus metrics**: payload latency, throughput per source, queue depth, cache hit rates, WAL pending entries. All actually recorded (not permanently zero).
- **Web dashboard** on port 8081. Read-only. Requires admin token.
- **`bubblefish doctor`**: checks daemon, destinations, disk space, config validity. Ollama-specific error messages if you're using Ollama.

## How Is This Different?

Most AI memory solutions fall into one of three categories. Nexus doesn't fit any of them.

**Cloud memory services** give you managed infrastructure but take your data. Every conversation, every preference, every private thought passes through someone else's servers. For developers handling proprietary code or personal data, that's a non-starter.

**Agent frameworks** couple memory to their execution model. If you want memory across different AI clients, you have to rewrite your integration for each framework. Nexus sits between any client and any backend. It's a gateway, not a framework.

**Python wrappers** are quick to build but lose data on crash, don't deduplicate, don't rate-limit, and require a runtime environment. Nexus is a single binary with a WAL, CRC32 checksums, idempotency tracking, constant-time auth, and a circuit breaker. It's infrastructure, not a script.

| Capability | Cloud Services | Agent Frameworks | Python Wrappers | **Nexus** |
|-----------|---------------|-----------------|----------------|-----------|
| Data stays on your machine | No | Depends | Yes | **Yes** |
| Crash-safe WAL with checksums | Varies | No | No | **Yes** |
| Works with any AI client | No (siloed) | No (coupled) | Manual | **Yes** |
| Single binary, zero deps | No | No | No | **Yes** |
| Idempotency + dedup | Varies | Rare | No | **Yes** |
| Rate limiting on reads + writes | Varies | No | No | **Yes** |
| Temporal decay reranking | Some | Some | No | **Yes** |
| MCP native integration | Some | Some | No | **Yes** |

## Known Limitations

Being honest about what Nexus doesn't do is more useful than pretending it does everything.

| Limitation | Details | Roadmap |
|-----------|---------|---------|
| No RBAC | Per-source API keys provide isolation. Sufficient for personal and small-team use. | Enterprise tier |
| SQLite single-writer | Writes are serialized. Works for personal use. Use PostgreSQL for concurrent teams. | Documented tradeoff |
| Client-side embedding | Nexus doesn't generate embeddings itself. If your frontend (like Open WebUI) doesn't send embeddings, configure a separate embedding provider. | v3: built-in ONNX |
| Cache lost on restart | Cache is a speed optimization, not a data store. The WAL and destination have everything. | v3: persistent cache |
| No built-in TLS | Nexus binds to localhost. Use Cloudflare Tunnel for remote access. | Documented tradeoff |
| Source-only hot reload | Changing destinations requires a restart. We fail safe rather than risking in-flight data. | Design decision |
| Temporal decay is heuristic | Doesn't replace entity resolution or knowledge graphs. Solves ~90% of contradiction cases. | v3: investigation |
| No background summarization | Nexus is a router, not an AI. Token management is the client's job for now. | v3: investigation |

## Building from Source

```bash
git clone https://github.com/bubblefish-tech/nexus.git
cd nexus
go build -o bubblefish ./cmd/bubblefish/
```

Running tests with race detection requires GCC (or TDM-GCC on Windows):

```bash
CGO_ENABLED=1 go test ./... -race
```

End users never need Go or GCC. The pre-built binaries include everything.

## Contributing

Contributions use the [Developer Certificate of Origin](https://developercertificate.org/) (DCO). Sign off your commits:

```bash
git commit -s -m "Add feature X"
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## License

[Apache License 2.0](LICENSE)

---

<p align="center">
  Built by <a href="https://github.com/bubblefish-tech">BubbleFish Technologies, Inc.</a><br/>
  Your data. Your machine. Your rules.
</p>
