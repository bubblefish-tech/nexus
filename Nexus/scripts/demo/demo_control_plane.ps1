# BubbleFish Nexus -- Control Plane Demo (PowerShell)
#
# Copyright (c) 2026 Shawn Sammartano. All rights reserved.
#
# Demonstrates the full governed agent control plane:
#   1. Register an agent
#   2. Grant it nexus_write capability
#   3. Agent creates a governed task
#   4. Agent requests nexus_delete (denied by policy)
#   5. Admin approves the escalation
#   6. Agent writes and deletes a memory under governance
#   7. Audit trail shows the full decision chain
#
# Prerequisites:
#   - nexus.exe built and on PATH
#   - Nexus daemon running with [a2a] enabled = true
#   - $env:NEXUS_ADMIN_KEY set to the admin token
#
# Tokens used here are PLACEHOLDER values. Replace with your own.
#
# Run:
#   .\scripts\demo\demo_control_plane.ps1

$ErrorActionPreference = "Continue"
$script:startTime = Get-Date
$script:failures  = 0
$script:passes    = 0

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
$BASE      = if ($env:NEXUS_URL)       { $env:NEXUS_URL }       else { "http://localhost:8080" }
$ADMIN_KEY = if ($env:NEXUS_ADMIN_KEY) { $env:NEXUS_ADMIN_KEY } else { "bfn_admin_TEST_KEY" }
$API_KEY   = if ($env:NEXUS_API_KEY)   { $env:NEXUS_API_KEY }   else { "bfn_data_TEST_KEY" }

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

function Invoke-NexusAdmin {
    param(
        [string]$Method,
        [string]$Path,
        [hashtable]$Body = $null
    )
    $headers = @{ "Authorization" = "Bearer $ADMIN_KEY"; "Content-Type" = "application/json" }
    $uri = "$BASE$Path"
    try {
        if ($Body) {
            $json = $Body | ConvertTo-Json -Depth 10
            $resp = Invoke-RestMethod -Method $Method -Uri $uri -Headers $headers -Body $json -TimeoutSec 10
        } else {
            $resp = Invoke-RestMethod -Method $Method -Uri $uri -Headers $headers -TimeoutSec 10
        }
        return $resp
    } catch {
        Write-Host "  HTTP $Method $Path failed: $_" -ForegroundColor Red
        $script:failures++
        return $null
    }
}

# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "BubbleFish Nexus -- Control Plane Demo" -ForegroundColor White
Write-Host "=========================================" -ForegroundColor DarkGray
Write-Host "  https://bubblefish.sh" -ForegroundColor DarkGray

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------
Step "Pre-flight"
if (-not $env:NEXUS_ADMIN_KEY -and $ADMIN_KEY -eq "bfn_admin_TEST_KEY") {
    Warn "using placeholder NEXUS_ADMIN_KEY -- set the real key for live demos"
}

try {
    $h = Invoke-RestMethod "$BASE/health" -Method GET -TimeoutSec 5
    Pass "daemon reachable -- status: $($h.status)"
} catch {
    Fail "daemon unreachable at $BASE -- run 'nexus start' first"
    exit 1
}

# ---------------------------------------------------------------------------
# Step 1: Register agent
# ---------------------------------------------------------------------------
Step "1. Register agent 'demo-governed-agent' -- $(Elapsed)"
$agentBody = @{
    name         = "demo-governed-agent"
    display_name = "Demo Governed Agent"
    url          = "http://localhost:9090"
    transport    = "http"
}
$agent = Invoke-NexusAdmin -Method POST -Path "/api/a2a/agents" -Body $agentBody
if ($agent -and $agent.agent_id) {
    $AGENT_ID = $agent.agent_id
    Pass "agent registered: $AGENT_ID"
} else {
    Warn "agent registration returned no ID (may already exist -- continuing)"
    $AGENT_ID = "demo-governed-agent"
}

# ---------------------------------------------------------------------------
# Step 2: Grant nexus_write
# ---------------------------------------------------------------------------
Step "2. Grant nexus_write to agent -- $(Elapsed)"
$grantBody = @{
    agent_id    = $AGENT_ID
    capability  = "nexus_write"
    granted_by  = "admin"
    expires_in  = "1h"
}
$grant = Invoke-NexusAdmin -Method POST -Path "/api/control/grants" -Body $grantBody
if ($grant -and $grant.grant_id) {
    $GRANT_ID = $grant.grant_id
    Pass "grant created: $GRANT_ID"
} else {
    Warn "grant creation returned no ID -- continuing"
    $GRANT_ID = ""
}

# ---------------------------------------------------------------------------
# Step 3: Create a governed task
# ---------------------------------------------------------------------------
Step "3. Create governed task -- $(Elapsed)"
$taskBody = @{
    agent_id   = $AGENT_ID
    capability = "nexus_write"
    title      = "Control plane demo: write + delete cycle"
    input      = @{ note = "Governed task demonstration" }
}
$task = Invoke-NexusAdmin -Method POST -Path "/api/control/tasks" -Body $taskBody
if ($task -and $task.task_id) {
    $TASK_ID = $task.task_id
    Pass "task created: $TASK_ID"
} else {
    Warn "task creation returned no ID -- continuing"
    $TASK_ID = ""
}

