# Nexus

Nexus is a small local memory store for AI coding assistants. It saves what you've talked about with tools like Claude Code, Cursor, and Continue, and lets them search across past conversations.

One Go binary. SQLite by default. Runs on Windows, Linux, and macOS.

> v0.1.3 — early hobby release. APIs will change.

---

## Install

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

After install, Nexus listens on `localhost:8080` for the HTTP API and `localhost:7474` for the MCP endpoint. A small status page is at `localhost:8081`.

---

## Try it

```bash
# Save something
curl -s -X POST http://localhost:8080/write \
  -H "Authorization: Bearer $NEXUS_DATA_KEY" \
  -H "Content-Type: application/json" \
  -d '{"content":"Nexus remembers this.","subject":"demo"}'

# Search for it
curl -s http://localhost:8080/search?q=remembers \
  -H "Authorization: Bearer $NEXUS_DATA_KEY"
```

The kill -9 test: write some memories, force-kill the process, restart, and search. Everything comes back. Nexus uses a write-ahead log so nothing is lost if it crashes.

---

## How it works
Coding assistant
│
▼
Nexus (local Go binary)
│
┌───┴───┐
▼       ▼
write    search
│       │
WAL    6-stage lookup:
│       exact cache → semantic →
▼       
SQLite     subject → full scan

Writes go to a write-ahead log first, then to SQLite. Searches run a six-stage cascade and stop at the first stage that returns results. Every response tells you which stage answered the query.

Postgres and Supabase are also supported as alternative storage backends.

---

## Connect your tools

### Claude Desktop / Claude Code / Cursor

These speak MCP. Add Nexus as an MCP server:

```json
{
  "mcpServers": {
    "nexus": {
      "url": "http://localhost:7474",
      "headers": {
        "Authorization": "Bearer YOUR_BFN_MCP_KEY"
      }
    }
  }
}
```

### Open WebUI + Ollama

There's an Open WebUI filter pipeline in `examples/integrations/openwebui/` that auto-saves conversations to Nexus and injects relevant past notes back into the prompt.

### Other tools

Anything that can hit a local HTTP endpoint can use Nexus. There's a TypeScript example in `examples/integrations/openclaw/` showing how a third-party tool can wire it up.


---

## Benchmarks

Run `nexus bench` on your own machine. I haven't published numbers yet — the project is too early and benchmark methodology for memory retrieval is its own rabbit hole.

---

## Documentation

- [Configuration](Nexus/Docs/CONFIGURATION.md) — `daemon.toml` reference
- [API Reference](Nexus/Docs/API.md) — HTTP and MCP endpoints
- [Known Limitations](Nexus/KNOWN_LIMITATIONS.md) — what doesn't work yet

---

## Roadmap

Rough plans, no timeline:

- Better backup/restore
- A retrieval firewall so memories can be filtered before they reach an assistant
- A small inspector for finding conflicting memories
- Time-travel queries (search the state at a past timestamp)

---

## License

AGPL-3.0. See [LICENSE](Nexus/LICENSE).

Contributions require a signed CLA — see [CONTRIBUTING.md](CONTRIBUTING.md) and [CLA.md](CLA.md).

---

## Security

If you find a security issue, please email me privately rather than opening a public issue. See [SECURITY.md](SECURITY.md).

---

## Community

- Issues: [github.com/bubblefish-tech/nexus/issues](https://github.com/bubblefish-tech/nexus/issues)
- Discussions: [github.com/bubblefish-tech/nexus/discussions](https://github.com/bubblefish-tech/nexus/discussions)

---

Copyright 2026 Shawn Sammartano. All rights reserved.
