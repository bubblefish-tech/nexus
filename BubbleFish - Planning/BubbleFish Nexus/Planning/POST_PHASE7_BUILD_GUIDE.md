# 🐟 BubbleFish Nexus — Post Phase 7 Build Guide

**Status after Phase 7:** v1.0.0 tagged and pushed. All 38 tests passing. 1000-request load test passed with zero data loss. WAL crash recovery confirmed.

This guide covers everything to do before starting v2 development.

---

## Part 1 — Immediate: GitHub Cleanup

### Step 1.1 — Verify .env is NOT in GitHub

```powershell
# Check what's currently tracked
git ls-files | Select-String "\.env"
git ls-files | Select-String "loadtest"
```

If `.env` appears in the output, remove it immediately:

```powershell
# Remove from git tracking (keeps the file locally)
git rm --cached .env
git rm --cached loadtest.ps1

# Add to .gitignore if not already there
Add-Content -Path D:\Bubblefish\Projects\Nexus\.gitignore -Value "`n.env`nloadtest.ps1`n*.ps1"

# Commit the removal
git add .gitignore
git commit -m "Remove .env and scripts from tracking — security cleanup"
git push origin master
```

### Step 1.2 — Remove Duplicate Template Files

```powershell
# Check if duplicate files exist
git ls-files | Select-String "\(1\)"
```

If they appear:

```powershell
# Remove duplicates from git and disk
git rm "internal/config/templates/ollama (1).toml"
git rm "internal/config/templates/openbrain (1).toml"
git commit -m "Remove duplicate template files"
git push origin master
```

If files don't exist in git but exist locally:

```powershell
# Remove locally only
Remove-Item "D:\Bubblefish\Projects\Nexus\internal\config\templates\ollama (1).toml" -Force -ErrorAction SilentlyContinue
Remove-Item "D:\Bubblefish\Projects\Nexus\internal\config\templates\openbrain (1).toml" -Force -ErrorAction SilentlyContinue
```

### Step 1.3 — Verify GitHub Repo State

Open https://github.com/shawnsammartano-hub/BubbleFish-Nexus and confirm:
- `.env` is NOT listed in the file browser
- No `(1).toml` files visible
- README renders correctly on the front page
- v1.0.0 tag appears under Releases

---

## Part 2 — Short Term: Connect to Real OpenBrain/Supabase

### Step 2.1 — Re-enable OpenBrain Destination

```powershell
# Rename back from disabled
Rename-Item C:\Users\shawn\.bubblefish\Nexus\destinations\openbrain.toml.disabled `
            C:\Users\shawn\.bubblefish\Nexus\destinations\openbrain.toml
```

### Step 2.2 — Set Real Supabase Credentials

Open your `.env` file and fill in real values:

```powershell
# View current .env
Get-Content D:\Bubblefish\Projects\Nexus\.env
```

Edit it to add your real values:

```
ADMIN_TOKEN=your-secure-admin-token-here
CLAUDE_SOURCE_KEY=nexus-key-claude-001
PERPLEXITY_SOURCE_KEY=nexus-key-perplexity-001
SUPABASE_URL=https://your-project.supabase.co
SUPABASE_SERVICE_ROLE_KEY=your-service-role-key-here
```

Load the env vars:

```powershell
Get-Content D:\Bubblefish\Projects\Nexus\.env | ForEach-Object {
    if ($_ -match '^([^#=]+)=(.*)$') {
        [System.Environment]::SetEnvironmentVariable($Matches[1].Trim(), $Matches[2].Trim(), 'Process')
    }
}
```

### Step 2.3 — Update Sources to Target OpenBrain

