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

// Package discover provides AI tool discovery and auto-connection for BubbleFish Nexus.
package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// DetectionMethod values for ToolDefinition.
const (
	MethodPort      = "port"
	MethodProcess   = "process"
	MethodDirectory = "directory"
	MethodMCPConfig = "mcp_config"
	MethodDocker    = "docker"
)

// ConnectionType values for ToolDefinition.
const (
	ConnOpenAICompat   = "openai_compat"
	ConnMCPStdio       = "mcp_stdio"
	ConnMCPSSE         = "mcp_sse"
	ConnIngest = "ingest"
	ConnHTTPAPI        = "http_api"
)

// ToolDefinition describes how to detect and connect to an AI tool.
type ToolDefinition struct {
	Name             string   `toml:"name"`
	DetectionMethod  string   `toml:"detection_method"`
	DefaultPort      int      `toml:"default_port,omitempty"`
	ProbeURL         string   `toml:"probe_url,omitempty"`
	ExpectedResponse string   `toml:"expected_response,omitempty"`
	ProcessNames     []string `toml:"process_names,omitempty"`
	DirectoryPaths   []string `toml:"directory_paths,omitempty"`
	MCPConfigPaths   []string `toml:"mcp_config_paths,omitempty"`
	DockerImages     []string `toml:"docker_images,omitempty"`
	ConnectionType   string   `toml:"connection_type"`
	DefaultEndpoint  string   `toml:"default_endpoint,omitempty"`
	Orchestratable   bool     `toml:"orchestratable"`
	IngestCapable    bool     `toml:"ingest_capable"`
}

// customToolsFile is the TOML struct for ~/.nexus/discovery/custom_tools.toml.
type customToolsFile struct {
	Tools []ToolDefinition `toml:"tools"`
}

