param(
    [string]$BaseUrl = "http://localhost:8080",
    [string]$ApiKey = $env:NEXUS_API_KEY,
    [string]$Source = "default",
    [string]$Destination = "sqlite",
    [string]$OutDir = "."
)

if (-not $ApiKey) {
    Write-Host "ERROR: Set -ApiKey or `$env:NEXUS_API_KEY (use a bfn_data_ key)" -ForegroundColor Red
    exit 1
}

$ErrorActionPreference = "Stop"
$RunId = (Get-Date).ToString("yyyyMMdd-HHmmss")
$Headers = @{ "Authorization" = "Bearer $ApiKey"; "Content-Type" = "application/json" }
$NexusProc = Get-Process -Name "nexus" -ErrorAction SilentlyContinue | Select-Object -First 1

Write-Host ""
Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host " NEXUS BENCHMARK - Run $RunId" -ForegroundColor Cyan
Write-Host "=================================================================" -ForegroundColor Cyan
Write-Host " Base URL:    $BaseUrl"
Write-Host " Source:      $Source"
Write-Host " Destination: $Destination"
if ($NexusProc) { Write-Host " Process:     PID $($NexusProc.Id)" }
Write-Host " Started:     $(Get-Date -Format 'HH:mm:ss')"
Write-Host "================================================================="
Write-Host ""

function Invoke-NexusWrite {
    param([string]$Content)
    $body = @{ message = @{ content = $Content; role = "user" }; model = "bench" } | ConvertTo-Json -Compress
    $h = @{ "Authorization" = "Bearer $ApiKey"; "Content-Type" = "application/json"; "X-Idempotency-Key" = [guid]::NewGuid().ToString() }
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        $null = Invoke-RestMethod -Uri "$BaseUrl/inbound/$Source" -Method Post -Headers $h -Body $body -TimeoutSec 30
        $sw.Stop()
        return @{ ok = $true; ms = $sw.Elapsed.TotalMilliseconds }
    } catch {
        $sw.Stop()
        return @{ ok = $false; ms = $sw.Elapsed.TotalMilliseconds; err = $_.Exception.Message }
    }
}

function Invoke-NexusQuery {
    param([string]$Query)
    $encoded = [uri]::EscapeDataString($Query)
    $url = "$BaseUrl/query/$Destination" + "?q=$encoded&limit=20"
    $sw = [System.Diagnostics.Stopwatch]::StartNew()
    try {
        $null = Invoke-RestMethod -Uri $url -Method Get -Headers $Headers -TimeoutSec 30
        $sw.Stop()
        return @{ ok = $true; ms = $sw.Elapsed.TotalMilliseconds }
    } catch {
        $sw.Stop()
        return @{ ok = $false; ms = $sw.Elapsed.TotalMilliseconds; err = $_.Exception.Message }
    }
}

function Get-Stats {
    param([double[]]$Samples)
    if ($Samples.Count -eq 0) { return $null }
    $sorted = $Samples | Sort-Object
    $n = $sorted.Count
    return @{
        count = $n
        min = [math]::Round($sorted[0], 2)
        max = [math]::Round($sorted[$n - 1], 2)
        avg = [math]::Round(($sorted | Measure-Object -Average).Average, 2)
        p50 = [math]::Round($sorted[[math]::Floor($n * 0.50)], 2)
        p95 = [math]::Round($sorted[[math]::Floor($n * 0.95)], 2)
        p99 = [math]::Round($sorted[[math]::Floor($n * 0.99)], 2)
    }
}

function Sample-Resources {
    if (-not $NexusProc) { return $null }
    try {
        $p = Get-Process -Id $NexusProc.Id -ErrorAction Stop
        return @{
            cpu_sec = [math]::Round($p.TotalProcessorTime.TotalSeconds, 2)
            ws_mb = [math]::Round($p.WorkingSet64 / 1MB, 2)
            handles = $p.HandleCount
            threads = $p.Threads.Count
        }
    } catch { return $null }
}

