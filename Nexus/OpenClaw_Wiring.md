# OpenClaw Wiring Changes — 2026-04-21

Changes made to OpenClaw files in WSL2 (`//wsl.localhost/Ubuntu/home/shawn/.openclaw/`)
to enable bidirectional A2A communication with Nexus.

---

## 1. `openclaw.json` — env section

**File:** `~/.openclaw/openclaw.json`

Added two new environment variables:

```json
"env": {
  "NEXUS_URL": "http://localhost:8080",
  "NEXUS_DATA_KEY": "bfn_data_REPLACE_WITH_YOUR_DATA_KEY",
  "NEXUS_ADMIN_KEY": "bfn_admin_REDACTED4f154216b48bc89be09821de1fbe488d1df16b12f2ea339a9c8de750b1",   // NEW
  "NEXUS_SOURCE": "default",
  "OPENCLAW_AGENT_ID": "agt_01KP7MN6P9VXPH5GB1BBZ6Q4WF"   // NEW
}
```

**Why:**
- `NEXUS_ADMIN_KEY`: The Nexus admin bearer token. OpenClaw's self-registration was
  sending `A2A_SHARED_SECRET` as the admin bearer, but Nexus's `requireAdminToken`
  middleware expects the actual admin token (`bfn_admin_...`). These are different
  secrets — `A2A_SHARED_SECRET` is for agent-to-agent transport auth, not admin API access.
- `OPENCLAW_AGENT_ID`: OpenClaw's registered agent ID in Nexus's registry. Used by the
  `nexus_invoke` tool as the `X-Agent-ID` header value so Nexus can identify the calling
  agent for governance checks.

---

## 2. `extensions/a2a-receiver/index.js` — registration auth fix

**File:** `~/.openclaw/extensions/a2a-receiver/index.js`

**Change:** `registerWithNexus()` function (line ~182-207)

Before:
```js
const secret = process.env.A2A_SHARED_SECRET;
if (!nexusUrl || !secret) {
  warn("[a2a-receiver] skipping nexus self-registration: NEXUS_URL or A2A_SHARED_SECRET missing");
  return;
}
// ...
Authorization: `Bearer ${secret}`,
```

After:
```js
const adminKey = process.env.NEXUS_ADMIN_KEY || process.env.A2A_SHARED_SECRET;
if (!nexusUrl || !adminKey) {
  warn("[a2a-receiver] skipping nexus self-registration: NEXUS_URL and (NEXUS_ADMIN_KEY or A2A_SHARED_SECRET) required");
  return;
}
// ...
Authorization: `Bearer ${adminKey}`,
```

**Why:** The `/a2a/admin/register-agent` endpoint on Nexus is behind `requireAdminToken`
middleware, which checks `cfg.ResolvedAdminKey`. The old code sent `A2A_SHARED_SECRET`
(`e1f0396039...`) but Nexus expected `bfn_admin_fd5a...` — returning 401.
Now uses `NEXUS_ADMIN_KEY` (correct admin token) with `A2A_SHARED_SECRET` as fallback.

---

## 3. `extensions/bubblefish-nexus/index.js` — new `nexus_invoke` tool

**File:** `~/.openclaw/extensions/bubblefish-nexus/index.js`

**Change:** Added `nexus_invoke` tool registration after `nexus_status` (inserted before
the final `api.logger?.info` call). Updated tool count in log message from 3 to 4.

**Replaced `nexus_invoke` with full A2A tool suite (10 tools total):**

| Tool | Purpose |
|------|---------|
| `nexus_write` | Write memories (enhanced description) |
| `nexus_search` | Search memories (enhanced description) |
| `nexus_status` | Daemon health check |
| `a2a_list_agents` | List all registered agents + status |
| `a2a_describe_agent` | Get detailed agent info by name or ID |
| `a2a_send_to_agent` | Send message to agent, get response (primary A2A tool) |
| `a2a_get_task` | Poll task status by ID |
| `a2a_cancel_task` | Cancel a running task |
| `a2a_list_grants` | List governance grants |
| `a2a_list_pending_approvals` | List pending approval requests |

**Behavior:**
1. Builds a JSON-RPC 2.0 request: `method: "agent/invoke"`, params: `{targetAgentId, message: {role: "user", parts: [{type: "text", text}]}}`
2. POSTs to `${NEXUS_URL}/a2a/jsonrpc`
3. Sets `X-Agent-ID: ${OPENCLAW_AGENT_ID}` header (no bearer token — the `/a2a/jsonrpc` endpoint uses method-level auth, not admin-token middleware)
4. Parses the JSON-RPC response; extracts text from artifacts if present
5. Returns formatted result to the user

**Why:** Enables OpenClaw to route tasks to any agent registered in Nexus's registry.
Combined with the `X-Agent-ID` context injection added to Nexus's `handleA2AJSONRPC`
(WIRE.10), this completes the bidirectional communication path:
- Nexus → OpenClaw: via `tasks/send` to `http://localhost:18789/a2a` (existing)
- OpenClaw → Nexus → OtherAgent: via `nexus_invoke` → `agent/invoke` JSON-RPC (new)

---

## Auth Model (final state)

| Direction | Token Used | Header |
|-----------|-----------|--------|
| Nexus → OpenClaw (tasks/send) | `A2A_SHARED_SECRET` | `Authorization: Bearer` |
| OpenClaw → Nexus self-register | `NEXUS_ADMIN_KEY` | `Authorization: Bearer` |
| OpenClaw → Nexus memory ops | `NEXUS_DATA_KEY` | `Authorization: Bearer` |
| OpenClaw → Nexus agent/invoke | (none) | `X-Agent-ID: agt_...` |

---

## Activation

Restart OpenClaw gateway in WSL2 to reload plugins and fire self-registration:
```bash
# In WSL2:
openclaw restart
# or kill and restart the gateway process
```

Then verify:
1. Check OpenClaw logs for `[a2a-receiver] nexus self-registration succeeded`
2. On Windows: `curl -H "Authorization: Bearer <admin_token>" http://localhost:8081/api/a2a/agents` — OpenClaw should appear
3. In OpenClaw, use `nexus_invoke` tool to test routing to another agent

## Known Startup Behaviors

- **Repeated `register()` calls**: OpenClaw's plugin loader calls `register(api)` many
  times during discovery (~40+). This is normal OpenClaw behavior. Routes use
  `replaceExisting: true` so duplicates are no-ops. Self-registration fires at most once
  per process (`_nexusRegistrationScheduled` guard).
- **404 on self-registration**: If Nexus isn't running when OpenClaw boots, the
  `registerWithNexus()` call gets 404 and logs it as harmless. Start Nexus first, then
  restart OpenClaw for self-registration to succeed.