```powershell
# Update claude source to target openbrain
(Get-Content C:\Users\shawn\.bubblefish\Nexus\sources\claude.toml -Raw) `
    -replace 'target_destination = "sqlite"','target_destination = "openbrain"' `
    | Set-Content C:\Users\shawn\.bubblefish\Nexus\sources\claude.toml -NoNewline

# Update perplexity source
(Get-Content C:\Users\shawn\.bubblefish\Nexus\sources\perplexity.toml -Raw) `
    -replace 'target_destination = "sqlite"','target_destination = "openbrain"' `
    | Set-Content C:\Users\shawn\.bubblefish\Nexus\sources\perplexity.toml -NoNewline
```

### Step 2.4 — Rebuild and Doctor Check

```powershell
Set-Location D:\Bubblefish\Projects\Nexus
.\bubblefish.exe build
.\bubblefish.exe doctor
```

Expected output:
```
  [OK]   WAL path writable
  [OK]   Destination "openbrain" reachable
  [OK]   Destination "sqlite" reachable
  [OK]   Config valid
  [OK]   Port 8080 available
  [OK]   Disk space sufficient (>500MB)
  [OK]   No duplicate API keys
All checks passed.
```

### Step 2.5 — Send Real Payload to Supabase

Start the daemon:

```powershell
.\bubblefish.exe daemon
```

In a second window, send a test payload:

```powershell
$h = @{
    "Authorization" = "Bearer nexus-key-claude-001"
    "Content-Type"  = "application/json"
}
$b = '{"message":{"content":"BubbleFish Nexus v1.0.0 — first real memory write to OpenBrain.","role":"user"},"model":"claude-opus-4-5"}'
Invoke-RestMethod -Method POST -Uri http://localhost:8080/inbound/claude -Headers $h -Body $b
```

Verify it appears in your Supabase dashboard under the `thoughts` table.

---

## Part 3 — Medium Term: Production Hardening

### Step 3.1 — Add GCC to PATH Permanently (Developers Only)

This is only needed for `go test -race`. End users do not need GCC.

```powershell
# Add permanently to system PATH (run as Administrator)
$currentPath = [System.Environment]::GetEnvironmentVariable("PATH", "Machine")
if ($currentPath -notlike "*msys64*") {
    [System.Environment]::SetEnvironmentVariable(
        "PATH",
        $currentPath + ";C:\msys64\mingw64\bin",
        "Machine"
    )
    Write-Host "GCC added to system PATH. Restart PowerShell to take effect."
} else {
    Write-Host "GCC already in PATH."
}
```

Verify after restarting PowerShell:

```powershell
gcc --version
$env:CGO_ENABLED="1"
go test ./... -race -count=1
```

### Step 3.2 — Fill in PERFORMANCE.md

```powershell
# Get current row count for baseline
$h = @{"Authorization"="Bearer nexus-key-claude-001";"Content-Type"="application/json"}
$result = Invoke-RestMethod -Method GET -Uri "http://localhost:8080/query/sqlite?limit=2000" -Headers $h
Write-Host "Total rows in SQLite: $($result.Count)"
```

Open `D:\Bubblefish\Projects\Nexus\PERFORMANCE.md` and fill in:
- Machine specs
- Go version (`go version`)
- Test date
- Rows written: 1000
- Failed requests: 0
- Data loss: 0
- Crash recovery: confirmed via WAL replay

```powershell
git add PERFORMANCE.md
git commit -m "Add v1.0.0 performance baseline numbers"
git push origin master
```

### Step 3.3 — Set Up Cloudflare Tunnel (Optional — Remote Access)

Install cloudflared:

```powershell
winget install -e --id Cloudflare.cloudflared
```

Authenticate:

```powershell
cloudflared tunnel login
```

Create tunnel:

```powershell
cloudflared tunnel create bubblefish-nexus
```

Create config file at `C:\Users\shawn\.cloudflared\config.yml`:

```yaml
tunnel: <your-tunnel-id>
credentials-file: C:\Users\shawn\.cloudflared\<tunnel-id>.json

ingress:
  - hostname: nexus.yourdomain.com
    service: http://127.0.0.1:8080
  - service: http_status:404
```

Run tunnel:

```powershell
cloudflared tunnel run bubblefish-nexus
```

