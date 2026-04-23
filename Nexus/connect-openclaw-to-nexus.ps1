#!/usr/bin/env pwsh
# ============================================================
#
#  connect-openclaw-to-nexus.ps1
#
#  Connects your OpenClaw installation to Nexus so they can
#  share memory and talk to each other via A2A.
#
#  Usage:  .\connect-openclaw-to-nexus.ps1
#
#  Copyright (c) 2026 Shawn Sammartano. All rights reserved.
#
# ============================================================

param(
    [string]$OpenClawHost = "",
    [int]$OpenClawPort = 18789
)

$ErrorActionPreference = "Stop"

# ============================================================
# BANNER
# ============================================================

Write-Host ""
Write-Host "  Connect OpenClaw to Nexus" -ForegroundColor Cyan
Write-Host "  =========================" -ForegroundColor Cyan

# ============================================================
# STEP 1: LOCATE NEXUS CONFIG
# ============================================================

Write-Host ""
Write-Host "  [1/7] Locating Nexus configuration..." -ForegroundColor Cyan

$NexusHome  = Join-Path $env:USERPROFILE ".nexus\Nexus"
$ConfigFile = Join-Path $NexusHome "daemon.toml"
$AgentsDir  = Join-Path $NexusHome "a2a\agents"
$AgentFile  = Join-Path $AgentsDir "openclaw.toml"

if (-not (Test-Path $ConfigFile)) {
    Write-Host "      daemon.toml not found at $ConfigFile" -ForegroundColor Red
    Write-Host "      Run 'nexus install' first." -ForegroundColor Red
    exit 1
}

Write-Host "      Found: $ConfigFile" -ForegroundColor Green

# ============================================================
# STEP 2: DETECT TOPOLOGY
# ============================================================

Write-Host ""
Write-Host "  [2/7] Detecting network topology..." -ForegroundColor Cyan

$wsl2Running = $false
$wsl2HostIP  = "localhost"
try {
    $wslCheck = wsl.exe --list --running 2>$null
    if ($LASTEXITCODE -eq 0 -and $wslCheck -match "Running") {
        $wsl2Running = $true
        $wsl2Adapter = Get-NetIPAddress -InterfaceAlias "vEthernet (WSL*)" -AddressFamily IPv4 -ErrorAction SilentlyContinue
        if ($wsl2Adapter) { $wsl2HostIP = $wsl2Adapter.IPAddress }
    }
} catch { }

$dockerRunning = $false
try {
    docker ps 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) { $dockerRunning = $true }
} catch { }

$lanIP = $null
try {
    $lanIP = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object {
        $_.InterfaceAlias -notmatch "Loopback|WSL|vEthernet" -and
        $_.IPAddress -notmatch "^169\." -and
        $_.IPAddress -ne "127.0.0.1"
    } | Select-Object -First 1).IPAddress
} catch { }
if (-not $lanIP) { $lanIP = "YOUR_LAN_IP" }

if ($OpenClawHost -ne "") {
    $openclawURL = "http://" + $OpenClawHost + ":" + $OpenClawPort
    Write-Host "      Using provided host: $openclawURL" -ForegroundColor Green
}
elseif ($wsl2Running) {
    $openclawURL = "http://localhost:" + $OpenClawPort
    Write-Host "      WSL2 detected. OpenClaw at $openclawURL" -ForegroundColor Green
}
else {
    $openclawURL = "http://localhost:" + $OpenClawPort
    Write-Host "      OpenClaw at $openclawURL" -ForegroundColor Green
}

if ($dockerRunning) { Write-Host "      Docker: active" -ForegroundColor Green }
Write-Host "      LAN IP: $lanIP" -ForegroundColor Green

# ============================================================
# STEP 3: GET OR GENERATE THE OPENCLAW GATEWAY TOKEN
# ============================================================

Write-Host ""
Write-Host "  [3/7] Reading OpenClaw gateway token..." -ForegroundColor Cyan

$gatewayToken   = $null
$tokenGenerated = $false

