# Copyright (c) 2026 Shawn Sammartano
param(
    [Parameter(Mandatory=$true)] [string]$Tape,
    [Parameter(Mandatory=$true)] [ValidateSet("dev","dogfood")] [string]$Instance
)

$env:NEXUS_API_URL     = "http://localhost:8080"
$env:NEXUS_ADMIN_TOKEN = $env:NEXUS_ADMIN_TOKEN  # inherit from shell

switch ($Instance) {
    "dev"     { Set-Location D:\BubbleFish\Nexus }
    "dogfood" { Set-Location D:\Test\BubbleFish\Dogfood }
}

# Warn if no admin token is configured.
if (-not $env:NEXUS_ADMIN_TOKEN) {
    Write-Warning "NEXUS_ADMIN_TOKEN is not set. Recording no-auth variant (EmptyStateFeatureGated paths expected)."
}

# Ensure daemon is running at :8080
$ok = $false
try {
    $headers = @{}
    if ($env:NEXUS_ADMIN_TOKEN) {
        $headers["Authorization"] = "Bearer $($env:NEXUS_ADMIN_TOKEN)"
    }
    $resp = Invoke-WebRequest -Uri "http://localhost:8080/api/status" -Headers $headers -TimeoutSec 2 -UseBasicParsing
    if ($resp.StatusCode -eq 200) { $ok = $true }
} catch { }
if (-not $ok) {
    Write-Error "Daemon not reachable on :8080 for instance '$Instance'. Start it first."
    exit 1
}

# Ensure output directory exists.
$outDir = "scripts/vhs/out"
if (-not (Test-Path $outDir)) {
    New-Item -ItemType Directory -Path $outDir | Out-Null
}

$tapeName = [System.IO.Path]::GetFileNameWithoutExtension($Tape)
$outGif = "$outDir/${tapeName}_${Instance}.gif"

# Modify Output line on the fly and propagate env vars into the tape.
$tmp = New-TemporaryFile
Get-Content $Tape | ForEach-Object {
    if ($_ -match '^Output ') {
        "Output $outGif"
    } elseif ($_ -match '^Env NEXUS_API_URL') {
        "Env NEXUS_API_URL=$($env:NEXUS_API_URL)"
    } elseif ($_ -match '^Env NEXUS_ADMIN_TOKEN') {
        if ($env:NEXUS_ADMIN_TOKEN) {
            "Env NEXUS_ADMIN_TOKEN=$($env:NEXUS_ADMIN_TOKEN)"
        } else {
            "# Env NEXUS_ADMIN_TOKEN not set — no-auth recording"
        }
    } else {
        $_
    }
} | Set-Content $tmp.FullName

vhs $tmp.FullName
Remove-Item $tmp.FullName
Write-Host "Recorded $outGif"
