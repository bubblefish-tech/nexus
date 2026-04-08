# BubbleFish Nexus — Dashboard Contract v1

This document is the **canonical contract** between the Nexus daemon and the
dashboard front-end (`nexus-dashboard-v4.html`).

The dashboard ships with `CONFIG.MOCK_MODE = true` by default. To go live:

```js
CONFIG.MOCK_MODE = false;
CONFIG.BASE_URL  = 'http://127.0.0.1:8081';
CONFIG.ADMIN_TOKEN = '<your admin token>';
```

When `MOCK_MODE` is false, the dashboard will hit the daemon directly. **Every
endpoint listed here must return a JSON body that matches the documented shape
exactly.** If a field is missing, the corresponding UI cell will be blank or
undefined. If a type is wrong (string vs number), the format helpers will throw.

The mock layer in the dashboard implements every shape below. To verify the
daemon is contract-compliant, set `MOCK_MODE = false` and confirm every panel
populates identically to mock mode.

---

## Authentication

All endpoints below are **admin-class** per Tech Spec §12. The dashboard sends:

```
Authorization: Bearer <admin_token>
Accept: application/json
```

For SSE, `EventSource` cannot send headers; the dashboard sends the token as a
query parameter:

```
GET /api/viz/events?token=<admin_token>
```

The daemon must accept the token in either location for SSE endpoints, and
**only** in the `Authorization` header for non-SSE endpoints.

Cross-class rejection (data token used on admin endpoint, admin token used on
data endpoint) must return **401** with the standard error envelope:

```json
{ "error": "invalid_token_class", "message": "admin token required" }
```

---

## Standard Error Envelope

All non-2xx responses must use:

```json
{
  "error": "rate_limit_exceeded",
  "message": "too many requests",
  "retry_after_seconds": 30,
  "details": {}
}
```

The dashboard surfaces non-2xx responses by setting an offline banner and
falling back to mock data so the UI never goes blank.

---

## GET /api/status

**Auth:** admin
**Polling:** every `CONFIG.REFRESH_MS` (5000 ms by default; matches Tech Spec
§10.1 "auto-refreshes every 5 seconds").

**Response 200:**

```json
{
  "version": "0.1.0",
  "uptime_seconds": 15734,
  "pid": 18472,
  "bind": "127.0.0.1:8080",
  "web_port": 8081,
  "memory_resident_bytes": 88293376,
  "goroutines": 47,
  "queue_depth": 0,
  "wal": {
    "pending_entries": 0,
    "healthy": true,
    "last_checkpoint_seconds_ago": 862,
    "integrity_mode": "crc32",
    "current_segment": "wal-00042.log"
  },
  "consistency_score": 0.98,
  "destinations": [
    { "name": "sqlite", "healthy": true, "last_error": null }
  ],
  "cache": {
    "hit_rate":      0.67,
    "exact_rate":    0.42,
    "semantic_rate": 0.25
  },
  "sources_total": 3,
  "memories_total": 1247
}
```

**Field notes:**

- `uptime_seconds` — integer seconds, monotonic from process start. Renders as
  `Xh Ym` via `fmt.uptime`.
- `memory_resident_bytes` — RSS in bytes. Renders via `fmt.bytes` (KB/MB/GB).
- `wal.integrity_mode` — one of `"crc32"` or `"mac"` (Tech Spec §2.2.3).
- `wal.healthy` — boolean. If `false`, the dashboard's "wal" terminal command
  shows `DEGRADED` and the WAL Pending metric card flips to amber.
- `consistency_score` — float 0.0–1.0. Tech Spec §11.5 says <0.95 is WARN,
  <0.80 is ERROR. The dashboard does not currently color-code this; future
  work.
- `destinations[]` — at minimum the active destination(s). For dev: `sqlite`.
  For home-lab: `postgres`. For air-gapped: `sqlite`.
- `cache.hit_rate` — overall hit rate across both Stage 1 (exact) and Stage 2
  (semantic). Sum of `exact_rate + semantic_rate` plus the implicit miss
  rate must equal 1.0.
- `sources_total` — count of currently configured (not "online") sources.
- `memories_total` — count of memories in the active destination at query
  time. This is the number rendered in the "Total Memories" metric card and
  the Witness Mode HUD.

**Wired UI elements:**
- `#mTotal` (Total Memories metric card)
- `#mWal` (WAL Pending metric card)
- 4th metric card (Cache Hit Rate) — value, sub-label, fill bar
- `#uptime` (header)
- `#wCount` (Witness HUD memory counter)
- Nexus console: `status`, `wal`, `memories`, `pipeline` commands

---

## GET /api/cache

**Auth:** admin
**Polling:** every 5 s (control view + pipeline view)

**Response 200:**

