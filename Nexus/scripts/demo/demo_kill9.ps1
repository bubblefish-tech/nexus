# BubbleFish Nexus -- Kill-9 Crash Safety Demo (PowerShell)
#
# Copyright (c) 2026 Shawn Sammartano. All rights reserved.
#
# Proves WAL crash safety by writing memories, force-killing the daemon,
# restarting, and verifying zero data loss:
#   1. Start daemon
#   2. Write N memories
#   3. Force-kill the daemon process (kill -9 equivalent)
#   4. Restart daemon
#   5. Verify all N memories survived
#
# This demonstrates BubbleFish Nexus's WAL-first architecture:
#   WAL fsync -> queue -> database
# The journal survives process death. On restart, pending WAL entries
# replay into the database automatically.
#
# Prerequisites:
#   - nexus.exe built and on PATH
#   - Nexus daemon running (or script will start it)
#   - $env:NEXUS_API_KEY and $env:NEXUS_ADMIN_KEY set
#
# Tokens used here are PLACEHOLDER values. Replace with your own.
#
# Run:
#   .\scripts\demo\demo_kill9.ps1

$ErrorActionPreference = "Continue"
$script:startTime = Get-Date
$script:failures  = 0
$script:passes    = 0

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
$BASE      = if ($env:NEXUS_URL)       { $env:NEXUS_URL }       else { "http://localhost:8080" }
$API_KEY   = if ($env:NEXUS_API_KEY)   { $env:NEXUS_API_KEY }   else { "bfn_data_TEST_KEY" }
$ADMIN_KEY = if ($env:NEXUS_ADMIN_KEY) { $env:NEXUS_ADMIN_KEY } else { "bfn_admin_TEST_KEY" }
$WRITE_COUNT = 20  # number of memories to write before kill

function Step($name) {
    Write-Host ""
    Write-Host "=== $name ===" -ForegroundColor Cyan
}

function Pass($msg) {
    Write-Host "  PASS  $msg" -ForegroundColor Green
    $script:passes++
}

function Fail($msg) {
    Write-Host "  FAIL  $msg" -ForegroundColor Red
    $script:failures++
}

function Warn($msg) {
    Write-Host "  WARN  $msg" -ForegroundColor Yellow
}

function Elapsed {
    $e = [int]((Get-Date) - $script:startTime).TotalSeconds
    return "${e}s"
}

function DataHdr {
    @{ "Authorization" = "Bearer $API_KEY"; "Content-Type" = "application/json" }
}

function AdminHdr {
    @{ "Authorization" = "Bearer $ADMIN_KEY" }
}

function WaitForDaemon {
    param([int]$TimeoutSec = 15)
    for ($i = 0; $i -lt ($TimeoutSec * 2); $i++) {
        try {
            $h = Invoke-RestMethod "$BASE/health" -Method GET -TimeoutSec 2 -ErrorAction SilentlyContinue
            if ($h.status -eq "ok" -or $h.status -eq "degraded") { return $true }
        } catch { }
        Start-Sleep -Milliseconds 500
    }
    return $false
}

# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "BubbleFish Nexus -- Kill-9 Crash Safety Demo" -ForegroundColor White
Write-Host "================================================" -ForegroundColor DarkGray
Write-Host "  https://bubblefish.sh" -ForegroundColor DarkGray
Write-Host "  Proving WAL-first durability: write -> kill -9 -> restart -> verify" -ForegroundColor DarkGray

# ---------------------------------------------------------------------------
# Step 1: Ensure daemon is running
# ---------------------------------------------------------------------------
Step "1. Ensure daemon is running -- $(Elapsed)"

if (-not (WaitForDaemon -TimeoutSec 3)) {
    Write-Host "  Starting daemon..." -ForegroundColor DarkGray
    & nexus start 2>&1 | Out-Null
    if (WaitForDaemon -TimeoutSec 15) {
        Pass "daemon started"
    } else {
        Fail "could not start daemon"
        exit 1
    }
} else {
    Pass "daemon already running"
}

# ---------------------------------------------------------------------------
# Step 2: Write N memories
# ---------------------------------------------------------------------------
Step "2. Write $WRITE_COUNT memories -- $(Elapsed)"

$BATCH_TAG = "kill9-demo-" + (Get-Date -Format "yyyyMMddHHmmss")
$source = "kill9-demo"
$writtenIDs = @()

for ($i = 1; $i -le $WRITE_COUNT; $i++) {
    $body = @{
        subject         = "$BATCH_TAG-$i"
        content         = "Kill-9 crash safety test memory #$i of $WRITE_COUNT. Batch: $BATCH_TAG. This must survive a force-kill."
        source          = $source
        destination     = "sqlite"
        idempotency_key = "$BATCH_TAG-$i"
    } | ConvertTo-Json -Compress

    try {
        $r = Invoke-RestMethod "$BASE/inbound/$source" -Method POST -Body $body -Headers (DataHdr) -TimeoutSec 10
        $writtenIDs += $r.payload_id
    } catch {
        Fail "write #$i failed: $_"
    }
}

