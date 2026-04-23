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

package discover_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/discover"
)

func TestKnownTools_MinimumCount(t *testing.T) {
	t.Helper()
	const minTools = 30
	if len(discover.KnownTools) < minTools {
		t.Errorf("KnownTools has %d entries, want at least %d", len(discover.KnownTools), minTools)
	}
}

func TestKnownTools_AllFieldsValid(t *testing.T) {
	t.Helper()
	validMethods := map[string]bool{
		discover.MethodPort:      true,
		discover.MethodProcess:   true,
		discover.MethodDirectory: true,
		discover.MethodMCPConfig: true,
		discover.MethodDocker:    true,
	}
	validConnTypes := map[string]bool{
		discover.ConnOpenAICompat:   true,
		discover.ConnMCPStdio:       true,
		discover.ConnMCPSSE:         true,
		discover.ConnIngest: true,
		discover.ConnHTTPAPI:        true,
	}
	for i, tool := range discover.KnownTools {
		if tool.Name == "" {
			t.Errorf("KnownTools[%d]: empty Name", i)
		}
		if !validMethods[tool.DetectionMethod] {
			t.Errorf("KnownTools[%d] %q: unknown DetectionMethod %q", i, tool.Name, tool.DetectionMethod)
		}
		if !validConnTypes[tool.ConnectionType] {
			t.Errorf("KnownTools[%d] %q: unknown ConnectionType %q", i, tool.Name, tool.ConnectionType)
		}
		switch tool.DetectionMethod {
		case discover.MethodPort:
			if tool.DefaultPort == 0 {
				t.Errorf("KnownTools[%d] %q: port tool missing DefaultPort", i, tool.Name)
			}
			if tool.ProbeURL == "" {
				t.Errorf("KnownTools[%d] %q: port tool missing ProbeURL", i, tool.Name)
			}
		case discover.MethodProcess:
			if len(tool.ProcessNames) == 0 {
				t.Errorf("KnownTools[%d] %q: process tool missing ProcessNames", i, tool.Name)
			}
		case discover.MethodDirectory:
			if len(tool.DirectoryPaths) == 0 {
				t.Errorf("KnownTools[%d] %q: directory tool missing DirectoryPaths", i, tool.Name)
			}
		case discover.MethodMCPConfig:
			if len(tool.MCPConfigPaths) == 0 {
				t.Errorf("KnownTools[%d] %q: mcp_config tool missing MCPConfigPaths", i, tool.Name)
			}
		case discover.MethodDocker:
			if len(tool.DockerImages) == 0 {
				t.Errorf("KnownTools[%d] %q: docker tool missing DockerImages", i, tool.Name)
			}
		}
	}
}

func TestKnownTools_DetectionMethodCoverage(t *testing.T) {
	t.Helper()
	counts := map[string]int{}
	for _, tool := range discover.KnownTools {
		counts[tool.DetectionMethod]++
	}
	for _, method := range []string{
		discover.MethodPort,
		discover.MethodProcess,
		discover.MethodDirectory,
		discover.MethodMCPConfig,
		discover.MethodDocker,
	} {
		if counts[method] == 0 {
			t.Errorf("no tools defined for detection method %q", method)
		}
	}
}

func TestLoadCustomTools_MissingFile(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	tools, err := discover.LoadCustomTools(dir)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected empty slice, got %d tools", len(tools))
	}
}

func TestLoadCustomTools_ParsesTOML(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	discDir := filepath.Join(dir, "discovery")
	if err := os.MkdirAll(discDir, 0700); err != nil {
		t.Fatal(err)
	}
	tomlContent := `
[[tools]]
name = "My Custom AI"
detection_method = "port"
default_port = 9999
probe_url = "/health"
expected_response = "ok"
connection_type = "openai_compat"
orchestratable = true
ingest_capable = false
`
	if err := os.WriteFile(filepath.Join(discDir, "custom_tools.toml"), []byte(tomlContent), 0600); err != nil {
		t.Fatal(err)
	}
	tools, err := discover.LoadCustomTools(dir)
	if err != nil {
		t.Fatalf("LoadCustomTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	got := tools[0]
	if got.Name != "My Custom AI" {
		t.Errorf("Name = %q, want %q", got.Name, "My Custom AI")
	}
	if got.DefaultPort != 9999 {
		t.Errorf("DefaultPort = %d, want 9999", got.DefaultPort)
	}
	if !got.Orchestratable {
		t.Error("expected Orchestratable = true")
	}
}

func TestLoadCustomTools_InvalidTOML(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	discDir := filepath.Join(dir, "discovery")
	if err := os.MkdirAll(discDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(discDir, "custom_tools.toml"), []byte("not valid toml ][[["), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := discover.LoadCustomTools(dir)
	if err == nil {
		t.Error("expected error for invalid TOML, got nil")
	}
}

func TestAllTools_MergesCustom(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	discDir := filepath.Join(dir, "discovery")
	if err := os.MkdirAll(discDir, 0700); err != nil {
		t.Fatal(err)
	}
	tomlContent := `
[[tools]]
name = "Extra Tool"
detection_method = "port"
default_port = 1111
probe_url = "/ping"
expected_response = "pong"
connection_type = "http_api"
`
	if err := os.WriteFile(filepath.Join(discDir, "custom_tools.toml"), []byte(tomlContent), 0600); err != nil {
		t.Fatal(err)
	}
	all, err := discover.AllTools(dir)
	if err != nil {
		t.Fatalf("AllTools: %v", err)
	}
	want := len(discover.KnownTools) + 1
	if len(all) != want {
		t.Errorf("AllTools len = %d, want %d", len(all), want)
	}
	last := all[len(all)-1]
	if last.Name != "Extra Tool" {
		t.Errorf("last tool Name = %q, want %q", last.Name, "Extra Tool")
	}
}

func TestAllTools_NoCustomFile(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	all, err := discover.AllTools(dir)
	if err != nil {
		t.Fatalf("AllTools: %v", err)
	}
	if len(all) != len(discover.KnownTools) {
		t.Errorf("AllTools len = %d, want %d", len(all), len(discover.KnownTools))
	}
}

func TestExpandPath_Home(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	got, err := discover.ExpandPath("~/.nexus")
	if err != nil {
		t.Fatalf("ExpandPath: %v", err)
	}
	want := filepath.Join(home, ".nexus")
	if got != want {
		t.Errorf("ExpandPath = %q, want %q", got, want)
	}
}

func TestExpandPath_Absolute(t *testing.T) {
	t.Helper()
	p := "/some/absolute/path"
	got, err := discover.ExpandPath(p)
	if err != nil {
		t.Fatalf("ExpandPath: %v", err)
	}
	if got != p {
		t.Errorf("ExpandPath = %q, want %q", got, p)
	}
}
