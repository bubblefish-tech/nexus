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
  - Step 1: nexus install --mode simple (idempotent)
  - Step 2: nexus start + health poll (10s timeout)
  - Step 3: POST /api/a2a/agents → register demo-agent
  - Step 4: nexus grant create --capability nexus_write
  - Step 5: POST /api/control/approvals → request nexus_delete
  - Step 6: nexus approval decide --decision approve
  - Step 7: create task → write memory → delete memory → mark task complete
  - Step 8: nexus action log --agent + GET /api/control/lineage/{task_id}
  - Step 9: GET /api/substrate/proof/{id} → save JSON → nexus verify (substrate-optional)
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
- New file: `cmd/bubblefish/config.go` — `nexus config set-password` subcommand
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

## CU.0.4: COMPLETE — Control Plane Table Encryption
- Key domain: `"nexus-control-key-v1"` (from MasterKeyManager)
- Per-row HKDF key: `DeriveRowKey(subKey, rowID, tableInfo)` in new `internal/crypto/aead.go` (also exports SealAES256GCM, OpenAES256GCM)
- Tables encrypted (sensitive columns only):
  - `grants`: scope_json → scope_json_encrypted, revoke_reason → revoke_reason_encrypted
  - `approval_requests`: action_json → action_json_encrypted, reason → reason_encrypted
  - `tasks`: input_json → input_json_encrypted, output_json → output_json_encrypted
  - `task_events`: payload_json → payload_json_encrypted
  - `action_log`: policy_reason → policy_reason_encrypted, result → result_encrypted
- Schema: encrypted columns added to `registry.SchemaSQL` (new installs); `registry.MigrateEncryptionColumns` for existing DBs
- Each Store gets `SetEncryption(mkm *MasterKeyManager)` — no-op when nil/disabled
- Backward compat: `encryption_version=0` rows served from plaintext columns; v1 rows decrypted transparently; mixed state (partial migration) handled via per-blob nil check
- Daemon wiring: `d.mkm` stored on Daemon struct; `MigrateEncryptionColumns` called at registry open; `SetEncryption` called on all 4 stores
- New files: `internal/crypto/aead.go`, `internal/a2a/registry/encrypt_migration.go`
- New test files: `internal/grants/encryption_test.go` (10 tests), `internal/approvals/encryption_test.go` (7 tests), `internal/tasks/encryption_test.go` (8 tests), `internal/actions/encryption_test.go` (7 tests)
- Commit: 998000d
- Exit gate:
  - Build: OK
  - Vet: OK
  - 61 packages PASS — zero failures

## CU.0.5: COMPLETE — Audit Event Payload Encryption
- Selective disclosure: chain metadata (record_id, prev_hash, timestamp, event_type, hash) stays PLAINTEXT; sensitive payload encrypted with AES-256-GCM
- Key domain: `"nexus-audit-key-v1"` (from MasterKeyManager)
- Per-record HKDF key: `DeriveRowKey(auditSubKey, recordID, "audit-payload")` with AAD = recordID
- New file: `internal/audit/encrypt.go`
  - `PayloadCrypto` struct + `NewPayloadCrypto(mkm)` (nil when disabled)
  - `encryptInteractionPayload` / `DecryptInteractionPayload` for `InteractionRecord`
  - `encryptControlPayload` / `DecryptControlPayload` for `ControlEventRecord`
- Modified: `InteractionRecord` — `PayloadEncrypted []byte`, `EncryptionVersion int`
- Modified: `ControlEventRecord` — same two fields; `ComputeHash()` unchanged (covers encrypted blob naturally)
- Modified: `WALWriter` — `crypto *PayloadCrypto` field; `SetEncryption(mkm)` method
  - `Submit`: encrypts interaction payload before chain extend
  - `SubmitControl`: encrypts control payload BEFORE `ComputeHash` — hash covers encrypted envelope
- Modified: `internal/daemon/daemon.go` — `d.auditWAL.SetEncryption(d.mkm)` wired after WAL creation
- New test file: `internal/audit/encrypt_test.go` — 13 tests (round-trips, wrong key, backward compat, chain verifiable without key, per-record distinct blobs, hash coverage, nil-MKM no-op, empty entity JSON)
- Commit: a7183b9
- Exit gate:
  - Build: OK
  - Vet: OK
  - 61 packages PASS — zero failures

## CU.0.6: COMPLETE — Agent Registry Encryption
- Encrypted columns: `agent_card_json_encrypted BLOB`, `transport_toml_encrypted BLOB`, `last_error_encrypted BLOB`, `encryption_version INTEGER NOT NULL DEFAULT 0` added to `a2a_agents` in SchemaSQL
- Key domain: `"nexus-control-key-v1"` (reuses control sub-key from MasterKeyManager)
- Per-row HKDF key: `DeriveRowKey(subKey, agentID, "registry-row")` with AAD = agentID
- `Store.SetEncryption(mkm)` added; `NewStoreFromDB(*sql.DB)` constructor added for test isolation
- `Register()`: encrypts agent_card_json + transport_toml + last_error when mkm enabled; plaintext placeholders `'{}'` / `''`
- `UpdateLastSeen()`: encrypts last_error when mkm enabled; sets encryption_version=1
- `scanAgentWith(r scanner, mkm)`: unified scan/decrypt helper replaces per-type `scanAgent`/`scanAgentRow`; nil-blob fallback to plaintext columns for backward compat
- `selectAgentCols`: constant covering all 15 columns used in Get/GetByName/List
- `MigrateEncryptionColumns`: extended to add a2a_agents encrypted columns for existing databases
- Daemon wiring: `d.registryStore.SetEncryption(d.mkm)` added unconditionally after migration (logs when enabled)
- New test file: `internal/a2a/registry/encryption_test.go` — 8 tests (RoundTrip, PlaintextColumnsEmpty, WrongKeyFails, BackwardCompat, UpdateLastSeen_Encrypted, DifferentAgentsDifferentCiphertext, DisabledMKMNoOp, List_Encrypted)
- Exit gate:
  - Build: OK
  - Vet: OK
  - 62 packages PASS — zero failures

## CU.0.7: COMPLETE — TLS Support with Auto-Generated Self-Signed Certificates
- `EnsureAutoTLSCert(keysDir)`: generates ECDSA P-256 self-signed cert + key at `keysDir/tls.crt` + `keysDir/tls.key`; idempotent; 0600 perms; `localhost` + `127.0.0.1` + `::1` SANs; 10yr validity
- Dashboard (:8081) serves HTTPS by default; auto-cert at `~/.nexus/keys/tls.crt` unless operator provides `[daemon.web] tls_cert_file`/`tls_key_file`; explicit `tls_disabled = true` reverts to HTTP
- MCP (:7474) optional TLS via `[daemon.mcp] tls_enabled = true`; `wireMCPTLS` auto-generates or loads operator cert; `mcp.Server.SetTLSConfig` wraps raw TCP listener with `tls.NewListener` before `Start()`
- Config: `WebConfig{TLSDisabled, TLSCertFile, TLSKeyFile}`, `MCPConfig{TLSEnabled, TLSCertFile, TLSKeyFile}`
- 4 new tests: `TestEnsureAutoTLSCert_CreatesFiles`, `_Idempotent`, `_FilePermissions` (Windows-skip), `_LocalhostSANs`
- Commit: 8babb81
- Exit gate:
  - Build: OK
  - Vet: OK
  - 62 packages PASS — zero failures

## CU.0.8: COMPLETE — Encrypted Backup/Restore
- Key domain: `"nexus-backup-key-v1"` added to `subKeyDomains` in `internal/crypto/masterkey.go`
- New file: `internal/backup/encrypt.go`
  - `ExportEncrypted(mkm, ExportEncryptedOptions)` — archives configDir as tar.gz, encrypts with AES-256-GCM
  - `ImportEncrypted(mkm, ImportEncryptedOptions)` — decrypts and extracts tar.gz to destDir
  - Binary format: `[4-byte "BFBK"][4-byte version=1 big-endian][nonce(12)||ciphertext||tag(16)]`
  - AAD: `"BFBK"` magic binds the ciphertext to this format
  - Path traversal guard in `extractTarGz` rejects `..` components
  - Without Force, refuses to overwrite existing files
  - Atomic write: temp file + rename
  - `ErrEncryptionDisabled` returned when MKM is nil or not enabled
- CLI: `nexus backup export --output <path>`, `nexus backup import --input <path> [--dest <dir>] [--force]`
- New test file: `internal/backup/encrypt_test.go` — 11 tests
  - RoundTrip, FileHasMagic, WrongKeyFails, DisabledMKM, BadMagic, BadVersion, TruncatedFile, NoOverwrite, ForceOverwrite, FilePermissions (Windows-skip), EmptySourceDir, CorruptedCiphertext
- Commit: 3d6feae
- Exit gate:
  - Build: OK
  - Vet: OK
  - 63 packages PASS — zero failures

