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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ── Port probe ────────────────────────────────────────────────────────────────

func TestProbePort_Hit(t *testing.T) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			fmt.Fprint(w, `{"version":"0.1.0"}`)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	def := ToolDefinition{
		Name:             "TestTool",
		DetectionMethod:  MethodPort,
		ProbeURL:         "/api/version",
		ExpectedResponse: "version",
		ConnectionType:   ConnOpenAICompat,
		DefaultEndpoint:  ts.URL,
		Orchestratable:   true,
	}
	dt, ok := probePortAt(def, ts.URL, 2*time.Second)
	if !ok {
		t.Fatal("expected hit")
	}
	if dt.Name != "TestTool" {
		t.Errorf("name = %q, want TestTool", dt.Name)
	}
	if dt.ConnectionType != ConnOpenAICompat {
		t.Errorf("connection_type = %q, want openai_compat", dt.ConnectionType)
	}
}

func TestProbePort_Miss_WrongBody(t *testing.T) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"ok"}`)
	}))
	defer ts.Close()

	def := ToolDefinition{
		Name:             "TestTool",
		DetectionMethod:  MethodPort,
		ProbeURL:         "/api/version",
		ExpectedResponse: "version",
		ConnectionType:   ConnOpenAICompat,
	}
	_, ok := probePortAt(def, ts.URL, 2*time.Second)
	if ok {
		t.Fatal("expected miss for wrong response body")
	}
}

func TestProbePort_Miss_NoServer(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:             "TestTool",
		DetectionMethod:  MethodPort,
		DefaultPort:      19999,
		ProbeURL:         "/api/version",
		ExpectedResponse: "version",
		ConnectionType:   ConnOpenAICompat,
	}
	_, ok := probePort(def, 200*time.Millisecond)
	if ok {
		t.Fatal("expected miss when no server is running")
	}
}

// ── Process probe ─────────────────────────────────────────────────────────────

func TestProbeProcess_Hit(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:           "MyTool",
		DetectionMethod: MethodProcess,
		ProcessNames:   []string{"mytool"},
		ConnectionType: ConnMCPStdio,
		IngestCapable:  true,
	}
	lister := func() ([]string, error) { return []string{"bash", "mytool", "go"}, nil }
	dt, ok := probeProcessWithLister(def, lister)
	if !ok {
		t.Fatal("expected hit")
	}
	if dt.Name != "MyTool" {
		t.Errorf("name = %q, want MyTool", dt.Name)
	}
	if !dt.IngestCapable {
		t.Error("expected IngestCapable=true")
	}
}

func TestProbeProcess_Miss(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:            "MyTool",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"mytool"},
		ConnectionType:  ConnMCPStdio,
	}
	lister := func() ([]string, error) { return []string{"bash", "go", "vim"}, nil }
	_, ok := probeProcessWithLister(def, lister)
	if ok {
		t.Fatal("expected miss")
	}
}

func TestProbeProcess_WindowsExeSuffix(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:            "Cursor",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"Cursor"},
		ConnectionType:  ConnMCPStdio,
	}
	lister := func() ([]string, error) { return []string{"Cursor.exe"}, nil }
	_, ok := probeProcessWithLister(def, lister)
	if !ok {
		t.Fatal("expected hit with .exe suffix stripped")
	}
}

func TestProbeProcess_ListerError(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:            "MyTool",
		DetectionMethod: MethodProcess,
		ProcessNames:    []string{"mytool"},
		ConnectionType:  ConnMCPStdio,
	}
	lister := func() ([]string, error) { return nil, errors.New("permission denied") }
	_, ok := probeProcessWithLister(def, lister)
	if ok {
		t.Fatal("expected miss on lister error")
	}
}

// ── Filesystem probe ──────────────────────────────────────────────────────────

func TestProbeFilesystem_Hit(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "myapp")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	def := ToolDefinition{
		Name:            "MyApp",
		DetectionMethod: MethodDirectory,
		ConnectionType:  ConnMCPStdio,
		IngestCapable:   true,
	}
	dt, ok := probeFilesystemWithPaths(def, []string{sub})
	if !ok {
		t.Fatal("expected hit")
	}
	if dt.DetectionMethod != MethodDirectory {
		t.Errorf("DetectionMethod = %q, want directory", dt.DetectionMethod)
	}
}

func TestProbeFilesystem_Miss(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:            "MyApp",
		DetectionMethod: MethodDirectory,
		ConnectionType:  ConnMCPStdio,
	}
	_, ok := probeFilesystemWithPaths(def, []string{"/no/such/path/xyz123"})
	if ok {
		t.Fatal("expected miss for non-existent path")
	}
}

// ── MCP config probe ──────────────────────────────────────────────────────────

func TestProbeMCPConfig_Hit(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfg := mcpConfigFile{
		MCPServers: map[string]mcpServerDef{
			"myserver": {Command: "node", Args: []string{"server.js"}},
		},
	}
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	def := ToolDefinition{
		Name:            "MyCursor",
		DetectionMethod: MethodMCPConfig,
		ConnectionType:  ConnMCPStdio,
		IngestCapable:   true,
	}
	dt, ok := probeMCPConfigAt(def, []string{cfgPath})
	if !ok {
		t.Fatal("expected hit")
	}
	if len(dt.MCPServers) != 1 {
		t.Fatalf("MCPServers len = %d, want 1", len(dt.MCPServers))
	}
	if dt.MCPServers[0].Command != "node" {
		t.Errorf("command = %q, want node", dt.MCPServers[0].Command)
	}
}

func TestProbeMCPConfig_Miss_NoFile(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:            "MyCursor",
		DetectionMethod: MethodMCPConfig,
		ConnectionType:  ConnMCPStdio,
	}
	_, ok := probeMCPConfigAt(def, []string{"/no/such/mcp.json"})
	if ok {
		t.Fatal("expected miss for missing file")
	}
}

func TestProbeMCPConfig_Miss_EmptyServers(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	data, _ := json.Marshal(mcpConfigFile{MCPServers: map[string]mcpServerDef{}})
	cfgPath := filepath.Join(dir, "mcp.json")
	os.WriteFile(cfgPath, data, 0o600) //nolint:errcheck

	def := ToolDefinition{
		Name:            "MyCursor",
		DetectionMethod: MethodMCPConfig,
		ConnectionType:  ConnMCPStdio,
	}
	_, ok := probeMCPConfigAt(def, []string{cfgPath})
	if ok {
		t.Fatal("expected miss for empty mcpServers")
	}
}

// ── Docker probe ──────────────────────────────────────────────────────────────

func TestProbeDocker_Hit(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:            "Ollama",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"ollama/ollama"},
		ConnectionType:  ConnOpenAICompat,
		DefaultEndpoint: "http://localhost:11434",
		Orchestratable:  true,
	}
	reader := func(_ time.Duration) ([]byte, error) {
		return []byte("ollama/ollama\nghcr.io/open-webui/open-webui\n"), nil
	}
	dt, ok := probeDockerWithReader(def, reader, 2*time.Second)
	if !ok {
		t.Fatal("expected hit")
	}
	if dt.Name != "Ollama" {
		t.Errorf("name = %q, want Ollama", dt.Name)
	}
}

func TestProbeDocker_Miss(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:            "vLLM",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"vllm/vllm-openai"},
		ConnectionType:  ConnOpenAICompat,
	}
	reader := func(_ time.Duration) ([]byte, error) {
		return []byte("ollama/ollama\n"), nil
	}
	_, ok := probeDockerWithReader(def, reader, 2*time.Second)
	if ok {
		t.Fatal("expected miss")
	}
}

func TestProbeDocker_DockerUnavailable(t *testing.T) {
	t.Helper()
	def := ToolDefinition{
		Name:            "Ollama",
		DetectionMethod: MethodDocker,
		DockerImages:    []string{"ollama/ollama"},
		ConnectionType:  ConnOpenAICompat,
	}
	reader := func(_ time.Duration) ([]byte, error) {
		return nil, errors.New("docker: command not found")
	}
	_, ok := probeDockerWithReader(def, reader, 2*time.Second)
	if ok {
		t.Fatal("expected miss when docker unavailable")
	}
}

// ── General port scan ─────────────────────────────────────────────────────────

func TestScanPorts_Hit(t *testing.T) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"object":"list","data":[]}`)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Probe fake port 19998; baseURLOf always returns ts.URL for testability.
	results := scanPorts([]int{19998}, func(_ int) string { return ts.URL }, 2*time.Second)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ConnectionType != ConnOpenAICompat {
		t.Errorf("connection_type = %q, want openai_compat", results[0].ConnectionType)
	}
}

