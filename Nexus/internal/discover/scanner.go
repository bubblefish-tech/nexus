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
	"context"
	"log/slog"
	"sync"
	"time"
)

// DiscoveredTool is an AI tool found during a scan with resolved connection details.
type DiscoveredTool struct {
	Name            string
	DetectionMethod string
	ConnectionType  string
	Endpoint        string
	Orchestratable  bool
	IngestCapable   bool
	MCPServers      []MCPServerEntry
}

// Scanner orchestrates the five-tier + general AI tool discovery scan.
type Scanner struct {
	configDir string
	timeout   time.Duration
	logger    *slog.Logger
}

// NewScanner creates a Scanner rooted at configDir.
// A nil logger falls back to slog.Default().
func NewScanner(configDir string, logger *slog.Logger) *Scanner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Scanner{
		configDir: configDir,
		timeout:   2 * time.Second,
		logger:    logger,
	}
}

// FullScan runs all five detection tiers and the general port scan concurrently.
// Results are deduplicated by tool name; the first occurrence (by tier) wins.
func (s *Scanner) FullScan(ctx context.Context) ([]DiscoveredTool, error) {
	defs, err := AllTools(s.configDir)
	if err != nil {
		defs = KnownTools
		s.logger.Warn("discover: could not load custom tools, using built-ins only", "err", err)
	}
	return s.fullScanWithDefs(ctx, defs)
}

// fullScanWithDefs is the testable core of FullScan.
func (s *Scanner) fullScanWithDefs(_ context.Context, defs []ToolDefinition) ([]DiscoveredTool, error) {
	results := make(chan []DiscoveredTool, 6)
	var wg sync.WaitGroup

	launch := func(fn func() []DiscoveredTool) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- fn()
		}()
	}

	launch(func() []DiscoveredTool { return s.runPortTier(defs) })
	launch(func() []DiscoveredTool { return s.runProcessTier(defs) })
	launch(func() []DiscoveredTool { return s.runFilesystemTier(defs) })
	launch(func() []DiscoveredTool { return s.runMCPConfigTier(defs) })
	launch(func() []DiscoveredTool { return s.runDockerTier(defs) })
	launch(func() []DiscoveredTool { return probeGeneralPorts(s.timeout) })

	go func() {
		wg.Wait()
		close(results)
	}()

	seen := make(map[string]struct{})
	var all []DiscoveredTool
	for batch := range results {
		for _, dt := range batch {
			if _, dup := seen[dt.Name]; dup {
				continue
			}
			seen[dt.Name] = struct{}{}
			all = append(all, dt)
		}
	}
	return all, nil
}

func (s *Scanner) runPortTier(defs []ToolDefinition) []DiscoveredTool {
	var out []DiscoveredTool
	for _, def := range defs {
		if def.DetectionMethod != MethodPort {
			continue
		}
		if dt, ok := probePort(def, s.timeout); ok {
			s.logger.Debug("discover: port hit", "tool", dt.Name, "port", def.DefaultPort)
			out = append(out, dt)
		}
	}
	return out
}

func (s *Scanner) runProcessTier(defs []ToolDefinition) []DiscoveredTool {
	var out []DiscoveredTool
	for _, def := range defs {
		if def.DetectionMethod != MethodProcess {
			continue
		}
		if dt, ok := probeProcess(def); ok {
			s.logger.Debug("discover: process hit", "tool", dt.Name)
			out = append(out, dt)
		}
	}
	return out
}

func (s *Scanner) runFilesystemTier(defs []ToolDefinition) []DiscoveredTool {
	var out []DiscoveredTool
	for _, def := range defs {
		if def.DetectionMethod != MethodDirectory {
			continue
		}
		if dt, ok := probeFilesystem(def); ok {
			s.logger.Debug("discover: filesystem hit", "tool", dt.Name)
			out = append(out, dt)
		}
	}
	return out
}

func (s *Scanner) runMCPConfigTier(defs []ToolDefinition) []DiscoveredTool {
	var out []DiscoveredTool
	for _, def := range defs {
		if def.DetectionMethod != MethodMCPConfig {
			continue
		}
		if dt, ok := probeMCPConfig(def); ok {
			s.logger.Debug("discover: mcp_config hit", "tool", dt.Name, "servers", len(dt.MCPServers))
			out = append(out, dt)
		}
	}
	return out
}

func (s *Scanner) runDockerTier(defs []ToolDefinition) []DiscoveredTool {
	var out []DiscoveredTool
	for _, def := range defs {
		if def.DetectionMethod != MethodDocker {
			continue
		}
		if dt, ok := probeDocker(def, s.timeout); ok {
			s.logger.Debug("discover: docker hit", "tool", dt.Name)
			out = append(out, dt)
		}
	}
	return out
}
