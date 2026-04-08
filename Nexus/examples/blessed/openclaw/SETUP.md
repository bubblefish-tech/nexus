# BubbleFish Nexus + OpenClaw Setup Guide

Connect OpenClaw (running in WSL 2) to BubbleFish Nexus (running on Windows) for sovereign local-first AI memory across all your messaging channels — WhatsApp, Telegram, Discord, Slack, Signal, and more.

---

## How It Works

```
Your phone (WhatsApp/Telegram/etc.)
        ↓
OpenClaw Gateway (WSL 2, port 18789)
        ↓
BubbleFish Nexus Plugin (nexus_write / nexus_search / nexus_status)
        ↓
Windows Port Proxy (port 18080 → 8080)
        ↓
BubbleFish Nexus Daemon (Windows, port 8080)
        ↓
SQLite memory store
```

Every conversation in OpenClaw can be saved to Nexus and retrieved by any other AI tool — Claude Desktop, Perplexity Comet, Open WebUI, ChatGPT.

---

## Prerequisites

- Windows 10/11 with WSL 2 enabled
- BubbleFish Nexus v0.1.1+ installed and running on Windows
- Node.js 22 LTS or 24 installed in WSL 2
- OpenClaw installed in WSL 2

---

## Part 1: Windows — Port Proxy Setup

Nexus binds to `127.0.0.1:8080` on Windows. WSL 2 runs in its own VM and cannot reach Windows localhost directly. You need a port proxy to forward a Windows port through to Nexus.

### Step 1.1: Open PowerShell as Administrator on Windows

```powershell
# Create the port proxy — forwards WSL traffic to Nexus
netsh interface portproxy add v4tov4 `
  listenaddress=0.0.0.0 `
  listenport=18080 `
  connectaddress=127.0.0.1 `
  connectport=8080

# Verify it was created
netsh interface portproxy show all
```

Expected output:
```
Listen on ipv4:             Connect to ipv4:
Address         Port        Address         Port
--------------- ----------  --------------- ----------
0.0.0.0         18080       127.0.0.1       8080
```

### Step 1.2: Add Windows Firewall Rule (if needed)

```powershell
# Allow WSL 2 to reach port 18080
New-NetFirewallRule -DisplayName "Nexus WSL2 Proxy" `
  -Direction Inbound `
  -Protocol TCP `
  -LocalPort 18080 `
  -Action Allow
```

### Step 1.3: Find Your Windows Host IP From WSL

From your WSL 2 terminal:
```bash
cat /etc/resolv.conf | grep nameserver | awk '{print $2}'
```

This gives you the Windows host IP — typically `172.28.x.x`. Save this — you'll need it for the plugin config.

### Step 1.4: Verify Nexus is Reachable From WSL 2

```bash
# From WSL 2 terminal — replace with your actual Windows host IP
NEXUS_HOST=$(cat /etc/resolv.conf | grep nameserver | awk '{print $2}')
curl -s http://$NEXUS_HOST:18080/health
```

Expected: `{"status":"ok"}` or similar health response.

If this fails:
- Make sure Nexus is running on Windows: `.\bubblefish start --home D:\Test\BubbleFish\v010-dogfood\home`
- Check the port proxy was created: `netsh interface portproxy show all`
- Check Windows Firewall isn't blocking port 18080

### Step 1.5: Get Your Nexus Data Key

On Windows PowerShell:
```powershell
Get-Content "D:\Test\BubbleFish\v010-dogfood\home\sources\default.toml" | Select-String "bfn_data_"
```

Copy the full `bfn_data_...` key — you'll need it in Part 3.

---

## Part 2: WSL 2 — Install OpenClaw

If OpenClaw isn't installed yet:

```bash
# Install Node.js 22 LTS if needed
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
sudo apt-get install -y nodejs

# Verify
node --version  # should be v22.x.x

# Install OpenClaw
npm install -g openclaw@latest

# Run onboarding
openclaw onboard --install-daemon
```

Follow the onboarding wizard to connect your first channel (Telegram is fastest for testing).

---

## Part 3: WSL 2 — Install the BubbleFish Nexus Plugin

### Step 3.1: Copy Plugin Files

