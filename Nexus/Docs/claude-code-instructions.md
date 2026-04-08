# Claude Code Instructions — Dashboard Wiring (Phases D-1 → D-8)

This document is the **operator runbook** for wiring the BubbleFish Nexus Go
daemon to the dashboard so every panel populates from a real source. It is
designed to be executed phase-by-phase in Claude Code, one command at a time,
with mandatory testing between phases per the project's discipline of
preventing regression cascades from batch AI fixes.

**Read first:**
- `dashboard-contract.md` — the canonical JSON shape every endpoint must return
- `nexus-dashboard-v4.html` — the dashboard front-end (CONFIG block at top)

**Companion to:** the existing 45-phase R-build (`R-1` through `R-45`) plus
Phase 0 sub-phases (`0A`–`0D`). These D-phases assume R-1 through R-22 are
complete (daemon, WAL, policy engine, cache, retrieval cascade, viz channel,
conflict inspector, security events, lint).

---

## Operating discipline (from project memory)

- **One PowerShell command at a time.** Paste the command, wait for output,
  paste the output back to Claude Code for diagnosis.
- **Mandatory test between phases.** Each phase has a `Quality Gate` block.
  All three gates must pass before starting the next phase. Do not proceed on
  partial green.
- **Mode selection:**
  - Foundation phases (D-1 ledger, D-2 router scaffolding): **default mode**
  - Implementation phases (D-3 → D-7): **`acceptEdits` mode**
  - Verification phase (D-8): **default mode**
  - Never use `dangerously-skip-permissions`.
- **Model:** Opus 4.6 for every D-phase.
- **Batch size:** each phase touches at most ~3 files. If Claude Code wants to
  edit more, stop it and split the phase.

---

## Pre-flight check

Before starting D-1, verify the build is currently green:

```powershell
cd D:\BubbleFish\Nexus
go build ./...
go vet ./...
go test ./... -count=1
```

All three must succeed. If any fail, stop and fix before starting D-phases.
Do NOT attempt to combine D-phases with build fixes.

**Note on `-race`:** Per project memory, Go 1.26.1 has a linker bug affecting
`modernc.org/sqlite` with `-race`. Skip `-race` until Go 1.26.2 ships.
Document this in your test commands.

---

## Phase D-1 — Admin endpoint contract test fixtures

**Goal:** Lock the response shapes for every dashboard endpoint into Go test
fixtures so the implementation phases have something to compile against.

**Mode:** default
**Files touched:** `internal/api/contract_test.go` (new),
`internal/api/testdata/dashboard/*.json` (new, 8 files)

**Claude Code prompt:**

```
Read dashboard-contract.md from the project root. For EVERY documented endpoint
in that file (status, cache, policies, security/summary, security/events, lint,
conflicts, viz/events), create a JSON fixture file under
internal/api/testdata/dashboard/ with the EXACT example response body shown in
the contract. File names: status.json, cache.json, policies.json,
security_summary.json, security_events.json, lint.json, conflicts.json,
viz_event.json (single SSE frame).

Then create internal/api/contract_test.go with a TestDashboardContractFixtures
function that:
  1. Loads each fixture file
  2. Unmarshals into json.RawMessage to verify it parses
  3. Defines a Go struct matching the documented shape for each endpoint
  4. Re-unmarshals into the typed struct to verify field types match

Do not implement any handlers yet. Do not modify any existing files outside
internal/api/. Stop after creating these files.
```

**Quality gate:**

```powershell
go build ./...
go vet ./...
go test ./internal/api/ -run TestDashboardContractFixtures -count=1 -v
```

The test must pass. If JSON parsing fails on any fixture, fix the fixture
(not the test) — the contract is canonical.

---

## Phase D-2 — Admin router scaffolding

**Goal:** Wire the admin endpoint URLs into the daemon's HTTP router with
stub handlers that return the test fixtures verbatim. This proves the routing
works before the real handlers exist.

