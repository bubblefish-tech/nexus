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

// Package registry holds the connector registry: a Merkle-verified, Ed25519-signed
// catalogue of every AI tool Nexus knows how to detect, configure, and repair.
// The registry sub-package deliberately does NOT import the parent maintain package
// so that convergence.go can import both without an import cycle.
package registry

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// RawStep mirrors maintain.Step without importing the parent package.
// convergence.go converts []RawStep → []maintain.Step before executing.
type RawStep struct {
	Action string         `json:"action"`
	Params map[string]any `json:"params"`
}

// KnownIssue pairs a human-readable issue description with a fix recipe.
type KnownIssue struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	FixRecipe   []RawStep `json:"fix_recipe"`
}

// DetectionConfig describes how to detect whether a tool is present.
type DetectionConfig struct {
	Method       string              `json:"method"`          // "port", "process", "filesystem", "mcp_config", "docker"
	PlatformPaths map[string][]string `json:"platform_paths"` // os → []path
	ConfigFormat string              `json:"config_format"`
	DefaultPort  int                 `json:"default_port"`
	Endpoint     string              `json:"endpoint"`
	ImagePrefix  string              `json:"image_prefix"`  // docker image prefix
	ProcessNames []string            `json:"process_names"` // process detection
}

// MCPConfigTemplate describes the key path and value Nexus injects into an MCP host.
type MCPConfigTemplate struct {
	KeyPath string `json:"key_path"` // dot-notation path e.g. "mcpServers.nexus"
	Value   any    `json:"value"`
}

// RuntimeAPIConfig describes how to talk to a tool's inference API.
type RuntimeAPIConfig struct {
	ConnectionType string `json:"connection_type"` // "openai_compat"
	BaseURL        string `json:"base_url"`
	HealthEndpoint string `json:"health_endpoint"`
}

// HealthCheckConfig describes how to probe a tool's liveness.
type HealthCheckConfig struct {
	Type           string              `json:"type"` // "http", "filesystem", "process"
	URL            string              `json:"url"`
	ExpectedStatus int                 `json:"expected_status"`
	PlatformPaths  map[string][]string `json:"platform_paths"`
	ProcessNames   []string            `json:"process_names"`
}

// Connector is the complete model for one AI tool in the registry.
type Connector struct {
	Name        string             `json:"name"`
	DisplayName string             `json:"display_name"`
	Detection   DetectionConfig    `json:"detection"`
	MCPTemplate *MCPConfigTemplate `json:"mcp_config_template"`
	RuntimeAPI  *RuntimeAPIConfig  `json:"runtime_api"`
	HealthCheck HealthCheckConfig  `json:"health_check"`
	KnownIssues []KnownIssue       `json:"known_issues"`
}

// registryFile is the top-level JSON schema for connectors.json.
type registryFile struct {
	Version    string      `json:"version"`
	Connectors []Connector `json:"connectors"`
}

// Registry is a thread-safe in-memory index of all known connectors.
// Callers build one from JSON via NewRegistry and optionally extend it
// with custom connectors via Merge.
type Registry struct {
	mu         sync.RWMutex
	connectors map[string]*Connector // keyed by Connector.Name
	merkle     [32]byte              // SHA-256 over sorted connector names
}

// NewRegistry parses raw JSON bytes and returns a Registry.
func NewRegistry(data []byte) (*Registry, error) {
	var rf registryFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("registry: parse failed: %w", err)
	}
	r := &Registry{connectors: make(map[string]*Connector, len(rf.Connectors))}
	for i := range rf.Connectors {
		c := &rf.Connectors[i]
		r.connectors[c.Name] = c
	}
	r.recomputeMerkle()
	return r, nil
}

// ConnectorFor returns the connector for the named tool, or (nil, false).
func (r *Registry) ConnectorFor(name string) (*Connector, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.connectors[name]
	return c, ok
}

// AllConnectors returns a snapshot of every connector, sorted by name.
func (r *Registry) AllConnectors() []*Connector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Connector, 0, len(r.connectors))
	for _, c := range r.connectors {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RecipeFor returns the fix recipe for the named tool and issue ID.
// Returns nil if the tool or issue is unknown.
func (r *Registry) RecipeFor(tool, issueID string) []RawStep {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.connectors[tool]
	if !ok {
		return nil
	}
	for _, ki := range c.KnownIssues {
		if ki.ID == issueID {
			return ki.FixRecipe
		}
	}
	return nil
}

// MCPDesiredState returns the map[string]any desired config for tool's MCP
// template (used by convergence to compute drift against the actual config).
// Returns nil if the connector has no MCP template.
func (r *Registry) MCPDesiredState(tool string) map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.connectors[tool]
	if !ok || c.MCPTemplate == nil {
		return nil
	}
	parts := strings.Split(c.MCPTemplate.KeyPath, ".")
	return buildNestedMap(parts, c.MCPTemplate.Value)
}

// Merkle returns the registry content hash (SHA-256 over sorted names).
func (r *Registry) Merkle() [32]byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.merkle
}

// Merge adds or replaces connectors from another registry (e.g. remote update).
// The new connectors' Merkle hash is verified before merging; if verification
// fails the registry is unchanged and an error is returned.
func (r *Registry) Merge(other *Registry, expectedMerkle [32]byte) error {
	if other.Merkle() != expectedMerkle {
		return fmt.Errorf("registry: Merkle mismatch — refusing to merge untrusted connectors")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, c := range other.connectors {
		r.connectors[name] = c
	}
	r.recomputeMerkle()
	return nil
}

// Len returns the number of connectors in the registry.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.connectors)
}

// recomputeMerkle must be called with r.mu held (write).
func (r *Registry) recomputeMerkle() {
	names := make([]string, 0, len(r.connectors))
	for n := range r.connectors {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
	}
	copy(r.merkle[:], h.Sum(nil))
}

// buildNestedMap constructs a nested map from a dot-split key path and a leaf value.
// e.g. ["mcpServers","nexus"] + val → {"mcpServers":{"nexus":val}}
func buildNestedMap(parts []string, val any) map[string]any {
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		return map[string]any{parts[0]: val}
	}
	return map[string]any{parts[0]: buildNestedMap(parts[1:], val)}
}
