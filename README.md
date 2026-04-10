# BubbleFish Nexus

**THE Underlying AI Memory Infrastructure. Connects the memory of your AI apps and agents. Survives `kill -9`.**

One daemon. Shared memory across Claude Desktop, ChatGPT, Perplexity, OpenWebUI, OpenClaw, LM Studio, and Cursor — all at once. Stored on your machine. Zero data loss.

Single Go binary. No Docker required. Runs on Windows, Linux, macOS.

---

## The problem

Every AI client keeps its memory in its own silo. Claude doesn't know what you told ChatGPT. Perplexity doesn't know what Claude learned. When the session ends, everything disappears.

Your AI memory is fragmented across vendors, and none of them talk to each other.

Nexus is a local daemon that gives all your AI clients a shared, persistent memory. Write a memory from Claude Desktop. Read it back from ChatGPT. Kill the process mid-write. Restart. Nothing lost.

Verified working simultaneously: Claude Desktop, ChatGPT Desktop, ChatGPT Web, Claude Web, Perplexity Comet, OpenWebUI, and LM Studio — all reading and writing to the same memory store in one session.

---

## What it does

**Shared memory across all your AI apps.** Any client connected via MCP or HTTP can write a memory. Every other client can read it. No additional configuration. The shared model is the default.

**Crash-safe persistence.** Every write hits a Write-Ahead Log with a CRC32 checksum and an fsync before you get a 200 OK. Kill the process mid-write. Restart. The WAL replays automatically. There is no window where data is at risk.

**6-stage retrieval cascade.** Policy gate → exact cache → semantic cache → structured lookup → vector search → hybrid merge with temporal decay. Cheapest safe path first. Every response tells you which stage fired.

**Multi-destination routing.** One write fans out to SQLite, PostgreSQL (pgvector), and Supabase asynchronously. Local fast access and remote shared access at the same time.

**MCP native.** Claude Desktop and Cursor integration via Model Context Protocol. Same pipeline as HTTP, lower latency.

**Local-first.** Binds to localhost by default. Your data never leaves your machine unless you configure it to.

---

## Crash recovery demo

This is the pitch. Everything else is details.

