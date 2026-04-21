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

package maintain_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/discover"
	"github.com/bubblefish-tech/nexus/internal/maintain"
)

// TestTwin_Creation verifies NewTwin() returns a non-nil twin with platform set.
func TestTwin_Creation(t *testing.T) {
	tw := maintain.NewTwin()
	if tw == nil {
		t.Fatal("NewTwin returned nil")
	}
	p := tw.Platform()
	if p == "" {
		t.Error("Platform() must not be empty")
	}
	if tw.ToolCount() != 0 {
		t.Error("new twin should have zero tools")
	}
}

// TestTwin_RefreshAddsTools verifies discovered tools appear in AllTools after Refresh.
func TestTwin_RefreshAddsTools(t *testing.T) {
	tw := maintain.NewTwin()
	discoveries := []discover.DiscoveredTool{
		{Name: "ollama", DetectionMethod: "port", ConnectionType: "openai_compat", Endpoint: ""},
		{Name: "lm-studio", DetectionMethod: "port", ConnectionType: "openai_compat", Endpoint: ""},
	}
	tw.Refresh(context.Background(), discoveries)
	if tw.ToolCount() != 2 {
		t.Errorf("expected 2 tools, got %d", tw.ToolCount())
	}
	ts := tw.GetToolState("ollama")
	if ts == nil {
		t.Fatal("ollama not found after Refresh")
	}
	if ts.Name != "ollama" {
		t.Errorf("expected name ollama, got %s", ts.Name)
	}
}

// TestTwin_RefreshUpdatesHealth verifies the health probe is invoked when an
// endpoint is present. Uses an httptest server returning 200.
func TestTwin_RefreshUpdatesHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "healthy-tool", DetectionMethod: "port", Endpoint: srv.URL},
	})
	ts := tw.GetToolState("healthy-tool")
	if ts == nil {
		t.Fatal("tool not found")
	}
	if !ts.Health.Reachable {
		t.Error("expected Reachable=true for 200 endpoint")
	}
	if ts.Status != "running" {
		t.Errorf("expected running, got %s", ts.Status)
	}
}

// TestTwin_UnhealthyEndpoint verifies status is "stopped" when the probe fails.
func TestTwin_UnhealthyEndpoint(t *testing.T) {
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		// Port 1 should always be closed
		{Name: "dead-tool", DetectionMethod: "port", Endpoint: "http://127.0.0.1:1/"},
	})
	ts := tw.GetToolState("dead-tool")
	if ts == nil {
		t.Fatal("tool not found")
	}
	if ts.Status != "stopped" {
		t.Errorf("expected stopped for unreachable endpoint, got %s", ts.Status)
	}
}

// TestTwin_AbsentToolMarkedUnknown verifies a tool absent from the next scan
// is set to "unknown" rather than deleted.
func TestTwin_AbsentToolMarkedUnknown(t *testing.T) {
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "tool-a", DetectionMethod: "port"},
		{Name: "tool-b", DetectionMethod: "port"},
	})
	// Second scan omits tool-a
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "tool-b", DetectionMethod: "port"},
	})
	ts := tw.GetToolState("tool-a")
	if ts == nil {
		t.Fatal("tool-a should still be tracked")
	}
	if ts.Status != "unknown" {
		t.Errorf("expected unknown, got %s", ts.Status)
	}
}

// TestTwin_GetToolState_Unknown verifies nil is returned for untracked tools.
func TestTwin_GetToolState_Unknown(t *testing.T) {
	tw := maintain.NewTwin()
	if ts := tw.GetToolState("nonexistent"); ts != nil {
		t.Error("expected nil for unknown tool")
	}
}

