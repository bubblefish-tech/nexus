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
	"encoding/json"
	"os"
)

// MCPServerEntry is a single MCP server entry parsed from a tool's config file.
type MCPServerEntry struct {
	Name    string
	Command string
	Args    []string
}

// mcpConfigFile is the shared JSON structure used by Claude Desktop, Cursor, Windsurf, Cline, etc.
type mcpConfigFile struct {
	MCPServers map[string]mcpServerDef `json:"mcpServers"`
}

type mcpServerDef struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// probeMCPConfig checks whether any of def.MCPConfigPaths exists and contains MCP server entries.
func probeMCPConfig(def ToolDefinition) (DiscoveredTool, bool) {
	return probeMCPConfigAt(def, nil)
}

// probeMCPConfigAt is the testable core of probeMCPConfig.
// pathsOverride replaces def.MCPConfigPaths when non-nil.
func probeMCPConfigAt(def ToolDefinition, pathsOverride []string) (DiscoveredTool, bool) {
	paths := def.MCPConfigPaths
	if pathsOverride != nil {
		paths = pathsOverride
	}
	for _, rawPath := range paths {
		expanded, err := ExpandPath(rawPath)
		if err != nil {
			continue
		}
		servers, err := parseMCPConfig(expanded)
		if err != nil || len(servers) == 0 {
			continue
		}
		return DiscoveredTool{
			Name:            def.Name,
			DetectionMethod: MethodMCPConfig,
			ConnectionType:  def.ConnectionType,
			Orchestratable:  def.Orchestratable,
			IngestCapable:   def.IngestCapable,
			MCPServers:      servers,
		}, true
	}
	return DiscoveredTool{}, false
}

// parseMCPConfig reads and parses an MCP config JSON file, returning its server entries.
func parseMCPConfig(path string) ([]MCPServerEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg mcpConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	var entries []MCPServerEntry
	for name, srv := range cfg.MCPServers {
		entries = append(entries, MCPServerEntry{
			Name:    name,
			Command: srv.Command,
			Args:    srv.Args,
		})
	}
	return entries, nil
}
