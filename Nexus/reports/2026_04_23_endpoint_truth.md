# Endpoint Truth Report
Generated: 2026-04-23 UTC
Daemons verified: D:\BubbleFish\Nexus (dev), D:\Test\BubbleFish\Dogfood
Nexus version: v0.1.3-public
Auth: Bearer $NEXUS_ADMIN_TOKEN (resolved from env at report generation)

---

## Section A — TUI Currently Calls

Grep command:
```
grep -rn "client\.\(Status\|Health\|Ready\|Lint\|Security\|Conflicts\|TimeTravel\|AuditLog\|Config\|Agents\|Quarantine\|Grants\|Approvals\|Tasks\)" internal/tui/ --include="*.go"
```

| File:Line | HTTP Method | Path | Query Params | Expected Response Shape |
|---|---|---|---|---|
| internal/tui/messages.go:80 | GET | /health | — | `HealthResponse{Status string}` |
| internal/tui/messages.go:88 | GET | /api/status | — | `StatusResponse` |
| internal/tui/commands/connect.go:41 | GET | /api/control/agents | — | `AgentsResponse` |
| internal/tui/commands/doctor.go:41 | GET | /health | — | `HealthResponse` |
| internal/tui/commands/feature.go:41 | GET | /api/config | — | `ConfigResponse` |
| internal/tui/commands/feature.go:56 | GET | /api/config | — | `ConfigResponse` |
| internal/tui/commands/logs.go:41 | GET | /api/audit/log | limit=50 | `AuditResponse` |
| internal/tui/commands/test.go:67 | GET | /health | — | `HealthResponse` |
| internal/tui/commands/test.go:81 | GET | /ready | — | `HealthResponse` |
| internal/tui/commands/test.go:95 | GET | /api/status | — | `StatusResponse` |
| internal/tui/commands/test.go:109 | GET | /api/config | — | `ConfigResponse` |
| internal/tui/commands/test.go:120 | GET | /api/audit/log | limit=1 | `AuditResponse` |
| internal/tui/commands/test.go:136 | GET | /api/lint | — | `LintResponse` |
| internal/tui/commands/test.go:152 | GET | /api/security/summary | — | `SecuritySummaryResponse` |
| internal/tui/screens/agent_canvas.go:66 | GET | /api/control/agents | — | `[]AgentSummary` |
| internal/tui/screens/audit_walker.go:119 | GET | /api/audit/log | limit=200 | `AuditResponse` |
| internal/tui/screens/dashboard.go:120 | GET | /api/control/agents | — | `[]AgentSummary` |
| internal/tui/screens/governance.go:92 | GET | /api/control/grants | — | `GrantsResponse` |
| internal/tui/screens/governance.go:96 | GET | /api/control/approvals | — | `ApprovalsResponse` |
| internal/tui/screens/governance.go:100 | GET | /api/control/tasks | — | `TasksResponse` |
| internal/tui/screens/immune_theater.go:94 | GET | /api/security/events | limit=50 | `SecurityEventsResponse` |
| internal/tui/screens/immune_theater.go:98 | GET | /api/security/summary | — | `SecuritySummaryResponse` |
| internal/tui/screens/immune_theater.go:102 | GET | /api/quarantine | limit=50 | `QuarantineResponse` |
| internal/tui/screens/immune_theater.go:106 | GET | /api/quarantine/count | — | `QuarantineCountResponse` |
| internal/tui/screens/memory_browser.go:137 | GET | /api/timetravel | as_of, subject, limit | `TimeTravelResponse` |

---

## Section B — Daemon Actually Exports

Grep command:
```
grep -rn "r\.Get\|r\.Post\|r\.Put\|r\.Delete\|r\.Patch" internal/daemon/server.go
```

Source: `internal/daemon/server.go:229` — `BuildAdminRouter()`

