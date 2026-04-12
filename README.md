# BubbleFish Nexus

**Your AI clients never touch your real API keys. And nothing they remember is ever lost.**

Nexus sits between your AI clients and your providers. It holds your real credentials. Clients get synthetic keys. If a key leaks, you rotate one thing. Nothing downstream breaks.

Every memory written through Nexus is protected by a write-ahead log that survives `kill -9`. Every read goes through a six-stage retrieval cascade with temporal decay. Every call is authenticated, rate-limited, and logged.

One Go binary. No Docker required. Runs on Windows, Linux, and macOS. AGPL-3.0. Patent pending.

---

## The two problems it solves

### 1. Your real provider keys are scattered everywhere

Every AI client you run — Claude Desktop, ChatGPT, Perplexity, OpenWebUI, LM Studio — needs your real provider keys. They live in config files, environment variables, and `.env` files spread across machines. One leaks and you're rotating everywhere, hoping you caught every place they lived.

Nexus fixes this with one architectural decision: **real keys never leave Nexus.**

```
Your clients → bfn_route_synthetic_key → Nexus → sk-real-key → OpenAI / Anthropic / etc.
```

Your clients use synthetic `bfn_route_` keys that Nexus generates. Those keys are worthless to anyone who intercepts them — they only work through your Nexus instance, which holds the real credentials. Rotate a real key in one config file. Every client keeps working.

### 2. Your AI clients forget everything when the conversation ends

ChatGPT has memory. Claude has memory. Perplexity has memory. None of them know what you told the others. Each one remembers a fragment of you, locked inside someone else's product, deletable on a policy change you didn't agree to.

Nexus is the memory substrate underneath all of them. Any client connected via MCP can write a memory. Every other client can read it. ChatGPT writes something. Claude reads it. Perplexity reads it too.

**Verified end-to-end, in one session:** ChatGPT Desktop, ChatGPT Web, Claude Desktop, Claude Web, Perplexity Comet, OpenWebUI, and LM Studio all connected to the same Nexus instance simultaneously and read each other's memories.

---

## The kill -9 demo

This is the pitch. Everything else is supporting evidence.

```bash
# 1. Write four real memories from any client
# 2. Find the daemon and kill it with extreme prejudice
ps aux | grep bubblefish
kill -9 <pid>

# 3. Restart
./bubblefish start

# 4. Query. All four memories return with full content.
./bubblefish status
```

WAL replay runs in milliseconds. `pending_entries=0`. Nothing lost. Personally verified — write four memories, force-kill the process mid-flight, restart, query, all four return.

Run it yourself with `./bubblefish demo`, which scripts the whole thing.

---

## How it works

Five primitives, integrated into one binary:

**Credential gateway.** Real provider keys live in one place. Clients use synthetic keys that only work through your Nexus instance. Rotate real keys without touching client configs.

**Write-ahead log with crash recovery.** Every write hits the WAL with a CRC32 checksum before you get a 200 OK. Optional HMAC-SHA256 integrity mode. Optional AES-256-GCM encryption at rest. Survives `kill -9`. Replays automatically on restart.

**Six-stage retrieval cascade.** Exact-match cache → semantic cache → keyword → recency → subject filter → full scan with temporal decay. Stops at the first stage that returns results. Every response includes which stage fired.

**Multi-destination routing.** One write fans out to SQLite, ChromaDB, and Postgres or Supabase asynchronously. Local fast access and remote shared access at the same time, with the WAL as the single source of truth.

**Protocol gateway.** Speaks MCP to Claude Desktop, OAuth 2.1 to ChatGPT, SSE to Perplexity Comet, Pipelines to OpenWebUI, and plain HTTP to anything else. Three token classes (admin, data, MCP) with strict separation.

Together, these form the **Black Box Recorder**: every credential routing decision, every memory write, every retrieval, every policy gate is captured in a tamper-evident append-only record. When something goes wrong, you can replay it. When compliance asks, you can produce it.

