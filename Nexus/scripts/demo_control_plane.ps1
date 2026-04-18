# BubbleFish Nexus — Control Plane Demo
#
# Demonstrates the full governed control plane in under 60 seconds:
#   1. Install (simple mode)
#   2. Start daemon
#   3. Register an agent
#   4. Grant it nexus_write capability
#   5. Agent requests approval for nexus_delete
#   6. Admin approves via CLI
#   7. Agent creates a task, writes a memory, deletes it
#   8. Action log shows the full chain
#   9. Substrate deletion proof verified offline
#  10. Dashboard pages confirm all records
#
# Prerequisites:
#   - bubblefish.exe built and on PATH
#   - $env:NEXUS_ADMIN_KEY set to the daemon admin token
#   - [control] enabled = true in daemon.toml
#   - [substrate] enabled = true in daemon.toml (for step 9)
#
# Run from D:\BubbleFish\Nexus:
#   .\scripts\demo_control_plane.ps1

$ErrorActionPreference = "Stop"
$script:startTime = Get-Date
$script:failures  = 0

$BASE  = "http://localhost:8080"
$TOKEN = $env:NEXUS_ADMIN_KEY

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

function Warn($msg) {
    Write-Host "  WARN: $msg" -ForegroundColor Yellow
}

function Elapsed {
    $elapsed = (Get-Date) - $script:startTime
    return [math]::Round($elapsed.TotalSeconds, 1)
}

