# BubbleFish Nexus — Complete Codebase Verification Audit
# Read-Only Investigation. Do NOT modify any code. Report findings only.
# Run from: D:\BubbleFish\Nexus

---

## Purpose

Verify which features have been fully implemented versus specced but not built.
This is a gap analysis, not a quality audit. For each feature, determine:
- EXISTS: package/files present, tests present, quality gate passes
- PARTIAL: some files present but incomplete or untested
- MISSING: not present at all

---

## Step 1 — Quality Gate Baseline

Run this first. Report exact output. If it fails, stop and report what fails.

```powershell
cd D:\BubbleFish\Nexus
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./... -race -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)" | Sort-Object
```

Report: total packages, any failures, any race reports.

---

## Step 2 — Package Inventory

List every package that exists under internal/ and cmd/:

```powershell
Get-ChildItem -Path "internal","cmd" -Recurse -Filter "*.go" |
  Select-Object DirectoryName -Unique |
  ForEach-Object { $_.DirectoryName.Replace("D:\BubbleFish\Nexus\","") } |
  Sort-Object
```

---

## Step 3 — Feature-by-Feature Verification

For each feature below, check the listed files and run the listed grep.
Report status as EXISTS / PARTIAL / MISSING with one sentence of evidence.

---

### [F-01] WAL Engine — CRC32 + HMAC + AES-256-GCM + Rotation + Crash Recovery
```powershell
Test-Path "internal\wal\wal.go"
Test-Path "internal\wal\rotation.go"
Select-String -Path "internal\wal\*.go" -Pattern "HMAC|hmac" -Quiet
Select-String -Path "internal\wal\*.go" -Pattern "AES|GCM|aes|gcm" -Quiet
Select-String -Path "internal\wal\*_test.go" -Pattern "TestIntegrityMAC|TestEncrypt|TestCrash|TestRotat" -List | Select-Object Filename
```

---

### [F-02] SQLite Destination
```powershell
Test-Path "internal\destination\sqlite.go"
Select-String -Path "internal\destination\sqlite.go" -Pattern "modernc.org/sqlite|modernc\.org" -Quiet
```

---

### [F-03] 6-Stage Retrieval Cascade
```powershell
Test-Path "internal\query\cascade.go"
Select-String -Path "internal\query\*.go" -Pattern "Stage[0-6]|stage[0-6]|stage_" -List | Select-Object Filename
```

---

### [F-04] Temporal Decay Reranking
```powershell
Select-String -Path "internal\query\*.go" -Pattern "temporal|decay|recency|final_score" -List | Select-Object Filename
```

---

### [F-05] LRU Cache
```powershell
Test-Path "internal\cache\cache.go"
Select-String -Path "internal\cache\*.go" -Pattern "LRU|lru|evict" -Quiet
```

---

### [F-06] Per-Source Policy Enforcement
```powershell
Test-Path "internal\policy\policy.go"
Select-String -Path "internal\policy\*.go" -Pattern "CanRead|CanWrite|can_read|can_write" -Quiet
```

---

### [F-07] Constant-Time Auth + Rate Limiting
```powershell
Select-String -Path "internal\daemon\*.go" -Pattern "ConstantTimeCompare|subtle" -List | Select-Object Filename
Test-Path "internal\ratelimit\ratelimit.go"
# If no internal\ratelimit, check daemon
Select-String -Path "internal\daemon\*.go" -Pattern "ratelimit|rate_limit|RateLimit" -List | Select-Object Filename
```

---

### [F-08] Prometheus Metrics
```powershell
Select-String -Path "internal\metrics\*.go" -Pattern "prometheus|NewCounter|NewGauge|NewHistogram" -Quiet
Select-String -Path "internal\daemon\*.go" -Pattern "prometheus|metrics\." -List | Select-Object Filename
```

---

### [F-09] Hot Reload (fsnotify)
```powershell
Select-String -Path "internal\hotreload\*.go","internal\daemon\*.go" -Pattern "fsnotify|watchLoop|Reload" -List | Select-Object Filename
```

---

### [F-10] 3-Stage Graceful Shutdown
```powershell
Select-String -Path "internal\daemon\*.go" -Pattern "stage.*1|stage.*2|stage.*3|drain|RequestShutdown" -List | Select-Object Filename
Test-Path "cmd\bubblefish\stop.go"
```

---

