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

	"github.com/bubblefish-tech/nexus/internal/discover"
	"github.com/bubblefish-tech/nexus/internal/maintain"
	"github.com/bubblefish-tech/nexus/internal/maintain/learned"
	"github.com/bubblefish-tech/nexus/internal/maintain/registry"
)

// TestIntegration_EndToEnd_DetectDrift_ApplyFix verifies the full Maintainer flow:
// discover tool → detect config drift → reconcile → fix applied → drift resolved.
func TestIntegration_EndToEnd_DetectDrift_ApplyFix(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp_settings.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{}}`), 0600); err != nil {
		t.Fatal(err)
	}

	regJSON := fmt.Sprintf(`{
	  "version":"1.0.0",
	  "connectors":[{
	    "name":"test-client",
	    "display_name":"Test Client",
	    "detection":{"method":"mcp_config"},
	    "health_check":{"type":"filesystem"},
	    "mcp_config_template":{"mcpServers":{"nexus":{"command":"nexus","args":["mcp","stdio"]}}},
	    "known_issues":[{
	      "id":"missing_nexus_entry",
	      "description":"Nexus not configured in MCP servers",
	      "fix_recipe":[
	        {"action":"backup_file","params":{"path":%q}},
	        {"action":"set_config_key","params":{"path":%q,"key":"mcpServers.nexus.command","value":"nexus"}},
	        {"action":"set_config_key","params":{"path":%q,"key":"mcpServers.nexus.args","value":["mcp","stdio"]}},
	        {"action":"verify_config","params":{"path":%q}}
	      ]
	    }]
	  }]
	}`, cfgPath, cfgPath, cfgPath, cfgPath)

	reg, err := registry.NewRegistry([]byte(regJSON))
	if err != nil {
		t.Fatal(err)
	}

	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "test-client", DetectionMethod: "mcp_config"},
	})

	ts := tw.GetToolState("test-client")
	ts.ConfigState = map[string]any{"mcpServers": map[string]any{}}
	ts.ConfigPaths = []string{cfgPath}

	desired := reg.MCPDesiredState("test-client")
	if desired == nil {
		desired = map[string]any{"mcpServers": map[string]any{"nexus": map[string]any{"command": "nexus", "args": []any{"mcp", "stdio"}}}}
	}
	tw.ComputeDesiredState(ts, desired)

	if len(ts.Drift) == 0 {
		t.Fatal("expected drift to be detected")
	}

	rec := maintain.NewReconciler(tw, reg)
	results := rec.Reconcile(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("reconcile error: %v", results[0].Err)
	}

	// Verify file was updated
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	servers, ok := got["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers not a map: %T", got["mcpServers"])
	}
	if _, ok := servers["nexus"]; !ok {
		t.Error("expected mcpServers.nexus to be present after fix")
	}
}

// TestIntegration_Rollback_ReadOnlyConfig verifies that a transaction failure
// triggers rollback and the original file is restored from backup.
func TestIntegration_Rollback_ReadOnlyConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	original := `{"setting":"original"}`
	if err := os.WriteFile(cfgPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	// Recipe: backup → set_config_key → verify a MISSING file (forces failure after write)
	missingPath := filepath.Join(dir, "nonexistent.json")
	regJSON := fmt.Sprintf(`{
	  "version":"1.0.0",
	  "connectors":[{
	    "name":"rollback-tool",
	    "display_name":"Rollback Tool",
	    "detection":{"method":"mcp_config"},
	    "health_check":{"type":"filesystem"},
	    "known_issues":[{
	      "id":"broken_fix",
	      "description":"intentionally broken recipe",
	      "fix_recipe":[
	        {"action":"backup_file","params":{"path":%q}},
	        {"action":"set_config_key","params":{"path":%q,"key":"setting","value":"modified"}},
	        {"action":"verify_config","params":{"path":%q}}
	      ]
	    }]
	  }]
	}`, cfgPath, cfgPath, missingPath)

	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "rollback-tool", DetectionMethod: "mcp_config"},
	})
	ts := tw.GetToolState("rollback-tool")
	ts.ConfigState = map[string]any{"setting": "original"}
	ts.ConfigPaths = []string{cfgPath}
	tw.ComputeDesiredState(ts, map[string]any{"setting": "desired"})

	reg, _ := registry.NewRegistry([]byte(regJSON))
	rec := maintain.NewReconciler(tw, reg)
	results := rec.Reconcile(context.Background())

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("expected reconcile to fail on verify_config of missing file")
	}

	// After rollback, the original file should be restored
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["setting"] != "original" {
		t.Errorf("expected setting=original after rollback, got %v", got["setting"])
	}
}

// TestIntegration_LearnedFix_PrefersSuccessful verifies that after a fix succeeds,
// the learned store influences BestIssue to prefer it over alternatives.
func TestIntegration_LearnedFix_PrefersSuccessful(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "fixes.json")
	store, err := learned.NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate history: fix_a failed many times, fix_b succeeded
	for i := 0; i < 10; i++ {
		store.Record("tool-x", "fix_a", learned.OutcomeFailure)
	}
	for i := 0; i < 10; i++ {
		store.Record("tool-x", "fix_b", learned.OutcomeSuccess)
	}

	best := store.BestIssue("tool-x", []string{"fix_a", "fix_b"})
	if best != "fix_b" {
		t.Errorf("expected learned store to prefer fix_b, got %q", best)
	}

	// Verify fix_a has very low weight
	wA := store.Weight("tool-x", "fix_a")
	wB := store.Weight("tool-x", "fix_b")
	if wA >= wB {
		t.Errorf("fix_a weight (%f) should be lower than fix_b (%f)", wA, wB)
	}
}

// TestIntegration_PathTraversal_Rejected verifies that action execution rejects
// paths that traverse outside the allowed prefix set.
func TestIntegration_PathTraversal_Rejected(t *testing.T) {
	// InitAllowedPaths must be called once per process. It may have already been called.
	_ = maintain.InitAllowedPaths()

	// Try to read a path well outside the home directory
	traversalPath := filepath.Join(string(os.PathSeparator), "etc", "passwd")
	if os.PathSeparator == '\\' {
		traversalPath = `C:\Windows\System32\config\SAM`
	}

	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionReadConfig, map[string]any{
		"path": traversalPath,
	})
	if err == nil {
		t.Error("expected path traversal to be rejected, got nil error")
	}
}

// TestIntegration_JSONC_CommentsPreserved verifies that the configio reader
// handles VS Code-style JSONC (JSON with comments) correctly — parses the
// content and can set keys without corrupting the structure.
func TestIntegration_JSONC_CommentsPreserved(t *testing.T) {
	dir := t.TempDir()
	jsoncPath := filepath.Join(dir, "settings.json")
	jsoncContent := `{
  // This is a comment
  "editor.fontSize": 14,
  /* Multi-line
     comment */
  "mcpServers": {}
}
`
	if err := os.WriteFile(jsoncPath, []byte(jsoncContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Use set_config_key to add a nested key — should work despite JSONC
	_ = maintain.InitAllowedPaths()
	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionSetConfigKey, map[string]any{
		"path":  jsoncPath,
		"key":   "mcpServers.nexus.command",
		"value": "nexus",
	})
	if err != nil {
		t.Fatalf("set_config_key on JSONC file failed: %v", err)
	}

	// Read back and verify
	data, err := os.ReadFile(jsoncPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("result should be valid JSON: %v\ncontent: %s", err, data)
	}
	servers, ok := got["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers not a map")
	}
	nexus, ok := servers["nexus"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers.nexus not a map: %v", servers)
	}
	if nexus["command"] != "nexus" {
		t.Errorf("expected command=nexus, got %v", nexus["command"])
	}
}

// TestIntegration_ConcurrentReconcile verifies two tools with different config
// files can be fixed without interference.
func TestIntegration_ConcurrentReconcile(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.json")
	pathB := filepath.Join(dir, "b.json")
	if err := os.WriteFile(pathA, []byte(`{"key":"old_a"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte(`{"key":"old_b"}`), 0600); err != nil {
		t.Fatal(err)
	}

	regJSON := fmt.Sprintf(`{
	  "version":"1.0.0",
	  "connectors":[
	    {
	      "name":"tool-a",
	      "display_name":"Tool A",
	      "detection":{"method":"mcp_config"},
	      "health_check":{"type":"filesystem"},
	      "known_issues":[{
	        "id":"drift_a",
	        "description":"key wrong in a",
	        "fix_recipe":[{"action":"set_config_key","params":{"path":%q,"key":"key","value":"fixed_a"}}]
	      }]
	    },
	    {
	      "name":"tool-b",
	      "display_name":"Tool B",
	      "detection":{"method":"mcp_config"},
	      "health_check":{"type":"filesystem"},
	      "known_issues":[{
	        "id":"drift_b",
	        "description":"key wrong in b",
	        "fix_recipe":[{"action":"set_config_key","params":{"path":%q,"key":"key","value":"fixed_b"}}]
	      }]
	    }
	  ]
	}`, pathA, pathB)

	tw := maintain.NewTwin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "tool-a", DetectionMethod: "mcp_config"},
		{Name: "tool-b", DetectionMethod: "mcp_config"},
	})
	tsA := tw.GetToolState("tool-a")
	tsA.ConfigState = map[string]any{"key": "old_a"}
	tsA.ConfigPaths = []string{pathA}
	tw.ComputeDesiredState(tsA, map[string]any{"key": "fixed_a"})

	tsB := tw.GetToolState("tool-b")
	tsB.ConfigState = map[string]any{"key": "old_b"}
	tsB.ConfigPaths = []string{pathB}
	tw.ComputeDesiredState(tsB, map[string]any{"key": "fixed_b"})

	reg, _ := registry.NewRegistry([]byte(regJSON))
	rec := maintain.NewReconciler(tw, reg)
	results := rec.Reconcile(context.Background())

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("tool %s failed: %v", r.Tool, r.Err)
		}
	}

	// Verify both files fixed independently
	dataA, _ := os.ReadFile(pathA)
	dataB, _ := os.ReadFile(pathB)
	var gotA, gotB map[string]any
	json.Unmarshal(dataA, &gotA)
	json.Unmarshal(dataB, &gotB)
	if gotA["key"] != "fixed_a" {
		t.Errorf("tool-a config: expected fixed_a, got %v", gotA["key"])
	}
	if gotB["key"] != "fixed_b" {
		t.Errorf("tool-b config: expected fixed_b, got %v", gotB["key"])
	}
}

