# nexus-mini.ps1 - Three benchmarks that matter
# 1. Write throughput   - how fast can Nexus ingest memories
# 2. Query latency      - how fast can Nexus return them
# 3. Memory footprint   - what does it cost to run

param(
    [string]$BaseUrl = "http://localhost:8080",
    [string]$ApiKey = $env:NEXUS_API_KEY,
    [string]$Source = "default",
    [string]$Destination = "sqlite",
    [int]$WriteCount = 1000,
    [int]$ReadCount = 1000
)

if (-not $ApiKey) {
    Write-Host "ERROR: set `$env:NEXUS_API_KEY to a bfn_data_ key" -ForegroundColor Red
    exit 1
}

$ErrorActionPreference = "Stop"
$proc = Get-Process -Name "nexus" -ErrorAction SilentlyContinue | Select-Object -First 1

function Get-Stats($samples) {
    if ($samples.Count -eq 0) { return $null }
    $sorted = $samples | Sort-Object
    $n = $sorted.Count
    return @{
        avg = [math]::Round(($sorted | Measure-Object -Average).Average, 2)
        p50 = [math]::Round($sorted[[math]::Floor($n * 0.50)], 2)
        p95 = [math]::Round($sorted[[math]::Floor($n * 0.95)], 2)
        p99 = [math]::Round($sorted[[math]::Floor($n * 0.99)], 2)
    }
}

function Get-Ram() {
    if (-not $proc) { return 0 }
    try { return [math]::Round((Get-Process -Id $proc.Id).WorkingSet64 / 1MB, 1) } catch { return 0 }
}

Write-Host ""
Write-Host "=========================================" -ForegroundColor Cyan
Write-Host " Nexus Mini Benchmark" -ForegroundColor Cyan
Write-Host "=========================================" -ForegroundColor Cyan
$health = Invoke-RestMethod -Uri "$BaseUrl/health" -TimeoutSec 5
Write-Host " Version: $($health.version)"
Write-Host " Source:  $Source -> Destination: $Destination"
Write-Host " Plan:    $WriteCount writes, $ReadCount reads"
Write-Host ""

$ramStart = Get-Ram
Write-Host " RAM at start: $ramStart MB"
Write-Host ""

# ---------- Benchmark 1: Write throughput ----------
Write-Host "[1/3] Write throughput..." -ForegroundColor Yellow
$writeMs = @()
$writeFails = 0
$writeStart = Get-Date
for ($i = 1; $i -le $WriteCount; $i++) {
    $body = @{ message = @{ content = "Mini bench write ${i}: lorem ipsum dolor sit amet"; role = "user" }; model = "bench" } | ConvertTo-Json -Compress
    $h = @{ "Authorization" = "Bearer $ApiKey"; "Content-Type" = "application/json"; "X-Idempotency-Key" = [guid]::NewGuid().ToString() }
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        $null = Invoke-RestMethod -Uri "$BaseUrl/inbound/$Source" -Method Post -Headers $h -Body $body -TimeoutSec 30
        $sw.Stop()
        $writeMs += $sw.Elapsed.TotalMilliseconds
    } catch {
        $sw.Stop()
        $writeFails++
    }
    if ($i % 200 -eq 0) { Write-Host "  $i/$WriteCount..." }
}
$writeDur = ((Get-Date) - $writeStart).TotalSeconds
$writeStats = Get-Stats $writeMs
$writeQps = [math]::Round($writeMs.Count / $writeDur, 1)
Write-Host ""

# ---------- Benchmark 2: Query latency ----------
Write-Host "[2/3] Query latency..." -ForegroundColor Yellow
$queryWords = @("lorem","ipsum","dolor","amet","bench","mini","memory","write","test","sample")
$readMs = @()
$readFails = 0
$readStart = Get-Date
for ($i = 1; $i -le $ReadCount; $i++) {
    $q = $queryWords[(Get-Random -Maximum $queryWords.Count)]
    $encoded = [uri]::EscapeDataString($q)
    $url = "$BaseUrl/query/$Destination" + "?q=$encoded&limit=20"
    $h = @{ "Authorization" = "Bearer $ApiKey" }
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        $null = Invoke-RestMethod -Uri $url -Method Get -Headers $h -TimeoutSec 30
        $sw.Stop()
        $readMs += $sw.Elapsed.TotalMilliseconds
    } catch {
        $sw.Stop()
        $readFails++
    }
    if ($i % 200 -eq 0) { Write-Host "  $i/$ReadCount..." }
}
$readDur = ((Get-Date) - $readStart).TotalSeconds
$readStats = Get-Stats $readMs
$readQps = [math]::Round($readMs.Count / $readDur, 1)
Write-Host ""

# ---------- Benchmark 3: Memory footprint ----------
Write-Host "[3/3] Memory footprint..." -ForegroundColor Yellow
$ramEnd = Get-Ram
$ramDelta = [math]::Round($ramEnd - $ramStart, 1)
Write-Host ""

# ---------- Results ----------
Write-Host "=========================================" -ForegroundColor Green
Write-Host " RESULTS" -ForegroundColor Green
Write-Host "=========================================" -ForegroundColor Green
Write-Host ""
Write-Host " WRITE THROUGHPUT" -ForegroundColor Cyan
Write-Host "   $writeQps writes/sec    ($($writeMs.Count) ok, $writeFails failed in $([math]::Round($writeDur,1))s)"
if ($writeStats) {
    Write-Host "   Latency:  p50=$($writeStats.p50)ms  p95=$($writeStats.p95)ms  p99=$($writeStats.p99)ms"
}
Write-Host ""
Write-Host " QUERY LATENCY" -ForegroundColor Cyan
Write-Host "   $readQps queries/sec   ($($readMs.Count) ok, $readFails failed in $([math]::Round($readDur,1))s)"
if ($readStats) {
    Write-Host "   Latency:  p50=$($readStats.p50)ms  p95=$($readStats.p95)ms  p99=$($readStats.p99)ms"
}
Write-Host ""
Write-Host " MEMORY FOOTPRINT" -ForegroundColor Cyan
Write-Host "   Start:  $ramStart MB"
Write-Host "   End:    $ramEnd MB"
Write-Host "   Delta:  +$ramDelta MB after $WriteCount writes + $ReadCount queries"
Write-Host ""
Write-Host "=========================================" -ForegroundColor Green
