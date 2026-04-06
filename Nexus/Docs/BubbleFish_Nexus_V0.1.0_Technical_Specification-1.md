# BubbleFish Nexus v0.1.0 — Technical Specification Addendum #1
# AI Agent Audit & Memory Gateway: Interaction Log, Retrieval Firewall, Black Box Recorder
# Authoritative behavioral contract — extends the base V0.1.0 Technical Specification
# © 2026 BubbleFish Technologies, Inc. All rights reserved.
# License: AGPL-3.0-or-later

> **ADDENDUM NOTE:** This document extends the base V0.1.0 Technical Specification.
> All section numbers use the "A" prefix (A1, A2, etc.) to avoid collision with
> base spec sections 1–19. All behavioral contracts in this addendum have the same
> authority as those in the base spec. Claude Code MUST treat them as mandatory.

> **COPYRIGHT NOTE:** Every .go file created from this addendum MUST include the
> standard AGPL-3.0 copyright header as defined in the base State & Verification Guide.

---

## Quick Lookup Index (Addendum)

| Topic | Section |
|-------|---------|
| Strategic context and positioning | A1 |
| AI Interaction Log overview | A2.1 |
| interaction_record schema | A2.2 |
| Interaction log file format | A2.3 |
| Interaction log emission points | A2.4 |
| GET /api/audit/log endpoint | A2.5 |
| Audit log Prometheus metrics | A2.6 |
| Dashboard audit tab | A2.7 |
| Retrieval Firewall overview | A3.1 |
| sensitivity_label on memories | A3.2 |
| Classification tiers | A3.3 |
| blocked_labels in source policy | A3.4 |
| Retrieval Firewall enforcement points | A3.5 |
| Namespace isolation enhancement | A3.6 |
| Retrieval Firewall security events | A3.7 |
| Retrieval Firewall metrics | A3.8 |
| Config reference (daemon.toml additions) | A4.1 |
| Config reference (source TOML additions) | A4.2 |
| Destination schema additions | A4.3 |
| New CLI commands | A5 |
| New HTTP API endpoints | A6 |
| V3 readiness (addendum features) | A7 |
| Glossary additions | A8 |

---

## Section A1 — Strategic Context

<!-- Section A1 — Strategic Context -->

This addendum transforms Nexus from a memory daemon into an **AI Agent Audit & Memory Gateway** by adding three integrated capabilities:

1. **AI Interaction Log (Black Box Recorder)** — A tamper-evident, queryable audit trail of every agent interaction with memory. Every write and every retrieval generates a structured interaction record.

2. **Retrieval Firewall** — Policy-governed access control at the retrieval level using sensitivity labels, classification tiers, blocked-label enforcement, and namespace isolation.

3. **Audit Query API** — A stable REST endpoint for querying the interaction log by actor, source, time range, namespace, operation type, and policy outcome.