**Mode:** default
**Files touched:** `internal/daemon/admin_routes.go` (new),
`internal/daemon/server.go` (mount the routes)

**Claude Code prompt:**

```
Create internal/daemon/admin_routes.go that registers these admin routes on
the existing chi router from internal/daemon/server.go (do NOT create a new
router):

  GET  /api/status            → adminStatusHandler
  GET  /api/cache             → adminCacheHandler
  GET  /api/policies          → adminPoliciesHandler
  GET  /api/security/summary  → adminSecuritySummaryHandler
  GET  /api/security/events   → adminSecurityEventsHandler
  GET  /api/lint              → adminLintHandler
  GET  /api/conflicts         → adminConflictsHandler
  GET  /api/viz/events        → adminVizEventsHandler  (SSE)

Each handler must:
  1. Verify admin token via the existing requireAdminToken middleware
     (or accept ?token= query param for /api/viz/events ONLY)
  2. Set Content-Type: application/json (or text/event-stream for SSE)
  3. For now, read the corresponding fixture from internal/api/testdata/dashboard/
     using //go:embed and write it to the response

The viz/events SSE handler should write the single fixture frame, then
sleep 5 seconds, then write it again, then close. This is the scaffold; the
real implementation is Phase D-7.

Modify internal/daemon/server.go to call admin_routes.Register(r, deps) where
deps is the existing daemon struct. Do not modify any other files.
```

**Quality gate:**

```powershell
go build ./...
go vet ./...
go test ./internal/daemon/ -count=1
```

Then a manual smoke test in a separate PowerShell window:

```powershell
# Start the daemon
.\bin\bubblefish.exe start

# In another window, verify each endpoint returns the fixture:
$token = (Get-Content $env:USERPROFILE\.bubblefish\Nexus\admin_token.txt)
$h = @{ Authorization = "Bearer $token" }

Invoke-RestMethod http://127.0.0.1:8081/api/status            -Headers $h
Invoke-RestMethod http://127.0.0.1:8081/api/cache             -Headers $h
Invoke-RestMethod http://127.0.0.1:8081/api/policies          -Headers $h
Invoke-RestMethod http://127.0.0.1:8081/api/security/summary  -Headers $h
Invoke-RestMethod http://127.0.0.1:8081/api/security/events   -Headers $h
Invoke-RestMethod http://127.0.0.1:8081/api/lint              -Headers $h
Invoke-RestMethod http://127.0.0.1:8081/api/conflicts         -Headers $h
```

Every call must return the fixture content with HTTP 200. Stop the daemon
before proceeding to D-3.

---

## Phase D-3 — Live `/api/status` from real daemon state

**Goal:** Replace the `/api/status` fixture stub with real values pulled from
the running daemon.

**Mode:** acceptEdits
**Files touched:** `internal/daemon/admin_routes.go`,
`internal/daemon/admin_status.go` (new)

**Claude Code prompt:**

