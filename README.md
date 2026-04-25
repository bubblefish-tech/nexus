# BubbleFish Nexus

BubbleFish Nexus is a gateway-first AI memory daemon. It sits between AI clients and memory databases, providing crash-safe, policy-aware, retrieval-optimized memory management.

One Go binary. No Docker required. Runs on Windows, Linux, and macOS.

> v0.1.3 (pre-1.0, API subject to change)

---

## Install and Run (30 Seconds)

```bash
# Linux / macOS
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/nexus_$(uname -s)_$(uname -m).tar.gz | tar xz
./nexus install --mode simple
./nexus start
```

```powershell
# Windows
iwr -useb https://get.bubblefish.sh/install.ps1 | iex
nexus install --mode simple
nexus start
```

Nexus is now listening: HTTP API on **:8080**, MCP server on **:7474**, web dashboard on **:8081**.

---

## Demo: Write, Search, Recover

```bash
# Write a memory
curl -s -X POST http://localhost:8080/write \
  -H "Authorization: Bearer $NEXUS_DATA_KEY" \
  -H "Content-Type: application/json" \
  -d '{"content":"Nexus remembers this.","subject":"demo"}'

# Search for it
curl -s http://localhost:8080/search?q=remembers \
  -H "Authorization: Bearer $NEXUS_DATA_KEY"

# Results include which retrieval stage fired
# { "results": [...], "_nexus": { "stage": "exact_cache", "latency_ms": 2 } }
```

**The kill -9 test.** Write memories. Force-kill the process. Restart. Query. Everything comes back. WAL replay runs in milliseconds. Nothing is lost.

```bash
nexus demo --api-key $NEXUS_DATA_KEY --admin-key $NEXUS_ADMIN_KEY
```

---

## Architecture

```
AI Clients ──► Auth + Rate Limit ──► Policy Engine
                                          │
                              ┌───────────┴───────────┐
                              ▼                       ▼
                         Write Path              Query Path
                              │               6-Stage Retrieval
                         WAL (fsync)          ┌─────────────┐
                              │               │ Exact Cache  │
                         Queue                │ Semantic     │
                              │               │ Keyword/BM25 │
                         Destinations         │ Recency      │
                         ┌────┴────┐          │ Subject      │
                         │ SQLite  │          │ Full Scan    │
                         │ Postgres│          └──────┬───────┘
                         │ Supabase│                 │
                         └─────────┘           Temporal Decay
                                              + Projection
```

Write path: WAL first, queue second, DB third. Always. Every write hits the WAL with a CRC32 checksum (optional HMAC-SHA256 + AES-256-GCM) and fsyncs before the client gets a 200 OK.

Query path: six-stage retrieval cascade with temporal decay. Stops at the first stage that returns results. Every response includes which stage fired.

Full architecture: [Nexus/Docs/](Nexus/Docs/)

---

## MCP Integration

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

### Perplexity / ChatGPT / Other Remote Clients

Expose Nexus via Cloudflare Tunnel, then use the tunnel URL as your MCP endpoint:

```bash
cloudflared tunnel --url http://localhost:7474
```

ChatGPT additionally supports OAuth 2.1 (RSA-2048, JWT RS256, PKCE S256) for browser-mediated authorization.

### Open WebUI + Ollama

Install the BubbleFish Nexus filter pipeline from [`Nexus/examples/integrations/openwebui/`](Nexus/examples/integrations/openwebui/). Filters run before and after every conversation, automatically writing to Nexus and injecting relevant memories into the system prompt.

### OpenClaw

OpenClaw is a self-hosted AI gateway with a **bubblefish-nexus** TypeScript plugin in [`Nexus/examples/integrations/openclaw/`](Nexus/examples/integrations/openclaw/). Reference integration for any third-party tool that wants Nexus support.

---

## Governed Control Plane

Nexus includes a governed control plane for agent coordination:

- **Grants and approvals** -- capability-scope grants with 17 reserved capability prefixes, configurable default policies (auto-allow, approve-once, always-approve), deny-always-wins
- **Agent-to-agent protocol (A2A)** -- register agents, grant scoped capabilities, invoke through any MCP client. Wire-compatible with the A2A v1.0 spec
- **Four transports** -- local subprocess (stdio), direct HTTP, Cloudflare tunnel, Windows-host-to-WSL2 loopback
- **Task governance** -- destructive skill escalation, expired/revoked grant enforcement, chain-depth limiting (max 4)
- **Hash-chained audit log** -- every grant, approval, denial, and agent action is recorded in a tamper-evident append-only chain

