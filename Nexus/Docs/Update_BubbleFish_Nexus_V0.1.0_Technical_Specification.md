# BubbleFish Nexus v0.1.0 — Technical Specification Update #1
# Hardening: Interaction Log Durability, Encryption, Dual-File Write
# Extends Technical Specification Addendum #1 (Sections A2, A4)
# © 2026 BubbleFish Technologies, Inc. All rights reserved.
# License: AGPL-3.0-or-later

> **UPDATE NOTE:** This document modifies specific sections of Tech Spec Addendum #1.
> Where this update conflicts with the addendum, THIS UPDATE takes precedence.
> Apply these changes to the R-29 implementation. No new phases required — these
> fold into the existing R-29 (Interaction Log Engine) phase.

---

## Quick Lookup Index (Update)

| Topic | Section | Modifies |
|-------|---------|----------|
| Separate HMAC key for interaction log | U1.1 | Addendum A2.3, A4.1 |
| Optional AES-256-GCM encryption on interaction log | U1.2 | Addendum A2.3, A4.1 |
| Dual-file write (primary + shadow) | U1.3 | Addendum A2.3 |
| Crash-safe segment rotation with dual-segment replay | U1.4 | Addendum A2.3 |
| Rotation marker record | U1.5 | Addendum A2.3 |
| Updated daemon.toml config | U1.6 | Addendum A4.1 |
| Updated Prometheus metrics | U1.7 | Addendum A2.6 |

---

## Section U1.1 — Separate HMAC Key for Interaction Log

<!-- Section U1.1 — Separate HMAC Key -->

**Replaces:** Addendum A2.3 statement "Uses same HMAC key as WAL (mac_key_file). Single key management story."

**New behavior:** The interaction log has its OWN HMAC key, separate from the WAL HMAC key. This ensures that compromising one key does not compromise the other, and that keys can be rotated independently.

```toml
[daemon.audit.integrity]
mode = "crc32"                   # crc32 (default) or mac
mac_key_file = ""                # Separate 32-byte HMAC-SHA256 key for interaction log
```

When `mode = "mac"`:
- Interaction log entries use format: `JSON_BYTES<TAB>CRC32_HEX<TAB>HMAC_HEX<NEWLINE>`
- HMAC computed using the key from `[daemon.audit.integrity] mac_key_file`
- This key is INDEPENDENT of `[daemon.wal.integrity] mac_key_file`
- If `mode = "mac"` but `mac_key_file` is missing/empty, daemon MUST refuse to start (same fail-fast as WAL)

When `mode = "crc32"` (default):
- Interaction log entries use format: `JSON_BYTES<TAB>CRC32_HEX<NEWLINE>`
- No HMAC computed. CRC32 provides corruption detection only.

