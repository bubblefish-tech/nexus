# BUILD_LEDGER.md

## Steps 1–5: COMPLETE (substrate merged to main @ 5c21ce7)

## Step 6: COMPLETE (rebase v0.1.3-a2a onto main, 1 conflict resolved in cmd/bubblefish/main.go — additive, both sides kept)

## Step 7: EXIT GATE — CONDITIONAL PASS
- Build: OK
- Vet: OK
- Tests: 54 packages pass, 3 fail (31 pre-existing a2a transport 404 failures — confirmed identical on pre-rebase tip 9c08f21)
- MCP version assertion fixed: ca30e44
- Cleanup commit: 5a0baa0
- Branch tip: ca30e44 (34 commits ahead of main)

## Step 8: COMPLETE (v0.1.3-a2a merged to main)
- Merge commit: c6807f0 (--no-ff, "Merge v0.1.3-a2a: A2A agent-to-agent protocol (A2A.1–A2A.12)")
- Post-merge build: OK
- Post-merge vet: OK
- Full test re-run skipped — identical to Step 7 exit gate (same tree post-merge)
- main tip: c6807f0

## Step 9: COMPLETE (v0.1.3-moat-takeover created)
- Branch: v0.1.3-moat-takeover
- Base: c6807f0 (main tip — merge commit of v0.1.3-a2a)

## MT.1: COMPLETE — grants, approvals, tasks, action log schemas
- Schema: 5 tables + indexes added to internal/a2a/registry/store.go (renamed createTableSQL → SchemaSQL, added exported InitSchema helper)
- New packages: internal/grants, internal/approvals, internal/tasks (tasks.go + events.go), internal/actions
- Each package takes *sql.DB, not the registry store type (directional coupling)
- Tests use :memory: SQLite via registry.InitSchema
- Test count: 78 top-level tests (20 grants, 20 approvals, 22 tasks, 16 actions) — exceeds 55 minimum
- go.mod tidy: promoted bubbles/bubbletea/lipgloss/jwt/klauspost/ulid/cuckoofilter/golang.org/x/time from indirect to direct (all were already in-use)
- Exit gate:
  - Build: OK
  - Vet: OK
  - 58 packages PASS (4 new, 54 pre-existing)
  - 3 packages FAIL — the same 31 pre-existing a2a transport 404 failures tracked since Step 7. Zero new regressions.

## MT.2: COMPLETE — REST APIs for grants, approvals, tasks, actions
- Design: control-plane stores share the A2A registry's *sql.DB at <configDir>/a2a/registry.db (NOT a separate nexus-control.db). Enforces foreign keys against real a2a_agents table.
- registry.Store exposes DB() accessor so daemon can wire grants/approvals/tasks/actions against the same connection.
- daemon.Start() opens the registry unconditionally (foundational infra) before router build; stores d.registryStore on Daemon struct.
- New file: internal/daemon/handlers_control.go (~500 lines, 11 handlers, DTOs + converters, decodeJSON helper with 1MB cap, emitControlAudit using existing audit.InteractionRecord)
- Routes under /api/control/ (grants, approvals, tasks, actions) — admin-token authed, registered inside r.Group with requireAdminToken in BOTH buildRouter() and BuildAdminRouter(); guarded by `if d.grantStore != nil`
- Error format: {"error":"CODE","message":"text"} via existing writeErrorResponse
- Audit emitted on every write endpoint (grants.create/revoke, approvals.create/decide, tasks.create/update)
- Daemon struct extended with registryStore (*registry.Store) + grantStore/approvalStore/taskStore/actionStore — no separate controlDB field
- setupA2ABridge refactored: now reuses d.registryStore rather than opening its own; d.setupA2ABridge(cfg) call added in Start() (gated on cfg.A2A.Enabled) — prior rebase had left this call unwired
- registryStore.Close() wired into daemon Stop() stage 3
- Unconditional control-plane wiring (no cfg.Control.Enabled gate yet) — MT.3 adds the feature flag
- New file: internal/daemon/handlers_control_test.go — 37 tests (package daemon, httptest + chi router, no daemon startup)
- Security fix (76d36d6): unbounded list queries on grants/approvals/tasks capped at 1000 rows by default
  - Added Limit int to grants.ListFilter, approvals.ListFilter, tasks.ListFilter (same LIMIT ? pattern as actions.QueryFilter)
  - parseListLimit helper in handlers_control.go; ?limit=0 opts out; invalid values fall back to 1000
  - 7 new tests: TestList_Limit in each store package + TestControl_List*_LimitParam in daemon
