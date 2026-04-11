# MCP Client Setup

How to connect each AI client to Nexus. The MCP server runs on port 7474 by default. All connections require the `bfn_mcp_` bearer token from your `daemon.toml`.

---

## Claude Desktop

Nexus connects to Claude Desktop via the MCP stdio bridge.

Edit `claude_desktop_config.json`:

**Windows:** `%APPDATA%\Claude\claude_desktop_config.json`
**macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`

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

> **macOS PATH note:** Claude Desktop uses the system PATH, not your shell PATH. Use the full path: `"command": "/usr/local/bin/bubblefish"`.

Restart Claude Desktop. You should see `nexus_write`, `nexus_search`, and `nexus_status` available as tools.

**Verify before configuring Claude Desktop:**
```bash
bubblefish mcp test    # Self-test. Exits 0 on success.
```

---

## Claude Web (claude.ai)

Claude Web can connect to Nexus via MCP if you expose Nexus publicly (e.g., via Cloudflare Tunnel).

In Claude Web Settings → Integrations → Add MCP Server:
- **URL:** `https://your-tunnel-url.yourdomain.com/mcp`
- **Authentication:** Bearer token → `bfn_mcp_YOUR_KEY_HERE`

The same `nexus_write`, `nexus_search`, and `nexus_status` tools will be available.

---

## ChatGPT

ChatGPT connects to Nexus via OAuth 2.1. Nexus includes a built-in authorization server with PKCE, RSA-2048, and JWKS.

ChatGPT requires an HTTPS endpoint. Expose Nexus via Cloudflare Tunnel:

```bash
cloudflared tunnel --url http://localhost:7474
```

In ChatGPT Settings → Connected apps → Add connection:
- **Endpoint:** `https://your-tunnel-url.yourdomain.com/mcp`

ChatGPT will discover the OAuth metadata automatically via RFC 8414 server metadata, prompt for authorization, and exchange tokens. No manual key configuration needed.

---

## Perplexity Comet

Perplexity Comet uses MCP over SSE (Server-Sent Events). Requires a Cloudflare Tunnel.

In Perplexity Settings → Custom Connectors → Add:
- **Name:** BubbleFish Nexus
- **URL:** `https://your-tunnel-url.yourdomain.com/mcp`
- **Transport:** SSE
- **Authentication:** API Key → paste your `bfn_mcp_` token

To activate per conversation: click `+` in the chatbox → Connectors → BubbleFish Nexus.

**Cloudflare Tunnel security for Perplexity:** Perplexity's backend is automated and cannot handle browser-based auth challenges. Configure a WAF bypass rule in Cloudflare to allow requests matching your `bfn_mcp_` key pattern before the Access policy fires. Nexus validates the key server-side regardless.

---

## Open WebUI + Ollama

Open WebUI connects to Nexus via the Pipelines server, not directly.

### Quick setup

```bash
bubblefish install --dest sqlite --profile openwebui
bubblefish start
```

This creates a source config tuned for Open WebUI's payload shape.

### Docker networking

Open WebUI and Pipelines run in Docker. Use `host.docker.internal` (not `localhost`) to reach Nexus on the host machine:

```
Nexus URL: http://host.docker.internal:8080
```

### Pipelines configuration

In Open WebUI Admin Settings → Pipelines:
- Add a pipeline that connects to `http://host.docker.internal:8080/inbound/openwebui`
- Use the `bfn_data_` key from your source config

Pipelines is configured under Admin Settings → Pipelines, not under Connections.

### Free local semantic search

Use Ollama as your embedding provider for zero-cost vector search:

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

**Note on internal prompts:** Open WebUI sends internal system prompts that should be filtered to prevent database bloat. The OpenWebUI source profile handles this automatically.

---

## OpenClaw

OpenClaw is a separate tool from Open WebUI. It's a Node.js-based AI gateway that runs in WSL 2 (or native Linux/macOS).

Nexus includes an OpenClaw integration example implemented as a TypeScript ESM plugin.

### Install

The OpenClaw integration is currently distributed from this repository and is not listed on ClawHub.

Copy the plugin files from `examples/integrations/openclaw/` in the Nexus repo into your OpenClaw plugins directory.

### Configure

Set environment variables for the plugin:

```bash
export NEXUS_URL="http://localhost:8080"
export NEXUS_DATA_KEY="bfn_data_YOUR_KEY"
export NEXUS_SOURCE="openclaw"
```

If OpenClaw runs in WSL 2 and Nexus runs on Windows, use the WSL → Windows port proxy:

```bash
# From WSL, reach Nexus on Windows host
export NEXUS_URL="http://$(cat /etc/resolv.conf | grep nameserver | awk '{print $2}'):8080"
```

### Verify

The plugin registers three tools: `nexus_write`, `nexus_search`, `nexus_status`.

### Auto-save hook

The plugin can optionally hook into OpenClaw's `before_agent_reply` pipeline to automatically surface relevant memories at the start of each session. Configure via plugin settings.

Full plugin guide: [OPENCLAW_SKILL.md](OPENCLAW_SKILL.md)

---

## LM Studio

LM Studio supports MCP via its developer settings:

- **MCP Server:** `http://localhost:7474`
- **Auth header:** `Authorization: Bearer bfn_mcp_YOUR_KEY_HERE`

LM Studio is typically on the same machine, so local HTTP works fine.

---

## Cursor

Cursor supports MCP natively. Add to your Cursor MCP config:

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

Same stdio bridge as Claude Desktop.

---

## Verify the connection

After connecting any client, run a quick test:

```
[via MCP tool call]
Tool: nexus_status
Result: { "status": "ok", "version": "0.1.2", "queue_depth": 0 }
```

Or via HTTP:
```bash
curl http://localhost:8080/health
```

---

## Sharing memory across clients

Once two or more clients are connected to the same Nexus instance, they share memory automatically. A write from Claude Desktop is readable by ChatGPT. A write from Perplexity is readable by OpenWebUI.

No additional configuration is needed. The shared memory model is the default.
