# Nexus A2A Overview

Nexus now speaks a governed agent-to-agent protocol. Register an agent once
with Nexus, grant it a scoped set of capabilities, and any MCP-compatible AI
assistant (Claude Desktop, ChatGPT, Perplexity, LM Studio, Open WebUI) can
invoke that agent through Nexus without any code changes on the AI side. Every
task is governed by capability-scope grants you control, every destructive
action is gated by explicit approval, every action is written to the
tamper-evident audit chain. Nexus A2A is wire-compatible with the public A2A
v1.0 specification, so you can point Nexus at any conformant A2A server.
OpenClaw is the first bundled integration.

Four physical transports ship on day one: local subprocess, direct HTTP,
Cloudflare-style tunnel, and Windows-host-to-WSL2 loopback. A dedicated web
UI page lets you inspect registered agents, edit grants in real time, approve
pending tasks, and review audit history. A second web page provides
OpenClaw-specific controls with per-channel grant toggles and a dedicated
"allow everything" flow gated behind two-step re-authenticated consent with a
visible expiration countdown.

Nexus A2A is disabled by default in v0.1.3. Enable it by setting
`[a2a] enabled = true` in `daemon.toml` and registering your first agent with
`bubblefish a2a agent add`.

## How It Works

```
MCP Client (Claude Desktop, etc.)
    |
    | MCP tool: a2a_send_to_agent
    v
Nexus MCP-to-NA2A Bridge
    |
    | 1. Derive source identity from MCP session
    | 2. Resolve target agent from registry
    | 3. Check governance grants
    | 4. Dispatch via NA2A client
    v
Target Agent (e.g., OpenClaw)
    |
    | Execute skill, return result
    v
Bridge translates response back to MCP
    |
    | Audit entry written to chain
    v
MCP Client receives result
```

## Key Concepts

**Agents** are registered services that expose skills. Each agent has an Agent
Card describing its capabilities, skills, transport configuration, and
optional public key for signature verification.

**Skills** are named operations an agent can perform. Each skill declares which
capabilities it requires (e.g., `messaging.send:signal`, `fs.read`).

**Grants** authorize a source agent to invoke capabilities on a target agent.
Grants are scoped by capability glob pattern (e.g., `messaging.send:*`) and
can have expiration times. Only the web UI and CLI can create or revoke grants.

**Approvals** are interactive prompts raised when a task requires a capability
that has no matching grant or when the default policy requires human review.
Approvals appear in the web UI and can be approved, denied, or
approved-and-cached.

**Audit chain** records every governance decision, task dispatch, and task
completion. Audit entries are linked to the Phase-4 cryptographic provenance
chain and can be verified with `bubblefish a2a audit verify`.

## Next Steps

- [Quickstart](quickstart.md) -- get running in 10 minutes
- [Permissions](permissions.md) -- capability vocabulary and policy reference
- [Transports](transports.md) -- configure agent connectivity
- [Troubleshooting](troubleshooting.md) -- common issues and fixes