# Try Windows config
$ocConfigWindows = Join-Path $env:USERPROFILE ".openclaw\openclaw.json"
if (Test-Path $ocConfigWindows) {
    try {
        $ocConfig = Get-Content $ocConfigWindows -Raw | ConvertFrom-Json
        if ($ocConfig.token -and $ocConfig.token -ne "") {
            $gatewayToken = $ocConfig.token
            Write-Host "      Read token from $ocConfigWindows" -ForegroundColor Green
        }
    } catch { }
}

# Try WSL2 config
if ((-not $gatewayToken) -and $wsl2Running) {
    try {
        $wslJson = wsl.exe bash -c "cat ~/.openclaw/openclaw.json 2>/dev/null" 2>$null
        if ($wslJson) {
            $wslConfig = $wslJson | ConvertFrom-Json
            if ($wslConfig.token -and $wslConfig.token -ne "") {
                $gatewayToken = $wslConfig.token
                Write-Host "      Read token from WSL2 ~/.openclaw/openclaw.json" -ForegroundColor Green
            }
        }
    } catch { }
}

# Generate if not found
if (-not $gatewayToken) {
    $gatewayToken = -join ((1..32) | ForEach-Object { '{0:x2}' -f (Get-Random -Max 256) })
    $tokenGenerated = $true
    Write-Host "      No existing token found. Generated new one." -ForegroundColor Yellow
    Write-Host "      You will need to set this in OpenClaw (see below)." -ForegroundColor Yellow
}

$tokenPreview = $gatewayToken.Substring(0, 12) + "..."
Write-Host "      Token: $tokenPreview" -ForegroundColor Green

# ============================================================
# STEP 4: SET OPENCLAW_TOKEN ENV VAR
# ============================================================

Write-Host ""
Write-Host "  [4/7] Setting OPENCLAW_TOKEN environment variable..." -ForegroundColor Cyan

[System.Environment]::SetEnvironmentVariable("OPENCLAW_TOKEN", $gatewayToken, "User")
$env:OPENCLAW_TOKEN = $gatewayToken

Write-Host "      OPENCLAW_TOKEN persisted to user environment" -ForegroundColor Green

# ============================================================
# STEP 5: CREATE AGENT REGISTRATION TOML
# ============================================================

Write-Host ""
Write-Host "  [5/7] Creating agent registration file..." -ForegroundColor Cyan

New-Item -ItemType Directory -Force -Path $AgentsDir | Out-Null

$agentToml = @"
# OpenClaw A2A agent registration
# Created by connect-openclaw-to-nexus.ps1

[agent]
name = "openclaw"
methods = ["tasks/send", "tasks/get", "agent/card"]

[transport]
kind = "http"

[transport.http]
url = "$openclawURL"
auth = "bearer"
bearer_token_env = "OPENCLAW_TOKEN"
"@

$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($AgentFile, $agentToml, $utf8NoBom)

Write-Host "      Written: $AgentFile" -ForegroundColor Green

# ============================================================
# STEP 6: ENSURE A2A ENABLED IN DAEMON.TOML
# ============================================================

Write-Host ""
Write-Host "  [6/7] Checking A2A is enabled in daemon.toml..." -ForegroundColor Cyan

$content = Get-Content $ConfigFile -Raw

if ($content -notmatch "\[a2a\]") {
    $newline = [System.Environment]::NewLine
    $addition = $newline + "[a2a]" + $newline + "enabled = true" + $newline
    Add-Content -Path $ConfigFile -Value $addition
    Write-Host "      Added [a2a] enabled = true" -ForegroundColor Green
}
elseif ($content -match "enabled\s*=\s*false") {
    $content = $content -replace "(enabled\s*=\s*)false", "`${1}true"
    [System.IO.File]::WriteAllText($ConfigFile, $content, $utf8NoBom)
    Write-Host "      Changed enabled = false to true" -ForegroundColor Green
}
else {
    Write-Host "      [a2a] already enabled" -ForegroundColor Green
}

# ============================================================
# STEP 7: RESTART NEXUS + VALIDATE
# ============================================================

Write-Host ""
Write-Host "  [7/7] Restarting Nexus and validating..." -ForegroundColor Cyan