---

## Quick start

```bash
# Linux / macOS
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish_$(uname -s)_$(uname -m).tar.gz | tar xz
./bubblefish install --mode simple
./bubblefish start
```

```powershell
# Windows
iwr -useb https://get.bubblefish.ai/install.ps1 | iex
.\bubblefish install --mode simple
.\bubblefish start
```

The install command writes a `daemon.toml`, generates an admin token, a data token, and an MCP token, creates the default source, and prints the next four steps. The start command brings up three listeners: HTTP API on **8080**, MCP server on **7474**, web dashboard on **8081**.

Open `http://localhost:8081` to see memories arriving in real time.

---

## Connect your clients

### Claude Desktop

```json
{
  "mcpServers": {
    "bubblefish-nexus": {
      "url": "http://localhost:7474",
      "headers": {
        "Authorization": "Bearer YOUR_BFN_MCP_KEY"
      }
    }
  }
}
```

### ChatGPT, Perplexity, and other remote clients

Expose Nexus via Cloudflare Tunnel, then use the tunnel URL as your MCP endpoint. Nexus validates the `bfn_mcp_` key on every request regardless of what passes through the tunnel.

```bash
cloudflared tunnel --url http://localhost:7474
```

ChatGPT additionally supports OAuth 2.1 (RSA-2048, JWT RS256, PKCE S256) for browser-mediated authorization without exposing tokens.

### OpenWebUI + Ollama

Install the BubbleFish Nexus filter pipeline from the [`examples/integrations/openwebui/`](examples/integrations/openwebui/) directory. Filters run before and after every conversation, automatically writing to Nexus and injecting relevant memories into the system prompt — no model selection required.

### OpenClaw

OpenClaw is a self-hosted AI gateway that ships with a **bubblefish-nexus** TypeScript plugin in [`examples/integrations/openclaw/`](examples/integrations/openclaw/). It's the reference integration: any third-party tool that wants to add Nexus support can use it as a starting point. OpenClaw users get memory and credential routing in one install.

Full setup for every client: [`docs/integrations/`](docs/integrations/)

---

## Credential routing

Point your clients at Nexus as their provider base URL. Nexus swaps in the real key and forwards the request. Clients never know the difference.

```toml
# daemon.toml
[[credentials.mappings]]
synthetic_key  = "bfn_route_your-client-key"
provider       = "openai"
real_key       = "sk-real-key-here"
allowed_models = ["gpt-4o", "gpt-4o-mini"]
rate_limit_rpm = 60
```

Rotate the real key: edit `daemon.toml`, run `./bubblefish reload`. Clients are unaffected. Audit which key was used, when, and by whom in the structured event log.

---

## Benchmarks

Three numbers from a 30-second run on a real install. Reproduce with [`bench/nexus-mini.ps1`](bench/nexus-mini.ps1).

| Operation        | Throughput      | p50    | p95     | p99      |
|------------------|-----------------|--------|---------|----------|
| Write            | 60 ops/sec      | 3.1 ms | 96 ms   | 104 ms   |
| Query            | 218 ops/sec     | 3.2 ms | 8.7 ms  | 9.9 ms   |
| Memory footprint | 41 MB resident  | —      | —       | —        |

Measured on Windows 11, Nexus v0.1.2, SQLite backend, default install. Write tail latency is fsync-bound on Windows; Linux numbers are typically 2-3x faster on the same hardware. Zero failures across 2,000 sequential operations. Memory grew 4.4 MB after 1,000 writes and 1,000 queries.

Full methodology in [`docs/BENCHMARKS.md`](docs/BENCHMARKS.md).

---

## Roadmap

Nexus ships today as a credential gateway, memory substrate, and retrieval cascade. The next releases turn it into a full AI infrastructure layer.

**v0.2 — Audit and Policy.** AI Interaction Log as a queryable plane (extending the WAL and security event log). Retrieval Firewall with sensitivity labels and per-source blocked-label lists. Backup and restore CLI with point-in-time recovery.