### [F-11] TLS/mTLS + Trusted Proxies (R-12)
```powershell
Select-String -Path "internal\daemon\*.go","internal\config\*.go" -Pattern "tls\.Config|ClientAuth|TrustedProxy|trusted_proxy|forwarded" -List | Select-Object Filename
Select-String -Path "internal\daemon\*_test.go" -Pattern "TestTLS|TestMTLS|TestTrustedProxy" -List | Select-Object Filename
```

---

### [F-12] Deployment Modes safe/balanced/fast (R-13)
```powershell
Select-String -Path "internal\config\*.go" -Pattern "applyMode|deployment_mode|safe|balanced|fast" -List | Select-Object Filename
Select-String -Path "internal\config\*_test.go" -Pattern "TestMode|TestDeployment" -List | Select-Object Filename
```

---

### [F-13] Config Lint (R-11)
```powershell
Test-Path "internal\lint\lint.go"
Test-Path "cmd\bubblefish\lint.go"
Select-String -Path "internal\daemon\*.go" -Pattern "/api/lint" -Quiet
```

---

### [F-14] Admin vs Data Token Separation (R-4)
```powershell
Select-String -Path "internal\daemon\*.go" -Pattern "wrong_token_class|TokenClass|token_class" -List | Select-Object Filename
Select-String -Path "internal\daemon\*_test.go" -Pattern "TestToken.*Class|TestAdmin.*Data|TestWrongToken" -List | Select-Object Filename
```

---

### [F-15] Bench Tooling (R-25)
```powershell
Test-Path "internal\bench\bench.go"
Test-Path "cmd\bubblefish\bench.go"
Select-String -Path "internal\bench\*_test.go" -Pattern "TestThroughput|TestLatency|TestEval" -List | Select-Object Filename
```

---

### [F-16] Backup / Restore (R-24)
```powershell
Test-Path "internal\backup\backup.go"
Test-Path "cmd\bubblefish\backup.go"
Select-String -Path "internal\backup\*_test.go" -Pattern "TestBackup|TestRestore" -List | Select-Object Filename
```

---

### [F-17] Retrieval Firewall Engine (R-31)
```powershell
Test-Path "internal\firewall\firewall.go"
Select-String -Path "internal\firewall\*.go" -Pattern "blocked_labels|PreQuery|PostFilter|BlockedLabels" -Quiet
Select-String -Path "internal\firewall\*_test.go" -Pattern "TestFirewall|TestBlocked" -List | Select-Object Filename
```

---

### [F-18] Interaction Log / Audit Engine Base (R-29)
```powershell
Test-Path "internal\audit\logger.go"
Test-Path "internal\audit\reader.go"
Test-Path "internal\audit\record.go"
Select-String -Path "internal\audit\*.go" -Pattern "InteractionRecord|AuditLogger|AuditReader" -Quiet
```

---

### [F-19] Interaction Log Hardening — Dual-File Shadow Write (R-29 Update)
```powershell
Select-String -Path "internal\audit\*.go" -Pattern "shadow|dual_write|DualWrite|interactions-shadow" -Quiet
Select-String -Path "internal\audit\*_test.go" -Pattern "TestShadow|TestDualWrite|TestCRC.*Fallback" -List | Select-Object Filename
```

---

### [F-20] Interaction Log Hardening — Separate Crypto Keys (R-29 Update)
```powershell
Test-Path "internal\audit\crypto.go"
Select-String -Path "internal\audit\crypto.go" -Pattern "mac_key_file|audit.*key|AuditHMAC" -Quiet
# Verify it is NOT importing WAL key directly
Select-String -Path "internal\audit\*.go" -Pattern "wal\..*[Kk]ey|walKey|WALKey" -Quiet
```
Expected: crypto.go EXISTS, audit has its own key loading, does NOT reuse WAL key variable.

---

### [F-21] Interaction Log Hardening — Rotation Marker + Crash-Safe Rotation
```powershell
Select-String -Path "internal\audit\*.go" -Pattern "rotation_marker|RotationMarker|rotation.*marker" -Quiet
Select-String -Path "internal\audit\*_test.go" -Pattern "TestRotat|TestCrash.*Rotat|TestMarker" -List | Select-Object Filename
```

---

### [F-22] Audit Query API (R-32)
```powershell
Select-String -Path "internal\daemon\*.go" -Pattern "/api/audit|audit.*handler|handleAudit" -List | Select-Object Filename
Select-String -Path "internal\daemon\*.go" -Pattern "handleAuditStats|handleAuditExport|handleAuditLog" -Quiet
```

