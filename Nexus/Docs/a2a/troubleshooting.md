# Nexus A2A Troubleshooting

Common errors and their solutions.

## Agent Registration

### "agent not found" when sending a task

The agent name in your `a2a_send_to_agent` call must match the registered
name exactly:

```bash
# Check registered agents
nexus a2a agent list
```

If the agent is not listed, register it:

```bash
nexus a2a agent add <name> --transport http --url <url>
```

### "transport: unknown kind" during registration

Supported transport kinds are `http`, `stdio`, `tunnel`, and `wsl`. Check
spelling and note that `wsl` is only available on Windows.

### Agent status shows "error"

The agent failed 5 or more consecutive health checks. Verify the agent is
running and reachable:

```bash
nexus a2a agent test <name>
```

Common causes:
- Agent process crashed or was stopped
- Network configuration changed (port, firewall)
- Agent bound to `127.0.0.1` instead of `0.0.0.0` (WSL transport)

## Governance

### "governance: denied" error

No matching grant exists for the `(source, target, capability)` triple, and
the default policy for the capability is not `auto-allow`.

Fix: add a grant:

```bash
nexus a2a grant add \
  --source <source_identity> \
  --target <agent_id> \
  --capability "<capability>"
```

### Task status shows "escalated"

The governance engine determined that human approval is required. This happens
when:

- The default policy is `always-approve` (e.g., `shell.exec`, `fs.delete`)
- The skill is marked as destructive
- No matching grant exists and the default policy is `approve-once-per-*`

Resolve by approving in the web UI (A2A Permissions > Pending Approvals) or
by adding a pre-authorized grant via CLI.

### Grant not taking effect

Grants are matched by `(source, target, capability)`:

1. Verify the **source identity** matches. Use `nexus a2a grant list` and
   compare with the identity shown in audit events.
2. Verify the **target agent ID** (not the display name). Use
   `nexus a2a agent show <name>` to see the agent ID.
3. Check for conflicting **deny grants**. Deny always wins over allow at the
   same specificity level.
4. Check grant **expiration**. Expired grants are ignored.

### "grant expired" in audit log

The grant's `expires_at` time has passed. Create a new grant or use a longer
expiration:

```bash
nexus a2a grant add \
  --source <src> --target <tgt> \
  --capability "<cap>" \
  --expires 720h  # 30 days
```

## Transport Issues

### HTTP: "connection refused"

The target agent is not listening on the configured URL. Verify:

```bash
curl -s http://<host>:<port>/a2a/jsonrpc \
  -d '{"jsonrpc":"2.0","method":"agent/ping","id":1}'
```

### Stdio: "executable not found"

The configured command path does not exist or is not executable:

```bash
ls -la /path/to/agent
```

### WSL: "connection timed out"

See the [WSL troubleshooting section](transports.md#troubleshooting) in the
transports guide. Most common cause: agent bound to `127.0.0.1` inside WSL2
instead of `0.0.0.0`.

### Tunnel: high latency or timeouts

Increase the timeout:

```toml
[transport]
kind = "tunnel"
url = "https://myagent.example.com"
timeout_ms = 120000  # 2 minutes
```

## MCP Bridge

### Bridge tools not appearing in Claude Desktop

Nexus A2A must be enabled in `daemon.toml`:

```toml
[a2a]
enabled = true
```

Restart the daemon after changing this setting. The bridge tools
(`a2a_list_agents`, `a2a_send_to_agent`, etc.) are only registered when A2A
is enabled.

### "unknown tool: a2a_send_to_agent"

The MCP client may need to refresh its tool list. In Claude Desktop, restart
the MCP connection or reload the project.

### Wrong source identity

The bridge derives source identity from the MCP client's `clientInfo.name`.
If your client reports an unexpected name, the source identity may not match
your grants. Check the audit log:

```bash
nexus a2a audit tail --since 5m
```

Look for the `source` field in the audit events.

## Audit Chain

### "chain verification failed"

The audit chain has a gap or integrity issue. Run verification with details:

```bash
nexus a2a audit verify
```

If this is a known-safe situation (e.g., after a database restore), use the
main audit recovery tool:

```bash
nexus audit recover
```

### Missing audit events

Audit events are written synchronously on the governance decision path. If
events are missing:

1. Check if A2A is enabled (`[a2a] enabled = true`)
2. Check if the task completed or was denied before reaching the audit step
3. Check daemon logs for audit sink errors

## Performance

### Slow task dispatch

Common causes:
- High latency transport (tunnel, WAN HTTP)
- Governance engine evaluating many grants (optimize with specific globs)
- Slow skill execution on the target agent

Check task timing:

```bash
nexus a2a task get <task_id>
```

### "SQLITE_BUSY" errors under load

The governance and task stores use SQLite with WAL mode and `busy_timeout=5000`.
Under very high concurrency, you may see occasional busy errors. These are
retried automatically. If persistent, check for long-running transactions in
other processes accessing the same database.

## Getting Help

If your issue is not covered here:

1. Check daemon logs: `~/.nexus/Nexus/logs/`
2. Run diagnostics: `nexus doctor`
3. Report an issue: https://github.com/bubblefish-tech/nexus/issues