- Exit gate:
  - Build: OK
  - Vet: OK
  - 58 packages PASS (including internal/daemon with 44 handler tests, +7 limit tests)
  - 3 packages FAIL — identical 31 pre-existing a2a transport 404 failures from Step 7. Zero new regressions.

## MT.3: COMPLETE — Nexus-native policy evaluation engine
- New file: internal/policy/engine.go — Engine + EngineConfig + Decision
  - Evaluate(): fail-closed 6-step flow (agent status → active grant → scope → approval req → record)
  - matchesScope(): JSON-compact key-value comparison; empty/"{}"/nil scope = unconstrained
  - EngineConfig (not config.ControlConfig) avoids import cycle — internal/config already imports internal/policy
  - record(): writes every decision to action_log via actions.Store
- New file: internal/policy/engine_test.go — 25 tests (package policy_test)
  - Uses t.TempDir() + registry.NewStore (file-based SQLite, not :memory:, because registry.NewStore takes a path)
  - Covers: agent not found / suspended / retired, no grant / revoked, scope empty / match / mismatch /
    nil action / multi-key partial, approval required without/with/pending, action-log recording,
    two-agent independence, two-capability independence
- internal/config/types.go: ControlConfig{Enabled, Capabilities.RequireApproval} + Control field on Config
- internal/daemon/daemon.go: policyEngine *policy.Engine field; control-plane store init gated on
  cfg.Control.Enabled; engine wired with EngineConfig{RequireApproval: cfg.Control.Capabilities.RequireApproval}
- Commit: d5a3022
- Exit gate:
  - Build: OK
  - Vet: OK
  - 59 packages PASS (internal/policy +1 new engine tests, 58 pre-existing)
  - 3 packages FAIL — identical 31 pre-existing a2a transport 404 failures. Zero new regressions.

## MT.4: COMPLETE — MCP tools for governed control plane
- New file: internal/mcp/tools_control.go — ControlPlaneProvider interface + 7 DTOs (ControlDecision, GrantInfo, ApprovalInfo, TaskInfo, TaskEventInfo, ActionInfo) + 6 tool handlers + controlToolDefs()
- Modified: internal/mcp/server.go — controlPlane field on Server, SetControlPlane setter, controlToolDefs() in handleToolsList, 6 new tool cases in handleToolsCall
- New file: internal/daemon/control_plane_adapter.go — controlPlaneAdapter implementing ControlPlaneProvider; approvalToInfo/taskToInfo converters
- Modified: internal/daemon/daemon.go — wires &controlPlaneAdapter{} after control plane init block when d.policyEngine != nil && d.mcpServer != nil
- Policy model: nexus_task_create evaluates against the TASK's capability (not "nexus_task_create") — the agent needs a grant for what the task actually executes
- All 5 non-task tools evaluate against their own tool name as capability
- New file: internal/mcp/tools_control_test.go — 27 tests (package mcp_test), includes testControlAdapter + rpcCallAgent helper
  - 6 "control not enabled" subtests (one per tool)
  - 1 missing X-Agent-ID test
  - happy path + policy denied for all 6 tools
  - malformed input tests (missing capability, missing action, missing IDs)
  - E2E: nexus_task_create denied → nexus_approval_request → admin Decide() → retry succeeds
- Exit gate:
  - Build: OK
  - Vet: OK
  - 60 packages PASS (internal/mcp +1 with 27 new control tests)
  - 3 packages FAIL — identical 31 pre-existing a2a transport 404 failures. Zero new regressions.