func TestScanPorts_Miss(t *testing.T) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"unknown"}`)
	}))
	defer ts.Close()

	results := scanPorts([]int{19997}, func(_ int) string { return ts.URL }, 2*time.Second)
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestGeneralPortList_Coverage(t *testing.T) {
	t.Helper()
	ports := generalPortList()
	if len(ports) < 200 {
		t.Errorf("expected ≥200 ports in general scan list, got %d", len(ports))
	}
	// Spot-check key ports are present.
	required := []int{11434, 4891, 7860, 8000, 3000, 1234}
	pm := make(map[int]bool, len(ports))
	for _, p := range ports {
		pm[p] = true
	}
	for _, p := range required {
		if !pm[p] {
			t.Errorf("port %d missing from general port list", p)
		}
	}
}

// ── Scanner integration ───────────────────────────────────────────────────────

func TestScanner_FullScan_Empty(t *testing.T) {
	t.Helper()
	s := NewScanner(t.TempDir(), nil)
	results, err := s.fullScanWithDefs(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results with empty defs, got %d", len(results))
	}
}

func TestScanner_Deduplication(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "tooldir")
	os.Mkdir(sub, 0o700) //nolint:errcheck

	// Two defs with the same Name: one filesystem, one process (won't match anything).
	defs := []ToolDefinition{
		{
			Name:            "DupTool",
			DetectionMethod: MethodDirectory,
			DirectoryPaths:  []string{sub},
			ConnectionType:  ConnMCPStdio,
		},
		{
			Name:            "DupTool",
			DetectionMethod: MethodDirectory,
			DirectoryPaths:  []string{sub},
			ConnectionType:  ConnOpenAICompat,
		},
	}

	s := NewScanner(t.TempDir(), nil)
	results, err := s.fullScanWithDefs(context.Background(), defs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	count := 0
	for _, r := range results {
		if r.Name == "DupTool" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 DupTool after dedup, got %d", count)
	}
}