---

### [F-23] Audit CLI Commands (R-33)
```powershell
Test-Path "cmd\bubblefish\audit.go"
Select-String -Path "cmd\bubblefish\audit.go" -Pattern "query|stats|export" -Quiet
```

---

### [F-24] OAuth 2.1 Server (Post-Build Add-On)
```powershell
Test-Path "internal\oauth\server.go"
Test-Path "internal\oauth\jwt.go"
Test-Path "internal\oauth\keys.go"
Test-Path "internal\oauth\store.go"
Test-Path "internal\oauth\authorize.go"
Test-Path "internal\oauth\token.go"
Test-Path "internal\oauth\metadata.go"
Test-Path "internal\oauth\oauth_test.go"
Select-String -Path "internal\daemon\*.go" -Pattern "oauth|OAuthServer|\.well-known" -Quiet
```

---

### [F-25] MCP Server — All Transports (POST/SSE/CORS/?key=)
```powershell
Test-Path "internal\mcp\server.go"
Select-String -Path "internal\mcp\server.go" -Pattern "GET.*mcp|SSE|EventStream|OPTIONS|CORS|session_id|\?key=" -List | Select-Object LineNumber,Line
```

---

### [F-26] MCP stdio Bridge (MCPB)
```powershell
Test-Path "cmd\bubblefish\mcp_stdio.go"
Select-String -Path "cmd\bubblefish\mcp_stdio.go" -Pattern "TrimRight|forward|scanner|bufio" -Quiet
```

---

### [F-27] bubblefish status --paths
```powershell
Test-Path "cmd\bubblefish\status.go"
Select-String -Path "cmd\bubblefish\status.go" -Pattern "\-\-paths|paths.*flag|ShowPaths" -Quiet
```

---

### [F-28] BUBBLEFISH_HOME / --home flag
```powershell
Select-String -Path "internal\config\*.go" -Pattern "BUBBLEFISH_HOME|ConfigDir" -List | Select-Object Filename
Select-String -Path "cmd\bubblefish\install.go" -Pattern "\-\-home|BUBBLEFISH_HOME" -Quiet
```

---

### [F-29] Install token generation (bfn_mcp_ on fresh install)
```powershell
Select-String -Path "cmd\bubblefish\install.go" -Pattern "bfn_mcp_|mcpKey|generateKey" -Quiet
Select-String -Path "cmd\bubblefish\install.go" -Pattern "source_name.*default" -Quiet
```

---

### [F-30] Config Signing (R-3)
```powershell
Test-Path "cmd\bubblefish\sign_config.go"
Select-String -Path "internal\config\*.go" -Pattern "sign|signature|VerifySignature|SignConfig" -List | Select-Object Filename
Select-String -Path "internal\config\*_test.go" -Pattern "TestSign|TestVerif" -List | Select-Object Filename
```

---

### [F-31] Provenance Fields actor_type / actor_id (R-5)
```powershell
Select-String -Path "internal\daemon\*.go" -Pattern "actor_type|actor_id|ActorType|ActorID" -List | Select-Object Filename
Select-String -Path "internal\destination\sqlite.go" -Pattern "actor_type|actor_id" -Quiet
```

---

### [F-32] Retrieval Profiles fast/balanced/deep (R-6)
```powershell
Select-String -Path "internal\query\*.go" -Pattern "profile|Profile|fast|balanced|deep" -List | Select-Object Filename
Select-String -Path "internal\query\*_test.go" -Pattern "TestProfile|TestFast|TestDeep" -List | Select-Object Filename
```

---

### [F-33] Tiered Temporal Decay per-destination (R-7)
```powershell
Select-String -Path "internal\query\*.go" -Pattern "tiered|per.*dest|decay.*dest|TieredDecay" -Quiet
Select-String -Path "internal\config\*.go" -Pattern "decay_mode|exponential|step_mode|TieredDecay" -Quiet
```

---

### [F-34] Semantic Short-Circuit / Fast Path (R-8)
```powershell
Select-String -Path "internal\query\*.go" -Pattern "short.circuit|ShortCircuit|fast.path|FastPath|exact.*subject.*bypass" -Quiet
```

---

### [F-35] WAL Health Watchdog (R-9)
```powershell
Select-String -Path "internal\wal\*.go" -Pattern "watchdog|Watchdog|diskSpace|walHealth" -Quiet
Select-String -Path "internal\daemon\*.go" -Pattern "WALWatchdog|walWatchdog|startWatchdog" -Quiet
```