```json
{
  "exact": {
    "hits":     8421,
    "misses":   1842,
    "hit_rate": 0.821
  },
  "semantic": {
    "hits":     5012,
    "misses":   1425,
    "hit_rate": 0.778
  },
  "misses_total":    3267,
  "evictions_total": 142,
  "capacity": 10000,
  "used":      8192,
  "watermark": "8192 / 10000"
}
```

**Field notes:**

- `exact.hits` / `exact.misses` are Stage 1 cache (Tech Spec §11.3 metric:
  `bubblefish_cache_exact_hits_total` / `bubblefish_cache_exact_misses_total`).
- `semantic.hits` / `semantic.misses` are Stage 2.
- `hit_rate` per cache layer is `hits / (hits + misses)`.
- `misses_total` is the count of queries that fell through both cache layers.
- `evictions_total` — LRU evictions since startup.
- `capacity` — configured max cache entries.
- `used` — current entry count.
- `watermark` — pre-formatted string `"used / capacity"` for direct rendering.

**Wired UI elements:**
- `#pipeCacheExact`, `#pipeCacheSem`, `#pipeCacheMiss`, `#pipeCacheEvict`,
  `#pipeCacheWater` (pipeline view cache stats card)
- Nexus console: `pipeline` command

---

## GET /api/policies

**Auth:** admin
**Polling:** every 5 s (control view + security view)

Returns the **compiled** policy summary per source. The dashboard does not
edit policies — for that, the operator edits TOML and runs `bubblefish build`.

**Response 200:**

```json
{
  "sources": [
    {
      "source": "claude-desktop",
      "can_read": true,
      "can_write": true,
      "allowed_destinations": ["sqlite"],
      "max_results": 20,
      "max_response_bytes": 16384,
      "rate_limit_per_min": 2000,
      "policy_hash": "a3f7c2d4"
    },
    {
      "source": "open-webui",
      "can_read": true,
      "can_write": true,
      "allowed_destinations": ["sqlite"],
      "max_results": 50,
      "max_response_bytes": 32768,
      "rate_limit_per_min": 2000,
      "policy_hash": "b8e1d9f1"
    }
  ]
}
```

**Field notes:**

- `source` — the source ID matching the per-source TOML filename.
- `policy_hash` — hash of the compiled policy. The dashboard renders the
  first 6 characters as a stable identifier. Used to detect policy drift.
- `allowed_destinations[]` — array of destination names this source can write
  to.

**Wired UI elements:**
- Source cards on the control plane (3 cards + the "+ Connect Source" placeholder)
- `#policyTableBody` (security view policies table)
- Nexus console: `sources`, `policy` commands

---

## GET /api/security/summary

**Auth:** admin
**Polling:** every 5 s (security view) + audit view (for filtered count)

Aggregated counters from `/metrics`. The daemon should compute these by
scanning the relevant Prometheus counter values; the response is a stable JSON
shape so the dashboard does not have to parse the Prometheus text format.

**Response 200:**

```json
{
  "auth_failures_total":   3,
  "policy_denials_total":  17,
  "rate_limit_hits_total": 0,
  "admin_calls_total":     142,
  "by_source": {
    "claude-desktop": { "auth_failures": 0, "policy_denials": 0,  "rate_limit_hits": 0 },
    "open-webui":     { "auth_failures": 2, "policy_denials": 2,  "rate_limit_hits": 0 },
    "cursor":         { "auth_failures": 1, "policy_denials": 0,  "rate_limit_hits": 0 }
  }
}
```

**Source metrics:** `bubblefish_auth_failures_total`,
`bubblefish_policy_denials_total`, `bubblefish_rate_limit_hits_total`,
`bubblefish_admin_calls_total` (Tech Spec §11.3).

**Wired UI elements:**
- `#secAuthFails`, `#secPolicyDenies`, `#secRateLimits`, `#secAdminCalls` (security view)
- `#audStatFiltered` (audit view stats card)
- Nexus console: `security` command

---

## GET /api/security/events

**Auth:** admin
**Polling:** every 5 s (security view)

Recent structured security events. Tech Spec §9.3 says these come from a
dedicated JSON Lines log; this endpoint should return the most recent N
entries from that log (suggested default: 50).

**Response 200:**

```json
{
  "events": [
    {
      "ts":       "2026-04-06T22:10:14.832Z",
      "kind":     "policy_denial",
      "source":   "open-webui",
      "severity": "warn",
      "message":  "subject:user:shawn labels:[pii] denied by pii-filter"
    },
    {
      "ts":       "2026-04-06T21:54:32.119Z",
      "kind":     "auth_failure",
      "source":   "cursor",
      "severity": "warn",
      "message":  "401 invalid bearer token (data-class on admin endpoint)"
    }
  ]
}
```

