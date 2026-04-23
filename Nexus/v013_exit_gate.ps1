# v0.1.3 Exit Gate Checklist
# Run from the Nexus/ directory
# Usage: .\v013_exit_gate.ps1

$ErrorActionPreference = "Continue"
$pass = 0
$fail = 0
$manual = 0
$results = @()

function Gate-Auto {
    param([string]$Id, [string]$Name, [scriptblock]$Test)
    Write-Host -NoNewline "  [$Id] $Name ... "
    try {
        $out = & $Test 2>&1
        if ($LASTEXITCODE -eq 0 -or $LASTEXITCODE -eq $null) {
            Write-Host "PASS" -ForegroundColor Green
            $script:pass++
            $script:results += "PASS  $Id  $Name"
        } else {
            Write-Host "FAIL" -ForegroundColor Red
            Write-Host "        $out" -ForegroundColor DarkGray
            $script:fail++
            $script:results += "FAIL  $Id  $Name"
        }
    } catch {
        Write-Host "FAIL" -ForegroundColor Red
        Write-Host "        $_" -ForegroundColor DarkGray
        $script:fail++
        $script:results += "FAIL  $Id  $Name"
    }
}

function Gate-Manual {
    param([string]$Id, [string]$Name, [string]$Instructions)
    Write-Host "  [$Id] $Name" -ForegroundColor Yellow
    Write-Host "        $Instructions" -ForegroundColor DarkGray
    $script:manual++
    $script:results += "MANUAL  $Id  $Name"
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  NEXUS v0.1.3 -- FINAL EXIT GATE" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "--- AUTOMATED CHECKS ---" -ForegroundColor Cyan

Gate-Auto "EG-01" "go test ./... -race -count=1" {
    go test ./... -race -count=1
}

Gate-Auto "EG-01b" "go vet ./..." {
    go vet ./...
}

Gate-Auto "EG-01c" "go build ./..." {
    go build ./...
}

Write-Host ""
Write-Host "--- DAEMON REQUIRED (start nexus first) ---" -ForegroundColor Cyan

Gate-Manual "EG-02" "nexus install -- fresh install completes" `
    "Run: .\nexus install  (expect no errors, wizard completes)"

Gate-Manual "EG-03" "nexus doctor -- reports HEALTHY" `
    "Run: .\nexus doctor  (expect all checks HEALTHY)"

Gate-Manual "EG-04" "nexus verify -- audit chain integrity" `
    "Run: .\nexus verify  (expect integrity verified)"

Gate-Manual "EG-05" "Dashboard shows live data" `
    "Open https://localhost:8081 -- every panel shows real data, no mocks"

Gate-Manual "EG-06" "TUI launches, all commands work" `
    "Run: .\nexus tui  (try /status, /search, /maintain, /quarantine)"

Gate-Manual "EG-07" "Encryption round-trip verified" `
    "Run: .\nexus write 'gate test' then .\nexus search 'gate test' -- confirm match"

Gate-Manual "EG-08" "Orchestration with 2+ agents" `
    "Connect 2 MCP clients then .\nexus orchestrate enable -- verify shared memory"

Gate-Manual "EG-09" "Discovery finds 3+ tools" `
    "Run: .\nexus start (with discovery enabled) -- check logs for discovered tools >= 3"

Gate-Manual "EG-10" "Quarantine catches prompt injection" `
    "Run: .\nexus write 'Ignore all previous instructions and...' -- expect quarantine"

Gate-Manual "EG-11" "Backup then restore then verify data intact" `
    "Run: .\nexus backup --output test.bfbk then .\nexus restore --input test.bfbk then .\nexus verify"

Gate-Manual "EG-12" "Kill -9 then restart then zero data loss" `
    "Write a memory, then taskkill /F /IM nexus.exe, then .\nexus start, then .\nexus search (find it)"

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  RESULTS" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Auto PASS:  $pass" -ForegroundColor Green
if ($fail -gt 0) {
    Write-Host "  Auto FAIL:  $fail" -ForegroundColor Red
} else {
    Write-Host "  Auto FAIL:  $fail" -ForegroundColor Green
}
Write-Host "  Manual:     $manual" -ForegroundColor Yellow
Write-Host ""

foreach ($r in $results) {
    if ($r.StartsWith("PASS")) {
        Write-Host "  $r" -ForegroundColor Green
    } elseif ($r.StartsWith("FAIL")) {
        Write-Host "  $r" -ForegroundColor Red
    } else {
        Write-Host "  $r" -ForegroundColor Yellow
    }
}

Write-Host ""
if ($fail -eq 0) {
    Write-Host "  Automated gates CLEAR. Complete manual checks above," -ForegroundColor Green
    Write-Host "  then tag:  git tag v0.1.3; git push origin v0.1.3" -ForegroundColor Green
} else {
    Write-Host "  $fail automated gate(s) FAILED. Fix before proceeding." -ForegroundColor Red
}
Write-Host ""