```bash
# 1. Send 50 memories through Nexus
for i in $(seq 1 50); do
  curl -s -X POST http://localhost:8080/inbound/default \
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

Or just run:

```bash
bubblefish demo    # Does all of the above automatically. Exits 0 on success.
```

---

## Quick start

### Linux

```bash
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-linux-amd64 -o bubblefish
chmod +x bubblefish && sudo mv bubblefish /usr/local/bin/
bubblefish install --mode simple
bubblefish start
```

### Windows (PowerShell)

```powershell
Invoke-WebRequest -Uri "https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-windows-amd64.exe" -OutFile bubblefish.exe
.\bubblefish.exe install --mode simple
.\bubblefish.exe start
```

### macOS (Apple Silicon)

```bash
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-darwin-arm64 -o bubblefish
chmod +x bubblefish && sudo mv bubblefish /usr/local/bin/
bubblefish install --mode simple
bubblefish start
```

That's it. Nexus is running on `localhost:8080` with a SQLite backend, a single source, and an auto-generated API key. No Docker, no Python, no cloud signup, no embedding API keys. Simple Mode prints your key and example commands.

---

## Connect your clients

### Claude Desktop

```json
{
  "mcpServers": {
    "bubblefish-nexus": {
      "command": "bubblefish",
      "args": ["mcp"]
    }
  }
}
```

### ChatGPT / Perplexity / remote clients

Expose Nexus via Cloudflare Tunnel, then use the tunnel URL as your MCP endpoint:

```bash
cloudflared tunnel --url http://localhost:7474
```

### OpenWebUI + Ollama

```bash
bubblefish install --dest sqlite --profile openwebui
bubblefish start
```

### OpenClaw

Install the **bubblefish-nexus** plugin. See [OPENCLAW_SKILL.md](docs/OPENCLAW_SKILL.md).

Full setup for all clients: [MCP_CLIENTS.md](docs/MCP_CLIENTS.md)

---

## Benchmarks

```bash
bubblefish bench --mode throughput   # writes/sec, WAL latency, p50/p95/p99
bubblefish bench --mode latency      # per-stage read breakdown
bubblefish bench --mode eval         # retrieval precision/recall/MRR/NDCG
```

---

## How is this different?

| Capability | Cloud Services | Agent Frameworks | Script Wrappers | **Nexus** |
|-----------|---------------|-----------------|----------------|-----------|
| Data stays on your machine | No | Depends | Yes | **Yes** |
| Crash-safe WAL with checksums | Varies | No | No | **Yes** |
| Works with any AI client | No (siloed) | No (coupled) | Manual | **Yes** |
| Single binary, zero deps | No | No | No | **Yes** |
| Idempotency + dedup | Varies | Rare | No | **Yes** |
| Temporal decay reranking | Some | Some | No | **Yes** |
| Tamper detection (HMAC) | Varies | No | No | **Yes** |
| MCP native integration | Some | Some | No | **Yes** |
| 60-second setup | No | No | Maybe | **Yes** |
| Built-in reliability demo | No | No | No | **Yes** |

---

## Known limitations

- Single WAL encryption key. Key rotation ring is planned for a future version.
- No RBAC. Per-source API keys and policies provide isolation. Sufficient for personal and small-team use.
- No clustering or HA. Single-node daemon.
- The MCP server does not currently support mutual TLS (use a reverse proxy for mTLS).

---

## Roadmap — v0.2

**Coming in v0.2: Credential gateway.** Synthetic key routing so your AI clients never touch your real provider API keys. Rate limiting per synthetic key. Model allowlists.

Target-state design:

```toml
# daemon.toml (planned — not implemented in v0.1.x)
[credentials]
  [[credentials.mappings]]
  synthetic_prefix = "bfn_sk_"
  provider = "openai"
  real_key = "env:OPENAI_API_KEY"
  allowed_models = ["gpt-4o", "gpt-4o-mini"]
  rate_limit_rpm = 100
```

This will let Nexus act as a credential proxy — clients authenticate with a `bfn_sk_` synthetic key and Nexus substitutes the real provider key on the upstream call. Not yet implemented.

---

## Docs

- [Deployment](docs/DEPLOYMENT.md) — install, service setup, Cloudflare Tunnel, monitoring
- [Configuration](docs/CONFIGURATION.md) — full annotated `daemon.toml` reference
- [MCP Clients](docs/MCP_CLIENTS.md) — setup for every supported client
- [API Reference](docs/API.md) — HTTP API endpoints
- [OpenClaw Plugin](docs/OPENCLAW_SKILL.md) — bubblefish-nexus plugin guide
- [Security](SECURITY.md) — security model and vulnerability reporting
- [Changelog](CHANGELOG.md) — release history

---

## Building from source

```bash
git clone https://github.com/bubblefish-tech/nexus.git
cd nexus
go build -o bubblefish ./cmd/bubblefish/
```

Running tests with race detection requires GCC:

```bash
CGO_ENABLED=1 go test ./... -race
```

End users never need Go or GCC. The pre-built binaries include everything.

---

## Contributing

Contributions use the [Developer Certificate of Origin](https://developercertificate.org/) (DCO). Sign off your commits:

```bash
git commit -s -m "Add feature X"
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

---

## License

AGPL-3.0. See [LICENSE](LICENSE).

Commercial license available for SaaS deployment, OEM embedding, or no-AGPL-obligation use. Contact: licensing@bubblefish.sh

---

*Built by [Shawn Sammartano](https://github.com/bubblefish-tech) / BubbleFish Technologies, Inc.*
*Your data. Your machine. Your rules.*