Copy the plugin directory to your WSL 2 home:

```bash
# Create plugin directory
mkdir -p ~/.openclaw/plugins/bubblefish-nexus
```

Copy these three files into `~/.openclaw/plugins/bubblefish-nexus/`:
- `package.json`
- `openclaw.plugin.json`
- `index.ts`

Or if the plugin is published to npm/ClawHub:
```bash
openclaw plugins install @bubblefish/openclaw-nexus
```

### Step 3.2: Install Plugin Dependencies

```bash
cd ~/.openclaw/plugins/bubblefish-nexus
npm install
```

### Step 3.3: Configure Environment Variables in OpenClaw

Edit `~/.openclaw/openclaw.json` and add the Nexus environment variables:

```json
{
  "env": {
    "NEXUS_URL": "http://172.28.x.x:18080",
    "NEXUS_DATA_KEY": "bfn_data_REPLACE_WITH_YOUR_DATA_KEY",
    "NEXUS_SOURCE": "default",
    "NEXUS_COLLECTION": "openclaw"
  }
}
```

**Replace:**
- `172.28.x.x` with your actual Windows host IP from Step 1.3
- `bfn_data_YOUR_DATA_KEY_HERE` with your actual data key from Step 1.5

### Step 3.4: Enable Plugin Tools in OpenClaw Config

Add to `~/.openclaw/openclaw.json`:

```json
{
  "env": {
    "NEXUS_URL": "http://172.28.x.x:18080",
    "NEXUS_DATA_KEY": "bfn_data_...",
    "NEXUS_SOURCE": "default",
    "NEXUS_COLLECTION": "openclaw"
  },
  "tools": {
    "allow": ["nexus_write", "nexus_search", "nexus_status"]
  },
  "plugins": {
    "local": ["~/.openclaw/plugins/bubblefish-nexus"]
  }
}
```

### Step 3.5: Restart OpenClaw

```bash
openclaw stop
openclaw start
```

Or if running as a daemon:
```bash
openclaw restart
```

---

## Part 4: Verify the Connection

### Step 4.1: Check Plugin Loaded

```bash
openclaw plugins list
```

Expected: `bubblefish-nexus` appears in the list.

### Step 4.2: Test via OpenClaw Dashboard

```bash
openclaw dashboard
```

Open `http://127.0.0.1:18789` in your browser. Start a new chat and type:

```
Call nexus_status
```

Expected response: `BubbleFish Nexus is running at http://172.28.x.x:18080. Health check: OK`

### Step 4.3: Test Write and Search

```
Save this to Nexus memory: I'm testing the OpenClaw + Nexus connection from WSL 2 on April 7 2026.
```

Then:
```
Search Nexus for memories about OpenClaw WSL
```

Expected: Your test memory returned in results.

---

## Part 5: Make the Port Proxy Persistent

The `netsh portproxy` rule survives reboots on Windows — it's stored in the registry. However, the WSL 2 host IP (`172.28.x.x`) **changes on every WSL restart**. You have two options:

### Option A: Static Host IP Script (Recommended)

Create a Windows startup script that updates the OpenClaw config with the current WSL IP:

**File: `C:\Users\shawn\update-nexus-wsl.ps1`**
```powershell
# Get current WSL 2 host IP
$wslIP = (wsl cat /etc/resolv.conf | Select-String "nameserver").ToString().Split(" ")[1].Trim()
Write-Host "WSL 2 host IP: $wslIP"

# Update OpenClaw config in WSL
wsl bash -c "sed -i 's|\"NEXUS_URL\": \"http://[^\"]*\"|\"NEXUS_URL\": \"http://$($wslIP):18080\"|' ~/.openclaw/openclaw.json"

# Restart OpenClaw in WSL
wsl bash -c "openclaw restart"

Write-Host "Nexus URL updated to http://$($wslIP):18080"
```

Run this script after each Windows restart.

### Option B: WSL 2 Fixed IP (Advanced)

Add to `/etc/wsl.conf` in WSL 2:
```ini
[network]
generateResolvConf = false
```

Then set a fixed IP in your Windows `.wslconfig`:
```ini
[wsl2]
# This requires Windows 11 22H2+
```

