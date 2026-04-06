# BubbleFish Nexus v0.1.0 — State & Verification Guide Update #1
# Hardening: Updated R-29 Behavioral Contract for Interaction Log Durability
# Extends State & Verification Guide Addendum #1 (Phase R-29)
# © 2026 BubbleFish Technologies, Inc. All rights reserved.
# License: AGPL-3.0-or-later
# For: Claude Code (Agentic Coding Tool)

> **UPDATE NOTE:** This document REPLACES the R-29 behavioral contract and
> verification gate in State & Verification Guide Addendum #1. When executing
> Phase R-29, Claude Code MUST use THIS version, not the original addendum version.
> No new phases are added. R-29 duration increases to 3–4 days.

---

═══════════════════════════════════════════════════════
## PHASE R-29: INTERACTION LOG ENGINE (HARDENED)
═══════════════════════════════════════════════════════

SPEC AUTHORITY: Tech Spec Addendum Section A2 + Tech Spec Update #1 (all U1 sections)
PERSONA: Senior Backend / Storage Engineer
DURATION: 3–4 days

─── OBJECTIVE ───

Create the AI Interaction Log engine with flight-recorder-grade durability: CRC32 on every entry, optional independent HMAC key, optional independent AES-256-GCM encryption, dual-file write (primary + shadow), crash-safe segment rotation with dual-segment replay, and rotation marker records. This is the Black Box Recorder — it must survive crash, power failure, disk corruption, and forensic investigation.

─── REQUIRED EXPERT MINDSET ───

Senior Backend / Storage Engineer. You built the WAL in Phase 0A. Now you're building its sibling — same durability religion, same fsync discipline, same "what happens if power fails between this line and the next" paranoia. But this time you're also building: dual-file redundancy (primary + shadow), a separate crypto key domain, and crash-safe rotation with deduplication. Every record must be recoverable from at least one of two files. Every rotation must be atomic or recoverable.

─── STRICT END-STATE ───

Files to CREATE:
- internal/audit/record.go — InteractionRecord struct (Tech Spec Addendum A2.2)
- internal/audit/logger.go — AuditLogger: dual-write Append, Rotate with marker, Close
- internal/audit/crypto.go — HMAC and encryption for audit log (separate from WAL crypto)
- internal/audit/reader.go — AuditReader: Read with primary→shadow fallback, Filter, Count, dedup by record_id
- internal/audit/logger_test.go — Durability, CRC32, HMAC, encryption, dual-write, rotation, crash recovery, concurrent writes
- internal/audit/reader_test.go — Filtering, pagination, CRC32 validation, shadow fallback, rotation marker skipping, cross-segment dedup

─── BEHAVIORAL CONTRACT ───

**Core (from Addendum):**
1. InteractionRecord struct matches Tech Spec Addendum A2.2 exactly. Reference: Addendum A2.2.
2. Log file format: `JSON_BYTES<TAB>CRC32_HEX<NEWLINE>` (default). Reference: Addendum A2.3.
3. Every Append call does fsync on ALL written files before returning. Reference: Addendum A2.3.
4. File opened with `O_APPEND | O_WRONLY | O_CREATE`, permissions `0600`. Directory `0700`. Reference: Addendum A2.3.
5. Append failure MUST NOT panic or cause request failure. Return error; caller logs WARN and increments metric. Reference: Addendum A2.4.
6. record_id generated via crypto/rand UUID. MUST be unique. Reference: Addendum A2.2.
7. AuditReader validates CRC32 on each entry. Reference: Addendum A2.5.
8. AuditReader supports all filter parameters from Addendum A2.5. Reference: Addendum A2.5.
9. AuditReader reads ALL rotated log files via glob, sorted oldest-first. Reference: Addendum A2.3.
10. Concurrent Append calls are safe (mutex-protected). Reference: Global Directives.

**Hardening (from Update):**
11. Interaction log has its OWN HMAC key via `[daemon.audit.integrity] mac_key_file`. SEPARATE from WAL key. When mode=mac and key missing → daemon refuses to start. Reference: Update U1.1.
12. Interaction log has its OWN AES-256-GCM encryption key via `[daemon.audit.encryption] key_file`. SEPARATE from WAL key. When enabled and key missing → daemon refuses to start. Reference: Update U1.2.
13. Encryption order: HMAC over plaintext → encrypt (plaintext + HMAC) → CRC over ciphertext. On read: CRC → decrypt → HMAC → parse. Reference: Update U1.2.
14. When dual_write=true (default), every record written to BOTH primary file and shadow file. BOTH fsync'd. Reference: Update U1.3.
15. AuditReader: read from primary. If CRC invalid → read from shadow. If both invalid → skip, log WARN, increment `bubblefish_audit_crc_failures_total`. Reference: Update U1.3.
16. Shadow recovery: when reader uses shadow instead of primary, increment `bubblefish_audit_shadow_recoveries_total`. Reference: Update U1.7.
17. Rotation writes a rotation_marker record before renaming. Reference: Update U1.5.
18. Rotation_marker has `operation_type: "rotation_marker"`. AuditReader MUST skip these in query results. Reference: Update U1.5.
19. Crash during rotation: if both old and new segments exist on startup, both replayed, deduplicated by record_id. Reference: Update U1.4.
20. Shadow files rotate alongside primary files. Shadow named `interactions-shadow-YYYYMMDD-HHMMSS.jsonl`. Reference: Update U1.3.
21. If one file write fails but other succeeds, log WARN with file label, increment `bubblefish_audit_log_errors_total{file=primary}` or `{file=shadow}`. Request still succeeds. Reference: Update U1.3.
22. `bubblefish lint` warns if audit HMAC key path equals WAL HMAC key path. Reference: Update U1.1.
23. `bubblefish lint` warns if audit encryption key path equals WAL encryption key path. Reference: Update U1.2.