**v0.3 — Observability.** Conflict inspector for contradictory memories across sources and timestamps. Time-travel queries. Pipeline visualizer with live per-stage latency.

**v1.0 — Enterprise.** AI Firewall: prompt and response inspection at the gateway. RBAC and multi-tenant scoping. Edge clustering and multi-site replication. Commercial license for customers who cannot accept AGPL obligations.

We ship when it works. Dates are not published.

---

## Known limitations

| Area | Limitation | Mitigation |
|---|---|---|
| Embeddings | No built-in local model. Semantic stage uses what the source provides. | Ollama-hosted embeddings work today. Pluggable local model on the v0.3 list. |
| SQLite backend | Single writer. High-concurrency teams should use Postgres. | Postgres source profile is tested and shipping. |
| Temporal decay | Heuristic exponential half-life, not learned. | Per-source half-life is configurable. Learned decay is v0.3 research. |
| Multi-user | X-Subject header is the only subject boundary within a source. | True RBAC is v1.0 enterprise. |
| Audit log | Append-only on disk. No remote SIEM shipping yet. | Webhook event sink lands in v0.2. |
| Windows service | CLI-managed only. No service installer yet. | Use NSSM or `sc.exe` if you need it as a service today. |

---

## Documentation

- [Architecture](docs/ARCHITECTURE.md) — how Nexus works internally
- [Deployment](docs/DEPLOYMENT.md) — install, service setup, Cloudflare Tunnel, monitoring
- [Configuration](docs/CONFIGURATION.md) — annotated `daemon.toml` reference
- [Integrations](docs/integrations/) — setup for every supported client
- [API Reference](docs/API.md) — HTTP and MCP endpoints
- [Threat Model](docs/THREAT_MODEL.md) — trust boundaries and attacker capabilities
- [Benchmarks](docs/BENCHMARKS.md) — methodology and reproducible results
- [OpenClaw Plugin](docs/OPENCLAW_SKILL.md) — bubblefish-memory plugin guide

---

## License

AGPL-3.0. See [LICENSE](LICENSE).

A commercial license is available for SaaS deployment, OEM embedding, or any use that cannot accept AGPL obligations: `licensing@bubblefish.ai`.

Contributions require a signed Contributor License Agreement. See [CONTRIBUTING.md](CONTRIBUTING.md) and [CLA.md](CLA.md). The CLA lets BubbleFish Technologies, Inc. distribute your contribution under both AGPL and the commercial license. You retain copyright.

---

## Patent pending

The credential gateway architecture, the WAL-based persistence and crash-recovery model, the six-stage retrieval cascade with temporal decay, and the integrated memory-and-credential gateway as a single self-contained daemon are covered by a provisional patent application filed with the United States Patent and Trademark Office by BubbleFish Technologies, Inc.

---

## Security

Three token classes (admin, data, MCP) with strict separation. All four HTTP server timeouts set. Rate limiting on reads and writes. WAL entries CRC32-checksummed with optional HMAC integrity and AES-256-GCM encryption. OAuth 2.1 with RSA-2048 / JWT RS256 / PKCE S256.

Report security issues privately per [SECURITY.md](SECURITY.md). Do not open public issues for vulnerabilities.

---

## Community

- Issues: [github.com/bubblefish-tech/nexus/issues](https://github.com/bubblefish-tech/nexus/issues)
- Discussions: [github.com/bubblefish-tech/nexus/discussions](https://github.com/bubblefish-tech/nexus/discussions)
- Security: see [SECURITY.md](SECURITY.md)
- Commercial and press: `hello@bubblefish.ai`

---

<p align="center">
Built by <a href="https://bubblefish.ai">BubbleFish Technologies, Inc.</a>, a frontier AI infrastructure company.<br/>
Secure, durable, and governed fabric for AI systems and agents.
</p>