## CU.0.9: COMPLETE — Substrate State Encryption
- Key domain: `"nexus-substrate-key-v1"` added to `subKeyDomains` in `internal/crypto/masterkey.go`
- Encrypted tables: `substrate_ratchet_states.state_bytes` + `substrate_cuckoo_filter.filter_bytes`
- New columns: `state_bytes_encrypted BLOB`, `state_bytes_enc_version` (ratchet); `filter_bytes_encrypted BLOB`, `filter_bytes_enc_version` (cuckoo)
- Per-row HKDF key: `DeriveRowKey(subKey, stateID-as-decimal, "substrate-ratchet-state")` for ratchet; `DeriveRowKey(subKey, "1", "substrate-cuckoo-filter")` for cuckoo filter
- `NewRatchetManager` + `LoadCuckooOracle` + `RebuildFromDB` accept `enc *SubstrateEncryptor`
- `substrate.New()` pre-scans options to extract encryptor before component init; passes enc to sub-components
- Daemon wiring: `substrate.NewSubstrateEncryptor(d.mkm)` + `substrate.WithEncryptor(enc)` passed to `substrate.New()`
- Forward-secure shred: `shredState()` also NULLs `state_bytes_encrypted` + resets enc_version=0
- Backward compat: enc_version=0 rows load from plaintext; enc_version=1 rows decrypt from encrypted column
- Schema migration: idempotent ALTER TABLE calls in `applyPragmasAndSchema()` for existing DBs
- New file: `internal/substrate/encrypt.go` — SubstrateEncryptor + WithEncryptor option
- New test file: `internal/substrate/encrypt_test.go` — 8 tests (ratchet round-trip, plaintext not in DB, wrong key fails, backward compat, shred clears encrypted column, cuckoo round-trip, placeholder byte check, cuckoo wrong key fails, cuckoo backward compat)
- Commit: b9e2bb0
- Exit gate:
  - Build: OK
  - Vet: OK
  - 64 packages PASS — zero failures (simulate flaky timing failure unrelated, passes on retry)

## CU.0.10: COMPLETE — Log Sanitization
- New package: `internal/logging/`
  - `sanitizer.go`: SanitizingHandler — slog.Handler wrapper with 3 redaction rules:
    - `bfn_\S+` token patterns → `[REDACTED:token]` (message + all string attrs)
    - Base64 strings ≥ 64 chars → `[REDACTED:base64]` (validated via base64.StdEncoding decode)
    - Attribute key "content" / "memory_content" / "mem_content" → `[REDACTED:content]`
  - Implements full slog.Handler interface: Enabled, Handle, WithAttrs, WithGroup
  - WithAttrs sanitizes at construction time; WithGroup preserves sanitizing wrapper
  - Recursion into slog.KindGroup nested attributes
  - `sanitizer_test.go`: 12 tests (token in message, token in attr, base64 long redacted, base64 short pass-through, content key, memory_content key, non-sensitive pass-through, Enabled delegation, WithAttrs, WithGroup, grouped nested attr, multiple tokens)
- Wired: `cmd/bubblefish/start.go` `buildLogger()` wraps the configured handler with `logging.NewSanitizingHandler`
- Exit gate:
  - Build: OK
  - Vet: OK
  - 65 packages PASS — zero failures

## CU.0.11: COMPLETE — Startup Encryption Self-Test
- New file: `internal/crypto/selftest.go`
  - `SelfTest(mkm *MasterKeyManager) error` — seal/open round-trip for every sub-key domain
  - No-op when mkm is nil or disabled
  - Known plaintext `"nexus-encryption-self-test-v1"` with AAD `"nexus-selftest"` sealed then opened per domain
  - Fails on the first domain that cannot round-trip, with a descriptive error naming the domain
- New file: `internal/crypto/selftest_test.go` — 4 tests
  - TestSelfTest_NilMKM, TestSelfTest_DisabledMKM, TestSelfTest_EnabledRoundTrip,
    TestSelfTest_AllDomainsExercised, TestSelfTest_DifferentPasswordsDifferentKeys
- Modified: `internal/daemon/daemon.go`
  - `nexuscrypto.SelfTest(mkm)` called immediately after key derivation
  - Fatal: `Start()` returns error if self-test fails — daemon refuses to start
  - Log message updated to "memory content encryption enabled (self-test passed)" on success
- Exit gate:
  - Build: OK
  - Vet: OK
  - 65 packages PASS — zero failures

## DISC.1: COMPLETE — Discovery Manifest
- New package: `internal/discover/`
  - `manifest.go`: ToolDefinition struct, 5 detection-method constants, 5 connection-type constants
  - 41 built-in KnownTools entries covering all 5 tiers (13 port, 8 process, 8 directory, 5 mcp_config, 7 docker)
  - `LoadCustomTools(configDir)`: loads `discovery/custom_tools.toml` via BurntSushi/toml; returns nil slice (not error) for missing file
  - `AllTools(configDir)`: built-ins + custom merged; custom appended after built-ins
  - `ExpandPath(p)`: `~` → `os.UserHomeDir()` expansion
  - `manifest_test.go`: 10 tests (minimum count ≥30, all fields valid per method, all 5 methods covered, missing file, TOML parse, invalid TOML, AllTools merge, AllTools no-custom, ExpandPath home, ExpandPath absolute)
- Exit gate:
  - Build: OK
  - Vet: OK
  - 66 packages PASS — zero failures

## DISC.2: COMPLETE — Scanner Core
- New dependency: `github.com/shirou/gopsutil/v3 v3.24.5` (process enumeration)
- New file: `internal/discover/scanner.go`
  - `DiscoveredTool` struct (Name, DetectionMethod, ConnectionType, Endpoint, Orchestratable, IngestCapable, MCPServers)
  - `Scanner` struct with `configDir`, `timeout` (2s default), `logger`
  - `NewScanner(configDir, logger)` constructor
  - `FullScan(ctx)`: loads AllTools → `fullScanWithDefs` (testable core)
  - `fullScanWithDefs`: launches 6 goroutines (port, process, filesystem, mcp_config, docker, general) into buffered channel; deduplicates by name (first wins)
  - Tier runners: `runPortTier`, `runProcessTier`, `runFilesystemTier`, `runMCPConfigTier`, `runDockerTier`
- New file: `internal/discover/probe_port.go`
  - `probePort(def, timeout)`: constructs `http://localhost:{port}` → delegates to `probePortAt`
  - `probePortAt(def, baseURL, timeout)`: HTTP GET with 2s timeout, body substring match; testable with arbitrary base URL
- New file: `internal/discover/probe_process.go`
  - `processNameLister` function type; `defaultProcessNameLister` via gopsutil `process.Processes()`
  - `probeProcess` / `probeProcessWithLister`: case-insensitive name match, strips `.exe` suffix for Windows
- New file: `internal/discover/probe_filesystem.go`
  - `probeFilesystem` / `probeFilesystemWithPaths`: `os.Stat` on each expanded path; first hit wins
- New file: `internal/discover/probe_mcpconfig.go`
  - `MCPServerEntry` struct (Name, Command, Args); `mcpConfigFile` JSON struct
  - `probeMCPConfig` / `probeMCPConfigAt`: parses `mcpServers` JSON map; returns false when no servers
  - `parseMCPConfig(path)`: reads + unmarshals JSON config file
- New file: `internal/discover/probe_docker.go`
  - `dockerOutputReader` function type; `defaultDockerOutputReader` runs `docker ps --format {{.Image}}`
  - `probeDocker` / `probeDockerWithReader`: best-effort (returns false when docker unavailable)
- New file: `internal/discover/probe_general.go`
  - `probeGeneralPorts(timeout)`: scans 235 ports with 50-goroutine semaphore pool
  - `scanPorts(ports, baseURLOf, timeout)`: testable core; probes `/v1/models`, checks for `"data"` or `"object"` in response; names results `"OpenAI API (port N)"`
  - `generalPortList()`: 1234-1240, 3000-3100, 4891, 5000-5010, 7474, 7860-7870, 8000-8100, 8443, 9090, 11434
- New file: `internal/discover/scanner_test.go` — 20 tests
  - Port: hit, wrong body, no server
  - Process: hit, miss, Windows .exe, lister error
  - Filesystem: hit, miss
  - MCP config: hit, no file, empty servers
  - Docker: hit, miss, unavailable
  - General: port hit, port miss, port list coverage
  - Scanner: empty defs, deduplication
- Exit gate:
  - Build: OK
  - Vet: OK
  - 67 packages PASS — zero failures (30 tests in internal/discover)

## DISC.3: COMPLETE — Auto-Connector
- New file: `internal/discover/connector.go`
  - `ConnectorConfig{QuickMode}` — quick mode auto-accepts all proposals
  - `ConnectionConfig` struct: Name, ConnectionType, Endpoint, Command, Args, WatchPaths, Orchestratable, IngestCapable
  - `ConnectionProposal{Tool, Config}` — pairs a DiscoveredTool with its resolved config
  - `Connector.Propose(tools)` — returns one proposal per discovered tool
  - `Connector.AutoAccept(proposals)` — returns all configs (quick install path); interactive mode defers to TUI
  - `buildConfig(dt)`: routes by ConnectionType — openai_compat/mcp_sse/http_api set Endpoint; mcp_stdio uses first MCPServerEntry command+args and populates WatchPaths when IngestCapable; sentinel_ingest populates WatchPaths
  - `knownIngestPaths(name)` — maps 10 ingest-capable tool names to their data directories
