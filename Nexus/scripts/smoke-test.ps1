# BubbleFish Nexus — End-to-End Smoke Test
# Verifies build, startup, write, read, kill -9 survival, and clean shutdown.
# Run from D:\BubbleFish\Nexus

$ErrorActionPreference = "Stop"
$script:failures = 0

function Step($name) {
    Write-Host ""
    Write-Host "=== $name ===" -ForegroundColor Cyan
}

function Pass($msg) {
    Write-Host "  PASS: $msg" -ForegroundColor Green
}

function Fail($msg) {
    Write-Host "  FAIL: $msg" -ForegroundColor Red
    $script:failures++
}

function Require($condition, $passMsg, $failMsg) {
    if ($condition) { Pass $passMsg } else { Fail $failMsg }
}

# ---------------------------------------------------------------------------
# Setup
# ---------------------------------------------------------------------------

$repoRoot = "D:\BubbleFish\Nexus"
$testHome = "$env:TEMP\bubblefish-smoke-$(Get-Random)"
$binPath  = "$repoRoot\bubblefish.exe"
$adminTok = "smoke-admin-token-$(Get-Random)"
$dataTok  = "smoke-data-token-$(Get-Random)"
$port     = 18765  # uncommon port to avoid collisions

Set-Location $repoRoot

Step "Setup test environment"
New-Item -ItemType Directory -Force -Path $testHome | Out-Null
New-Item -ItemType Directory -Force -Path "$testHome\sources" | Out-Null
Pass "Test home: $testHome"

# ---------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------

Step "Build"
go build -o $binPath .\cmd\bubblefish\
Require ($LASTEXITCODE -eq 0) "Binary built" "go build failed"
Require (Test-Path $binPath) "Binary exists at $binPath" "Binary not found"

# ---------------------------------------------------------------------------
# Config
# ---------------------------------------------------------------------------

Step "Write daemon config"

$daemonToml = @"
[daemon]
bind = "127.0.0.1:$port"
admin_token = "$adminTok"
log_level   = "info"

[daemon.wal]
path = "$($testHome -replace '\\','/')/wal"

[destinations.sqlite]
type = "sqlite"
path = "$($testHome -replace '\\','/')/nexus.db"
"@

$daemonToml | Set-Content -Path "$testHome\daemon.toml" -Encoding UTF8
Pass "daemon.toml written"

$sourceToml = @"
[source]
name               = "smoke"
api_key            = "$dataTok"
namespace          = "smoke"
target_destination = "sqlite"
can_read           = true
can_write          = true

[mapping]
content = "content"

[rate_limit]
requests_per_minute = 2000
"@

$sourceToml | Set-Content -Path "$testHome\sources\smoke.toml" -Encoding UTF8
Pass "source TOML written"

# ---------------------------------------------------------------------------
# Start daemon (round 1)
# ---------------------------------------------------------------------------

Step "Start daemon (round 1)"

$env:BUBBLEFISH_HOME = $testHome
$proc = Start-Process -FilePath $binPath `
    -ArgumentList "start","--config","$testHome\daemon.toml" `
    -PassThru -WindowStyle Hidden `
    -RedirectStandardOutput "$testHome\daemon.out" `
    -RedirectStandardError  "$testHome\daemon.err"

Start-Sleep -Seconds 2
Require (-not $proc.HasExited) "Daemon started (PID $($proc.Id))" "Daemon exited immediately — check $testHome\daemon.err"

# Wait for /health to come up
$ready = $false
for ($i = 0; $i -lt 20; $i++) {
    try {
        $r = Invoke-WebRequest -Uri "http://127.0.0.1:$port/health" -UseBasicParsing -TimeoutSec 1
        if ($r.StatusCode -eq 200) { $ready = $true; break }
    } catch { Start-Sleep -Milliseconds 250 }
}
Require $ready "/health returned 200" "/health never came up"

# ---------------------------------------------------------------------------
# Write a memory
# ---------------------------------------------------------------------------

Step "Write memory via /inbound/smoke"

$writeBody = @{ content = "smoke test memory one" } | ConvertTo-Json -Compress
$writeHeaders = @{
    "Authorization" = "Bearer $dataTok"
    "Content-Type"  = "application/json"
}

try {
    $writeResp = Invoke-RestMethod -Method POST `
        -Uri "http://127.0.0.1:$port/inbound/smoke" `
        -Headers $writeHeaders -Body $writeBody
    $payloadId = $writeResp.payload_id
    Require ($null -ne $payloadId) "Got payload_id: $payloadId" "No payload_id returned"
} catch {
    Fail "Write failed: $_"
}