**Field notes:**

- `ts` — RFC3339 timestamp.
- `kind` — one of `auth_failure`, `policy_denial`, `rate_limit`, `wal_tamper`,
  `config_signature_failure`, `admin_access`.
- `severity` — one of `info`, `warn`, `error`.
- Newest events first.

**Wired UI elements:**
- `#secEventsList` (security view recent events panel)

---

## GET /api/lint

**Auth:** admin
**Polling:** every 5 s (security view)

Config lint results, equivalent to running `bubblefish lint` against the
currently loaded config (Tech Spec §13.1).

**Response 200:**

```json
{
  "warnings": [
    {
      "severity": "warn",
      "code":     "tls.disabled",
      "message":  "daemon.tls.enabled = false; OK for localhost binding only",
      "file":     "daemon.toml",
      "line":     14
    },
    {
      "severity": "info",
      "code":     "wal.encryption.off",
      "message":  "WAL encryption disabled; OK for non-sensitive data",
      "file":     "daemon.toml",
      "line":     22
    }
  ]
}
```

**Field notes:**

- `severity` — `info`, `warn`, or `error`. Color-coded in UI.
- `code` — short stable identifier (e.g. `tls.disabled`). Should be a stable
  enum the operator can suppress in config.
- `file` / `line` — source location of the offending config line, relative to
  the Nexus config root.

**Wired UI elements:**
- `#lintList` (security view config lint panel)
- Nexus console: `lint` command

---

## GET /api/conflicts

**Auth:** admin
**Polling:** on view mount + on filter change (no auto-refresh)

Tech Spec §13.2 "Conflict Inspector" — read-only view identifying
contradictory memories: multiple entries for the same `subject + entity` with
different content.

**Query parameters:**

| name | type | default | description |
|---|---|---|---|
| `source` | string | (all) | Filter by source ID |
| `actor_type` | string | (all) | `user`, `agent`, or `system` |
| `subject` | string | (all) | Substring match on subject namespace |
| `from` | RFC3339 | (none) | Lower time bound |
| `to` | RFC3339 | (none) | Upper time bound |
| `limit` | int | 50 | Max conflicts to return |

**Response 200:**

```json
{
  "conflicts": [
    {
      "id":         "cf_001",
      "subject":    "user:shawn",
      "entity":     "preferred_theme",
      "group_size": 2,
      "memories": [
        {
          "source":     "open-webui",
          "actor_type": "agent",
          "ts":         "2026-04-01T14:22:11Z",
          "content":    "User prefers light mode"
        },
        {
          "source":     "claude-desktop",
          "actor_type": "user",
          "ts":         "2026-04-04T09:11:48Z",
          "content":    "User prefers dark mode for all applications"
        }
      ]
    }
  ]
}
```

**Field notes:**

- `id` — stable opaque identifier for the conflict group.
- `subject` — the subject namespace.
- `entity` — the inferred entity key. Phase R-22 build plan says: "if content
  contains 'is' or '=' patterns, extract the left side as entity_key.
  Fallback: use content hash prefix."
- `group_size` — count of divergent memories in this group (always ≥ 2).
- `memories[]` — each divergent memory, newest first within the group.

**Wired UI elements:**
- `#conflictList` (conflicts view main list)
- `#conflictCount` (conflicts view filter pill)
- Nexus console: `conflicts` command

---

## GET /api/viz/events  (Server-Sent Events)

**Auth:** admin (via `?token=` query param — see Authentication section)
**Connection:** long-lived. The dashboard opens **one** SSE connection on boot
and fans events out to all views via the `bus`.

Tech Spec §10.3 "Live Pipeline Visualization" + Build Plan Phase R-21.

**Frame format:**

Standard SSE frames. Each frame is a single JSON object on one `data:` line,
followed by a blank line:

```
data: {"ts":"2026-04-06T22:11:43.821Z","request_id":"req_a8f2c91d","source":"claude-desktop","op":"QUERY","subject":"user:shawn","actor_type":"user","status":"ALLOWED","labels":[],"result_count":7,"total_ms":4.82,"stages":[{"stage":0,"name":"policy","ms":0.12,"hit":false},{"stage":1,"name":"cache","ms":0.24,"hit":true},{"stage":2,"name":"semantic","ms":1.18,"hit":false},{"stage":3,"name":"lookup","ms":2.41,"hit":false},{"stage":4,"name":"vector","ms":5.07,"hit":false},{"stage":5,"name":"merge","ms":1.23,"hit":false}]}

```

**Frame fields:**

