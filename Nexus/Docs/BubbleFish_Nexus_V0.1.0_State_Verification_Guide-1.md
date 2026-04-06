# BubbleFish Nexus v0.1.0 — State & Verification Guide Addendum #1
# AI Agent Audit & Memory Gateway: Interaction Log, Retrieval Firewall, Black Box Recorder
# For: Claude Code (Agentic Coding Tool)
# Mode: Enterprise-Grade, Agent-Optimized
# Build Type: ADDENDUM (extends existing codebase from base phases Init through R-28)
# License: GNU Affero General Public License v3.0 (AGPL-3.0)
# © 2026 BubbleFish Technologies, Inc. All rights reserved.
# Rule: ONE phase per session. Quality Gate must pass before proceeding.
# Rule: If ANY requirement is ambiguous, STOP and ask the human operator.
# Rule: No shortcuts, no TODOs, no placeholder implementations, no mock security.

> **ADDENDUM NOTE:** These phases (R-29 through R-35) extend the base State &
> Verification Guide. They MUST be executed AFTER all base phases (Init through R-28)
> are complete. The Tech Spec Addendum #1 is the authoritative behavioral contract.

> **COPYRIGHT:** Every .go file MUST include the standard AGPL-3.0 header.

---

## Addendum Phase Map

| Phase | Title | Primary Persona | Duration |
|-------|-------|-----------------|----------|
| R-29 | Interaction Log Engine | Senior Backend / Storage Engineer | 2–3 days |
| R-30 | Sensitivity Labels + Schema Migration | Database / Data Infra Specialist | 1–2 days |
| R-31 | Retrieval Firewall Engine | Security Architect + Search & Retrieval Engineer | 2–3 days |
| R-32 | Audit Query API | Principal Systems Architect | 1–2 days |
| R-33 | Audit CLI Commands | Developer Experience Engineer | 1 day |
| R-34 | Dashboard Audit Tab | UX / Frontend Engineer | 1–2 days |
| R-35 | Addendum Ship Verification | Principal Systems Architect | 1 day |

**Universal Quality Gate (run after every phase):**

```
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./... -race -count=1
```

---

## Addendum Spec Cross-Reference Map

| Tech Spec Addendum Section | Phases That Reference It |
|---------------------------|------------------------|
| Section A1 — Strategic Context | All addendum phases |
| Section A2 — Interaction Log | R-29, R-32, R-33, R-34 |
| Section A3 — Retrieval Firewall | R-30, R-31 |
| Section A4 — Configuration | R-29, R-30, R-31, R-32 |
| Section A5 — CLI Commands | R-33 |
| Section A6 — HTTP API | R-32 |
| Section A7 — V3 Readiness | R-35 |
| Section A8 — Glossary | All addendum phases |

---

═══════════════════════════════════════════════════════
## PHASE R-29: INTERACTION LOG ENGINE
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Addendum Section A2
PERSONA: Senior Backend / Storage Engineer
DURATION: 2–3 days

─── OBJECTIVE ───

Create the AI Interaction Log engine: an append-only, CRC32-protected, optionally HMAC'd audit trail that records every HTTP interaction with Nexus. This is the Black Box Recorder. It shares durability primitives with the WAL but is a separate file, separate concern, and separate package.

─── REQUIRED EXPERT MINDSET ───

Senior Backend / Storage Engineer. You are building an audit log that must survive crash, power failure, and disk theft investigation. Every entry must have a CRC32 checksum. Every write must fsync. The log must never block the hot path — if the log file is unwritable, the request still succeeds. You treat this log with the same reverence as the WAL.

─── STRICT END-STATE ───

Files to CREATE:
- internal/audit/record.go — InteractionRecord struct matching Tech Spec Addendum Section A2.2
- internal/audit/logger.go — AuditLogger: Append, Rotate, Close methods
- internal/audit/reader.go — AuditReader: Read, Filter, Count methods (for query API)
- internal/audit/logger_test.go — Durability, CRC32, rotation, concurrent writes, error handling
- internal/audit/reader_test.go — Filtering, pagination, CRC32 validation on read