# Phase 0: Baseline
Write-Host "[Phase 0] Baseline..." -ForegroundColor Yellow
$baselineHealth = Invoke-RestMethod -Uri "$BaseUrl/health" -Method Get -TimeoutSec 10
$baselineRes = Sample-Resources
Write-Host "  Health: $($baselineHealth | ConvertTo-Json -Compress)"
if ($baselineRes) { Write-Host "  RAM: $($baselineRes.ws_mb) MB | Threads: $($baselineRes.threads) | Handles: $($baselineRes.handles)" }
Write-Host ""

# Phase 1: Seed
Write-Host "[Phase 1] Seeding 200 memories..." -ForegroundColor Yellow
$seedSamples = @()
$seedFails = 0
$phaseStart = Get-Date
for ($i = 1; $i -le 200; $i++) {
    $content = "Benchmark seed memory ${i}: lorem ipsum dolor sit amet consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."
    $r = Invoke-NexusWrite -Content $content
    if ($r.ok) { $seedSamples += $r.ms } else { $seedFails++ }
    if ($i % 50 -eq 0) { Write-Host "  $i/200..." }
}
$seedDur = ((Get-Date) - $phaseStart).TotalSeconds
$seedStats = Get-Stats $seedSamples
Write-Host "  Done: $($seedSamples.Count) ok, $seedFails failed, $([math]::Round($seedDur,1))s"
if ($seedStats) { Write-Host "  Latency: avg=$($seedStats.avg)ms p50=$($seedStats.p50)ms p95=$($seedStats.p95)ms p99=$($seedStats.p99)ms" }
Write-Host ""
Start-Sleep -Seconds 2

# Phase 2: Cold vs warm
Write-Host "[Phase 2] Cold vs warm query..." -ForegroundColor Yellow
$coldR = Invoke-NexusQuery -Query "lorem benchmark"
Start-Sleep -Milliseconds 100
$warmSamples = @()
for ($i = 1; $i -le 20; $i++) {
    $r = Invoke-NexusQuery -Query "lorem benchmark"
    if ($r.ok) { $warmSamples += $r.ms }
}
$warmStats = Get-Stats $warmSamples
Write-Host "  Cold: $([math]::Round($coldR.ms,2)) ms"
if ($warmStats) { Write-Host "  Warm: avg=$($warmStats.avg)ms p50=$($warmStats.p50)ms p95=$($warmStats.p95)ms" }
Write-Host ""

# Phase 3: Query sweep
Write-Host "[Phase 3] Query latency sweep - 900 queries..." -ForegroundColor Yellow
$queryWords = @("lorem","ipsum","dolor","amet","consectetur","tempor","labore","magna","benchmark","seed")
$querySamples = @()
$queryFails = 0
$qStart = Get-Date
for ($i = 1; $i -le 900; $i++) {
    $q = $queryWords[(Get-Random -Maximum $queryWords.Count)]
    $r = Invoke-NexusQuery -Query $q
    if ($r.ok) { $querySamples += $r.ms } else { $queryFails++ }
    if ($i % 150 -eq 0) { Write-Host "  $i/900..." }
}
$qDur = ((Get-Date) - $qStart).TotalSeconds
$queryStats = Get-Stats $querySamples
if ($queryStats) {
    $queryStats.qps = [math]::Round($querySamples.Count / $qDur, 2)
    $queryStats.fails = $queryFails
    $queryStats.duration_s = [math]::Round($qDur, 1)
    Write-Host "  avg=$($queryStats.avg)ms p50=$($queryStats.p50)ms p95=$($queryStats.p95)ms p99=$($queryStats.p99)ms qps=$($queryStats.qps) fails=$queryFails"
} else {
    Write-Host "  NO SAMPLES fails=$queryFails" -ForegroundColor Red
    $queryStats = @{ count = 0; avg = 0; p50 = 0; p95 = 0; p99 = 0; qps = 0; fails = $queryFails; duration_s = [math]::Round($qDur,1) }
}
Write-Host ""

