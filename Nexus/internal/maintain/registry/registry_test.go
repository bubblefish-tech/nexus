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

package registry_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/ed25519"

	"github.com/bubblefish-tech/nexus/internal/maintain/registry"
)

// minimalJSON is a valid registry with two connectors for fast unit tests.
const minimalJSON = `{
  "version": "1.0.0",
  "connectors": [
    {
      "name": "ollama",
      "display_name": "Ollama",
      "detection": {"method": "port", "default_port": 11434, "endpoint": "http://127.0.0.1:11434"},
      "health_check": {"type": "http", "url": "http://127.0.0.1:11434/api/tags", "expected_status": 200},
      "known_issues": [
        {
          "id": "port_not_listening",
          "description": "Ollama not running",
          "fix_recipe": [
            {"action": "restart_process", "params": {"name": "ollama", "args": ["serve"]}},
            {"action": "wait_for_port",   "params": {"port": 11434}}
          ]
        }
      ]
    },
    {
      "name": "claude-desktop",
      "display_name": "Claude Desktop",
      "detection": {"method": "mcp_config"},
      "mcp_config_template": {
        "key_path": "mcpServers.nexus",
        "value": {"command": "nexus", "args": ["mcp-stdio"]}
      },
      "health_check": {"type": "filesystem"},
      "known_issues": []
    }
  ]
}`

func TestRegistry_ConnectorFor(t *testing.T) {
	r, err := registry.NewRegistry([]byte(minimalJSON))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	c, ok := r.ConnectorFor("ollama")
	if !ok {
		t.Fatal("ollama not found")
	}
	if c.Name != "ollama" {
		t.Errorf("expected ollama, got %q", c.Name)
	}
	if _, ok := r.ConnectorFor("nonexistent"); ok {
		t.Error("expected false for unknown connector")
	}
}

func TestRegistry_AllConnectors_Sorted(t *testing.T) {
	r, err := registry.NewRegistry([]byte(minimalJSON))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	all := r.AllConnectors()
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
	// claude-desktop < ollama alphabetically
	if all[0].Name != "claude-desktop" || all[1].Name != "ollama" {
		t.Errorf("not sorted: %v, %v", all[0].Name, all[1].Name)
	}
}

func TestRegistry_RecipeFor(t *testing.T) {
	r, err := registry.NewRegistry([]byte(minimalJSON))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	steps := r.RecipeFor("ollama", "port_not_listening")
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].Action != "restart_process" {
		t.Errorf("expected restart_process, got %q", steps[0].Action)
	}
	if steps[1].Action != "wait_for_port" {
		t.Errorf("expected wait_for_port, got %q", steps[1].Action)
	}
	if r.RecipeFor("ollama", "unknown-issue") != nil {
		t.Error("expected nil for unknown issue")
	}
	if r.RecipeFor("nonexistent", "port_not_listening") != nil {
		t.Error("expected nil for unknown tool")
	}
}

func TestRegistry_MCPDesiredState(t *testing.T) {
	r, err := registry.NewRegistry([]byte(minimalJSON))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	state := r.MCPDesiredState("claude-desktop")
	if state == nil {
		t.Fatal("MCPDesiredState returned nil")
	}
	servers, ok := state["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("expected mcpServers map, got %T", state["mcpServers"])
	}
	nexus, ok := servers["nexus"].(map[string]any)
	if !ok {
		t.Fatalf("expected nexus map, got %T", servers["nexus"])
	}
	if nexus["command"] != "nexus" {
		t.Errorf("expected command=nexus, got %v", nexus["command"])
	}
	if r.MCPDesiredState("ollama") != nil {
		t.Error("expected nil for tool with no MCP template")
	}
	if r.MCPDesiredState("nonexistent") != nil {
		t.Error("expected nil for unknown tool")
	}
}

func TestRegistry_Merkle_Deterministic(t *testing.T) {
	r1, _ := registry.NewRegistry([]byte(minimalJSON))
	r2, _ := registry.NewRegistry([]byte(minimalJSON))
	if r1.Merkle() != r2.Merkle() {
		t.Error("same input must produce same Merkle hash")
	}
}

func TestRegistry_Len(t *testing.T) {
	r, err := registry.NewRegistry([]byte(minimalJSON))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if r.Len() != 2 {
		t.Errorf("expected 2, got %d", r.Len())
	}
}

func TestRegistry_Merge_ValidMerkle(t *testing.T) {
	base, _ := registry.NewRegistry([]byte(minimalJSON))
	extra := `{"version":"1.0.0","connectors":[{"name":"custom-tool","display_name":"Custom","detection":{"method":"port"},"health_check":{"type":"http"},"known_issues":[]}]}`
	other, _ := registry.NewRegistry([]byte(extra))

	expectedMerkle := other.Merkle()
	if err := base.Merge(other, expectedMerkle); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if _, ok := base.ConnectorFor("custom-tool"); !ok {
		t.Error("custom-tool should appear after merge")
	}
}

func TestRegistry_Merge_MerkleMismatch(t *testing.T) {
	base, _ := registry.NewRegistry([]byte(minimalJSON))
	other, _ := registry.NewRegistry([]byte(minimalJSON))
	var badMerkle [32]byte
	if err := base.Merge(other, badMerkle); err == nil {
		t.Error("expected error for Merkle mismatch")
	}
}

