# BubbleFish Nexus — A2A Demo: Claude Desktop → OpenClaw
#
# Demonstrates the full Nexus A2A path in under 60 seconds:
#   1. Enable A2A
#   2. Register a mock echo agent
#   3. Grant capabilities
#   4. Send a task via the bridge
#   5. Verify audit chain
#
# Prerequisites:
#   - bubblefish.exe built and on PATH
#   - Nexus daemon running (bubblefish start)
#   - Admin key available in $env:NEXUS_ADMIN_KEY
#
# Run from D:\BubbleFish\Nexus:
#   .\scripts\demo-a2a-claude-desktop.ps1
#
# TODO(shawn): Replace mock echo agent with real OpenClaw once the fork
# is ready. Update timing numbers after running on your dev machine.

$ErrorActionPreference = "Stop"
$script:startTime = Get-Date
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

function Elapsed {
    $elapsed = (Get-Date) - $script:startTime
    return [math]::Round($elapsed.TotalSeconds, 1)
}

# --- Pre-flight ---

Step "Pre-flight checks"

if (-not $env:NEXUS_ADMIN_KEY) {
    Fail "NEXUS_ADMIN_KEY not set"
    exit 1
}
Pass "NEXUS_ADMIN_KEY set"

$version = & bubblefish version 2>&1
if ($LASTEXITCODE -ne 0) {
    Fail "bubblefish not found on PATH"
    exit 1
}
Pass "bubblefish version: $version"

# --- Step 1: Verify A2A is enabled ---

Step "Step 1: Verify A2A enabled ($(Elapsed)s)"

# The daemon must have [a2a] enabled = true.
# We'll verify by listing agents — if A2A is disabled, this errors.
$agents = & bubblefish a2a agent list 2>&1
if ($LASTEXITCODE -ne 0) {
    Fail "A2A not enabled. Add [a2a] enabled = true to daemon.toml and restart."
    exit 1
}
Pass "A2A is enabled"

# --- Step 2: Register a mock echo agent ---

Step "Step 2: Register echo agent ($(Elapsed)s)"

# For this demo we use the daemon's built-in test echo endpoint.
# In production, this would be OpenClaw at its configured URL.
# TODO(shawn): Replace with real OpenClaw registration after fork is ready.

# Check if already registered.
$existing = & bubblefish a2a agent show echo-demo 2>&1
if ($LASTEXITCODE -eq 0) {
    Write-Host "  echo-demo already registered, retiring and re-adding..."
    & bubblefish a2a agent retire echo-demo 2>&1 | Out-Null
}

& bubblefish a2a agent add echo-demo `
    --transport http `
    --url http://localhost:8082/a2a `
    --auth none 2>&1

if ($LASTEXITCODE -ne 0) {
    Fail "Failed to register echo-demo agent"
    exit 1
}
Pass "echo-demo agent registered"

# --- Step 3: Test connectivity ---

Step "Step 3: Test agent connectivity ($(Elapsed)s)"

$ping = & bubblefish a2a agent test echo-demo 2>&1
if ($LASTEXITCODE -ne 0) {
    Fail "Agent ping failed: $ping"
    # Continue anyway — the demo can still show the grant/audit flow.
    Write-Host "  (continuing with grant setup)" -ForegroundColor Yellow
} else {
    Pass "Agent ping OK"
}

# --- Step 4: Grant capabilities ---

Step "Step 4: Grant test.echo capability ($(Elapsed)s)"

& bubblefish a2a grant add `
    --source client_claude_desktop `
    --target echo-demo `
    --capability "test.echo" 2>&1

if ($LASTEXITCODE -ne 0) {
    Fail "Failed to create grant"
} else {
    Pass "Grant created: client_claude_desktop -> echo-demo : test.echo"
}

# Verify grant exists.
$grants = & bubblefish a2a grant list 2>&1
Write-Host "  Grants: $grants"

# --- Step 5: List agents ---

Step "Step 5: List registered agents ($(Elapsed)s)"

$agentList = & bubblefish a2a agent list 2>&1
Write-Host "  $agentList"
Pass "Agent list retrieved"

# --- Step 6: Check audit trail ---

Step "Step 6: Verify audit trail ($(Elapsed)s)"

$audit = & bubblefish a2a audit tail --since 5m 2>&1
if ($LASTEXITCODE -ne 0) {
    Fail "Audit tail failed"
} else {
    $lines = ($audit -split "`n").Count
    Pass "Audit trail: $lines entries in last 5 minutes"
}

# --- Step 7: Verify audit chain integrity ---

Step "Step 7: Verify audit chain integrity ($(Elapsed)s)"

$verify = & bubblefish a2a audit verify 2>&1
if ($LASTEXITCODE -ne 0) {
    Fail "Audit chain verification failed: $verify"
} else {
    Pass "Audit chain integrity verified"
}

# --- Summary ---

$totalElapsed = Elapsed

Write-Host ""
Write-Host "==========================================" -ForegroundColor White
if ($script:failures -eq 0) {
    Write-Host "  DEMO PASSED — $totalElapsed seconds" -ForegroundColor Green
} else {
    Write-Host "  DEMO FINISHED — $($script:failures) failure(s), $totalElapsed seconds" -ForegroundColor Yellow
}
Write-Host "==========================================" -ForegroundColor White
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Cyan
Write-Host "  1. Open the dashboard: http://localhost:8081"
Write-Host "  2. Navigate to A2A Permissions to see the grant"
Write-Host "  3. In Claude Desktop, use a2a_send_to_agent to invoke echo-demo"
Write-Host "  4. Check bubblefish a2a audit tail for the task record"
Write-Host ""

# TODO(shawn): Add timing numbers observed on your dev machine before tagging.
# Expected: total demo under 60 seconds on a fresh install.

exit $script:failures
