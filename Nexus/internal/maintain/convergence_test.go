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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/discover"
	"github.com/bubblefish-tech/nexus/internal/maintain"
	"github.com/bubblefish-tech/nexus/internal/maintain/registry"
)

// buildRegistry returns a Registry from inline JSON.
func buildRegistry(t *testing.T, raw string) *registry.Registry {
	t.Helper()
	r, err := registry.NewRegistry([]byte(raw))
	if err != nil {
		t.Fatalf("buildRegistry: %v", err)
	}
	return r
}

// noIssueRegistry has a connector for "converged-tool" with no known issues.
const noIssueRegistryJSON = `{
  "version":"1.0.0",
  "connectors":[{
    "name":"converged-tool",
    "display_name":"Converged",
    "detection":{"method":"port"},
    "health_check":{"type":"http"},
    "known_issues":[]
  }]
}`

// TestReconciler_EmptyTwin verifies Reconcile returns an empty slice for an
// empty twin regardless of registry contents.
func TestReconciler_EmptyTwin(t *testing.T) {
	tw := maintain.NewTwin()
	reg := buildRegistry(t, noIssueRegistryJSON)
	rec := maintain.NewReconciler(tw, reg)
	results := rec.Reconcile(context.Background())
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty twin, got %d", len(results))
	}
}

// TestReconciler_SkipsConvergedTool verifies a running tool with no drift is skipped.
func TestReconciler_SkipsConvergedTool(t *testing.T) {
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "converged-tool", DetectionMethod: "port"},
	})
	reg := buildRegistry(t, noIssueRegistryJSON)
	rec := maintain.NewReconciler(tw, reg)

	results := rec.Reconcile(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Skipped {
		t.Error("expected tool to be skipped (no drift, no liveness failure)")
	}
	if results[0].Err != nil {
		t.Errorf("unexpected error: %v", results[0].Err)
	}
}

// TestReconciler_SkipsMissingConnector verifies a tool not in the registry is skipped.
func TestReconciler_SkipsMissingConnector(t *testing.T) {
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "unknown-tool", DetectionMethod: "port"},
	})
	reg := buildRegistry(t, noIssueRegistryJSON)
	rec := maintain.NewReconciler(tw, reg)

	results := rec.Reconcile(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Skipped {
		t.Error("tool not in registry must be skipped")
	}
}

// TestReconciler_SkipsNoApplicableIssue verifies a tool with drift but no
// matching issue in the connector is skipped gracefully.
func TestReconciler_SkipsNoApplicableIssue(t *testing.T) {
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "converged-tool", DetectionMethod: "mcp_config"},
	})
	ts := tw.GetToolState("converged-tool")
	ts.ConfigState = map[string]any{} // empty config → drift when desired is non-empty
	tw.ComputeDesiredState(ts, map[string]any{"key": "value"})

	reg := buildRegistry(t, noIssueRegistryJSON) // connector has no known_issues
	rec := maintain.NewReconciler(tw, reg)

	results := rec.Reconcile(context.Background())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Skipped {
		t.Error("tool with drift but no known issues must be skipped")
	}
}