# Phase 4: Write throughput
Write-Host "[Phase 4] Write throughput - 500 sequential..." -ForegroundColor Yellow
$writeSamples = @()
$writeFails = 0
$start = Get-Date
for ($i = 1; $i -le 500; $i++) {
    $r = Invoke-NexusWrite -Content "Throughput write ${i} at $(Get-Date -Format 'HH:mm:ss.fff')"
    if ($r.ok) { $writeSamples += $r.ms } else { $writeFails++ }
    if ($i % 100 -eq 0) { Write-Host "  $i/500..." }
}
$writeDur = ((Get-Date) - $start).TotalSeconds
$writeStats = Get-Stats $writeSamples
if ($writeStats) {
    $writeStats.qps = [math]::Round($writeSamples.Count / $writeDur, 2)
    $writeStats.fails = $writeFails
    $writeStats.duration_s = [math]::Round($writeDur, 1)
    Write-Host "  avg=$($writeStats.avg)ms p50=$($writeStats.p50)ms p95=$($writeStats.p95)ms p99=$($writeStats.p99)ms qps=$($writeStats.qps) fails=$writeFails"
} else {
    Write-Host "  NO SAMPLES fails=$writeFails" -ForegroundColor Red
    $writeStats = @{ count = 0; avg = 0; p50 = 0; p95 = 0; p99 = 0; qps = 0; fails = $writeFails; duration_s = [math]::Round($writeDur,1) }
}
Write-Host ""

# Phase 5: 80/20 mix
Write-Host "[Phase 5] 80/20 mix - 120s sustained..." -ForegroundColor Yellow
$mixReads = @()
$mixWrites = @()
$mixFails = 0
$mixStart = Get-Date
$lastSample = $mixStart
$opCount = 0
while (((Get-Date) - $mixStart).TotalSeconds -lt 120) {
    $opCount++
    if ((Get-Random -Maximum 100) -lt 80) {
        $q = $queryWords[(Get-Random -Maximum $queryWords.Count)]
        $r = Invoke-NexusQuery -Query $q
        if ($r.ok) { $mixReads += $r.ms } else { $mixFails++ }
    } else {
        $r = Invoke-NexusWrite -Content "Mix write $opCount at $(Get-Date -Format 'HH:mm:ss.fff')"
        if ($r.ok) { $mixWrites += $r.ms } else { $mixFails++ }
    }
    if (((Get-Date) - $lastSample).TotalSeconds -ge 5) {
        $lastSample = Get-Date
        $elapsed = [math]::Round(((Get-Date) - $mixStart).TotalSeconds)
        Write-Host "  ${elapsed}s: $opCount ops - $($mixReads.Count) reads, $($mixWrites.Count) writes, $mixFails fails"
    }
}
$mixDur = ((Get-Date) - $mixStart).TotalSeconds
$mixReadStats = Get-Stats $mixReads
$mixWriteStats = Get-Stats $mixWrites
$mixTotalQps = [math]::Round($opCount / $mixDur, 2)
Write-Host "  Total: $opCount ops in $([math]::Round($mixDur,1))s = $mixTotalQps ops/s"
if ($mixReadStats) { Write-Host "  Reads:  avg=$($mixReadStats.avg)ms p95=$($mixReadStats.p95)ms p99=$($mixReadStats.p99)ms" }
if ($mixWriteStats) { Write-Host "  Writes: avg=$($mixWriteStats.avg)ms p95=$($mixWriteStats.p95)ms p99=$($mixWriteStats.p99)ms" }
Write-Host ""