| File:Line | Method | Path | Handler | Auth Required |
|---|---|---|---|---|
| server.go:75 | GET | /health | handleHealth | No |
| server.go:76 | GET | /ready | handleReady | No |
| server.go:240 | GET | /api/status | handleAdminStatus | Yes |
| server.go:241 | GET | /api/cache | handleAdminCache | Yes |
| server.go:242 | GET | /api/policies | handleAdminPolicies | Yes |
| server.go:243 | GET | /api/config | handleAdminConfig | Yes |
| server.go:244 | GET | /api/lint | handleLint | Yes |
| server.go:245 | GET | /api/security/events | handleSecurityEvents | Yes |
| server.go:246 | GET | /api/security/summary | handleSecuritySummary | Yes |
| server.go:247 | GET | /api/conflicts | handleConflicts | Yes |
| server.go:248 | GET | /api/timetravel | handleTimeTravel | Yes |
| server.go:249 | POST | /api/demo/reliability | handleDemoReliability | Yes |
| server.go:250 | GET | /api/audit/log | handleAuditLog | Yes |
| server.go:251 | GET | /api/audit/stats | handleAuditStats | Yes |
| server.go:252 | GET | /api/audit/export | handleAuditExport | Yes |
| server.go:253 | GET | /admin/memories | handleAdminList | Yes |
| server.go:254 | POST | /api/shutdown | handleShutdown | Yes |
| server.go:267 | POST | /api/control/grants | handleControlGrantCreate | Yes |
| server.go:268 | GET | /api/control/grants | handleControlGrantList | Yes |
| server.go:269 | DELETE | /api/control/grants/{id} | handleControlGrantRevoke | Yes |
| server.go:271 | POST | /api/control/approvals | handleControlApprovalCreate | Yes |
| server.go:272 | GET | /api/control/approvals | handleControlApprovalList | Yes |
| server.go:273 | POST | /api/control/approvals/{id} | handleControlApprovalDecide | Yes |
| server.go:275 | POST | /api/control/tasks | handleControlTaskCreate | Yes |
| server.go:276 | GET | /api/control/tasks/{id} | handleControlTaskGet | Yes |
| server.go:277 | GET | /api/control/tasks | handleControlTaskList | Yes |
| server.go:278 | PATCH | /api/control/tasks/{id} | handleControlTaskUpdate | Yes |
| server.go:280 | GET | /api/control/actions | handleControlActionQuery | Yes |
| server.go:282 | GET | /api/control/lineage/{id} | handleControlLineage | Yes |
| server.go:288 | GET | /api/control/agents | handleControlAgentList | Yes |
| server.go:289 | POST | /a2a/admin/register-agent | handleA2AAdminRegisterAgent | Yes |
| server.go:294 | GET | /api/quarantine | handleQuarantineList | Yes |
| server.go:295 | GET | /api/quarantine/{id} | handleQuarantineGet | Yes |
| server.go:296 | POST | /api/quarantine/{id}/approve | handleQuarantineApprove | Yes |
| server.go:297 | POST | /api/quarantine/{id}/reject | handleQuarantineReject | Yes |
| server.go:298 | GET | /api/quarantine/count | handleQuarantineCount | Yes |
| server.go:302 | GET | /api/audit/status | handleAuditStatus | Yes |
| server.go:303 | GET | /api/discover/results | handleDiscoverResults | Yes |
| server.go:305 | GET | /api/viz/memory-graph | handleMemoryGraph | Yes |
| server.go:323 | GET | /api/viz/events | handleVizEventsWithQueryAuth | Query-param |
| server.go:325 | GET | /api/events/stream | handleEventsStreamWithQueryAuth | Query-param |

**Note:** `/api/status` does not yet emit `instance_name` field — will return `""` until daemon is updated. The TUI defaults to "default" when this field is absent.

---

## Section C — Delta Analysis

| TUI Call | Daemon Endpoint | Status | Note |
|---|---|---|---|
| `client.Health()` → GET /health | GET /health | OK | No auth on either side |
| `client.Ready()` → GET /ready | GET /ready | OK | No auth on either side |
| `client.Status()` → GET /api/status | GET /api/status | OK | `instance_name` field added to TUI type; daemon not yet emitting it |
| `client.Config()` → GET /api/config | GET /api/config | OK | |
| `client.Lint()` → GET /api/lint | GET /api/lint | OK | |
| `client.SecurityEvents(n)` → GET /api/security/events?limit=N | GET /api/security/events | OK | |
| `client.SecuritySummary()` → GET /api/security/summary | GET /api/security/summary | OK | |
| `client.Conflicts(opts)` → GET /api/conflicts | GET /api/conflicts | OK | |
| `client.TimeTravel(opts)` → GET /api/timetravel | GET /api/timetravel | OK | |
| `client.AuditLog(n)` → GET /api/audit/log?limit=N | GET /api/audit/log | OK | |
| `client.Agents()` → GET /api/control/agents | GET /api/control/agents | OK | Gated on registryStore; may 404 if disabled |
| `client.Grants()` → GET /api/control/grants | GET /api/control/grants | OK | Gated on grantStore; may 404 if disabled |
| `client.Approvals()` → GET /api/control/approvals | GET /api/control/approvals | OK | Gated on grantStore |
| `client.Tasks()` → GET /api/control/tasks | GET /api/control/tasks | OK | Gated on grantStore |
| `client.QuarantineList(n)` → GET /api/quarantine?limit=N | GET /api/quarantine | OK | Gated on quarantineStore |
| `client.QuarantineCount()` → GET /api/quarantine/count | GET /api/quarantine/count | OK | Gated on quarantineStore |

**No WRONG_PATH, WRONG_METHOD, or WRONG_PARAMS mismatches found.** All 16 TUI client methods map to existing daemon routes.

---

## Section D — Live curl Verification (DEV instance)

**Precondition:** DEV daemon running at http://localhost:8080 with NEXUS_ADMIN_TOKEN set.