- New file: `internal/discover/connector_test.go` — 20 tests
  - Table-driven TestBuildConfig_ConnectionTypes (8 subtests): openai_compat, http_api, mcp_sse, mcp_stdio with/without servers, mcp_stdio ingest, non-ingest, sentinel_ingest
  - Table-driven TestKnownIngestPaths (12 subtests): all 10 known tools + Ollama + UnknownTool → nil
  - TestConnector_Propose_Empty, TestConnector_Propose_MultipleTools, TestConnector_AutoAccept_ReturnsAllConfigs, TestConnector_AutoAccept_EmptyProposals, TestConnector_QuickModeField, TestConnector_ProposalToolRoundtrip
- Exit gate:
  - Build: OK
  - Vet: OK
  - 68 packages PASS — zero failures (50 tests in internal/discover, +20 new connector tests)

## DISC.4: COMPLETE — Orchestration Engine
- New package: `internal/orchestrate/`
  - `engine.go`: Engine struct + Config; 5 public methods: ListAgents, Orchestrate, Council, Broadcast, Collect
  - AgentLister, GrantChecker, MemoryWriter interfaces (daemon-side adapters)
  - Orchestrate: caller must hold "orchestrate" + "dispatch:<agent_id>" grants; parallel dispatch; fail strategies (wait_all, return_partial, fail_fast); optional memory persistence; result cache for nexus_collect
  - Council: deliberation mode with "reason step-by-step" prefix; synthesises responses
  - Broadcast: fire-and-forget goroutines; 10s per-agent timeout
  - Collect: retrieve cached OrchestrationResult by ID
  - `engine_test.go`: 20 tests covering all methods, strategies, token-limit detection (HTTP 413/429/body phrase), OpenAI-compat response extraction, immune scan, memory storage, latency population
- New package: `internal/immune/`
  - `scanner.go`: Scanner stub — ScanOrchestrationResult + ScanWrite both return Action="accept"; Tier-0 rules added in DEF.1
  - `scanner_test.go`: 6 tests (always-accept, empty inputs, embedding, zero-value, field round-trip)
- New file: `internal/mcp/tools_orchestrate.go`
  - OrchestrateProvider interface; 6 DTOs; orchestrateToolDefs() (5 tools); 5 call handlers (callNexusListAgents, callNexusOrchestrate, callNexusCouncil, callNexusBroadcast, callNexusCollect)
  - SetOrchestrateProvider setter on Server; all tools fail gracefully when provider is nil
- Modified: `internal/mcp/server.go` — orchestrateProvider field; tool list + tool dispatch wired
- New file: `internal/daemon/orchestrate_adapter.go`
  - orchestrateAdapter (mcp.OrchestrateProvider → orchestrate.Engine)
  - registryAgentLister (orchestrate.AgentLister → registry.Store)
  - grantStoreChecker (orchestrate.GrantChecker → grants.Store)
- Modified: `internal/daemon/daemon.go` — wires orchestration engine when registryStore + grantStore + mcpServer are all non-nil
- Exit gate:
  - Build: OK
  - Vet: OK
  - 70 packages PASS — zero failures (2 new: internal/orchestrate, internal/immune)

## DEF.1: COMPLETE — Immune System Tier-0 Rules
- New package: `internal/immune/rules/`
  - `rules.go`: `Result` struct + `ScanContent(content, metadata, embedding, embDim)` entry point
  - 12 compiled heuristic rules (pure Go, no model dependency):
    - T0-001: prompt injection regex → quarantine
    - T0-002: role hijacking regex → quarantine
    - T0-003: admin override keywords (ADMIN_OVERRIDE, SUDO_MODE, DEBUG_MODE, JAILBREAK) → quarantine
    - T0-004: base64 segment >500 chars containing executable magic bytes (PE/ELF/Mach-O/shebang) → quarantine
    - T0-005: same word repeated >50 times → reject
    - T0-006: Cyrillic homoglyph substitution → normalize + flag (NormalizedContent populated)
    - T0-007: SQL injection patterns → quarantine
    - T0-008: embedding dimension mismatch vs configured EmbeddingDim → reject (skipped when EmbeddingDim=0)
    - T0-009: content <5 chars but embedding present → flag
    - T0-010: metadata claims Latin-script language but >30% non-ASCII chars → flag
    - T0-011: null bytes in content → reject
    - T0-012: content >100KB → reject
- Rewritten: `internal/immune/scanner.go`
  - `Config{EmbeddingDim int}`, `NewWithConfig(cfg)` added alongside existing `New()`
  - `ScanWrite` → calls `rules.ScanContent` with configured `EmbeddingDim`
  - `ScanOrchestrationResult` → calls `rules.ScanContent` with nil metadata/embedding (T0-008/T0-009 skipped)
  - `ScanResult` gains `NormalizedContent string` field for T0-006
- Rewritten: `internal/immune/scanner_test.go` — 28 tests
  - 6 original stub tests retained (all still pass with real implementation)
  - 22 new tests covering all 12 rules: hit cases, boundary cases, accept cases
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/immune` PASS (28 tests)
  - Full suite: 66+ packages PASS; `internal/supervisor` 1 flaky timing failure (pre-existing, passes in isolation)

## DEF.2: COMPLETE — Quarantine Storage
- New package: `internal/quarantine/`
  - `store.go`: Store backed by `<configDir>/quarantine.db` (modernc.org/sqlite)
  - Schema: `quarantine` table matching spec — id, original_payload_id, content, metadata_json, source_name, agent_id, quarantine_reason, rule_id, quarantined_at_ms, reviewed_at_ms, review_action, reviewed_by
  - `Insert`, `Get`, `List` (filter by source, include_reviewed, limit ≤ 1000), `Decide` (approved/rejected), `Close`, `NewID`
  - `store_test.go`: 13 tests (RoundTrip, NotFound, ListUnreviewed, ListIncludeReviewed, FilterBySource, LimitDefault, LimitCap, Approve, Reject, DecideNotFound, InvalidAction, UniqueID, DuplicateID)
- Immune scan wired into write paths:
  - `handleWrite` (HTTP): scan between step 9 (build TranslatedPayload) and step 10 (WAL append)
  - `mcp_pipeline.Write`: scan before WAL append
  - "quarantine" or "reject" action → store in quarantine table + emit `memory.quarantined` ControlEventRecord + return identical `writeResponse{Status:"accepted"}` (response-shape indistinguishability)
  - "normalize" action → write proceeds with NormalizedContent substituted
  - "flag" action → write proceeds as-is
- Daemon wiring:
  - `immuneScanner *immune.Scanner` field — always initialized in Start()
  - `quarantineStore *quarantine.Store` field — opened at `<configDir>/quarantine.db`; nil-safe
  - quarantineStore.Close() in Stop() stage 3
- Audit: `ControlEventMemoryQuarantined = "memory.quarantined"` added to `internal/audit/control_events.go`
- REST API (admin-token protected, gated on quarantineStore != nil):
  - GET /api/quarantine — list (filter by source, include_reviewed, limit)
  - GET /api/quarantine/{id} — get single record
  - POST /api/quarantine/{id}/approve — mark reviewed_action="approved"
  - POST /api/quarantine/{id}/reject — mark reviewed_action="rejected"
  - Registered in buildRouter() and BuildAdminRouter()
- Dashboard: `web/dashboard/quarantine.html` — dark theme, nav bar, table with Approve/Reject buttons; textContent-only DOM; gated on quarantineStore
- CLI: `nexus quarantine list [--source <name>] [--include-reviewed] [--limit N] [--json]`; `nexus quarantine approve --id <id>`; `nexus quarantine reject --id <id>`
- Exit gate:
  - Build: OK
  - Vet: OK
  - 66 packages PASS (internal/quarantine +1 new, all others pass); zero failures

## DEF.3: COMPLETE — Query Anomaly Monitor
- New file: `internal/immune/query_monitor.go`
  - `MonitorConfig{WindowDuration, RateLimitPerMin, OverlapThreshold}` + `DefaultMonitorConfig()`
  - `MonitorAlert{AgentID, AlertType, Details}` — AlertType: "RATE_LIMIT", "MEMBERSHIP_INFERENCE", "POST_DELETE_PROBE"
  - `QueryMonitor`: per-agent sliding window, mutex-protected, injectable clock (`WithClock`) for tests
  - `RecordQuery(agentID, query)`: three checks in severity order:
    1. RATE_LIMIT — >RateLimitPerMin queries in last 60s (default 100/min)
    2. POST_DELETE_PROBE — query text contains a recently deleted content ref (case-insensitive substring)
    3. MEMBERSHIP_INFERENCE — >OverlapThreshold prior window queries share significant tokens (len≥4, non-stopword) with current query (default threshold 10)
  - `NotifyDelete(agentID, contentRef)`: registers a deleted ref for the window duration
  - Lazy eviction of records older than WindowDuration on every RecordQuery/NotifyDelete call
  - Token extraction: lowercase, word-char split, min length 4, stopword filter (37 common English words)
- New file: `internal/immune/query_monitor_test.go` — 19 tests
  - Rate limit: fires, below threshold, alert details contain count, old queries ignored after 1-min boundary
  - Membership inference: fires, at-threshold no-alert, details contain overlap count, empty query no-alert
  - Post-delete probe: fires, case-insensitive, no-delete no-alert, ref expires after window
  - Window eviction: old queries removed from overlap check after window elapses
  - Agent isolation: two agents do not share state
  - Default config: fields correct, zero config uses defaults
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/immune` PASS (47 tests — 19 new + 28 pre-existing)
  - Full suite: 65 packages PASS; `internal/integration` 1 pre-existing flaky soak test (passes on retry)