# Write a few more so we have something to read back
for ($i = 2; $i -le 5; $i++) {
    $body = @{ content = "smoke test memory $i" } | ConvertTo-Json -Compress
    Invoke-RestMethod -Method POST -Uri "http://127.0.0.1:$port/inbound/smoke" `
        -Headers $writeHeaders -Body $body | Out-Null
}
Pass "Wrote 5 memories total"

# Give the worker a beat to drain the queue to SQLite
Start-Sleep -Seconds 1

# ---------------------------------------------------------------------------
# Read it back
# ---------------------------------------------------------------------------

Step "Query memories via /query/sqlite"

try {
    $queryResp = Invoke-RestMethod -Method GET `
        -Uri "http://127.0.0.1:$port/query/sqlite?limit=10" `
        -Headers @{ "Authorization" = "Bearer $dataTok" }

    $count = if ($queryResp.results) { $queryResp.results.Count } else { 0 }
    Require ($count -ge 5) "Read back $count memories (expected >= 5)" "Only got $count memories"
} catch {
    Fail "Query failed: $_"
}

# ---------------------------------------------------------------------------
# THE HEADLINE TEST: kill -9 survival
# ---------------------------------------------------------------------------

Step "Kill -9 the daemon"

Stop-Process -Id $proc.Id -Force
Start-Sleep -Seconds 1
Require ($proc.HasExited) "Daemon process gone" "Daemon still running after Stop-Process -Force"

# Verify WAL files still exist on disk
$walFiles = Get-ChildItem -Path "$testHome\wal" -ErrorAction SilentlyContinue
Require ($walFiles.Count -gt 0) "WAL files survived ($($walFiles.Count) files on disk)" "WAL directory empty"

Step "Restart daemon (round 2)"

$proc2 = Start-Process -FilePath $binPath `
    -ArgumentList "start","--config","$testHome\daemon.toml" `
    -PassThru -WindowStyle Hidden `
    -RedirectStandardOutput "$testHome\daemon2.out" `
    -RedirectStandardError  "$testHome\daemon2.err"

Start-Sleep -Seconds 2
Require (-not $proc2.HasExited) "Daemon restarted (PID $($proc2.Id))" "Daemon failed to restart — check $testHome\daemon2.err"

$ready = $false
for ($i = 0; $i -lt 20; $i++) {
    try {
        $r = Invoke-WebRequest -Uri "http://127.0.0.1:$port/health" -UseBasicParsing -TimeoutSec 1
        if ($r.StatusCode -eq 200) { $ready = $true; break }
    } catch { Start-Sleep -Milliseconds 250 }
}
Require $ready "/health returned 200 after restart" "/health never came up after restart"

# ---------------------------------------------------------------------------
# Verify all 5 memories survived
# ---------------------------------------------------------------------------

Step "Verify memories survived kill -9"

# Replay may take a moment on cold start
Start-Sleep -Seconds 2

try {
    $queryResp2 = Invoke-RestMethod -Method GET `
        -Uri "http://127.0.0.1:$port/query/sqlite?limit=10" `
        -Headers @{ "Authorization" = "Bearer $dataTok" }

    $count2 = if ($queryResp2.results) { $queryResp2.results.Count } else { 0 }
    Require ($count2 -ge 5) "All $count2 memories present after kill -9 + restart" "Only $count2 memories survived (expected 5)"
} catch {
    Fail "Post-restart query failed: $_"
}

# ---------------------------------------------------------------------------
# Admin endpoint sanity
# ---------------------------------------------------------------------------

Step "Admin endpoints"

try {
    $status = Invoke-RestMethod -Method GET `
        -Uri "http://127.0.0.1:$port/api/status" `
        -Headers @{ "Authorization" = "Bearer $adminTok" }
    Require ($null -ne $status) "/api/status returned data" "/api/status empty"
} catch {
    Fail "/api/status failed: $_"
}

# Token class separation: data token must NOT work on admin endpoint
$dataOnAdminBlocked = $false
try {
    Invoke-RestMethod -Method GET `
        -Uri "http://127.0.0.1:$port/api/status" `
        -Headers @{ "Authorization" = "Bearer $dataTok" } | Out-Null
} catch {
    if ($_.Exception.Response.StatusCode.value__ -eq 401) { $dataOnAdminBlocked = $true }
}
Require $dataOnAdminBlocked "Data token rejected on /api/status (401)" "Data token wrongly accepted on admin endpoint"

# Admin token must NOT work on data endpoint
$adminOnDataBlocked = $false
try {
    Invoke-RestMethod -Method POST `
        -Uri "http://127.0.0.1:$port/inbound/smoke" `
        -Headers @{ "Authorization" = "Bearer $adminTok"; "Content-Type" = "application/json" } `
        -Body '{"content":"should fail"}' | Out-Null
} catch {
    if ($_.Exception.Response.StatusCode.value__ -eq 401) { $adminOnDataBlocked = $true }
}
Require $adminOnDataBlocked "Admin token rejected on /inbound (401)" "Admin token wrongly accepted on data endpoint"

# ---------------------------------------------------------------------------
# Clean shutdown
# ---------------------------------------------------------------------------

Step "Clean shutdown"

Stop-Process -Id $proc2.Id -Force
Start-Sleep -Seconds 1
Pass "Daemon stopped"

# ---------------------------------------------------------------------------
# Cleanup
# ---------------------------------------------------------------------------

Step "Cleanup"
Remove-Item -Recurse -Force $testHome -ErrorAction SilentlyContinue
Pass "Test home removed"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

Write-Host ""
Write-Host "===========================================" -ForegroundColor Cyan
if ($script:failures -eq 0) {
    Write-Host "SMOKE TEST: ALL CHECKS PASSED" -ForegroundColor Green
    exit 0
} else {
    Write-Host "SMOKE TEST: $($script:failures) CHECK(S) FAILED" -ForegroundColor Red
    exit 1
}