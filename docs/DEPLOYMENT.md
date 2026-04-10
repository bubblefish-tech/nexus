# Deployment Guide

How to get Nexus running and keep it running.

---

## Requirements

- Go 1.26+ (only needed if building from source)
- A system that can run a persistent background process (Windows Service, systemd, launchd)
- At minimum 256MB RAM and ~100MB disk for SQLite + WAL
- SQLite is bundled. No other runtime dependencies for the default configuration.

---

## Install

### Pre-built binary (recommended)

Download from the [releases page](https://github.com/bubblefish-tech/nexus/releases).

```bash
# Linux (amd64)
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-linux-amd64 -o bubblefish
chmod +x bubblefish && sudo mv bubblefish /usr/local/bin/

# macOS (Apple Silicon)
curl -L https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-darwin-arm64 -o bubblefish
chmod +x bubblefish && sudo mv bubblefish /usr/local/bin/
```

```powershell
# Windows (PowerShell)
Invoke-WebRequest -Uri "https://github.com/bubblefish-tech/nexus/releases/latest/download/bubblefish-windows-amd64.exe" -OutFile bubblefish.exe
```

### Build from source

```bash
git clone https://github.com/bubblefish-tech/nexus.git
cd nexus
go build -o bubblefish ./cmd/bubblefish/
```

---

## Initialize

```bash
bubblefish install --mode simple
```

This creates:
- `daemon.toml` — main config file
- `sources/default.toml` — default source with auto-generated API key
- `destinations/sqlite.toml` — SQLite backend
- `wal/` — Write-Ahead Log directory

The install command prints your API key and example commands.

---

## Start

```bash
bubblefish start
```

Verify it's running:

```bash
bubblefish status
```

Or:
```bash
curl http://localhost:8080/health
```

---

## Run as a service

### Windows

```powershell
# Install as Windows Service
.\bubblefish.exe service install

# Start it
Start-Service bubblefish-nexus
```

### Linux (systemd)

```bash
# Generate systemd unit file
bubblefish service install --systemd > /etc/systemd/system/bubblefish-nexus.service

systemctl daemon-reload
systemctl enable bubblefish-nexus
systemctl start bubblefish-nexus
```

### macOS (launchd)

```bash
bubblefish service install --launchd > ~/Library/LaunchAgents/ai.bubblefish.nexus.plist
launchctl load ~/Library/LaunchAgents/ai.bubblefish.nexus.plist
```

---

## Remote access (Cloudflare Tunnel)

For clients on other machines or cloud AI services:

```bash
# Install cloudflared: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/get-started/
cloudflared tunnel login
cloudflared tunnel create nexus
cloudflared tunnel route dns nexus your-subdomain.yourdomain.com

# Run tunnel pointing at MCP port
cloudflared tunnel --url http://localhost:7474 run nexus
```

Then configure your AI clients to use `https://your-subdomain.yourdomain.com` as the MCP endpoint.

**Security note:** Add a Cloudflare Access policy on the tunnel domain. For automated AI clients that can't do browser auth, add a WAF bypass rule matching your `bfn_mcp_` key in the Authorization header, ordered above the Access policy. Nexus validates the key server-side regardless.

---

## Key rotation

To rotate a source API key:

1. Generate a new key
2. Update the source TOML file with the new key
3. Nexus hot-reloads source configs automatically — no restart needed
4. Update the key in your AI client configuration

---

## Monitoring

Prometheus metrics are available at `http://localhost:8080/metrics` (requires admin token).

Key metrics to watch:
- `nexus_wal_queue_depth` — should be near 0 under normal load
- `nexus_writes_total{status="error"}` — persistent errors mean a destination is unhealthy
- `nexus_destination_latency_seconds` — spikes indicate destination issues

---

## Upgrade

```bash
# Stop the daemon
bubblefish stop

# Replace the binary with the new version
# (WAL and data are not touched by binary replacement)

# Start again
bubblefish start

# Verify
bubblefish status
```

Nexus never modifies the WAL format in a way that breaks backward compatibility within a major version. Minor version upgrades are always in-place safe.

---

## Backup and restore

```bash
# Create a backup snapshot (config, WAL, optionally SQLite DB)
bubblefish backup create

# Restore from backup
bubblefish backup restore --from /path/to/backup
```

---

## Troubleshooting

**Daemon won't start**
- Check `daemon.toml` is valid TOML: `bubblefish lint`
- Check that ports 8080 and 7474 are not in use
- On Windows: `taskkill /F /IM bubblefish.exe /T` to kill old processes

**MCP clients can't connect**
- Confirm the daemon is running: `bubblefish status`
- Verify the `bfn_mcp_` key matches what's in `daemon.toml`
- Run `bubblefish mcp test` to self-test the MCP server
- Check firewall rules if connecting remotely

**Writes succeeding but not appearing in search**
- Run `bubblefish doctor` to check WAL replay status and destination health
- Check queue depth: a non-zero queue means writes are queued, not lost

**Semantic search not available**
- An embedding provider must be configured (`[daemon.embedding]` in `daemon.toml`)
- Without embeddings, Nexus uses structured lookup (Stage 3) instead of vector search (Stage 4)

---

## Data location

By default, all data lives under `~/.bubblefish/Nexus/` (configurable via `BUBBLEFISH_HOME`):
- `wal/` — Write-Ahead Log segments
- `data/nexus.db` — SQLite destination
- `daemon.toml` — main config
- `sources/` — source configs (contain API keys)
- `logs/` — application and security event logs

Back up the entire `~/.bubblefish/Nexus/` directory. That's everything you need to restore.