```
Replace the adminStatusHandler stub in internal/daemon/admin_routes.go with a
real implementation that returns the live state. Move the heavy logic into a
new file internal/daemon/admin_status.go.

The response struct must match the dashboard-contract.md /api/status shape
EXACTLY — same field names, same nesting, same types. The Go struct should
use json:"snake_case" tags.

Field sources:
  version                      → from internal/version.Version constant
  uptime_seconds               → time.Since(daemon.startedAt).Seconds()
  pid                          → os.Getpid()
  bind                         → daemon.config.Daemon.Bind
  web_port                     → daemon.config.Daemon.Web.Port
  memory_resident_bytes        → runtime.ReadMemStats — Sys field is fine for v1
  goroutines                   → runtime.NumGoroutine()
  queue_depth                  → daemon.queue.Depth()  (existing method from R-3)
  wal.pending_entries          → daemon.wal.PendingCount()  (existing from R-7)
  wal.healthy                  → daemon.wal.Healthy()  (existing from R-9)
  wal.last_checkpoint_seconds_ago → time.Since(daemon.wal.LastCheckpoint()).Seconds()
  wal.integrity_mode           → daemon.config.Daemon.WAL.Integrity.Mode  ("crc32" or "mac")
  wal.current_segment          → daemon.wal.CurrentSegment()  (filename only, not full path)
  consistency_score            → daemon.consistency.LatestScore()  (existing from R-10)
  destinations                 → iterate daemon.destinations, return name + Healthy() + LastError()
  cache.hit_rate               → see Phase D-4 — for now return 0.0
  cache.exact_rate             → 0.0
  cache.semantic_rate          → 0.0
  sources_total                → len(daemon.sources)
  memories_total               → daemon.destinations[primary].MemoryCount()
                                 (add MemoryCount() to Destination interface if missing;
                                 SQLite impl: SELECT COUNT(*) FROM memories)

If MemoryCount() does not exist on the Destination interface, ADD it as a
required method. Update the SQLite destination to implement it. If Postgres
or OpenBrain destinations exist, return 0 for now and add a TODO comment.

Update the contract test in internal/api/contract_test.go to also unmarshal
the LIVE response from a test daemon instance into the typed struct.
```

**Quality gate:**

```powershell
go build ./...
go vet ./...
go test ./internal/daemon/ -count=1
go test ./internal/api/ -count=1
```

Manual verification:

```powershell
.\bin\bubblefish.exe start
# In another window:
Invoke-RestMethod http://127.0.0.1:8081/api/status -Headers $h | ConvertTo-Json -Depth 5
```

Verify: `pid` matches the daemon process PID, `uptime_seconds` is small and
growing, `memories_total` matches `SELECT COUNT(*) FROM memories` against the
SQLite file directly.

---

## Phase D-4 — Live `/api/cache` from cache stats

**Goal:** Replace the `/api/cache` stub with live values from the existing
cache layer (Stage 1 + Stage 2).

**Mode:** acceptEdits
**Files touched:** `internal/daemon/admin_routes.go`,
`internal/daemon/admin_cache.go` (new), possibly `internal/cache/cache.go` (add Stats())

**Claude Code prompt:**

```
Replace the adminCacheHandler stub with a real implementation. Move logic to
internal/daemon/admin_cache.go.

Response shape must match dashboard-contract.md /api/cache exactly.

Field sources (the cache layer was built in Phase R-12 / R-13):
  exact.hits             → cache.ExactHits()       (counter from existing metric)
  exact.misses           → cache.ExactMisses()
  exact.hit_rate         → ExactHits / (ExactHits + ExactMisses), 0.0 if denominator is 0
  semantic.hits          → cache.SemanticHits()
  semantic.misses        → cache.SemanticMisses()
  semantic.hit_rate      → similar
  misses_total           → cache.TotalMisses()
  evictions_total        → cache.Evictions()
  capacity               → daemon.config.Cache.MaxEntries
  used                   → cache.Size()
  watermark              → fmt.Sprintf("%d / %d", used, capacity)

If the cache package does not expose these getters yet, ADD a Stats() method
that returns a single CacheStats struct with all fields. Wire the
adminCacheHandler to call Stats() once. Do not call atomics individually for
each field — that creates race-window inconsistencies in the response.

Also update adminStatusHandler to fill in the cache.* nested object using
the same Stats() call. The status endpoint and the cache endpoint must agree.
```

**Quality gate:**

```powershell
go build ./...
go vet ./...
go test ./internal/cache/ -count=1
go test ./internal/daemon/ -count=1
```

Manual:

```powershell
.\bin\bubblefish.exe start
# Run a few queries to populate cache
$dataKey = (Get-Content $env:USERPROFILE\.bubblefish\Nexus\sources\claude-desktop\api_key.txt)
$dh = @{ Authorization = "Bearer $dataKey" }
1..20 | ForEach-Object { Invoke-RestMethod "http://127.0.0.1:8080/query/sqlite?limit=5&q=test" -Headers $dh | Out-Null }

# Then check cache stats
Invoke-RestMethod http://127.0.0.1:8081/api/cache -Headers $h | ConvertTo-Json -Depth 5
```

Verify: hits + misses across both layers > 0, hit_rate is between 0 and 1.

---

## Phase D-5 — Live `/api/policies` from compiled policy summary

**Goal:** Return the actual compiled policies from disk.

**Mode:** acceptEdits
**Files touched:** `internal/daemon/admin_routes.go`,
`internal/daemon/admin_policies.go` (new)

**Claude Code prompt:**

```
Replace the adminPoliciesHandler stub. Move logic to admin_policies.go.

Response shape: dashboard-contract.md /api/policies. The "sources" array must
contain one entry per source registered in daemon.sources, populated from the
COMPILED policy in compiled/sources/<source>.json (NOT from the raw TOML —
the dashboard reflects the effective policy).

Field sources:
  source                    → source.ID
  can_read                  → source.Policy.CanRead
  can_write                 → source.Policy.CanWrite
  allowed_destinations      → source.Policy.AllowedDestinations
  max_results               → source.Policy.MaxResults
  max_response_bytes        → source.Policy.MaxResponseBytes
  rate_limit_per_min        → source.Policy.RateLimitPerMin
  policy_hash               → first 8 hex chars of SHA-256 of the compiled
                              policy JSON for this source

Use crypto/sha256, encode hex with encoding/hex, take [:8] of the result.

Order the sources alphabetically by ID for stable rendering.
```

**Quality gate:**

```powershell
go build ./...
go vet ./...
go test ./internal/daemon/ -count=1
```

Manual:

```powershell
.\bin\bubblefish.exe start
Invoke-RestMethod http://127.0.0.1:8081/api/policies -Headers $h | ConvertTo-Json -Depth 5
```

Verify: every source you have configured shows up, policy_hash is 8 hex chars,
all the limits match what you have in the source TOML files.

---

## Phase D-6 — Live `/api/security/summary`, `/api/security/events`, `/api/lint`

**Goal:** Wire all three security endpoints in a single phase. They share a
common dependency (the security event log + the metrics package).

**Mode:** acceptEdits
**Files touched:** `internal/daemon/admin_routes.go`,
`internal/daemon/admin_security.go` (new),
`internal/daemon/admin_lint.go` (new)

**Important:** This phase touches three handlers but they are tightly related
and small. If Claude Code wants to also touch the security_events package
internals or the lint package internals, STOP IT and split the phase. Limit
changes to the admin handlers and any minimal getter additions.

**Claude Code prompt:**

```
Implement three admin handlers in three new files.

1. internal/daemon/admin_security.go containing both:
   - adminSecuritySummaryHandler
   - adminSecurityEventsHandler

   adminSecuritySummaryHandler returns dashboard-contract.md /api/security/summary
   shape. Pull the totals from the existing Prometheus metrics:
     auth_failures_total      → bubblefish_auth_failures_total (sum across labels)
     policy_denials_total     → bubblefish_policy_denials_total (sum across labels)
     rate_limit_hits_total    → bubblefish_rate_limit_hits_total (sum)
     admin_calls_total        → bubblefish_admin_calls_total (sum)
   The by_source map: iterate over daemon.sources, fetch each metric value
   filtered by source label.

   The metrics package must expose a way to read counter values directly. If
   it does not, add a small ReadCounter(name string, labels map[string]string)
   helper to internal/metrics/metrics.go. Do not parse the Prometheus text
   format — read the underlying counter values directly.

   adminSecurityEventsHandler returns the most recent 50 entries from the
   security event log (Phase R-23 or wherever the JSON Lines log was wired).
   Read the file in reverse, newest first. If the log file does not exist,
   return {"events": []} and 200, NOT 404.

2. internal/daemon/admin_lint.go containing adminLintHandler.
   This should call the same code path that `bubblefish lint` uses (see
   internal/lint or wherever Phase R-19 put it). Return the warnings array
   directly. If the lint package does not expose a programmatic API and only
   has a CLI entry point, ADD a Run(config) function that returns
   ([]Warning, error) and have both the CLI and this handler call it.

Update adminRoutes registration to wire the three new handlers.
```