─── INVARIANTS ───

1. NEVER: Write interaction records without CRC32.
2. NEVER: Skip fsync after append (on ANY file — primary or shadow).
3. NEVER: Block the hot path on audit log failure.
4. NEVER: Log secret values in interaction records.
5. NEVER: Include memory content in interaction records. Only metadata.
6. NEVER: Use sequential or predictable record_id values.
7. NEVER: Use the WAL's HMAC key for the interaction log. Separate keys.
8. NEVER: Use the WAL's encryption key for the interaction log. Separate keys.
9. NEVER: Return rotation_marker records in API or CLI query results.
10. NEVER: Lose records during rotation. If crash mid-rotation, both segments replayed.

─── SECURITY CHECKPOINT ───

1. All files: 0600 permissions. Directories: 0700.
2. HMAC key is SEPARATE from WAL — compromise of one does not compromise other.
3. Encryption key is SEPARATE from WAL — same isolation principle.
4. Encrypted records use unique 12-byte nonce per entry via crypto/rand.
5. IP addresses in records are GDPR-relevant. Document in code comments.

─── IMPLEMENTATION DIRECTIVES ───

- USE: Same CRC32 pattern as WAL (`crc32.ChecksumIEEE`)
- USE: Same HMAC pattern as WAL Phase R-1 (`crypto/hmac`, `crypto/sha256`)
- USE: Same encryption pattern as WAL Phase R-2 (`crypto/aes`, `crypto/cipher` GCM)
- USE: `sync.Mutex` protecting file handles, rotation, and dual-write coordination
- USE: `crypto/rand` for both record_id UUID and encryption nonces
- USE: `filepath.Glob` for segment discovery across rotated files
- USE: `sort.Strings` for deterministic replay order
- REUSE: WAL crypto utility functions where possible (import from internal/wal or extract to shared internal/crypto package)
- AVOID: Sharing key instances between WAL and audit logger — separate key loading, separate key storage
- AVOID: Buffered writes — every Append is direct write + fsync
- EDGE CASE: Primary file deleted while daemon running → next Append recreates primary, shadow unaffected
- EDGE CASE: Shadow file deleted → next Append recreates shadow, primary unaffected
- EDGE CASE: Both files deleted → next Append recreates both
- EDGE CASE: Disk full → Append returns error for both files, caller handles gracefully
- EDGE CASE: Rotation with dual_write → four file operations: rename primary, rename shadow, create primary, create shadow. If crash between any pair, reader handles via glob + dedup.

─── VERIFICATION GATE ───

Compilation:
```
go build ./...
go vet ./...
CGO_ENABLED=1 go test ./... -race -count=1
```

Behavioral Verification:
- [ ] Write 100 interaction records. All have CRC32 after tab.
- [ ] Corrupt one primary record (flip byte). Reader recovers from shadow. `shadow_recoveries_total` incremented.
- [ ] Corrupt BOTH primary and shadow for same record. Reader skips it with WARN. `crc_failures_total` incremented.
- [ ] Rotation triggers at configured size. New file created. Old file renamed. Rotation marker present.
- [ ] Crash mid-rotation (both segments exist). Restart replays both, zero duplicates (dedup by record_id).
- [ ] Rotation marker records NOT returned by AuditReader query.
- [ ] Concurrent writes from 50 goroutines — zero data corruption, zero race reports.
- [ ] Append failure (unwritable file) — returns error, does NOT panic.
- [ ] HMAC mode: records have CRC32 + HMAC. HMAC key is NOT the WAL key (verify different paths).
- [ ] Encryption mode: records are encrypted. Encryption key is NOT the WAL key.
- [ ] Encryption + HMAC: order is HMAC→encrypt→CRC. Read order is CRC→decrypt→HMAC.
- [ ] dual_write=true: both primary and shadow files exist with identical record count.
- [ ] dual_write=false: only primary file exists. No shadow.
- [ ] Primary write fails, shadow succeeds: WARN logged with label file=primary. Request succeeds.
- [ ] Shadow write fails, primary succeeds: WARN logged with label file=shadow. Request succeeds.
- [ ] Reader reads across multiple rotated files in chronological order.
- [ ] record_id is unique across 1000 records.
- [ ] No memory content in any interaction record.
- [ ] File permissions are 0600.
- [ ] Daemon refuses to start with audit integrity=mac and missing mac_key_file.
- [ ] Daemon refuses to start with audit encryption=true and missing key_file.
- [ ] `bubblefish lint` warns when audit and WAL HMAC key paths are identical.
- [ ] `bubblefish lint` warns when audit and WAL encryption key paths are identical.

─── COMMIT ───
```
git add -A
git commit -s -m "Phase R-29: Interaction Log Engine (Black Box Recorder — Hardened)"
```

═══════════════════════════════════════════════════════

## UPDATED R-35: ADDENDUM SHIP CHECKLIST ADDITIONS

Add these items to the R-35 ship checklist (in addition to all existing items):

Interaction Log Hardening:
- [ ] Audit HMAC key is separate from WAL HMAC key
- [ ] Audit encryption key is separate from WAL encryption key
- [ ] Dual-write: both primary and shadow files written and fsync'd
- [ ] Shadow fallback: corrupted primary record recovered from shadow
- [ ] Crash mid-rotation: both segments replayed, zero duplicates
- [ ] Rotation marker records filtered from query results
- [ ] Encryption round-trip: write encrypted, read decrypted, CRC valid
- [ ] `bubblefish lint` warns on shared key paths

═══════════════════════════════════════════════════════
# END OF STATE & VERIFICATION GUIDE UPDATE #1
═══════════════════════════════════════════════════════
