# Agent Gateway Configuration

Reference for the Agent Gateway TOML configuration sections added in v0.1.3.

## Credential Gateway

Add to `daemon.toml`:

```toml
[credentials]
enabled = true

[[credentials.mappings]]
synthetic_prefix = "bfn_sk_openai_dev"
real_key_ref     = "env:OPENAI_API_KEY"
provider         = "openai"
allowed_agents   = ["agent-research", "agent-coder"]
allowed_models   = ["gpt-4o", "gpt-4o-mini"]
rate_limit_rpm   = 100

[[credentials.mappings]]
synthetic_prefix = "bfn_sk_anth_prod"
real_key_ref     = "env:ANTHROPIC_API_KEY"
provider         = "anthropic"
allowed_agents   = []  # empty = all agents allowed
allowed_models   = ["claude-sonnet-4-6", "claude-haiku-4-5-20251001"]
rate_limit_rpm   = 60
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | yes | Master switch for the credential gateway |
| `synthetic_prefix` | string | yes | Prefix agents use as their API key (e.g., `bfn_sk_openai_dev`) |
| `real_key_ref` | string | yes | Reference to real provider key: `env:VAR_NAME` or `file:/path` |
| `provider` | string | yes | `"openai"` or `"anthropic"` |
| `allowed_agents` | string[] | no | Agent IDs allowed to use this mapping. Empty = all |
| `allowed_models` | string[] | no | Model names allowed. Empty = all |
| `rate_limit_rpm` | int | no | Requests per minute per synthetic key. 0 = unlimited |

### Security

- `real_key_ref` uses the `env:` or `file:` reference scheme. The resolved value is **never** logged, stored in config structs, or included in error messages.
- Synthetic key validation uses `crypto/subtle.ConstantTimeCompare` on equal-length prefix slices.
- Upstream error responses are sanitized to strip any key reference strings before returning to the client.

## Agent Registration

Agents are registered via CLI:

```bash
bubblefish agent register --name "research-agent" --description "RAG research agent"
bubblefish agent list
bubblefish agent show <agent_id>
bubblefish agent suspend <agent_id>
bubblefish agent retire <agent_id>
bubblefish agent health
```

Agent identity is stored in `nexus.db` (SQLite). The `X-Agent-ID` header is optional in v0.1.3 and carries the agent UUID on requests.

## Per-Agent Rate Limits and Quotas

Configure in agent TOML files (future; currently set via `QuotaManager.SetConfig()` API):

```toml
[rate_limit]
requests_per_minute = 600
bytes_per_second    = 524288
writes_per_day      = 50000
tool_calls_per_day  = 10000
```

Quota state is persisted hourly to `$BUBBLEFISH_HOME/quotas.state` and survives restart. Day-bounded quotas reset at UTC midnight.

## Tool-Use Policy

Configure in agent TOML files (future; currently set via `ToolPolicyChecker.SetPolicy()` API):

```toml
[tools]
allowed = ["nexus_write", "nexus_search", "nexus_status"]
denied  = []

[tools.nexus_write]
max_content_bytes = 10000

[tools.nexus_search]
max_limit        = 50
allowed_profiles = ["fast", "balanced"]
```

Denylist takes precedence over allowlist. Empty allowlist means all tools are permitted.