**Quality gate:**

```powershell
go build ./...
go vet ./...
go test ./internal/daemon/ -count=1
go test ./internal/lint/ -count=1
go test ./internal/metrics/ -count=1
```

Manual:

```powershell
.\bin\bubblefish.exe start

Invoke-RestMethod http://127.0.0.1:8081/api/security/summary -Headers $h | ConvertTo-Json -Depth 5
Invoke-RestMethod http://127.0.0.1:8081/api/security/events  -Headers $h | ConvertTo-Json -Depth 5
Invoke-RestMethod http://127.0.0.1:8081/api/lint             -Headers $h | ConvertTo-Json -Depth 5
```

Verify: lint returns at least the warnings you'd expect from your current
config. Trigger a 401 by hitting `/api/status` with no token; then check
`/api/security/events` shows the new auth_failure entry within 5 seconds.

---

## Phase D-7 — Live `/api/conflicts` and `/api/viz/events` SSE

**Goal:** Wire the read-only conflict inspector (Phase R-22 was already
implemented, this just exposes it via the admin endpoint) and the live SSE
event stream.

**Mode:** acceptEdits
**Files touched:** `internal/daemon/admin_routes.go`,
`internal/daemon/admin_conflicts.go` (new),
`internal/daemon/admin_viz.go` (new)

**Claude Code prompt:**

```
Implement two handlers.

1. internal/daemon/admin_conflicts.go containing adminConflictsHandler.
   Parse query params: source, actor_type, subject, from, to, limit (default 50).
   Call the existing conflicts package from Phase R-22. Map its result to the
   dashboard-contract.md /api/conflicts shape exactly. Field name notes:
     - The dashboard expects "id" as a stable string per conflict group.
       If the conflicts package returns groups without IDs, generate them as
       "cf_" + hex(sha256(subject + entity))[:6]
     - "group_size" must equal len(memories) and be at least 2
     - "memories" must be ordered newest-first by ts within the group
     - Top-level result is { "conflicts": [...] }

2. internal/daemon/admin_viz.go containing adminVizEventsHandler.
   This is the SSE handler. Per dashboard-contract.md it must:
     - Accept admin token from EITHER the Authorization header OR ?token= query
     - Set headers: Content-Type: text/event-stream, Cache-Control: no-cache,
       Connection: keep-alive
     - Subscribe to the existing viz channel from Phase R-21 (internal/viz)
     - For each event received from the channel, marshal to JSON and write
       as: "data: " + json + "\n\n"  (note: TWO newlines)
     - Flush after every write
     - On client disconnect (ctx.Done() OR write error), unsubscribe and exit
     - The viz channel is LOSSY by design (Phase R-21). Do not buffer events
       on the handler side — if a write would block, drop the event and
       increment bubblefish_visualization_events_dropped_total

   The event JSON shape must match dashboard-contract.md viz/events frame
   EXACTLY. Field-for-field. The internal viz event struct from Phase R-21
   may not match — add a translation function vizEventToDashboard(ev) that
   converts to the contract shape. Fields needed:
     ts (RFC3339), request_id, source, op (WRITE/QUERY uppercase),
     subject, actor_type, status (ALLOWED/FILTERED/DENIED uppercase),
     labels ([] not null), result_count (int), total_ms (float),
     stages (array OR null for WRITE)

   For QUERY events, stages must be a 6-element array even if some stages
   are skipped (set ms to 0 for skipped stages and hit:false). Only the
   stage that served the result has hit:true.

If multiple dashboard clients connect simultaneously, the daemon must
fan-out viz events to all of them. If Phase R-21 used a single-consumer
channel, change it to a publisher pattern (subscribe → channel out, with
the publisher closing channels on unsubscribe). Per Phase R-21 acceptance
criteria, "Consider using a fan-out pattern if multiple dashboard clients
connect simultaneously" — that's now required, not optional.
```

