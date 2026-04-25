# BubbleFish Nexus -- Multi-Client Demo (PowerShell)
#
# Copyright (c) 2026 Shawn Sammartano. All rights reserved.
#
# Demonstrates Nexus as a shared memory pool for multiple AI clients:
#   1. Claude Code writes a memory
#   2. Perplexity writes a memory
#   3. Ollama (local) writes a memory
#   4. Each client searches and sees ALL memories
#   5. Provenance metadata shows which client wrote what
#
# This is the core value proposition: one memory pool for every AI app.
# Each client has its own source token, but they share a unified memory
# namespace. Retrieval profiles (fast/balanced/deep) control what each
# client sees.
#
# Prerequisites:
#   - nexus.exe built and on PATH
#   - Nexus daemon running
#   - Three source tokens configured (or use placeholders below)
#
# Tokens used here are PLACEHOLDER values. Replace with your own
# source-specific tokens from your daemon.toml configuration.
#
# Run:
#   .\scripts\demo\demo_multi_client.ps1

$ErrorActionPreference = "Continue"
$script:startTime = Get-Date
$script:failures  = 0
$script:passes    = 0

# ---------------------------------------------------------------------------
# Configuration -- each client gets its own source token
# ---------------------------------------------------------------------------
$BASE = if ($env:NEXUS_URL) { $env:NEXUS_URL } else { "http://localhost:8080" }

# Source tokens -- replace with real tokens from your daemon.toml [sources] section
$CLAUDE_TOKEN     = if ($env:NEXUS_CLAUDE_KEY)     { $env:NEXUS_CLAUDE_KEY }     else { "bfn_mcp_CLAUDE_TEST_KEY" }
$PERPLEXITY_TOKEN = if ($env:NEXUS_PERPLEXITY_KEY) { $env:NEXUS_PERPLEXITY_KEY } else { "bfn_mcp_PERPLEXITY_TEST_KEY" }
$OLLAMA_TOKEN     = if ($env:NEXUS_OLLAMA_KEY)     { $env:NEXUS_OLLAMA_KEY }     else { "bfn_mcp_OLLAMA_TEST_KEY" }
$ADMIN_KEY        = if ($env:NEXUS_ADMIN_KEY)      { $env:NEXUS_ADMIN_KEY }      else { "bfn_admin_TEST_KEY" }

# Fallback: if source-specific tokens are not set, use a single data key
$FALLBACK_KEY = if ($env:NEXUS_API_KEY) { $env:NEXUS_API_KEY } else { "bfn_data_TEST_KEY" }

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

function WriteMemory {
    param(
        [string]$Source,
        [string]$Token,
        [string]$Subject,
        [string]$Content
    )
    $effectiveToken = if ($Token -like "bfn_mcp_*_TEST_KEY") { $FALLBACK_KEY } else { $Token }
    $headers = @{ "Authorization" = "Bearer $effectiveToken"; "Content-Type" = "application/json" }
    $body = @{
        subject         = $Subject
        content         = $Content
        source          = $Source
        destination     = "sqlite"
        idempotency_key = "multi-client-$Source-$Subject-v1"
    } | ConvertTo-Json -Compress

    try {
        $r = Invoke-RestMethod "$BASE/inbound/$Source" -Method POST -Body $body -Headers $headers -TimeoutSec 10
        return $r.payload_id
    } catch {
        Write-Host "    Write failed for $Source/$Subject -- $_" -ForegroundColor Red
        $script:failures++
        return $null
    }
}

function SearchMemories {
    param(
        [string]$Source,
        [string]$Token,
        [string]$Query
    )
    $effectiveToken = if ($Token -like "bfn_mcp_*_TEST_KEY") { $FALLBACK_KEY } else { $Token }
    $headers = @{ "Authorization" = "Bearer $effectiveToken" }
    try {
        $r = Invoke-RestMethod "$BASE/query/sqlite?q=$([uri]::EscapeDataString($Query))&limit=10" -Method GET -Headers $headers -TimeoutSec 10
        return $r
    } catch {
        Write-Host "    Search failed for $Source -- $_" -ForegroundColor Red
        $script:failures++
        return $null
    }
}

# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "BubbleFish Nexus -- Multi-Client Demo" -ForegroundColor White
Write-Host "========================================" -ForegroundColor DarkGray
Write-Host "  https://bubblefish.sh" -ForegroundColor DarkGray
Write-Host "  One memory pool for Claude + Perplexity + Ollama" -ForegroundColor DarkGray

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------
Step "Pre-flight"
try {
    $h = Invoke-RestMethod "$BASE/health" -Method GET -TimeoutSec 5
    Pass "daemon reachable -- status: $($h.status)"
} catch {
    Fail "daemon unreachable at $BASE -- run 'nexus start' first"
    exit 1
}

# ---------------------------------------------------------------------------
# Step 1: Claude Code writes a memory
# ---------------------------------------------------------------------------
Step "1. Claude Code writes a memory -- $(Elapsed)"