// TestReconciler_AppliesConfigRecipe verifies that a tool with drift has its
// fix recipe applied, updating the config file on disk.
func TestReconciler_AppliesConfigRecipe(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cfg.json")
	if err := os.WriteFile(cfgPath, []byte(`{"tool":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}

	regJSON := fmt.Sprintf(`{
      "version":"1.0.0",
      "connectors":[{
        "name":"fix-tool",
        "display_name":"Fix Tool",
        "detection":{"method":"mcp_config"},
        "health_check":{"type":"filesystem"},
        "known_issues":[{
          "id":"wrong_value",
          "description":"tool key has wrong value",
          "fix_recipe":[
            {"action":"backup_file",    "params":{"path":%q}},
            {"action":"set_config_key", "params":{"path":%q,"key":"tool","value":"nexus"}},
            {"action":"verify_config",  "params":{"path":%q}}
          ]
        }]
      }]
    }`, cfgPath, cfgPath, cfgPath)

	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "fix-tool", DetectionMethod: "mcp_config"},
	})
	ts := tw.GetToolState("fix-tool")
	ts.ConfigState = map[string]any{"tool": "old"}
	ts.ConfigPaths = []string{cfgPath}
	tw.ComputeDesiredState(ts, map[string]any{"tool": "nexus"})

	reg := buildRegistry(t, regJSON)
	rec := maintain.NewReconciler(tw, reg)
	results := rec.Reconcile(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Skipped {
		t.Fatal("expected tool to be reconciled, not skipped")
	}
	if res.Err != nil {
		t.Fatalf("Reconcile error: %v", res.Err)
	}
	if res.IssueID != "wrong_value" {
		t.Errorf("expected issue wrong_value, got %q", res.IssueID)
	}
	if res.Steps != 3 {
		t.Errorf("expected 3 steps, got %d", res.Steps)
	}

	// Verify the config file was actually updated
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("result file is not valid JSON: %v", err)
	}
	if got["tool"] != "nexus" {
		t.Errorf("expected tool=nexus in file, got %v", got["tool"])
	}
}

// TestReconciler_TemplateSubstitution verifies that {{config_path}} in a recipe
// param is replaced with the tool's actual config path before execution.
func TestReconciler_TemplateSubstitution(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcp":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}

	const regJSON = `{
      "version":"1.0.0",
      "connectors":[{
        "name":"template-tool",
        "display_name":"Template Tool",
        "detection":{"method":"mcp_config"},
        "health_check":{"type":"filesystem"},
        "known_issues":[{
          "id":"config_drift",
          "description":"mcp key has wrong value",
          "fix_recipe":[
            {"action":"set_config_key","params":{"path":"{{config_path}}","key":"mcp","value":"nexus"}}
          ]
        }]
      }]
    }`

	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "template-tool", DetectionMethod: "mcp_config"},
	})
	ts := tw.GetToolState("template-tool")
	ts.ConfigState = map[string]any{"mcp": "old"}
	ts.ConfigPaths = []string{cfgPath} // provides {{config_path}} substitution
	tw.ComputeDesiredState(ts, map[string]any{"mcp": "nexus"})

	reg := buildRegistry(t, regJSON)
	rec := maintain.NewReconciler(tw, reg)
	results := rec.Reconcile(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("Reconcile error: %v", results[0].Err)
	}

	data, _ := os.ReadFile(cfgPath)
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("file not valid JSON: %v", err)
	}
	if got["mcp"] != "nexus" {
		t.Errorf("expected mcp=nexus, got %v (template substitution may have failed)", got["mcp"])
	}
}

// TestReconciler_ContinuesAfterError verifies that a failed reconciliation for
// one tool does not prevent other tools from being reconciled.
func TestReconciler_ContinuesAfterError(t *testing.T) {
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.json")
	if err := os.WriteFile(goodPath, []byte(`{"k":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// broken-tool recipe references a missing.json — will fail verify_config
	// good-tool recipe operates on a real file — must succeed
	regJSON := fmt.Sprintf(`{
      "version":"1.0.0",
      "connectors":[
        {
          "name":"broken-tool",
          "display_name":"Broken",
          "detection":{"method":"mcp_config"},
          "health_check":{"type":"filesystem"},
          "known_issues":[{
            "id":"will_fail",
            "description":"intentional failure",
            "fix_recipe":[
              {"action":"verify_config","params":{"path":%q}}
            ]
          }]
        },
        {
          "name":"good-tool",
          "display_name":"Good",
          "detection":{"method":"mcp_config"},
          "health_check":{"type":"filesystem"},
          "known_issues":[{
            "id":"fix_k",
            "description":"k has wrong value",
            "fix_recipe":[
              {"action":"set_config_key","params":{"path":%q,"key":"k","value":"fixed"}}
            ]
          }]
        }
      ]
    }`, filepath.Join(dir, "missing.json"), goodPath)

	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "broken-tool", DetectionMethod: "mcp_config"},
		{Name: "good-tool", DetectionMethod: "mcp_config"},
	})

	brokenTS := tw.GetToolState("broken-tool")
	brokenTS.ConfigState = map[string]any{}
	tw.ComputeDesiredState(brokenTS, map[string]any{"x": "y"})

	goodTS := tw.GetToolState("good-tool")
	goodTS.ConfigState = map[string]any{"k": "old"}
	tw.ComputeDesiredState(goodTS, map[string]any{"k": "fixed"})

	reg := buildRegistry(t, regJSON)
	rec := maintain.NewReconciler(tw, reg)
	results := rec.Reconcile(context.Background())

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	byTool := map[string]maintain.ReconcileResult{}
	for _, r := range results {
		byTool[r.Tool] = r
	}

	if byTool["broken-tool"].Err == nil {
		t.Error("expected broken-tool to fail")
	}
	if byTool["good-tool"].Err != nil {
		t.Errorf("good-tool should succeed: %v", byTool["good-tool"].Err)
	}
	if byTool["good-tool"].Skipped {
		t.Error("good-tool should not be skipped")
	}
}

