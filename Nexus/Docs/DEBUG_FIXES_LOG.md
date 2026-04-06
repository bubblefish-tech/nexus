# BubbleFish Nexus — Debug Fixes Log

**Scope:** Phases R-16 through R-23 (April 2026 build cycle)
**Audit Date:** 2026-04-06
**Tooling:** `go vet`, `staticcheck`, `golangci-lint` (errcheck, unused, ineffassign, staticcheck)
**Commit:** `f354790` (Phase R-23: Debug Stages — bug fix + lint sweep)

---

## Severity Definitions

| Severity | Definition |
|----------|-----------|
| **P0 — Critical** | Security vulnerability, data loss, or spec non-compliance that breaks a behavioral contract |
| **P1 — High** | Dead code on a security-sensitive path, unchecked errors on I/O operations |
| **P2 — Medium** | Lint violations, deprecated API usage, unchecked errors in non-critical paths |
| **P3 — Low** | Style issues, unchecked errors in test code only |

---

## Bug #1: Dead Code — Admin Debug Stages Unreachable

| Field | Value |
|-------|-------|
| **ID** | BFN-2026-001 |
| **Severity** | P0 — Critical |
| **Phase** | R-23 (Debug Stages) |
| **File** | `internal/daemon/auth.go:134–166`, `internal/daemon/handlers.go:610` |
| **Spec Reference** | Tech Spec Section 7.3 |
| **Discovered By** | Codebase audit + staticcheck |

**Description:**
The `requireDataToken` middleware rejected admin tokens with HTTP 401 `wrong_token_class` before the request reached `handleQuery`. The `isAdminToken(r)` check inside `handleQuery` was dead code — it could never return `true` because the middleware already blocked admin tokens.

This violated the spec contract: "`?debug_stages=true` + admin token → `_nexus.debug` present."

**Root Cause:**
Phase 0C designed `requireDataToken` to strictly separate token classes (admin vs. data). Phase R-23 added debug_stages support that required admin tokens on the query endpoint, but did not update the middleware to allow this flow.

**Fix:**
- Modified `requireDataToken` to pass admin tokens through with a `ctxIsAdmin` context flag (no source set).
- `handleQuery` checks `isAdminFromContext(ctx)` and synthesises a permissive `_admin` source for admin queries.
- Write handlers (`handleWrite`, `handleOpenAIWrite`) explicitly reject admin tokens with 401 to preserve write-path security.
- Removed dead `isAdminToken` method, replaced with `isAdminFromContext` context helper.
- Updated `TestAuth_AdminTokenOnDataEndpoint` test to expect 200 (pass-through) instead of 401.

**Verification:**
- Admin token + `?debug_stages=true` on `/query/{dest}` now returns `_nexus.debug` in response.
- Data token + `?debug_stages=true` returns normal response without debug (silently ignored).
- Admin token on `/inbound/{source}` returns 401 `wrong_token_class`.
- All existing auth tests pass.

---

## Bug #2: Nil Context Passed to slog.LogAttrs

| Field | Value |
|-------|-------|
| **ID** | BFN-2026-002 |
| **Severity** | P2 — Medium |
| **Phase** | R-1 (WAL HMAC), R-3 (Config Signing), R-9 (WAL Watchdog) |
| **File** | `internal/daemon/daemon.go:204, 280, 760` |
| **Lint Rule** | SA1012 (staticcheck) |

**Description:**
Three `d.logger.LogAttrs(nil, slog.LevelWarn, ...)` calls passed `nil` as the context argument. While `slog` accepts nil contexts, `staticcheck` flags this as SA1012: "do not pass a nil Context, even if a function permits it."

**Fix:**
Replaced `nil` with `context.Background()` in all three call sites.

---

## Bug #3: Unused Struct Field in Event Sink

| Field | Value |
|-------|-------|
| **ID** | BFN-2026-003 |
| **Severity** | P2 — Medium |
| **Phase** | R-19 (Event Sink) |
| **File** | `internal/eventsink/eventsink.go:77` |
| **Lint Rule** | U1000 (staticcheck) |

**Description:**
The `Sink` struct declared a `client *http.Client` field that was never used. The `send()` method created a new `http.Client` inline with a per-request timeout instead of using the struct field.

**Fix:**
Removed the unused `client` field from the `Sink` struct.

---

## Bug #4: Unchecked rows.Close() in Conflict Query

| Field | Value |
|-------|-------|
| **ID** | BFN-2026-004 |
| **Severity** | P1 — High |
| **Phase** | R-22 (Conflict Inspector) |
| **File** | `internal/destination/sqlite.go:670, 678` |
| **Lint Rule** | errcheck (golangci-lint) |

**Description:**
The `QueryConflicts` method called `detailRows.Close()` without checking the error return value in two locations. One was in an error path (early return) and the other at the end of a loop iteration. Additionally, `defer` was not used because the query was inside a loop, making resource cleanup fragile.

**Root Cause:**
Inline query within a for-loop prevented clean `defer` usage. The detail query and scan logic was interleaved with the loop body.

**Fix:**
Extracted the detail query into a dedicated `scanConflictDetail()` helper method. The helper uses `defer func() { _ = rows.Close() }()` for deterministic cleanup and properly checks `rows.Err()` after iteration.

---

## Bug #5: Deprecated ECDSA Key Field Access in JWT Tests

| Field | Value |
|-------|-------|
| **ID** | BFN-2026-005 |
| **Severity** | P2 — Medium |
| **Phase** | R-20 (OAuth Edge Templates) |
| **File** | `internal/jwtauth/jwtauth_test.go:105-106` |
| **Lint Rule** | SA1019 (staticcheck) |