Packages/Interfaces to export:
- `audit.InteractionRecord` struct
- `audit.AuditLogger` struct with `Log(record InteractionRecord) error`
- `audit.AuditReader` struct with `Query(filter AuditFilter) ([]InteractionRecord, int, error)`

─── BEHAVIORAL CONTRACT ───

1. InteractionRecord struct matches Tech Spec Addendum Section A2.2 exactly. All fields, all JSON tags, all types. Reference: Tech Spec Addendum A2.2.
2. Log file format: `JSON_BYTES<TAB>CRC32_HEX<NEWLINE>`. CRC32 computed over JSON bytes with `crc32` field set to empty string. Reference: Tech Spec Addendum A2.3.
3. When WAL integrity mode is `mac`, interaction log also appends HMAC: `JSON_BYTES<TAB>CRC32_HEX<TAB>HMAC_HEX<NEWLINE>`. Same HMAC key as WAL. Reference: Tech Spec Addendum A2.3.
4. Every Append call does fsync before returning. Reference: Tech Spec Addendum A2.3.
5. File opened with `O_APPEND | O_WRONLY | O_CREATE`, permissions `0600`. Directory `0700`. Reference: Tech Spec Addendum A2.3.
6. Rotation: when file exceeds `max_file_size_mb`, current file renamed to `interactions-YYYYMMDD-HHMMSS.jsonl`, new file created. Rotation is atomic (rename + create). Reference: Tech Spec Addendum A2.3.
7. Append failure MUST NOT panic or cause request failure. Return error; caller logs WARN and increments `bubblefish_audit_log_errors_total`. Reference: Tech Spec Addendum A2.4.
8. record_id generated via crypto/rand UUID. MUST be unique. Reference: Tech Spec Addendum A2.2.
9. AuditReader parses JSONL, validates CRC32 on each entry, skips entries with CRC mismatch (log WARN). Reference: Tech Spec Addendum A2.5.
10. AuditReader supports all filter parameters from Tech Spec Addendum A2.5: source, actor_type, actor_id, operation, policy_decision, subject, destination, after, before, limit, offset. Reference: Tech Spec Addendum A2.5.
11. AuditReader reads ALL rotated log files (discovered via glob), sorted by filename (oldest first). Reference: Tech Spec Addendum A2.3.
12. Concurrent Append calls are safe (mutex-protected). Reference: Global Directives — Concurrency Safety.

─── INVARIANTS ───

1. NEVER: Write interaction records without CRC32.
2. NEVER: Skip fsync after append.
3. NEVER: Block the hot path on audit log failure. Swallow error, log WARN, increment metric.
4. NEVER: Log secret values (API keys, tokens) in interaction records.
5. NEVER: Include memory content in interaction records. Only metadata.
6. NEVER: Use sequential or predictable record_id values.

─── SECURITY CHECKPOINT ───

1. Interaction log files: 0600. Directory: 0700. Reference: Tech Spec Addendum A2.3.
2. CRC32 provides corruption detection. HMAC (when enabled) provides tamper detection. Reference: Tech Spec Addendum A2.3.
3. IP addresses in records are subject to GDPR. Document this in comments. Reference: Tech Spec Addendum A2.3.

─── IMPLEMENTATION DIRECTIVES ───

- USE: `hash/crc32` with `crc32.ChecksumIEEE` — same as WAL
- USE: `encoding/json` + `sync.Pool` for serialization
- USE: `sync.Mutex` protecting the file handle and rotation logic
- USE: `crypto/rand` for record_id UUID generation
- USE: `os.OpenFile` with `O_APPEND|O_WRONLY|O_CREATE` and `0600` permissions
- USE: `filepath.Glob` to discover rotated log files
- USE: `bufio.NewScanner` with 10MB buffer for reading (same as WAL)
- AVOID: Writing memory content to interaction records
- AVOID: Any global state — AuditLogger struct holds all state
- EDGE CASE: Log file deleted while daemon running → next Append recreates file
- EDGE CASE: Disk full → Append returns error, caller handles gracefully
- EDGE CASE: Concurrent rotation + append → mutex ensures atomicity