# Phase 6: Concurrency
Write-Host "[Phase 6] Concurrency sweep..." -ForegroundColor Yellow
$concResults = @{}
foreach ($parallel in @(1, 10, 50, 100, 200)) {
    Write-Host "  $parallel parallel..."
    $jobs = @()
    $start = Get-Date
    1..$parallel | ForEach-Object {
        $jobs += Start-Job -ScriptBlock {
            param($url, $key, $dest, $word)
            $h = @{ "Authorization" = "Bearer $key" }
            $encoded = [uri]::EscapeDataString($word)
            $fullUrl = "$url/query/$dest" + "?q=$encoded&limit=20"
            $sw = [System.Diagnostics.Stopwatch]::StartNew()
            try {
                $null = Invoke-RestMethod -Uri $fullUrl -Method Get -Headers $h -TimeoutSec 60
                $sw.Stop()
                return @{ ok = $true; ms = $sw.Elapsed.TotalMilliseconds }
            } catch {
                $sw.Stop()
                return @{ ok = $false; ms = $sw.Elapsed.TotalMilliseconds }
            }
        } -ArgumentList $BaseUrl, $ApiKey, $Destination, "lorem"
    }
    $results = $jobs | Wait-Job | Receive-Job
    $jobs | Remove-Job
    $totalDur = ((Get-Date) - $start).TotalSeconds
    $okSamples = @($results | Where-Object { $_.ok } | ForEach-Object { $_.ms })
    $stats = Get-Stats $okSamples
    if (-not $stats) { $stats = @{ count = 0; avg = 0; p50 = 0; p95 = 0; p99 = 0 } }
    $stats.parallel = $parallel
    $stats.wallclock_s = [math]::Round($totalDur, 2)
    $stats.effective_qps = [math]::Round($parallel / $totalDur, 2)
    $stats.fails = @($results | Where-Object { -not $_.ok }).Count
    $concResults["p$parallel"] = $stats
    Write-Host "    wall=$($stats.wallclock_s)s eff_qps=$($stats.effective_qps) avg=$($stats.avg)ms p95=$($stats.p95)ms fails=$($stats.fails)"
}
Write-Host ""

# Phase 7: Final
Write-Host "[Phase 7] Final health..." -ForegroundColor Yellow
$finalHealth = Invoke-RestMethod -Uri "$BaseUrl/health" -Method Get -TimeoutSec 10
$finalRes = Sample-Resources
Write-Host "  Health: $($finalHealth | ConvertTo-Json -Compress)"
if ($finalRes -and $baselineRes) {
    $ramDelta = [math]::Round($finalRes.ws_mb - $baselineRes.ws_mb, 2)
    Write-Host "  RAM: $($baselineRes.ws_mb) -> $($finalRes.ws_mb) MB (delta $ramDelta MB)"
    Write-Host "  Threads: $($baselineRes.threads) -> $($finalRes.threads)"
}
Write-Host ""

# Output files
$results = @{
    run_id = $RunId
    base_url = $BaseUrl
    source = $Source
    destination = $Destination
    nexus_version = $finalHealth.version
    baseline = @{ resources = $baselineRes }
    final = @{ resources = $finalRes }
    seed = @{ stats = $seedStats; fails = $seedFails; duration_s = [math]::Round($seedDur,1) }
    cold_warm = @{ cold_ms = [math]::Round($coldR.ms,2); warm = $warmStats }
    query_sweep = $queryStats
    write_throughput = $writeStats
    mix = @{
        total_ops = $opCount
        duration_s = [math]::Round($mixDur,1)
        total_qps = $mixTotalQps
        reads = $mixReadStats
        writes = $mixWriteStats
        fails = $mixFails
    }
    concurrency = $concResults
}

$jsonPath = Join-Path $OutDir "nexus-bench-results-$RunId.json"
$txtPath = Join-Path $OutDir "nexus-bench-results-$RunId.txt"
$results | ConvertTo-Json -Depth 10 | Out-File -FilePath $jsonPath -Encoding utf8

