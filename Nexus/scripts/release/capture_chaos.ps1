# capture_chaos.ps1 - Run chaos test and capture results
#
# Runs bubblefish chaos with the A+B verifier and saves the report.

param(
    [string]$OutputDir = ".",
    [string]$BaseUrl = "http://localhost:8080",
    [string]$Duration = "5m",
    [string]$DBPath = ""
)

$ErrorActionPreference = "Stop"

if (-not $env:NEXUS_API_KEY) {
    Write-Host "ERROR: set NEXUS_API_KEY" -ForegroundColor Red
    exit 1
}
if (-not $env:NEXUS_ADMIN_KEY) {
    Write-Host "ERROR: set NEXUS_ADMIN_KEY" -ForegroundColor Red
    exit 1
}

# Auto-detect DB path if not specified
if (-not $DBPath) {
    $home = $env:BUBBLEFISH_HOME
    if (-not $home) {
        $home = Join-Path $env:USERPROFILE ".bubblefish\Nexus"
    }
    $DBPath = Join-Path $home "memories.db"
}

if (-not (Test-Path $DBPath)) {
    Write-Host "ERROR: DB not found at $DBPath" -ForegroundColor Red
    exit 1
}

$outPath = Join-Path $OutputDir "chaos.json"
Write-Host "Chaos: duration=$Duration, db=$DBPath"

.\bubblefish.exe chaos `
    -url $BaseUrl `
    -api-key $env:NEXUS_API_KEY `
    -admin-key $env:NEXUS_ADMIN_KEY `
    -db $DBPath `
    -duration $Duration `
    -seed 42 `
    -report $outPath

if ($LASTEXITCODE -ne 0) {
    Write-Host "CHAOS FAILED" -ForegroundColor Red
    exit 1
}

Write-Host "Chaos results written to $outPath"
$result = Get-Content $outPath | ConvertFrom-Json
Write-Host "Verdict: $($result.verdict)" -ForegroundColor $(if ($result.pass) { "Green" } else { "Red" })