| field | type | required | notes |
|---|---|---|---|
| `ts` | RFC3339 string | yes | Event timestamp at the daemon |
| `request_id` | string | yes | Format: `req_<8 hex>`. Stable per-request ID for correlation with logs |
| `source` | string | yes | Source ID |
| `op` | `"WRITE"` \| `"QUERY"` | yes | Operation class |
| `subject` | string | yes | Subject namespace from the request |
| `actor_type` | `"user"` \| `"agent"` \| `"system"` | yes | Provenance from `X-Actor-Type` header |
| `status` | `"ALLOWED"` \| `"FILTERED"` \| `"DENIED"` | yes | Final policy decision |
| `labels` | string[] | yes | Empty array if none. `["pii"]` for filtered-by-pii |
| `result_count` | int | yes | For QUERY: number of results returned. For WRITE: 0 |
| `total_ms` | float | yes | End-to-end request duration in ms |
| `stages` | array \| null | yes | For QUERY: per-stage breakdown. For WRITE: `null` |

**`stages[]` element:**

```json
{ "stage": 0, "name": "policy", "ms": 0.12, "hit": false }
```

| field | type | notes |
|---|---|---|
| `stage` | int 0–5 | Cascade stage number |
| `name` | string | One of `policy`, `cache`, `semantic`, `lookup`, `vector`, `merge` |
| `ms` | float | Duration in ms for this stage |
| `hit` | bool | True if this stage **served** the result (request short-circuited here) |

**Stage semantics (Tech Spec §10.3):**

- Only **one** stage per request can have `hit: true`.
- If `cache` (stage 1) has `hit: true`, stages 2–5 should still be reported with
  `ms: 0` to keep array length consistent (recommended), OR the `stages` array
  may be truncated at the hit. The dashboard handles both.
- For WRITE operations, `stages` is `null`.

**Lossiness:** Per Build Plan Phase R-21, the daemon must use a non-blocking
send pattern for the viz channel:

```go
select { case vizChan <- event: default: dropped++ }
```

The dashboard tolerates dropped frames — they simply don't appear in the
audit log. Drop count should be exposed via
`bubblefish_visualization_events_dropped_total` Prometheus metric.

**Wired UI elements:**
- `#auditList` (control plane audit panel) — live tail
- `#auditTableBody` (audit view full table) — filtered live tail
- `#sseList` (pipeline view live event stream)
- `#witnessLogList` (Witness Mode bottom-left log panel)
- Pipeline node animation on both control plane and pipeline view
- Throughput counter on control plane (computed from sliding 1s window)

---

## Endpoints NOT currently consumed by the dashboard

These endpoints exist in the spec (Tech Spec §12) but are not yet wired:

- `POST /api/replay` — manual WAL replay trigger
- `GET /api/timetravel?as_of=` — historical state query
- `POST /api/demo/reliability` — reliability demo trigger
- `GET /metrics` — Prometheus text format (the dashboard uses
  `/api/security/summary` for derived counters instead)
- `GET /health`, `GET /ready` — health probes (used by load balancers, not the dashboard)

Future dashboard versions may consume these. The contract for them is owned
by Tech Spec §12 directly.

---

## Source ID conventions

Throughout the dashboard, source IDs are **kebab-case** and match the per-source
TOML filename (without extension):

- `claude-desktop`
- `open-webui`
- `cursor`

The dashboard hardcodes color assignments for these three source IDs (cyan,
green, amber) in the `srcDot()` function. Adding a new source means:

1. Drop a TOML file in `sources/`
2. Run `bubblefish build`
3. The source appears automatically in `/api/policies` and the dashboard
4. New sources get the default text-tertiary color until you add them to the
   `srcDot()` map. Future work: load source colors from `/api/policies`
   `display_color` field (not in current contract).

---

## Response time budgets

The dashboard polls these endpoints every 5 seconds. The daemon should
respond well under 100 ms for each, ideally under 25 ms. None of these
endpoints should ever be slow — they all read from in-memory state or short
SQLite queries.

| Endpoint | Target p99 | Hard ceiling |
|---|---|---|
| `/api/status` | 5 ms | 50 ms |
| `/api/cache` | 2 ms | 25 ms |
| `/api/policies` | 5 ms | 25 ms |
| `/api/security/summary` | 10 ms | 50 ms |
| `/api/security/events` | 10 ms | 100 ms |
| `/api/lint` | 50 ms | 250 ms |
| `/api/conflicts` | 100 ms | 500 ms |

If any endpoint exceeds its hard ceiling repeatedly, the dashboard's polling
will accumulate behind in-flight requests and visibly stutter. This is not
gracefully handled — fix the daemon side.

---

## Versioning

This is **contract v1**. Breaking changes to any documented field require
a contract version bump and a corresponding dashboard release. Additive
changes (new optional fields) do not require a version bump.

The dashboard tolerates extra unknown fields in responses; do not rely on
the daemon being strict about response shape.