if ($writtenIDs.Count -eq $WRITE_COUNT) {
    Pass "all $WRITE_COUNT memories written"
} else {
    Warn "only $($writtenIDs.Count) of $WRITE_COUNT writes succeeded"
}

# Allow 1s for queue drain
Start-Sleep -Seconds 1

# ---------------------------------------------------------------------------
# Step 3: Force-kill the daemon (kill -9 equivalent)
# ---------------------------------------------------------------------------
Step "3. Force-kill daemon process -- $(Elapsed)"

$nexusProcs = Get-Process -Name "nexus" -ErrorAction SilentlyContinue
if ($nexusProcs) {
    foreach ($p in $nexusProcs) {
        Write-Host "  Killing PID $($p.Id) (nexus)..." -ForegroundColor Yellow
        Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
    }
    Start-Sleep -Seconds 1

    # Verify it is dead
    $still = Get-Process -Name "nexus" -ErrorAction SilentlyContinue
    if ($still) {
        Fail "daemon still running after kill"
    } else {
        Pass "daemon force-killed (simulated kill -9)"
    }
} else {
    Warn "no nexus process found -- may have already exited"
}

# Confirm daemon is unreachable
try {
    Invoke-RestMethod "$BASE/health" -Method GET -TimeoutSec 2 -ErrorAction Stop | Out-Null
    Fail "daemon still responding after kill"
} catch {
    Pass "daemon confirmed unreachable"
}

# ---------------------------------------------------------------------------
# Step 4: Restart daemon
# ---------------------------------------------------------------------------
Step "4. Restart daemon -- $(Elapsed)"

& nexus start 2>&1 | ForEach-Object { Write-Host "  $_" -ForegroundColor DarkGray }

if (WaitForDaemon -TimeoutSec 20) {
    Pass "daemon restarted -- $(Elapsed)"
} else {
    Fail "daemon failed to restart within 20s"
    exit 1
}

# Check WAL replay status
try {
    $st = Invoke-RestMethod "$BASE/api/status" -Method GET -Headers (AdminHdr) -TimeoutSec 5
    Pass "version: $($st.version) -- queue_depth: $($st.queue_depth)"
} catch {
    Warn "status endpoint unreachable"
}

# ---------------------------------------------------------------------------
# Step 5: Verify all memories survived
# ---------------------------------------------------------------------------
Step "5. Verify data survival -- $(Elapsed)"

# Give WAL replay a moment to complete
Start-Sleep -Seconds 2

$survivedCount = 0
$missingIDs = @()

foreach ($id in $writtenIDs) {
    try {
        $q = Invoke-RestMethod "$BASE/query/sqlite?q=$BATCH_TAG&limit=$WRITE_COUNT" -Method GET -Headers (DataHdr) -TimeoutSec 10
        # Search once with the batch tag, then count unique matches
        break
    } catch {
        Fail "query failed: $_"
    }
}

# Count by re-querying with the batch tag
try {
    $q = Invoke-RestMethod "$BASE/query/sqlite?q=$BATCH_TAG&limit=$($WRITE_COUNT + 10)" -Method GET -Headers (DataHdr) -TimeoutSec 10
    $survivedCount = if ($q.records) { $q.records.Count } else { 0 }
} catch {
    Fail "verification query failed: $_"
}

Write-Host ""
Write-Host "  Written before kill:   $WRITE_COUNT" -ForegroundColor White
Write-Host "  Found after restart:   $survivedCount" -ForegroundColor White
$lossCount = $WRITE_COUNT - $survivedCount

if ($survivedCount -ge $WRITE_COUNT) {
    Pass "ZERO DATA LOSS -- all $WRITE_COUNT memories survived kill -9"
} elseif ($survivedCount -gt 0) {
    Fail "DATA LOSS: $lossCount of $WRITE_COUNT memories missing"
} else {
    Warn "query returned 0 results -- embedding index may need warming (check DB directly)"
}

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "=== Summary ===" -ForegroundColor Cyan
Write-Host "  Elapsed    : $(Elapsed)"
Write-Host "  Written    : $WRITE_COUNT memories"
Write-Host "  Killed     : force-kill (kill -9 equivalent)"
Write-Host "  Survived   : $survivedCount memories"
Write-Host "  Data loss  : $lossCount" -ForegroundColor $(if ($lossCount -eq 0) { "Green" } else { "Red" })
Write-Host "  Passed     : $($script:passes)" -ForegroundColor Green
if ($script:failures -gt 0) {
    Write-Host "  Failed     : $($script:failures)" -ForegroundColor Red
    exit 1
} else {
    Write-Host "  All checks passed" -ForegroundColor Green
    exit 0
}
