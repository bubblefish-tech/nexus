# BubbleFish Nexus v0.1.0 — State & Verification Guide
# For: Claude Code (Agentic Coding Tool)
# Mode: Enterprise-Grade, Agent-Optimized
# Build Type: FROM SCRATCH (empty project, no existing codebase)
# License: GNU Affero General Public License v3.0 (AGPL-3.0)
# Rule: ONE phase per session. Quality Gate must pass before proceeding.
# Rule: If ANY requirement is ambiguous, STOP and ask the human operator.
# Rule: No shortcuts, no TODOs, no placeholder implementations, no mock security.

> **VERSION NOTE:** All internal references to "v2.2" or "2.2.0" in the Tech Spec
> and Build Guide refer to the internal development version. The PUBLIC version
> for GitHub, README, CLI, and all user-facing surfaces is **v0.1.0**.
>
> - Wherever code sets a version string, use `"0.1.0"`
> - Wherever docs reference a version, use `v0.1.0`
> - CLI output: `bubblefish nexus v0.1.0 (pre-1.0, API subject to change)`
> - Git tag: `v0.1.0`
> - GitHub Release title: `v0.1.0 — First public release (pre-1.0, API subject to change)`

> **BUILD TYPE NOTE:** This guide builds BubbleFish Nexus from an EMPTY directory.
> There is no existing V2.1 codebase. Every file is CREATED, never MODIFIED.
> The CR (Code Review) hardening requirements from the original Build Guide have
> been merged into the phases that CREATE those files. "Build it right the first
> time" — not "build it wrong then fix it."

---

## Phase Map

| Phase | Title | Primary Persona | Duration |
|-------|-------|-----------------|----------|
| **Foundation** | | | |
| Init | Project Bootstrap | Principal Systems Architect | 1 hr |
| Pre-0 | Repo Hygiene Verification | Principal Systems Architect | 0.5 hr |
| **Core Build** | | | |
| 0A | WAL Engine + Idempotency Store | Senior Backend / Storage Engineer | 2–3 days |
| 0B | Queue + Destination Adapter + SQLite | Senior Backend / Storage Engineer | 2–3 days |
| 0C | Config Loader + HTTP Server + Auth + Handlers | Security Architect + Principal Systems Architect | 3–4 days |
| 0D | Metrics + Doctor + Hot Reload + Shutdown Wiring | Observability Engineer + Principal Systems Architect | 2–3 days |
| 1 | Policy Model + Compilation | Security Architect | 3–4 days |
| 2 | Projection Engine | Search & Retrieval Engineer | 2–3 days |
| 3 | Cascade + Structured Lookup | Search & Retrieval Engineer + Database Specialist | 3–4 days |
| 4 | Exact Cache | Search & Retrieval Engineer | 3–4 days |
| 5 | Embedding + Semantic Retrieval | Search & Retrieval Engineer + Database Specialist | 5–6 days |
| 6 | Semantic Cache + Temporal Decay | Search & Retrieval Engineer | 4–5 days |
| 7 | MCP + OpenAI + SSE | Client Integration Engineer | 4–5 days |
| 8 | Install + Package + Tray | Developer Experience Engineer | 3–4 days |
| 9 | Testing + Security Audit | Site Reliability Engineer + Security Architect | 4–5 days |
| 10 | README + Docs + Demo | Developer Experience Engineer | 2–3 days |
| **V2.2 Refinement** | | | |
| R-1 | WAL HMAC Integrity | Senior Backend / Storage Engineer + Security Architect | 1–2 days |
| R-2 | WAL Encryption | Senior Backend / Storage Engineer + Security Architect | 1–2 days |
| R-3 | Config Signing | Security Architect | 1 day |
| R-4 | Admin vs Data Token Separation | Security Architect | 0.5–1 day |
| R-5 | Provenance Fields | Database / Data Infra Specialist | 1 day |
| R-6 | Retrieval Profiles | Search & Retrieval Engineer | 1–2 days |
| R-7 | Tiered Temporal Decay | Search & Retrieval Engineer | 1–2 days |
| R-8 | Semantic Short-Circuit + Fast Path | Search & Retrieval Engineer | 1 day |
| R-9 | WAL Health Watchdog | Site Reliability Engineer | 1 day |
| R-10 | Consistency Assertions | Site Reliability Engineer | 1 day |
| R-11 | Config Lint | Developer Experience Engineer | 1 day |
| R-12 | TLS/mTLS + Trusted Proxies | Security Architect | 1–2 days |
| R-13 | Deployment Modes | Principal Systems Architect | 0.5 day |
| R-14 | Simple Mode Install | Developer Experience Engineer | 1 day |
| R-15 | Install Profiles | Client Integration Engineer | 1 day |
| R-16 | bubblefish dev | Developer Experience Engineer | 0.5 day |
| R-17 | Structured Security Events | Observability Engineer + Security Architect | 1 day |
| R-18 | Security Metrics | Observability Engineer | 0.5 day |
| R-19 | Event Sink (Webhooks) | Principal Systems Architect | 2–3 days |
| R-20 | OAuth Edge Templates | Security Architect | 1 day |
| R-21 | Pipeline Visualization + Black Box | UX / Frontend Engineer + Observability Engineer | 2 days |
| R-22 | Conflict Inspector + Time-Travel | Database Specialist + UX Engineer | 1–2 days |
| R-23 | Debug Stages | Search & Retrieval Engineer | 0.5–1 day |
| R-24 | Backup and Restore | Senior Backend / Storage Engineer | 1–2 days |
| R-25 | bubblefish bench | Site Reliability Engineer + Search & Retrieval Engineer | 2–3 days |
| R-26 | Reliability Demo | Site Reliability Engineer | 1 day |
| R-27 | Security Tab + Blessed Configs + Ref Archs | Client Integration Engineer + Security Architect | 1–2 days |
| R-28 | Architecture Diagram + Doc Polish | Developer Experience Engineer | 1 day |
| **Ship** | | | |
| Ship | Tag v0.1.0 + Release | Principal Systems Architect | 1 day |

---

## Approved Dependency List

| Dependency | License | Status | Purpose |
|-----------|---------|--------|---------|
| go-chi/chi/v5 | MIT | APPROVED | HTTP router |
| BurntSushi/toml | MIT | APPROVED | TOML parser |
| prometheus/client_golang | Apache 2.0 | APPROVED | Metrics |
| getlantern/systray | Apache 2.0 | APPROVED | System tray (CGO on Linux) |
| mark3labs/mcp-go | MIT | APPROVED | MCP protocol |
| etcd.io/bbolt | MIT | APPROVED | Embedded KV |
| modernc.org/sqlite | BSD | APPROVED | Pure-Go SQLite driver |
| asg017/sqlite-vec | MIT/Apache 2.0 | APPROVED | Vector search for SQLite |
| jackc/pgx/v5 | MIT | APPROVED | PostgreSQL driver |
| charmbracelet/bubbletea | MIT | APPROVED | TUI framework |
| fsnotify/fsnotify | BSD-3 | APPROVED | Filesystem watcher |
| tidwall/gjson | MIT | APPROVED | JSON field extraction |
| hashicorp/golang-lru | MPL 2.0 | **FORBIDDEN** | Replaced with zero-dep LRU |
| valyala/fastjson | MIT | **FORBIDDEN** | Unreliable with AI tools |

---

## GLOBAL DIRECTIVES (apply to EVERY phase — non-negotiable)

### Copyright Header (REQUIRED on every .go file)
Every `.go` file MUST start with this exact copyright header before the `package` declaration:

```go
// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.
```

This header goes ABOVE the `package` line. No exceptions. Test files too.

### Zero-Trust Input Handling
- All HTTP inputs validated and sanitized before processing.
- All SQL queries use parameterized statements. NEVER construct SQL via string concatenation.
- All gjson dot-path mappings operate on validated fields only.
- MaxBytesReader applied BEFORE reading any request body.
- Content-Type validated on POST endpoints.

### Fail-Secure Error Handling
- The system fails closed, not open. On any unexpected error, deny the operation.
- No raw stack traces or internal routing logic exposed to API clients.
- Error responses use the canonical format: `{"error": "code", "message": "human text", "retry_after_seconds": N, "details": {}}`.
- Internal errors logged at ERROR level with full context. Client receives sanitized 500.

### Comprehensive Structured Logging
- Every gateway transition (auth, policy, WAL, queue, destination) is logged with structured fields.
- Required fields on every log entry: time, level, msg, component, source, subject, request_id.
- Security events logged to both the main log and the dedicated security event log when enabled.
- NEVER log secret values (API keys, tokens, encryption keys) at ANY level including DEBUG.

