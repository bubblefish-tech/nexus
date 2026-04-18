# Nexus A2A Quickstart

Get Nexus A2A running in 10 minutes: enable A2A, register an agent, grant
a capability, and send your first task from Claude Desktop.

## Prerequisites

- BubbleFish Nexus v0.1.3 installed and running (`bubblefish start`)
- An A2A-compatible agent to register (e.g., OpenClaw)
- Claude Desktop (or any MCP-compatible client) connected to Nexus

## 1. Enable Nexus A2A

Edit `~/.bubblefish/Nexus/daemon.toml`:

```toml
[a2a]
enabled = true
```

Restart the daemon:

```bash
bubblefish stop && bubblefish start
```

## 2. Register an Agent

Register OpenClaw (or any A2A agent) via the CLI:

```bash
# HTTP transport (agent running on localhost:9100)
bubblefish a2a agent add openclaw \
  --transport http \
  --url http://localhost:9100 \
  --auth bearer
```

Verify the agent is reachable:

```bash
bubblefish a2a agent test openclaw
```

You should see:

```
openclaw: ping OK (12ms)
```

## 3. Grant a Capability

Grant the `messaging.send:signal` capability so Claude Desktop can ask
OpenClaw to send Signal messages on your behalf:

```bash
bubblefish a2a grant add \
  --source client_claude_desktop \
  --target openclaw \
  --capability "messaging.send:signal"
```

Verify the grant:

```bash
bubblefish a2a grant list
```

## 4. Send a Task from Claude Desktop

In Claude Desktop, use the Nexus MCP tools:

```
Use the a2a_send_to_agent tool to send a message via OpenClaw:
  agent: openclaw
  skill: send_signal_message
  input: {"channel": "signal", "recipient": "+1234567890", "text": "Hello from Claude!"}
```

Claude Desktop will invoke the `a2a_send_to_agent` MCP tool, which routes
through the Nexus bridge to OpenClaw. The response includes the task state,
task ID, and any output from the skill.

## 5. Check the Audit Trail

Every task is recorded in the audit chain:

```bash
bubblefish a2a audit tail --since 5m
```

Verify the chain integrity:

```bash
bubblefish a2a audit verify
```

## 6. Inspect via the Web Dashboard

Open the Nexus dashboard at `http://localhost:8081` and navigate to:

- **A2A Permissions** -- registered agents, grants, pending approvals, audit feed
- **OpenClaw** -- OpenClaw-specific controls, skill catalog, channel grants

## What's Next

- Add more agents: `bubblefish a2a agent add <name> --transport <kind> --url <url>`
- Fine-tune grants: use glob patterns like `messaging.send:*` or `fs.read`
- Set up the ALL grant for trusted agents via the web UI two-step consent flow
- See [Permissions](permissions.md) for the full capability vocabulary
- See [Transports](transports.md) for stdio, tunnel, and WSL configuration