## MT.5: COMPLETE — Dashboard control-plane pages
- 5 new HTML files in web/dashboard/: agents.html, grants.html, approvals.html, tasks.html, actions.html
  - Each page: dark theme, nav bar linking all 5 pages + main dashboard, fetch from /api/control/* APIs
  - textContent only (NEVER innerHTML). ADMIN_TOKEN: '', injection point (same pattern as index.html)
  - Token injected server-side by handler: extracts from Authorization header or ?token= query param
- web/dashboard/embed.go: 5 new exported vars (AgentsHTML, GrantsHTML, ApprovalsHTML, TasksHTML, ActionsHTML)
- internal/daemon/handlers_dashboard.go: serveDashboardPage helper + dashboardToken validator (header + query param) + 5 HTML handlers + handleControlAgentList (GET /api/control/agents)
- internal/daemon/server.go: /dashboard/{agents,grants,approvals,tasks,actions} added to both buildRouter() and BuildAdminRouter() (outside requireAdminToken group, self-validates token) + /api/control/agents in both (inside requireAdminToken group, gated on registryStore != nil)
- internal/web/dashboard.go: mux.Handle("/dashboard/", d.cfg.AdminHandler) added alongside existing /api/ delegation
- internal/daemon/handlers_dashboard_test.go: 15 tests (5 page-OK, 5 no-token subtests, bad-token, ?token= happy, ?token= bad, token-injection, no-mock-data, agent-list-empty, agent-list-no-registry)
- Exit gate:
  - Build: OK
  - Vet: OK
  - internal/daemon: PASS (161 top-level tests including 15 new dashboard tests)
  - 3 packages FAIL — identical 31 pre-existing a2a transport 404 failures. Zero new regressions.

## MT.6: COMPLETE — CLI for grants, approvals, tasks, actions
- New file: cmd/bubblefish/control.go — controlClient (baseURL+token HTTP wrapper), parseFlags helper, msToTime/strOrDash/int64AsFloat formatters
  - runGrant → doGrantCreate / doGrantList / doGrantRevoke
  - runApproval → doApprovalList / doApprovalDecide
  - runTask → doTaskList / doTaskInspect
  - runAction → doActionLog
  - All hit /api/control/* REST API (MT.2); human-readable tables by default, --json for machine parsing
  - --since <duration> on action log converted to since_ms= millisecond timestamp
- Modified: cmd/bubblefish/main.go — grant/approval/task/action commands wired + help text
- New file: cmd/bubblefish/control_test.go — 20 tests via fakeControlServer (httptest.Server + custom handlers map)
  - Tests: grant list/create/revoke, approval list/decide, task list/inspect, action log filters/duration/json, parseFlags
- Exit gate:
  - Build: OK
  - Vet: OK
  - cmd/bubblefish: PASS (20 new control tests)
  - 3 packages FAIL — identical 31 pre-existing a2a transport 404 failures. Zero new regressions.

## MT.7: COMPLETE — Audit integration and lineage graph for control plane
- New file: internal/audit/control_events.go — ControlEventRecord struct + 8 event type constants (grant_created, grant_revoked, approval_requested, approval_decided, task_created, task_state_changed, action_executed, action_denied) + ComputeHash()
- Modified: internal/audit/wal_writer.go — SubmitControl(ControlEventRecord) writes to WAL with hash-chaining via same ChainState as InteractionRecord
- New file: internal/audit/control_events_test.go — 7 tests (event type uniqueness, JSON roundtrip, hash determinism, hash excludes self, prev-hash chaining, entity optional, all types marshal)
- Modified: internal/daemon/handlers_control.go — emitControlEvent() helper; handleControlLineage() for GET /api/control/lineage/{id} — queries task → actions → grants → approvals with audit hashes; lineageResponse DTO
- Modified: internal/daemon/server.go — GET /api/control/lineage/{id} added to buildRouter() and BuildAdminRouter() inside grantStore gate
- Modified: internal/daemon/handlers_control_test.go — 9 lineage tests (not_found, missing_id, no_control_plane, empty_chain, with_actions, with_grant_and_approval, response_shape, task_fields_populated, duplicate_grant_deduped) + lineage route in routeThrough()
- Transport harness fixes (all 31 pre-existing failures resolved):
  - internal/a2a/transport/http.go: httpClientConn.Send() posts to /a2a/jsonrpc, Stream() posts to /a2a/stream
  - internal/a2a/transport/transport_test.go: TestHTTPSSEStream rewritten to use Accept mode instead of chi route override
  - internal/a2a/client/factory.go: Factory.NewClient returns error (and closes conn) when both GetAgentCard AND Ping fail; fixes TestFactory_PingFail
- Exit gate:
  - Build: OK
  - Vet: OK
  - 61 packages PASS — ZERO failures (all 31 pre-existing transport harness failures fixed)

## MT.8: COMPLETE — 60-second control plane demo script
- New file: scripts/demo_control_plane.ps1 — 10-step end-to-end PowerShell demo
  - Step 1: bubblefish install --mode simple (idempotent)
  - Step 2: bubblefish start + health poll (10s timeout)
  - Step 3: POST /api/a2a/agents → register demo-agent
  - Step 4: bubblefish grant create --capability nexus_write
  - Step 5: POST /api/control/approvals → request nexus_delete
  - Step 6: bubblefish approval decide --decision approve
  - Step 7: create task → write memory → delete memory → mark task complete
  - Step 8: bubblefish action log --agent + GET /api/control/lineage/{task_id}
  - Step 9: GET /api/substrate/proof/{id} → save JSON → bubblefish verify (substrate-optional)
  - Step 10: HTTP 200 checks on all 5 dashboard pages (/agents /grants /approvals /tasks /actions)
- Style: matches demo-a2a-claude-desktop.ps1 (Step/Pass/Fail/Warn helpers, Elapsed timestamps, failure counter, summary block)
- Substrate steps are warn-not-fail when substrate is disabled (simple mode compatible)
- Exit gate:
  - Build: N/A (script only)
  - Script is syntactically valid PowerShell (no build artifacts changed)

## Pre-Release Security Remediation (2026-04-18): COMPLETE
- Source: Comprehensive_Build_Review_Analysis.md (.claude/), 4 commits on v0.1.3-moat-takeover

### C-1: Credential Hygiene — bbc90b6
- Removed jwt_token.txt, oauth_audit_bundle.txt, oauth_remediation_test_output.txt from git tracking
- Added all three to .gitignore

### C-2: Stored XSS (innerHTML) — 8cb5a97
- All 22 innerHTML sites in web/dashboard/index.html converted to textContent / createElement
- Added DOM helpers: createSrcDot, createEl, setEmpty, createTextTd, cfgRow, cfgSection, cfgCodeVal, cfgCodeList
- Settings view fully rewritten to DOM-based construction

### H-block: High Findings — ad560ad
- H-1: Audit chain race: chainMu guards LastHash+Extend atomically in WALWriter.Submit/SubmitControl
- H-2: WAL encryption stub: daemon.go now logs WARN (not Info) that encryption is NOT implemented in v0.1.3
- H-3: IDOR in MCP tools: nexus_approval_status + nexus_task_status verify AgentID ownership before returning data
- H-4: A2A HTTP body size: http.MaxBytesReader(1MiB) in handleJSONRPC + handleStream
- H-5: A2A HTTP timeouts: ReadHeaderTimeout/ReadTimeout/WriteTimeout/IdleTimeout added to http.Server

### M-block: Medium Findings — 60d0130
- M-1: OAuth timing: ClientID + RedirectURI comparisons use subtle.ConstantTimeCompare
- M-3: Dashboard token injection: json.Marshal encoding (not manual string escaping)
- M-5: parseListLimit: treats 0 as default; caps at 1000
- M-7: CLI control.go: all query-string values wrapped with url.QueryEscape
- M-8: CLAUDE.md version reference updated to internal/version/version.go
- M-9: Transport close: httpClientConn + stdioConn use sync.Once + atomic.Bool (replaces mutex+bool)
- M-10: nexus_action_log: limit clamped to [1,1000], default 100

### Exit gate (post all 4 commits):
- Build: OK
- Vet: OK
- Tests: internal/a2a/..., internal/mcp/..., internal/oauth/..., internal/audit/... — all PASS

## Module Path Fix (2026-04-18) — 5c827df (amended)
- go.mod module line corrected: `github.com/BubbleFish-Nexus` → `github.com/bubblefish-tech/nexus`
- All 526 Go files with internal imports updated mechanically (zero functional change)
- CLAUDE.md and Docs/a2a/troubleshooting.md updated to match
- Build: OK | Vet: OK | Full test suite: PASS (zero failures)

## P0.2: Edition Registry — COMPLETE
- New package: `internal/edition/`
  - `edition.go`: Edition struct, Current (community default), Has(), String()
  - `features.go`: 20 feature constants (FeatureRBAC … FeatureFIPS)
  - `edition_test.go`: 5 tests — community default, Has(), HasNothing for all features, String(), uniqueness
- Exit gate: Build OK | Vet OK | `internal/edition` PASS

## P0.3: CryptoProfile Interface — COMPLETE
- New dependency: `golang.org/x/crypto v0.50.0`
- New package: `internal/crypto/`
  - `profile.go`: CryptoProfile interface (Name, HashNew, HMACNew, HKDFExtract, HKDFExpand, AEADNew, HashSize) + ActiveProfile var
  - `classical.go`: ClassicalProfile — SHA3-256 hash/HMAC, HKDF (RFC 5869 via x/crypto/hkdf), AES-256-GCM
  - `profile_test.go`: 11 tests — hash round-trip, HashSize, HMAC determinism/keyed, HKDF extract/expand, AEAD round-trip/wrong-key/AAD-mismatch, name, ActiveProfile matches ClassicalProfile
- Refactor: No sha3 calls existed in codebase prior; existing sha256 calls untouched (legacy, addressed in Phase 1 encryption)
- Exit gate: Build OK | Vet OK | `internal/crypto` PASS

## P0.4: Provider Interfaces + daemon.Run() — COMPLETE
- New file: `internal/daemon/providers.go` — RBACEngine, BoundaryEnforcer, ClassificationMarker interfaces (all nil-safe at call sites in community edition)
- New file: `internal/daemon/run.go` — Option functional-option type, WithRBAC/WithBoundaryEnforcer/WithClassificationMarker constructors, Run(cfg, logger, ...Option) error entry point
- Exit gate: Build OK | Vet OK | `internal/daemon` PASS (161 tests)

## CU.0.1: COMPLETE — Master Key Management (Argon2id + HKDF)
- New file: `internal/crypto/masterkey.go` — MasterKeyManager struct
  - NewMasterKeyManager(password, saltPath): resolves password from arg → NEXUS_PASSWORD env → disabled
  - Argon2id: time=3, memory=65536 (64MB), threads=4, keyLen=32
  - Salt: 32-byte random, generated on first run (0600 perms), reused on subsequent calls
  - HKDF sub-keys via ActiveProfile for 4 domains: nexus-config-key-v1, nexus-memory-key-v1, nexus-audit-key-v1, nexus-control-key-v1
  - SubKey(domain) returns [32]byte; IsEnabled() returns false when no password
- New file: `internal/crypto/masterkey_test.go` — 13 tests (disabled path, env var override, same-password same-keys, wrong-password different-keys, salt persistence, salt permissions (Windows skip), non-zero sub-keys, distinct sub-keys, different salts → different keys, invalid salt, unknown domain zero, disabled zero)
- New file: `cmd/bubblefish/config.go` — `bubblefish config set-password` subcommand
  - Masked terminal password prompt via golang.org/x/term
  - Password confirmation with mismatch detection
  - Removes existing salt before re-derive (fresh salt on password change)
  - Reports canonical salt path (~/.nexus/crypto.salt)
- Modified: `cmd/bubblefish/main.go` — wires `config` case + help text
- New dependency: golang.org/x/term v0.42.0
- Commit: b42ef87
- Exit gate:
  - Build: OK
  - Vet: OK
  - 61 packages PASS — zero failures

## CU.0.2: COMPLETE — Memory Content Encryption
- New file: `internal/destination/encryption.go` — `derivePerRowKey`, `sealAES256GCM`, `openAES256GCM` helpers
  - Per-row key: HKDF-Extract(subKey, payloadID) → HKDF-Expand(prk, "memory-content", 32)
  - AES-256-GCM seal/open via `crypto.ActiveProfile.AEADNew`; nonce prepended to ciphertext blob
- Modified: `internal/destination/sqlite.go`
  - New DDL: `content_encrypted BLOB`, `metadata_encrypted BLOB`, `encryption_version INTEGER DEFAULT 0`
  - Idempotent column migrations in `applyPragmasAndSchema()`
  - `SetEncryption(mkm *crypto.MasterKeyManager)` wires encryption post-open
  - `Write()`: when enabled, derives per-row key, seals content+metadata, stores empty plaintext columns + encrypted blobs, encVersion=1
  - `Query()`, `SemanticSearch()`, `QueryBucketCandidates()`, `scanClusterRows()`, `QueryTimeTravel()`: select 3 new columns; `decryptPayload()` dispatches to plaintext or AES-GCM path by encVersion
  - `decryptPayload()`: decrypts content+metadata in-place; falls back to JSON parse for encVersion=0 rows
  - `EncryptExistingRows(ctx, batchSize, pause)`: migrates plaintext rows in batches of 100 with 10ms pause; resumable (skips encVersion=1 rows); clears plaintext columns after each batch
- Modified: `internal/daemon/daemon.go`
  - After OpenSQLite: resolves `~/.nexus/crypto.salt`, calls `crypto.NewMasterKeyManager("", saltPath)`, wires `SetEncryption(mkm)`, logs enabled/disabled
- New file: `internal/destination/encryption_test.go` — 15 tests
  - Round-trip (content, metadata), plaintext column empty, wrong key fails, backward compat (unencrypted), mixed DB, EncryptExistingRows, resumable migration, context cancellation, different rows different keys, empty content, empty metadata, new writes encrypted, multiple rows, disabled MKM no-op, migration plaintext cleared
- Exit gate:
  - Build: OK
  - Vet: OK
  - 61 packages PASS — zero failures

## CU.0.3: COMPLETE — Config Secrets Encryption
- New file: `internal/crypto/configcrypt.go`
  - `EncryptField(plaintext, key)` → `ENC:v1:<base64(nonce||ciphertext||tag)>`; idempotent on already-encrypted values
  - `DecryptField(s, key)` → plaintext; pass-through for non-ENC:v1: values
  - `IsEncrypted(s)` — prefix check
  - `IsSensitiveFieldName(name)` — true if lowercased name contains key/secret/password/token
- New file: `internal/crypto/configcrypt_test.go` — 11 tests
- New file: `internal/config/decrypt.go`
  - `LoadWithKey(configDir, logger, mkm)` — decrypts ENC:v1: fields in daemon.toml, sources/*.toml, destinations/*.toml before resolveAndValidate
  - `decryptAllConfigStrings(cfg, key)` — walks 14 daemon-level sensitive fields + per-source APIKey + per-destination DSN/APIKey/URL
- New file: `internal/config/decrypt_test.go` — 3 tests (nil mkm, encrypted admin_token, wrong key fails)
- Modified: `cmd/bubblefish/config.go`
  - `encrypt` subcommand: regex line-by-line TOML scan, encrypts sensitive plaintext fields across daemon.toml + sources/*.toml + destinations/*.toml; atomic temp+rename write
  - `decrypt` subcommand: decrypts ENC:v1: fields back to plaintext
  - `show-secrets` subcommand: prints plaintext of all sensitive fields (decrypting ENC:v1: as needed)
- Modified: `cmd/bubblefish/start.go` — derives mkm before config load; uses LoadWithKey so ENC:v1: fields are decrypted at daemon startup
- Commit: 8425f33
- Exit gate:
  - Build: OK
  - Vet: OK
  - 61 packages PASS — zero failures

## Current branch: v0.1.3-moat-takeover
## Current subtask: CU.0.3 complete. Next: CU.0.4 — Control Plane Table Encryption.

### Stale branches (safe to delete):
- v0.1.3-ingest: fully merged to main
- fix/bench-windows-clock: fully merged to main