**Quality gate:**

```powershell
go build ./...
go vet ./...
go test ./internal/daemon/ -count=1
go test ./internal/viz/ -count=1
go test ./internal/conflicts/ -count=1 2>$null  # may not exist as separate pkg
```

Manual:

```powershell
.\bin\bubblefish.exe start

# Test conflicts (will be empty unless you have conflicting writes)
Invoke-RestMethod http://127.0.0.1:8081/api/conflicts -Headers $h | ConvertTo-Json -Depth 6

# Test SSE: this should stream events as you write/query
curl.exe -N -H "Authorization: Bearer $token" http://127.0.0.1:8081/api/viz/events
```

In a separate window, drive some traffic:

```powershell
$dataKey = (Get-Content $env:USERPROFILE\.bubblefish\Nexus\sources\claude-desktop\api_key.txt)
$dh = @{ Authorization = "Bearer $dataKey"; "Content-Type" = "application/json" }
1..30 | ForEach-Object {
  Invoke-RestMethod "http://127.0.0.1:8080/query/sqlite?limit=5" -Headers $dh | Out-Null
  Start-Sleep -Milliseconds 200
}
```

Each query should produce one `data: {...}\n\n` frame in the SSE curl window.
Verify the JSON matches dashboard-contract.md viz/events shape — every field
present, types correct, status is uppercase, labels is `[]` not `null`, stages
has 6 elements with one `hit: true`.

Test fan-out: open TWO curl SSE windows simultaneously, drive traffic, both
should receive events.

---

## Phase D-8 — End-to-end dashboard verification

**Goal:** Point the dashboard at the live daemon and verify every panel
populates with real data.

**Mode:** default
**Files touched:** `web/dashboard/nexus-dashboard-v4.html` (one line —
the CONFIG block)

**Claude Code prompt:**

```
Open web/dashboard/nexus-dashboard-v4.html and find the CONFIG block near the
top of the <script> section. Change:

  MOCK_MODE: true,

to:

  MOCK_MODE: false,

Do not change anything else. Do not modify ADMIN_TOKEN — that should be empty
in source and injected at runtime by the daemon when serving the dashboard
HTML (see below).

Then, in internal/daemon/dashboard_server.go (or wherever the dashboard HTML
is served — likely an embed.FS handler), modify the handler to:
  1. Read the embedded HTML into memory
  2. Replace the literal string ADMIN_TOKEN: '' with
     ADMIN_TOKEN: '<the actual admin token, JSON-string-escaped>'
  3. Serve the modified HTML

This ensures the browser running the dashboard authenticates as admin without
the operator having to manually paste the token into the source.

The token must be embedded server-side, not exposed via a separate endpoint —
treating it like a session injection.
```

**Quality gate:**

```powershell
go build ./...
go vet ./...
go test ./internal/daemon/ -count=1
.\bin\bubblefish.exe start
```

Then open `http://127.0.0.1:8081` in a browser. Verify, page by page:

**Control Plane (default view):**
- [ ] Total Memories shows the real count from the SQLite DB
- [ ] Throughput is 0 initially, ticks up when you drive query traffic
- [ ] WAL Pending shows 0 (assuming healthy daemon)
- [ ] Cache Hit Rate populates after a few queries
- [ ] Header shows real uptime, ticking
- [ ] Source cards show real W/R/D counters from actual policy + traffic
- [ ] Pipeline nodes light up when SSE events arrive
- [ ] Audit log panel scrolls live with real requests

