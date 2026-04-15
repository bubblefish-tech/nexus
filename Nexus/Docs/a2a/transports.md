# Nexus A2A Transports

Nexus A2A supports four physical transports for agent communication. Each
transport handles the JSON-RPC 2.0 framing between Nexus and the remote agent.

## HTTP (Direct)

The default transport. Nexus connects to the agent's HTTP endpoint and sends
JSON-RPC requests as POST bodies.

```bash
bubblefish a2a agent add myagent \
  --transport http \
  --url http://localhost:9100
```

### Configuration

```toml
# ~/.bubblefish/Nexus/a2a/agents/myagent.toml
[transport]
kind = "http"
url = "http://localhost:9100"
auth_type = "bearer"       # "bearer", "none"
auth_token = "env:MYAGENT_TOKEN"  # use env: or file: references
timeout_ms = 30000         # connection timeout (default: 30s)
```

### When to Use

- Agent running on the same machine or local network
- Agent behind a reverse proxy with TLS termination
- Simplest setup, lowest latency

## Stdio (Local Subprocess)

Nexus spawns the agent as a child process and communicates via stdin/stdout
using newline-delimited JSON-RPC.

```bash
bubblefish a2a agent add localagent \
  --transport stdio \
  --command /usr/local/bin/myagent \
  --args "--mode a2a"
```

### Configuration

```toml
[transport]
kind = "stdio"
command = "/usr/local/bin/myagent"
args = ["--mode", "a2a"]
timeout_ms = 60000
```

### When to Use

- Agent distributed as a single binary
- No network configuration needed
- Agent lifecycle managed by Nexus (started on first use, stopped on shutdown)
- Ideal for local-only agents that should not listen on a port

### Notes

- The agent binary must read JSON-RPC requests from stdin and write responses
  to stdout, one per line
- Stderr is captured and logged by Nexus at the WARN level
- If the process exits unexpectedly, Nexus logs the error, marks the agent
  status as `error`, and attempts to restart on the next request

## Tunnel (Cloudflare-Style)

Routes traffic through an existing HTTP endpoint with tunnel configuration
flags. This is functionally identical to the HTTP transport but signals to
Nexus that the connection may traverse a tunnel with additional latency.

```bash
bubblefish a2a agent add tunneled \
  --transport tunnel \
  --url https://myagent.example.com
```

### Configuration

```toml
[transport]
kind = "tunnel"
url = "https://myagent.example.com"
auth_type = "bearer"
auth_token = "env:TUNNEL_TOKEN"
timeout_ms = 60000         # higher timeout for tunnel latency
```

### When to Use

- Agent running behind a Cloudflare Tunnel, ngrok, or similar
- Agent on a remote machine without direct port access
- Production deployments where the agent is on a different network

## WSL (Windows-Host-to-WSL2 Loopback)

Connects from the Windows host to an agent running inside WSL2. WSL2
automatically forwards `localhost` ports, so this transport dials
`localhost:<port>` and performs a health check.

```bash
bubblefish a2a agent add wsl-agent \
  --transport wsl \
  --url http://localhost:8200
```

### Configuration

```toml
[transport]
kind = "wsl"
url = "http://localhost:8200"
timeout_ms = 30000
```

### When to Use

- Agent running inside a WSL2 Linux distribution
- Windows host running Nexus, Linux agent in WSL2
- Development and testing on Windows with Linux-native agents

### Requirements

- WSL2 must be installed and running
- The agent must bind to `0.0.0.0` (not `127.0.0.1`) inside WSL2
- The port must not be blocked by Windows Firewall

### Troubleshooting

If the WSL transport cannot reach the agent:

1. Verify the agent is running inside WSL2:
   ```bash
   wsl ss -tlnp | grep <port>
   ```

2. Verify the agent binds to `0.0.0.0`:
   ```bash
   wsl ss -tlnp | grep <port>
   # Look for 0.0.0.0:<port>, not 127.0.0.1:<port>
   ```

3. Check Windows Firewall is not blocking the port

4. Test connectivity from Windows:
   ```powershell
   curl http://localhost:<port>/a2a/jsonrpc
   ```

### Platform Note

The WSL transport is only available on Windows. On Linux and macOS, attempting
to register an agent with `--transport wsl` returns an error at registration
time. Use the HTTP transport instead.

## Common Configuration

All transports support these shared options:

| Option | Description | Default |
|--------|-------------|---------|
| `timeout_ms` | Connection and request timeout in milliseconds | 30000 |
| `auth_type` | Authentication type: `bearer`, `mtls`, `none` | `none` |
| `auth_token` | Bearer token (use `env:` or `file:` references) | -- |

## Health Checks

Nexus periodically health-checks registered agents by calling `agent/ping`.
The default check interval is 60 seconds. After 5 consecutive failures, the
agent status transitions to `error`.

```bash
# Manual health check
bubblefish a2a agent test myagent

# View agent status
bubblefish a2a agent show myagent
```

## Changing Transports

To change an agent's transport, retire the old registration and add a new one:

```bash
bubblefish a2a agent retire myagent
bubblefish a2a agent add myagent --transport stdio --command /path/to/agent
```

Existing grants are preserved across re-registration if the agent name
remains the same.