// TestReconciler_LivenessRecipe verifies that a "stopped" tool with a liveness
// recipe (wait_for_port) is matched and the issue is selected.
func TestReconciler_LivenessRecipe(t *testing.T) {
	const regJSON = `{
      "version":"1.0.0",
      "connectors":[{
        "name":"port-tool",
        "display_name":"Port Tool",
        "detection":{"method":"port","default_port":19999},
        "health_check":{"type":"http","url":"http://127.0.0.1:19999/health","expected_status":200},
        "known_issues":[{
          "id":"port_not_listening",
          "description":"service not running",
          "fix_recipe":[
            {"action":"wait_for_port","params":{"port":19999,"timeout_seconds":1}}
          ]
        }]
      }]
    }`

	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		// No endpoint → probeHealth returns Reachable=false → status stays "running"
		// Force status via direct assignment after Refresh
		{Name: "port-tool", DetectionMethod: "port"},
	})
	ts := tw.GetToolState("port-tool")
	ts.Status = "stopped" // simulate stopped state

	reg := buildRegistry(t, regJSON)
	rec := maintain.NewReconciler(tw, reg)
	results := rec.Reconcile(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Skipped {
		t.Fatal("stopped tool with liveness recipe should not be skipped")
	}
	// wait_for_port will fail (port 19999 is not open) — that's expected in this test.
	// We only care that the issue was selected (IssueID is set).
	if res.IssueID != "port_not_listening" {
		t.Errorf("expected issue port_not_listening, got %q", res.IssueID)
	}
}

// TestReconciler_Run_CancelsCleanly verifies Run exits promptly when ctx is cancelled.
func TestReconciler_Run_CancelsCleanly(t *testing.T) {
	tw := maintain.NewTwin()
	reg := buildRegistry(t, noIssueRegistryJSON)
	rec := maintain.NewReconciler(tw, reg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- rec.Run(ctx, 100*time.Millisecond)
	}()

	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Run did not exit after context cancellation")
	}
}

// TestReconciler_MultipleToolsAllConverged verifies Reconcile marks all tools
// skipped when none have drift or liveness failures.
func TestReconciler_MultipleToolsAllConverged(t *testing.T) {
	const regJSON = `{
      "version":"1.0.0",
      "connectors":[
        {"name":"tool-a","display_name":"A","detection":{"method":"port"},"health_check":{"type":"http"},"known_issues":[]},
        {"name":"tool-b","display_name":"B","detection":{"method":"port"},"health_check":{"type":"http"},"known_issues":[]}
      ]
    }`
	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "tool-a", DetectionMethod: "port"},
		{Name: "tool-b", DetectionMethod: "port"},
	})
	reg := buildRegistry(t, regJSON)
	rec := maintain.NewReconciler(tw, reg)

	results := rec.Reconcile(context.Background())
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Skipped {
			t.Errorf("tool %q should be skipped (converged)", r.Tool)
		}
	}
}
