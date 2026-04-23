// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

package discover

// ConnectorConfig holds configuration for the Connector.
type ConnectorConfig struct {
	// QuickMode auto-accepts all proposals without user interaction (nexus install --quick).
	QuickMode bool
}

// ConnectionConfig holds resolved parameters for a single tool connection.
type ConnectionConfig struct {
	Name           string
	ConnectionType string
	// Endpoint is the base URL for openai_compat, mcp_sse, and http_api connections.
	Endpoint string
	// Command and Args configure the subprocess for mcp_stdio connections.
	Command string
	Args    []string
	// WatchPaths lists directories to monitor for ingest connections
	// and for ingest-capable mcp_stdio tools.
	WatchPaths     []string
	Orchestratable bool
	IngestCapable  bool
}

// ConnectionProposal pairs a discovered tool with its proposed connection config.
type ConnectionProposal struct {
	Tool   DiscoveredTool
	Config ConnectionConfig
}

// Connector builds and proposes connection configurations for discovered tools.
type Connector struct {
	cfg ConnectorConfig
}

// NewConnector creates a Connector with the given configuration.
func NewConnector(cfg ConnectorConfig) *Connector {
	return &Connector{cfg: cfg}
}

// Propose returns a ConnectionProposal for each discovered tool.
func (c *Connector) Propose(tools []DiscoveredTool) []ConnectionProposal {
	out := make([]ConnectionProposal, 0, len(tools))
	for _, dt := range tools {
		out = append(out, ConnectionProposal{
			Tool:   dt,
			Config: buildConfig(dt),
		})
	}
	return out
}

// AutoAccept returns the ConnectionConfig for every proposal.
// Used in --quick install mode; interactive mode presents proposals to the user first.
func (c *Connector) AutoAccept(proposals []ConnectionProposal) []ConnectionConfig {
	out := make([]ConnectionConfig, 0, len(proposals))
	for _, p := range proposals {
		out = append(out, p.Config)
	}
	return out
}

// buildConfig creates a ConnectionConfig appropriate for dt's ConnectionType.
func buildConfig(dt DiscoveredTool) ConnectionConfig {
	cfg := ConnectionConfig{
		Name:           dt.Name,
		ConnectionType: dt.ConnectionType,
		Endpoint:       dt.Endpoint,
		Orchestratable: dt.Orchestratable,
		IngestCapable:  dt.IngestCapable,
	}

	switch dt.ConnectionType {
	case ConnMCPStdio:
		if len(dt.MCPServers) > 0 {
			srv := dt.MCPServers[0]
			cfg.Command = srv.Command
			cfg.Args = srv.Args
		}
		if dt.IngestCapable {
			cfg.WatchPaths = knownIngestPaths(dt.Name)
		}
	case ConnIngest:
		cfg.WatchPaths = knownIngestPaths(dt.Name)
	}
	// openai_compat, mcp_sse, http_api: Endpoint already set from dt.Endpoint.

	return cfg
}

// knownIngestPaths returns the data directories to watch for a named tool.
// Returns nil when the tool has no known ingest paths.
func knownIngestPaths(toolName string) []string {
	switch toolName {
	case "Claude Code":
		return []string{"~/.claude/projects"}
	case "Claude Desktop":
		return []string{
			"~/Library/Application Support/Claude",
			"~/AppData/Roaming/Claude",
		}
	case "Cursor":
		return []string{"~/.cursor/workspaceStorage"}
	case "Codex CLI":
		return []string{"~/.codex"}
	case "Windsurf":
		return []string{"~/.windsurf"}
	case "Cline":
		return []string{"~/.cline"}
	case "VS Code":
		return []string{"~/.vscode"}
	case "ChatGPT Desktop":
		return []string{
			"~/Library/Application Support/ChatGPT",
			"~/AppData/Roaming/ChatGPT",
		}
	case "AnythingLLM":
		return []string{"~/.anythingllm"}
	case "OpenClaw Desktop":
		return []string{"~/.openclaw"}
	}
	return nil
}