> **INVARIANT:** The interaction log HMAC key MUST be different from the WAL HMAC key. If an operator configures both to the same file path, `bubblefish lint` emits a WARNING (not an error — it's technically functional but reduces security).

---

## Section U1.2 — Optional AES-256-GCM Encryption on Interaction Log

<!-- Section U1.2 — AES-256-GCM Encryption -->

**Adds to:** Addendum A2.3

The interaction log supports optional at-rest encryption using the same AES-256-GCM scheme as the WAL (Tech Spec Section 6.4.2), with its own independent key.

```toml
[daemon.audit.encryption]
enabled = false
key_file = ""                    # Separate 32-byte AES-256 key for interaction log
```

**Encrypted entry layout:** Identical to WAL encrypted layout (Tech Spec Section 4.1):

| **Component** | **Size** | **Description** |
|---------------|----------|-----------------|
| version | 1 byte | Audit log encryption format version (currently 1) |
| key_id | 4 bytes | Identifier for the encryption key (currently 0x00000001) |
| nonce | 12 bytes | AES-256-GCM nonce. Unique per entry. Generated via crypto/rand. |
| ciphertext | variable | AES-256-GCM encrypted JSON record with authentication tag |
| crc32 | 4 bytes | CRC32 over (version + key_id + nonce + ciphertext). Written as 8-char hex after tab. |

**Order of operations when both HMAC and encryption are enabled:**
1. Compute HMAC over plaintext JSON
2. Encrypt (plaintext JSON + HMAC) with AES-256-GCM and fresh nonce
3. Compute CRC32 over encrypted form
4. Write: `encrypted_bytes<TAB>CRC32_HEX<NEWLINE>`

**On read:**
1. Validate CRC32 over encrypted bytes
2. Decrypt with AES-256-GCM
3. Validate HMAC over decrypted JSON
4. Parse JSON

**Fail-fast:** If encryption enabled but `key_file` missing/empty/unreadable/wrong size, daemon MUST refuse to start.

> **INVARIANT:** The interaction log encryption key MUST be different from the WAL encryption key. Same lint warning as HMAC keys.

---

## Section U1.3 — Dual-File Write (Primary + Shadow)

<!-- Section U1.3 — Dual-File Write -->

**Adds to:** Addendum A2.3

Every interaction record is written to TWO files simultaneously:
- **Primary:** `~/.bubblefish/Nexus/logs/interactions.jsonl`
- **Shadow:** `~/.bubblefish/Nexus/logs/interactions-shadow.jsonl`

Both files use identical format (CRC32, optional HMAC, optional encryption). Both receive the same fsync call.

**Write sequence:**
1. Serialize interaction record to JSON bytes
2. Compute CRC32 (and HMAC, and encrypt, if configured)
3. Append formatted line to primary file
4. Append formatted line to shadow file
5. fsync primary file
6. fsync shadow file
7. Return success

If the primary write fails but shadow succeeds (or vice versa), log WARN and increment `bubblefish_audit_log_errors_total` with label `file=primary` or `file=shadow`. The request still succeeds.

**On read (AuditReader):**
1. Read entry from primary file
2. Validate CRC32
3. If CRC32 valid → use this entry
4. If CRC32 invalid → read same-position entry from shadow file
5. If shadow CRC32 valid → use shadow entry, log WARN about primary corruption
6. If both invalid → skip entry, log WARN, increment `bubblefish_audit_crc_failures_total`

**Rotation:** Both primary and shadow rotate together. Shadow file named `interactions-shadow-YYYYMMDD-HHMMSS.jsonl`.

**Disk overhead:** Exactly 2x the interaction log size. Interaction records are metadata-only (no memory content), so individual records are small (~500 bytes). At 1000 interactions/hour, that's ~1MB/hour or ~24MB/day for both files combined. Acceptable.

**Configuration:**

```toml
[daemon.audit]
dual_write = true                # Enable shadow file. Default: true.
```

When `dual_write = false`, only the primary file is written. Shadow file not created.

> **INVARIANT:** When dual_write is enabled, BOTH files MUST be written and BOTH MUST be fsync'd before the append is considered complete. If one filesystem is full and the other is not, the write is still attempted to both — partial success is acceptable and metered.

---

## Section U1.4 — Crash-Safe Segment Rotation with Dual-Segment Replay

<!-- Section U1.4 — Crash-Safe Rotation -->

**Replaces:** Addendum A2.3 statement "Rotation is atomic (rename + create)."

**New behavior:** Interaction log rotation uses the same crash-safe pattern as the WAL (Tech Spec Section 4.2):

**Rotation sequence:**
1. Write rotation marker record to current file (see U1.5)
2. fsync current file
3. Rename current file to `interactions-YYYYMMDD-HHMMSS.jsonl`
4. Create new `interactions.jsonl` (and shadow equivalents if dual_write enabled)
5. Resume writing to new file

**Crash during rotation:** If the process crashes between steps 3 and 4, on restart both the renamed file and the new (possibly empty or non-existent) file may exist. The reader handles this:

1. Discover all interaction log files via `filepath.Glob`
2. Sort by filename (oldest first)
3. Replay all files
4. Deduplicate by `record_id` (same as WAL deduplicates by idempotency key)

> **INVARIANT:** If both old and new interaction log segments exist after crash, BOTH are replayed. Entries are deduplicated by record_id. No records lost.

---

## Section U1.5 — Rotation Marker Record

<!-- Section U1.5 — Rotation Marker -->

**Adds to:** Addendum A2.3

Before rotation, a special interaction record is written with:

```json
{
  "record_id": "<uuid>",
  "timestamp": "<RFC3339Nano>",
  "operation_type": "rotation_marker",
  "policy_decision": "allowed",
  "latency_ms": 0,
  "crc32": "<computed>"
}
```

The `operation_type: "rotation_marker"` tells the reader that rotation was initiated. On startup, if the reader finds a rotation_marker without a subsequent file (crash between marker write and file creation), it knows rotation was interrupted and creates the new file.

The AuditReader MUST skip rotation_marker records when returning query results. They are internal bookkeeping, not user-visible.

The `/api/audit/log` endpoint MUST NOT return rotation_marker records.

---

## Section U1.6 — Updated daemon.toml Configuration

<!-- Section U1.6 — Updated Config -->

**Replaces:** Addendum A4.1 `[daemon.audit]` block.

```toml
# === AI Interaction Log (Black Box Recorder) — Hardened ===
[daemon.audit]
enabled = true
log_file = "~/.bubblefish/Nexus/logs/interactions.jsonl"
max_file_size_mb = 100
admin_rate_limit_per_minute = 60
dual_write = true                                         # Primary + shadow file

[daemon.audit.integrity]
mode = "crc32"                                            # crc32 or mac
mac_key_file = ""                                         # SEPARATE key from WAL HMAC

[daemon.audit.encryption]
enabled = false
key_file = ""                                             # SEPARATE key from WAL encryption
```

---

## Section U1.7 — Updated Prometheus Metrics

<!-- Section U1.7 — Updated Metrics -->

**Adds to:** Addendum A2.6:

| **Metric** | **Type** | **Description** |
|------------|----------|-----------------|
| bubblefish_audit_shadow_errors_total | Counter | Shadow file write failures |
| bubblefish_audit_crc_failures_total | Counter | Records with CRC32 mismatch (both primary and shadow corrupt) |
| bubblefish_audit_shadow_recoveries_total | Counter | Records recovered from shadow after primary corruption |

---

# END OF TECHNICAL SPECIFICATION UPDATE #1