---

### [F-36] Consistency Assertions (R-10)
```powershell
Select-String -Path "internal\consistency\*.go" -Pattern "ConsistencyScore|SampleWAL|consistency_score" -Quiet
# If no internal\consistency, check daemon
Select-String -Path "internal\daemon\*.go" -Pattern "consistency|ConsistencyScore" -List | Select-Object Filename
```

---

### [F-37] Event Sink / Webhooks (R-19)
```powershell
Select-String -Path "internal\events\*.go","internal\daemon\*.go" -Pattern "webhook|EventSink|event_sink|sink" -List | Select-Object Filename
```

---

### [F-38] Pipeline Visualization / Black Box Mode (R-21)
```powershell
Select-String -Path "internal\viz\*.go","internal\daemon\*.go" -Pattern "pipeline.*viz|BlackBox|black_box|pipelineSSE" -List | Select-Object Filename
```

---

### [F-39] Conflict Inspector / Time-Travel (R-22)
```powershell
Select-String -Path "internal\daemon\*.go" -Pattern "/api/timetravel|/api/conflicts|TimeTravel|ConflictInspector" -Quiet
```

---

### [F-40] Debug Stages _nexus.debug (R-23)
```powershell
Select-String -Path "internal\query\*.go","internal\daemon\*.go" -Pattern "_nexus\.debug|nexus_debug|DebugStages" -Quiet
```

---

### [F-41] Reliability Demo CLI / bubblefish demo (R-26)
```powershell
Test-Path "cmd\bubblefish\demo.go"
Test-Path "internal\demo\demo.go"
```

---

### [F-42] Security Dashboard Tab (R-27)
```powershell
Select-String -Path "internal\web\*.go","internal\daemon\*.go" -Pattern "security.*tab|SecurityTab|auth.*failure.*dashboard" -Quiet
```

---

### [F-43] bubblefish dev command (R-16)
```powershell
Test-Path "cmd\bubblefish\dev.go"
Select-String -Path "cmd\bubblefish\main.go" -Pattern '"dev"' -Quiet
```

---

### [F-44] Postgres Install Profile (R-15)
```powershell
Select-String -Path "cmd\bubblefish\install.go" -Pattern "\-\-dest.*postgres|\-\-profile.*postgres|postgres.*install" -Quiet
```

---

### [F-45] Audit Rate Limiting on stats/export endpoints
```powershell
Select-String -Path "internal\daemon\audit_handlers.go" -Pattern "auditRateLimiter|RateLimit|Allow()" -List | Select-Object LineNumber,Line
```
Expected: all three handlers (handleAuditLog, handleAuditStats, handleAuditExport) show rate limit check.

---

## Step 4 — Sensitivity Labels (R-30)
```powershell
Select-String -Path "internal\destination\sqlite.go" -Pattern "sensitivity|sensitivity_label|SensitivityLabel" -Quiet
Select-String -Path "internal\firewall\*.go" -Pattern "blocked_labels|sensitivity|SensitivityLabel" -Quiet
```

---

## Step 5 — go.mod Dependency Check

Verify key dependencies are present:
```powershell
Select-String -Path "go.mod" -Pattern "golang-jwt|jwt" -Quiet
Select-String -Path "go.mod" -Pattern "modernc.org/sqlite" -Quiet
Select-String -Path "go.mod" -Pattern "go-chi/chi" -Quiet
Select-String -Path "go.mod" -Pattern "prometheus" -Quiet
Select-String -Path "go.mod" -Pattern "fsnotify" -Quiet
```

---

## Step 6 — Final Report

Produce a table in this exact format:

```
| Feature | Status | Evidence |
|---------|--------|----------|
| [F-01] WAL Engine | EXISTS | wal.go, rotation.go, 34 tests pass |
| [F-02] SQLite Destination | EXISTS | sqlite.go present |
| ... | ... | ... |
```

Status values: EXISTS / PARTIAL / MISSING

At the end, provide two summary lists:

CONFIRMED BUILT (all files present, tests exist):
- List each feature ID and name

GAPS (PARTIAL or MISSING):
- List each feature ID, name, and exactly what is missing
  (e.g., "files present but no tests", "package directory empty", "not found")

Do not editorialize. Do not suggest fixes. Report only what exists.