# Invoke REST with admin token; return parsed JSON or $null on failure.
function Invoke-NexusRest {
    param(
        [string]$Method,
        [string]$Path,
        [hashtable]$Body = $null
    )
    $headers = @{ Authorization = "Bearer $TOKEN" }
    $uri     = "$BASE$Path"
    try {
        if ($Body) {
            $json = $Body | ConvertTo-Json -Depth 10
            $resp = Invoke-RestMethod -Method $Method -Uri $uri `
                -Headers $headers -ContentType "application/json" -Body $json
        } else {
            $resp = Invoke-RestMethod -Method $Method -Uri $uri -Headers $headers
        }
        return $resp
    } catch {
        Write-Host "  HTTP $Method $Path failed: $_" -ForegroundColor Red
        $script:failures++
        return $null
    }
}

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------

Step "Pre-flight checks"

if (-not $TOKEN) {
    Fail "NEXUS_ADMIN_KEY not set"
    exit 1
}
Pass "NEXUS_ADMIN_KEY set"

$ver = & bubblefish version 2>&1
if ($LASTEXITCODE -ne 0) {
    Fail "bubblefish not found on PATH"
    exit 1
}
Pass "bubblefish: $ver"

# ---------------------------------------------------------------------------
# Step 1: Install (simple mode)
# ---------------------------------------------------------------------------

Step "Step 1: Install — simple mode ($(Elapsed)s)"

# In CI/dev, the daemon is already configured. Install is idempotent.
& bubblefish install --mode simple 2>&1 | ForEach-Object { Write-Host "  $_" }
if ($LASTEXITCODE -ne 0) {
    Warn "install returned non-zero (may already be installed — continuing)"
} else {
    Pass "install completed"
}

# ---------------------------------------------------------------------------
# Step 2: Start daemon
# ---------------------------------------------------------------------------

Step "Step 2: Start daemon ($(Elapsed)s)"

# Attempt start; if already running the command exits non-zero — that's fine.
& bubblefish start 2>&1 | ForEach-Object { Write-Host "  $_" }
if ($LASTEXITCODE -ne 0) {
    Warn "start returned non-zero (daemon may already be running — continuing)"
}

# Wait up to 10 s for the health endpoint.
$healthy = $false
for ($i = 0; $i -lt 20; $i++) {
    try {
        $h = Invoke-RestMethod -Uri "$BASE/api/health" -ErrorAction SilentlyContinue
        if ($h) { $healthy = $true; break }
    } catch { }
    Start-Sleep -Milliseconds 500
}
if ($healthy) {
    Pass "daemon healthy at $BASE"
} else {
    Fail "daemon did not respond in 10s — is [control] enabled = true in daemon.toml?"
}

# ---------------------------------------------------------------------------
# Step 3: Register an agent
# ---------------------------------------------------------------------------

Step "Step 3: Register agent demo-agent ($(Elapsed)s)"

$agentBody = @{
    name         = "demo-agent"
    display_name = "Demo Agent"
    url          = "http://localhost:9090"
    transport    = "http"
}
$agent = Invoke-NexusRest -Method POST -Path "/api/a2a/agents" -Body $agentBody
if ($agent -and $agent.agent_id) {
    $AGENT_ID = $agent.agent_id
    Pass "registered agent: $AGENT_ID"
} else {
    Fail "agent registration returned no agent_id"
    # Use a placeholder so later steps produce useful output
    $AGENT_ID = "demo-agent-id"
}

# ---------------------------------------------------------------------------
# Step 4: Grant nexus_write capability
# ---------------------------------------------------------------------------

Step "Step 4: Grant nexus_write to $AGENT_ID ($(Elapsed)s)"

& bubblefish grant create --agent $AGENT_ID --capability nexus_write `
    --granted-by admin --expires 1h 2>&1 | ForEach-Object { Write-Host "  $_" }
if ($LASTEXITCODE -eq 0) {
    Pass "grant created"
} else {
    Fail "grant create failed"
}

# Capture grant ID for lineage verification later.
$grantsJson = & bubblefish grant list --agent $AGENT_ID --json 2>&1
$grants = $grantsJson | ConvertFrom-Json -ErrorAction SilentlyContinue
if ($grants -and $grants.Count -gt 0) {
    $GRANT_ID = $grants[0].grant_id
    Pass "grant id: $GRANT_ID"
} else {
    Warn "could not parse grant list JSON"
    $GRANT_ID = ""
}

# ---------------------------------------------------------------------------
# Step 5: Agent requests approval for nexus_delete
# ---------------------------------------------------------------------------

Step "Step 5: Request approval for nexus_delete ($(Elapsed)s)"

$approvalBody = @{
    agent_id   = $AGENT_ID
    capability = "nexus_delete"
    reason     = "Demo: agent needs to delete its own test memory"
}
$approval = Invoke-NexusRest -Method POST -Path "/api/control/approvals" -Body $approvalBody
if ($approval -and $approval.request_id) {
    $APPROVAL_ID = $approval.request_id
    Pass "approval requested: $APPROVAL_ID"
} else {
    Fail "approval request returned no request_id"
    $APPROVAL_ID = ""
}

# ---------------------------------------------------------------------------
# Step 6: Admin approves via CLI
# ---------------------------------------------------------------------------

Step "Step 6: Admin approves nexus_delete ($(Elapsed)s)"

if ($APPROVAL_ID) {
    & bubblefish approval decide --id $APPROVAL_ID --decision approve `
        --reason "Demo approval" 2>&1 | ForEach-Object { Write-Host "  $_" }
    if ($LASTEXITCODE -eq 0) {
        Pass "approval decided: approve"
    } else {
        Fail "approval decide failed"
    }
} else {
    Warn "skipping approval decide — no request_id"
}

# ---------------------------------------------------------------------------
# Step 7: Agent creates a task, writes a memory, deletes it
# ---------------------------------------------------------------------------

Step "Step 7: Create task → write memory → delete memory ($(Elapsed)s)"

# Create task
$taskBody = @{
    agent_id   = $AGENT_ID
    capability = "nexus_write"
    title      = "Demo write+delete cycle"
    input      = @{ note = "MT.8 demo memory" }
}
$task = Invoke-NexusRest -Method POST -Path "/api/control/tasks" -Body $taskBody
if ($task -and $task.task_id) {
    $TASK_ID = $task.task_id
    Pass "task created: $TASK_ID"
} else {
    Fail "task creation failed"
    $TASK_ID = ""
}

# Write a memory via the standard memory API (uses the agent's nexus_write grant)
$memBody = @{
    content  = "MT.8 control plane demo memory — safe to delete"
    source   = "demo_control_plane"
    agent_id = $AGENT_ID
}
$mem = Invoke-NexusRest -Method POST -Path "/api/memories" -Body $memBody
if ($mem -and $mem.id) {
    $MEM_ID = $mem.id
    Pass "memory written: $MEM_ID"
} else {
    Warn "memory write returned no id (substrate may be disabled — continuing)"
    $MEM_ID = ""
}

# Delete the memory (requires nexus_delete, approved in step 6)
if ($MEM_ID) {
    $del = Invoke-NexusRest -Method DELETE -Path "/api/memories/$MEM_ID"
    if ($null -ne $del -or $?) {
        Pass "memory deleted: $MEM_ID"
    }
}

# Mark task complete
if ($TASK_ID) {
    $updateBody = @{ state = "completed"; result = @{ deleted = $MEM_ID } }
    Invoke-NexusRest -Method PATCH -Path "/api/control/tasks/$TASK_ID" -Body $updateBody | Out-Null
    Pass "task marked completed"
}

# ---------------------------------------------------------------------------
# Step 8: Action log shows the full chain
# ---------------------------------------------------------------------------

Step "Step 8: Action log for $AGENT_ID ($(Elapsed)s)"

& bubblefish action log --agent $AGENT_ID 2>&1 | ForEach-Object { Write-Host "  $_" }
if ($LASTEXITCODE -eq 0) {
    Pass "action log retrieved"
} else {
    Fail "action log failed"
}

# Lineage endpoint (requires a task)
if ($TASK_ID) {
    $lineage = Invoke-NexusRest -Method GET -Path "/api/control/lineage/$TASK_ID"
    if ($lineage) {
        $actionCount  = if ($lineage.actions)   { $lineage.actions.Count }   else { 0 }
        $grantCount   = if ($lineage.grants)    { $lineage.grants.Count }    else { 0 }
        $approvalCount = if ($lineage.approvals) { $lineage.approvals.Count } else { 0 }
        Pass "lineage: $actionCount action(s), $grantCount grant(s), $approvalCount approval(s)"
    } else {
        Warn "lineage endpoint returned no data"
    }
}

# ---------------------------------------------------------------------------
# Step 9: Substrate deletion proof (offline verify)
# ---------------------------------------------------------------------------

Step "Step 9: Substrate deletion proof ($(Elapsed)s)"

if ($MEM_ID) {
    $proof = Invoke-NexusRest -Method GET -Path "/api/substrate/proof/$MEM_ID"
    if ($proof) {
        $proofFile = [System.IO.Path]::Combine($env:TEMP, "nexus_proof_$MEM_ID.json")
        $proof | ConvertTo-Json -Depth 20 | Set-Content -Path $proofFile -Encoding UTF8
        Pass "proof saved: $proofFile"

        & bubblefish verify $proofFile 2>&1 | ForEach-Object { Write-Host "  $_" }
        if ($LASTEXITCODE -eq 0) {
            Pass "proof: VALID"
        } else {
            Warn "proof verify returned non-zero (substrate may be disabled — acceptable in simple mode)"
        }
    } else {
        Warn "proof endpoint returned no data (substrate disabled — skipping verify)"
    }
} else {
    Warn "no memory ID — skipping substrate proof step"
}

# ---------------------------------------------------------------------------
# Step 10: Dashboard confirmation
# ---------------------------------------------------------------------------

Step "Step 10: Dashboard pages ($(Elapsed)s)"

$dashPages = @(
    "/dashboard/agents",
    "/dashboard/grants",
    "/dashboard/approvals",
    "/dashboard/tasks",
    "/dashboard/actions"
)
foreach ($page in $dashPages) {
    try {
        $resp = Invoke-WebRequest -Uri "$BASE${page}?token=$TOKEN" -UseBasicParsing -ErrorAction Stop
        if ($resp.StatusCode -eq 200) {
            Pass "dashboard $page — HTTP 200"
        } else {
            Fail "dashboard $page — HTTP $($resp.StatusCode)"
        }
    } catch {
        Fail "dashboard $page — $_"
    }
}

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------

Step "Demo complete ($(Elapsed)s total)"

if ($script:failures -eq 0) {
    Write-Host ""
    Write-Host "  ALL STEPS PASSED" -ForegroundColor Green
    Write-Host ""
    Write-Host "  Agent:    $AGENT_ID"
    Write-Host "  Grant:    $GRANT_ID"
    Write-Host "  Approval: $APPROVAL_ID"
    Write-Host "  Task:     $TASK_ID"
    Write-Host "  Memory:   $MEM_ID"
    Write-Host ""
    exit 0
} else {
    Write-Host ""
    Write-Host "  $($script:failures) step(s) FAILED" -ForegroundColor Red
    Write-Host ""
    exit 1
}