func TestRegistry_InvalidJSON(t *testing.T) {
	if _, err := registry.NewRegistry([]byte("not json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadEmbedded(t *testing.T) {
	r, err := registry.LoadEmbedded()
	if err != nil {
		t.Fatalf("LoadEmbedded: %v", err)
	}
	if r.Len() == 0 {
		t.Error("embedded registry must not be empty")
	}
	// spot-check that core connectors are present
	for _, name := range []string{"claude-desktop", "cursor", "ollama", "lm-studio"} {
		if _, ok := r.ConnectorFor(name); !ok {
			t.Errorf("embedded registry missing %q", name)
		}
	}
}

func TestVerifyHash_Valid(t *testing.T) {
	data := []byte("hello registry")
	hash := registry.ContentHash(data)
	if err := registry.VerifyHash(data, hash); err != nil {
		t.Errorf("VerifyHash should pass for correct hash: %v", err)
	}
}

func TestVerifyHash_Invalid(t *testing.T) {
	data := []byte("hello registry")
	badHash := hex.EncodeToString(make([]byte, 32))
	if err := registry.VerifyHash(data, badHash); err == nil {
		t.Error("expected error for wrong hash")
	}
}

func TestVerifyPayload_Ed25519(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	registry.SetRegistryPublicKey(pub)
	t.Cleanup(func() { registry.SetRegistryPublicKey(make([]byte, ed25519.PublicKeySize)) })

	data := []byte(`{"version":"1.0.0","connectors":[]}`)
	sig := ed25519.Sign(priv, data)
	hash := registry.ContentHash(data)

	if err := registry.VerifyPayload(data, sig, hash); err != nil {
		t.Errorf("VerifyPayload with valid key/sig should pass: %v", err)
	}

	// Wrong signature must fail
	sig[0] ^= 0xFF
	if err := registry.VerifyPayload(data, sig, hash); err == nil {
		t.Error("expected error for bad signature")
	}
}

func TestTrySyncRemote_Success(t *testing.T) {
	data := []byte(minimalJSON)
	hash := registry.ContentHash(data)
	manifest, _ := json.Marshal(map[string]string{"sha256": hash})

	var connectorsServed, manifestServed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/connectors.json":
			connectorsServed = true
			w.Write(data)
		case "/manifest.json":
			manifestServed = true
			w.Write(manifest)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	r, err := registry.TrySyncRemote(context.Background(), registry.SyncOptions{
		ConnectorsURL: srv.URL + "/connectors.json",
		ManifestURL:   srv.URL + "/manifest.json",
	})
	if err != nil {
		t.Fatalf("TrySyncRemote: %v", err)
	}
	if !connectorsServed || !manifestServed {
		t.Error("expected both endpoints to be hit")
	}
	if r.Len() != 2 {
		t.Errorf("expected 2 connectors, got %d", r.Len())
	}
}

func TestTrySyncRemote_NetworkError(t *testing.T) {
	_, err := registry.TrySyncRemote(context.Background(), registry.SyncOptions{
		ConnectorsURL: "http://127.0.0.1:1/connectors.json",
		ManifestURL:   "http://127.0.0.1:1/manifest.json",
	})
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

func TestTrySyncRemote_HashMismatch(t *testing.T) {
	data := []byte(minimalJSON)
	badHash := hex.EncodeToString(make([]byte, 32))
	manifest, _ := json.Marshal(map[string]string{"sha256": badHash})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/connectors.json":
			w.Write(data)
		case "/manifest.json":
			w.Write(manifest)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	_, err := registry.TrySyncRemote(context.Background(), registry.SyncOptions{
		ConnectorsURL: srv.URL + "/connectors.json",
		ManifestURL:   srv.URL + "/manifest.json",
	})
	if err == nil {
		t.Error("expected error for hash mismatch")
	}
}

func TestLoadWithFallback_UsesEmbeddedOnFailure(t *testing.T) {
	r := registry.LoadWithFallback(context.Background(), registry.SyncOptions{
		ConnectorsURL: "http://127.0.0.1:1/connectors.json",
		ManifestURL:   "http://127.0.0.1:1/manifest.json",
	})
	if r == nil {
		t.Fatal("LoadWithFallback must never return nil")
	}
	if r.Len() == 0 {
		t.Error("fallback registry must not be empty")
	}
}

func TestRegistry_CustomToolMerge(t *testing.T) {
	base, _ := registry.LoadEmbedded()
	initial := base.Len()

	customJSON := `{"version":"1.0.0","connectors":[{"name":"my-private-llm","display_name":"Private LLM","detection":{"method":"port","default_port":9999},"health_check":{"type":"http","url":"http://127.0.0.1:9999/health","expected_status":200},"known_issues":[]}]}`
	custom, err := registry.NewRegistry([]byte(customJSON))
	if err != nil {
		t.Fatalf("parse custom: %v", err)
	}
	if err := base.Merge(custom, custom.Merkle()); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if base.Len() != initial+1 {
		t.Errorf("expected %d, got %d", initial+1, base.Len())
	}
	if _, ok := base.ConnectorFor("my-private-llm"); !ok {
		t.Error("custom connector not found after merge")
	}
}
