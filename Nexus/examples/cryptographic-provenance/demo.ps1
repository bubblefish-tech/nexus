# BubbleFish Nexus - 60-Second Cryptographic Provenance Demo (Windows)
#
# This script demonstrates end-to-end cryptographic provenance.
# See demo.sh for the full documentation.
#
# Reference: v0.1.3 Build Plan Phase 4 Subtask 4.12.
#
# Copyright (c) 2026 BubbleFish Technologies, Inc.
# Licensed under the GNU Affero General Public License v3.0.

$ErrorActionPreference = "Stop"

$Daemon = "http://127.0.0.1:3080"
$AdminKey = if ($env:BFN_ADMIN_KEY) { $env:BFN_ADMIN_KEY } else { "bfn_admin_TEST_KEY" }
$AgentAKey = if ($env:AGENT_A_KEY) { $env:AGENT_A_KEY } else { "bfn_data_TEST_AGENT_A" }
$AgentBKey = if ($env:AGENT_B_KEY) { $env:AGENT_B_KEY } else { "bfn_data_TEST_AGENT_B" }

Write-Host "=== BubbleFish Nexus: 60-Second Cryptographic Provenance Demo ==="
Write-Host ""

# Step 1: Agent A writes a memory
Write-Host "[1/6] Agent A writes a signed memory..."
$writeResp = Invoke-RestMethod -Uri "$Daemon/inbound/agent-a" -Method POST `
    -Headers @{ Authorization = "Bearer $AgentAKey" } `
    -ContentType "application/json" `
    -Body '{"content":"The quarterly revenue forecast is $4.2M","subject":"finance/forecast","actor_type":"agent","actor_id":"agent-a-demo"}'
$payloadId = $writeResp.payload_id
Write-Host "  Written: payload_id=$payloadId"

# Step 2: Agent B reads
Write-Host "[2/6] Agent B reads the memory..."
$readResp = Invoke-RestMethod -Uri "$Daemon/query/sqlite?subject=finance/forecast&limit=1" `
    -Headers @{ Authorization = "Bearer $AgentBKey" }
Write-Host "  Read: ok"

# Step 3: Export proof
Write-Host "[3/6] Exporting proof bundle..."
$proofPath = "$env:TEMP\proof-bundle.json"
Invoke-RestMethod -Uri "$Daemon/verify/$payloadId" `
    -Headers @{ Authorization = "Bearer $AdminKey" } `
    -OutFile $proofPath
Write-Host "  Saved: $proofPath"

# Step 4: Verify with Go CLI
Write-Host "[4/6] Verifying with Go CLI..."
& bubblefish.exe verify $proofPath

# Step 5: Verify with Python
Write-Host "[5/6] Verifying with Python..."
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
& python3 "$scriptDir\..\..\tools\verify-python\verify.py" $proofPath

# Step 6: Tamper
Write-Host "[6/6] Tampering with proof bundle..."
$bundle = Get-Content $proofPath | ConvertFrom-Json
$bundle.memory.content = "TAMPERED CONTENT"
$tamperedPath = "$env:TEMP\proof-bundle-tampered.json"
$bundle | ConvertTo-Json -Depth 10 | Set-Content $tamperedPath

Write-Host "  Verifying tampered bundle..."
try {
    & bubblefish.exe verify $tamperedPath
    Write-Host "  ERROR: tampered bundle should have failed!" -ForegroundColor Red
    exit 1
} catch {
    Write-Host "  Go CLI correctly detected tampering." -ForegroundColor Green
}

Write-Host ""
Write-Host "=== 60 seconds, full cryptographic provenance across vendors. ==="