Note: Fixed WSL 2 IPs require Windows 11 22H2 or later. For most setups Option A is simpler.

---

## Part 6: Auto-Save Conversations (Optional)

To automatically save every OpenClaw conversation to Nexus without the agent needing to call `nexus_write` explicitly, add a **hook** to your OpenClaw config:

```json
{
  "hooks": {
    "afterMessage": {
      "exec": "~/.openclaw/hooks/save-to-nexus.sh"
    }
  }
}
```

**File: `~/.openclaw/hooks/save-to-nexus.sh`**
```bash
#!/bin/bash
# Receives message content via stdin as JSON
# Saves to Nexus automatically

NEXUS_URL="${NEXUS_URL:-http://localhost:8080}"
NEXUS_DATA_KEY="${NEXUS_DATA_KEY}"
NEXUS_SOURCE="${NEXUS_SOURCE:-default}"

if [ -z "$NEXUS_DATA_KEY" ]; then
  exit 0
fi

# Read message from stdin
MESSAGE=$(cat)
CONTENT=$(echo "$MESSAGE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('content',''))" 2>/dev/null)
ROLE=$(echo "$MESSAGE" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('role','user'))" 2>/dev/null)

if [ -z "$CONTENT" ]; then
  exit 0
fi

# Write to Nexus (fire and forget)
curl -s -X POST "$NEXUS_URL/inbound/$NEXUS_SOURCE" \
  -H "Authorization: Bearer $NEXUS_DATA_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"message\":{\"content\":$(echo "$CONTENT" | python3 -c "import sys,json; print(json.dumps(sys.stdin.read()))"),\"role\":\"$ROLE\"},\"model\":\"openclaw\",\"collection\":\"openclaw\"}" \
  > /dev/null 2>&1 &

exit 0
```

```bash
chmod +x ~/.openclaw/hooks/save-to-nexus.sh
```

---

## Troubleshooting

### "Cannot reach Nexus" from WSL 2

```bash
# Check current Windows host IP
NEXUS_HOST=$(cat /etc/resolv.conf | grep nameserver | awk '{print $2}')
echo "Windows host IP: $NEXUS_HOST"

# Test port proxy
curl -v http://$NEXUS_HOST:18080/health

# Check Nexus is running on Windows
# (run in Windows PowerShell)
# .\bubblefish status
```

### Port proxy not working after reboot

```powershell
# Re-add on Windows PowerShell (as admin)
netsh interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=18080
netsh interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=18080 connectaddress=127.0.0.1 connectport=8080
```

### Plugin not showing in openclaw plugins list

```bash
# Check plugin directory
ls ~/.openclaw/plugins/bubblefish-nexus/
# Should show: index.ts, package.json, openclaw.plugin.json

# Check for TypeScript errors
cd ~/.openclaw/plugins/bubblefish-nexus
npx tsc --noEmit

# Restart OpenClaw with verbose logging
openclaw stop
openclaw start --log-level debug
```

### NEXUS_DATA_KEY not set error

```bash
# Check your config
cat ~/.openclaw/openclaw.json | grep NEXUS

# Verify the key works (replace IP and key)
curl -s http://172.28.x.x:18080/query/sqlite \
  -H "Authorization: Bearer bfn_data_..."
```

---

## Port Reference

| Service | Host | Port | Notes |
|---------|------|------|-------|
| Nexus main API | Windows | 8080 | Inbound/query/admin |
| Nexus MCP server | Windows | 7474 | MCP clients only |
| Nexus dashboard | Windows | 8081 | Web UI |
| Port proxy (WSL→Nexus) | Windows | 18080 | Forwards to 8080 |
| OpenClaw gateway | WSL 2 | 18789 | Dashboard |

---

## Environment Variables Reference

| Variable | Description | Default |
|----------|-------------|---------|
| `NEXUS_URL` | Nexus API base URL from WSL 2 | `http://localhost:8080` |
| `NEXUS_DATA_KEY` | bfn_data_ token from daemon.toml | (required) |
| `NEXUS_SOURCE` | Source name from sources/*.toml | `default` |
| `NEXUS_COLLECTION` | Collection tag for OpenClaw memories | `openclaw` |