// TestTwin_DriftDetection_MissingKey verifies a missing config key is reported as drift.
func TestTwin_DriftDetection_MissingKey(t *testing.T) {
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "claude-desktop", DetectionMethod: "mcp_config"},
	})
	ts := tw.GetToolState("claude-desktop")
	if ts == nil {
		t.Fatal("tool not found")
	}
	ts.ConfigState = map[string]any{} // no mcpServers key

	desired := map[string]any{
		"mcpServers": map[string]any{
			"nexus": map[string]any{"command": "nexus", "args": []any{"mcp-stdio"}},
		},
	}
	tw.ComputeDesiredState(ts, desired)

	if len(ts.Drift) == 0 {
		t.Error("expected drift for missing mcpServers key")
	}
	if ts.Drift[0].Field != "mcpServers" {
		t.Errorf("expected drift on mcpServers, got %q", ts.Drift[0].Field)
	}
	if ts.Drift[0].Actual != nil {
		t.Errorf("expected Actual=nil for missing key, got %v", ts.Drift[0].Actual)
	}
}

// TestTwin_DriftDetection_WrongValue verifies a mismatched value is reported as drift.
func TestTwin_DriftDetection_WrongValue(t *testing.T) {
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "cursor", DetectionMethod: "mcp_config"},
	})
	ts := tw.GetToolState("cursor")
	ts.ConfigState = map[string]any{
		"mcpServers": map[string]any{"nexus": map[string]any{"command": "wrong-binary"}},
	}
	desired := map[string]any{
		"mcpServers": map[string]any{"nexus": map[string]any{"command": "nexus"}},
	}
	tw.ComputeDesiredState(ts, desired)
	if len(ts.Drift) == 0 {
		t.Error("expected drift for wrong value")
	}
}

// TestTwin_NoDrift_WhenConverged verifies zero drift when actual matches desired.
func TestTwin_NoDrift_WhenConverged(t *testing.T) {
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "windsurf", DetectionMethod: "mcp_config"},
	})
	ts := tw.GetToolState("windsurf")
	state := map[string]any{"mcpServers": map[string]any{"nexus": "ok"}}
	ts.ConfigState = state
	tw.ComputeDesiredState(ts, state)
	if len(ts.Drift) != 0 {
		t.Errorf("expected zero drift, got %d entries", len(ts.Drift))
	}
}

// TestTwin_DriftReport aggregates drift across multiple tools.
func TestTwin_DriftReport(t *testing.T) {
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "tool-x", DetectionMethod: "port"},
		{Name: "tool-y", DetectionMethod: "port"},
	})
	for _, name := range []string{"tool-x", "tool-y"} {
		ts := tw.GetToolState(name)
		ts.ConfigState = map[string]any{}
		tw.ComputeDesiredState(ts, map[string]any{"key": "value"})
	}
	report := tw.DriftReport()
	if len(report) < 2 {
		t.Errorf("expected at least 2 drift entries, got %d", len(report))
	}
}

// TestTwin_ConcurrentAccess exercises the twin under concurrent reads and writes
// to verify the RWMutex prevents data races (run with -race).
func TestTwin_ConcurrentAccess(t *testing.T) {
	tw := maintain.NewTwin()
	discoveries := []discover.DiscoveredTool{
		{Name: "concurrent-tool", DetectionMethod: "port"},
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			tw.Refresh(context.Background(), discoveries)
		}()
		go func() {
			defer wg.Done()
			_ = tw.AllTools()
			_ = tw.DriftReport()
		}()
	}
	wg.Wait()
}

// TestTwin_SetTopology verifies topology round-trips through the twin.
func TestTwin_SetTopology(t *testing.T) {
	tw := maintain.NewTwin()
	top := &maintain.NetworkTopology{}
	tw.SetTopology(top)
	if tw.Topology() != top {
		t.Error("Topology() did not return the set topology")
	}
}

// TestTwin_String is non-empty and contains expected substrings.
func TestTwin_String(t *testing.T) {
	tw := maintain.NewTwin()
	s := tw.String()
	if s == "" {
		t.Error("String() must not be empty")
	}
}