// KnownTools is the built-in manifest of known AI tools across all detection tiers.
var KnownTools = []ToolDefinition{
	// ── Port-detectable ─────────────────────────────────────────────────────────
	{
		Name:             "Ollama",
		DetectionMethod:  MethodPort,
		DefaultPort:      11434,
		ProbeURL:         "/api/version",
		ExpectedResponse: "version",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  "http://localhost:11434",
		Orchestratable:   true,
		IngestCapable:    false,
	},
	{
		Name:             "LM Studio",
		DetectionMethod:  MethodPort,
		DefaultPort:      1234,
		ProbeURL:         "/v1/models",
		ExpectedResponse: "object",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  "http://localhost:1234",
		Orchestratable:   true,
		IngestCapable:    false,
	},
	{
		Name:             "LocalAI",
		DetectionMethod:  MethodPort,
		DefaultPort:      8080,
		ProbeURL:         "/v1/models",
		ExpectedResponse: "data",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  "http://localhost:8080",
		Orchestratable:   true,
		IngestCapable:    false,
	},
	{
		Name:             "Jan",
		DetectionMethod:  MethodPort,
		DefaultPort:      1337,
		ProbeURL:         "/v1/models",
		ExpectedResponse: "data",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  "http://localhost:1337",
		Orchestratable:   true,
		IngestCapable:    false,
	},
	{
		Name:             "GPT4All",
		DetectionMethod:  MethodPort,
		DefaultPort:      4891,
		ProbeURL:         "/v1/models",
		ExpectedResponse: "data",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  "http://localhost:4891",
		Orchestratable:   true,
		IngestCapable:    false,
	},
	{
		Name:             "vLLM",
		DetectionMethod:  MethodPort,
		DefaultPort:      8000,
		ProbeURL:         "/v1/models",
		ExpectedResponse: "data",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  "http://localhost:8000",
		Orchestratable:   true,
		IngestCapable:    false,
	},
	{
		Name:             "Text Generation Inference",
		DetectionMethod:  MethodPort,
		DefaultPort:      8080,
		ProbeURL:         "/info",
		ExpectedResponse: "model_id",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  "http://localhost:8080",
		Orchestratable:   true,
		IngestCapable:    false,
	},
	{
		Name:             "Open WebUI",
		DetectionMethod:  MethodPort,
		DefaultPort:      3000,
		ProbeURL:         "/api/config",
		ExpectedResponse: "status",
		ConnectionType:   ConnHTTPAPI,
		DefaultEndpoint:  "http://localhost:3000",
		Orchestratable:   false,
		IngestCapable:    false,
	},
	{
		Name:             "AnythingLLM",
		DetectionMethod:  MethodPort,
		DefaultPort:      3001,
		ProbeURL:         "/api/health",
		ExpectedResponse: "ok",
		ConnectionType:   ConnHTTPAPI,
		DefaultEndpoint:  "http://localhost:3001",
		Orchestratable:   false,
		IngestCapable:    true,
	},
	{
		Name:             "LibreChat",
		DetectionMethod:  MethodPort,
		DefaultPort:      3080,
		ProbeURL:         "/api/health",
		ExpectedResponse: "ok",
		ConnectionType:   ConnHTTPAPI,
		DefaultEndpoint:  "http://localhost:3080",
		Orchestratable:   false,
		IngestCapable:    false,
	},
	{
		Name:             "oobabooga",
		DetectionMethod:  MethodPort,
		DefaultPort:      7860,
		ProbeURL:         "/v1/models",
		ExpectedResponse: "data",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  "http://localhost:7860",
		Orchestratable:   true,
		IngestCapable:    false,
	},
	{
		Name:             "koboldcpp",
		DetectionMethod:  MethodPort,
		DefaultPort:      5001,
		ProbeURL:         "/api/v1/model",
		ExpectedResponse: "result",
		ConnectionType:   ConnHTTPAPI,
		DefaultEndpoint:  "http://localhost:5001",
		Orchestratable:   true,
		IngestCapable:    false,
	},
	{
		Name:             "Tabby",
		DetectionMethod:  MethodPort,
		DefaultPort:      8080,
		ProbeURL:         "/v1/health",
		ExpectedResponse: "ok",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  "http://localhost:8080",
		Orchestratable:   true,
		IngestCapable:    false,
	},

	// ── Process-detectable ───────────────────────────────────────────────────────
	{
		Name:            "Claude Desktop",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"Claude", "claude"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  false,
		IngestCapable:   true,
	},
	{
		Name:            "ChatGPT Desktop",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"ChatGPT"},
		ConnectionType:  ConnHTTPAPI,
		Orchestratable:  false,
		IngestCapable:   true,
	},
	{
		Name:            "Cursor",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"Cursor", "cursor"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  false,
		IngestCapable:   true,
	},
	{
		Name:            "VS Code",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"Code", "code", "code-insiders"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  false,
		IngestCapable:   true,
	},
	{
		Name:            "Windsurf",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"Windsurf", "windsurf"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  false,
		IngestCapable:   true,
	},
	{
		Name:            "Codex CLI",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"codex"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  true,
		IngestCapable:   true,
	},
	{
		Name:            "OpenClaw Desktop",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"OpenClaw", "openclaw"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  false,
		IngestCapable:   true,
	},
	{
		Name:            "Cline",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"cline"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  false,
		IngestCapable:   true,
	},

	// ── Directory-detectable ─────────────────────────────────────────────────────
	{
		Name:            "Claude Code",
		DetectionMethod: MethodDirectory,
		DirectoryPaths:  []string{"~/.claude"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  true,
		IngestCapable:   true,
	},
	{
		Name:            "Ollama",
		DetectionMethod: MethodDirectory,
		DirectoryPaths:  []string{"~/.ollama"},
		ConnectionType:  ConnOpenAICompat,
		DefaultEndpoint: "http://localhost:11434",
		Orchestratable:  true,
		IngestCapable:   false,
	},
	{
		Name:            "LM Studio",
		DetectionMethod: MethodDirectory,
		DirectoryPaths:  []string{"~/.cache/lm-studio"},
		ConnectionType:  ConnOpenAICompat,
		DefaultEndpoint: "http://localhost:1234",
		Orchestratable:  true,
		IngestCapable:   false,
	},
	{
		Name:            "GPT4All",
		DetectionMethod: MethodDirectory,
		DirectoryPaths:  []string{"~/.local/share/nomic.ai"},
		ConnectionType:  ConnOpenAICompat,
		DefaultEndpoint: "http://localhost:4891",
		Orchestratable:  true,
		IngestCapable:   false,
	},
	{
		Name:            "Jan",
		DetectionMethod: MethodDirectory,
		DirectoryPaths:  []string{"~/jan"},
		ConnectionType:  ConnOpenAICompat,
		DefaultEndpoint: "http://localhost:1337",
		Orchestratable:  true,
		IngestCapable:   false,
	},
	{
		Name:            "HuggingFace",
		DetectionMethod: MethodDirectory,
		DirectoryPaths:  []string{"~/.cache/huggingface"},
		ConnectionType:  ConnHTTPAPI,
		Orchestratable:  false,
		IngestCapable:   false,
	},
	{
		Name:            "Cursor",
		DetectionMethod: MethodDirectory,
		DirectoryPaths:  []string{"~/.cursor"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  false,
		IngestCapable:   true,
	},
	{
		Name:            "Codex CLI",
		DetectionMethod: MethodDirectory,
		DirectoryPaths:  []string{"~/.codex"},
		ConnectionType:  ConnMCPStdio,
		Orchestratable:  true,
		IngestCapable:   true,
	},

	// ── MCP-config-detectable ────────────────────────────────────────────────────
	{
		Name:            "Claude Desktop",
		DetectionMethod: MethodMCPConfig,
		MCPConfigPaths: []string{
			"~/.config/claude/claude_desktop_config.json",
			"~/Library/Application Support/Claude/claude_desktop_config.json",
			"~/AppData/Roaming/Claude/claude_desktop_config.json",
		},
		ConnectionType: ConnMCPStdio,
		Orchestratable: false,
		IngestCapable:  true,
	},
	{
		Name:            "Cursor",
		DetectionMethod: MethodMCPConfig,
		MCPConfigPaths: []string{
			"~/.cursor/mcp.json",
		},
		ConnectionType: ConnMCPStdio,
		Orchestratable: false,
		IngestCapable:  true,
	},
	{
		Name:            "Windsurf",
		DetectionMethod: MethodMCPConfig,
		MCPConfigPaths: []string{
			"~/.windsurf/mcp.json",
		},
		ConnectionType: ConnMCPStdio,
		Orchestratable: false,
		IngestCapable:  true,
	},
	{
		Name:            "Cline",
		DetectionMethod: MethodMCPConfig,
		MCPConfigPaths: []string{
			"~/.cline/mcp_settings.json",
		},
		ConnectionType: ConnMCPStdio,
		Orchestratable: false,
		IngestCapable:  true,
	},
	{
		Name:            "VS Code",
		DetectionMethod: MethodMCPConfig,
		MCPConfigPaths: []string{
			"~/.vscode/settings.json",
		},
		ConnectionType: ConnMCPStdio,
		Orchestratable: false,
		IngestCapable:  true,
	},

	// ── Docker-detectable ────────────────────────────────────────────────────────
	{
		Name:            "Open WebUI",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"ghcr.io/open-webui/open-webui", "open-webui"},
		ConnectionType:  ConnHTTPAPI,
		DefaultEndpoint: "http://localhost:3000",
		Orchestratable:  false,
		IngestCapable:   false,
	},
	{
		Name:            "Ollama",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"ollama/ollama"},
		ConnectionType:  ConnOpenAICompat,
		DefaultEndpoint: "http://localhost:11434",
		Orchestratable:  true,
		IngestCapable:   false,
	},
	{
		Name:            "vLLM",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"vllm/vllm-openai"},
		ConnectionType:  ConnOpenAICompat,
		DefaultEndpoint: "http://localhost:8000",
		Orchestratable:  true,
		IngestCapable:   false,
	},
	{
		Name:            "Text Generation Inference",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"ghcr.io/huggingface/text-generation-inference"},
		ConnectionType:  ConnOpenAICompat,
		DefaultEndpoint: "http://localhost:8080",
		Orchestratable:  true,
		IngestCapable:   false,
	},
	{
		Name:            "LocalAI",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"localai/localai", "quay.io/go-skynet/local-ai"},
		ConnectionType:  ConnOpenAICompat,
		DefaultEndpoint: "http://localhost:8080",
		Orchestratable:  true,
		IngestCapable:   false,
	},
	{
		Name:            "AnythingLLM",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"mintplexlabs/anythingllm"},
		ConnectionType:  ConnHTTPAPI,
		DefaultEndpoint: "http://localhost:3001",
		Orchestratable:  false,
		IngestCapable:   true,
	},
	{
		Name:            "LibreChat",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"ghcr.io/danny-avila/librechat"},
		ConnectionType:  ConnHTTPAPI,
		DefaultEndpoint: "http://localhost:3080",
		Orchestratable:  false,
		IngestCapable:   false,
	},
}

// ExpandPath replaces a leading ~ with the user's home directory.
func ExpandPath(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand path %q: %w", p, err)
	}
	return filepath.Join(home, p[1:]), nil
}

// LoadCustomTools reads custom tool definitions from configDir/discovery/custom_tools.toml.
// Returns an empty slice (not an error) if the file does not exist.
func LoadCustomTools(configDir string) ([]ToolDefinition, error) {
	path := filepath.Join(configDir, "discovery", "custom_tools.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read custom tools %q: %w", path, err)
	}
	var f customToolsFile
	if err := toml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse custom tools %q: %w", path, err)
	}
	return f.Tools, nil
}

// AllTools returns the built-in KnownTools merged with any custom tools loaded
// from configDir. Custom tools are appended after built-ins.
func AllTools(configDir string) ([]ToolDefinition, error) {
	custom, err := LoadCustomTools(configDir)
	if err != nil {
		return nil, err
	}
	result := make([]ToolDefinition, len(KnownTools), len(KnownTools)+len(custom))
	copy(result, KnownTools)
	return append(result, custom...), nil
}