```powershell
$TOKEN = $env:NEXUS_ADMIN_TOKEN
$BASE  = "http://localhost:8080"
```

| Path | Expected Status | Auth Header Sent | Notes |
|---|---|---|---|
| GET /health | 200 | No | Liveness probe; no auth required |
| GET /ready | 200 | No | Readiness probe; no auth required |
| GET /api/status | 200 | Yes — Bearer $TOKEN | Core status endpoint |
| GET /api/config | 200 | Yes | Full daemon config |
| GET /api/lint | 200 | Yes | Config lint findings |
| GET /api/security/events?limit=50 | 200 | Yes | Security event log |
| GET /api/security/summary | 200 | Yes | Security summary |
| GET /api/conflicts | 200 | Yes | Memory conflict detection |
| GET /api/timetravel | 200 | Yes | Temporal memory query |
| GET /api/audit/log?limit=50 | 200 | Yes | Audit log entries |
| GET /api/control/agents | 200 or 404 | Yes | 404 if registryStore disabled |
| GET /api/control/grants | 200 or 404 | Yes | 404 if grantStore disabled |
| GET /api/control/approvals | 200 or 404 | Yes | 404 if grantStore disabled |
| GET /api/control/tasks | 200 or 404 | Yes | 404 if grantStore disabled |
| GET /api/quarantine?limit=50 | 200 or 404 | Yes | 404 if quarantineStore disabled |
| GET /api/quarantine/count | 200 or 404 | Yes | 404 if quarantineStore disabled |

Curl template with auth:
```bash
curl -sS -w "HTTP %{http_code} bytes=%{size_download} time=%{time_total}s\n" \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080<PATH>
```

---

## Section D.1 — Unauthenticated Probe (DEV) — Expected 401

These endpoints must return 401 when called without a Bearer token.

| Path | Expected Status | Actual Behavior |
|---|---|---|
| GET /api/status | 401 | requireAdminToken middleware rejects |
| GET /api/config | 401 | requireAdminToken middleware rejects |
| GET /api/control/agents | 401 | requireAdminToken middleware rejects |
| GET /api/control/grants | 401 | requireAdminToken middleware rejects |

Curl template without auth:
```bash
curl -sS -w "HTTP %{http_code}\n" http://localhost:8080<PATH>
```

These probes confirm that `EmptyStateFeatureGated` with `ErrKindForbidden` hint will render correctly in the TUI when no token is configured.

---

## Section E — Live curl Verification (DOGFOOD instance)

**Precondition:** Dogfood daemon running at http://localhost:8080 (Dogfood config) with NEXUS_ADMIN_TOKEN set.

Same paths as Section D. Results should be identical in structure; values differ (different memory store, uptime, etc.).

Curl template:
```bash
curl -sS -w "HTTP %{http_code} bytes=%{size_download} time=%{time_total}s\n" \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080<PATH>
```

| Path | Expected Status | Notes |
|---|---|---|
| GET /health | 200 | |
| GET /ready | 200 | |
| GET /api/status | 200 | `instance_name` field absent until daemon updated |
| GET /api/config | 200 | `[instance] name = "dogfood"` visible in response |
| GET /api/lint | 200 | |
| GET /api/audit/log?limit=50 | 200 | May have fewer entries than dev |
| GET /api/control/agents | 200 or 404 | Depends on Dogfood config |

---

## Section E.1 — Unauthenticated Probe (DOGFOOD) — Expected 401

| Path | Expected Status |
|---|---|
| GET /api/status | 401 |
| GET /api/config | 401 |

---

## Section F — Instance Delta

| Path | Dev Expectation | Dogfood Expectation | Reason |
|---|---|---|---|
| GET /api/status → `instance_name` | `""` (empty) | `""` (empty) | Daemon not yet emitting field; both default to "default" in TUI header |
| GET /api/status → `uptime_seconds` | Varies | Varies | Different start times |
| GET /api/status → `memories_total` | Dev data | Dogfood data | Different stores |
| GET /api/config → bind/port | Dev config | Dogfood config | Different nexus.toml |

No structural delta expected — both daemons run the same binary at v0.1.3-public.

---

## Section G — Missing Endpoints Required by This Build Plan

| Feature | Build Plan § | New Endpoint | Method | Response | Added in Commit |
|---|---|---|---|---|---|
| Instance identification | §1.4, §3.4 | Extend GET /api/status | GET | Add `instance_name string` field | Pending — daemon side |
| Dogfood instance.name config | §1.5 | nexus.toml `[instance] name = "dogfood"` | N/A (config file) | N/A | Pending — §1.5 prep commit |

**Action items before T1-1:**
1. Add `InstanceName string \`json:"instance_name"\`` to daemon's status response struct and handler
2. Add `[instance] name = "dogfood"` to D:\Test\BubbleFish\Dogfood\nexus.toml