**Description:**
The `ecJWKS` test helper accessed `key.X.Bytes()` and `key.Y.Bytes()` directly on `*ecdsa.PublicKey`. These fields were deprecated in Go 1.26 (SA1019) in favor of the `crypto/ecdh` package.

**Fix:**
Replaced with `key.ECDH()` conversion followed by `ecdhKey.Bytes()` to extract the uncompressed point bytes (04 || x || y), then split into x and y components by byte length.

---

## Bug #6: Unchecked Error Returns in Test Code

| Field | Value |
|-------|-------|
| **ID** | BFN-2026-006 |
| **Severity** | P3 — Low |
| **Phase** | R-22 (Conflict Inspector) |
| **File** | `internal/destination/sqlite_test.go:289, 352` |
| **Lint Rule** | errcheck (golangci-lint) |

**Description:**
Two `d.Write(p)` calls in test functions did not check the error return value. While unlikely to fail in test scenarios, unchecked errors can mask test setup failures.

**Fix:**
Wrapped both calls with `if err := d.Write(p); err != nil { t.Fatalf(...) }`.

---

## Summary

| ID | Severity | Phase | Category | Status |
|----|----------|-------|----------|--------|
| BFN-2026-001 | P0 | R-23 | Dead code / spec violation | Fixed |
| BFN-2026-002 | P2 | R-1/R-3/R-9 | Nil context | Fixed |
| BFN-2026-003 | P2 | R-19 | Unused field | Fixed |
| BFN-2026-004 | P1 | R-22 | Unchecked I/O error | Fixed |
| BFN-2026-005 | P2 | R-20 | Deprecated API | Fixed |
| BFN-2026-006 | P3 | R-22 | Test quality | Fixed |
| BFN-2026-007 | P1 | R-9 | Windows rename + WAL batch perf | Fixed |

**Pre-existing issues not addressed (out of scope):**
- `internal/daemon/phase9_test.go:486,507` — unused test helper functions (`multiSourceDaemon`, `retryAfterValue`)
- `internal/mcp/server.go:79` — unused type `initializeParams`
- Various unchecked `Close()` returns in pre-existing test files (embedding, wal, mcp, web)
- `internal/hotreload/watcher_test.go:197` — ineffectual assignment (pre-existing)

---

## Bug #7: Load Test Fails on Windows — WAL Segment Rename Access Denied

| Field | Value |
|-------|-------|
| **ID** | BFN-2026-007 |
| **Severity** | P1 — High |
| **Phase** | R-9 (Phase 9 Load Test) |
| **File** | `internal/daemon/phase9_test.go:177-212` |
| **Test** | `TestPhase9_LoadTest_1000ConcurrentWrites` |
| **Discovered By** | Quality gate during Phase R-33 (2026-04-06) |

**Description:**
`TestPhase9_LoadTest_1000ConcurrentWrites` consistently fails on Windows. All 1000 HTTP writes return 200, but only ~585/1000 payloads drain to SQLite within the 60-second timeout. The queue's `MarkDelivered` fails because the WAL segment rename is blocked by a Windows file lock.

**Error:**
```
wal: rename temp to segment: rename ...\wal-1744422495.tmp ...\wal-1775508803909762300.jsonl: Access is denied.
```

**Root Cause:**
Windows enforces mandatory file locking. When another handle (WAL reader, scanner, or fsync) holds the `.tmp` file open, `os.Rename` fails with `Access is denied`. On Linux/macOS, rename succeeds even with open handles because POSIX allows unlinking open files.

**Impact:**
- Test-only. The daemon's write path succeeds (HTTP 200). The queue delivery acknowledgment fails, leaving payloads stuck in the WAL. In production this would cause duplicate deliveries on restart (safe due to idempotency) but not data loss.
- All 1000 writes are accepted and persisted to the WAL. The failure is in the post-delivery WAL segment rotation.

**Fix:**
Created `internal/fsutil` package with `RobustRename` — wraps `os.Rename` with retry on transient Windows file-locking errors (`ERROR_ACCESS_DENIED` errno 5, `ERROR_SHARING_VIOLATION` errno 32). 5 attempts with exponential backoff (5ms, 10ms, 20ms, 40ms, 80ms = 155ms worst case). On POSIX, `isRetryableRenameErr` returns false so first failure propagates immediately with zero added latency.

Applied `fsutil.RobustRename` to all 6 `os.Rename` call sites in the codebase:
- `internal/wal/updater.go:214` — WAL segment rewrite (primary bug site)
- `internal/audit/logger.go:427,432` — Audit log rotation (primary + shadow)
- `internal/signing/signing.go:84` — Signature sidecar write
- `internal/hotreload/watcher.go:336` — Compiled JSON atomic write
- `internal/policy/compile.go:90` — Policy compilation output

Platform-specific error classification via build tags (`rename_windows.go`, `rename_other.go`).

**Performance Fix (same bug):**
The rename retry eliminated `Access is denied` errors, but 1000 entries still couldn't drain in 60s due to O(N²) WAL rewrite cost: each `MarkDelivered` scanned and rewrote the full segment file individually.

Added `MarkDeliveredBatch(payloadIDs []string) error` to the `WALUpdater` interface and implemented `markStatusBatch` / `markStatusBatchInSegment` that marks all entries in a single segment rewrite pass (O(N) instead of O(N²)). Queue worker refactored to accumulate delivered IDs and flush via `MarkDeliveredBatch` every 100ms or 50 entries.

Result: 1000-entry load test dropped from 90–100s (timeout) to ~10s. All 30 packages pass with zero failures and zero race reports.

**Status:** Fixed.
