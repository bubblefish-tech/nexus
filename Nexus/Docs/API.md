# Agent Gateway API Reference

HTTP endpoints and MCP tools added by the Agent Gateway in v0.1.3.

## HTTP Endpoints

### Credential Proxy

These endpoints proxy requests to upstream AI providers using synthetic keys. Authentication is via `Authorization: Bearer <synthetic_key>`, not the admin token.

#### `POST /v1/chat/completions`

OpenAI-compatible chat completions proxy. Validates synthetic key, checks model allowlist, substitutes real provider key, proxies upstream. Supports streaming (`"stream": true`).

**Headers:**
- `Authorization: Bearer bfn_sk_...` (required)
- `X-Agent-ID: <agent_id>` (optional; required if `allowed_agents` is configured)

**Request body:** Standard OpenAI chat completion request.

**Responses:**
- `200` — proxied response from OpenAI
- `401` — invalid or missing synthetic key
- `403` — model not in allowlist, or agent not authorized
- `429` — rate limit exceeded (includes `Retry-After` header)
- `502` — upstream provider unavailable or configuration error

#### `POST /v1/messages`

Anthropic-compatible messages proxy. Same behavior as the OpenAI proxy but uses `x-api-key` header upstream and `anthropic-version` header.

### Agent Management (Admin Token Required)

#### `GET /api/agents/{agent_id}/sessions`

Returns active sessions for the specified agent.

#### `GET /api/agents/{agent_id}/activity?since=<RFC3339>&limit=<N>`

Returns recent activity events. `since` filters by timestamp, `limit` caps results (max 1000).

#### `POST /api/agents/{agent_id}/heartbeat`

Heartbeat ping. Updates last-seen time and health state.

## MCP Tools

All tools require MCP authentication (static key or OAuth JWT).

### `agent_broadcast`

Send a signal to other agents.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `type` | string | yes | Signal type identifier |
| `payload` | object | no | Signal payload |
| `persistent` | boolean | no | If true, written to WAL (survives restart) |
| `targets` | string[] | no | Target agent IDs. Empty = all active agents |

**Returns:** `{ "status": "sent", "sequence": <int> }`

### `agent_pull_signals`

Retrieve pending signals for the calling agent.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `max_n` | integer | no | Max signals to retrieve (default 100) |

**Returns:** `{ "signals": [...], "count": <int> }`

### `agent_status_query`

Query status of another registered agent.

**Parameters:**
| Name | Type | Required | Description |
|------|------|----------|-------------|
| `agent_id` | string | yes | Agent ID or name to query |

**Returns:** `{ "agent_id": "...", "status": "active", "last_seen_at": "...", "session_count": 0 }`

## Error Format

All error responses follow the standard Nexus format:

```json
{
  "error": "error_code",
  "message": "human-readable description"
}
```

Credential proxy errors never reveal the real provider API key or rate limit configuration.
