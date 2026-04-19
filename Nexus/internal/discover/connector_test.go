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

import (
	"testing"
)

// ── buildConfig ───────────────────────────────────────────────────────────────

func TestBuildConfig_ConnectionTypes(t *testing.T) {
	t.Helper()

	cases := []struct {
		name       string
		tool       DiscoveredTool
		wantConn   string
		wantEndpt  string
		wantCmd    string
		wantArgs   []string
		wantPaths  []string
		wantOrch   bool
		wantIngest bool
	}{
		{
			name: "openai_compat sets endpoint",
			tool: DiscoveredTool{
				Name:           "Ollama",
				ConnectionType: ConnOpenAICompat,
				Endpoint:       "http://localhost:11434",
				Orchestratable: true,
			},
			wantConn:  ConnOpenAICompat,
			wantEndpt: "http://localhost:11434",
			wantOrch:  true,
		},
		{
			name: "http_api sets endpoint",
			tool: DiscoveredTool{
				Name:           "AnythingLLM",
				ConnectionType: ConnHTTPAPI,
				Endpoint:       "http://localhost:3001",
				IngestCapable:  true,
			},
			wantConn:   ConnHTTPAPI,
			wantEndpt:  "http://localhost:3001",
			wantIngest: true,
		},
		{
			name: "mcp_sse sets endpoint",
			tool: DiscoveredTool{
				Name:           "SomeSSETool",
				ConnectionType: ConnMCPSSE,
				Endpoint:       "http://localhost:9000/sse",
			},
			wantConn:  ConnMCPSSE,
			wantEndpt: "http://localhost:9000/sse",
		},
		{
			name: "mcp_stdio with MCPServers uses first entry command+args",
			tool: DiscoveredTool{
				Name:           "Claude Desktop",
				ConnectionType: ConnMCPStdio,
				MCPServers: []MCPServerEntry{
					{Name: "nexus", Command: "nexus", Args: []string{"mcp", "--stdio"}},
					{Name: "other", Command: "other", Args: []string{"--flag"}},
				},
			},
			wantConn: ConnMCPStdio,
			wantCmd:  "nexus",
			wantArgs: []string{"mcp", "--stdio"},
		},
		{
			name: "mcp_stdio without MCPServers leaves command empty",
			tool: DiscoveredTool{
				Name:           "Cursor",
				ConnectionType: ConnMCPStdio,
			},
			wantConn: ConnMCPStdio,
			wantCmd:  "",
		},
		{
			name: "mcp_stdio ingest-capable tool gets watch paths",
			tool: DiscoveredTool{
				Name:           "Claude Code",
				ConnectionType: ConnMCPStdio,
				IngestCapable:  true,
			},
			wantConn:   ConnMCPStdio,
			wantPaths:  []string{"~/.claude/projects"},
			wantIngest: true,
		},
		{
			name: "mcp_stdio non-ingest tool has no watch paths",
			tool: DiscoveredTool{
				Name:           "SomeTool",
				ConnectionType: ConnMCPStdio,
				IngestCapable:  false,
			},
			wantConn:  ConnMCPStdio,
			wantPaths: nil,
		},
		{
			name: "sentinel_ingest populates watch paths",
			tool: DiscoveredTool{
				Name:           "Codex CLI",
				ConnectionType: ConnSentinelIngest,
				IngestCapable:  true,
			},
			wantConn:   ConnSentinelIngest,
			wantPaths:  []string{"~/.codex"},
			wantIngest: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Helper()
			cfg := buildConfig(tc.tool)
			if cfg.Name != tc.tool.Name {
				t.Errorf("Name: got %q, want %q", cfg.Name, tc.tool.Name)
			}
			if cfg.ConnectionType != tc.wantConn {
				t.Errorf("ConnectionType: got %q, want %q", cfg.ConnectionType, tc.wantConn)
			}
			if cfg.Endpoint != tc.wantEndpt {
				t.Errorf("Endpoint: got %q, want %q", cfg.Endpoint, tc.wantEndpt)
			}
			if cfg.Command != tc.wantCmd {
				t.Errorf("Command: got %q, want %q", cfg.Command, tc.wantCmd)
			}
			if !sliceEqual(cfg.Args, tc.wantArgs) {
				t.Errorf("Args: got %v, want %v", cfg.Args, tc.wantArgs)
			}
			if !sliceEqual(cfg.WatchPaths, tc.wantPaths) {
				t.Errorf("WatchPaths: got %v, want %v", cfg.WatchPaths, tc.wantPaths)
			}
			if cfg.Orchestratable != tc.wantOrch {
				t.Errorf("Orchestratable: got %v, want %v", cfg.Orchestratable, tc.wantOrch)
			}
			if cfg.IngestCapable != tc.wantIngest {
				t.Errorf("IngestCapable: got %v, want %v", cfg.IngestCapable, tc.wantIngest)
			}
		})
	}
}

// ── knownIngestPaths ──────────────────────────────────────────────────────────