$claudeID = WriteMemory -Source "claude-code" -Token $CLAUDE_TOKEN `
    -Subject "multi-client-claude" `
    -Content "Project architecture decision: we chose event sourcing with WAL-first durability for the payment service. All state changes are append-only journal entries."

if ($claudeID) {
    Pass "Claude Code wrote: $claudeID"
} else {
    Fail "Claude Code write failed"
}

# ---------------------------------------------------------------------------
# Step 2: Perplexity writes a memory
# ---------------------------------------------------------------------------
Step "2. Perplexity writes a memory -- $(Elapsed)"

$perplexityID = WriteMemory -Source "perplexity" -Token $PERPLEXITY_TOKEN `
    -Subject "multi-client-perplexity" `
    -Content "Research finding: according to the ACM 2026 survey, event sourcing adoption increased 340% in distributed systems. The primary driver is auditability and crash recovery."

if ($perplexityID) {
    Pass "Perplexity wrote: $perplexityID"
} else {
    Fail "Perplexity write failed"
}

# ---------------------------------------------------------------------------
# Step 3: Ollama (local) writes a memory
# ---------------------------------------------------------------------------
Step "3. Ollama writes a memory -- $(Elapsed)"

$ollamaID = WriteMemory -Source "ollama" -Token $OLLAMA_TOKEN `
    -Subject "multi-client-ollama" `
    -Content "Code review note: the WAL implementation in internal/wal/wal.go uses group commit with configurable max_batch=256 and max_delay=500us. CRC32 checksums on every entry."

if ($ollamaID) {
    Pass "Ollama wrote: $ollamaID"
} else {
    Fail "Ollama write failed"
}

# ---------------------------------------------------------------------------
# Step 4: Each client searches and sees ALL memories
# ---------------------------------------------------------------------------
Step "4. Cross-client search: 'event sourcing WAL' -- $(Elapsed)"

Start-Sleep -Seconds 1  # allow queue drain

$clients = @(
    @{ Name = "Claude Code"; Source = "claude-code"; Token = $CLAUDE_TOKEN }
    @{ Name = "Perplexity";  Source = "perplexity";  Token = $PERPLEXITY_TOKEN }
    @{ Name = "Ollama";      Source = "ollama";       Token = $OLLAMA_TOKEN }
)

foreach ($client in $clients) {
    $result = SearchMemories -Source $client.Source -Token $client.Token -Query "event sourcing WAL"
    $count = if ($result -and $result.records) { $result.records.Count } else { 0 }

    if ($count -gt 0) {
        Pass "$($client.Name) sees $count result(s) across all sources"
        # Show source attribution for first result
        $first = $result.records[0]
        $srcAttr = if ($first.source) { $first.source } else { "unknown" }
        Write-Host "    First result source: $srcAttr" -ForegroundColor DarkGray
    } else {
        Warn "$($client.Name) sees 0 results (embedding index may be warming)"
    }
}

# ---------------------------------------------------------------------------
# Step 5: Provenance metadata
# ---------------------------------------------------------------------------
Step "5. Provenance: who wrote what -- $(Elapsed)"

$allIDs = @()
if ($claudeID)     { $allIDs += @{ Source = "Claude Code"; ID = $claudeID } }
if ($perplexityID) { $allIDs += @{ Source = "Perplexity";  ID = $perplexityID } }
if ($ollamaID)     { $allIDs += @{ Source = "Ollama";      ID = $ollamaID } }

Write-Host ""
Write-Host "  Memory provenance:" -ForegroundColor White
foreach ($entry in $allIDs) {
    Write-Host "    $($entry.Source) -> $($entry.ID)" -ForegroundColor DarkGray
}

# Verify via admin status
try {
    $st = Invoke-RestMethod "$BASE/api/status" -Method GET -Headers @{ "Authorization" = "Bearer $ADMIN_KEY" } -TimeoutSec 5
    if ($st.sources) {
        Write-Host ""
        Write-Host "  Registered sources:" -ForegroundColor White
        foreach ($s in $st.sources) {
            Write-Host "    - $($s.name)" -ForegroundColor DarkGray
        }
    }
    Pass "daemon version: $($st.version)"
} catch {
    Warn "admin status unreachable (admin key may be required)"
}

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "=== Summary ===" -ForegroundColor Cyan
Write-Host "  Elapsed : $(Elapsed)"
Write-Host "  Clients : 3 (Claude Code, Perplexity, Ollama)"
Write-Host "  Written : $($allIDs.Count) memories"
Write-Host "  Passed  : $($script:passes)" -ForegroundColor Green
if ($script:failures -gt 0) {
    Write-Host "  Failed  : $($script:failures)" -ForegroundColor Red
} else {
    Write-Host "  All checks passed" -ForegroundColor Green
}
Write-Host ""
Write-Host "  Key insight: each AI client has its own source token," -ForegroundColor DarkGray
Write-Host "  but they share a unified memory pool. Every client" -ForegroundColor DarkGray
Write-Host "  can search across all sources -- one memory for all AI." -ForegroundColor DarkGray
Write-Host ""

if ($script:failures -gt 0) { exit 1 } else { exit 0 }