These features are architecturally integrated with the existing WAL, provenance, policy engine, and retrieval cascade. They share the same durability guarantees (append-only, CRC32/HMAC-protected, fsync'd) and the same failure semantics (never block hot paths, fail closed on policy ambiguity).

**Design philosophy addition:** The audit log is a durable record of what happened. It is not a cache, not a convenience layer, and not optional in production. If Nexus saw an interaction, the interaction log recorded it.

---

## Section A2 — AI Interaction Log (Black Box Recorder)

<!-- Section A2 — AI Interaction Log (Black Box Recorder) -->

### A2.1 Overview

Every write operation and every retrieval operation through Nexus generates an **interaction record**. These records are appended to a dedicated, append-only interaction log file with the same durability guarantees as the WAL (CRC32 on every entry, optional HMAC, fsync after write). The interaction log is the "black box recorder" for AI agents — it answers: who interacted, what operation, what data was touched, what policies applied, and what the outcome was.

The interaction log is a **separate file** from the WAL. The WAL records payload data for crash recovery. The interaction log records operational metadata for audit, compliance, and forensics. They share durability primitives but serve different purposes.

### A2.2 interaction_record Schema

<!-- Section A2.2 — interaction_record Schema -->

Every interaction generates exactly one record with this schema:

```go
type InteractionRecord struct {
    // Identity
    RecordID        string    `json:"record_id"`        // UUID via crypto/rand, unique per interaction
    RequestID       string    `json:"request_id"`       // Correlation ID from HTTP ingress
    Timestamp       time.Time `json:"timestamp"`        // RFC3339Nano, when the interaction started

    // Actor
    Source          string    `json:"source"`           // Source name from config
    ActorType       string    `json:"actor_type"`       // user, agent, or system
    ActorID         string    `json:"actor_id"`         // Identity of the actor
    EffectiveIP     string    `json:"effective_ip"`     // Client IP (from trusted proxy or TCP source)

    // Operation
    OperationType   string    `json:"operation_type"`   // write, query, admin
    Endpoint        string    `json:"endpoint"`         // e.g. /inbound/claude, /query/sqlite
    HTTPMethod      string    `json:"http_method"`      // GET, POST
    HTTPStatusCode  int       `json:"http_status_code"` // Response status

    // Write-specific (empty for reads)
    PayloadID       string    `json:"payload_id,omitempty"`
    Destination     string    `json:"destination,omitempty"`
    Subject         string    `json:"subject,omitempty"`
    IdempotencyKey  string    `json:"idempotency_key,omitempty"`
    IsDuplicate     bool      `json:"is_duplicate,omitempty"`
    SensitivityLabelsSet []string `json:"sensitivity_labels_set,omitempty"` // Labels assigned on write

    // Read-specific (empty for writes)
    RetrievalProfile string   `json:"retrieval_profile,omitempty"`
    StagesHit       []string  `json:"stages_hit,omitempty"`
    ResultCount     int       `json:"result_count,omitempty"`
    CacheHit        bool      `json:"cache_hit,omitempty"`

    // Policy
    PolicyDecision  string    `json:"policy_decision"`  // allowed, denied, filtered
    PolicyReason    string    `json:"policy_reason,omitempty"`
    SensitivityLabelsFiltered []string `json:"sensitivity_labels_filtered,omitempty"`
    TierFiltered    bool      `json:"tier_filtered,omitempty"`

    // Performance
    LatencyMs       float64   `json:"latency_ms"`
    WALAppendMs     float64   `json:"wal_append_ms,omitempty"`

    // Integrity
    CRC32           string    `json:"crc32"` // CRC32 of JSON record (computed with crc32 field empty)
}
```

**Field contracts:**

- `record_id`: Generated via `crypto/rand` UUID. MUST be unique. NEVER sequential or predictable.
- `request_id`: Same UUID assigned at HTTP ingress. Enables correlation with structured logs and security events.
- `timestamp`: `time.Now().UTC()` at the start of request processing. NEVER the time of log emission.
- `policy_decision`: One of `"allowed"`, `"denied"`, `"filtered"`. `"filtered"` means results returned but some removed by retrieval firewall.
- `sensitivity_labels_filtered`: Only populated when retrieval firewall removes results.
- `crc32`: Computed over JSON bytes with `crc32` field set to empty string. Uses `crc32.ChecksumIEEE`.

### A2.3 Interaction Log File Format

<!-- Section A2.3 — Interaction Log File Format -->

File location: `~/.bubblefish/Nexus/logs/interactions.jsonl`

Configurable via `[daemon.audit]` in daemon.toml.

Format: One JSON object per line, followed by tab, followed by CRC32 hex, followed by newline. Identical layout to unencrypted WAL entries:

```
JSON_BYTES<TAB>CRC32_HEX<NEWLINE>
```

When `[daemon.wal.integrity] mode = "mac"`, the interaction log also includes HMAC:

```
JSON_BYTES<TAB>CRC32_HEX<TAB>HMAC_HEX<NEWLINE>
```

Uses same HMAC key as WAL (`mac_key_file`). Single key management story.

**Durability guarantees:**
- Every record written with `fsync` before HTTP response is sent.
- File opened with `O_APPEND | O_WRONLY | O_CREATE`, permissions `0600`.
- Directory permissions `0700`.

**Rotation:** Rotates when exceeding `max_file_size_mb` (default 100MB). Rotated files named `interactions-YYYYMMDD-HHMMSS.jsonl`. Old files never deleted by Nexus — operator responsibility.

> **⚠ WARNING:** Interaction log contains IP addresses and actor identifiers. GDPR-sensitive deployments must configure appropriate retention.

> **INVARIANT:** An interaction record MUST be emitted for every HTTP request reaching the auth layer, regardless of outcome. Denied requests generate records with `policy_decision: "denied"`. Non-negotiable for audit completeness.

### A2.4 Interaction Log Emission Points

<!-- Section A2.4 — Interaction Log Emission Points -->

Records emitted at END of request processing, after HTTP status determined, before response written.

**Write path emission (extends Tech Spec Section 3.2):**

After step 19, before step 20:

| **Step** | **Operation** | **Notes** |
|----------|--------------|-----------|
| 19a | Interaction record emitted | Append to interaction log. CRC32 + optional HMAC. fsync. |

**Read path emission (extends Tech Spec Section 3.3):**

After step 14, before step 15:

| **Step** | **Operation** | **Notes** |
|----------|--------------|-----------|
| 14a | Interaction record emitted | Append to interaction log. CRC32 + optional HMAC. fsync. |

**Denied request emission:** For 401, 403, 429, 413 — record emitted immediately after denial decision, before error response written.

**Performance contract:** At most 2ms p99 added (dominated by fsync). If log file unwritable, request still succeeds — failure logged as WARN, `bubblefish_audit_log_errors_total` incremented.

> **INVARIANT:** Interaction log write failure MUST NOT cause request failure. Audit is critical but gateway function takes priority.

### A2.5 GET /api/audit/log Endpoint

<!-- Section A2.5 — GET /api/audit/log Endpoint -->

**Auth:** Admin token required. Data-plane tokens receive 401 `wrong_token_class`.

**Method:** GET

**Path:** `/api/audit/log`

**Query parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| source | string | (all) | Filter by source name |
| actor_type | string | (all) | Filter: user, agent, system |
| actor_id | string | (all) | Filter by specific actor |
| operation | string | (all) | Filter: write, query, admin |
| policy_decision | string | (all) | Filter: allowed, denied, filtered |
| subject | string | (all) | Filter by subject namespace |
| destination | string | (all) | Filter by destination |
| after | RFC3339 | (none) | Records after this timestamp |
| before | RFC3339 | (none) | Records before this timestamp |
| limit | int | 100 | Max records (1–1000) |
| offset | int | 0 | Pagination offset |

**Response:**

```json
{
  "records": [ /* interaction_record objects */ ],
  "total_matching": 1234,
  "limit": 100,
  "offset": 0,
  "has_more": true
}
```

**Implementation:** File-scan with in-memory filtering for v0.1.0. Acceptable for logs up to ~1GB. Indexed store is v3.

**Rate limiting:** 60 requests/minute per admin token (configurable).

### A2.6 Audit Log Prometheus Metrics

<!-- Section A2.6 — Audit Log Prometheus Metrics -->

| **Metric** | **Type** | **Description** |
|------------|----------|-----------------|
| bubblefish_audit_records_total | Counter | Records written. Labels: operation_type, policy_decision |
| bubblefish_audit_log_errors_total | Counter | Log write failures |
| bubblefish_audit_log_bytes | Gauge | Current log file size |
| bubblefish_audit_log_rotation_total | Counter | Log rotations |
| bubblefish_audit_query_latency_seconds | Histogram | /api/audit/log query latency |
| bubblefish_retrieval_firewall_filtered_total | Counter | Memories filtered. Labels: source, label |
| bubblefish_retrieval_firewall_denied_total | Counter | Queries fully denied. Labels: source |

### A2.7 Dashboard Audit Tab

<!-- Section A2.7 — Dashboard Audit Tab -->

New "Audit" tab on web dashboard:

- **Recent interactions:** Last 50 records, auto-refreshing. Sortable by timestamp, source, operation, policy decision.
- **Per-agent timeline:** Select actor_id, see all interactions chronologically.
- **Policy denial feed:** Real-time denied/filtered interactions.
- **Statistics:** Interactions/hour (write vs read), denial rate, firewall filter rate.

Dashboard uses `/api/audit/log`. `textContent` only — NEVER `innerHTML`.

---

## Section A3 — Retrieval Firewall

<!-- Section A3 — Retrieval Firewall -->

### A3.1 Overview

The Retrieval Firewall is a policy-governed access control layer operating at the retrieval level — between the cascade's data fetch and projection. It determines which memories a source/actor can see based on sensitivity labels, classification tiers, and namespace rules.

The Retrieval Firewall is NOT content-inspection (that is v1.0 AI Firewall). It operates on metadata: labels, tiers, namespaces, source identity. Deterministic, fast, auditable.

**Pipeline position:**

- **Pre-query:** Sub-stage within Stage 0 (Policy Gate) for access-level decisions.
- **Post-retrieval:** Filter after Stage 5 (Hybrid Merge) for result-level filtering.

### A3.2 sensitivity_label on Memories

<!-- Section A3.2 — sensitivity_label on Memories -->

TranslatedPayload (Tech Spec Section 7.1) extended:

```go
type TranslatedPayload struct {
    // ... all existing fields ...

    // Addendum A3: Retrieval Firewall
    SensitivityLabels  []string  // e.g. ["pii", "financial", "medical", "legal-hold"]
    ClassificationTier string    // e.g. "public", "internal", "confidential", "restricted"
}
```

**Label semantics:**

- `SensitivityLabels`: Free-form string array. Operators define domain-appropriate labels. Examples: `"pii"`, `"financial"`, `"medical"`, `"hr"`, `"legal-hold"`, `"executive"`, `"trade-secret"`.
- `ClassificationTier`: Single string from configurable ordered list. Default order: `"public"`, `"internal"`, `"confidential"`, `"restricted"`.
- Both optional. Absent = `"public"` tier, no labels.

**Setting on write via headers:**

- `X-Sensitivity-Labels`: Comma-separated. Example: `X-Sensitivity-Labels: pii,financial`
- `X-Classification-Tier`: Single value. Example: `X-Classification-Tier: confidential`

MCP: `sensitivity_labels` and `classification_tier` as tool input parameters.

**Storage:** WAL entry, destination columns, interaction log record.

**Schema migration:**

SQLite: `ALTER TABLE memories ADD COLUMN sensitivity_labels TEXT DEFAULT '';` and `ALTER TABLE memories ADD COLUMN classification_tier TEXT DEFAULT 'public';` with index on `classification_tier`.

Postgres: `ALTER TABLE memories ADD COLUMN sensitivity_labels TEXT[] DEFAULT '{}';` and `ALTER TABLE memories ADD COLUMN classification_tier TEXT DEFAULT 'public';` with GIN index on labels, B-tree on tier.

### A3.3 Classification Tiers

<!-- Section A3.3 — Classification Tiers -->

Hierarchical access model. Source's `max_classification_tier` defines highest retrievable tier.

```toml
[daemon.retrieval_firewall]
enabled = true
tier_order = ["public", "internal", "confidential", "restricted"]
default_tier = "public"
```

Comparison by index in `tier_order`. Lower index = less sensitive. Source sees own index or lower. Unknown tiers = maximally restricted (denied).

### A3.4 blocked_labels in Source Policy

<!-- Section A3.4 — blocked_labels in Source Policy -->

Source TOML `[source.policy]` extended:

```toml
[source.policy.retrieval_firewall]
blocked_labels = ["executive", "trade-secret"]
max_classification_tier = "internal"
required_labels = []
default_classification_tier = "public"
visible_namespaces = []
cross_namespace_read = false
```

**Enforcement (AND conditions):**

1. **blocked_labels:** Memory has ANY blocked label → removed. No exceptions.
2. **max_classification_tier:** Memory tier exceeds source max → removed.
3. **required_labels:** If non-empty, only memories with ALL required labels returned.

**Default:** If `[source.policy.retrieval_firewall]` absent → all memories visible (backward compatible).

### A3.5 Retrieval Firewall Enforcement Points

<!-- Section A3.5 — Retrieval Firewall Enforcement Points -->

**Pre-query (Stage 0 enhancement):**

1. Source has `max_classification_tier` → verify query not requesting above maximum. Exceeds → 403 `retrieval_firewall_denied`, reason `tier_exceeds_maximum`.
2. Source has `required_labels` → validate subject namespace against label requirements.

**Post-retrieval (after Stage 5, before Projection):**

For each memory in result set:
1. Check `sensitivity_labels` against `blocked_labels`. Remove if any match.
2. Check `classification_tier` against `max_classification_tier`. Remove if exceeds.
3. Check `sensitivity_labels` against `required_labels`. Remove if required not present.
4. Record removed memories in interaction record `sensitivity_labels_filtered`.
5. If ALL removed → `policy_decision: "filtered"`, return empty with `_nexus.retrieval_firewall_filtered: true`.
6. Update `_nexus.result_count` to post-filter count.

**Performance:** At most 0.1ms per result. Metadata only — no content inspection.

> **INVARIANT:** Retrieval firewall MUST execute after retrieval, before projection. NOT bypassable by query params, profiles, or debug flags. Even admin tokens respect it.

> **INVARIANT:** blocked_labels are absolute. No override, no escape hatch. If label is in blocked_labels, memory is invisible. Period.

### A3.6 Namespace Isolation Enhancement

<!-- Section A3.6 — Namespace Isolation Enhancement -->

```toml
[source.policy.retrieval_firewall]
visible_namespaces = ["shared", "claude"]  # Source can ONLY see these namespaces
cross_namespace_read = false                # Allow reading any namespace
```

- `visible_namespaces` empty (default) → existing behavior.
- `visible_namespaces` set → source ONLY sees those namespaces.
- `cross_namespace_read = true` → read any namespace (admin/audit sources).

### A3.7 Retrieval Firewall Security Events

<!-- Section A3.7 — Retrieval Firewall Security Events -->

Extends Tech Spec Section 11.2:

- **retrieval_firewall_filtered:** Fields: source, subject, labels_blocked, tier_blocked, count_filtered, count_remaining.
- **retrieval_firewall_denied:** Fields: source, subject, reason, requested_tier, max_tier.

Written to both security events log AND interaction log.

### A3.8 Retrieval Firewall Metrics

See A2.6. Additional:

| **Metric** | **Type** | **Description** |
|------------|----------|-----------------|
| bubblefish_retrieval_firewall_latency_seconds | Histogram | Firewall filtering time. Labels: source |

---

## Section A4 — Configuration Additions

<!-- Section A4 — Configuration Additions -->

### A4.1 daemon.toml Additions

```toml
[daemon.audit]
enabled = true
log_file = "~/.bubblefish/Nexus/logs/interactions.jsonl"
max_file_size_mb = 100
admin_rate_limit_per_minute = 60

[daemon.retrieval_firewall]
enabled = false
tier_order = ["public", "internal", "confidential", "restricted"]
default_tier = "public"
```

### A4.2 Source TOML Additions

```toml
[source.policy.retrieval_firewall]
blocked_labels = []
max_classification_tier = "restricted"
required_labels = []
default_classification_tier = "public"
visible_namespaces = []
cross_namespace_read = false
```

### A4.3 Destination Schema Additions

SQLite:
```sql
ALTER TABLE memories ADD COLUMN sensitivity_labels TEXT DEFAULT '';
ALTER TABLE memories ADD COLUMN classification_tier TEXT DEFAULT 'public';
CREATE INDEX idx_memories_classification ON memories(classification_tier);
```

PostgreSQL:
```sql
ALTER TABLE memories ADD COLUMN sensitivity_labels TEXT[] DEFAULT '{}';
ALTER TABLE memories ADD COLUMN classification_tier TEXT DEFAULT 'public';
CREATE INDEX idx_memories_classification ON memories(classification_tier);
CREATE INDEX idx_memories_sensitivity ON memories USING GIN(sensitivity_labels);
```

---

## Section A5 — New CLI Commands

| **Command** | **Description** |
|-------------|-----------------|
| `bubblefish audit tail` | Stream interaction log to stdout. --source, --actor, --operation filters. |
| `bubblefish audit query` | Query interaction log. Same params as GET /api/audit/log. JSON output. |
| `bubblefish audit stats` | Summary: interactions/hour, denial rate, top sources/actors. |
| `bubblefish audit export` | Export to CSV or JSON. --after, --before filters. |

---

## Section A6 — New HTTP API Endpoints

| **Endpoint** | **Method** | **Auth** | **Description** |
|--------------|-----------|----------|-----------------|
| `/api/audit/log` | GET | Admin | Query interaction log (Section A2.5) |
| `/api/audit/stats` | GET | Admin | Summary statistics |
| `/api/audit/export` | GET | Admin | Export as JSON array or CSV |

---

## Section A7 — V3 Architecture Readiness

### A7.1 Indexed Audit Store
V3 may add indexed backing store for sub-second queries over millions of records.
*V0.1.0 readiness:* Schema stable. API contract stable. File format directly importable.

### A7.2 Compliance Retention Policies
V3 may add auto-purge, legal hold markers, GDPR right-to-erasure.
*V0.1.0 readiness:* Records include actor_id. File rotation provides basic lifecycle.

### A7.3 Full AI Firewall
V3/v1.0 may add prompt/response content inspection.
*V0.1.0 readiness:* Policy engine extensible. Enforcement points are same locations content inspection would hook.

### A7.4 Edge/SD-WAN Multi-Nexus Replication
V3/v1.0+ may add encrypted interaction log replication between Nexus instances.
*V0.1.0 readiness:* Append-only JSONL with integrity protection. Directly replicable.

---

## Section A8 — Glossary Additions

| **Term** | **Definition** |
|----------|---------------|
| Interaction Record | Structured audit entry for one HTTP request. Schema: Section A2.2. |
| Interaction Log | Append-only JSONL of interaction records. The Black Box Recorder. |
| Black Box Recorder | Positioning name for the AI Interaction Log. |
| Retrieval Firewall | Policy-governed retrieval access control via labels, tiers, namespaces. |
| Sensitivity Label | Free-form string tag on memory indicating sensitivity (e.g. "pii"). |
| Classification Tier | Hierarchical level: public < internal < confidential < restricted. |
| blocked_labels | Source policy: labels source can NEVER see. |
| max_classification_tier | Source policy: highest tier source can access. |
| AI Agent Audit & Memory Gateway | Enterprise product positioning for Nexus. |

---

# END OF TECHNICAL SPECIFICATION ADDENDUM #1