func TestKnownIngestPaths(t *testing.T) {
	t.Helper()

	cases := []struct {
		toolName  string
		wantPaths []string
	}{
		{"Claude Code", []string{"~/.claude/projects"}},
		{"Claude Desktop", []string{
			"~/Library/Application Support/Claude",
			"~/AppData/Roaming/Claude",
		}},
		{"Cursor", []string{"~/.cursor/workspaceStorage"}},
		{"Codex CLI", []string{"~/.codex"}},
		{"Windsurf", []string{"~/.windsurf"}},
		{"Cline", []string{"~/.cline"}},
		{"VS Code", []string{"~/.vscode"}},
		{"ChatGPT Desktop", []string{
			"~/Library/Application Support/ChatGPT",
			"~/AppData/Roaming/ChatGPT",
		}},
		{"AnythingLLM", []string{"~/.anythingllm"}},
		{"OpenClaw Desktop", []string{"~/.openclaw"}},
		{"Ollama", nil},
		{"UnknownTool", nil},
	}

	for _, tc := range cases {
		t.Run(tc.toolName, func(t *testing.T) {
			t.Helper()
			got := knownIngestPaths(tc.toolName)
			if !sliceEqual(got, tc.wantPaths) {
				t.Errorf("knownIngestPaths(%q): got %v, want %v", tc.toolName, got, tc.wantPaths)
			}
		})
	}
}

// ── Connector ─────────────────────────────────────────────────────────────────

func TestConnector_Propose_Empty(t *testing.T) {
	t.Helper()
	c := NewConnector(ConnectorConfig{})
	proposals := c.Propose(nil)
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals, got %d", len(proposals))
	}
}

func TestConnector_Propose_MultipleTools(t *testing.T) {
	t.Helper()
	tools := []DiscoveredTool{
		{Name: "Ollama", ConnectionType: ConnOpenAICompat, Endpoint: "http://localhost:11434", Orchestratable: true},
		{Name: "Claude Code", ConnectionType: ConnMCPStdio, IngestCapable: true},
	}
	c := NewConnector(ConnectorConfig{})
	proposals := c.Propose(tools)
	if len(proposals) != len(tools) {
		t.Fatalf("expected %d proposals, got %d", len(tools), len(proposals))
	}
	if proposals[0].Tool.Name != "Ollama" {
		t.Errorf("proposal[0] tool name: got %q, want %q", proposals[0].Tool.Name, "Ollama")
	}
	if proposals[0].Config.Endpoint != "http://localhost:11434" {
		t.Errorf("proposal[0] endpoint: got %q", proposals[0].Config.Endpoint)
	}
	if proposals[1].Tool.Name != "Claude Code" {
		t.Errorf("proposal[1] tool name: got %q, want %q", proposals[1].Tool.Name, "Claude Code")
	}
	if !sliceEqual(proposals[1].Config.WatchPaths, []string{"~/.claude/projects"}) {
		t.Errorf("proposal[1] WatchPaths: got %v", proposals[1].Config.WatchPaths)
	}
}

func TestConnector_AutoAccept_ReturnsAllConfigs(t *testing.T) {
	t.Helper()
	tools := []DiscoveredTool{
		{Name: "Ollama", ConnectionType: ConnOpenAICompat, Endpoint: "http://localhost:11434"},
		{Name: "LM Studio", ConnectionType: ConnOpenAICompat, Endpoint: "http://localhost:1234"},
	}
	c := NewConnector(ConnectorConfig{QuickMode: true})
	proposals := c.Propose(tools)
	configs := c.AutoAccept(proposals)
	if len(configs) != len(tools) {
		t.Fatalf("AutoAccept: expected %d configs, got %d", len(tools), len(configs))
	}
	if configs[0].Name != "Ollama" || configs[1].Name != "LM Studio" {
		t.Errorf("unexpected config names: %v", []string{configs[0].Name, configs[1].Name})
	}
}

func TestConnector_AutoAccept_EmptyProposals(t *testing.T) {
	t.Helper()
	c := NewConnector(ConnectorConfig{})
	configs := c.AutoAccept(nil)
	if len(configs) != 0 {
		t.Errorf("expected 0 configs, got %d", len(configs))
	}
}

func TestConnector_QuickModeField(t *testing.T) {
	t.Helper()
	c := NewConnector(ConnectorConfig{QuickMode: true})
	if !c.cfg.QuickMode {
		t.Error("QuickMode not stored on Connector")
	}
}

func TestConnector_ProposalToolRoundtrip(t *testing.T) {
	t.Helper()
	dt := DiscoveredTool{
		Name:           "Cursor",
		DetectionMethod: MethodProcess,
		ConnectionType: ConnMCPStdio,
		Orchestratable: false,
		IngestCapable:  true,
		MCPServers: []MCPServerEntry{
			{Name: "nexus", Command: "nexus", Args: []string{"--mcp"}},
		},
	}
	c := NewConnector(ConnectorConfig{})
	proposals := c.Propose([]DiscoveredTool{dt})
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal")
	}
	p := proposals[0]
	if p.Tool.Name != dt.Name {
		t.Errorf("proposal tool name: got %q", p.Tool.Name)
	}
	if p.Config.Command != "nexus" {
		t.Errorf("Config.Command: got %q, want %q", p.Config.Command, "nexus")
	}
	if !sliceEqual(p.Config.Args, []string{"--mcp"}) {
		t.Errorf("Config.Args: got %v", p.Config.Args)
	}
	// Cursor's IngestCapable paths
	if !sliceEqual(p.Config.WatchPaths, []string{"~/.cursor/workspaceStorage"}) {
		t.Errorf("Config.WatchPaths: got %v", p.Config.WatchPaths)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