─── VERIFICATION GATE ───

Compilation:
```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

Behavioral Verification:
- [ ] Write 100 interaction records. All have CRC32 after tab.
- [ ] Corrupt one record (flip byte). Reader skips it with WARN. Other 99 readable.
- [ ] Rotation triggers at configured size. New file created. Old file renamed correctly.
- [ ] Concurrent writes from 50 goroutines — zero data corruption, zero race reports.
- [ ] Append failure (unwritable file) — returns error, does NOT panic.
- [ ] HMAC mode: records have CRC32 + HMAC when WAL integrity=mac.
- [ ] Reader filtering: filter by source returns only matching records.
- [ ] Reader filtering: filter by time range returns only records in range.
- [ ] Reader filtering: limit and offset work correctly.
- [ ] Reader reads across multiple rotated files in chronological order.
- [ ] record_id is unique across 1000 records.
- [ ] No memory content in any interaction record (grep for "content" field).
- [ ] File permissions are 0600.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-29: Interaction Log Engine (Black Box Recorder)"
```

═══════════════════════════════════════════════════════
## PHASE R-30: SENSITIVITY LABELS + SCHEMA MIGRATION
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Addendum Sections A3.2, A3.3, A4.2, A4.3
PERSONA: Database / Data Infra Specialist
DURATION: 1–2 days

─── OBJECTIVE ───

Add sensitivity_labels and classification_tier fields to TranslatedPayload, WAL entries, destination schemas (SQLite + Postgres), and source config parsing. This phase adds the DATA layer for the Retrieval Firewall — the enforcement logic comes in R-31.

─── STRICT END-STATE ───

Files to MODIFY:
- internal/config/types.go — Add retrieval_firewall fields to source policy struct
- internal/config/daemon.go — Add [daemon.audit] and [daemon.retrieval_firewall] parsing
- internal/destination/sqlite.go — Schema migration for sensitivity_labels + classification_tier
- internal/destination/postgres.go — Schema migration with array type + GIN index
- internal/daemon/handlers.go — Parse X-Sensitivity-Labels and X-Classification-Tier headers on write path

Files that already have these structures and need fields added:
- TranslatedPayload struct (wherever defined) — Add SensitivityLabels []string, ClassificationTier string

─── BEHAVIORAL CONTRACT ───

1. TranslatedPayload gains `SensitivityLabels []string` and `ClassificationTier string`. Reference: Tech Spec Addendum A3.2.
2. X-Sensitivity-Labels header parsed as comma-separated, trimmed. Reference: Tech Spec Addendum A3.2.
3. X-Classification-Tier header parsed as single trimmed string. Reference: Tech Spec Addendum A3.2.
4. If headers absent, labels default to empty, tier defaults to source's `default_classification_tier` (default: "public"). Reference: Tech Spec Addendum A3.2.
5. MCP writes: sensitivity_labels and classification_tier as tool input parameters. Reference: Tech Spec Addendum A3.2.
6. SQLite migration: `ALTER TABLE ... ADD COLUMN sensitivity_labels TEXT DEFAULT ''` + `ADD COLUMN classification_tier TEXT DEFAULT 'public'` + index. Reference: Tech Spec Addendum A4.3.
7. Postgres migration: `ALTER TABLE ... ADD COLUMN sensitivity_labels TEXT[] DEFAULT '{}'` + `ADD COLUMN classification_tier TEXT DEFAULT 'public'` + GIN index + B-tree index. Reference: Tech Spec Addendum A4.3.
8. Labels and tier stored in WAL entry alongside existing fields. Reference: Tech Spec Addendum A3.2.
9. Labels and tier included in interaction record (write operations). Reference: Tech Spec Addendum A2.2 (`sensitivity_labels_set` field).
10. Source TOML gains `[source.policy.retrieval_firewall]` block with blocked_labels, max_classification_tier, required_labels, default_classification_tier, visible_namespaces, cross_namespace_read. Reference: Tech Spec Addendum A4.2.
11. daemon.toml gains `[daemon.audit]` block (enabled, log_file, max_file_size_mb, admin_rate_limit_per_minute). Reference: Tech Spec Addendum A4.1.
12. daemon.toml gains `[daemon.retrieval_firewall]` block (enabled, tier_order, default_tier). Reference: Tech Spec Addendum A4.1.
13. Existing data survives migration with default values ('', 'public'). Reference: Tech Spec Addendum A4.3.