With this running, AI clients anywhere on the internet can reach Nexus at `https://nexus.yourdomain.com`.

---

## Part 4 — Bug Fixes Before V2

### Step 4.1 — Fix WAL DELIVERED Marking (Critical)

This is the most important bug to fix. Currently every restart re-enqueues all WAL entries because workers never mark them DELIVERED.

The fix goes in `internal/queue/worker.go`. After a successful `plugin.Write()`, update the WAL entry status. This requires passing a WAL reference to the worker.

This is a code change — tracked in the v2 build plan as Phase 0 item 1.

### Step 4.2 — Raise Default Rate Limits

Edit source TOML templates to use 2000 requests/minute as the default:

```powershell
# Update all source templates
Get-ChildItem D:\Bubblefish\Projects\Nexus\internal\config\templates\*.toml | ForEach-Object {
    $content = Get-Content $_.FullName -Raw
    $updated = $content -replace 'requests_per_minute = 120','requests_per_minute = 2000'
    $updated = $updated -replace 'requests_per_minute = 60','requests_per_minute = 1000'
    if ($content -ne $updated) {
        Set-Content -Path $_.FullName -Value $updated -NoNewline
        Write-Host "Updated: $($_.Name)"
    }
}
```

Also update user configs:

```powershell
Get-ChildItem C:\Users\shawn\.bubblefish\Nexus\sources\*.toml | ForEach-Object {
    $content = Get-Content $_.FullName -Raw
    $updated = $content -replace 'requests_per_minute = 120','requests_per_minute = 2000'
    $updated = $updated -replace 'requests_per_minute = 60','requests_per_minute = 1000'
    if ($content -ne $updated) {
        Set-Content -Path $_.FullName -Value $updated -NoNewline
        Write-Host "Updated: $($_.Name)"
    }
}
.\bubblefish.exe build
```

Commit template changes:

```powershell
git add internal/config/templates/
git commit -m "Raise default rate limits to 2000/min"
git push origin master
```

### Step 4.3 — Tag Post-Cleanup Release

```powershell
Set-Location D:\Bubblefish\Projects\Nexus
go build ./...
go vet ./...
$env:CGO_ENABLED="1"
$env:PATH += ";C:\msys64\mingw64\bin"
go test ./... -race -count=1

git add -A
git commit -m "Post v1.0.0 cleanup — security, rate limits, performance docs"
git tag v1.0.1
git push origin master --tags
```

---

## Part 5 — Verify Everything Is Clean

```powershell
# Final verification checklist
Write-Host "=== BubbleFish Nexus v1.0.1 Verification ==="

# 1. Build clean
go build ./...
if ($LASTEXITCODE -eq 0) { Write-Host "[OK] go build" } else { Write-Host "[FAIL] go build" }

# 2. Vet clean
go vet ./...
if ($LASTEXITCODE -eq 0) { Write-Host "[OK] go vet" } else { Write-Host "[FAIL] go vet" }

# 3. Tests pass
$env:CGO_ENABLED="1"
go test ./... -count=1
if ($LASTEXITCODE -eq 0) { Write-Host "[OK] go test" } else { Write-Host "[FAIL] go test" }

# 4. Binary builds
go build -o bubblefish.exe .\cmd\bubblefish\
.\bubblefish.exe version
.\bubblefish.exe doctor

Write-Host "=== Done ==="
```

---

## Summary Checklist

```
□ .env removed from GitHub tracking
□ Duplicate (1).toml files removed
□ GitHub repo verified clean
□ OpenBrain destination re-enabled
□ Real Supabase credentials in .env
□ Sources updated to target openbrain
□ bubblefish doctor passes all checks
□ Real payload confirmed in Supabase
□ GCC added to permanent PATH (developer machines)
□ PERFORMANCE.md filled in with real numbers
□ Cloudflare Tunnel configured (optional)
□ Default rate limits raised to 2000/min
□ v1.0.1 tagged and pushed
□ Ready to start v2
```
