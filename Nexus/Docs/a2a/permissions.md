# Nexus A2A Permissions

Nexus A2A uses a capability-scope grant model. Every task dispatched between
agents is governed by grants that specify which source agent may invoke which
capabilities on which target agent.

## Capability Vocabulary

Capabilities use a dotted prefix notation. The following prefixes are reserved
and have default policies:

| Prefix | Meaning | Default Policy |
|--------|---------|----------------|
| `memory.read` | Read stored memory, embeddings, retrieval | auto-allow |
| `memory.write` | Store new memories, update existing | approve-once-per-session |
| `memory.delete` | Delete memories | always-approve, always-audit |
| `fs.read` | Read files on host | approve-once-per-path |
| `fs.write` | Write or append files on host | always-approve, always-audit |
| `fs.delete` | Delete files on host | always-approve, always-audit |
| `shell.exec` | Execute shell commands | always-approve, always-audit |
| `net.fetch` | HTTP GET outbound | approve-once-per-domain |
| `net.post` | HTTP POST outbound | approve-once-per-domain |
| `messaging.send:<channel>` | Send a message on a named channel | approve-once-per-channel |
| `messaging.read:<channel>` | Read messages from a named channel | approve-once-per-channel |
| `messaging.delete:<channel>` | Delete messages on a named channel | always-approve, always-audit |
| `media.generate:<kind>` | Generate image, audio, video, music | approve-once-per-kind |
| `browser.navigate` | Drive a headless browser | always-approve, always-audit |
| `agent.invoke:<agent_id>` | Invoke another A2A agent through this gateway | approve-once-per-target |
| `system.info` | Read host metadata (OS, CPU, memory stats) | auto-allow |
| `system.run` | Run arbitrary system-level operations | always-approve, always-audit |

Custom capabilities outside these prefixes follow the `approve-once-per-session`
default policy unless overridden.

## Policy Types

- **auto-allow** -- task dispatches immediately. An audit entry is written but
  no human approval is needed.
- **approve-once-per-X** -- raises an approval prompt in the web UI the first
  time. Once approved, a cached grant is created scoped to X (session, path,
  domain, channel, kind, or target agent). Subsequent matching tasks dispatch
  without prompting.
- **always-approve** -- raises an approval prompt every invocation regardless
  of prior grants. Used for destructive or high-risk operations.
- **always-approve, always-audit** -- same as always-approve, plus the audit
  entry is flagged for compliance review.

## Grant Structure

A grant authorizes a specific `(source, target, capability)` triple:

```toml
[[a2a.grants]]
source = "client_claude_desktop"
target = "openclaw"
capability = "messaging.send:signal"
decision = "allow"
# expires = "24h"  # optional TTL
```

### Glob Patterns

Capability globs use `*` as a wildcard:

- `messaging.send:*` -- matches all messaging channels
- `fs.*` -- matches `fs.read`, `fs.write`, `fs.delete`
- `*` -- matches everything (use with extreme caution)

### Grant Precedence

When multiple grants match a request:

1. **Explicit deny wins** -- a deny grant on `test.echo` overrides an allow on `test.*`
2. **More specific wins** -- `messaging.send:signal` takes precedence over `messaging.send:*`
3. **Deny > allow** -- when specificity is equal, deny wins

## Customizing Defaults

Override default policies in `~/.nexus/Nexus/a2a/policy.toml`:

```toml
[defaults]
"memory.write" = "auto-allow"        # relax memory writes
"shell.exec"   = "deny"              # block shell exec entirely
"net.fetch"    = "approve-once-per-session"
```

Per-(source, target) overrides are configured via the web UI or CLI:

```bash
nexus a2a grant add \
  --source client_claude_desktop \
  --target openclaw \
  --capability "messaging.send:*"
```

## The ALL Grant

The ALL grant (`scope = "ALL"`) authorizes a source agent to invoke every
capability on a target agent. This is a powerful and dangerous grant intended
for trusted agents in supervised environments.

### Two-Step Consent Flow

The ALL grant requires a two-step consent process in the web UI:

**Step 1: Risk Disclosure**

A full-page modal displays:
- The target agent's `riskDisclosure` from its Agent Card
- Every skill the target agent exposes, with destructive skills highlighted
- Every channel the target agent has configured
- A mandatory 5-second reading delay
- A required checkbox confirming you understand the risks

**Step 2: Re-Authentication**

After confirming the risk disclosure:
- Enter your admin token (cannot be pre-filled from cache)
- Select an expiration (default: 24 hours)
- Confirm with "I accept full responsibility for all actions the source agent
  takes on my behalf"

### Monitoring Active ALL Grants

While an ALL grant is active, a persistent red banner appears at the top of
every dashboard page showing the source, target, and expiration countdown with
a one-click "Revoke Now" button.

### CLI ALL Grant

```bash
nexus a2a grant elevate \
  --source client_claude_desktop \
  --target openclaw \
  --expires 24h
```

The CLI displays the full risk disclosure and requires interactive
confirmation.

## Managing Grants

```bash
# List all grants
nexus a2a grant list

# List grants for a specific source
nexus a2a grant list --source client_claude_desktop

# Revoke a grant
nexus a2a grant revoke <grant_id>
```

Grants can also be managed via the web UI A2A Permissions page, which supports
inline editing, one-click revoke with undo, and real-time hot-reload
confirmation.

## Source Identity

MCP clients are automatically assigned source identities based on their
handshake metadata:

| MCP Client | Source Identity |
|------------|---------------|
| Claude Desktop | `client_claude_desktop` |
| ChatGPT | `client_chatgpt` |
| Perplexity | `client_perplexity` |
| LM Studio | `client_lm_studio` |
| Open WebUI | `client_openwebui` |
| Other | `client_generic` |

Identity is derived from the MCP `clientInfo.name` and `clientInfo.version`
fields plus the bearer token hash.