**Audit Log:**
- [ ] Stats cards show non-zero values
- [ ] Filter dropdowns actually filter the live table
- [ ] Filtered/denied entries flash amber

**Security:**
- [ ] Counter cards show real metric values
- [ ] Policies table shows your actual configured sources
- [ ] Recent events panel shows entries from the security event log
- [ ] Lint panel shows your actual config warnings

**Pipeline:**
- [ ] Cache stats card shows live values from /api/cache
- [ ] Live cascade event stream populates from SSE
- [ ] Hero pipeline animates on each query

**Conflicts:**
- [ ] Empty state shows "No conflicts found" if you have none
- [ ] If you write conflicting memories (same subject, different content for
      the same entity), they should appear

**Settings:**
- [ ] All config rows show the values from your actual daemon.toml

**Witness Mode:**
- [ ] Logo center stays clear
- [ ] Memory cards spawn in the 5×2 grid (skipping center column)
- [ ] Bottom-left log panel streams real audit lines
- [ ] Bottom-right Nexus Console responds to commands; `status`, `wal`,
      `policy`, `security`, `conflicts`, `lint` all return real data

**Offline behavior:**
- [ ] Stop the daemon. Within ~5 seconds, the offline banner should appear
      at the top of the page: "⚠ daemon unreachable — using mock data"
- [ ] The dashboard should not go blank — mock data takes over
- [ ] Restart the daemon. The banner should disappear, real data resumes

---

## Failure modes and how to recover

**Build fails after a phase:** revert the phase's commits, re-read the prompt
for that phase, look for any "if X does not exist, add Y" instructions you
may have skipped. Re-run the phase from a clean state.

**Test fails on a contract mismatch:** the contract is canonical. Fix the
handler to match the contract, NOT the contract to match the handler. The
dashboard depends on the documented shape.

**Dashboard panel is blank but daemon endpoint returns 200:** open the
browser DevTools console. Look for a `MOCK: no handler for ...` warning
(means the dashboard is hitting an endpoint that isn't in the mock router and
the daemon isn't returning what it expects). Fix the endpoint to match the
contract shape.

**SSE events arrive in DevTools Network tab but pipeline doesn't animate:**
the event JSON is missing a field or has wrong types. Open one event, compare
field-by-field against dashboard-contract.md viz/events frame.

**Witness Mode terminal `status` command shows daemon as UNHEALTHY but the
daemon is running fine:** check that `/api/status` returns `wal.healthy: true`
as a literal boolean, not a string. The dashboard does `s.wal.healthy ?
'HEALTHY' : 'UNHEALTHY'` — Go's JSON encoder will produce the right boolean
if the struct field is `bool`, but a `string` field with value `"true"` will
also be truthy and render as HEALTHY incorrectly. Use bool.

---

## What this build plan does NOT do

- **Does not implement `/api/replay`, `/api/timetravel`, `/api/demo/reliability`,
  or `/metrics` parsing.** Those endpoints exist in the daemon per Tech Spec
  §12 but the dashboard doesn't currently consume them. They're future work.
- **Does not add policy editing.** The dashboard is read-only by design.
- **Does not change auth model.** Admin tokens stay strictly separated from
  data tokens per Tech Spec §8.1.
- **Does not add multi-tenant dashboards.** One daemon, one dashboard.

---

## Sign-off

This plan is complete when:

1. All 8 phases pass their quality gates
2. The end-to-end checklist in Phase D-8 is fully green
3. `dashboard-contract.md` and `nexus-dashboard-v4.html` are committed to
   the repo under `docs/` and `web/dashboard/` respectively
4. The README has a "Dashboard" section linking to both
5. The witness-mode demo works against a live daemon for at least 5
   continuous minutes without the offline banner appearing
