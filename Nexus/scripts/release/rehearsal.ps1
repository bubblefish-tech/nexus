# rehearsal.ps1 - Full v0.1.3 release rehearsal
#
# Builds a clean release binary, creates a fresh sandbox, starts the daemon
# with Ingest enabled, runs benchmarks and chaos, and captures results.
#
# Usage: .\scripts\release\rehearsal.ps1
#
# Prerequisites:
#   - Go toolchain installed
#   - $env:NEXUS_API_KEY and $env:NEXUS_ADMIN_KEY set
#   - No other bubblefish process running on port 8080

param(
    [string]$SandboxDir = "D:\Test\BubbleFish\v013-rehearsal",
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"

Write-Host "=== v0.1.3 Release Rehearsal ===" -ForegroundColor Cyan
Write-Host "Sandbox: $SandboxDir"
Write-Host ""

# 1. Build clean release binary
if (-not $SkipBuild) {
    Write-Host "[1/7] Building release binary..." -ForegroundColor Yellow
    $buildTime = (Get-Date -Format o)
    $env:CGO_ENABLED = '0'
    go build -ldflags "-s -w -X main.Version=0.1.3 -X main.BuildTime=$buildTime" -o dist\bubblefish.exe .\cmd\bubblefish
    if ($LASTEXITCODE -ne 0) { Write-Host "BUILD FAILED" -ForegroundColor Red; exit 1 }
    $size = (Get-Item dist\bubblefish.exe).Length
    Write-Host "  Binary size: $([math]::Round($size / 1MB, 1)) MB"
} else {
    Write-Host "[1/7] Skipping build (--SkipBuild)" -ForegroundColor DarkGray
}

# 2. Fresh sandbox
Write-Host "[2/7] Creating fresh sandbox..." -ForegroundColor Yellow
if (Test-Path $SandboxDir) {
    Remove-Item -Recurse -Force $SandboxDir
}
New-Item -ItemType Directory -Force "$SandboxDir\home" | Out-Null
$env:BUBBLEFISH_HOME = "$SandboxDir\home"
Write-Host "  BUBBLEFISH_HOME = $env:BUBBLEFISH_HOME"

# 3. Install
Write-Host "[3/7] Installing..." -ForegroundColor Yellow
.\dist\bubblefish.exe install --mode simple
if ($LASTEXITCODE -ne 0) { Write-Host "INSTALL FAILED" -ForegroundColor Red; exit 1 }

# 4. Start daemon
Write-Host "[4/7] Starting daemon..." -ForegroundColor Yellow
Start-Process -FilePath ".\dist\bubblefish.exe" -ArgumentList "start" -NoNewWindow
$deadline = (Get-Date).AddSeconds(30)
$healthy = $false
while ((Get-Date) -lt $deadline) {
    try {
        $h = Invoke-RestMethod -Uri "http://localhost:8080/health" -ErrorAction SilentlyContinue
        if ($h.status -eq "ok") { $healthy = $true; break }
    } catch {}
    Start-Sleep -Seconds 1
}
if (-not $healthy) { Write-Host "DAEMON DID NOT START" -ForegroundColor Red; exit 1 }
Write-Host "  Daemon healthy (version $($h.version))"

# 5. Run benchmarks
Write-Host "[5/7] Running benchmarks..." -ForegroundColor Yellow
.\scripts\release\capture_benchmark.ps1 -OutputDir $SandboxDir

# 6. Run chaos (short form for rehearsal)
Write-Host "[6/7] Running chaos (5 minute rehearsal)..." -ForegroundColor Yellow
.\scripts\release\capture_chaos.ps1 -OutputDir $SandboxDir -Duration "5m"

# 7. Collect results
Write-Host "[7/7] Results:" -ForegroundColor Green
if (Test-Path "$SandboxDir\benchmark.json") {
    Write-Host "  Benchmark: $SandboxDir\benchmark.json"
    Get-Content "$SandboxDir\benchmark.json" | ConvertFrom-Json | Format-List
}
if (Test-Path "$SandboxDir\chaos.json") {
    Write-Host "  Chaos: $SandboxDir\chaos.json"
    Get-Content "$SandboxDir\chaos.json" | ConvertFrom-Json | Format-List
}

Write-Host ""
Write-Host "=== Rehearsal complete ===" -ForegroundColor Cyan
Write-Host "Next steps:"
Write-Host "  1. Review the numbers above"
Write-Host "  2. Run sign_artifacts.ps1 to sign the results"
Write-Host "  3. Update CHANGELOG.md with actual observed values"
Write-Host "  4. Schedule the 24-hour chaos run (Sunday night)"