nexus stop
Start-Process -FilePath "nexus" -ArgumentList "start" -WindowStyle Hidden
Start-Sleep -Seconds 3

$healthOk = $false
try {
    $health = curl.exe -s http://localhost:8080/health 2>$null
    if ($health) { $healthOk = $true }
} catch { }

$oclawOk = $false
try {
    $oclawUrl = "http://localhost:" + $OpenClawPort + "/healthz"
    $oclawHealth = curl.exe -s $oclawUrl 2>$null
    if ($oclawHealth -match "ok") { $oclawOk = $true }
} catch { }

Write-Host ""
Write-Host "  Results:" -ForegroundColor White
Write-Host ""

if ($healthOk) { Write-Host "    Nexus daemon           OK" -ForegroundColor Green }
else            { Write-Host "    Nexus daemon           FAILED" -ForegroundColor Red }

if ($oclawOk)  { Write-Host "    OpenClaw gateway       OK" -ForegroundColor Green }
else            { Write-Host "    OpenClaw gateway       NOT RESPONDING" -ForegroundColor Yellow }

Write-Host "    Agent TOML             OK" -ForegroundColor Green
Write-Host "    OPENCLAW_TOKEN         OK" -ForegroundColor Green
Write-Host "    [a2a] enabled          OK" -ForegroundColor Green

# ============================================================
# OPENCLAW INSTRUCTIONS (if needed)
# ============================================================

if ($tokenGenerated -or (-not $oclawOk)) {
    Write-Host ""
    Write-Host "  ============================================================" -ForegroundColor DarkCyan
    Write-Host "  OPENCLAW SETUP NEEDED" -ForegroundColor White
    Write-Host "  ============================================================" -ForegroundColor DarkCyan

    if ($tokenGenerated) {
        Write-Host ""
        Write-Host "  A new token was generated. Set it in OpenClaw:" -ForegroundColor White
        Write-Host ""
        Write-Host "  OPTION A: Edit the config file" -ForegroundColor Yellow
        Write-Host "    Open ~/.openclaw/openclaw.json" -ForegroundColor White
        Write-Host "    Find the token field and replace with:" -ForegroundColor White
        Write-Host "    $gatewayToken" -ForegroundColor Green
        Write-Host "    Then restart OpenClaw." -ForegroundColor White

        if ($wsl2Running) {
            Write-Host ""
            Write-Host "  OPTION B: WSL2 (paste into terminal)" -ForegroundColor Yellow
            Write-Host "    cd ~/.openclaw" -ForegroundColor Green
            Write-Host "    cp openclaw.json openclaw.json.bak" -ForegroundColor Green
            Write-Host "    sed -i 's|""token"":""[^""]*""|""token"":""$gatewayToken""|' openclaw.json" -ForegroundColor Green
            Write-Host "    pkill -f openclaw-gateway" -ForegroundColor Green
            Write-Host "    # Then restart your gateway" -ForegroundColor Gray
        }

        Write-Host ""
        Write-Host "  OPTION C: Docker (.env file)" -ForegroundColor Yellow
        Write-Host "    OPENCLAW_GATEWAY_TOKEN=$gatewayToken" -ForegroundColor Green
        Write-Host "    docker compose down && docker compose up -d" -ForegroundColor Green
    }

    if (-not $oclawOk) {
        Write-Host ""
        Write-Host "  OpenClaw is not responding on port $OpenClawPort." -ForegroundColor Yellow
        Write-Host "  Start it, then run: nexus doctor" -ForegroundColor Yellow
    }

    Write-Host ""
    Write-Host "  ============================================================" -ForegroundColor DarkCyan
}
else {
    Write-Host ""
    Write-Host "  ============================================================" -ForegroundColor DarkCyan
    Write-Host "  CONNECTION COMPLETE" -ForegroundColor Green
    Write-Host "  Nexus and OpenClaw are linked." -ForegroundColor Green
    Write-Host "  ============================================================" -ForegroundColor DarkCyan
}

Write-Host ""
Write-Host "  Verify anytime: nexus doctor" -ForegroundColor Gray
Write-Host ""