─── INVARIANTS ───

1. NEVER: Break existing data on schema migration. Always use DEFAULT values.
2. NEVER: Store sensitivity labels in a format that can't be parsed back to []string.
3. NEVER: Accept invalid classification tiers (not in tier_order list). Return 400.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Write with X-Sensitivity-Labels: pii,financial. WAL and DB have both labels.
- [ ] Write without headers. Labels empty, tier "public" in WAL and DB.
- [ ] Write with X-Classification-Tier: confidential. Stored correctly.
- [ ] Write with invalid tier (not in tier_order). Returns 400.
- [ ] Schema migration: existing data has default empty labels and "public" tier.
- [ ] Source TOML with [source.policy.retrieval_firewall] parses correctly.
- [ ] Source TOML without retrieval_firewall section → defaults applied (backward compatible).
- [ ] daemon.toml [daemon.audit] and [daemon.retrieval_firewall] parse correctly.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-30: Sensitivity Labels + Schema Migration"
```

═══════════════════════════════════════════════════════
## PHASE R-31: RETRIEVAL FIREWALL ENGINE
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Addendum Sections A3.1, A3.4, A3.5, A3.6, A3.7, A3.8
PERSONA: Security Architect + Search & Retrieval Engineer
DURATION: 2–3 days

─── OBJECTIVE ───

Implement the Retrieval Firewall: pre-query tier enforcement in Stage 0, post-retrieval label/tier/namespace filtering after Stage 5, security event emission, and metrics. This is the enforcement layer that makes sensitivity labels meaningful.

─── STRICT END-STATE ───

Files to CREATE:
- internal/firewall/firewall.go — RetrievalFirewall struct with PreQuery and PostFilter methods
- internal/firewall/firewall_test.go — Comprehensive tests for all enforcement rules

Files to MODIFY:
- internal/query/cascade.go — Wire PreQuery into Stage 0, PostFilter after Stage 5
- internal/security/events.go — Add retrieval_firewall_filtered and retrieval_firewall_denied event types
- internal/metrics/metrics.go — Add retrieval firewall metrics

─── BEHAVIORAL CONTRACT ───

1. PreQuery (Stage 0 enhancement): source has max_classification_tier → verify query not requesting above max. Exceeds → 403 `retrieval_firewall_denied`, reason `tier_exceeds_maximum`. Reference: Tech Spec Addendum A3.5.
2. PostFilter (after Stage 5): for each memory, check sensitivity_labels against blocked_labels. ANY match → remove. Reference: Tech Spec Addendum A3.5.
3. PostFilter: check classification_tier against max_classification_tier. Exceeds → remove. Reference: Tech Spec Addendum A3.5.
4. PostFilter: check sensitivity_labels against required_labels (if non-empty). Missing required → remove. Reference: Tech Spec Addendum A3.5.
5. PostFilter: record removed memories in interaction record sensitivity_labels_filtered. Reference: Tech Spec Addendum A3.5.
6. PostFilter: if ALL results removed → policy_decision "filtered", empty results, `_nexus.retrieval_firewall_filtered: true`. Reference: Tech Spec Addendum A3.5.
7. visible_namespaces: if set, source only sees memories in those namespaces. Reference: Tech Spec Addendum A3.6.
8. cross_namespace_read: if true, source reads any namespace. Reference: Tech Spec Addendum A3.6.
9. Security event retrieval_firewall_filtered emitted when memories removed. Reference: Tech Spec Addendum A3.7.
10. Security event retrieval_firewall_denied emitted when query fully denied. Reference: Tech Spec Addendum A3.7.
11. Metrics: bubblefish_retrieval_firewall_filtered_total, bubblefish_retrieval_firewall_denied_total, bubblefish_retrieval_firewall_latency_seconds. Reference: Tech Spec Addendum A2.6, A3.8.
12. If [daemon.retrieval_firewall] enabled = false, all firewall logic is skipped. Zero overhead. Reference: Tech Spec Addendum A4.1.
13. blocked_labels are ABSOLUTE. No override. No admin bypass. Reference: Tech Spec Addendum A3.5.
14. Performance: at most 0.1ms per result. Metadata only. Reference: Tech Spec Addendum A3.5.

─── INVARIANTS ───

1. NEVER: Allow bypassing blocked_labels via any mechanism (admin token, debug flag, query param).
2. NEVER: Perform content inspection in the retrieval firewall. Metadata only.
3. NEVER: Block the read hot path longer than 0.1ms per result.
4. NEVER: Skip PostFilter when retrieval_firewall is enabled, regardless of retrieval profile.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Source with blocked_labels=["pii"]. Memory with label "pii" → not in results.
- [ ] Source with blocked_labels=["pii"]. Memory without "pii" label → in results.
- [ ] Source with max_classification_tier="internal". Memory tier "confidential" → not in results.
- [ ] Source with max_classification_tier="internal". Memory tier "public" → in results.
- [ ] Source with required_labels=["financial"]. Memory without "financial" → not in results.
- [ ] All results filtered → empty response with retrieval_firewall_filtered=true.
- [ ] visible_namespaces=["shared"]. Memory in "private" namespace → not in results.
- [ ] cross_namespace_read=true → source sees all namespaces.
- [ ] Security event retrieval_firewall_filtered emitted with correct fields.
- [ ] Security event retrieval_firewall_denied emitted on full denial.
- [ ] Metrics incremented correctly.
- [ ] retrieval_firewall enabled=false → no filtering, zero overhead.
- [ ] Admin token does NOT bypass blocked_labels (test this explicitly).
- [ ] Benchmark: 1000 results filtered in < 1ms.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-31: Retrieval Firewall Engine"
```

