# HTTP API Reference

The Nexus HTTP API runs on port 8080 (configurable). Endpoints require bearer tokens with strict token class separation.

---

## Authentication

Three token classes, strictly separated:

| Token | Used for | Configured in |
|---|---|---|
| `bfn_data_` | Memory writes and reads via HTTP | Source TOML files |
| Admin token | Admin operations (status, reload, cache, security) | `daemon.toml` `admin_token` |
| `bfn_mcp_` | MCP tool calls on port 7474 | `daemon.toml` `[daemon.mcp]` |

Admin tokens are rejected on data endpoints. Data keys are rejected on admin endpoints. MCP keys are rejected on both. Cross-use returns `401 wrong_token_class`.

Include in the `Authorization` header:
```
Authorization: Bearer bfn_data_YOUR_KEY
```

---

## Endpoints

### Health (no auth)

```
GET /health
```

```json
{
  "status": "ok",
  "version": "0.1.2"
}
```

### Readiness (no auth)

```
GET /ready
```

Returns 200 when the daemon is ready to accept requests.

---

### Write a memory

```
POST /inbound/{source}
Authorization: Bearer bfn_data_...
Content-Type: application/json
```

**Request body:**
```json
{
  "message": {
    "content": "User prefers dark mode",
    "role": "user"
  },
  "model": "claude-4"
}
```

**Optional headers:**
- `X-Idempotency-Key` — deduplicate writes. Checked before rate limiting.
- `X-Actor-Type` — `user`, `agent`, or `system` (default from source config)
- `X-Actor-ID` — identifier for the writing client (default from source config)

**Response:**
```json
{
  "payload_id": "9cc2a55a30b8d300684a672c595b6eee",
  "status": "accepted"
}
```

`status: "accepted"` means the WAL commit succeeded. Destination write is asynchronous.

### OpenAI-compatible write

```
POST /v1/memories
Authorization: Bearer bfn_data_...
Content-Type: application/json
```

Same write path, OpenAI-compatible payload shape.

---

### Search memories

```
GET /query/{destination}
Authorization: Bearer bfn_data_...
```

**Query parameters:**
- `limit` — max results (default: 10, max: 200)
- `cursor` — pagination cursor from previous response
- `profile` — `fast`, `balanced`, or `deep`
- `actor_type` — filter by provenance
- `subject` — filter by subject tag

**Profiles:**
- `fast` — Stages 0, 1, 3 only. No vector search. Lowest latency.
- `balanced` — Full cascade (0, 1, 2, 3, 4, 5). Default.
- `deep` — Maximum recall. Skips exact cache. Highest latency.

**Response:**
```json
{
  "results": [
    {
      "payload_id": "...",
      "content": "User prefers dark mode",
      "actor_type": "user",
      "actor_id": "claude-desktop",
      "timestamp": "2026-04-08T20:07:30Z",
      "_nexus": {
        "retrieval_stage": 3,
        "latency_ms": 12
      }
    }
  ],
  "total_count": 47,
  "cursor": "eyJ0aW1l..."
}
```

Cursors are stable across writes.

**SSE streaming:** Add `Accept: text/event-stream` header for Server-Sent Events.

---

### Admin: status

```
GET /api/status
Authorization: Bearer ADMIN_TOKEN
```

Daemon health, queue depth, WAL pending entries, consistency score, destination status, cache hit ratios.

### Admin: cache stats

```
GET /api/cache
Authorization: Bearer ADMIN_TOKEN
```

### Admin: config lint

```
GET /api/lint
Authorization: Bearer ADMIN_TOKEN
```

### Admin: conflict detection

```
GET /api/conflicts
Authorization: Bearer ADMIN_TOKEN
```

Identifies contradictory memories: multiple entries for the same subject with different content.

### Admin: time-travel

```
GET /api/timetravel?as_of=2026-04-01T00:00:00Z
Authorization: Bearer ADMIN_TOKEN
```

Query memories as they existed at a specific point in time.

### Admin: live pipeline visualization

```
GET /api/viz/events
Authorization: Bearer ADMIN_TOKEN
Accept: text/event-stream
```

SSE stream of pipeline events for the dashboard.

### Admin: security events

```
GET /api/security/events
Authorization: Bearer ADMIN_TOKEN
```

Recent security events (auth failures, policy denials, tampering).

### Admin: reload config

```
POST /api/reload
Authorization: Bearer ADMIN_TOKEN
```

Reloads source configs without restarting the daemon.

**Response:**
```json
{
  "status": "reloaded",
  "changed_fields": ["destinations.sqlite.cache_size_kb"]
}
```

### Admin: shutdown

```
POST /api/shutdown
Authorization: Bearer ADMIN_TOKEN
```

Graceful shutdown. Returns 202, polls `/health` to confirm exit.

### Admin: reliability demo

```
POST /api/demo/reliability
Authorization: Bearer ADMIN_TOKEN
```

Triggers the built-in crash recovery demo.

### Metrics

```
GET /metrics
Authorization: Bearer ADMIN_TOKEN
```

Prometheus-format metrics. 30+ counters, histograms, and gauges.

---

## Error responses

All errors use a consistent shape:

```json
{
  "error": "rate_limit_exceeded",
  "message": "too many requests",
  "retry_after_seconds": 30
}
```

| Status | Meaning | Data Safe? |
|--------|---------|-----------|
| 200 | Write accepted or read returned results | Yes — payload is in WAL |
| 401 | Invalid credentials or wrong token class | No write attempted |
| 403 | Permission denied by policy | No write attempted |
| 413 | Payload too large | No write attempted |
| 429 (rate_limit) | Rate limit exceeded | Not written to WAL — retry |
| 429 (queue_full) | Queue full (load shed) | Yes — payload is in WAL |
| 500 | Internal error | Depends — check logs |
| 503 | Destination unavailable (circuit breaker) | Yes — WAL has the payload |

The key insight: **429 (queue_full) and 503 are safe.** Your data is in the WAL. Nexus will deliver it.

---

## Pagination

```bash
# First page
curl "http://localhost:8080/query/sqlite?limit=20" -H "Authorization: Bearer $KEY"

# Next page (cursor from previous response)
curl "http://localhost:8080/query/sqlite?limit=20&cursor=eyJ0aW1l..." -H "Authorization: Bearer $KEY"
```