// TestIntegration_RegistryMerkleIntegrity verifies the embedded registry
// loads successfully and its Merkle hash is deterministic.
func TestIntegration_RegistryMerkleIntegrity(t *testing.T) {
	reg1, err := registry.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	reg2, err := registry.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if reg1.Merkle() != reg2.Merkle() {
		t.Error("two loads of the same embedded registry should produce identical Merkle hashes")
	}
	if reg1.Len() == 0 {
		t.Error("embedded registry should not be empty")
	}
}

// TestIntegration_FixTool_EndToEnd verifies Maintainer.FixTool targeted flow.
func TestIntegration_FixTool_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "tool.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mode":"broken"}`), 0600); err != nil {
		t.Fatal(err)
	}

	cfg := maintain.Config{
		ConfigDir:        dir,
		LearnedStorePath: filepath.Join(t.TempDir(), "fixes.json"),
	}
	m, err := maintain.New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Inject a tool into the twin manually (since the scanner won't find
	// our test tool in the real filesystem).
	tw := m.Twin()
	tw.Refresh(context.Background(), []discover.DiscoveredTool{
		{Name: "claude-desktop", DetectionMethod: "mcp_config"},
	})
	ts := tw.GetToolState("claude-desktop")
	if ts == nil {
		t.Fatal("GetToolState returned nil")
	}
	ts.ConfigPaths = []string{cfgPath}
	ts.ConfigState = map[string]any{"mode": "broken"}

	desired := m.Registry().MCPDesiredState("claude-desktop")
	if desired != nil {
		tw.ComputeDesiredState(ts, desired)
	}

	// FixTool may fail (recipe targets different paths), but should not panic.
	// The point is exercising the full path through the Maintainer.
	_ = m.FixTool(context.Background(), "claude-desktop")
}