═══════════════════════════════════════════════════════
## PHASE R-32: AUDIT QUERY API
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Addendum Sections A2.5, A6
PERSONA: Principal Systems Architect
DURATION: 1–2 days

─── OBJECTIVE ───

Wire the interaction log into the HTTP server with the /api/audit/log, /api/audit/stats, and /api/audit/export endpoints. Wire interaction record emission into the write and read hot paths (steps 19a and 14a). Register all audit metrics.

─── STRICT END-STATE ───

Files to MODIFY:
- internal/daemon/handlers.go — Wire interaction record emission into write + read paths
- internal/daemon/routes.go — Register /api/audit/* endpoints
- internal/metrics/metrics.go — Register audit metrics

Files to CREATE:
- internal/daemon/audit_handlers.go — Handlers for /api/audit/log, /api/audit/stats, /api/audit/export

─── BEHAVIORAL CONTRACT ───

1. Interaction record emitted at step 19a (writes) and 14a (reads). Reference: Tech Spec Addendum A2.4.
2. Denied requests (401, 403, 429, 413) emit interaction record with policy_decision "denied". Reference: Tech Spec Addendum A2.4.
3. GET /api/audit/log: admin token required. Data token → 401 wrong_token_class. Reference: Tech Spec Addendum A2.5.
4. All query parameters from Tech Spec Addendum A2.5 supported: source, actor_type, actor_id, operation, policy_decision, subject, destination, after, before, limit, offset. Reference: Tech Spec Addendum A2.5.
5. Response format matches Tech Spec Addendum A2.5: {records, total_matching, limit, offset, has_more}. Reference: Tech Spec Addendum A2.5.
6. Rate limited at admin_rate_limit_per_minute (default 60). Reference: Tech Spec Addendum A2.5.
7. /api/audit/stats: returns interactions/hour, denial rate, top sources, top actors. Reference: Tech Spec Addendum A6.
8. /api/audit/export: JSON array or CSV based on Accept header. Reference: Tech Spec Addendum A6.
9. Audit log write failure does NOT fail the request. WARN log + metric increment. Reference: Tech Spec Addendum A2.4.
10. All audit metrics registered and incrementing. Reference: Tech Spec Addendum A2.6.

─── INVARIANTS ───

1. NEVER: Let audit log failure cause request failure.
2. NEVER: Expose /api/audit/* to data-plane tokens.
3. NEVER: Return more than 1000 records per query (hard cap).

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] POST /inbound/claude → interaction record emitted with operation_type "write".
- [ ] GET /query/sqlite → interaction record emitted with operation_type "query".
- [ ] 401 on bad auth → interaction record emitted with policy_decision "denied".
- [ ] GET /api/audit/log with admin token → returns records.
- [ ] GET /api/audit/log with data token → 401 wrong_token_class.
- [ ] Filter by source → only matching records returned.
- [ ] Filter by time range → only records in range.
- [ ] limit=10, offset=5 → correct pagination.
- [ ] /api/audit/stats returns valid statistics.
- [ ] /api/audit/export with Accept: text/csv → valid CSV output.
- [ ] Audit metrics incrementing (check /metrics endpoint).
- [ ] Rate limiting on /api/audit/log → 429 after limit exceeded.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-32: Audit Query API"
```

═══════════════════════════════════════════════════════
## PHASE R-33: AUDIT CLI COMMANDS
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Addendum Section A5
PERSONA: Developer Experience Engineer
DURATION: 1 day

─── OBJECTIVE ───

Add `bubblefish audit tail`, `bubblefish audit query`, `bubblefish audit stats`, and `bubblefish audit export` CLI commands.

─── BEHAVIORAL CONTRACT ───

1. `bubblefish audit tail`: streams interaction log entries to stdout, auto-refreshing. --source, --actor-type, --operation filters. Reference: Tech Spec Addendum A5.
2. `bubblefish audit query`: queries interaction log with same params as API. JSON output to stdout. Reference: Tech Spec Addendum A5.
3. `bubblefish audit stats`: prints summary statistics. Reference: Tech Spec Addendum A5.
4. `bubblefish audit export`: exports to file. --format json|csv. --after, --before filters. Reference: Tech Spec Addendum A5.
5. All commands read from interaction log files directly (no running daemon required for query/stats/export). Reference: Implementation detail.
6. `audit tail` connects to running daemon via SSE or file tail. Reference: Implementation detail.

─── VERIFICATION GATE ───

- [ ] `bubblefish audit query --source claude` returns only claude records.
- [ ] `bubblefish audit stats` prints valid statistics.
- [ ] `bubblefish audit export --format csv --after 2026-04-01T00:00:00Z` produces valid CSV.
- [ ] `bubblefish audit tail` streams new records as they arrive.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-33: Audit CLI Commands"
```

═══════════════════════════════════════════════════════
## PHASE R-34: DASHBOARD AUDIT TAB
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Addendum Section A2.7
PERSONA: UX / Frontend Engineer
DURATION: 1–2 days

─── OBJECTIVE ───

Add Audit tab to the web dashboard showing recent interactions, per-agent timelines, policy denial feed, and statistics.

─── BEHAVIORAL CONTRACT ───

1. Audit tab: recent 50 interactions, auto-refreshing. Sortable. Reference: Tech Spec Addendum A2.7.
2. Per-agent timeline view. Reference: Tech Spec Addendum A2.7.
3. Policy denial feed: real-time denied/filtered interactions. Reference: Tech Spec Addendum A2.7.
4. Statistics: interactions/hour, denial rate, firewall filter rate. Reference: Tech Spec Addendum A2.7.
5. textContent ONLY. NEVER innerHTML. Reference: Security Checkpoint.

─── VERIFICATION GATE ───

- [ ] Audit tab renders with interaction records.
- [ ] Per-agent timeline works when actor_id selected.
- [ ] Denial feed updates in real time.
- [ ] No innerHTML in audit tab code (grep audit tab JS).

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-34: Dashboard Audit Tab"
```

═══════════════════════════════════════════════════════
## PHASE R-35: ADDENDUM SHIP VERIFICATION
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Addendum — all sections
PERSONA: Principal Systems Architect
DURATION: 1 day

─── OBJECTIVE ───

Full regression verification of all addendum features. Every item must pass. This is the gate before tagging the release with addendum features.

─── ADDENDUM SHIP CHECKLIST (all items must pass) ───

Interaction Log:
- [ ] Write → interaction record emitted with correct fields
- [ ] Read → interaction record emitted with correct fields
- [ ] Denied request → interaction record with policy_decision "denied"
- [ ] CRC32 on every interaction record (validate manually)
- [ ] HMAC on records when integrity=mac (validate manually)
- [ ] fsync on every append (strace or equivalent)
- [ ] Log rotation at configured size
- [ ] Log write failure → request still succeeds, WARN logged, metric incremented
- [ ] No memory content in interaction records
- [ ] record_id unique across 1000 records
- [ ] File permissions 0600

Retrieval Firewall:
- [ ] blocked_labels: memory with blocked label → invisible
- [ ] blocked_labels: admin token does NOT bypass
- [ ] max_classification_tier: memory above max → invisible
- [ ] required_labels: memory missing required → invisible
- [ ] All results filtered → empty response with retrieval_firewall_filtered=true
- [ ] visible_namespaces: source only sees configured namespaces
- [ ] cross_namespace_read: true → sees all namespaces
- [ ] Security events emitted for filtered and denied
- [ ] Metrics incrementing correctly
- [ ] enabled=false → zero overhead, no filtering
- [ ] Performance: 1000 results filtered in < 1ms

Audit API:
- [ ] /api/audit/log: admin only, data token → 401
- [ ] All query parameters working
- [ ] Pagination (limit + offset) correct
- [ ] /api/audit/stats returning valid data
- [ ] /api/audit/export CSV output valid
- [ ] Rate limiting enforced

CLI:
- [ ] `bubblefish audit query` returns records
- [ ] `bubblefish audit stats` prints statistics
- [ ] `bubblefish audit export` produces file

Dashboard:
- [ ] Audit tab renders correctly
- [ ] No innerHTML (grep)

Schema:
- [ ] SQLite: sensitivity_labels and classification_tier columns exist
- [ ] Postgres: sensitivity_labels array and classification_tier columns with indexes
- [ ] Existing data survives migration

Configuration:
- [ ] [daemon.audit] parsed correctly
- [ ] [daemon.retrieval_firewall] parsed correctly
- [ ] [source.policy.retrieval_firewall] parsed correctly
- [ ] Missing config sections → backward-compatible defaults

Code Quality:
- [ ] `go build ./...` — zero warnings
- [ ] `go vet ./...` — zero findings
- [ ] `CGO_ENABLED=1 go test ./... -race -count=1` — zero failures, zero race reports
- [ ] All new .go files have AGPL-3.0 copyright header
- [ ] No TODO comments in addendum code

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-35: Addendum Ship Verification — Audit & Firewall"
```

═══════════════════════════════════════════════════════
# END OF STATE & VERIFICATION GUIDE ADDENDUM #1
═══════════════════════════════════════════════════════