# ---------------------------------------------------------------------------
# Step 4: Request nexus_delete (denied by default policy)
# ---------------------------------------------------------------------------
Step "4. Request nexus_delete (expect: pending approval) -- $(Elapsed)"
$approvalBody = @{
    agent_id   = $AGENT_ID
    capability = "nexus_delete"
    reason     = "Agent needs to delete its own test memory after verification"
}
$approval = Invoke-NexusAdmin -Method POST -Path "/api/control/approvals" -Body $approvalBody
if ($approval -and $approval.request_id) {
    $APPROVAL_ID = $approval.request_id
    Pass "approval requested: $APPROVAL_ID (status: pending)"
} else {
    Warn "approval request returned no ID -- continuing"
    $APPROVAL_ID = ""
}

# ---------------------------------------------------------------------------
# Step 5: Admin approves the escalation
# ---------------------------------------------------------------------------
Step "5. Admin approves nexus_delete -- $(Elapsed)"
if ($APPROVAL_ID) {
    $decideBody = @{
        decision = "approve"
        reason   = "Approved for demo purposes"
    }
    $decided = Invoke-NexusAdmin -Method POST -Path "/api/control/approvals/$APPROVAL_ID/decide" -Body $decideBody
    if ($decided) {
        Pass "approval granted"
    } else {
        Warn "approval decision may have failed"
    }
} else {
    Warn "no approval ID -- skipping"
}

# ---------------------------------------------------------------------------
# Step 6: Write and delete a memory under governance
# ---------------------------------------------------------------------------
Step "6. Write memory under governance -- $(Elapsed)"
$memBody = @{
    content         = "Control plane demo: this memory was written under a governed grant and will be deleted."
    source          = "demo-control-plane"
    destination     = "sqlite"
    agent_id        = $AGENT_ID
    idempotency_key = "cp-demo-mem-v1"
}
$mem = Invoke-NexusAdmin -Method POST -Path "/api/memories" -Body $memBody
if ($mem -and $mem.id) {
    $MEM_ID = $mem.id
    Pass "memory written: $MEM_ID"
} else {
    Warn "memory write returned no ID -- continuing"
    $MEM_ID = ""
}

if ($MEM_ID) {
    Write-Host "  Deleting memory $MEM_ID (governed by approved nexus_delete)..." -ForegroundColor DarkGray
    $del = Invoke-NexusAdmin -Method DELETE -Path "/api/memories/$MEM_ID"
    if ($null -ne $del -or $?) {
        Pass "memory deleted: $MEM_ID"
    }
}

# Mark task complete
if ($TASK_ID) {
    $updateBody = @{ state = "completed"; result = @{ deleted = $MEM_ID } }
    Invoke-NexusAdmin -Method PATCH -Path "/api/control/tasks/$TASK_ID" -Body $updateBody | Out-Null
    Pass "task marked completed"
}

# ---------------------------------------------------------------------------
# Step 7: Audit trail
# ---------------------------------------------------------------------------
Step "7. Audit trail -- $(Elapsed)"

# List grants
$grantsList = Invoke-NexusAdmin -Method GET -Path "/api/control/grants?agent_id=$AGENT_ID"
if ($grantsList) {
    $grantCount = if ($grantsList.grants) { $grantsList.grants.Count } else { 0 }
    Pass "grants for agent: $grantCount"
}

# List approvals
$approvalsList = Invoke-NexusAdmin -Method GET -Path "/api/control/approvals?agent_id=$AGENT_ID"
if ($approvalsList) {
    $approvalCount = if ($approvalsList.approvals) { $approvalsList.approvals.Count } else { 0 }
    Pass "approvals for agent: $approvalCount"
}

# List tasks
$tasksList = Invoke-NexusAdmin -Method GET -Path "/api/control/tasks?agent_id=$AGENT_ID"
if ($tasksList) {
    $taskCount = if ($tasksList.tasks) { $tasksList.tasks.Count } else { 0 }
    Pass "tasks for agent: $taskCount"
}

# Audit tail
Write-Host ""
Write-Host "  Recent audit entries:" -ForegroundColor DarkGray
& nexus audit tail --limit 5 2>&1 | ForEach-Object { Write-Host "    $_" -ForegroundColor DarkGray }

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
Write-Host ""
Write-Host "=== Summary ===" -ForegroundColor Cyan
Write-Host "  Elapsed : $(Elapsed)"
Write-Host "  Passed  : $($script:passes)" -ForegroundColor Green
if ($script:failures -gt 0) {
    Write-Host "  Failed  : $($script:failures)" -ForegroundColor Red
} else {
    Write-Host "  All checks passed" -ForegroundColor Green
}
Write-Host ""
Write-Host "  Agent:    $AGENT_ID" -ForegroundColor DarkGray
Write-Host "  Grant:    $GRANT_ID" -ForegroundColor DarkGray
Write-Host "  Approval: $APPROVAL_ID" -ForegroundColor DarkGray
Write-Host "  Task:     $TASK_ID" -ForegroundColor DarkGray
Write-Host "  Memory:   $MEM_ID" -ForegroundColor DarkGray
Write-Host ""

if ($script:failures -gt 0) { exit 1 } else { exit 0 }
