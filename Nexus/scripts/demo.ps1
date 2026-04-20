# BubbleFish Nexus — 60-Second Show-Off Demo (PowerShell)
#
# Demonstrates core Nexus capabilities in under 60 seconds:
#   1. Daemon health check
#   2. Write 3 memories from different sources
#   3. Search memories
#   4. Fetch + verify cryptographic proof (browser-verifiable HTML)
#   5. Open memory graph dashboard
#   6. Print summary
#
# Prerequisites:
#   - bubblefish.exe built and on PATH  (or in current directory)
#   - Nexus daemon running  (run `bubblefish start` first)
#   - $env:NEXUS_API_KEY   set to a data-plane API key
#   - $env:NEXUS_ADMIN_KEY set to the admin token
#
# Run from D:\BubbleFish\Nexus:
#   .\scripts\demo.ps1

$ErrorActionPreference = "Continue"
$script:startTime = Get-Date
$script:failures  = 0
$script:passes    = 0

$BASE       = if ($env:NEXUS_URL)       { $env:NEXUS_URL }       else { "http://localhost:8080" }
$DASH_BASE  = if ($env:NEXUS_DASH_URL)  { $env:NEXUS_DASH_URL }  else { "http://localhost:8081" }
$API_KEY    = if ($env:NEXUS_API_KEY)   { $env:NEXUS_API_KEY }   else { "" }
$ADMIN_KEY  = if ($env:NEXUS_ADMIN_KEY) { $env:NEXUS_ADMIN_KEY } else { "" }

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

function AuthHdr { @{ "Authorization" = "Bearer $ADMIN_KEY" } }
function DataHdr { @{ "Authorization" = "Bearer $API_KEY"; "Content-Type" = "application/json" } }

# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "BubbleFish Nexus — Show-Off Demo" -ForegroundColor White
Write-Host "===================================" -ForegroundColor DarkGray

# Step 1: Health check
Step "1. Daemon health"
try {
    $h = Invoke-RestMethod "$BASE/health" -Method GET -TimeoutSec 5
    if ($h.status -eq "ok" -or $h.status -eq "degraded") {
        Pass "Daemon is up ($($h.status)) — $(Elapsed)"
    } else {
        Fail "Unexpected status: $($h.status)"
    }
} catch {
    Fail "Daemon unreachable at $BASE — is `bubblefish start` running?"
    Write-Host "  Hint: run 'bubblefish start' in another terminal, then retry." -ForegroundColor DarkYellow
    exit 1
}

# Step 2: Write 3 memories
Step "2. Write memories"
$source   = "demo-nexus"
$memories = @(
    @{ subject = "nexus-demo-1"; content = "BubbleFish Nexus stores AI memories with cryptographic provenance." }
    @{ subject = "nexus-demo-2"; content = "Every write is WAL-durability-first: the journal survives crashes before the DB." }
    @{ subject = "nexus-demo-3"; content = "The substrate BF-Sketch provides forward-secure deletion proofs." }
)
$writtenID = $null
foreach ($m in $memories) {
    $body = @{
        subject         = $m.subject
        content         = $m.content
        source          = $source
        destination     = "sqlite"
        idempotency_key = "demo-$($m.subject)-v1"
    } | ConvertTo-Json -Compress
    try {
        $r = Invoke-RestMethod "$BASE/inbound/$source" -Method POST -Body $body -Headers (DataHdr) -TimeoutSec 10
        if ($null -eq $writtenID) { $writtenID = $r.payload_id }
        Pass "Written: $($m.subject) → $($r.payload_id)"
    } catch {
        Fail "Write failed for $($m.subject): $_"
    }
}

# Step 3: Search
Step "3. Search memories"
try {
    $q = Invoke-RestMethod "$BASE/query/sqlite?q=WAL-durability&limit=5" -Method GET -Headers (DataHdr) -TimeoutSec 10
    $count = if ($q.records) { $q.records.Count } else { 0 }
    if ($count -gt 0) {
        Pass "Search returned $count result(s) — $(Elapsed)"
    } else {
        Warn "Search returned 0 results (index may still be warm)"
    }
} catch {
    Fail "Search failed: $_"
}

# Step 4: Fetch proof + generate browser-verifiable HTML
Step "4. Cryptographic proof"
$proofFile = "$env:TEMP\nexus-proof-demo.html"
if ($writtenID -and $ADMIN_KEY) {
    try {
        # Fetch proof bundle
        $proof = Invoke-RestMethod "$DASH_BASE/verify/$writtenID" -Method GET -Headers (AuthHdr) -TimeoutSec 10
        # Write raw JSON alongside
        $jsonFile = "$env:TEMP\nexus-proof-demo.json"
        $proof | ConvertTo-Json -Depth 20 | Out-File $jsonFile -Encoding utf8
        Pass "Proof bundle fetched for $writtenID — $(Elapsed)"
        # Generate HTML via CLI if available
        try {
            & bubblefish verify --proof $writtenID --url $DASH_BASE --token $ADMIN_KEY --output $proofFile 2>$null
            if (Test-Path $proofFile) {
                Pass "Browser-verifiable HTML written to $proofFile"
            }
        } catch {
            Warn "CLI HTML generation unavailable; raw proof JSON at $jsonFile"
        }
    } catch {
        Warn "Proof fetch failed (admin key required): $_"
    }
} elseif (-not $ADMIN_KEY) {
    Warn "No NEXUS_ADMIN_KEY — skipping proof verification"
} else {
    Warn "No memory ID from step 2 — skipping proof"
}

# Step 5: Open memory graph dashboard
Step "5. Memory graph dashboard"
if ($ADMIN_KEY) {
    $graphURL = "$DASH_BASE/dashboard/memgraph?token=$ADMIN_KEY"
    try {
        $r = Invoke-WebRequest $graphURL -TimeoutSec 5 -UseBasicParsing
        if ($r.StatusCode -eq 200) {
            Pass "Memory graph page OK — $graphURL"
            try {
                Start-Process $graphURL
                Pass "Opened in browser"
            } catch {
                Warn "Could not auto-open browser"
            }
        } else {
            Warn "Memory graph returned $($r.StatusCode)"
        }
    } catch {
        Warn "Memory graph unavailable: $_"
    }
    if ($proofFile -and (Test-Path $proofFile)) {
        try {
            Start-Process $proofFile
            Pass "Opened proof HTML in browser"
        } catch {}
    }
} else {
    Warn "No NEXUS_ADMIN_KEY — skipping dashboard open"
}

# Summary
Write-Host ""
Write-Host "=== Summary ===" -ForegroundColor Cyan
Write-Host "  Elapsed : $(Elapsed)"
Write-Host "  Passed  : $($script:passes)" -ForegroundColor Green
if ($script:failures -gt 0) {
    Write-Host "  Failed  : $($script:failures)" -ForegroundColor Red
    exit 1
} else {
    Write-Host "  All checks passed" -ForegroundColor Green
    exit 0
}