$lines = @()
$lines += "================================================================="
$lines += " NEXUS BENCHMARK RESULTS - Run $RunId"
$lines += "================================================================="
$lines += " Version:     $($finalHealth.version)"
$lines += " Source:      $Source"
$lines += " Destination: $Destination"
$lines += ""
$lines += "-----------------------------------------------------------------"
$lines += " Phase 1 - Seed Writes - 200 ops"
$lines += "-----------------------------------------------------------------"
if ($seedStats) { $lines += " avg=$($seedStats.avg)ms p50=$($seedStats.p50)ms p95=$($seedStats.p95)ms p99=$($seedStats.p99)ms" }
$lines += " fails=$seedFails duration=$([math]::Round($seedDur,1))s"
$lines += ""
$lines += "-----------------------------------------------------------------"
$lines += " Phase 2 - Cold vs Warm"
$lines += "-----------------------------------------------------------------"
$lines += " Cold: $([math]::Round($coldR.ms,2)) ms"
if ($warmStats) { $lines += " Warm: avg=$($warmStats.avg)ms p95=$($warmStats.p95)ms" }
$lines += ""
$lines += "-----------------------------------------------------------------"
$lines += " Phase 3 - Query Sweep - 900 queries"
$lines += "-----------------------------------------------------------------"
$lines += " avg=$($queryStats.avg)ms p50=$($queryStats.p50)ms p95=$($queryStats.p95)ms p99=$($queryStats.p99)ms"
$lines += " qps=$($queryStats.qps) fails=$($queryStats.fails) duration=$($queryStats.duration_s)s"
$lines += ""
$lines += "-----------------------------------------------------------------"
$lines += " Phase 4 - Write Throughput - 500 sequential"
$lines += "-----------------------------------------------------------------"
$lines += " avg=$($writeStats.avg)ms p50=$($writeStats.p50)ms p95=$($writeStats.p95)ms p99=$($writeStats.p99)ms"
$lines += " qps=$($writeStats.qps) fails=$writeFails duration=$($writeStats.duration_s)s"
$lines += ""
$lines += "-----------------------------------------------------------------"
$lines += " Phase 5 - 80/20 Mix - 120s sustained"
$lines += "-----------------------------------------------------------------"
$lines += " Total ops: $opCount  reads=$($mixReads.Count) writes=$($mixWrites.Count)"
$lines += " Throughput: $mixTotalQps ops/s  fails=$mixFails"
if ($mixReadStats) { $lines += " Reads:  avg=$($mixReadStats.avg)ms p50=$($mixReadStats.p50)ms p95=$($mixReadStats.p95)ms p99=$($mixReadStats.p99)ms" }
if ($mixWriteStats) { $lines += " Writes: avg=$($mixWriteStats.avg)ms p50=$($mixWriteStats.p50)ms p95=$($mixWriteStats.p95)ms p99=$($mixWriteStats.p99)ms" }
$lines += ""
$lines += "-----------------------------------------------------------------"
$lines += " Phase 6 - Concurrency Sweep"
$lines += "-----------------------------------------------------------------"
$lines += " Parallel | wall_s  | eff_qps | avg_ms  | p95_ms  | fails"
foreach ($p in @(1,10,50,100,200)) {
    $s = $concResults["p$p"]
    $line = " {0,8} | {1,7} | {2,7} | {3,7} | {4,7} | {5}" -f $p, $s.wallclock_s, $s.effective_qps, $s.avg, $s.p95, $s.fails
    $lines += $line
}
$lines += ""
$lines += "-----------------------------------------------------------------"
$lines += " Phase 7 - Resource Footprint"
$lines += "-----------------------------------------------------------------"
if ($baselineRes -and $finalRes) {
    $ramDelta = [math]::Round($finalRes.ws_mb - $baselineRes.ws_mb, 2)
    $lines += " RAM:     $($baselineRes.ws_mb) MB -> $($finalRes.ws_mb) MB  delta=$ramDelta MB"
    $lines += " Threads: $($baselineRes.threads) -> $($finalRes.threads)"
    $lines += " Handles: $($baselineRes.handles) -> $($finalRes.handles)"
}
$lines += ""
$lines += " Final Health: $($finalHealth | ConvertTo-Json -Compress)"
$lines += "================================================================="

$lines -join "`r`n" | Out-File -FilePath $txtPath -Encoding utf8

Write-Host ""
Write-Host "=================================================================" -ForegroundColor Green
Write-Host " BENCHMARK COMPLETE" -ForegroundColor Green
Write-Host "=================================================================" -ForegroundColor Green
Write-Host " JSON: $jsonPath"
Write-Host " TXT:  $txtPath"
Write-Host "================================================================="
Write-Host ""
Get-Content $txtPath