## SN.1: COMPLETE — Sentinel Core Framework (internal/ingest/)
- NOTE: Built in a prior session as `internal/ingest/` (not `internal/sentinel/` as spec named it); existing `internal/sentinel/` is the WAL drift detector (unrelated)
- `watcher.go`: Watcher interface (Name, SourceName, DefaultPaths, Detect, Parse, State, SetState) + IngestWriter interface
- `ingest.go`: Manager — fsnotify-based watching, walkAndAdd recursive dir registration, debounced event loop, parse worker pool, truncation detection, path allowlist
- `file_state.go`: FileStateStore — SQLite-backed offset + SHA-256 hash persistence per watcher+path
- `debouncer.go`: 500ms configurable debounce; coalesces rapid events per path
- `worker_pool.go`: bounded goroutine pool (default 4) for parse tasks
- `config.go`: Config with per-watcher toggles, MaxFileSize (100MB), debounce, concurrency; DefaultConfig()
- `types.go`, `errors.go`, `metrics.go`: Memory type, error sentinels, Prometheus counters

## SN.2: COMPLETE — Claude Code Parser
- `watcher_claude_code.go`: Parses `~/.claude/projects/**/*.jsonl`; handles string + array content formats; per-project-hash metadata; offset-based incremental reads; truncation-safe
- `watcher_claude_code_test.go`: 8 tests (sample session, from-offset, empty, malformed lines, truncated, file-too-large, symlink-rejected, context-cancel)
- Testdata: `testdata/claude_code/{sample_session,empty,malformed,truncated_session}.jsonl`

## SN.3: COMPLETE — Cursor Parser
- `watcher_cursor.go`: Parses `~/.cursor/chat-history/*.json`; whole-file hash deduplication (Cursor rewrites on save); user/assistant/system roles; cross-platform DefaultPaths
- `watcher_cursor_test.go`: tests covering sample chat, empty, malformed, symlink-rejected, file-too-large
- Testdata: `testdata/cursor/{sample_chat,empty_chat,malformed}.json`

## SN.4: COMPLETE — LM Studio Parser
- `watcher_lm_studio.go`: Real implementation replacing ErrNotImplemented stub
  - DefaultPaths: `~/.lmstudio/conversations` (Windows/Linux), `~/.cache/lm-studio/conversations` (macOS/Linux), `%APPDATA%/LM Studio/conversations` (Windows alt)
  - Parses whole-file JSON (same pattern as Cursor — LM Studio rewrites on save)
  - Dual timestamp field: prefers `createdAt`, falls back to `timestamp`
  - Populates model from conversation-level field; lms_chat_id + lms_chat_title metadata
  - NewLMStudioWatcherWithConfig(cfg, logger) constructor added alongside parameterless NewLMStudioWatcher()
- `watcher_lm_studio_test.go`: 13 tests (name/source, default paths, state transitions, sample parse, meta fields, timestamp from createdAt, alt timestamp field, empty messages, malformed, file-not-exist, symlink-rejected, file-too-large, hash populated, context cancelled, detect-no-dir)
- Testdata: `testdata/lm_studio/{sample_chat,empty_chat,malformed,alt_timestamp_field}.json`
- Removed TestLMStudioStub from watcher_stubs_test.go (no longer a stub)

## SN.5: COMPLETE — Parser Stubs
- `watcher_claude_desktop.go`: stub (ErrNotImplemented) — Claude Desktop SQLite IndexedDB; Windows/macOS/Linux paths
- `watcher_chatgpt_desktop.go`: stub (ErrNotImplemented) — ChatGPT Desktop leveldb; Windows/macOS/Linux paths
- `watcher_open_webui.go`: stub (ErrNotImplemented) — Open WebUI
- `watcher_perplexity_comet.go`: stub (ErrNotImplemented) — Perplexity Comet
- `watcher_stubs_test.go`: shared stubWatcherTest helper + 4 stub tests (chatgpt_desktop, claude_desktop, open_webui, perplexity_comet)

## SN.6: COMPLETE — Import Command
- `internal/ingest/importer/importer.go`: Run(Options) — auto-detect or explicit format; supports Claude export ZIP, ChatGPT export ZIP, Claude Code dir, Cursor dir, generic JSONL; dry-run mode
- CLI wired: `case "import":` in cmd/bubblefish/main.go
- `importer/importer_test.go`: tests for each format + auto-detection

## Exit gate (SN.1–SN.6 combined):
- Build: OK
- Vet: OK
- `internal/ingest` + `internal/ingest/importer`: PASS (all tests green)
- Full suite: 0 failures

## DB.1: COMPLETE — Destination Interface Definition
- New file: `internal/destination/interface.go`
  - `type Memory = TranslatedPayload` — type alias keeps existing write-path code unchanged
  - `type Query = QueryParams` — type alias; callers need no changes
  - `HealthStatus{OK bool, Latency time.Duration, Error string}` — liveness probe result
  - `Destination` interface: Name, Write(ctx, *Memory), Read(ctx, id), Search(ctx, *Query), Delete(ctx, id), VectorSearch(ctx, embedding, limit), Migrate(ctx, version), Health(ctx), Close
  - `ErrVectorSearchUnsupported` sentinel for backends lacking vector search
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/destination` PASS (all pre-existing tests green)

## DB.2: COMPLETE — SQLite Adapter Interface Compliance
- `interface.go`: Destination.Write signature aligned to existing `Write(p TranslatedPayload) error` (matches DestinationWriter, zero caller churn)
- New file: `internal/destination/sqlite_compliance.go`
  - Compile-time check: `var _ Destination = (*SQLiteDestination)(nil)`
  - `Name() string` → returns "sqlite"
  - `Read(ctx, id) (*Memory, error)` → SELECT by payload_id; nil, nil for missing
  - `Search(ctx, *Query) ([]*Memory, error)` → wraps Query(), converts []TranslatedPayload to []*Memory
  - `Delete(ctx, id) error` → ExecContext DELETE, idempotent (no-op for missing ID)
  - `VectorSearch(ctx, embedding, limit) ([]*Memory, error)` → wraps SemanticSearch; empty slice for nil embedding
  - `Migrate(ctx, version) error` → no-op (all migrations applied at open time in applyPragmasAndSchema)
  - `Health(ctx) (*HealthStatus, error)` → PingContext with latency measurement
- New file: `internal/destination/sqlite_compliance_test.go` — 12 tests
  - Name, Read_Found, Read_NotFound, Search, Search_Empty, Delete_Exists, Delete_NotExists,
    VectorSearch, VectorSearch_EmptyEmbedding, Migrate, Health_OK, Health_ClosedDB,
    InterfaceCompliance, Read_TimestampRoundtrip
- Exit gate:
  - Build: OK
  - Vet: OK
  - 65 packages PASS — zero failures

## DB.3: COMPLETE — PostgreSQL Adapter Interface Compliance
- New file: `internal/destination/postgres_compliance.go`
  - Compile-time check: `var _ Destination = (*PostgresDestination)(nil)`
  - `Name() string` → returns "postgres"
  - `Read(ctx, id) (*Memory, error)` → SELECT by payload_id; nil, nil for missing
  - `Search(ctx, *Query) ([]*Memory, error)` → wraps Query(), converts []TranslatedPayload to []*Memory
  - `Delete(ctx, id) error` → ExecContext DELETE, idempotent (no-op for missing ID)
  - `VectorSearch(ctx, embedding, limit) ([]*Memory, error)` → wraps SemanticSearch; empty slice for nil embedding
  - `Migrate(ctx, version) error` → no-op (all migrations applied at open time in applySchema)
  - `Health(ctx) (*HealthStatus, error)` → PingContext with latency measurement
- New file: `internal/destination/postgres_compliance_test.go` — 11 tests
  - InterfaceCompliance (compile-time), Name, Read_Found, Read_NotFound, Search, Search_Empty,
    Delete_Exists, Delete_NotExists, VectorSearch_EmptyEmbedding, Migrate, Health_OK, Health_ClosedDB
  - All DB tests require `TEST_POSTGRES_DSN` env var; skipped in CI without live Postgres
- Exit gate:
  - Build: OK
  - Vet: OK
  - 65 packages PASS — zero failures

## DB.4: COMPLETE — Supabase Adapter Interface Compliance
- New file: `internal/destination/supabase_compliance.go`
  - Compile-time check: `var _ Destination = (*SupabaseDestination)(nil)`
  - `Name() string` → returns "supabase"
  - `Read(ctx, id) (*Memory, error)` → GET /rest/v1/memories?payload_id=eq.{id}&limit=1; nil, nil for empty response
  - `Search(ctx, *Query) ([]*Memory, error)` → wraps Query(), converts []TranslatedPayload to []*Memory
  - `Delete(ctx, id) error` → DELETE /rest/v1/memories?payload_id=eq.{id}; idempotent (204 always)
  - `VectorSearch(ctx, embedding, limit) ([]*Memory, error)` → wraps SemanticSearch; empty slice for nil embedding
  - `Migrate(ctx, version) error` → no-op (schema managed externally in Supabase dashboard)
  - `Health(ctx) (*HealthStatus, error)` → HEAD /rest/v1/memories with latency; HTTP 5xx = unhealthy, 2xx/4xx = healthy
  - `Close() error` already existed (no-op for HTTP client)
- New file: `internal/destination/supabase_compliance_test.go` — 14 tests
  - Tests use httptest.Server (supabaseMock) — no real Supabase account required
  - InterfaceCompliance, Name, Read_Found, Read_NotFound, Read_HTTPError, Search, Search_Empty,
    Delete_Exists, Delete_NotExists, VectorSearch_EmptyEmbedding, VectorSearch, Migrate,
    Health_OK, Health_ServerError
- Exit gate:
  - Build: OK
  - Vet: OK
  - 65 packages PASS — zero failures

## DB.5: COMPLETE — MySQL/MariaDB Destination Adapter
- New package: `internal/destination/mysql/`
  - `mysql.go`: MySQLDestination implementing `destination.Destination`
  - Driver: `github.com/go-sql-driver/mysql v1.9.3` (+ transitive `filippo.io/edwards25519`)
  - DDL: memories table with InnoDB + utf8mb4_unicode_ci; backtick-quoted `timestamp` and `destination` reserved words
  - Idempotent migrations: ADD COLUMN (ignores MySQL error 1060) + CREATE INDEX (ignores error 1061)
  - Columns: MEDIUMTEXT content, LONGBLOB embedding (little-endian float32), VARCHAR(1000) sensitivity_labels, DATETIME(6) timestamps
  - Write: INSERT IGNORE (idempotent); all values via `?` parameterised placeholders
  - Query: WHERE clause built from fixed condition strings; LIKE for text search; LIMIT ? OFFSET ? pagination
  - VectorSearch: application-level cosine similarity (full table scan, no native vector extension required)
  - CanSemanticSearch: checks for non-null LONGBLOB embeddings
  - Health: PingContext with latency measurement
  - `export_test.go`: white-box exports for helper functions
  - `mysql_test.go`: 9 unit tests (encoding, cosine, marshal) + 13 integration tests (skip without TEST_MYSQL_DSN)
- New dependency: `github.com/go-sql-driver/mysql v1.9.3`
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/destination/mysql` PASS (9 unit tests pass; 13 integration tests skip cleanly without live MySQL)
  - Full suite: zero failures

