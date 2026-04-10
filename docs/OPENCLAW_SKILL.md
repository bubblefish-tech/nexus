# bubblefish-nexus: OpenClaw Plugin

**Crash-proof persistent memory for OpenClaw. Survives compaction. Works across Claude Desktop, ChatGPT, Perplexity, and more.**

Install via npm, then connect it to a running Nexus daemon.

---

## Why this exists

OpenClaw's native memory runs inside the OpenClaw process. When compaction fires, when the session file grows too large, when the process crashes — in-process memory is at risk.

BubbleFish Nexus runs as a separate daemon on a different port. OpenClaw crashing does not crash Nexus. Compaction firing does not affect the WAL. When OpenClaw restarts, all memory is intact and retrievable.

The other thing Nexus gives you: the same memory store works across every AI client you run. Claude Desktop, ChatGPT, Perplexity, OpenWebUI — they all write to and read from the same Nexus instance. One brain, not a separate memory silo per app.

---

## Requirements

- OpenClaw (any recent version with plugin support)
- BubbleFish Nexus daemon running locally or remotely
- The `bfn_data_` key from your source config

---

## Install

### From npm

```bash
npm install bubblefish-nexus
```

### Manual install

Copy the plugin files from the Nexus repo to your OpenClaw plugins directory:

```bash
cp -r examples/integrations/openclaw/* ~/.openclaw/plugins/bubblefish-nexus/
```

Restart OpenClaw or reload plugins.

---

## Configure

Set environment variables for the plugin:

```bash
export NEXUS_URL="http://localhost:8080"
export NEXUS_DATA_KEY="bfn_data_YOUR_KEY"
export NEXUS_SOURCE="openclaw"
export NEXUS_COLLECTION="default"
```

### WSL 2 → Windows networking

If OpenClaw runs in WSL 2 and Nexus runs on the Windows host, `localhost` won't work. Use the WSL DNS resolver to find the Windows host IP:

```bash
export NEXUS_URL="http://$(cat /etc/resolv.conf | grep nameserver | awk '{print $2}'):8080"
```

Or set up a Windows port proxy:

```powershell
# Run in elevated PowerShell on Windows
netsh interface portproxy add v4tov4 listenport=8080 listenaddress=0.0.0.0 connectport=8080 connectaddress=127.0.0.1
```

Then from WSL:
```bash
export NEXUS_URL="http://localhost:8080"
```

### Verify the connection

```bash
curl -s http://localhost:8080/health
# {"status":"ok","version":"0.1.2"}
```

---

## Registered tools

The plugin registers three tools via OpenClaw's `registerTool` API:

| Tool | Description |
|------|-------------|
| `nexus_write` | Write a memory to Nexus via `POST /inbound/{source}` |
| `nexus_search` | Search memories via `GET /query/sqlite` using the 6-stage cascade |
| `nexus_status` | Check daemon health via `GET /health` |

---

## Auto-inject hook

The plugin can hook into OpenClaw's `before_agent_reply` pipeline to automatically surface relevant memories at the start of each session. This is configurable:

```bash
export NEXUS_AUTO_INJECT="true"   # or "false" to disable
```

When enabled, the plugin passes your current conversation context as the search query and returns the most relevant memories within your context window budget.

---

## Memory format

When the plugin writes a memory:
- Text content from the conversation
- Current timestamp
- OpenClaw session ID as the `actor_id`
- `actor_type: agent`

When retrieving, the plugin uses the Nexus 6-stage cascade to return the most relevant memories.

---

## Sharing with other apps

If another AI client (Claude Desktop, ChatGPT, etc.) is connected to the same Nexus instance, they see the same memories. There's no extra configuration. Writes from OpenClaw are readable by Claude Desktop and vice versa.

This is the whole point.

---

## Troubleshooting

**Connection refused**
- Verify Nexus daemon is running: `bubblefish status` on the machine running Nexus
- If in WSL 2: check the host IP resolution (see WSL 2 networking above)
- Verify the port matches your `daemon.toml`

**Slow response on search**
- If semantic search is unavailable (no embedding provider configured), results come from structured lookup (Stage 3). This is fast but not vector-based.
- Check the `before_agent_reply` hook: if another plugin is doing heavy work in that hook, it adds latency.

**Memories not persisting across sessions**
- Check that Nexus is reachable from within OpenClaw
- Check queue depth via `nexus_status` — if nonzero and growing, the destination may be slow

---

## Source

The plugin source is in the main Nexus repo at [`examples/integrations/openclaw/`](https://github.com/bubblefish-tech/nexus/tree/main/examples/integrations/openclaw).

Issues and PRs welcome. See [CONTRIBUTING.md](../../CONTRIBUTING.md).

---

## License

AGPL-3.0. See [LICENSE](../../LICENSE).
