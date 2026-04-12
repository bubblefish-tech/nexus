# BubbleFish Nexus — User Guide
**Version:** 0.1.2 | **Date:** 2026-04-11
**Repo:** github.com/bubblefish-tech/nexus

This guide covers getting Nexus running, connecting your AI apps, troubleshooting common problems, and adding new AI clients beyond the ones with built-in support.

---

## Table of Contents

1. [Install and Start](#1-install-and-start)
2. [Connecting Claude Desktop](#2-connecting-claude-desktop)
3. [Connecting Claude Web (claude.ai)](#3-connecting-claude-web-claudeai)
4. [Connecting ChatGPT](#4-connecting-chatgpt)
5. [Connecting Perplexity Comet](#5-connecting-perplexity-comet)
6. [Connecting Open WebUI + Ollama](#6-connecting-open-webui--ollama)
7. [Connecting OpenClaw](#7-connecting-openclaw)
8. [Connecting LM Studio](#8-connecting-lm-studio)
9. [Connecting Cursor](#9-connecting-cursor)
10. [Connecting Windsurf](#10-connecting-windsurf)
11. [Setting Up Cloudflare Tunnel (Required for Cloud AI Services)](#11-setting-up-cloudflare-tunnel-required-for-cloud-ai-services)
12. [Connecting Additional AI Apps](#12-connecting-additional-ai-apps)
13. [Troubleshooting Guide](#13-troubleshooting-guide)
14. [Configuration Quick Reference](#14-configuration-quick-reference)

---

## 1. Install and Start

### Windows

```powershell
# Download — use curl.exe with -f so a broken release fails loudly instead of saving an error body
curl.exe -fL --ssl-no-revoke -o bubblefish.exe `
  "https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-windows-amd64.exe"

# Verify the download worked (should be ~25 MB)
Get-Item .\bubblefish.exe | Select-Object Length

# Initialize — creates config files and generates your API keys
.\bubblefish.exe install --mode simple

# Start the daemon
.\bubblefish.exe start

# Verify it's running
.\bubblefish.exe status
```

### macOS (Apple Silicon)

```bash
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-darwin-arm64 -o bubblefish
chmod +x bubblefish && sudo mv bubblefish /usr/local/bin/
bubblefish install --mode simple
bubblefish start
```

**If macOS blocks the binary ("unidentified developer"):**
```bash
xattr -d com.apple.quarantine /usr/local/bin/bubblefish
```

### macOS (Intel)

```bash
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-darwin-amd64 -o bubblefish
chmod +x bubblefish && sudo mv bubblefish /usr/local/bin/
bubblefish install --mode simple
bubblefish start
```

### Linux

```bash
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-linux-amd64 -o bubblefish
chmod +x bubblefish && sudo mv bubblefish /usr/local/bin/
bubblefish install --mode simple
bubblefish start
```

### What install creates

The `install --mode simple` command initializes your Nexus home directory at `~/.bubblefish/Nexus/` with:
- A main `daemon.toml` config
- A default source config under `sources/` with an auto-generated `bfn_data_` API key
- A SQLite destination config under `destinations/`
- An MCP bearer token (`bfn_mcp_`) in the daemon config

It prints both keys when it's done. **Save both.** You'll need them when connecting clients.

```
MCP Key:  bfn_mcp_...   ← use this for Claude Desktop, Cursor, LM Studio, remote clients
Data Key: bfn_data_...  ← use this for HTTP-based clients and custom integrations
```

**Can't find your keys later?** Run:
```bash
# macOS/Linux
cat ~/.bubblefish/Nexus/daemon.toml | grep api_key
cat ~/.bubblefish/Nexus/sources/default.toml | grep api_key

# Windows
Get-Content "$env:USERPROFILE\.bubblefish\Nexus\daemon.toml" | Select-String "api_key"
Get-Content "$env:USERPROFILE\.bubblefish\Nexus\sources\default.toml" | Select-String "api_key"
```

---

## 2. Connecting Claude Desktop

Claude Desktop connects via MCP stdio transport. It spawns Nexus as a helper process.

### Config file location

**Windows:** `C:\Users\<username>\AppData\Roaming\Claude\claude_desktop_config.json`
```powershell
# Open it in Notepad
notepad "$env:APPDATA\Claude\claude_desktop_config.json"
```

**macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`

### Config content

**Windows:**
```json
{
  "mcpServers": {
    "bubblefish-nexus": {
      "command": "C:\\path\\to\\bubblefish.exe",
      "args": ["mcp"]
    }
  }
}
```

Replace `C:\\path\\to\\bubblefish.exe` with the actual path. Find it with:
```powershell
(Get-Command bubblefish.exe -ErrorAction SilentlyContinue)?.Path
# OR
where.exe bubblefish
```

Use double backslashes (`\\`) in the JSON.

**macOS:**
```json
{
  "mcpServers": {
    "bubblefish-nexus": {
      "command": "/usr/local/bin/bubblefish",
      "args": ["mcp"]
    }
  }
}
```

> **macOS important:** Claude Desktop doesn't use your shell PATH. You must use the full path like `/usr/local/bin/bubblefish`, not just `bubblefish`. Run `which bubblefish` to get the full path.

### After editing config

Restart Claude Desktop completely (quit and reopen, not just close a chat). In a new conversation, ask: "Use nexus_status to check the memory daemon." If it returns version info, you're connected.

---

## 3. Connecting Claude Web (claude.ai)

Claude Web connects via HTTP MCP. This requires an HTTPS public URL — set up a Cloudflare Tunnel first (see [Section 11](#11-setting-up-cloudflare-tunnel-required-for-cloud-ai-services)).

Once you have your tunnel URL:
1. In Claude Web: Settings → Integrations → Add MCP Server
2. **URL:** `https://your-tunnel-url/mcp`
3. **Authentication:** Bearer token → paste your `bfn_mcp_` key

---

## 4. Connecting ChatGPT

ChatGPT (Web and Desktop) connects via OAuth 2.1. Nexus handles the authorization flow automatically.

**Requires:** Cloudflare Tunnel (see [Section 11](#11-setting-up-cloudflare-tunnel-required-for-cloud-ai-services))

Once you have a stable HTTPS tunnel URL:
1. In ChatGPT: Settings → Connected apps → Add connection
2. **Endpoint URL:** `https://your-tunnel-url/mcp`
3. ChatGPT will discover the authorization server automatically
4. A consent screen appears — click Approve
5. Done. ChatGPT can now write and read memories through Nexus

---

## 5. Connecting Perplexity Comet

Perplexity Comet uses MCP over SSE. Requires Cloudflare Tunnel.

1. In Perplexity: Profile → Settings → Custom Connectors → Add
2. **Name:** BubbleFish Nexus (or anything you like)
3. **URL:** `https://your-tunnel-url/mcp`
4. **Transport:** SSE
5. **Authentication:** API Key → paste your `bfn_mcp_` key

To activate per conversation: click `+` in the Perplexity chat input → Connectors → BubbleFish Nexus. The connector must be activated per conversation.

> **Cloudflare WAF note for Perplexity:** Perplexity's backend is automated and cannot handle browser-based authentication challenges. If you protect your tunnel with Cloudflare Access, configure a WAF bypass rule that allows requests carrying your `bfn_mcp_` key pattern *before* the Access policy fires. Nexus validates the bearer token server-side, so the bypass is safe.

---

## 6. Connecting Open WebUI + Ollama

Open WebUI connects via the Pipelines container. Nexus sits on your host machine; Pipelines runs in Docker.

### Prerequisites

- Docker installed and running
- Ollama installed (for local AI models and optional embeddings)

### Step 1 — Start Docker containers

**Windows/macOS:**
```bash
docker run -d -p 3000:8080 --name open-webui \
  ghcr.io/open-webui/open-webui:main

docker run -d -p 9099:9099 --name pipelines \
  ghcr.io/open-webui/pipelines:main
```

**Linux** (requires `--add-host` flag):
```bash
docker run -d -p 3000:8080 --add-host=host.docker.internal:host-gateway \
  --name open-webui ghcr.io/open-webui/open-webui:main

docker run -d -p 9099:9099 --add-host=host.docker.internal:host-gateway \
  --name pipelines ghcr.io/open-webui/pipelines:main
```

### Step 2 — Install Nexus Open WebUI profile

```bash
bubblefish install --dest sqlite --profile openwebui
```

This creates `sources/openwebui.toml` tuned for Open WebUI's payload format.

### Step 3 — Configure Pipelines in Open WebUI

1. Open `http://localhost:3000`
2. Admin Panel → Settings → Pipelines
3. Set Pipelines URL: `http://host.docker.internal:9099`
4. Upload the BubbleFish pipeline file from `examples/integrations/openwebui/`
5. In the pipeline valves:
   - `nexus_url`: `http://host.docker.internal:8080`
   - `bfn_data_key`: your `bfn_data_` key (find it with `grep api_key ~/.bubblefish/Nexus/sources/openwebui.toml`)

### Step 4 — Optional: Local semantic search with Ollama

```bash
ollama pull nomic-embed-text
```

Then in `~/.bubblefish/Nexus/daemon.toml`, add:
```toml
[daemon.embedding]
enabled   = true
provider  = "ollama"
url       = "http://localhost:11434"
model     = "nomic-embed-text"
dimensions = 768
```

Restart Nexus.

---

## 7. Connecting OpenClaw

OpenClaw connects via the **bubblefish-nexus** TypeScript ESM plugin.

### Install the plugin

The OpenClaw integration is currently distributed from the Nexus repository and is **not** listed on ClawHub or npm. Copy the plugin files directly from the Nexus repo into your OpenClaw plugins directory:

```bash
cp -r examples/integrations/openclaw/* ~/.openclaw/plugins/bubblefish-nexus/
```

Restart OpenClaw or reload plugins.

### Configure

```bash
export NEXUS_URL="http://localhost:8080"
export NEXUS_DATA_KEY="bfn_data_YOUR_KEY"
export NEXUS_SOURCE="openclaw"
export NEXUS_COLLECTION="default"
```

**If OpenClaw runs in WSL 2 on Windows:** WSL 2 uses a separate network. Get the Windows host IP from inside WSL:
```bash
export NEXUS_URL="http://$(cat /etc/resolv.conf | grep nameserver | awk '{print $2}'):8080"
```

The plugin registers three tools: `nexus_write`, `nexus_search`, `nexus_status`.

---

## 8. Connecting LM Studio

LM Studio supports MCP via a config file.

### Config file location

- **Windows:** `%APPDATA%\LM-Studio\mcp.json`
- **macOS:** `~/Library/Application Support/LM Studio/mcp.json`
- **Linux:** `~/.config/LM-Studio/mcp.json`

Or: LM Studio → Settings → Developer → MCP Configuration.

### Config content

```json
{
  "mcpServers": {
    "bubblefish-nexus": {
      "type": "http",
      "url": "http://localhost:7474/mcp",
      "name": "bubblefish-nexus",
      "headers": {
        "Authorization": "Bearer bfn_mcp_YOUR_KEY_HERE"
      }
    }
  }
}
```

Replace `bfn_mcp_YOUR_KEY_HERE` with your actual `bfn_mcp_` key.

> **Compatibility note:** If your LM Studio version doesn't recognize `"type": "http"`, change it to `"type": "sse"`.

---

## 9. Connecting Cursor

### Config file location

- **Windows:** `%USERPROFILE%\.cursor\mcp.json`
- **macOS/Linux:** `~/.cursor/mcp.json`

Or: Cursor Settings → MCP → Configure.

### Config content

**Windows:**
```json
{
  "mcpServers": {
    "bubblefish-nexus": {
      "command": "C:\\path\\to\\bubblefish.exe",
      "args": ["mcp"]
    }
  }
}
```

**macOS/Linux:**
```json
{
  "mcpServers": {
    "bubblefish-nexus": {
      "command": "/usr/local/bin/bubblefish",
      "args": ["mcp"]
    }
  }
}
```

Same rule as Claude Desktop: use the full binary path.

---

## 10. Connecting Windsurf

Windsurf → Settings → MCP Servers → Add Server:

```json
{
  "mcpServers": {
    "bubblefish-nexus": {
      "type": "http",
      "url": "http://localhost:7474/mcp",
      "headers": {
        "Authorization": "Bearer bfn_mcp_YOUR_KEY"
      }
    }
  }
}
```

---

## 11. Setting Up Cloudflare Tunnel (Required for Cloud AI Services)

Claude Web, ChatGPT, and Perplexity Comet require an HTTPS URL to reach Nexus. Cloudflare Tunnel provides this for free.

**The tunnel must point to port 7474** (the MCP server), not port 8080.

### Install cloudflared

**Windows:**
```powershell
Invoke-WebRequest -Uri "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe" -OutFile cloudflared.exe
```

**macOS:**
```bash
brew install cloudflared
```

**Linux:**
```bash
curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 -o cloudflared
chmod +x cloudflared && sudo mv cloudflared /usr/local/bin/
```

### Quick tunnel (simplest — URL changes on restart)

```bash
cloudflared tunnel --url http://localhost:7474
```

The command prints a URL like `https://some-random-words.trycloudflare.com`. Copy that URL — it's what you paste into ChatGPT, Perplexity, and Claude Web.

**Limitation:** The URL is different every time cloudflared restarts. For stable connections, use a named tunnel.

### Named tunnel (stable URL — requires free Cloudflare account)

```bash
cloudflared tunnel login
cloudflared tunnel create nexus
cloudflared tunnel route dns nexus your-subdomain.yourdomain.com
cloudflared tunnel --url http://localhost:7474 run nexus
```

Your tunnel URL is then `https://your-subdomain.yourdomain.com` and it stays the same.

---

## 12. Connecting Additional AI Apps

Nexus supports four connection methods. Any AI app that supports at least one of these can connect.

### Method A — MCP Stdio

**What it is:** The AI app spawns `bubblefish mcp` as a child process and communicates via stdin/stdout.

**Supported by:** Claude Desktop, Cursor, Windsurf (stdio mode), and any app with MCP stdio support.

**What you need in the app config:**
```json
{
  "command": "/full/path/to/bubblefish",
  "args": ["mcp"]
}
```

### Method B — MCP HTTP

**What it is:** The AI app sends HTTP POST requests to `http://localhost:7474/mcp` (for local apps) or a Cloudflare Tunnel URL (for cloud apps). Uses JSON-RPC 2.0.

**Supported by:** LM Studio, Windsurf, and any app with MCP HTTP support.

**What you need:**
```
URL:  http://localhost:7474/mcp
Auth: Authorization: Bearer bfn_mcp_YOUR_KEY
```

### Method C — MCP SSE

**What it is:** The AI app subscribes to `GET /mcp` with `Accept: text/event-stream`. Memories arrive as Server-Sent Events.

**Supported by:** Perplexity Comet, Claude Web, and browser-native AI apps.

**What you need:**
```
URL:  https://your-tunnel/mcp   (HTTPS required — use Cloudflare Tunnel)
Auth: API Key → bfn_mcp_YOUR_KEY
      OR query param: ?key=bfn_mcp_YOUR_KEY  (for apps that can't set headers)
```

### Method D — HTTP Inbound

**What it is:** The AI app (or middleware between the app and Nexus) sends `POST /inbound/{source}` with a JSON body. This is the most flexible method — any HTTP client can use it.

**Supported by:** Open WebUI via Pipelines, OpenClaw plugin, custom integrations.

**What you need:**
```
URL:  http://localhost:8080/inbound/{source-name}
Auth: Authorization: Bearer bfn_data_YOUR_KEY
Body: JSON with your memory content
```

### Creating a source config for a new app

Every app that uses Methods B, C, or D needs a source config file. Source configs control the API key, rate limits, and how the app's payload maps to memory fields.

#### Step 1 — Create the source file

```bash
# macOS/Linux
cp ~/.bubblefish/Nexus/sources/default.toml ~/.bubblefish/Nexus/sources/myapp.toml

# Windows
Copy-Item "$env:USERPROFILE\.bubblefish\Nexus\sources\default.toml" `
          "$env:USERPROFILE\.bubblefish\Nexus\sources\myapp.toml"
```

#### Step 2 — Edit the source file

```toml
[source]
name       = "myapp"              # ← matches the URL: /inbound/myapp
api_key    = "bfn_data_REPLACE_WITH_NEW_KEY"   # ← unique key for this app
can_read   = true
can_write  = true
target_destination = "sqlite"

rate_limit_rpm    = 2000
max_payload_bytes = 65536

[source.mapping]
# These defaults work for most apps that use OpenAI-shaped payloads
content = "message.content"
role    = "message.role"
model   = "model"
```

Change `name` to match your app. Set a unique `api_key`. Nexus hot-reloads this file automatically — no restart needed.

#### Step 3 — Test the connection

```bash
# macOS/Linux
curl -X POST http://localhost:8080/inbound/myapp \
  -H "Authorization: Bearer bfn_data_YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{"message":{"content":"hello from myapp","role":"user"},"model":"test"}'
# Expected response: {"payload_id":"...","status":"accepted"}

# Windows PowerShell
$key = "bfn_data_YOUR_KEY"
Invoke-RestMethod -Uri "http://localhost:8080/inbound/myapp" `
  -Method POST `
  -Headers @{ Authorization = "Bearer $key"; "Content-Type" = "application/json" } `
  -Body '{"message":{"content":"hello from myapp","role":"user"},"model":"test"}'
```

#### Step 4 — Verify the memory was stored

```bash
curl "http://localhost:8080/query/sqlite?limit=5" \
  -H "Authorization: Bearer bfn_data_YOUR_KEY"
```

### Adjusting payload mapping

If your app sends data in a different shape, adjust `[source.mapping]`. For example, if your app sends:
```json
{"text": "remember this", "sender": "assistant", "model_name": "gpt-4"}
```

Then:
```toml
[source.mapping]
content = "text"
role    = "sender"
model   = "model_name"
```

Use dot notation for nested fields: `content = "body.text"` for `{"body": {"text": "..."}}`

### Idempotency — preventing duplicate writes

Add `X-Idempotency-Key` to your write requests. Same key from the same source returns success without writing again:

```bash
curl -X POST http://localhost:8080/inbound/myapp \
  -H "Authorization: Bearer bfn_data_YOUR_KEY" \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: session-123-memory-456" \
  -d '{"message":{"content":"hello","role":"user"},"model":"test"}'
```

### Querying memories via HTTP (for read-capable integrations)

```bash
# Basic query
curl "http://localhost:8080/query/sqlite" \
  -H "Authorization: Bearer bfn_data_YOUR_KEY"

# With parameters
curl "http://localhost:8080/query/sqlite?limit=20&profile=balanced" \
  -H "Authorization: Bearer bfn_data_YOUR_KEY"
```

Query parameters:
- `limit` — max results (default 10, max 200)
- `q` — search query string
- `profile` — `fast` (no vector search), `balanced` (default), `deep` (max recall)
- `cursor` — pagination cursor from previous response
- `subject` — filter by subject tag

### Connecting to a local Postgres database

Add a postgres destination config:

```toml
# ~/.bubblefish/Nexus/destinations/postgres.toml
[destination]
name = "postgres"
type = "postgres"
connection_string = "env:POSTGRES_URL"
```

Set the environment variable:
```bash
export POSTGRES_URL="postgres://user:password@localhost:5432/mydb"
```

Then in your source config, set:
```toml
target_destination = "postgres"
```

Restart Nexus. Run `bubblefish doctor` to verify the connection.

---

## 13. Troubleshooting Guide

### "Not a valid Win32 application" / downloaded file is tiny

The download failed. The Windows binary should be ~25 MB. If it's a few bytes (often exactly 9 bytes — the literal string "Not Found"), GitHub returned a 404 error body and your client saved it as if it were the binary.

```powershell
# Check the file size
Get-Item .\bubblefish.exe | Select-Object Length
# If much smaller than ~25 MB, the download failed
```

**Most common causes:**

1. **No published release at `/releases/latest/`.** Open `https://github.com/bubblefish-tech/nexus/releases` in a browser and confirm a release is listed and marked **Latest** (not Draft, not Pre-release), with the Windows binary attached as an asset.
2. **`Invoke-WebRequest` saved an error page silently.** Use `curl.exe -fL --ssl-no-revoke ...` instead — the `-f` flag makes curl exit non-zero on HTTP errors instead of saving a 9-byte error body.
3. **Corporate proxy or antivirus stripping the binary.** Try downloading directly from the Releases page in a browser: `https://github.com/bubblefish-tech/nexus/releases/latest`.

### Daemon won't start — port already in use

```powershell
# Windows — find what's using port 8080
Get-NetTCPConnection -LocalPort 8080 -State Listen -ErrorAction SilentlyContinue

# Kill old Nexus process
taskkill /F /IM bubblefish.exe /T
.\bubblefish.exe start
```

```bash
# macOS/Linux — find what's using port 8080
lsof -i :8080
pkill -f bubblefish
bubblefish start
```

### Tools not appearing in Claude Desktop or Cursor

1. Is Nexus running? `curl http://localhost:8080/health`
2. Is the binary path in the config file correct?
   - Windows: double backslashes (`C:\\Users\\...`)
   - macOS: full path (`/usr/local/bin/bubblefish`), not just `bubblefish`
3. Restart the AI client completely (quit and reopen)

### 401 Unauthorized errors

Most common cause: using the wrong type of key.
- `bfn_mcp_` keys only work on port 7474 (MCP server)
- `bfn_data_` keys only work on `/inbound/` and `/query/` endpoints
- Admin keys only work on `/api/` endpoints

Check which key you're using and which endpoint you're hitting.

### Writes succeed (status "accepted") but memories don't appear in queries

```bash
# Check queue depth — is the background writer backed up?
curl http://localhost:8080/health

# Run the diagnostic tool
bubblefish doctor

# Check source mapping — maybe content is empty because field names don't match
cat ~/.bubblefish/Nexus/sources/default.toml   # or the relevant source file
```

If `content` arrives empty in your memories, the `[source.mapping]` in your source TOML doesn't match the field names your app sends.

### Semantic search not returning results (vector search)

Without an embedding provider, Nexus uses structured search (SQL) only. Semantic/vector search requires Ollama:

```bash
ollama pull nomic-embed-text
```

Then enable in daemon.toml:
```toml
[daemon.embedding]
enabled   = true
provider  = "ollama"
url       = "http://localhost:11434"
model     = "nomic-embed-text"
dimensions = 768
```

Restart Nexus.

### Docker containers can't reach Nexus

**macOS:** Should work automatically via `host.docker.internal`.

**Linux:** Requires `--add-host=host.docker.internal:host-gateway` when creating the container. If you forgot it, you need to recreate the container:

```bash
docker stop open-webui && docker rm open-webui
docker run -d -p 3000:8080 --add-host=host.docker.internal:host-gateway \
  --name open-webui ghcr.io/open-webui/open-webui:main
```

**Test from inside the container:**
```bash
docker exec -it open-webui curl http://host.docker.internal:8080/health
```

### OpenClaw (in WSL 2 on Windows) can't reach Nexus

WSL 2 has its own network. `localhost` inside WSL points to the Linux VM, not Windows.

```bash
# From inside WSL — get the Windows host IP
cat /etc/resolv.conf | grep nameserver

# Use that IP for NEXUS_URL
export NEXUS_URL="http://<windows-ip-from-above>:8080"
```

Note: This IP may change when WSL restarts. Re-check after rebooting.

### Logs location

```bash
# macOS/Linux
tail -f ~/.bubblefish/Nexus/logs/bubblefish.log

# Windows
Get-Content "$env:USERPROFILE\.bubblefish\Nexus\logs\bubblefish.log" -Wait
```

### Run built-in diagnostics

```bash
bubblefish doctor          # Checks config, destinations, WAL, disk space
bubblefish lint            # Config warnings
bubblefish status          # Live daemon health
bubblefish status --paths  # Shows where config files are
```

---

## 14. Configuration Quick Reference

### Important paths

| Platform | Config directory |
|----------|-----------------|
| Windows | `C:\Users\<username>\.bubblefish\Nexus\` |
| macOS | `~/.bubblefish/Nexus/` |
| Linux | `~/.bubblefish/Nexus/` |

Override with `BUBBLEFISH_HOME` environment variable.

### Port map

| Port | Service |
|------|---------|
| 8080 | HTTP API (writes, reads, admin) |
| 7474 | MCP Server (Claude Desktop, Cursor, ChatGPT, etc.) |
| 8081 | Web dashboard (experimental, in development) |

### Key commands

| Command | What it does |
|---------|-------------|
| `bubblefish install --mode simple` | Initialize config and generate API keys |
| `bubblefish start` | Start the daemon |
| `bubblefish stop` | Graceful shutdown |
| `bubblefish status` | Show health and queue depth |
| `bubblefish doctor` | Full diagnostic check |
| `bubblefish lint` | Config file validation |
| `bubblefish demo` | Run the crash recovery demo |
| `bubblefish bench` | Benchmarks (throughput, latency, retrieval) |

### Run as a service

**Windows:**
```powershell
.\bubblefish.exe service install
Start-Service bubblefish-nexus
```

**macOS:**
```bash
bubblefish service install --launchd > ~/Library/LaunchAgents/ai.bubblefish.nexus.plist
launchctl load ~/Library/LaunchAgents/ai.bubblefish.nexus.plist
```

**Linux:**
```bash
bubblefish service install --systemd > /etc/systemd/system/bubblefish-nexus.service
systemctl daemon-reload
systemctl enable --now bubblefish-nexus
```

---

*BubbleFish Nexus is AGPL-3.0 open source. Commercial licensing available for enterprise use.*
*Contact: licensing@bubblefish.sh*
*Docs: github.com/bubblefish-tech/nexus*