---

## Credential Gateway

Real provider keys never leave Nexus. Clients use synthetic `bfn_route_` keys that only work through your Nexus instance.

```
Your clients --> bfn_route_synthetic_key --> Nexus --> sk-real-key --> Provider
```

Rotate a real key in one config file. Every client keeps working. Audit which key was used, when, and by whom.

---

## What Ships in v0.1.3

| Capability | Detail |
|---|---|
| Governed control plane | Grants, approvals, tasks, action log, agent registry |
| Agent-to-agent (A2A) | 9 MCP tools, 4 transports, governance engine |
| BM25 hybrid search | Keyword + semantic + temporal decay fusion |
| Built-in embedding | Local embedding pipeline, no external API required |
| Terminal UI | Charm-based TUI with dashboard, agents, memory, audit, crypto, governance views |
| Encryption at rest | AES-256-GCM on WAL entries, per-memory encryption keys via HKDF |
| WAL crash safety | Group commit, dual integrity sentinels, fsync verification, zstd compression |
| Proactive ingestion | Watches Claude Code, Cursor, generic JSONL directories |
| Cryptographic provenance | Ed25519 signing, hash-chained audit, Merkle roots, query attestation |
| OAuth 2.1 | RSA-2048, JWT RS256, PKCE S256 for ChatGPT and browser-mediated MCP |
| Web dashboard | Real-time memory feed, agents, proofs, security events |
| Worm auto-discovery | Automatic detection of connected AI clients |

---

## Benchmarks

[benchmark pending] -- reproducible benchmarks will be published with the v0.1.3 release artifacts. Run `nexus bench` to generate your own numbers.

---

## Documentation

- [Architecture](Nexus/Docs/) -- how Nexus works internally
- [Configuration](Nexus/Docs/CONFIGURATION.md) -- annotated `daemon.toml` reference
- [API Reference](Nexus/Docs/API.md) -- HTTP and MCP endpoints
- [A2A Overview](Nexus/Docs/a2a/overview.md) -- agent-to-agent protocol
- [Known Limitations](Nexus/KNOWN_LIMITATIONS.md) -- current scope and constraints
- [Threat Model](Nexus/THREAT_MODEL.md) -- trust boundaries and attacker capabilities

---

## Roadmap

| Version | Focus |
|---------|-------|
| v0.1.3 | Governed control plane, A2A, hybrid search, TUI, encryption at rest |
| v0.2 | Audit and policy plane, retrieval firewall, backup/restore |
| v0.3 | Conflict inspector, time-travel queries, pipeline visualizer |
| v1.0 | AI firewall, RBAC, multi-tenant, edge clustering, commercial license |

---

## License

AGPL-3.0. See [LICENSE](Nexus/LICENSE).

A commercial license is available for SaaS deployment, OEM embedding, or any use that cannot accept AGPL obligations: `licensing@bubblefish.sh`

Contributions require a signed Contributor License Agreement. See [CONTRIBUTING.md](CONTRIBUTING.md) and [CLA.md](CLA.md).

---

## Security

Three token classes (admin, data, MCP) with strict separation. All token comparisons use `subtle.ConstantTimeCompare`. WAL entries CRC32-checksummed with optional HMAC integrity and AES-256-GCM encryption. OAuth 2.1 with RSA-2048 / JWT RS256 / PKCE S256.

Report security issues privately per [SECURITY.md](SECURITY.md). Do not open public issues for vulnerabilities.

---

## Patent Pending

The credential gateway architecture, the WAL-based persistence and crash-recovery model, the six-stage retrieval cascade with temporal decay, and the integrated memory-and-credential gateway as a single self-contained daemon are covered by a provisional patent application filed with the United States Patent and Trademark Office.

---

## Community

- Issues: [github.com/bubblefish-tech/nexus/issues](https://github.com/bubblefish-tech/nexus/issues)
- Discussions: [github.com/bubblefish-tech/nexus/discussions](https://github.com/bubblefish-tech/nexus/discussions)
- Security: see [SECURITY.md](SECURITY.md)
- Commercial and press: `hello@bubblefish.sh`

---

Copyright 2026 Shawn Sammartano. All rights reserved.

Built by [BubbleFish Technologies, Inc.](https://bubblefish.sh), a frontier AI infrastructure company.