### No Mock Security
- No hardcoded keys, dummy authentication bypasses, or "TODO: add security later" comments.
- If a phase requires auth, it must be fully implemented with constant-time comparison.
- If a phase requires encryption, it must use real cryptographic primitives from Go stdlib (crypto/*).
- Every file and directory must have explicit permissions (0600 for files, 0700 for directories).

### Concurrency Safety
- Every shared mutable state must be protected by explicit synchronization (sync.Mutex, sync.RWMutex, or channel).
- `go test -race` must pass with zero reports on every phase.
- Non-blocking channel operations use `select { case ch <- v: default: }` pattern. NEVER block hot paths.
- sync.Once for one-time initialization (Drain, Stop). NEVER use mutex + bool flag pattern.
- Goroutine lifecycles must be bounded by context.Context and respect shutdown timeouts.

### Dependency Discipline
- Use Go stdlib wherever possible (crypto/*, log/slog, encoding/json, container/list).
- No hashicorp/golang-lru (MPL 2.0 license concern). Zero-dependency LRU with Go generics.
- No valyala/fastjson (unreliable with AI code generation). Use encoding/json + sync.Pool.
- Every external dependency must have an acceptable license (MIT, Apache 2.0, BSD). See Approved Dependency List above.

### Testing Standards
- Every package must have _test.go files.
- Tests must cover: happy path, error path, edge cases, and concurrency.
- Table-driven tests preferred for multiple input scenarios.
- Test helpers must use t.Helper() for clean stack traces.
- No time.Sleep in tests except for shutdown/drain tests with explicit justification.

### WAL Invariant (applies to EVERY phase that touches the write path)
- Every payload is written to the WAL with CRC32 + fsync BEFORE it enters the queue.
- The database is NEVER written to directly — always through the queue.
- Temp files for WAL operations MUST be in filepath.Dir(wal.path), NEVER os.TempDir().
- os.Rename is atomic only on the same filesystem. NEVER rename across filesystems.

### Autonomy Constraints
- If a requirement is ambiguous, STOP and ask the human operator. Flag with `[CLARIFICATION NEEDED]`.
- Do not add features, modes, endpoints, or behaviors not in the Tech Spec or Build Guide.
- No `// TODO: add auth later` comments. No hardcoded bypass keys.
- Every error must be logged, metered, and returned with a machine-readable code. No `_ = err` without justification.
- No package-level `var db *sql.DB` or `var logger *slog.Logger`. All state via struct fields. Exception: version string.
- Each phase produces exactly one commit: `Phase [ID]: [Title]`.
- Write test files first when the behavioral contract is clear.

---

## Spec Cross-Reference Map

| Tech Spec Section | Phases That Reference It |
|-------------------|------------------------|
| Section 1 — Executive Summary | Init |
| Section 2 — Environment and Deployment | R-13, R-14, R-15 |
| Section 3 — Architecture | 0A, 0B, 0C, 0D, 3, 4, 5, 6, R-6, R-7, R-8 |
| Section 4 — WAL Design | 0A, R-1, R-2, R-9, R-24 |
| Section 5 — Queue Design | 0B |
| Section 6 — Security | 0C, 1, R-1, R-2, R-3, R-4, R-12, R-17, R-20 |
| Section 7 — Data Contracts | 2, R-5, R-23 |
| Section 8 — Failure Contracts | 9, R-26 |
| Section 9 — Configuration | 0C, 1, 8, R-11, R-13, R-14, R-15 |
| Section 10 — Event Sink | R-19 |
| Section 11 — Observability | 0D, R-9, R-10, R-17, R-18 |
| Section 12 — HTTP API | 0C, 7, R-22, R-23 |
| Section 13 — Admin UX | 8, R-14, R-16, R-21, R-22, R-25, R-26, R-27 |
| Section 14 — Hot Reload, Shutdown, MCP | 0D, 7, R-24 |
| Section 15 — V2.2 Feature Set | All phases |
| Section 16 — Validation Plan | 9, R-26, Ship |

---

═══════════════════════════════════════════════════════
## PHASE Init: PROJECT BOOTSTRAP
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 1 — Executive Summary
PERSONA: Principal Systems Architect
DURATION: 1 hour

─── OBJECTIVE ───

Create the entire project from nothing: Go module, git repository, AGPL-3.0 license, complete directory tree reflecting all seven system planes, version package, and main entry point. After this phase the project compiles, vets, and has a clean git history.

─── REQUIRED EXPERT MINDSET ───

Principal Systems Architect. You own the overall architecture. The directory tree you create here defines the package boundaries for the entire system. Every package in internal/ maps to a system plane or a well-defined concern. No "utils" or "helpers" packages. No circular dependencies. The tree must include every package that any future phase will need.

─── STRICT END-STATE ───

Files to CREATE:
- cmd/bubblefish/main.go — CLI entry point, prints version
- internal/version/version.go — `const Version = "0.1.0-dev"`
- .gitignore — Comprehensive Go + Nexus-specific ignores
- LICENSE — GNU Affero General Public License v3.0 (AGPL-3.0)
- go.mod — Module github.com/bubblefish-tech/nexus, Go 1.22+

Directories to CREATE (all under internal/):
- config, config/templates, daemon, destination, doctor, hotreload
- idempotency, metrics, queue, version, wal, web
- identity, authz, policy, query, cache, projection
- embedding, mcp, tray, security, events, viz
- consistency, bench, demo, backup, lint

Also: cmd/bubblefish, examples/blessed, examples/oauth, docs

─── BEHAVIORAL CONTRACT ───

1. `go mod init github.com/bubblefish-tech/nexus` produces a valid go.mod. Reference: Tech Spec Section 1.
2. `go build ./...` succeeds with zero errors. Reference: Tech Spec Section 1.
3. `go vet ./...` produces zero findings. Reference: Tech Spec Section 1.
4. Running the binary prints `bubblefish nexus v0.1.0-dev (pre-1.0, API subject to change)`. Reference: Tech Spec Section 1.
5. .gitignore excludes: .env, *.exe, *.log, *.jsonl, compiled/, dist/, vendor/, coverage.*, __debug_bin*, backups/. Reference: Build Guide Init.4.
6. LICENSE file contains the full AGPL-3.0 text.
7. Every .go file has the AGPL-3.0 copyright header above the package declaration.

─── INVARIANTS (must NEVER be violated) ───

1. NEVER: Track .env, loadtest*, or any file containing secrets in git.
2. NEVER: Use Go version higher than 1.22 in go.mod (1.26.1 does not exist).
3. NEVER: Create a "utils" or "helpers" package. Every package has a single, named responsibility.
4. NEVER: Use Apache 2.0 license. This project is AGPL-3.0.

─── SECURITY CHECKPOINT ───

1. .gitignore must exclude .env and all secret-bearing files. Reference: Tech Spec Section 6.5.
2. LICENSE file must be AGPL-3.0. Reference: Project requirement.

─── IMPLEMENTATION DIRECTIVES ───

- USE: `os.MkdirAll` with 0755 for directories
- USE: Separate `internal/version` package with `const Version` (allows ldflags override)
- USE: CLI output format: `fmt.Printf("bubblefish nexus v%s (pre-1.0, API subject to change)\n", version.Version)`
- AVOID: Any third-party dependencies at this phase. Pure stdlib only.
- AVOID: Any business logic. This is scaffolding only.
- LICENSE: Download from `https://www.gnu.org/licenses/agpl-3.0.txt` or create manually.

─── VERIFICATION GATE ───

Compilation:
```
go build ./...
go vet ./...
```

Behavioral Verification:
- [ ] `go run ./cmd/bubblefish/` prints version string with "v0.1.0-dev"
- [ ] All directories in internal/ exist
- [ ] .gitignore exists and contains .env
- [ ] LICENSE file contains "GNU AFFERO GENERAL PUBLIC LICENSE" on first line
- [ ] Every .go file starts with the AGPL-3.0 copyright header

─── COMMIT ───
```
git init
git add -A
git commit -s -m "Phase Init: Project Bootstrap"
```

═══════════════════════════════════════════════════════
## PHASE Pre-0: REPO HYGIENE VERIFICATION
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 1 — Executive Summary
PERSONA: Principal Systems Architect
DURATION: 0.5 hours

─── OBJECTIVE ───

Verify the repository is clean before any feature work. No secrets in git history, go.mod has a valid Go version, .gitignore is comprehensive. This is a gate — no feature work proceeds until this passes.

─── STRICT END-STATE ───

Files to VERIFY:
- .gitignore — Confirm comprehensive
- go.mod — Go version is 1.22
- LICENSE — AGPL-3.0

─── BEHAVIORAL CONTRACT ───

1. `git ls-files` returns zero results matching `.env` or `loadtest*`. Reference: Tech Spec Section 6.5.
2. go.mod specifies `go 1.22`. Reference: Build Guide Pre-0.2.
3. `go build ./...` succeeds. Reference: Build Guide Pre-0.2.
4. `go mod tidy` produces no changes. Reference: Build Guide Pre-0.2.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
```

- [ ] `git ls-files | Select-String '\.env'` returns zero results
- [ ] go.mod contains `go 1.22`
- [ ] `go mod tidy` produces no diff
- [ ] LICENSE contains AGPL-3.0

─── COMMIT ───
```
git add -A
git commit -s -m "Phase Pre-0: Repo Hygiene Verification"
```

═══════════════════════════════════════════════════════
## PHASE 0A: WAL ENGINE + IDEMPOTENCY STORE
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 4 — WAL Design
PERSONA: Senior Backend / Storage Engineer
DURATION: 2–3 days

─── OBJECTIVE ───

Create the Write-Ahead Log engine from scratch with all V2.2 hardening built in from the start: 10MB scanner buffer, CRC32 checksums on every entry, atomic MarkDelivered via temp-file-on-same-filesystem, WAL segment rotation with crash-safe dual-segment replay, and idempotency store rebuilt exclusively from WAL replay on startup. This is the most crash-sensitive code in the entire system.

─── REQUIRED EXPERT MINDSET ───

Senior Backend / Storage Engineer. You are building the durability layer that the entire system's crash-safety guarantee depends on. Every file operation must be atomic or idempotent. The question you ask about every line of code: "What happens if power fails between this line and the next?" CRC32 is computed on every write. The scanner buffer is 10MB from day one. Temp files are always in filepath.Dir(wal.path). fsync is never skipped. The idempotency store is empty on restart and rebuilt exclusively from WAL replay.

─── STRICT END-STATE ───

Files to CREATE:
- internal/wal/wal.go — WAL engine: Append (with CRC32 + fsync), Replay, segment discovery, 10MB scanner
- internal/wal/updater.go — WALUpdater interface, MarkDelivered (atomic rename, same FS, CRC32 recompute)
- internal/wal/wal_test.go — CRC32 validation, corrupt entry handling, replay, idempotency rebuild, crash-mid-rotation, 10MB entry test
- internal/idempotency/store.go — In-memory dedup map with Register/Seen/PayloadID methods
- internal/idempotency/store_test.go — Registration, duplicate detection, rebuild from WAL

Packages/Interfaces to export:
- `wal.WAL` struct with Append, Replay, Close methods
- `wal.WALUpdater` interface with MarkDelivered(payloadID string) error
- `idempotency.Store` struct with Register, Seen, PayloadID methods

─── BEHAVIORAL CONTRACT ───

1. WAL entry format: `JSON_BYTES<TAB>CRC32_HEX<NEWLINE>`. CRC32 computed over JSON bytes using `crc32.ChecksumIEEE`. Reference: Tech Spec Section 4.1.
2. WAL scanner uses 10MB buffer: `scanner.Buffer(make([]byte, 10<<20), 10<<20)`. Reference: Tech Spec Section 4.1.
3. On replay, CRC32 is recomputed. Mismatches: entry skipped, WARN log, `bubblefish_wal_crc_failures_total` incremented. Reference: Tech Spec Section 4.1.
4. Partial lines (crash-mid-write, no newline) are silently skipped. Reference: Tech Spec Section 4.5.
5. DELIVERED and PERMANENT_FAILURE entries are skipped on replay. Only PENDING entries re-enqueued. Reference: Tech Spec Section 4.5.
6. In-memory idempotency maps are empty on restart. Rebuilt exclusively from WAL replay — PENDING entries register their idempotency keys. Reference: Tech Spec Section 4.5.
7. WAL segments discovered via filepath.Glob, sorted oldest-first for deterministic replay. Reference: Tech Spec Section 4.5.
8. MarkDelivered: read segment, rewrite entry with status=DELIVERED + new CRC32, write to temp file in filepath.Dir(wal.path), fsync, os.Rename. Reference: Tech Spec Section 4.3.
9. WAL rotation crash safety: if both old and new segments exist on restart (crash during rotation), both are replayed. Entries deduplicated by idempotency key. Reference: Tech Spec Section 4.2.
10. WAL segments rotate when current segment exceeds max_segment_size_mb (default 50MB). Reference: Tech Spec Section 4.2.
11. WAL Append: write JSON + CRC32 to current segment, fsync. On failure: return error (caller returns 500). Reference: Tech Spec Section 4.1.
12. WAL files: 0600 permissions. WAL directory: 0700. Reference: Tech Spec Section 6.5.
13. MarkDelivered failure after successful destination write: log WARN (not ERROR). Data is safe — destination idempotency prevents duplicate on replay. Reference: Tech Spec Section 4.5.

─── INVARIANTS (must NEVER be violated) ───

1. NEVER: Write WAL entries without computing and appending CRC32.
2. NEVER: Skip fsync after WAL append or before MarkDelivered rename.
3. NEVER: Write temp files to os.TempDir(). ALWAYS filepath.Dir(wal.path).
4. NEVER: os.Rename across filesystems (fails with EXDEV).
5. NEVER: Modify WAL segment in-place. Always write-new-then-rename.
6. NEVER: Trust stale in-memory idempotency state across restarts. Rebuild from WAL only.
7. NEVER: Process a WAL entry with a CRC32 mismatch.
8. NEVER: Use a scanner buffer smaller than 10MB.

─── SECURITY CHECKPOINT ───

1. WAL files created with 0600 permissions. WAL directory 0700. Reference: Tech Spec Section 6.5.
2. CRC32 provides corruption detection, not tamper resistance. HMAC added in Phase R-1. Reference: Tech Spec Section 4.1.

─── IMPLEMENTATION DIRECTIVES ───

- USE: `hash/crc32` with `crc32.ChecksumIEEE(jsonBytes)` for checksum
- USE: `fmt.Sprintf("%08x", checksum)` for hex encoding
- USE: `bufio.NewScanner` with `scanner.Buffer(make([]byte, 10<<20), 10<<20)`
- USE: `strings.SplitN(line, "\t", 2)` to split JSON from CRC32
- USE: `os.CreateTemp(filepath.Dir(wal.path), "wal-*.tmp")` for temp files
- USE: `f.Sync()` before `os.Rename(tmpPath, segmentPath)`
- USE: `filepath.Glob(filepath.Join(walDir, "wal-*.jsonl"))` to discover segments
- USE: `sort.Strings(segments)` for deterministic replay order
- USE: `log/slog` for all logging. Logger passed via struct field.
- AVOID: `bufio.ScanLines` with default buffer (64KB — too small)
- AVOID: In-place file modification
- AVOID: Any global state — WAL struct holds all state
- EDGE CASE: WAL file ends without newline (crash mid-write). Scanner returns partial line. Check for tab separator and valid CRC — if not, skip silently.
- EDGE CASE: Empty WAL file. Replay produces zero entries. No error.
- EDGE CASE: WAL entry with valid CRC but malformed JSON. Log WARN, skip entry.
- EDGE CASE: Two segments exist after crash-during-rotation. Both must be replayed, entries deduplicated.

─── VERIFICATION GATE ───

Compilation:
```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

Behavioral Verification:
- [ ] Write 10 entries. All have CRC32 appended after tab.
- [ ] Corrupt one entry (flip a byte). Replay skips it with WARN. Other 9 replayed.
- [ ] Write 10, MarkDelivered on all 10. Restart. Zero entries replayed (all DELIVERED).
- [ ] Write 10, MarkDelivered on 5. Restart. Exactly 5 entries replayed.
- [ ] Crash mid-write (truncate last line without newline). Replay skips partial, processes complete entries.
- [ ] Entry larger than 64KB writes and replays correctly (tests 10MB buffer).
- [ ] Crash mid-rotation (two segments exist). Restart replays both, zero duplicates.
- [ ] MarkDelivered temp file is in same directory as WAL segment (verify path).
- [ ] MarkDelivered failure: logged as WARN (not ERROR or panic).
- [ ] Idempotency rebuilt from WAL: duplicate write returns 200 with original payload_id.
- [ ] WAL files created with 0600 permissions.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 0A: WAL Engine + Idempotency Store"
```

═══════════════════════════════════════════════════════
## PHASE 0B: QUEUE + DESTINATION ADAPTER + SQLITE
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 5 — Queue Design, Section 3 — Architecture
PERSONA: Senior Backend / Storage Engineer
DURATION: 2–3 days

─── OBJECTIVE ───

Create the message queue with all V2.2 hardening built in (non-blocking channel send, sync.Once drain, DrainWithContext, slog.Logger injection), the destination adapter interface, and the SQLite destination implementation. After this phase, payloads can flow from WAL → Queue → SQLite.

─── REQUIRED EXPERT MINDSET ───

Senior Backend / Storage Engineer. The queue is where concurrency bugs hide. Every channel operation is either non-blocking or bounded by context. sync.Once prevents double-close panics. The destination adapter interface must be clean enough that Postgres and Supabase can be added later without changing the queue. The SQLite adapter must use PRAGMA journal_mode=WAL and busy_timeout=5000 from day one.

─── STRICT END-STATE ───

Files to CREATE:
- internal/queue/queue.go — Non-blocking enqueue, sync.Once drain, DrainWithContext, worker loop
- internal/queue/queue_test.go — Concurrent enqueue, double-drain, timeout, load shed
- internal/destination/adapter.go — DestinationWriter interface (Write, Close, Ping, Exists)
- internal/destination/sqlite.go — SQLite adapter with PRAGMA WAL, busy_timeout, parameterized queries
- internal/destination/sqlite_test.go — Write, read, Ping, schema creation

Packages/Interfaces to export:
- `queue.Queue` struct with Enqueue, Drain, DrainWithContext methods
- `destination.DestinationWriter` interface
- `destination.SQLiteDestination` struct implementing DestinationWriter

─── BEHAVIORAL CONTRACT ───

1. Enqueue() uses `select { case q.ch <- payload: return nil; default: return ErrLoadShed }`. No mutex. Reference: Tech Spec Section 5.
2. Drain() wraps `close(q.done)` in sync.Once. Safe for multiple calls. Never panics. Reference: Tech Spec Section 5.
3. DrainWithContext respects context.Context for timeout. Returns bool. Reference: Tech Spec Section 5.
4. Worker receives slog.Logger via Queue struct field. Logger is never nil. Reference: Tech Spec Section 5.
5. Worker calls MarkDelivered after successful destination write. On failure: classify TRANSIENT (backoff retry) or PERMANENT (mark WAL, log ERROR). Reference: Tech Spec Section 5.
6. Queue full: Enqueue returns ErrLoadShed. HTTP handler translates to 429 queue_full with Retry-After. WAL entry still durable. Reference: Tech Spec Section 8.1.
7. Queue size configurable via config (default 10000). Reference: Tech Spec Section 5.
8. SQLite opened with `PRAGMA journal_mode=WAL` and `PRAGMA busy_timeout=5000`. Reference: Build Guide CR-9.
9. All SQL queries use parameterized statements. NEVER string concatenation. Reference: Tech Spec Section 6.
10. SQLite adapter implements Ping() (verify DB accessible) and Close() (close connection). Reference: Tech Spec Section 13.1.
11. SQLite adapter implements Exists(payloadID) for consistency assertions (Phase R-10). Reference: Tech Spec Section 11.5.
12. DestinationWriter.Write accepts a TranslatedPayload and returns error. Reference: Tech Spec Section 3.2.
13. io.LimitReader on any external response body (50MB bound). Reference: Build Guide CR-8.

─── INVARIANTS ───

1. NEVER: Use a mutex to protect channel send. Channel operations are goroutine-safe.
2. NEVER: Call close() on a channel without sync.Once protection.
3. NEVER: Block indefinitely in Enqueue. Must return immediately if channel full.
4. NEVER: Allow Worker to have a nil logger. Panic at construction time if nil.
5. NEVER: Construct SQL via string concatenation. Always parameterized.
6. NEVER: Open SQLite without PRAGMA journal_mode=WAL.

─── IMPLEMENTATION DIRECTIVES ───

- USE: `select { case q.ch <- p: return nil; default: return ErrLoadShed }` for Enqueue
- USE: `sync.Once` field on Queue struct for Drain
- USE: `context.Context` parameter on DrainWithContext
- USE: `modernc.org/sqlite` for pure-Go SQLite (no CGO requirement for production)
- USE: `database/sql` with parameterized queries (`?` placeholders)
- AVOID: `mattn/go-sqlite3` for production (CGO dependency). Use for `-race` tests if needed.
- EDGE CASE: DrainWithContext timeout — document goroutine lifecycle in code comment.
- EDGE CASE: Calling Drain() after DrainWithContext — must be safe (sync.Once).

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] 100 goroutines calling Enqueue concurrently: zero panics, zero race reports
- [ ] Drain() called 3 times: zero panics
- [ ] DrainWithContext with 1s timeout on busy queue: returns within 2s
- [ ] Enqueue on full channel: returns ErrLoadShed immediately (< 1ms)
- [ ] SQLite write + read round-trip: data matches
- [ ] SQLite PRAGMA journal_mode query returns "wal"
- [ ] Nil logger at Queue construction: panics with clear message

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 0B: Queue + Destination Adapter + SQLite"
```

═══════════════════════════════════════════════════════
## PHASE 0C: CONFIG LOADER + HTTP SERVER + AUTH + HANDLERS
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 6 — Security, Section 9 — Configuration, Section 12 — HTTP API
PERSONA: Security Architect + Principal Systems Architect
DURATION: 3–4 days

─── OBJECTIVE ───

Create the config loader (daemon.toml + source/destination TOML parsing), the HTTP server with all four timeouts and chi router, authentication middleware with constant-time comparison and cached resolved keys, and the write/read handlers with correct operation order. After this phase, the daemon can start, accept HTTP requests, authenticate them, write to WAL → Queue → SQLite, and serve basic reads.

─── REQUIRED EXPERT MINDSET ───

Security Architect + Principal Systems Architect. The HTTP layer is the primary trust boundary. Every timeout must be set (slowloris prevention). Token comparison is constant-time (timing attack prevention). Keys are resolved at startup from env:/file:/literal and cached — zero os.Getenv on the hot path. The write path operation order is a correctness requirement: idempotency before rate limiting, MaxBytesReader before body read. The config loader validates at startup — empty keys, unknown destinations, and duplicate resolved keys are build failures, not runtime surprises.

─── STRICT END-STATE ───

Files to CREATE:
- internal/config/types.go — Config structs for daemon.toml, source TOML, destination TOML
- internal/config/loader.go — Load daemon.toml, discover source/destination TOML files, validate
- internal/config/resolve.go — ResolveEnv for env:, file:, literal secret references
- internal/config/loader_test.go — Valid config, missing fields, env:/file: resolution
- internal/daemon/daemon.go — Daemon struct, New(), Start(), Stop(), main lifecycle
- internal/daemon/server.go — HTTP server with chi, all 4 timeouts, route registration
- internal/daemon/auth.go — Auth middleware: constant-time, cached keys, CanRead/CanWrite
- internal/daemon/handlers.go — Write handler (/inbound/{source}), read handler (/query/{dest}), health/ready
- internal/daemon/auth_test.go — Timing test, CanRead/CanWrite, wrong token class
- internal/daemon/handlers_test.go — Write path operation order, read path, error format

Packages/Interfaces to export:
- `config.Config` struct (loaded daemon config)
- `config.ResolveEnv(ref string) (string, error)` function
- `daemon.Daemon` struct with Start/Stop

─── BEHAVIORAL CONTRACT ───

1. config.ResolveEnv supports: `env:VAR_NAME`, `file:/path/to/secret`, plain literal. Reference: Tech Spec Section 6.1.
2. file: reads os.ReadFile, trims whitespace. Logs path at DEBUG. NEVER logs value. Reference: Tech Spec Section 6.1.
3. Empty resolved API key fails build (SCHEMA_ERROR at startup). Reference: Tech Spec Section 6.1.
4. Duplicate resolved API keys across sources fails build. Env vars resolved BEFORE comparison. Reference: Tech Spec Section 6.1.
5. ALL token comparisons use `subtle.ConstantTimeCompare`. Reference: Tech Spec Section 6.1.
6. All resolved keys cached at startup in map. Zero os.Getenv on auth hot path. Reference: Tech Spec Section 6.1.
7. http.Server configured with: ReadHeaderTimeout=10s, ReadTimeout=30s, WriteTimeout=60s, IdleTimeout=120s. Reference: Tech Spec Section 6.6.
8. Write path operation order (exact sequence): auth → CanWrite → subject → policy gate → MaxBytesReader → idempotency → rate limit → field mapping → transforms → WAL → idempotency register → queue → return 200. Reference: Tech Spec Section 3.2.
9. Idempotency check BEFORE rate limiting. Duplicates don't burn rate budget. Reference: Tech Spec Section 3.2 Steps 7-8.
10. MaxBytesReader applied BEFORE reading body. 413 payload_too_large if over limit. Reference: Tech Spec Section 3.2 Step 6.
11. Rate limiting on BOTH read and write paths. 429 + Retry-After header. Reference: Tech Spec Section 6.6.
12. CanRead = false → 403 source_not_permitted_to_read. CanWrite = false → 403 source_not_permitted_to_write. Reference: Tech Spec Section 6.1.
13. /health: 200, no auth. /ready: 200 (or 503 if unhealthy), no auth. Reference: Tech Spec Section 12.
14. Error format: `{"error":"code","message":"text","retry_after_seconds":N,"details":{}}`. Reference: Tech Spec Section 7.4.
15. chi.URLParam(r, "source") for path parameter extraction. Reference: Build Guide CR-9.
16. Query limit: default 20, max 200. Values > 200 capped at 200. Reference: Tech Spec Section 3.8.
17. os.UserHomeDir() failure is fatal at startup. Reference: Build Guide CR-8.
18. writeJSON is a method on the daemon struct, not a package-level function. Reference: Build Guide CR-8.
19. All logging uses log/slog with structured fields. No fmt.Println for operational messages. Reference: Tech Spec Section 11.1.
20. Every Write() return value checked. No `_ = w.Write(...)`. Reference: Build Guide CR-10.
21. Config directory: ~/.bubblefish/Nexus/ (0700). Config files: 0600. Reference: Tech Spec Section 9.1.

─── INVARIANTS ───

1. NEVER: Use `==` for token comparison. Always `subtle.ConstantTimeCompare`.
2. NEVER: Call `os.Getenv` or `os.ReadFile` on the auth hot path. All resolved at startup.
3. NEVER: Log API key values at any level. Log only the source name.
4. NEVER: Check rate limit before idempotency.
5. NEVER: Read request body before applying MaxBytesReader.
6. NEVER: Omit any of the 4 HTTP server timeouts.
7. NEVER: Return 429 without a Retry-After header.
8. NEVER: Expose raw stack traces or internal routing to API clients.
9. NEVER: Allow a source with an empty resolved API key to pass build validation.

─── SECURITY CHECKPOINT ───

1. Timing test: 1000 samples with wrong key vs correct key. p99 difference < 1ms. Reference: Tech Spec Section 16.
2. file: reference: path logged at DEBUG, value never logged. Reference: Tech Spec Section 6.1.
3. Error responses never include internal details. Reference: Tech Spec Section 7.4.
4. All config files written with 0600 permissions. Reference: Tech Spec Section 9.1.

─── IMPLEMENTATION DIRECTIVES ───

- USE: `crypto/subtle.ConstantTimeCompare([]byte(provided), []byte(expected))` — returns 1 if equal
- USE: `map[string][]byte` for resolved keys (pre-convert to bytes at startup)
- USE: `go-chi/chi/v5` router with `chi.URLParam(r, "source")`
- USE: `BurntSushi/toml` for config parsing
- USE: `http.MaxBytesReader(w, r.Body, maxBytes)` at step 6 of write path
- USE: Named constants for timeout values, not magic numbers
- AVOID: String comparison with `==` for any security-sensitive value
- AVOID: Reading r.Body before wrapping with MaxBytesReader
- AVOID: Global variables for config, logger, or server
- EDGE CASE: Source with api_key = "env:NONEXISTENT" — os.Getenv returns "". Build must fail.
- EDGE CASE: Source with api_key = "file:/nonexistent" — os.ReadFile fails. Build must fail.
- EDGE CASE: Client sends exactly max_bytes. Should succeed (not off-by-one).
- EDGE CASE: Client sends max_bytes + 1. Returns 413 before any processing.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Daemon starts, responds on configured port
- [ ] Correct key: 200. Wrong key: 401. Timing difference p99 < 1ms.
- [ ] CanWrite=false: POST returns 403 source_not_permitted_to_write
- [ ] CanRead=false: GET returns 403 source_not_permitted_to_read
- [ ] Empty resolved key: startup fails with SCHEMA_ERROR
- [ ] file: reference resolves correctly, path logged at DEBUG, value not logged
- [ ] Duplicate write with same idempotency key: second returns 200 with original payload_id, rate counter NOT incremented
- [ ] MaxBytesReader: payload at max succeeds, max+1 returns 413
- [ ] /health returns 200 with no auth. /ready returns 200.
- [ ] Query with limit=500: capped to 200 results

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 0C: Config Loader + HTTP Server + Auth + Handlers"
```

═══════════════════════════════════════════════════════
## PHASE 0D: METRICS + DOCTOR + HOT RELOAD + SHUTDOWN WIRING
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 11 — Observability, Section 14 — Hot Reload and Shutdown
PERSONA: Observability / Telemetry Engineer + Principal Systems Architect
DURATION: 2–3 days

─── OBJECTIVE ───

Create the Prometheus metrics system (private registry, all initial counters/gauges/histograms), the doctor command, the hot reload watcher (source-only, configMu RWMutex), and the 3-stage budgeted shutdown sequence. After this phase, the daemon is a fully operational single-binary with write path, read path, metrics, health checks, config reload, and clean shutdown.

─── REQUIRED EXPERT MINDSET ───

Observability / Telemetry Engineer + Principal Systems Architect. Metrics use a private Prometheus registry (no global registry panics in tests). Every metric registered must be incremented somewhere — permanently-zero metrics are bugs. Hot reload uses configMu as RWMutex: auth reads use RLock (many concurrent), reload uses Lock (exclusive). Destination changes are NEVER applied via hot reload — log WARN, keep old config. Shutdown has a 3-stage budget (HTTP shutdown, queue drain, WAL close) and each stage has a timeout.

─── STRICT END-STATE ───

Files to CREATE:
- internal/metrics/metrics.go — Private Prometheus registry, all initial metrics
- internal/metrics/metrics_test.go — Verify non-zero after exercise, no global registry panics
- internal/doctor/doctor.go — Health checks: daemon running, destinations reachable, WAL dir writable, disk space
- internal/doctor/doctor_test.go — Healthy setup passes, unwritable WAL fails
- internal/hotreload/watcher.go — fsnotify on sources/ dir, configMu RWMutex, stopReload channel
- internal/hotreload/watcher_test.go — Concurrent reload + auth, destination change rejected

Files to MODIFY:
- internal/daemon/daemon.go — Wire metrics, doctor, hot reload, shutdown stages
- internal/daemon/server.go — Wire /metrics endpoint from private registry

─── BEHAVIORAL CONTRACT ───

1. All metrics use private `prometheus.Registry`. No `prometheus.DefaultRegisterer`. Reference: Tech Spec Section 11.3.
2. All metric names follow `bubblefish_` prefix convention. Reference: Tech Spec Section 11.3.
3. After exercising write + read paths, all throughput/latency/queue metrics have non-zero values. Reference: Tech Spec Section 11.3.
4. /metrics endpoint serves from private registry with admin auth. Reference: Tech Spec Section 12.
5. configMu is sync.RWMutex. Auth hot path uses RLock(). Reload uses Lock(). Reference: Tech Spec Section 14.1.
6. Source config changes applied atomically. In-flight handlers complete with old config. Reference: Tech Spec Section 14.1.
7. Destination config changes: WARN logged. Old config active. Restart required. Reference: Tech Spec Section 14.1.
8. Compiled JSON written atomically: temp file + fsync + os.Rename. Reference: Tech Spec Section 14.1.
9. Shutdown Stage 1 (10s): HTTP server graceful shutdown. Stage 2 (10s): Queue drain via DrainWithContext. Stage 3 (10s): WAL close + cleanup. Reference: Tech Spec Section 14.2.
10. watchLoop goroutine exits cleanly when stopReload channel is closed. Reference: Tech Spec Section 14.1.
11. Doctor checks: daemon running, destinations reachable (Ping + Close), WAL dir writable (probe file write + delete), disk space. Reference: Tech Spec Section 13.1.
12. DB connections closed after Ping() in doctor (not leaked). Reference: Build Guide CR-7.
13. WAL directory probe: write small file, delete it. Report failure if either fails. Reference: Build Guide CR-7.

─── INVARIANTS ───

1. NEVER: Use global prometheus.DefaultRegisterer. Always private registry.
2. NEVER: Use Lock() on the auth hot path. Only RLock().
3. NEVER: Apply destination config changes via hot reload.
4. NEVER: Write compiled JSON non-atomically.
5. NEVER: Let shutdown exceed drain_timeout_seconds total.
6. NEVER: Register a metric that is never incremented/observed.
7. NEVER: Leak DB connections in doctor (close after Ping).

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Multiple daemon instances in tests: zero registry panics
- [ ] After 10 writes + 5 reads: all throughput/latency/queue metrics non-zero
- [ ] /metrics endpoint returns valid Prometheus text format
- [ ] Concurrent reload + 100 auth requests: zero race reports
- [ ] Destination config change during reload: WARN logged, old config active
- [ ] SIGTERM during active writes: exit within drain_timeout_seconds
- [ ] watchLoop goroutine not leaked after shutdown
- [ ] Doctor with healthy setup: all checks pass
- [ ] Doctor with unwritable WAL dir: reports failure

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 0D: Metrics + Doctor + Hot Reload + Shutdown Wiring"
```

═══════════════════════════════════════════════════════
## PHASE 1: POLICY MODEL + COMPILATION
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 6 — Security, Section 9 — Configuration
PERSONA: Security Architect
DURATION: 3–4 days

─── OBJECTIVE ───

Create the [source.policy] TOML block with all V2.2 fields, compile policies to JSON artifacts, and enforce build-time validation (empty keys, unknown destinations, duplicate resolved keys). After this phase, `bubblefish build` produces compiled/policies.json.

─── STRICT END-STATE ───

Files to CREATE:
- internal/policy/types.go — PolicyConfig struct matching all [source.policy] TOML fields
- internal/policy/compile.go — Compile policies to compiled/policies.json
- internal/policy/validate.go — Validate rules (allowed_destinations must exist)
- internal/config/build.go — `bubblefish build` command logic
- internal/policy/policy_test.go — Valid policy, unknown dest, field visibility

─── BEHAVIORAL CONTRACT ───

1. PolicyConfig struct matches all fields from Tech Spec Section 9.3: allowed_destinations, allowed_operations, allowed_retrieval_modes, allowed_profiles, max_results, max_response_bytes, field_visibility, cache, decay overrides. Reference: Tech Spec Section 9.3.
2. Build fails on: empty resolved api_key, unknown destination reference, duplicate resolved keys. Reference: Tech Spec Section 6.1.
3. Env vars resolved BEFORE duplicate key check. Reference: Tech Spec Section 6.1.
4. compiled/ directory: 0700. Files inside: 0600. Reference: Tech Spec Section 9.1.
5. `bubblefish build` produces compiled/policies.json with correct permissions. Reference: Tech Spec Section 9.1.

─── INVARIANTS ───

1. NEVER: Log resolved secret values. Only log the reference format.
2. NEVER: Allow a source to reference a nonexistent destination.
3. NEVER: Allow an empty resolved API key to pass validation.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] `bubblefish build` produces compiled/policies.json with 0600 permissions
- [ ] Unknown destination reference: build fails with clear error
- [ ] Duplicate resolved keys: build fails with "duplicate key" error
- [ ] file: reference resolves correctly

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 1: Policy Model + Compilation"
```

═══════════════════════════════════════════════════════
## PHASE 2: PROJECTION ENGINE
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 7 — Data Contracts
PERSONA: Search & Retrieval Engineer
DURATION: 2–3 days

─── OBJECTIVE ───

Create the projection engine: field visibility allowlists, byte budget truncation on word boundaries, _nexus metadata injection, and metadata stripping when policy.strip_metadata = true.

─── STRICT END-STATE ───

Files to CREATE:
- internal/projection/project.go — Apply field allowlist from policy
- internal/projection/truncate.go — Byte budget truncation on word boundary
- internal/projection/budget.go — Calculate response size vs max_response_bytes
- internal/projection/projection_test.go — Allowlist, truncation, _nexus metadata, stripping
- internal/projection/metadata.go — _nexus metadata struct and injection

─── BEHAVIORAL CONTRACT ───

1. Field allowlist from policy.field_visibility.include_fields applied. Only listed fields survive. Reference: Tech Spec Section 9.3.
2. Byte budget truncation on word boundaries. Reference: Tech Spec Section 9.3.
3. _nexus metadata: stage, semantic_unavailable, result_count, truncated, next_cursor, has_more, temporal_decay_applied, profile, consistency_score. Reference: Tech Spec Section 7.2.
4. _nexus stripped when policy.strip_metadata = true. Reference: Tech Spec Section 9.3.
5. Error format: `{"error":"code","message":"text","retry_after_seconds":N,"details":{}}`. Reference: Tech Spec Section 7.4.
6. Unmapped gjson fields populate the TranslatedPayload.Metadata map. Reference: Build Guide CR-10.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Response with field allowlist: only listed fields present
- [ ] Response exceeding byte budget: truncated on word boundary, _nexus.truncated = true
- [ ] strip_metadata = true: _nexus block absent
- [ ] strip_metadata = false: _nexus block present with all fields

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 2: Projection Engine"
```

═══════════════════════════════════════════════════════
## PHASE 3: CASCADE + STRUCTURED LOOKUP
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 3.4 — 6-Stage Retrieval Cascade
PERSONA: Search & Retrieval Engineer + Database / Data Infra Specialist
DURATION: 3–4 days

─── OBJECTIVE ───

Create the 6-stage cascade framework with Stages 0 (policy gate) and 3 (structured lookup) fully operational, plus cursor-based pagination. Stages 1, 2, 4, 5 are stub pass-throughs implemented in later phases.

─── STRICT END-STATE ───

Files to CREATE:
- internal/query/cascade.go — 6-stage cascade framework
- internal/query/normalize.go — CanonicalQuery normalization
- internal/query/structured.go — Stage 3 parameterized WHERE clauses
- internal/query/cascade_test.go — Stage execution order, policy gate, pagination

─── BEHAVIORAL CONTRACT ───

1. Cascade stages 0→5 execute in order. Each stage can return results, pass through, or short-circuit. Reference: Tech Spec Section 3.4.
2. Stage 0 (Policy Gate): checks CanRead, allowed_operations, allowed_destinations, allowed_retrieval_modes. Returns 403 with specific denial reason. Reference: Tech Spec Section 3.4.
3. Stage 3 (Structured Lookup): parameterized WHERE clauses. NO SQL string concatenation. Reference: Tech Spec Section 3.4.
4. Cursor-based pagination: ?limit=20 (default), max 200. Cursors are opaque base64 strings stable across writes. Reference: Tech Spec Section 3.8.
5. Query limit: default 20, max 200. Values > 200 silently capped. Reference: Tech Spec Section 3.8.

─── INVARIANTS ───

1. NEVER: Construct SQL via string concatenation.
2. NEVER: Allow query limit > 200.
3. NEVER: Execute stages out of order.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Policy gate rejects CanRead=false source with 403
- [ ] Structured lookup returns correct results with parameterized WHERE
- [ ] Cursor pagination: page 1 returns next_cursor, page 2 with cursor returns next set
- [ ] limit=500: capped to 200 results

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 3: Cascade + Structured Lookup"
```

═══════════════════════════════════════════════════════
## PHASE 4: EXACT CACHE (STAGE 1)
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 3.4 — Stage 1
PERSONA: Search & Retrieval Engineer
DURATION: 3–4 days

─── OBJECTIVE ───

Create a zero-dependency generic LRU cache with scope isolation (source A cannot see source B's cached entries), watermark-based invalidation, and Prometheus hit/miss counters.

─── STRICT END-STATE ───

Files to CREATE:
- internal/cache/lru.go — Generic LRU: map + container/list, ~50 lines, thread-safe
- internal/cache/exact.go — Stage 1 exact cache with scope isolation
- internal/cache/watermark.go — Monotonic counter per destination
- internal/cache/stats.go — Hit/miss Prometheus counters
- internal/cache/cache_test.go — Scope isolation, watermark invalidation, LRU eviction, concurrency

─── BEHAVIORAL CONTRACT ───

1. LRU: Go 1.18+ generics, map + container/list. Thread-safe via sync.Mutex. Reference: Tech Spec Section 3.4.
2. CRITICAL: Do NOT use hashicorp/golang-lru (MPL 2.0). Reference: Approved Dependency List.
3. Cache key: SHA256(scope_hash + destination + query_params + policy_hash). Reference: Tech Spec Section 3.4.
4. Scope isolation: scope_hash includes source identity. Source A invisible to Source B. Reference: Tech Spec Section 3.4.
5. Watermark invalidation: entries with watermark < current are stale. Reference: Tech Spec Section 3.4.
6. LRU capped at configurable max size (default 50MB). Reference: Build Guide Phase 4.
7. Metrics: bubblefish_cache_exact_hits_total, bubblefish_cache_exact_misses_total. Reference: Tech Spec Section 11.3.

─── INVARIANTS ───

1. NEVER: Use hashicorp/golang-lru or any MPL-licensed cache library.
2. NEVER: Allow source A to retrieve source B's cached entries.
3. NEVER: Serve a stale cache entry (watermark check).

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Scope isolation: source A writes, source B queries same key: miss
- [ ] Watermark invalidation: write advances watermark, old cache entry invalidated
- [ ] LRU eviction: fill to limit, add one more: oldest evicted
- [ ] 100 goroutines: zero race reports

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 4: Exact Cache"
```

═══════════════════════════════════════════════════════
## PHASE 5: EMBEDDING + SEMANTIC RETRIEVAL
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 3.4 — Stage 4, Section 14.4
PERSONA: Search & Retrieval Engineer + Database / Data Infra Specialist
DURATION: 5–6 days

─── OBJECTIVE ───

Create the embedding client factory (OpenAI, Ollama, compatible), wire Stage 4 semantic retrieval with sqlite-vec and pgvector, and implement graceful degradation when embedding is not configured.

─── STRICT END-STATE ───

Files to CREATE:
- internal/embedding/client.go — EmbeddingClient interface
- internal/embedding/openai.go — OpenAI-compatible client
- internal/embedding/ollama.go — Ollama client (localhost:11434)
- internal/embedding/factory.go — Create client from daemon.toml config
- internal/embedding/embedding_test.go — Mock provider, graceful degradation
- internal/destination/postgres.go — PostgreSQL adapter with pgvector
- internal/destination/supabase.go — Supabase/OpenBrain adapter

─── BEHAVIORAL CONTRACT ───

1. EmbeddingClient interface: Embed(ctx, text) ([]float32, error). Reference: Tech Spec Section 14.4.
2. Factory creates client from config: provider = "openai" | "ollama" | "compatible". Reference: Tech Spec Section 9.2.8.
3. Stage 4: sqlite-vec (SQLite), pgvector (Postgres), or Supabase RPC. Reference: Tech Spec Section 3.4.
4. Graceful degradation: no embedding → Stages 2+4 skipped, _nexus.semantic_unavailable = true with reason. Reference: Tech Spec Section 3.4.
5. Embedding timeout: configurable (default 10s). Timeout = graceful degradation for that request. Reference: Tech Spec Section 9.2.8.

─── INVARIANTS ───

1. NEVER: Crash when embedding provider is unreachable. Gracefully degrade.
2. NEVER: Log embedding API keys.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Embedding disabled: _nexus.semantic_unavailable = true
- [ ] Provider unreachable: same graceful degradation
- [ ] sqlite-vec vector search returns results ranked by cosine similarity

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 5: Embedding + Semantic Retrieval"
```

═══════════════════════════════════════════════════════
## PHASE 6: SEMANTIC CACHE + HYBRID MERGE + TEMPORAL DECAY
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 3.4 — Stages 2 + 5, Section 3.6
PERSONA: Search & Retrieval Engineer
DURATION: 4–5 days

─── OBJECTIVE ───

Complete the full 6-stage cascade: Stage 2 (semantic cache), Stage 5 (hybrid merge with deduplication and temporal decay reranking). All six stages now operational.

─── STRICT END-STATE ───

Files to CREATE:
- internal/cache/semantic.go — Stage 2 semantic cache
- internal/query/hybrid.go — Stage 5 dedup + merge
- internal/query/decay.go — Temporal decay reranking

─── BEHAVIORAL CONTRACT ───

1. Stage 2: cosine similarity >= threshold (default 0.92, configurable per source). Reference: Tech Spec Section 3.4.
2. Stage 5: dedup by payload_id. Temporal decay reranking. Trim to max_results. Projection. Reference: Tech Spec Section 3.4.
3. Temporal decay: `final_score = (cos_sim * 0.7) + (exp(-lambda * days) * 0.3)`, lambda = ln(2) / half_life_days. Reference: Tech Spec Section 3.6.
4. Over-sample factor: retrieve top N candidates (default 100) before decay reranking. Reference: Tech Spec Section 3.6.
5. Deterministic: same config + data = same ranking. Reference: Tech Spec Section 3.6.

─── INVARIANTS ───

1. NEVER: Produce non-deterministic rankings.
2. NEVER: Return duplicate payload_ids.
3. NEVER: Hardcode similarity threshold, half_life, or over_sample_factor.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Full cascade: all 6 stages execute for balanced-profile query
- [ ] Temporal decay: newer memory ranks higher than older contradiction
- [ ] Determinism: same query twice = identical ranking
- [ ] Dedup: same payload_id from Stages 3+4 appears once in results

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 6: Semantic Cache + Temporal Decay"
```

═══════════════════════════════════════════════════════
## PHASE 7: MCP + OPENAI + SSE
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 14.3 — MCP Server, Section 12 — HTTP API
PERSONA: Client Integration Engineer
DURATION: 4–5 days

─── OBJECTIVE ───

Create the MCP server for Claude Desktop/Cursor, the /v1/memories OpenAI-compatible write endpoint, SSE streaming, and the MCP self-test command.

─── STRICT END-STATE ───

Files to CREATE:
- internal/mcp/server.go — MCP JSON-RPC server
- internal/mcp/tools.go — nexus_write, nexus_search, nexus_status tools
- internal/mcp/mcp_test.go — Binding, tools, self-test

─── BEHAVIORAL CONTRACT ───

1. MCP binds to 127.0.0.1 ONLY. NEVER 0.0.0.0. Reference: Tech Spec Section 14.3.
2. Tools: nexus_write, nexus_search, nexus_status. Reference: Tech Spec Section 14.3.
3. Internal pipeline (not HTTP round-trip). Same auth, policy, WAL, queue as HTTP path. Reference: Tech Spec Section 14.3.
4. MCP auth: dedicated mcp_key with constant-time comparison. Reference: Tech Spec Section 14.3.
5. MCP startup failure does NOT crash daemon. Log WARN. Reference: Tech Spec Section 14.3.
6. `bubblefish mcp test`: exits 0 on success within 5 seconds. Reference: Tech Spec Section 14.3.
7. POST /v1/memories: OpenAI chat messages format. Reference: Tech Spec Section 12.
8. SSE: GET /query/{dest} with Accept: text/event-stream. Reference: Tech Spec Section 12.

─── INVARIANTS ───

1. NEVER: Bind MCP to 0.0.0.0. Hardcode 127.0.0.1 in code.
2. NEVER: Route MCP calls through HTTP. Use internal function calls.
3. NEVER: Crash daemon because MCP failed to start.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] MCP binds to 127.0.0.1 (verify in server config)
- [ ] `bubblefish mcp test` exits 0 within 5 seconds
- [ ] nexus_write via MCP produces WAL entry
- [ ] POST /v1/memories with OpenAI format: 200
- [ ] SSE: text/event-stream returns events
- [ ] MCP port conflict: daemon starts, MCP warned

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 7: MCP + OpenAI + SSE"
```

═══════════════════════════════════════════════════════
## PHASE 8: INSTALL + PACKAGE + TRAY
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 13 — Admin UX, Section 2 — Environment
PERSONA: Developer Experience Engineer
DURATION: 3–4 days

─── OBJECTIVE ───

Create `bubblefish install`, `bubblefish start`, system tray, web dashboard skeleton, and packaging (Makefile, Dockerfile, docker-compose.yml).

─── STRICT END-STATE ───

Files to CREATE:
- cmd/bubblefish/install.go — Install wizard with --dest and --mode flags
- cmd/bubblefish/start.go — Start command: daemon + MCP + dashboard + tray
- internal/tray/tray.go — Windows tray. Headless: skip, log INFO
- internal/web/dashboard.go — Web dashboard skeleton (admin auth required)
- Makefile — Build targets for all platforms
- Dockerfile — Multi-stage build
- docker-compose.yml — Volumes, secrets, read-only filesystem

─── BEHAVIORAL CONTRACT ───

1. `bubblefish install --dest sqlite`: creates daemon.toml, sources/, destinations/. Prints config path and next steps. NEVER silent. Reference: Tech Spec Section 13.1.
2. `bubblefish start`: starts daemon + MCP + dashboard + tray. Reference: Tech Spec Section 13.1.
3. Headless Linux: skip tray, log INFO. Reference: Tech Spec Section 2.1.
4. All paths: filepath.Join. NEVER string concatenation with "/". Reference: Build Guide Phase 8.
5. os.UserHomeDir() errors are fatal. Reference: Build Guide CR-8.
6. docker-compose.yml valid. Reference: Tech Spec Section 2.1.
7. Web dashboard requires admin auth on all endpoints. Reference: Tech Spec Section 13.2.
8. Dashboard uses textContent only, NEVER innerHTML (XSS prevention). Reference: Tech Spec Section 13.2.

─── INVARIANTS ───

1. NEVER: Use string concatenation for file paths.
2. NEVER: Complete install silently.
3. NEVER: Panic on headless Linux.
4. NEVER: Use innerHTML in dashboard code.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] `bubblefish install --dest sqlite`: creates config, prints paths
- [ ] `bubblefish start`: daemon responds on configured port
- [ ] Headless Linux: INFO log about skipped tray
- [ ] docker-compose.yml: valid syntax

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 8: Install + Package + Tray"
```

═══════════════════════════════════════════════════════
## PHASE 9: TESTING + SECURITY AUDIT
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 8 — Failure Contracts, Section 16 — Validation Plan
PERSONA: Site Reliability Engineer + Security Architect
DURATION: 4–5 days

─── OBJECTIVE ───

Build the full test suite: concurrent tests, crash recovery golden scenario, load test, timing attack test, CRC32 corruption detection. Validates all previous phases under stress.

─── BEHAVIORAL CONTRACT ───

1. Concurrent test: 100 goroutines mixed reads + writes. Zero -race reports. Reference: Tech Spec Section 16.
2. Crash recovery (golden scenario): 50 payloads, kill -9, restart, all 50 present, 0 duplicates. Reference: Tech Spec Section 16.
3. Load test: 1000 concurrent writes. Zero data loss. Reference: Tech Spec Section 16.
4. Timing attack test: 1000 samples wrong vs correct key. p99 < 1ms. Reference: Tech Spec Section 16.
5. CRC32 corruption detection: corrupt entry, replay skips with WARN. Reference: Tech Spec Section 16.
6. Scope isolation: two sources, source A never sees source B cache entries. Reference: Tech Spec Section 16.
7. Queue overload: fill queue, verify 429 with Retry-After. Reference: Tech Spec Section 16.

─── VERIFICATION GATE ───

```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] All tests pass. Zero race reports. Golden crash recovery: 50/50, 0 dups.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 9: Testing + Security Audit"
```

═══════════════════════════════════════════════════════
## PHASE 10: README + DOCS + DEMO
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 13 — Admin UX
PERSONA: Developer Experience Engineer
DURATION: 2–3 days

─── OBJECTIVE ───

Create README, user documentation, crash recovery demo instructions, and Known Limitations table.

─── BEHAVIORAL CONTRACT ───

1. README opens with: "Your AI clients forget everything. Nexus fixes that." Reference: Build Guide Phase 10.
2. Crash recovery demo: concrete commands. Reference: Build Guide Phase 10.
3. Known Limitations table: honest, specific. Reference: Tech Spec Section 15.1.
4. No competitor names. Describe capability gaps. Reference: Build Guide Phase 10.
5. No confidential content in public documents. Reference: Build Guide Phase 10.

─── INVARIANTS ───

1. NEVER: Include confidential strategic content in public documents.
2. NEVER: Name specific competitors.
3. NEVER: Claim unimplemented features.

─── VERIFICATION GATE ───

- [ ] README renders correctly on GitHub
- [ ] Known Limitations present and honest
- [ ] No competitor names
- [ ] No confidential content

─── COMMIT ───
```
git add -A
git commit -s -m "Phase 10: README + Docs + Demo"
```
# V2.2 REFINEMENT PHASES (R-1 through R-28)

> **Rule: Feed ONE phase at a time. Attach the V2.2 Tech Spec as context. Run Verification Gate after each phase. Do not proceed if ANY check fails.**

═══════════════════════════════════════════════════════
## PHASE R-1: WAL HMAC INTEGRITY
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 4.1 — WAL Record Structure (Unencrypted Layout integrity=mac), Section 6.4.1
PERSONA: Senior Backend / Storage Engineer + Security Architect
DURATION: 1–2 days

─── OBJECTIVE ───
Add optional HMAC-SHA256 integrity verification to WAL entries alongside existing CRC32. CRC32 detects accidental corruption; HMAC detects intentional tampering. This is a Tier 2 feature: optional, disabled by default, fail-fast on misconfiguration.

─── REQUIRED EXPERT MINDSET ───
Senior Backend / Storage Engineer + Security Architect. CRC32 is cheap and catches bit flips but an attacker can recompute it. HMAC-SHA256 requires a secret key, making tampering detectable. The key is loaded once at startup via config.ResolveEnv — never re-read per entry. MarkDelivered must recompute BOTH CRC32 and HMAC when rewriting entry status. A structured security event (wal_tamper_detected) must be emitted on HMAC mismatch, not just a log line.

─── STRICT END-STATE ───
Files to MODIFY:
- internal/wal/wal.go — Integrity mode support, HMAC computation on write, validation on replay, MarkDelivered recomputation
- internal/config/types.go — WAL integrity config block

Files to CREATE:
- internal/wal/integrity.go — HMAC computation and validation functions
- internal/wal/integrity_test.go — Tamper detection, fail-fast, MarkDelivered recomputation tests

─── BEHAVIORAL CONTRACT ───
1. Config: `[daemon.wal.integrity] mode = "crc32" | "mac"`, `mac_key_file = "file:/path"`. Default: mode="crc32". Reference: Tech Spec Section 6.4.1.
2. When mode=mac, each WAL line is: `JSON_BYTES<TAB>CRC32_HEX<TAB>HMAC_HEX<NEWLINE>`. Reference: Tech Spec Section 4.1.
3. HMAC computed over JSON bytes (same scope as CRC32) using `crypto/hmac` + `crypto/sha256`. Reference: Tech Spec Section 4.1.
4. HMAC key loaded once at startup via config.ResolveEnv. Never re-read per entry. Reference: Tech Spec Section 6.4.1.
5. On replay: CRC32 validated first (cheap), then HMAC (more expensive). If CRC fails, skip entry, WARN, increment bubblefish_wal_crc_failures_total. If HMAC fails, skip entry, WARN, increment bubblefish_wal_integrity_failures_total, emit structured security event (wal_tamper_detected). Reference: Tech Spec Section 4.1.
6. MarkDelivered: recompute BOTH CRC32 and HMAC over the new JSON bytes (with status=DELIVERED). Reference: Tech Spec Section 4.3.
7. Fail-fast: integrity=mac + missing/empty/unreadable mac_key_file = daemon MUST refuse to start. Reference: Tech Spec Section 4.1.
8. New metric: bubblefish_wal_integrity_failures_total (Counter). Reference: Tech Spec Section 11.3.

─── INVARIANTS ───
1. NEVER: Compute HMAC when integrity=crc32. No performance penalty when disabled.
2. NEVER: Re-read mac_key_file per entry. Load once at startup.
3. NEVER: Leave old HMAC on a MarkDelivered rewrite. Recompute over new JSON bytes.
4. NEVER: Start daemon with integrity=mac and missing key. Fail fast.
5. NEVER: Process an entry with invalid HMAC. Skip it.

─── SECURITY CHECKPOINT ───
1. HMAC key never logged at any level. Reference: Tech Spec Section 6.5.
2. Structured security event (wal_tamper_detected) emitted on HMAC mismatch with segment file path and line number. Reference: Tech Spec Section 11.2.

─── IMPLEMENTATION DIRECTIVES ───
- USE: `crypto/hmac` + `crypto/sha256` from Go stdlib
- USE: `hmac.New(sha256.New, keyBytes)` — create once at startup, reuse (or create per entry — HMAC is stateful, must create new per computation)
- USE: `hex.EncodeToString(mac.Sum(nil))` for hex encoding
- AVOID: Any third-party HMAC library
- EDGE CASE: mode=crc32 (default). WAL lines have NO third tab-separated field. Backward compatible.
- EDGE CASE: Upgrading from crc32 to mac mode. Old entries have no HMAC. Replay must handle both formats: 2-field lines (CRC only) and 3-field lines (CRC + HMAC). Old entries without HMAC are treated as valid (no tamper check possible for pre-upgrade entries).

─── VERIFICATION GATE ───
Compilation:
```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

Behavioral Verification:
- [ ] Write 10 entries with integrity=mac. Each line has 3 tab-separated fields.
- [ ] Modify one byte of a WAL entry JSON. Replay skips it with WARN + security event.
- [ ] Modify CRC but not HMAC. Replay skips at CRC stage (HMAC never computed).
- [ ] integrity=mac with missing mac_key_file: daemon refuses to start with clear error.
- [ ] integrity=crc32 (default): WAL lines have 2 fields only. No HMAC overhead.
- [ ] MarkDelivered with integrity=mac: new HMAC computed over DELIVERED JSON.

Security Verification:
- [ ] HMAC key value not in any log output (grep all log levels)
- [ ] wal_tamper_detected security event includes segment file and line number

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-1: WAL HMAC Integrity"
```

═══════════════════════════════════════════════════════
## PHASE R-2: WAL ENCRYPTION
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 4.1 — Encrypted Layout, Section 6.4.2
PERSONA: Senior Backend / Storage Engineer + Security Architect
DURATION: 1–2 days

─── OBJECTIVE ───
Add optional AES-256-GCM at-rest encryption for WAL entries. Every encrypted entry gets a unique 12-byte nonce from crypto/rand. CRC32 covers the encrypted form, ensuring corruption is detected before decryption is attempted. MarkDelivered must decrypt, modify status, re-encrypt with a NEW nonce, and recompute CRC.

─── BEHAVIORAL CONTRACT ───
1. Config: `[daemon.wal.encryption] enabled = false`, `key_file = "file:/path"` (32-byte AES-256 key). Reference: Tech Spec Section 6.4.2.
2. Encrypted entry layout: version(1B) + key_id(4B) + nonce(12B) + ciphertext(var) + tab + CRC32_HEX. Reference: Tech Spec Section 4.1.
3. CRC32 computed over (version + key_id + nonce + ciphertext). Validates before decryption. Reference: Tech Spec Section 4.1.
4. Nonce: 12 bytes from crypto/rand. Unique per entry. NEVER reuse. Reference: Tech Spec Section 4.1.
5. key_id: currently always 0x00000001 (single key). v3 key rotation will use different IDs. Reference: Tech Spec Section 6.4.2.
6. MarkDelivered with encryption: decrypt old entry, change status, re-encrypt with NEW nonce, recompute CRC. Reference: Tech Spec Section 4.3.
7. Fail-fast: encryption enabled + missing/empty/wrong-size key = refuse to start. Reference: Tech Spec Section 4.1.
8. If HMAC also enabled: HMAC over plaintext BEFORE encryption. On replay: CRC → decrypt → HMAC → process. Reference: Tech Spec Section 4.1.
9. Use crypto/aes + crypto/cipher (GCM) from Go stdlib. Reference: Tech Spec Section 6.4.2.

─── INVARIANTS ───
1. NEVER: Reuse a nonce. Every entry and every MarkDelivered rewrite gets a fresh 12-byte nonce from crypto/rand.
2. NEVER: Attempt decryption before CRC validation passes.
3. NEVER: Start daemon with encryption enabled and missing/wrong-size key.
4. NEVER: Write unencrypted WAL entries when encryption is configured.

─── VERIFICATION GATE ───
```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

- [ ] Write 10 entries encrypted. Restart. All 10 decrypted and readable.
- [ ] hexdump of encrypted WAL: no plaintext memory content visible.
- [ ] Wrong key: decryption fails (GCM auth tag mismatch). Entry skipped with WARN.
- [ ] Encryption enabled + key_file missing: daemon refuses to start.
- [ ] Encryption enabled + key_file wrong size (not 32 bytes): daemon refuses to start.
- [ ] MarkDelivered with encryption: new nonce differs from original (verify in test).
- [ ] Encryption + HMAC both enabled: full write-replay-verify cycle passes.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-2: WAL Encryption"
```

═══════════════════════════════════════════════════════
## PHASE R-3: CONFIG SIGNING
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 6.5
PERSONA: Security Architect
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. `bubblefish sign-config --keyref env:NEXUS_SIGNING_KEY`: reads compiled/*.json, computes HMAC-SHA256, writes *.sig files. Reference: Tech Spec Section 6.5.
2. `[daemon.signing] enabled = false, key_file = "file:/path"`. Reference: Tech Spec Section 9.2.13.
3. Startup: if signing enabled, verify all compiled/*.json have valid *.sig. Refuse to start if any missing or invalid. Reference: Tech Spec Section 6.5.
4. Hot reload: re-verify signatures. Refuse reload on invalid (log ERROR, keep old config). Reference: Tech Spec Section 6.5.
5. Structured security event: config_signature_invalid on verification failure. Reference: Tech Spec Section 11.2.
6. sign-config works independently of running daemon. Reference: Tech Spec Section 6.5.

─── VERIFICATION GATE ───
- [ ] Sign config, verify passes. Modified config byte: verification fails, daemon refuses to start.
- [ ] Signing disabled: no .sig checking. Backward compatible.
- [ ] Security event emitted on signature failure.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-3: Config Signing"
```

═══════════════════════════════════════════════════════
## PHASE R-4: ADMIN VS DATA TOKEN SEPARATION
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 6.1
PERSONA: Security Architect
DURATION: 0.5–1 day

─── BEHAVIORAL CONTRACT ───
1. Token classes: 'admin' (admin_token), 'data' (source api_key), 'mcp' (mcp api_key). Reference: Tech Spec Section 6.1.
2. Data endpoints (/inbound/{source}, /query/{dest}, /v1/memories): reject admin and mcp tokens with 401 wrong_token_class. Reference: Tech Spec Section 6.1.
3. Admin endpoints (/api/*, /metrics, web dashboard): reject data and mcp tokens with 401 wrong_token_class. Reference: Tech Spec Section 6.1.
4. /health and /ready: no auth required. Reference: Tech Spec Section 12.
5. Token class determined at startup. Check AFTER constant-time comparison succeeds. Reference: Tech Spec Section 6.1.
6. Error: `{"error":"unauthorized","message":"wrong token class for this endpoint"}`. Reference: Tech Spec Section 6.1.

─── VERIFICATION GATE ───
- [ ] Admin token on /inbound/{source}: 401 wrong_token_class.
- [ ] Data key on /api/status: 401 wrong_token_class.
- [ ] MCP key on /inbound: 401. MCP key on /api/status: 401.
- [ ] Correct token class on each endpoint: expected result.
- [ ] /health and /ready: 200 with no auth.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-4: Admin vs Data Token Separation"
```

═══════════════════════════════════════════════════════
## PHASE R-5: PROVENANCE FIELDS
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 7.1
PERSONA: Database / Data Infra Specialist
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. TranslatedPayload: add ActorType string, ActorID string. Reference: Tech Spec Section 7.1.
2. Valid actor_type: "user", "agent", "system". Invalid returns 400. Reference: Tech Spec Section 7.1.
3. Set from X-Actor-Type and X-Actor-ID headers. Fall back to source config defaults. Reference: Tech Spec Section 7.1.
4. Schema migration: ALTER TABLE ADD COLUMN with DEFAULT '' for existing data. Reference: Tech Spec Section 7.1.
5. Stage 3: support ?actor_type= filter. Reference: Tech Spec Section 7.1.
6. Projection: actor_type and actor_id available in include_fields. Reference: Tech Spec Section 7.1.

─── INVARIANTS ───
1. NEVER: Break existing data with schema migration. DEFAULT '' preserves compatibility.
2. NEVER: Accept actor_type values other than "user", "agent", "system".

─── VERIFICATION GATE ───
- [ ] Write with X-Actor-Type: agent. WAL and DB have actor_type='agent'.
- [ ] Query with ?actor_type=user: only user-type returned.
- [ ] Schema migration: existing data survives with empty defaults.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-5: Provenance Fields"
```

═══════════════════════════════════════════════════════
## PHASE R-6: RETRIEVAL PROFILES
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 3.5
PERSONA: Search & Retrieval Engineer
DURATION: 1–2 days

─── BEHAVIORAL CONTRACT ───
1. fast: stages 0,1,3. No vector search. Lowest latency. Reference: Tech Spec Section 3.5.
2. balanced (default): stages 0,1,2,3,4,5. Over-sample 100. Decay on (7d). Reference: Tech Spec Section 3.5.
3. deep: stages 0,2,3,4,5 (skip exact cache). Over-sample 500. Decay on (30d). Reference: Tech Spec Section 3.5.
4. ?profile= query parameter. Falls back to source config default_profile. Reference: Tech Spec Section 3.5.
5. Policy: allowed_profiles list. 403 if requested profile not allowed. Reference: Tech Spec Section 3.5.
6. _nexus.profile in response metadata. Reference: Tech Spec Section 7.2.
7. profileEnabled(stage, profile) checked before each stage. Reference: Tech Spec Section 3.5.

─── VERIFICATION GATE ───
- [ ] profile=fast: stages 2+4 not executed. _nexus.profile='fast'.
- [ ] profile=deep: stage 1 not executed. Over-sample = 500.
- [ ] Profile not in allowed_profiles: 403 policy_denied.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-6: Retrieval Profiles"
```

═══════════════════════════════════════════════════════
## PHASE R-7: TIERED TEMPORAL DECAY
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 3.6
PERSONA: Search & Retrieval Engineer
DURATION: 1–2 days

─── BEHAVIORAL CONTRACT ───
1. Three levels: global [retrieval] > per-destination [destination.decay] > per-collection. Most specific wins. Reference: Tech Spec Section 3.6.
2. Exponential mode: final_score = (cos_sim * 0.7) + (exp(-lambda * days) * 0.3). Reference: Tech Spec Section 3.6.
3. Step mode: score = cos_sim if days < threshold, else cos_sim * 0.1. Reference: Tech Spec Section 3.6.
4. resolveDecay(global, dest, collection, sourcePolicy) returns (mode, halfLife, threshold). Reference: Tech Spec Section 3.6.
5. Deterministic: same config + data = same ranking. Reference: Tech Spec Section 3.6.

─── VERIFICATION GATE ───
- [ ] Per-collection decay overrides per-destination which overrides global.
- [ ] Step mode: 29 days scores normally, 31 days scores 10%.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-7: Tiered Temporal Decay"
```

═══════════════════════════════════════════════════════
## PHASE R-8: SEMANTIC SHORT-CIRCUIT + FAST PATH
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 3.7
PERSONA: Search & Retrieval Engineer
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. Cache hit (Stage 1 or 2) short-circuits remaining stages. Reference: Tech Spec Section 3.7.
2. Exact-subject fast path: query with only subject + limit bypasses cascade. `SELECT * FROM memories WHERE subject = ? ORDER BY timestamp DESC LIMIT ?`. Reference: Tech Spec Section 3.7.
3. _nexus.stage = 'fast_path' when fast path used. Reference: Tech Spec Section 3.7.
4. Fast path auto-selected on query shape match. No opt-in. Reference: Tech Spec Section 3.7.
5. Fast path returning 0 results: return empty. Do NOT fall through to cascade. Reference: Tech Spec Section 3.7.

─── VERIFICATION GATE ───
- [ ] Cache hit skips remaining stages.
- [ ] Subject + limit only query uses fast path. _nexus.stage='fast_path'.
- [ ] Fast path with 0 results: empty response, no cascade fallthrough.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-8: Semantic Short-Circuit + Fast Path"
```

═══════════════════════════════════════════════════════
## PHASE R-9: WAL HEALTH WATCHDOG
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 4.4
PERSONA: Site Reliability Engineer
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. Background goroutine every watchdog.interval_seconds (default 30s). Reference: Tech Spec Section 4.4.
2. Checks: WAL dir writable (probe file), disk space >= min_disk_bytes, WAL append latency p99, pending count. Reference: Tech Spec Section 4.4.
3. Metrics: bubblefish_wal_disk_bytes_free, bubblefish_wal_healthy (gauge), bubblefish_wal_append_latency_seconds. Reference: Tech Spec Section 4.4.
4. Unhealthy: /ready returns 503. Log ERROR. Reference: Tech Spec Section 4.4.
5. Own context for clean shutdown. Reference: Tech Spec Section 4.4.

─── INVARIANTS ───
1. NEVER: Block the write path from the watchdog goroutine. It reads state, never writes WAL entries.

─── VERIFICATION GATE ───
- [ ] Watchdog detects unwritable WAL dir. /ready returns 503.
- [ ] Watchdog shutdown: goroutine exits cleanly, no leak.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-9: WAL Health Watchdog"
```

═══════════════════════════════════════════════════════
## PHASE R-10: CONSISTENCY ASSERTIONS
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 11.5
PERSONA: Site Reliability Engineer
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. Background goroutine every consistency.interval_seconds (default 300s). Reference: Tech Spec Section 11.5.
2. Sample consistency.sample_size (default 100) random DELIVERED WAL entries. Reference: Tech Spec Section 11.5.
3. Query destination: verify each sampled payload exists. Score = found / sampled. Reference: Tech Spec Section 11.5.
4. Score < 0.95: WARN. Score < 0.80: ERROR. Reference: Tech Spec Section 11.5.
5. Expose via bubblefish_consistency_score gauge and /api/status. Reference: Tech Spec Section 11.5.
6. Read-only. NEVER modify WAL or destination. Reference: Tech Spec Section 11.5.

─── VERIFICATION GATE ───
- [ ] All delivered: score = 1.0. Delete 5 from destination: score drops, WARN logged.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-10: Consistency Assertions"
```

═══════════════════════════════════════════════════════
## PHASE R-11: CONFIG LINT
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 6.7
PERSONA: Developer Experience Engineer
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. `bubblefish lint` CLI + /api/lint endpoint (admin auth). Reference: Tech Spec Section 6.7.
2. Checks: dangerous bind (0.0.0.0 without TLS), missing idempotency, unbounded rate limits, literal keys, missing keyfiles, unsafe proxy CIDRs (0.0.0.0/0), unknown destinations, duplicate keys, unsigned configs in signed mode, event sinks without retry. Reference: Tech Spec Section 6.7.
3. Results: warnings with severity (warn, error). Exit code 0 if no errors, 1 if errors. Reference: Tech Spec Section 6.7.
4. Metric: bubblefish_config_lint_warnings gauge. Reference: Tech Spec Section 11.3.

─── VERIFICATION GATE ───
- [ ] Config with 0.0.0.0 bind + no TLS: lint returns warning.
- [ ] Config with empty api_key: lint returns error. Exit code 1.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-11: Config Lint"
```

═══════════════════════════════════════════════════════
## PHASE R-12: TLS/mTLS + TRUSTED PROXIES
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 6.2, 6.3
PERSONA: Security Architect
DURATION: 1–2 days

─── BEHAVIORAL CONTRACT ───
1. [daemon.tls] config: enabled, cert_file, key_file, min_version, max_version, client_ca_file, client_auth. Reference: Tech Spec Section 6.2.
2. Fail-fast on missing certs when TLS enabled. Reference: Tech Spec Section 6.2.
3. mTLS: client_ca_file set = load CA pool. client_auth modes: require_and_verify, verify_if_given, no_client_cert. Reference: Tech Spec Section 6.2.
4. Trusted proxies: [daemon.trusted_proxies] cidrs, forwarded_headers. Reference: Tech Spec Section 6.3.
5. From trusted CIDR: read forwarded headers for effective_client_ip. Non-trusted: TCP source. Reference: Tech Spec Section 6.3.
6. effective_client_ip used in logging, rate limiting, security events. Reference: Tech Spec Section 6.3.

─── VERIFICATION GATE ───
- [ ] TLS enabled: HTTPS works. HTTP rejected.
- [ ] mTLS: client without cert rejected. Client with valid cert accepted.
- [ ] Trusted proxy: forwarded headers read. Non-trusted: TCP source used.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-12: TLS/mTLS + Trusted Proxies"
```

═══════════════════════════════════════════════════════
## PHASE R-13: DEPLOYMENT MODES
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 2.2.3
PERSONA: Principal Systems Architect
DURATION: 0.5 days

─── BEHAVIORAL CONTRACT ───
1. mode field: safe, balanced, fast. Applied as config overlay at startup. User values override. Reference: Tech Spec Section 2.2.3.
2. safe: TLS required, encryption required, MAC integrity, 500/min. Reference: Tech Spec Section 2.2.3.
3. balanced: TLS off, encryption off, CRC32, 2000/min. Reference: Tech Spec Section 2.2.3.
4. fast: TLS off, encryption off, CRC32, 10000/min. Reference: Tech Spec Section 2.2.3.
5. Lint warns if mode=safe but TLS explicitly disabled. Reference: Tech Spec Section 6.7.

─── VERIFICATION GATE ───
- [ ] mode=safe: TLS defaults applied. mode=safe + explicit tls.enabled=false: lint warns.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-13: Deployment Modes"
```

═══════════════════════════════════════════════════════
## PHASE R-14: SIMPLE MODE INSTALL
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 2.2.1
PERSONA: Developer Experience Engineer
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. `bubblefish install --mode simple`: creates daemon.toml, sources/default.toml, destinations/sqlite.toml. Reference: Tech Spec Section 2.2.1.
2. API key: auto-generated 32-byte hex via crypto/rand. Reference: Tech Spec Section 2.2.1.
3. Embeddings disabled. No external API keys needed. Reference: Tech Spec Section 2.2.1.
4. Prints three next steps: start, example curl, optional integration config. Reference: Tech Spec Section 2.2.1.
5. Config files have explanatory comments. Reference: Tech Spec Section 2.2.1.
6. Refuses if config dir exists unless --force. Reference: Tech Spec Section 2.2.1.

─── VERIFICATION GATE ───
- [ ] Simple install on clean machine: config created. `bubblefish start` works. curl POST returns 200.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-14: Simple Mode Install"
```

═══════════════════════════════════════════════════════
## PHASE R-15: INSTALL PROFILES
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 2.2.2
PERSONA: Client Integration Engineer
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. --profile openwebui: source config for Open WebUI + sqlite dest + example provider JSON. Reference: Tech Spec Section 2.2.2.
2. --dest postgres: prompts for connection string, runs doctor check. Reference: Tech Spec Section 2.2.2.
3. --dest openbrain: prompts for Supabase URL and key. Reference: Tech Spec Section 2.2.2.
4. --oauth-template caddy/traefik: example configs in examples/oauth/. Reference: Tech Spec Section 2.2.2.

─── VERIFICATION GATE ───
- [ ] Each profile generates valid TOML that `bubblefish build` accepts.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-15: Install Profiles"
```

═══════════════════════════════════════════════════════
## PHASE R-16: BUBBLEFISH DEV
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 13.1
PERSONA: Developer Experience Engineer
DURATION: 0.5 days

─── BEHAVIORAL CONTRACT ───
1. Same daemon as start. log_level=debug, auto-reload enabled, config paths printed at startup. Reference: Tech Spec Section 13.1.
2. Does NOT change pipeline semantics. No dev-only code paths. Reference: Tech Spec Section 13.1.

─── VERIFICATION GATE ───
- [ ] `bubblefish dev` starts with DEBUG logging. Same write/read behavior as `bubblefish start`.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-16: bubblefish dev"
```

═══════════════════════════════════════════════════════
## PHASE R-17: STRUCTURED SECURITY EVENTS
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 11.2
PERSONA: Observability / Telemetry Engineer + Security Architect
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. [security_events] enabled, log_file path. Reference: Tech Spec Section 9.2.18.
2. Events: auth_failure, policy_denied, rate_limit_hit, wal_tamper_detected, config_signature_invalid, admin_access. Reference: Tech Spec Section 11.2.
3. JSON Lines: event_type, source, subject, ip, endpoint, timestamp, details. Reference: Tech Spec Section 11.2.
4. /api/security/events (admin): last N events. /api/security/summary (admin): aggregated counts. Reference: Tech Spec Section 12.
5. Append-only file writer with mutex. Reference: Tech Spec Section 11.2.

─── VERIFICATION GATE ───
- [ ] Auth failure: event in security log. /api/security/events returns it.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-17: Structured Security Events"
```

═══════════════════════════════════════════════════════
## PHASE R-18: SECURITY METRICS
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 11.3
PERSONA: Observability / Telemetry Engineer
DURATION: 0.5 days

─── BEHAVIORAL CONTRACT ───
1. bubblefish_auth_failures_total{source} on 401s. Reference: Tech Spec Section 11.3.
2. bubblefish_policy_denials_total{source,reason} on 403s. Reference: Tech Spec Section 11.3.
3. bubblefish_rate_limit_hits_total{source} on 429s. Reference: Tech Spec Section 11.3.
4. bubblefish_admin_calls_total{endpoint} on admin access. Reference: Tech Spec Section 11.3.

─── VERIFICATION GATE ───
- [ ] All 4 metrics visible in /metrics after triggering events.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-18: Security Metrics"
```

═══════════════════════════════════════════════════════
## PHASE R-19: EVENT SINK (WEBHOOKS)
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 10
PERSONA: Principal Systems Architect
DURATION: 2–3 days

─── BEHAVIORAL CONTRACT ───
1. [daemon.events] enabled, max_inflight (1000), retry_backoff_seconds. [[daemon.events.sinks]] name, url, timeout_seconds, max_retries, content (summary|full). Reference: Tech Spec Section 10.
2. Lossy buffered channel (capacity = max_inflight). NEVER blocks write path. Reference: Tech Spec Section 10.1.
3. Emission after WAL append. Non-blocking send. If full: drop, increment bubblefish_events_dropped_total. Reference: Tech Spec Section 10.1.
4. Per-sink retry with exponential backoff. After max_retries: drop, log WARN. Reference: Tech Spec Section 10.3.
5. Summary payload: payload_id, source, subject, destination, timestamp, actor_type. Reference: Tech Spec Section 10.2.
6. Metrics: events_delivered_total, events_failed_total, events_dropped_total. Reference: Tech Spec Section 11.3.
7. Shutdown: drain event workers in Stage 3. Reference: Tech Spec Section 14.2.

─── INVARIANTS ───
1. NEVER: Block the write path for event delivery. Channel is lossy by design.
2. NEVER: Include memory content in summary mode.

─── VERIFICATION GATE ───
- [ ] Webhook receives event within 5s of write.
- [ ] Unreachable sink: retries, then drops. Main write path returns 200.
- [ ] 1000 writes with unreachable sink: all 1000 return 200. Events dropped, metrics incremented.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-19: Event Sink (Webhooks)"
```

═══════════════════════════════════════════════════════
## PHASE R-20: OAUTH EDGE TEMPLATES
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 6.6
PERSONA: Security Architect
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. [daemon.jwt] enabled, jwks_url, claim_to_source, audience. Reference: Tech Spec Section 6.6.
2. Middleware: extract JWT from Bearer, validate against JWKS, map claim to source. Reference: Tech Spec Section 6.6.
3. JWKS fetched at startup, cached. Refresh on failure max once/minute. Reference: Tech Spec Section 6.6.
4. Invalid JWT: 401. Reference: Tech Spec Section 6.6.
5. Template generators: --oauth-template caddy|traefik. Static files in examples/oauth/. Reference: Tech Spec Section 6.6.

─── VERIFICATION GATE ───
- [ ] Valid JWT maps to source. Invalid JWT: 401. Templates generated.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-20: OAuth Edge Templates"
```

═══════════════════════════════════════════════════════
## PHASE R-21: PIPELINE VISUALIZATION + BLACK BOX MODE
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 13.2
PERSONA: UX / Frontend Engineer + Observability / Telemetry Engineer
DURATION: 2 days

─── BEHAVIORAL CONTRACT ───
1. Lossy buffered channel (capacity 1000). Non-blocking send: `select { case vizChan <- event: default: dropped++ }`. Reference: Tech Spec Section 13.2.
2. /api/viz/events SSE endpoint (admin auth). Reference: Tech Spec Section 12.
3. Event: request_id, stage, duration_ms, hit/miss, result_count, timestamp. Reference: Tech Spec Section 13.2.
4. Black Box Mode: client-side toggle. Server sends full events. Reference: Tech Spec Section 13.2.
5. Metric: bubblefish_visualization_events_dropped_total. Reference: Tech Spec Section 11.3.
6. Dashboard: textContent only, NEVER innerHTML. Reference: Build Guide R-27.

─── INVARIANTS ───
1. NEVER: Block hot paths for visualization. Channel is lossy.
2. NEVER: Use innerHTML in dashboard. Always textContent (XSS prevention).

─── VERIFICATION GATE ───
- [ ] SSE endpoint streams events. 1000 concurrent queries: zero hangs from viz channel.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-21: Pipeline Visualization + Black Box Mode"
```

═══════════════════════════════════════════════════════
## PHASE R-22: CONFLICT INSPECTOR + TIME-TRAVEL
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 13.2
PERSONA: Database / Data Infra Specialist + UX / Frontend Engineer
DURATION: 1–2 days

─── BEHAVIORAL CONTRACT ───
1. /api/conflicts (admin): GROUP BY subject, entity_key. Flag divergent content. Paginated. Filterable. Reference: Tech Spec Section 13.2.
2. /api/timetravel (admin): WHERE timestamp <= ? on structured query. Reference: Tech Spec Section 13.2.
3. Both read-only. NEVER modify data. Reference: Tech Spec Section 13.2.
4. Conflict results: subject, entity_key, conflicting_values[], sources[], timestamps[]. Reference: Tech Spec Section 13.2.

─── INVARIANTS ───
1. NEVER: Modify WAL or destination state from these endpoints. Read-only introspection only.

─── VERIFICATION GATE ───
- [ ] Contradictory memories detected. Time-travel returns correct historical state.
- [ ] Both endpoints return 401 for data-plane tokens.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-22: Conflict Inspector + Time-Travel"
```

═══════════════════════════════════════════════════════
## PHASE R-23: DEBUG STAGES
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 7.3
PERSONA: Search & Retrieval Engineer
DURATION: 0.5–1 day

─── BEHAVIORAL CONTRACT ───
1. ?debug_stages=true + admin token: _nexus.debug with stages_hit, candidates_per_stage, per_stage_latency_ms, cache_hit, cache_type, temporal_decay_config, total_latency_ms. Reference: Tech Spec Section 7.3.
2. Data token + debug_stages=true: silently ignored. Normal response. Reference: Tech Spec Section 7.3.

─── VERIFICATION GATE ───
- [ ] Admin + debug_stages: _nexus.debug present. Data token: debug absent, no error.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-23: Debug Stages"
```

═══════════════════════════════════════════════════════
## PHASE R-24: BACKUP AND RESTORE
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 14.5
PERSONA: Senior Backend / Storage Engineer
DURATION: 1–2 days

─── BEHAVIORAL CONTRACT ───
1. `bubblefish backup create --dest /path [--include-db]`: config, compiled, WAL, optionally SQLite (.backup API). Reference: Tech Spec Section 14.5.
2. `bubblefish backup restore --from /path [--force]`: restore all. Verify SHA256 checksums from manifest.json. Reference: Tech Spec Section 14.5.
3. Online backup: works while daemon running. Copy WAL last. Reference: Tech Spec Section 14.5.
4. Restore refuses overwrite without --force. Reference: Tech Spec Section 14.5.

─── VERIFICATION GATE ───
- [ ] Backup, delete config, restore: daemon starts with restored config.
- [ ] Corrupted backup file (checksum mismatch): restore fails with clear error.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-24: Backup and Restore"
```

═══════════════════════════════════════════════════════
## PHASE R-25: BUBBLEFISH BENCH
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 13.4
PERSONA: Site Reliability Engineer + Search & Retrieval Engineer
DURATION: 2–3 days

─── BEHAVIORAL CONTRACT ───
1. --mode throughput: N concurrent writes. req/s, p50/p95/p99, WAL latency, queue depth. Reference: Tech Spec Section 13.4.
2. --mode latency: N sequential reads. Per-stage breakdown via _nexus.debug. Reference: Tech Spec Section 13.4.
3. --mode eval: compare vs known-good JSON. Precision, recall, MRR, NDCG. Reference: Tech Spec Section 13.4.
4. HTTP client against live daemon (not in-process). Reference: Tech Spec Section 13.4.
5. --output=file.json for machine-readable results. Reference: Tech Spec Section 13.4.

─── VERIFICATION GATE ───
- [ ] Throughput bench produces stable metrics (run twice, within 10% variance).
- [ ] Latency bench shows per-stage breakdown.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-25: bubblefish bench"
```

═══════════════════════════════════════════════════════
## PHASE R-26: RELIABILITY DEMO
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 13.3, Section 16
PERSONA: Site Reliability Engineer
DURATION: 1 day

─── OBJECTIVE ───
Implement the golden crash-recovery scenario — the single most compelling proof of Nexus's value proposition. If this demo fails, nothing else matters.

─── BEHAVIORAL CONTRACT ───
1. `bubblefish demo`: write 50 memories (demo-001 through demo-050), SIGKILL daemon, wait 2s, restart, wait for /ready, query, assert 50 present + 0 duplicates. Reference: Tech Spec Section 13.3.
2. /api/demo/reliability (admin): same sequence, JSON results. Reference: Tech Spec Section 12.
3. Demo runner starts separate daemon process for SIGKILL simulation. Reference: Tech Spec Section 13.3.
4. Clean up demo data unless --keep. Exit 0 on success, 1 on failure. Reference: Tech Spec Section 13.3.

─── GOLDEN SCRIPT TEST ───
1. Start daemon process.
2. POST 50 writes with idempotency keys demo-001 through demo-050. All return 200.
3. SIGKILL daemon process.
4. Wait 2 seconds.
5. Start new daemon process. Wait for /ready = 200.
6. GET /query/sqlite?limit=100. Assert exactly 50 results. Assert 0 duplicate payload_ids. Assert all 50 idempotency keys represented.
7. Print PASS. Exit 0.

─── VERIFICATION GATE ───
- [ ] `bubblefish demo` passes: 50 present, 0 duplicates, exit 0.
- [ ] Run 3 times consecutively: all 3 pass (reliability, not luck).

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-26: Reliability Demo"
```

═══════════════════════════════════════════════════════
## PHASE R-27: SECURITY TAB + BLESSED CONFIGS + REF ARCHS
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 13.2, 13.5, 13.6
PERSONA: Client Integration Engineer + Security Architect
DURATION: 1–2 days

─── BEHAVIORAL CONTRACT ───
1. Dashboard security tab: source policies (read-only), auth failure history, lint warnings. Reference: Tech Spec Section 13.2.
2. examples/blessed/: Claude Desktop MCP (conservative), Claude Code HTTP, Open WebUI, Perplexity. Reference: Tech Spec Section 13.5.
3. docs/: dev-laptop.md, home-lab.md, air-gapped.md. Reference: Tech Spec Section 13.6.
4. THREAT_MODEL.md: in-scope (local attackers, eavesdroppers, disk theft, tampering). Out-of-scope (compromised host, hostile hypervisor, supply chain, DDoS). Reference: Tech Spec Section 6.8.
5. Dashboard: textContent only, NEVER innerHTML. Reference: Security Checkpoint.

─── VERIFICATION GATE ───
- [ ] Security tab renders with source policies and auth failure counts.
- [ ] Blessed configs are valid TOML (bubblefish build accepts them).
- [ ] THREAT_MODEL.md covers all items from Tech Spec Section 6.8.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-27: Security Tab + Blessed Configs + Ref Archs"
```

═══════════════════════════════════════════════════════
## PHASE R-28: ARCHITECTURE DIAGRAM + DOC POLISH
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 15
PERSONA: Developer Experience Engineer
DURATION: 1 day

─── BEHAVIORAL CONTRACT ───
1. Mermaid flowchart in README: Clients → Ingress → Nexus (planes) → Destinations. Reference: Tech Spec Section 15.
2. README: all V2.2 features. Simple Mode prominent. Crash demo prominent. Reference: Tech Spec Section 15.
3. CHANGELOG.md: all V2.1 → V2.2 changes. Reference: Build Guide R-28.
4. No confidential content in any public document. Reference: Build Guide R-28.
5. Known Limitations table updated and honest. Reference: Tech Spec Section 15.1.

─── INVARIANTS ───
1. NEVER: Include confidential strategic content in public documents.
2. NEVER: Claim unimplemented features in README.

─── VERIFICATION GATE ───
- [ ] Mermaid diagram renders on GitHub.
- [ ] README accurately reflects V2.2 spec.
- [ ] grep for confidential terms returns zero hits in public files.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-28: Architecture Diagram + Doc Polish"
```

## PHASE Ship: TAG v0.1.0 + RELEASE
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Section 16 — Validation Plan
PERSONA: Principal Systems Architect
DURATION: 1 day

─── OBJECTIVE ───

Final verification of all systems, cross-platform binary build, git tag, and GitHub release. Every item in the pre-ship checklist must pass. This is a REGRESSION gate — things verified in earlier phases could have been broken by later phases. No exceptions. No shortcuts.

─── PRE-SHIP CHECKLIST (all 45 items must pass) ───

Code Quality:
- [ ] `go build ./...` — zero warnings
- [ ] `go vet ./...` — zero findings
- [ ] `CGO_ENABLED=1 go test ./... -race -count=1` — zero failures, zero race reports

WAL & Durability:
- [ ] WAL DELIVERED: atomic rename, same FS, CRC32 (+ HMAC if integrity=mac)
- [ ] WAL encryption: AES-256-GCM round-trip tested
- [ ] WAL Scanner 10MB: entries > 64KB write and replay correctly
- [ ] WAL segment rotation crash: both segments replayed, zero duplicates
- [ ] Idempotency rebuilt from WAL replay after restart

Queue & Pipeline:
- [ ] Non-blocking queue select: Enqueue returns ErrLoadShed when full
- [ ] sync.Once on Drain() and Stop(): multiple calls safe
- [ ] DrainWithContext budgeted shutdown: exits within drain_timeout_seconds

HTTP & Auth:
- [ ] http.Server all 4 timeouts set
- [ ] handleQuery rate limited (both read and write paths)
- [ ] subtle.ConstantTimeCompare everywhere (timing test p99 < 1ms)
- [ ] Admin vs data token separation enforced (wrong_token_class on cross-use)
- [ ] configMu RWMutex on d.sources: RLock on auth, Lock on reload

Crash Recovery:
- [ ] `bubblefish demo` passes: 50 present, 0 duplicates (run 3 times)
- [ ] CRC32 corruption detection: tampered entry skipped with WARN
- [ ] HMAC tamper detection tested (integrity=mac)
- [ ] Config signing: modified config rejected in signed mode

Security:
- [ ] No API keys in any log output (grep all log levels)
- [ ] 0600/0700 on all sensitive files and directories
- [ ] Structured security events working (auth_failure, policy_denied, etc.)
- [ ] TLS/mTLS verified when enabled
- [ ] Trusted proxy IP derivation correct

Retrieval:
- [ ] Retrieval profiles: fast, balanced, deep all working
- [ ] Temporal decay: tiered, per-collection, exponential + step modes
- [ ] Semantic short-circuit and fast path tested
- [ ] Provenance filtering: actor_type filter works
- [ ] All Prometheus metrics non-zero after exercising write + read paths
- [ ] Consistency assertions producing valid score

Admin UX:
- [ ] textContent not innerHTML in dashboard (no XSS vectors)
- [ ] Conflict inspector detecting contradictions
- [ ] Time-travel returning correct historical state
- [ ] Debug stages payload present with admin token
- [ ] Pipeline visualization SSE streaming
- [ ] WAL watchdog reporting health correctly

Install & Operations:
- [ ] Simple Mode install works end-to-end
- [ ] All install profiles generate valid config
- [ ] bubblefish dev starts with debug defaults
- [ ] Backup create/restore round-trip working
- [ ] bubblefish bench all 3 modes working
- [ ] bubblefish mcp test exits 0
- [ ] Event sinks: delivery, retry, drop all working
- [ ] OAuth JWT mapping working when enabled
- [ ] docker-compose.yml valid

Documentation:
- [ ] README complete with no confidential content
- [ ] Security tab, blessed configs, reference architectures complete
- [ ] Architecture diagram renders on GitHub
- [ ] THREAT_MODEL.md present and accurate
- [ ] CHANGELOG.md covers all changes
- [ ] Known Limitations table honest and complete
- [ ] All .go files have AGPL-3.0 copyright header
- [ ] LICENSE file is AGPL-3.0

─── RELEASE COMMANDS ───

```bash
# Final build + test
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1

# Commit and tag
git add -A
git commit -s -m "Release v0.1.0 — BubbleFish Nexus"
git tag v0.1.0
git push origin main --tags

# Cross-platform binaries
GOOS=windows GOARCH=amd64 go build -ldflags "-X github.com/bubblefish-tech/nexus/internal/version.Version=0.1.0" -o dist/bubblefish-v0.1.0-windows-amd64.exe ./cmd/bubblefish/
GOOS=linux GOARCH=amd64 go build -ldflags "-X github.com/bubblefish-tech/nexus/internal/version.Version=0.1.0" -o dist/bubblefish-v0.1.0-linux-amd64 ./cmd/bubblefish/
GOOS=darwin GOARCH=amd64 go build -ldflags "-X github.com/bubblefish-tech/nexus/internal/version.Version=0.1.0" -o dist/bubblefish-v0.1.0-darwin-amd64 ./cmd/bubblefish/
GOOS=darwin GOARCH=arm64 go build -ldflags "-X github.com/bubblefish-tech/nexus/internal/version.Version=0.1.0" -o dist/bubblefish-v0.1.0-darwin-arm64 ./cmd/bubblefish/
```

─── COMMIT ───
```
git tag v0.1.0
git push origin main --tags
```

═══════════════════════════════════════════════════════
# END OF STATE & VERIFICATION GUIDE
═══════════════════════════════════════════════════════
