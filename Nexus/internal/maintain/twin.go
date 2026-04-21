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

package maintain

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"runtime"
	"sync"
	"time"

	"github.com/bubblefish-tech/nexus/internal/discover"
	"github.com/bubblefish-tech/nexus/internal/maintain/topology"
)

// ToolState is the twin's complete model of one AI tool: identity, runtime
// status, health, parsed config, and the divergence from desired state.
type ToolState struct {
	Name            string
	Status          string         // "running", "stopped", "unknown"
	DetectionMethod string         // "port", "process", "filesystem", "mcp_config", "docker"
	Port            int            // 0 if not port-detected
	ProcessPID      int            // 0 if not process-detected
	ConfigPaths     []string       // known config file paths for this tool
	ConfigState     map[string]any // parsed config values relevant to Nexus
	Version         string         // detected version string if available
	Health          HealthState
	DesiredState    map[string]any // what Nexus wants the config to look like
	Drift           []DriftEntry   // current vs desired differences
	LastUpdated     time.Time
}

// DriftEntry describes one deviation between actual and desired configuration.
type DriftEntry struct {
	Field   string // keypath e.g. "mcpServers.nexus"
	Actual  any    // current value (nil if key is missing)
	Desired any    // what it should be
}

// HealthState tracks a tool's API liveness.
type HealthState struct {
	Reachable  bool
	LatencyMs  int
	LastCheck  time.Time
	ErrorCount int
}

// NetworkTopology is an alias for the real type defined in the topology sub-package.
// Re-exported here so callers of the maintain package do not need to import topology directly.
type NetworkTopology = topology.NetworkTopology

// EnvironmentTwin is a continuously-synchronised in-memory model of every
// discovered AI tool's state, health, and configuration drift.
// Inspired by NASA's Apollo spacecraft digital twin: a complete software model
// of the real system, maintained in lockstep with reality.
type EnvironmentTwin struct {
	mu           sync.RWMutex
	tools        map[string]*ToolState
	platform     string // runtime.GOOS: "windows", "linux", "darwin"
	topology     *NetworkTopology
	NexusMCPPort int
	NexusAPIPort int
}

// NewTwin returns an empty EnvironmentTwin for the current platform.
func NewTwin() *EnvironmentTwin {
	return &EnvironmentTwin{
		tools:    make(map[string]*ToolState),
		platform: runtime.GOOS,
	}
}

// Refresh synchronises the twin with the latest discovery output. Newly seen
// tools are added; existing tools have their runtime state and health updated.
// Tools absent from the new scan are marked "unknown" (not deleted — history
// is preserved until the next explicit purge).
func (t *EnvironmentTwin) Refresh(ctx context.Context, discoveries []discover.DiscoveredTool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	seen := make(map[string]bool, len(discoveries))
	for _, d := range discoveries {
		seen[d.Name] = true
		ts, exists := t.tools[d.Name]
		if !exists {
			ts = &ToolState{Name: d.Name}
			t.tools[d.Name] = ts
		}
		ts.DetectionMethod = d.DetectionMethod
		ts.Status = "running"
		ts.LastUpdated = time.Now().UTC()
		ts.Health = t.probeHealth(ctx, d)
		if !ts.Health.Reachable && d.Endpoint != "" {
			ts.Status = "stopped"
		}
	}
	// Mark absent tools as unknown — not deleted so drift history is preserved
	for name, ts := range t.tools {
		if !seen[name] {
			ts.Status = "unknown"
		}
	}
}

// probeHealth performs a 2-second HTTP health probe against the tool's endpoint.
// Returns an empty HealthState (Reachable=false) for tools with no endpoint.
func (t *EnvironmentTwin) probeHealth(ctx context.Context, d discover.DiscoveredTool) HealthState {
	if d.Endpoint == "" {
		return HealthState{LastCheck: time.Now().UTC()}
	}
	client := &http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	resp, err := client.Get(d.Endpoint)
	latencyMs := int(time.Since(start).Milliseconds())
	if err != nil {
		return HealthState{
			Reachable:  false,
			LatencyMs:  latencyMs,
			LastCheck:  time.Now().UTC(),
			ErrorCount: 1,
		}
	}
	resp.Body.Close()
	return HealthState{
		Reachable: resp.StatusCode < 500,
		LatencyMs: latencyMs,
		LastCheck: time.Now().UTC(),
	}
}

// GetToolState returns the state for a named tool, or nil if not tracked.
func (t *EnvironmentTwin) GetToolState(name string) *ToolState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.tools[name]
}

// AllTools returns a snapshot slice of all tracked tool states.
func (t *EnvironmentTwin) AllTools() []*ToolState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]*ToolState, 0, len(t.tools))
	for _, ts := range t.tools {
		out = append(out, ts)
	}
	return out
}

// DriftReport returns all drift entries across every tracked tool.
func (t *EnvironmentTwin) DriftReport() []DriftEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var all []DriftEntry
	for _, ts := range t.tools {
		all = append(all, ts.Drift...)
	}
	return all
}

// ComputeDesiredState sets the desired config for a tool and recomputes its drift.
// The desired map comes from the connector registry (W5). Passing nil clears drift.
func (t *EnvironmentTwin) ComputeDesiredState(tool *ToolState, desired map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tool.DesiredState = desired
	tool.Drift = computeDrift(tool.ConfigState, desired)
}

// computeDrift compares actual vs desired and returns one DriftEntry per
// diverging or missing key. Keys present in actual but absent from desired
// are ignored (Nexus does not remove unexpected config).
func computeDrift(actual, desired map[string]any) []DriftEntry {
	if desired == nil {
		return nil
	}
	var entries []DriftEntry
	for field, want := range desired {
		have, ok := actual[field]
		if !ok {
			entries = append(entries, DriftEntry{Field: field, Actual: nil, Desired: want})
			continue
		}
		if !reflect.DeepEqual(have, want) {
			entries = append(entries, DriftEntry{Field: field, Actual: have, Desired: want})
		}
	}
	return entries
}

// Platform returns the OS platform string ("windows", "linux", "darwin").
func (t *EnvironmentTwin) Platform() string {
	return t.platform
}

// ToolCount returns the number of tools currently tracked by the twin.
func (t *EnvironmentTwin) ToolCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.tools)
}

// SetTopology stores the resolved network topology (populated by W7).
func (t *EnvironmentTwin) SetTopology(top *NetworkTopology) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.topology = top
}

// Topology returns the current network topology (nil before W7 initialises it).
func (t *EnvironmentTwin) Topology() *NetworkTopology {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.topology
}

// String returns a one-line summary for structured logging.
func (t *EnvironmentTwin) String() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return fmt.Sprintf("EnvironmentTwin{platform=%s tools=%d}", t.platform, len(t.tools))
}
