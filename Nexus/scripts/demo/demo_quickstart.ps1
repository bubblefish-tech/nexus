# BubbleFish Nexus -- Quickstart Demo (PowerShell)
#
# Copyright (c) 2026 Shawn Sammartano. All rights reserved.
#
# Demonstrates the core install-write-search loop in under 30 seconds:
#   1. Install Nexus in simple mode (idempotent)
#   2. Start the daemon
#   3. Write a memory
#   4. Search for it
#   5. Verify the result
#
# Prerequisites:
#   - nexus.exe built and on PATH (or in current directory)
#
# Tokens used here are PLACEHOLDER values. Replace them with your own
# after running `nexus install --mode simple`, which prints your keys.
#
# Run:
#   .\scripts\demo\demo_quickstart.ps1

$ErrorActionPreference = "Continue"
$script:startTime = Get-Date
$script:failures  = 0
$script:passes    = 0

# ---------------------------------------------------------------------------
# Configuration — replace these after first install
# ---------------------------------------------------------------------------
$BASE      = if ($env:NEXUS_URL)       { $env:NEXUS_URL }       else { "http://localhost:8080" }
$API_KEY   = if ($env:NEXUS_API_KEY)   { $env:NEXUS_API_KEY }   else { "bfn_data_TEST_KEY" }
$ADMIN_KEY = if ($env:NEXUS_ADMIN_KEY) { $env:NEXUS_ADMIN_KEY } else { "bfn_admin_TEST_KEY" }

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

# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "BubbleFish Nexus -- Quickstart Demo" -ForegroundColor White
Write-Host "=======================================" -ForegroundColor DarkGray
Write-Host "  https://bubblefish.sh" -ForegroundColor DarkGray

# ---------------------------------------------------------------------------
# Step 1: Install
# ---------------------------------------------------------------------------
Step "1. Install (simple mode)"
& nexus install --mode simple --force 2>&1 | ForEach-Object { Write-Host "  $_" }
if ($LASTEXITCODE -eq 0) {
    Pass "install completed -- $(Elapsed)"
} else {
    Warn "install returned non-zero (may already be installed -- continuing)"
}

# ---------------------------------------------------------------------------
# Step 2: Start daemon
# ---------------------------------------------------------------------------
Step "2. Start daemon"
& nexus start 2>&1 | ForEach-Object { Write-Host "  $_" }

# Wait for daemon health (up to 15s)
$healthy = $false
for ($i = 0; $i -lt 30; $i++) {
    try {
        $h = Invoke-RestMethod "$BASE/health" -Method GET -TimeoutSec 2 -ErrorAction SilentlyContinue
        if ($h.status -eq "ok" -or $h.status -eq "degraded") {
            $healthy = $true
            break
        }
    } catch { }
    Start-Sleep -Milliseconds 500
}
if ($healthy) {
    Pass "daemon is up -- $(Elapsed)"
} else {
    Fail "daemon did not respond in 15s"
    Write-Host "  Hint: check 'nexus doctor' for diagnostics" -ForegroundColor DarkYellow
    exit 1
}

# ---------------------------------------------------------------------------
# Step 3: Write a memory
# ---------------------------------------------------------------------------
Step "3. Write a memory"
$source = "demo-quickstart"
$body = @{
    subject         = "quickstart-test"
    content         = "BubbleFish Nexus stores AI memories with WAL-first crash safety and cryptographic provenance."
    source          = $source
    destination     = "sqlite"
    idempotency_key = "quickstart-demo-v1"
} | ConvertTo-Json -Compress

try {
    $r = Invoke-RestMethod "$BASE/inbound/$source" -Method POST -Body $body -Headers (DataHdr) -TimeoutSec 10
    $WRITTEN_ID = $r.payload_id
    Pass "written: $WRITTEN_ID -- $(Elapsed)"
} catch {
    Fail "write failed: $_"
    $WRITTEN_ID = $null
}

# ---------------------------------------------------------------------------
# Step 4: Search for it
# ---------------------------------------------------------------------------
Step "4. Search memories"
Start-Sleep -Seconds 1  # allow queue drain
try {
    $q = Invoke-RestMethod "$BASE/query/sqlite?q=WAL+crash+safety&limit=5" -Method GET -Headers (DataHdr) -TimeoutSec 10
    $count = if ($q.records) { $q.records.Count } else { 0 }
    if ($count -gt 0) {
        Pass "search returned $count result(s) -- $(Elapsed)"
        $firstContent = $q.records[0].content
        if ($firstContent.Length -gt 80) { $firstContent = $firstContent.Substring(0,80) + "..." }
        Write-Host "  First: $firstContent" -ForegroundColor DarkGray
    } else {
        Warn "search returned 0 results (embedding index may still be warming)"
    }
} catch {
    Fail "search failed: $_"
}

# ---------------------------------------------------------------------------
# Step 5: Verify via status endpoint
# ---------------------------------------------------------------------------
Step "5. Verify daemon status"
try {
    $st = Invoke-RestMethod "$BASE/api/status" -Method GET -Headers (AdminHdr) -TimeoutSec 5
    Pass "version: $($st.version)"
    Pass "queue depth: $($st.queue_depth)"
    if ($st.destinations) {
        foreach ($d in $st.destinations) {
            Pass "destination '$($d.name)' -- status: $($d.status)"
        }
    }
} catch {
    Warn "status endpoint unreachable (admin key may be required)"
}

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "=== Summary ===" -ForegroundColor Cyan
Write-Host "  Elapsed : $(Elapsed)"
Write-Host "  Passed  : $($script:passes)" -ForegroundColor Green
if ($script:failures -gt 0) {
    Write-Host "  Failed  : $($script:failures)" -ForegroundColor Red
    exit 1
} else {
    Write-Host "  All checks passed" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Next steps:" -ForegroundColor White
    Write-Host "    nexus tui           -- terminal dashboard" -ForegroundColor DarkGray
    Write-Host "    nexus doctor        -- health diagnostics" -ForegroundColor DarkGray
    Write-Host "    nexus audit tail    -- watch the audit chain" -ForegroundColor DarkGray
    Write-Host "    https://bubblefish.sh/docs  -- full documentation" -ForegroundColor DarkGray
    Write-Host ""
    exit 0
}
