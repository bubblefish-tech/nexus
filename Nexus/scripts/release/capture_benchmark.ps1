# capture_benchmark.ps1 - Capture write/query throughput and latency
#
# Runs nexus-mini.ps1 and captures the results as structured JSON.
# Machine specs are recorded alongside the numbers.

param(
    [string]$OutputDir = ".",
    [string]$BaseUrl = "http://localhost:8080",
    [int]$WriteCount = 10000,
    [int]$ReadCount = 10000
)

$ErrorActionPreference = "Stop"

if (-not $env:NEXUS_API_KEY) {
    Write-Host "ERROR: set NEXUS_API_KEY" -ForegroundColor Red
    exit 1
}

# Capture machine specs
$os = [System.Environment]::OSVersion
$cpu = (Get-CimInstance Win32_Processor -ErrorAction SilentlyContinue | Select-Object -First 1).Name
if (-not $cpu) { $cpu = "unknown" }
$ram = [math]::Round((Get-CimInstance Win32_ComputerSystem -ErrorAction SilentlyContinue).TotalPhysicalMemory / 1GB, 1)
if (-not $ram) { $ram = 0 }

Write-Host "Benchmark: $WriteCount writes, $ReadCount reads"
Write-Host "Machine: $cpu, ${ram}GB RAM, $($os.VersionString)"

# Run the mini benchmark
$benchOutput = & powershell -File .\bench\nexus-mini.ps1 `
    -BaseUrl $BaseUrl `
    -ApiKey $env:NEXUS_API_KEY `
    -WriteCount $WriteCount `
    -ReadCount $ReadCount 2>&1

# Parse output for key metrics (best-effort regex extraction)
$writeRate = 0; $queryRate = 0
$writeP99 = 0; $queryP99 = 0
foreach ($line in $benchOutput) {
    $s = "$line"
    if ($s -match "writes/sec.*?(\d+\.?\d*)") { $writeRate = [double]$Matches[1] }
    if ($s -match "queries/sec.*?(\d+\.?\d*)") { $queryRate = [double]$Matches[1] }
    if ($s -match "write.*p99.*?(\d+\.?\d*)") { $writeP99 = [double]$Matches[1] }
    if ($s -match "query.*p99.*?(\d+\.?\d*)") { $queryP99 = [double]$Matches[1] }
}

# Get resident memory
$proc = Get-Process -Name "bubblefish" -ErrorAction SilentlyContinue | Select-Object -First 1
$residentMB = 0
if ($proc) {
    $residentMB = [math]::Round($proc.WorkingSet64 / 1MB, 1)
}

$result = @{
    timestamp      = (Get-Date -Format o)
    machine        = @{
        os   = $os.VersionString
        cpu  = $cpu
        ram_gb = $ram
    }
    writes         = @{
        count    = $WriteCount
        rate_per_sec = $writeRate
        p99_ms   = $writeP99
    }
    queries        = @{
        count    = $ReadCount
        rate_per_sec = $queryRate
        p99_ms   = $queryP99
    }
    resident_mb    = $residentMB
    raw_output     = ($benchOutput | Out-String)
}

$outPath = Join-Path $OutputDir "benchmark.json"
$result | ConvertTo-Json -Depth 5 | Set-Content -Path $outPath -Encoding UTF8
Write-Host "Benchmark results written to $outPath"