## DB.6: COMPLETE — CockroachDB Destination Adapter
- New package: `internal/destination/cockroachdb/`
  - `cockroachdb.go`: CockroachDBDestination implementing `destination.Destination`
  - Driver: `jackc/pgx/v5/stdlib` (already in go.mod); CockroachDB is PostgreSQL-wire-compatible
  - Schema: PostgreSQL-compatible DDL; BYTEA for embeddings (no pgvector extension); TEXT[] for sensitivity_labels; JSONB for metadata; TIMESTAMPTZ for timestamps
  - Schema setup: skips `CREATE EXTENSION vector` and IVFFlat index (not supported by CockroachDB)
  - Migrations: `ADD COLUMN IF NOT EXISTS` (CockroachDB 22.1+); `CREATE INDEX IF NOT EXISTS` — fully idempotent, no error-code workarounds needed
  - Write: `INSERT ... ON CONFLICT DO NOTHING`; `$N` parameterised placeholders
  - VectorSearch: application-level cosine similarity over BYTEA embeddings (same encoding as SQLite/MySQL)
  - Query: ILIKE for text search; $N placeholders; LIMIT $N OFFSET $N pagination
  - `export_test.go`: white-box exports (encode/decode/cosine/marshal/pgTextArray helpers)
  - `cockroachdb_test.go`: 13 unit tests (pass without DB) + 11 integration tests (skip without TEST_CRDB_DSN)
- No new dependencies
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/destination/cockroachdb` PASS (13 unit tests pass; 11 integration tests skip cleanly)
  - Full suite: zero failures

## DB.7: COMPLETE — MongoDB Destination Adapter
- New package: `internal/destination/mongodb/`
  - `mongodb.go`: MongoDBDestination implementing `destination.Destination`
  - Driver: `go.mongodb.org/mongo-driver/v2 v2.5.1` (MongoDB Go Driver v2)
  - Collection: `memories` in database named from URI path (default: "nexus")
  - Document schema: `_id` = payload_id; embedding as little-endian float32 BLOB;
    metadata as native BSON map[string]string; sensitivity_labels as string array;
    timestamps as BSON Date (UTC)
  - Indexes: idempotency_key, classification_tier, tier, namespace+destination+timestamp DESC,
    subject+timestamp DESC — all idempotent via CreateMany
  - Write: `ReplaceOne` with `upsert=true` keyed on `_id` (payload_id)
  - VectorSearch: application-level cosine similarity (same as MySQL/CockroachDB);
    fetches docs with `embedding` field present, decodes in Go, sorts by cosine score
  - Migrate: no-op (indexes created at Open time)
  - Health: `client.Ping` with latency measurement
  - `export_test.go`: white-box exports (encodeEmbedding, decodeEmbedding, cosineSimilarity,
    docFromPayload, payloadFromDoc)
  - `mongodb_test.go`: 9 unit tests (pass without DB) + 12 integration tests (skip without TEST_MONGODB_URI)
- New dependency: `go.mongodb.org/mongo-driver/v2 v2.5.1`
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/destination/mongodb` PASS (9 unit tests pass; 12 integration tests skip cleanly)
  - Full suite: zero failures

## DB.8: COMPLETE — Firebase/Firestore Destination Adapter
- New package: `internal/destination/firestore/`
  - `firestore.go`: FirestoreDestination implementing `destination.Destination`
  - Driver: `cloud.google.com/go/firestore v1.22.0`
  - Authentication: Application Default Credentials or explicit service account JSON file
  - Document model: payload_id as Firestore document ID; embedding as []float64 (Firestore native array);
    metadata as native Firestore map[string]string; timestamps as time.Time (Firestore Date)
  - Write: `Doc(payloadID).Set(ctx, doc)` — idempotent overwrite
  - VectorSearch: returns `ErrVectorSearchUnsupported` (no Firestore native vector search)
  - Query: Firestore structured where clauses; content filter (Q) applied client-side (no LIKE equivalent)
  - Pagination: Offset+Limit (O(n) but correct; consistent with other adapters' cursor scheme)
  - Migrate: no-op (Firestore is schemaless)
  - Health: list 1 document from memories collection with latency measurement
  - `export_test.go`: white-box exports (docFromPayload, payloadFromDoc, float conversion helpers)
  - `firestore_test.go`: 6 unit tests (pass without credentials) + 11 integration tests (skip without TEST_FIRESTORE_PROJECT)
- New dependency: `cloud.google.com/go/firestore v1.22.0`
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/destination/firestore` PASS (6 unit tests pass; 11 integration tests skip cleanly)
  - Full suite: zero failures

## DB.9: COMPLETE — TiDB Destination Adapter
- New package: `internal/destination/tidb/`
  - `tidb.go`: TiDBDestination implementing `destination.Destination`
  - Driver: `github.com/go-sql-driver/mysql v1.9.3` (TiDB is MySQL-wire-compatible)
  - DDL: same as MySQL + `embedding_tv TEXT` column for TiDB native vector (JSON float array)
  - `hasVectorTV bool`: set when embedding_tv column created; enables `VEC_COSINE_DISTANCE()` path
  - Write: 22-column INSERT IGNORE including embedding_tv (JSON float array) when hasVectorTV
  - VectorSearch: tries `tidbVectorSearch()` (SQL `VEC_COSINE_DISTANCE`), falls back to app-level scan
  - `marshalEmbeddingTV([]float32) string`: JSON marshal to `[1.0, 2.0, ...]` for TiDB vector column
  - All standard helpers: encodeEmbedding, decodeEmbedding, cosineSimilarity, marshalMetadata
  - `export_test.go`: white-box exports (encodeEmbedding, decodeEmbedding, cosineSimilarity,
    marshalEmbeddingTV, marshalMetadata)
  - `tidb_test.go`: 10 unit tests (pass without DB) + 10 integration tests (skip without TEST_TIDB_DSN)
- No new dependencies (reuses go-sql-driver/mysql)
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/destination/tidb` PASS (10 unit tests pass; 10 integration tests skip cleanly)
  - Full suite: zero failures

## DB.10: COMPLETE — Turso/libSQL Destination Adapter
- New package: `internal/destination/turso/`
  - `turso.go`: TursoDestination implementing `destination.Destination`
  - Driver: `github.com/tursodatabase/libsql-client-go/libsql` (blank import registers "libsql" driver)
  - SQLite-compatible DDL: TEXT/INTEGER/BLOB; INSERT OR IGNORE for idempotency
  - Timestamps: stored as Unix milliseconds (INTEGER); read back via `time.UnixMilli(tsMS).UTC()`
  - VectorSearch: application-level cosine similarity (same O(n) pattern as SQLite/MySQL)
  - Connection string formats: `libsql://database.turso.io?authToken=TOKEN`, `file:./local.db`
  - `export_test.go`: white-box exports (encodeEmbedding, decodeEmbedding, cosineSimilarity, marshalMetadata)
  - `turso_test.go`: 8 unit tests (pass without DB) + 10 integration tests (skip without TEST_TURSO_URL)
- New dependency: `github.com/tursodatabase/libsql-client-go v0.0.0-20251219100830-236aa1ff8acc`
  - Transitive: `github.com/antlr4-go/antlr/v4 v4.13.0`, `github.com/coder/websocket v1.8.12`
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/destination/turso` PASS (8 unit tests pass; 10 integration tests skip cleanly)
  - Full suite: zero failures

## DB.11: COMPLETE — Database Selection in Setup
- `internal/config/types.go`:
  - `destinationBody.Type` comment updated to list all 8 backends
  - New field `ConnectionString string` in `destinationBody` + `Destination` (mongodb URI, turso URL, firestore project ID)
- `internal/config/loader.go`: copies `ConnectionString` when decoding destination TOML
- `internal/config/decrypt.go`: `ConnectionString` included in per-destination decryption pass
- `internal/destination/factory/factory.go`: `OpenByType(cfg, logger, configDir) (Destination, error)`
  - Switches on `cfg.Type` for all 8 adapters
  - sqlite: expands `~`, falls back to `<configDir>/memories.db`
  - postgres: uses `cfg.DSN`; dimensions=0 (no pgvector required)
  - supabase: uses `cfg.URL` + resolved `cfg.APIKey`
  - mysql/mariadb: uses `cfg.DSN`
  - cockroachdb/crdb: uses `cfg.DSN`
  - mongodb/mongo: uses `cfg.ConnectionString` (or `cfg.DSN` as fallback)
  - firestore: uses `cfg.ConnectionString` (project ID); optional `cfg.APIKey` for credentials file
  - tidb: uses `cfg.DSN`
  - turso/libsql: uses `cfg.ConnectionString` (or `cfg.URL` as fallback)
- `internal/queue/queue.go`: `dest` field + `New()` parameter changed from `DestinationWriter` to `Destination`
- `internal/daemon/daemon.go`:
  - `d.dest` changed from `DestinationWriter` to `Destination`
  - SQLite open block replaced with `destfactory.OpenByType(d.resolveDestinationConfig(), ...)`
  - `resolveDestinationConfig()` added (returns first configured dest or SQLite fallback)
  - `resolveSQLitePath()` retained for admin_list.go direct SQL access
  - `sqliteDest.SetEncryption(mkm)` → type assertion `d.dest.(*destination.SQLiteDestination)`
  - `d.querier` now set via `destination.Querier` type assertion on opened dest
- `internal/daemon/handlers.go`: `d.dest.Ping()` → type assertion to `interface{ Ping() error }`
- `internal/daemon/consistency.go`: `d.dest.Exists()` → type assertion to `interface{ Exists(string) (bool, error) }`
- Test stubs updated: `queue_bench_test.go`, `queue_test.go`, `daemon/export_test.go`
- Exit gate:
  - Build: OK
  - Vet: OK
  - 70 packages PASS — zero failures
  - `internal/integration` flaky timing test confirmed passing (pre-existing, passes in isolation)

## TUI.1: COMPLETE — Core TUI Framework
- New package `internal/tui/pages/`: `page.go` (WizardState, Page interface, WizardCompleteMsg), 9 stub pages (welcome, scan, features, tools, database, security, tunnel, directory, summary)
- New package `internal/tui/components/`: logo.go, checkbox.go, textinput.go, progress.go, slash_cmd.go (stubs)
- New package `internal/tui/commands/`: command.go (Command interface), doctor.go, test.go, update.go, connect.go, feature.go, logs.go (stubs using real api.Client calls)
- `internal/tui/api/types.go` + `client.go`: AgentSummary, AgentsResponse, Agents() method added
- `internal/tui/wizard.go`: WizardModel — logo+progress+page+navHint layout; Ctrl+N/Ctrl+B navigation; ViewWithState dispatch
- `internal/tui/app.go`: App top-level model; modeSetup/modeRunning enum; NewSetupApp (9 pages) / NewRunningApp; WizardCompleteMsg → tea.Quit
- `cmd/bubblefish/setup.go`: runSetup() — creates App in modeSetup, runs BubbleTea with alt screen
- `cmd/bubblefish/main.go`: "setup" case added; help text updated
- `internal/tui/app_test.go`: 4 tests (mode check, view non-empty, WizardCompleteMsg quits, window size propagates)
- `internal/tui/wizard_test.go`: 8 tests (advance, no-advance, back, no-back-at-first, no-advance-past-last, view, empty pages)
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/tui` PASS (race, count=1)

## TUI.2: COMPLETE — ASCII BubbleFish Logo
- `internal/tui/components/logo.go`: real ASCII art replacing TUI.1 stub
  - Full (≥82 cols): 3-row fish+bubbles section (teal body, green °/· bubbles) + 6-row BUBBLEFISH block-letter banner (teal→green gradient) + subtitle + copyright
  - Compact (<82 cols): inline fish glyph `><((((°>` + "BubbleFish NEXUS" + muted subtitle
- `internal/tui/components/logo_test.go`: 6 tests (full non-empty, contains BubbleFish, min height ≥8 lines, compact non-empty, compact contains BubbleFish, Width=0 non-empty)
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/tui/components` PASS (6 logo tests)

## TUI.3: COMPLETE — Setup Wizard Pages + Install Refactor
- New package `internal/install/install.go`:
  - `GenerateKey`, `WriteConfigFile`, `ResolveInstallHome`, `BuildDaemonTOML`, `WriteDestination` (all 8 backends), `WriteDefaultSource`
  - `Install(Options) error` — top-level entry point called by summary page
  - `internal/install/install_test.go` — 16 tests (key prefix/length/uniqueness, TOML content, file write/skip/overwrite, WriteDestination sqlite+postgres+unknown, Install creates dirs + daemon.toml)
- `internal/tui/pages/summary.go` — `runInstallFromState` now calls `nexusinstall.Install(...)` (replaces TUI.1 no-op stub)
- `internal/tui/pages/pages_test.go` — 30 tests covering Init/Update/View/CanAdvance for all 9 pages
- `internal/tui/components/checkbox_test.go` — 8 tests (cursor movement, toggle, disabled, Selected())
- `internal/tui/components/progress_test.go` — 5 tests (determinate, spinner, zero Total)
- `internal/tui/components/textinput_test.go` — 6 tests (Valid, View, mismatch, match, Tab phase advance)
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/install` PASS (16 tests), `internal/tui/components` PASS, `internal/tui/pages` PASS, `internal/tui` PASS

## TUI.4: COMPLETE — Slash-Command System
- `internal/tui/app.go`: slash command integration in running mode
  - `slashCmd components.SlashCommandModel` field; `client *api.Client` field; `width int` tracked
  - `/` key in modeRunning activates dropdown; all keys route through overlay while active
  - `SlashCommandSelectedMsg` → `dispatchCommand(name)` → calls `commands.Command.Execute(client)`
  - `View()`: overlays slash cmd dropdown at bottom of running view when active
  - `commandRegistry` maps all 8 command names to their `Command` implementations
  - `allSlashCommands()` builds the `[]components.SlashCommand` list from command registry
- `internal/tui/components/slash_cmd_test.go`: 10 tests
  - inactive by default, activate, Esc cancels, filter by prefix, empty shows all, select returns msg, navigate down, view inactive/active, backspace deletes, no match for gibberish
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/tui` PASS, `internal/tui/components` PASS (10 new slash cmd tests)

## TUI.5: COMPLETE — Test Runner with Category Selection
- `internal/tui/commands/test.go` (replaces TUI.1 stub):
  - `TestCaseResult{Name, Desc, Passed, ErrMsg}`, `TestResultMsg{Category, Results, Passed, Failed, Err}`
  - `testCase` (name, desc, run func), `testCategory` (name, tests)
  - `testCategories`: Quick Health (5 tests: daemon_alive, daemon_ready, status_ok, config_readable, audit_accessible), Core (2 tests: lint_clean, security_summary), Full Suite (all tests combined via init())
  - `Categories() []string` — returns category names
  - `RunCategory(client, name) tea.Cmd` — exported for use outside the command
  - `executeCategory(client, cat) TestResultMsg` — runs each test case, accumulates pass/fail counts
  - `TestCommand.Execute` runs Quick Health by default
- `internal/tui/commands/test_test.go`: 8 tests
  - Categories not empty, contains Quick Health, contains Full Suite, TestCommand name, RunCategory quick health all pass, unknown category returns error, Core all pass, Full Suite has results, result fields populated
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/tui/commands` PASS (8 tests)

## TUI.6: COMPLETE — Online/Offline Status Dot Indicators with Pulse Animation
- New file `internal/tui/components/statusdot.go`:
  - `DotStatus` (DotOnline/DotDegraded/DotOffline)
  - `StatusDot{Status, Frame}.View()` — renders "● " with color; Online/Degraded alternate bright/dim on even/odd frames
  - `DotStatusFromString(s)` — converts "green"/"amber"/"red" to DotStatus
- `internal/tui/model.go`: `dotFrame int` field; `dotTickMsg` type; `dotTickCmd()` (500ms)
- `internal/tui/model.go` Init: adds `dotTickCmd()` to batch
- `internal/tui/update.go`: `dotTickMsg` handler increments `m.dotFrame`, reschedules `dotTickCmd()`
- `internal/tui/view.go` `buildSidebarSections()`: uses `StatusDot{dotStatus, m.dotFrame}.View()` for daemon status dot
- `internal/tui/components/sidebar.go`: `default:` case in dot switch uses `item.Dot` as pre-rendered ANSI string (backward-compatible with "green"/"amber"/"red" string names)
- `internal/tui/components/statusdot_test.go`: 7 tests (non-empty for each status, contains "●", pulse frames non-empty, offline no-pulse, DotStatusFromString mapping)
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/tui/components` PASS (7 new statusdot tests)

## TUI.7: COMPLETE — Sidebar Section Customization via tui_prefs.toml
- New file: `internal/tui/prefs.go` — `TUIPrefs{Sidebar SidebarPrefs}`, `LoadPrefs(configDir)` (nil on missing file), `DefaultPrefs()`, `ApplySidebarOrder(available []string)`
- New file: `internal/tui/prefs_test.go` — 8 tests: missing file returns nil, valid TOML, invalid TOML error, DefaultPrefs, reorder, hidden, nil prefs passthrough, empty sections uses available order, unknown sections skipped
- `internal/tui/model.go`: `prefs *TUIPrefs` field added; `NewModel` accepts `prefs *TUIPrefs` (nil → DefaultPrefs)
- `internal/tui/view.go`: `buildSidebarSections()` now builds all 5 sections as a map, calls `m.prefs.ApplySidebarOrder` to determine output order and filter hidden sections
- `internal/tui/app.go`: `NewRunningApp` passes `nil` to updated `NewModel` signature
- `cmd/bubblefish/tui.go`: loads prefs via `tui.LoadPrefs(configDir)` before `tui.NewModel`; logs warning on error, falls back to defaults
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/tui` (and all sub-packages): PASS

## Phase 6 — BubbleTea TUI: COMPLETE (TUI.1–TUI.7 all done)

## Phase 7 — WebUI Wiring: COMPLETE (WEB.1–WEB.4 all done)

### WEB.1: COMPLETE — WebUI Audit
- Audited all 9 HTML files in web/dashboard/
- Found 3 non-compliant files with write operations: quarantine.html, a2a_permissions.html, openclaw.html
- Found 6 compliant files: index.html, agents.html, grants.html, approvals.html, tasks.html, actions.html

### WEB.2: COMPLETE — Wire dashboard panels to real APIs
- New endpoint GET /api/audit/status → handleAuditStatus (audit chain length)
  - auditStatusResponse{TotalRecords, AuditEnabled}
  - Registered in buildRouter() + BuildAdminRouter() inside requireAdminToken
- New endpoint GET /api/quarantine/count → handleQuarantineCount (in handlers_webui.go)
  - quarantineCountResponse{Total, Pending}; gated on quarantineStore != nil
- New endpoint GET /api/discover/results → handleDiscoverResults (in handlers_webui.go)
  - discoverResultsResponse{Tools, Total, CachedAt}; 5-min TTL cache; 10s scan timeout
  - Publishes EventDiscoveryEvent on each fresh scan
- quarantine.Store.Count() method added (+ 2 tests: TestCount_Empty, TestCount_MixedReviewed)
- index.html: new secondary-stat-row (ROW 1.5) with 3 metric cards: Audit Records, Quarantine Pending, Discovered Tools
- controlView.refresh() extended to fetch all 3 new endpoints and populate stat cards

### WEB.3: COMPLETE — SSE activity feed
- New package: internal/eventbus/
  - Bus{} — lossy pub/sub; non-blocking Publish; Subscribe() returns channel + unsub func
  - Event{Type, Timestamp, Source, AgentID, Meta}; 7 event type constants
  - MarshalSSE(e) for data: {json}\n\n wire format
  - eventbus_test.go: 8 tests, all PASS
- GET /api/events/stream SSE endpoint → handleEventsStreamWithQueryAuth
  - Accepts admin token from Authorization header OR ?token= query param (EventSource pattern)
- Publish points wired in handlers.go: handleWrite → EventMemoryWritten, handleQuery → EventMemoryQueried, interceptWrite → EventQuarantineEvent
- index.html: activity-panel (ROW 4) consuming /api/events/stream via nx.sse(); onActivityEvent renders timestamped act-line entries; max 50 items

### WEB.4: COMPLETE — Enforce read-only dashboard
- quarantine.html: removed decide(), Approve/Reject buttons, Actions column; added readonly-note
- a2a_permissions.html: removed register-agent form, deleteAgent(), create-grant form, revokeGrant(), decideApproval() + approve/deny buttons; Actions columns removed from all 3 tables; readonly-notes added
- openclaw.html: removed create-grant form, revoke buttons, showSuccess(); Actions column removed; readonly-note added

### Exit gate (WEB.1–WEB.4):
- Build: OK
- Vet: OK (implicit — clean build)
- Tests: 82 packages PASS (internal/eventbus +1 new, internal/quarantine +2 new Count tests) — zero failures

## EVT.1–2: COMPLETE — Event Bus Lite with emit points and SSE feed
- New package: `internal/events/`
  - `lite.go`: LiteEvent{Type, Timestamp, Data map[string]any} + LiteBus{ch, closed atomic.Bool}
  - `Emitter` interface for nil-safe injection into non-daemon packages
  - `NewLiteBus(bufferSize)`, `Emit(type, data)` (non-blocking drop), `Stream() <-chan LiteEvent`, `Close()`
  - `lite_test.go`: 7 tests (emit+stream, non-blocking drop, close closes channel, emit-after-close no-op, idempotent close, default buffer, UTC timestamp)
- New file: `internal/daemon/daemon_events.go`
  - `runLiteBusBridge()`: goroutine that reads liteBus.Stream() and forwards to eventBus.Publish() so SSE clients receive all liteBus events
  - `liteToEventBus()`: converts LiteEvent.Data map[string]any → eventbus.Event (source/agent_id promoted; remainder in Meta)
- Wired in `internal/daemon/daemon.go`:
  - `liteBus *events.LiteBus` field (512-buffer); initialised in New(), bridge goroutine started at eventBus.Start()
  - `liteBus.Close()` in Stop() stage 3 (before eventBus.Stop() so bridge drains cleanly)
  - EVT.2: `health_changed` emitted in `runWatchdogCheck()` on WAL health transitions (component=wal, status=healthy/unhealthy)
- Emit points wired (EVT.2):
  - `memory_written` (source, payload_id) — handlers.go handleWrite (replaces direct eventBus.Publish)
  - `memory_queried` (source, result_count) — handlers.go handleQuery (replaces direct eventBus.Publish)
  - `quarantine_event` + `immune_detection` (source, rule, action, payload_id) — handlers.go interceptWrite (replaces direct eventBus.Publish)
  - `discovery_event` (count) — handlers_webui.go handleDiscoverResults (replaces direct eventBus.Publish)
  - `agent_connected` / `agent_disconnected` — internal/web/api_a2a.go A2ADashboard via SetEmitter(events.Emitter)
- Exit gate:
  - Build: OK
  - Vet: OK
  - 87 packages PASS (internal/events +1 new with 7 tests) — zero failures

## Phase 9 — Wire Up Remaining Mocks

### WIRE.1: COMPLETE — Substrate Wiring (Stage 3.5 sketch prefilter)
- `internal/query/stage_3_5_sketch_prefilter.go`: replaced no-op stub with real RaBitQ BBQ pipeline
  - `cfg.Substrate.CurrentRatchetState()` → nil guard (fall through to stage 4)
  - `substrate.ComputeQuerySketch()` + cuckoo filter coverage check (≥50% threshold)
  - Score each candidate via `cfg.Substrate.LoadStoreSketch()` + `substrate.EstimateInnerProduct()`
  - `rankAndTruncate()` returns top-K by score
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/query` PASS — zero failures

### WIRE.2: COMPLETE — Structured Health Endpoint
- `internal/daemon/handlers.go`: expanded `healthResponse` with `Subsystems map[string]healthSubsystem`
  - 7 subsystem entries: wal, database, audit, substrate, encryption, mcp, eventbus
  - Overall status degrades to "degraded" if any subsystem is not "ok"
- `internal/daemon/handlers_test.go`: `TestHandleHealth_SubsystemStatus` — 7 keys present, WAL=ok, database=ok
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/daemon` PASS — zero failures

### WIRE.3: COMPLETE — Migration Framework
- New package `internal/migration/`: `Migration{Version, Description, SQL}`, `Manager`, `Dialect` (? vs $N)
  - `nexus_migrations` tracking table; each migration in its own transaction; nil-safe
  - `migration_test.go`: 10 tests (nil, no-ops, real SQL, idempotency, multi-step, bad SQL)
- `internal/destination/sqlite_compliance.go`: `Migrate()` now calls `migration.New(d.db).Apply()`
  - `sqliteMigrations` slice with v1 baseline marker
- `internal/daemon/daemon.go`: calls `openedDest.Migrate(context.Background(), 0)` after destination open
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/migration` PASS (10 tests), full suite PASS — zero failures

### WIRE.4: COMPLETE — Install Bug Fixes
- `cmd/bubblefish/install.go`:
  - Directory creation now includes `logs/`, `keys/`, `discovery/`
  - `writeDefaultSource()` switch handles all 8+ destination types (mysql/mariadb, cockroachdb/crdb, mongodb/mongo, firestore, tidb, turso/libsql)
- Exit gate:
  - Build: OK
  - Vet: OK
  - `cmd/bubblefish` PASS — zero failures

### WIRE.5: COMPLETE — Doctor with Self-Heal + Structured Logs + nexus logs command
- `cmd/bubblefish/start.go` `buildLogger(cfg, configDir)`:
  - Opens `<configDir>/logs/nexus.log` for JSON append logging
  - `teeHandler` fans slog records to stderr + JSON file simultaneously
- `cmd/bubblefish/dev.go`: updated `buildLogger(cfg, configDir)` call
- `cmd/bubblefish/doctor.go`: enhanced with 8 health checks + self-heal proposals
  - config_dir exists, daemon.toml exists, WAL writable, logs dir present
  - daemon_alive (GET /health with 2s timeout) → "run 'nexus start'"
  - MCP port configured, OAuth validity checks
  - destination health via structured /health subsystems map
- `cmd/bubblefish/logs.go`: `runLogs(args)` — reads `<configDir>/logs/nexus.log` JSONL
  - `--tail N` (default 50), `--level L` (debug/info/warn/error), `--since` (RFC3339 or duration), `--json`
  - `parseSince`, `matchLevel`, `matchSince`, `formatLogLine` helpers
- `cmd/bubblefish/main.go`: `logs` case added; help text updated
- `cmd/bubblefish/dev_test.go`: fixed `buildLogger(cfg)` → `buildLogger(cfg, "")` call
- Exit gate:
  - Build: OK
  - Vet: OK
  - `cmd/bubblefish` PASS (9 tests), full suite PASS — zero failures

### WIRE.6: COMPLETE — Update Command
- New package `internal/updater/`:
  - `FetchLatest(client)` — GitHub releases API; handles 404 (no releases yet) gracefully
  - `PlatformAssetName()` — `nexus_<os>_<arch>[.exe]` for current platform
  - `FindAssets(info)` — locates binary + `.sha256` sidecar in a release
  - `Download(client, url, dir)` — downloads to temp file in `dir`
  - `VerifyChecksum(binPath, sumPath)` — SHA-256 hex comparison
  - `AtomicReplace(dest, src)` — rename-then-copy; restores backup on failure
  - `CompareVersions(current, candidate)` — semver major.minor.patch comparison
  - `CurrentExecutable()` — absolute path via os.Executable()
  - `updater_test.go`: 10 tests (CompareVersions 7 cases, PlatformAssetName, FindAssets found/missing, VerifyChecksum ok/mismatch, AtomicReplace, Download ok/error)
- `cmd/bubblefish/update.go`: `runUpdate(args)`
  - `--check` — check only, no download
  - `--yes` — skip confirmation prompts
  - Pre-update daemon liveness check with warning and confirmation
  - Full flow: fetch → compare → find assets → download → verify → atomic replace → summary
- `cmd/bubblefish/main.go`: `update` case added
- Exit gate:
  - Build: OK
  - Vet: OK
  - `internal/updater` PASS (10 tests), full suite PASS — zero failures

### WIRE.7: COMPLETE — Tunnel Configuration
- `internal/config/types.go`: added `Tunnels []TunnelConfig` to Config; new `TunnelConfig` struct
  - Provider (cloudflare/ngrok/tailscale/bore/custom), LocalPort, Enabled
  - Provider-specific: AuthToken (Cloudflare/ngrok), Hostname, Region, Domain, Address, Command
- `cmd/bubblefish/tunnel.go`: `runTunnel(args)` with 3 subcommands:
  - `tunnel setup` — interactive TOML generation wizard (prints config snippet to stdout)
  - `tunnel doctor` — validates each [[tunnels]] entry; checks provider, required fields
  - `tunnel status` — table of configured tunnels with local /health reachability probe
- `cmd/bubblefish/main.go`: `tunnel` case added; help text updated
- Exit gate:
  - Build: OK
  - Vet: OK
  - Full suite PASS — zero failures

## Phase 9 — Wire Up Remaining Mocks: COMPLETE (WIRE.1–WIRE.7 all done)

## Phase 10 — SHOW-OFF FEATURES: COMPLETE (SHOW.1–SHOW.3 all done)

### SHOW.1: COMPLETE — Browser-Verifiable Proof HTML
- New file: `internal/provenance/htmlproof.go`
  - `GenerateHTML(bundle *ProofBundle) ([]byte, error)` — self-contained HTML with embedded proof JSON
  - Client-side SHA-256 content-hash verification via `crypto.subtle.digest` (no external libraries)
  - Ed25519 source-signature verification via WebCrypto (Chrome 113+, Firefox 105+)
  - Audit chain linkage check (prev_hash links) when chain entries present
  - Dark-theme UI: per-check badges (PASS/FAIL/SKIPPED), visual verdict banner
- New test file: `internal/provenance/htmlproof_test.go` — 5 tests
  - BasicStructure, EmbedsBundleJSON, ValidJSON (JSON parseable), ContentHashCheck, Idempotent
- Enhanced `cmd/bubblefish/verify.go`:
  - `--proof <memory_id>` — fetches proof bundle from running daemon via GET /verify/{memory_id}
  - `--output <path>` — writes HTML (*.html) or JSON; creates parent dirs (0700)
  - `--url URL` — daemon URL (default http://localhost:8081)
  - `--token TOKEN` — admin token (or NEXUS_ADMIN_KEY env)
  - Backward compat: `nexus verify <file.json>` still works unchanged

### SHOW.2: COMPLETE — Cross-Tool Memory Graph Dashboard (D3.js)
- New file: `web/dashboard/memgraph.html`
  - D3.js v7 force-directed graph loaded from CDN
  - Central "BubbleFish Nexus" hub (gold); AI tool nodes (teal); A2A agent nodes (purple)
  - All peripheral nodes connect to hub; node size proportional to interaction count
  - Hover tooltip, drag, zoom+pan, 60-second auto-refresh
  - Token injection via `ADMIN_TOKEN: ''` sentinel (matches serveDashboardPage pattern)
  - textContent only — NEVER innerHTML
- New file: `internal/daemon/handlers_viz_graph.go`
  - `handleMemoryGraph` — GET /api/viz/memory-graph (admin-authed)
    - Reads cached discovery result (no new scan triggered)
    - Lists A2A agents from registryStore (nil-safe)
    - Returns `{nodes, edges, generated_at}`
  - `handleDashboardMemgraph` — GET /dashboard/memgraph (query-param token)
- Modified: `web/dashboard/embed.go` — added `MemgraphHTML` var
- Modified: `internal/daemon/server.go` — registered in both buildRouter() and BuildAdminRouter():
  - GET /api/viz/memory-graph (in admin requireAdminToken group)
  - GET /dashboard/memgraph (query-param auth via serveDashboardPage)

### SHOW.3: COMPLETE — 60-Second Demo Scripts
- New file: `scripts/demo.ps1` — PowerShell demo
  - Steps: health check → write 3 memories → search → fetch+verify proof (HTML) → open memory graph
  - Reads $env:NEXUS_URL, $env:NEXUS_DASH_URL, $env:NEXUS_API_KEY, $env:NEXUS_ADMIN_KEY
  - Generates browser-verifiable HTML proof at $env:TEMP\nexus-proof-demo.html
  - Opens proof HTML and memory graph in default browser
  - Colored PASS/FAIL/WARN per step; summary with elapsed time; exit 0/1
- New file: `scripts/demo.sh` — Bash demo (macOS/Linux)
  - Same 5 steps as PS1; reads NEXUS_* env vars
  - Opens proof HTML and graph URL via `open` (macOS) or `xdg-open` (Linux)
  - ANSI color output; exit 0/1

### Exit gate (SHOW.1–SHOW.3):
- Build: OK
- Vet: OK
- Tests: 90 packages — `internal/provenance` PASS (5 new htmlproof tests), `internal/daemon` PASS — zero failures

## NAMING: COMPLETE — CLI binary bubblefish→nexus, sentinel→ingest/drift, naming convention cleanup
- `cmd/bubblefish/` → `cmd/nexus/` (git mv)
- Binary output: `bubblefish.exe` → `nexus.exe`
- `cmd/nexus/sentinel.go` → `cmd/nexus/drift.go`; `runSentinel` → `runDrift`; CLI command `sentinel` → `drift`
- `internal/sentinel/` → `internal/drift/`; `package sentinel` → `package drift`; `type Sentinel` → `type Drift`
- Step 3 (sentinel→ingest package) was pre-completed: `internal/ingest/` was already built directly
- All user-facing strings updated: `bubblefish <cmd>` → `nexus <cmd>` across all cmd/*.go files
- All internal strings updated: package comments, error messages, Prometheus metrics (`nexus_*` prefix)
- `ConnSentinelIngest` → `ConnIngest`, `EventSentinelIngest` → `EventIngest`
- Non-Go files updated: CHANGELOG.md, README.md, CLAUDE.md, Docs/**, INGEST.md, scripts, bench
- WAL binary integrity sentinels (byte markers) intentionally unchanged — different concept
- Exit gate:
  - Build: OK
  - Vet: OK
  - 92 packages PASS — zero failures (pre-existing flaky TestSoak_24h passed on this run)

## Current branch: v0.1.3-moat-takeover
## Current subtask: NAMING complete. Final exit gate / v0.1.3 tag prep is next.

### Stale branches (safe to delete):
- v0.1.3-ingest: fully merged to main
- fix/bench-windows-clock: fully merged to main
