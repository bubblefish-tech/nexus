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

// Behavioral verification tests for Phase 7 MCP + OpenAI + SSE.
// Covers: binding, auth, nexus_write, nexus_search, nexus_status, self-test.
package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/mcp"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// recordingPipeline records calls to Write/Search/Status for inspection in tests.
type recordingPipeline struct {
	mu          sync.Mutex
	writeParams []mcp.WriteParams
	writeResult mcp.WriteResult
	writeErr    error

	searchParams []mcp.SearchParams
	searchResult mcp.SearchResult
	searchErr    error

	statusResult mcp.StatusResult
	statusErr    error
}

func (p *recordingPipeline) Write(_ context.Context, params mcp.WriteParams) (mcp.WriteResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.writeParams = append(p.writeParams, params)
	return p.writeResult, p.writeErr
}

func (p *recordingPipeline) Search(_ context.Context, params mcp.SearchParams) (mcp.SearchResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.searchParams = append(p.searchParams, params)
	return p.searchResult, p.searchErr
}

func (p *recordingPipeline) Status(_ context.Context) (mcp.StatusResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statusResult, p.statusErr
}

// newTestPipeline returns a recordingPipeline with sane defaults.
func newTestPipeline() *recordingPipeline {
	return &recordingPipeline{
		writeResult: mcp.WriteResult{PayloadID: "test-payload-abc123", Status: "accepted"},
		searchResult: mcp.SearchResult{
			Records:        []destination.TranslatedPayload{},
			RetrievalStage: 3,
		},
		statusResult: mcp.StatusResult{Status: "ok", Version: "0.1.0", QueueDepth: 0},
	}
}

// startServer starts a test MCP server on a random loopback port and returns
// the server, its URL, and a cleanup function.
func startServer(t *testing.T, pipeline mcp.Pipeline, key string) (*mcp.Server, string, func()) {
	t.Helper()
	// Use port 0 to get a random free port — then restart on the actual port.
	// We need to find a free port first.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("startServer: find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	srv := mcp.New("127.0.0.1", port, []byte(key), "test-source", pipeline, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("startServer: start: %v", err)
	}

	baseURL := "http://" + srv.Addr() + "/mcp"
	return srv, baseURL, func() { _ = srv.Stop() }
}

// rpcCall posts a JSON-RPC 2.0 request to the MCP server and returns the
// decoded response body.
func rpcCall(t *testing.T, client *http.Client, url, key, method string, params interface{}) map[string]interface{} {
	t.Helper()

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("rpcCall: marshal params: %v", err)
		}
		rawParams = b
	}

	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if rawParams != nil {
		reqBody["params"] = rawParams
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("rpcCall: new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("rpcCall: do: %v", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("rpcCall: unmarshal response: %v\nbody: %s", err, b)
	}
	return result
}

// ---------------------------------------------------------------------------
// CHECK MCP-1: Server binds to 127.0.0.1 ONLY
// ---------------------------------------------------------------------------

func TestMCPServer_BindsLoopbackOnly(t *testing.T) {
	pipeline := newTestPipeline()
	srv, baseURL, stop := startServer(t, pipeline, "test-mcp-key-abc")
	defer stop()

	// Verify the Addr() is 127.0.0.1:port (not 0.0.0.0 or ::).
	addr := srv.Addr()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("CHECK MCP-1 FAIL: addr=%q does not start with 127.0.0.1:", addr)
	}

	// Verify the server responds at the loopback address.
	client := &http.Client{Timeout: 5 * time.Second}
	resp := rpcCall(t, client, baseURL, "test-mcp-key-abc", "ping", nil)
	if resp["error"] != nil {
		t.Fatalf("CHECK MCP-1 FAIL: ping returned error: %v", resp["error"])
	}

	t.Logf("CHECK MCP-1 PASS: MCP server bound to %s (127.0.0.1 only)", addr)
}

// Verify that trying to bind to 0.0.0.0 returns an error and does NOT start.
func TestMCPServer_RejectsNonLoopbackBind(t *testing.T) {
	pipeline := newTestPipeline()
	srv := mcp.New("0.0.0.0", 0, []byte("key"), "src", pipeline, nil)
	err := srv.Start()
	if err == nil {
		_ = srv.Stop()
		t.Fatal("CHECK MCP-1b FAIL: expected error for 0.0.0.0 bind, got nil")
	}
	t.Logf("CHECK MCP-1b PASS: 0.0.0.0 bind rejected: %v", err)
}

// ---------------------------------------------------------------------------
// CHECK MCP-2: Auth — wrong key returns error; correct key succeeds
// ---------------------------------------------------------------------------

func TestMCPServer_AuthRejectsWrongKey(t *testing.T) {
	pipeline := newTestPipeline()
	_, baseURL, stop := startServer(t, pipeline, "correct-mcp-key-xyz")
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}

	// Wrong key must be rejected.
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	req, _ := http.NewRequest(http.MethodPost, baseURL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-key-000000000")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("CHECK MCP-2 FAIL: do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("CHECK MCP-2 FAIL: wrong key returned HTTP %d, want 401", resp.StatusCode)
	}

	var result map[string]interface{}
	b, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(b, &result)
	rpcErr, _ := result["error"].(map[string]interface{})
	if rpcErr == nil {
		t.Fatalf("CHECK MCP-2 FAIL: response has no 'error' field: %s", b)
	}
	code, _ := rpcErr["code"].(float64)
	if int(code) != -32001 {
		t.Fatalf("CHECK MCP-2 FAIL: error code=%v want -32001", code)
	}

	t.Logf("CHECK MCP-2 PASS: wrong key → 401 with JSON-RPC error code -32001")
}

func TestMCPServer_AuthAcceptsCorrectKey(t *testing.T) {
	pipeline := newTestPipeline()
	_, baseURL, stop := startServer(t, pipeline, "correct-key-abcdef")
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp := rpcCall(t, client, baseURL, "correct-key-abcdef", "ping", nil)
	if resp["error"] != nil {
		t.Fatalf("CHECK MCP-2b FAIL: correct key returned error: %v", resp["error"])
	}
	t.Logf("CHECK MCP-2b PASS: correct key → ping succeeded")
}

// ---------------------------------------------------------------------------
// CHECK MCP-3: nexus_status tool returns status=ok
// ---------------------------------------------------------------------------

func TestMCPTools_NexusStatus(t *testing.T) {
	pipeline := newTestPipeline()
	_, baseURL, stop := startServer(t, pipeline, "status-key")
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}

	// First do initialize handshake.
	initResp := rpcCall(t, client, baseURL, "status-key", "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "1.0"},
	})
	if initResp["error"] != nil {
		t.Fatalf("CHECK MCP-3 FAIL: initialize returned error: %v", initResp["error"])
	}

	// Call nexus_status.
	resp := rpcCall(t, client, baseURL, "status-key", "tools/call", map[string]interface{}{
		"name":      "nexus_status",
		"arguments": map[string]interface{}{},
	})

	if resp["error"] != nil {
		t.Fatalf("CHECK MCP-3 FAIL: nexus_status returned error: %v", resp["error"])
	}

	result, _ := resp["result"].(map[string]interface{})
	if result == nil {
		t.Fatalf("CHECK MCP-3 FAIL: no result field in response")
	}

	// Parse the content[0].text JSON.
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatalf("CHECK MCP-3 FAIL: empty content array")
	}
	block, _ := content[0].(map[string]interface{})
	text, _ := block["text"].(string)

	var statusResult map[string]interface{}
	if err := json.Unmarshal([]byte(text), &statusResult); err != nil {
		t.Fatalf("CHECK MCP-3 FAIL: parse tool text: %v", err)
	}
	if statusResult["status"] != "ok" {
		t.Fatalf("CHECK MCP-3 FAIL: status=%v want ok", statusResult["status"])
	}

	t.Logf("CHECK MCP-3 PASS: nexus_status → status=%q version=%v queue_depth=%v",
		statusResult["status"], statusResult["version"], statusResult["queue_depth"])
}

// ---------------------------------------------------------------------------
// CHECK MCP-4: nexus_write tool calls pipeline.Write
// ---------------------------------------------------------------------------

func TestMCPTools_NexusWrite_CallsPipeline(t *testing.T) {
	pipeline := newTestPipeline()
	_, baseURL, stop := startServer(t, pipeline, "write-key")
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp := rpcCall(t, client, baseURL, "write-key", "tools/call", map[string]interface{}{
		"name": "nexus_write",
		"arguments": map[string]interface{}{
			"content":    "remember: dark mode preferred",
			"subject":    "user-prefs",
			"actor_type": "user",
		},
	})

	if resp["error"] != nil {
		t.Fatalf("CHECK MCP-4 FAIL: nexus_write returned error: %v", resp["error"])
	}

	result, _ := resp["result"].(map[string]interface{})
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatalf("CHECK MCP-4 FAIL: empty content")
	}
	block, _ := content[0].(map[string]interface{})
	text, _ := block["text"].(string)

	var wr map[string]interface{}
	if err := json.Unmarshal([]byte(text), &wr); err != nil {
		t.Fatalf("CHECK MCP-4 FAIL: parse write result: %v", err)
	}
	if wr["status"] != "accepted" {
		t.Fatalf("CHECK MCP-4 FAIL: status=%v want accepted", wr["status"])
	}
	if wr["payload_id"] == "" {
		t.Fatal("CHECK MCP-4 FAIL: empty payload_id")
	}

	// Verify pipeline.Write was called with the correct params.
	pipeline.mu.Lock()
	calls := len(pipeline.writeParams)
	var lastParams mcp.WriteParams
	if calls > 0 {
		lastParams = pipeline.writeParams[calls-1]
	}
	pipeline.mu.Unlock()

	if calls != 1 {
		t.Fatalf("CHECK MCP-4 FAIL: pipeline.Write called %d times, want 1", calls)
	}
	if lastParams.Content != "remember: dark mode preferred" {
		t.Fatalf("CHECK MCP-4 FAIL: content=%q want %q", lastParams.Content, "remember: dark mode preferred")
	}
	if lastParams.Source != "test-source" {
		t.Fatalf("CHECK MCP-4 FAIL: source=%q want test-source", lastParams.Source)
	}

	t.Logf("CHECK MCP-4 PASS: nexus_write routed to pipeline — payload_id=%v source=%q", wr["payload_id"], lastParams.Source)
}

func TestMCPTools_NexusWrite_MissingContent_ReturnsToolError(t *testing.T) {
	pipeline := newTestPipeline()
	_, baseURL, stop := startServer(t, pipeline, "write-key2")
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp := rpcCall(t, client, baseURL, "write-key2", "tools/call", map[string]interface{}{
		"name":      "nexus_write",
		"arguments": map[string]interface{}{}, // missing content
	})

	if resp["error"] != nil {
		t.Fatalf("CHECK MCP-4b FAIL: unexpected JSON-RPC error: %v", resp["error"])
	}

	result, _ := resp["result"].(map[string]interface{})
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Fatalf("CHECK MCP-4b FAIL: expected isError=true for missing content")
	}
	t.Logf("CHECK MCP-4b PASS: nexus_write with missing content → isError=true")
}

// ---------------------------------------------------------------------------
// CHECK MCP-5: nexus_search tool calls pipeline.Search
// ---------------------------------------------------------------------------

func TestMCPTools_NexusSearch_CallsPipeline(t *testing.T) {
	pipeline := newTestPipeline()
	_, baseURL, stop := startServer(t, pipeline, "search-key")
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp := rpcCall(t, client, baseURL, "search-key", "tools/call", map[string]interface{}{
		"name": "nexus_search",
		"arguments": map[string]interface{}{
			"q":     "dark mode",
			"limit": 5,
		},
	})

	if resp["error"] != nil {
		t.Fatalf("CHECK MCP-5 FAIL: nexus_search returned error: %v", resp["error"])
	}

	result, _ := resp["result"].(map[string]interface{})
	content, _ := result["content"].([]interface{})
	if len(content) == 0 {
		t.Fatalf("CHECK MCP-5 FAIL: empty content")
	}
	block, _ := content[0].(map[string]interface{})
	text, _ := block["text"].(string)

	var sr map[string]interface{}
	if err := json.Unmarshal([]byte(text), &sr); err != nil {
		t.Fatalf("CHECK MCP-5 FAIL: parse search result: %v", err)
	}

	// Verify pipeline.Search was called.
	pipeline.mu.Lock()
	calls := len(pipeline.searchParams)
	var lastParams mcp.SearchParams
	if calls > 0 {
		lastParams = pipeline.searchParams[calls-1]
	}
	pipeline.mu.Unlock()

	if calls != 1 {
		t.Fatalf("CHECK MCP-5 FAIL: pipeline.Search called %d times, want 1", calls)
	}
	if lastParams.Q != "dark mode" {
		t.Fatalf("CHECK MCP-5 FAIL: q=%q want %q", lastParams.Q, "dark mode")
	}

	t.Logf("CHECK MCP-5 PASS: nexus_search routed to pipeline — q=%q", lastParams.Q)
}

// ---------------------------------------------------------------------------
// CHECK MCP-6: tools/list returns 3 tools
// ---------------------------------------------------------------------------

func TestMCPTools_List_Returns3Tools(t *testing.T) {
	pipeline := newTestPipeline()
	_, baseURL, stop := startServer(t, pipeline, "list-key")
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp := rpcCall(t, client, baseURL, "list-key", "tools/list", nil)

	if resp["error"] != nil {
		t.Fatalf("CHECK MCP-6 FAIL: tools/list returned error: %v", resp["error"])
	}

	result, _ := resp["result"].(map[string]interface{})
	tools, _ := result["tools"].([]interface{})
	if len(tools) != 3 {
		t.Fatalf("CHECK MCP-6 FAIL: got %d tools, want 3", len(tools))
	}

	names := make([]string, 0, 3)
	for _, tool := range tools {
		m, _ := tool.(map[string]interface{})
		name, _ := m["name"].(string)
		names = append(names, name)
	}

	wantNames := map[string]bool{"nexus_write": true, "nexus_search": true, "nexus_status": true}
	for _, name := range names {
		if !wantNames[name] {
			t.Errorf("CHECK MCP-6 FAIL: unexpected tool name %q", name)
		}
		delete(wantNames, name)
	}
	for missing := range wantNames {
		t.Errorf("CHECK MCP-6 FAIL: missing tool %q", missing)
	}

	t.Logf("CHECK MCP-6 PASS: tools/list returned 3 tools: %v", names)
}

// ---------------------------------------------------------------------------
// CHECK MCP-7: initialize handshake returns correct protocol version
// ---------------------------------------------------------------------------

func TestMCPTools_Initialize_Handshake(t *testing.T) {
	pipeline := newTestPipeline()
	_, baseURL, stop := startServer(t, pipeline, "init-key")
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp := rpcCall(t, client, baseURL, "init-key", "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "claude-desktop", "version": "1.0"},
	})

	if resp["error"] != nil {
		t.Fatalf("CHECK MCP-7 FAIL: initialize returned error: %v", resp["error"])
	}

	result, _ := resp["result"].(map[string]interface{})
	version, _ := result["protocolVersion"].(string)
	if version != "2024-11-05" {
		t.Fatalf("CHECK MCP-7 FAIL: protocolVersion=%q want 2024-11-05", version)
	}

	serverInfo, _ := result["serverInfo"].(map[string]interface{})
	name, _ := serverInfo["name"].(string)
	if name != "bubblefish-nexus" {
		t.Fatalf("CHECK MCP-7 FAIL: serverInfo.name=%q want bubblefish-nexus", name)
	}

	t.Logf("CHECK MCP-7 PASS: initialize → protocolVersion=%q serverInfo.name=%q", version, name)
}

// ---------------------------------------------------------------------------
// CHECK MCP-8: unknown method returns method-not-found error
// ---------------------------------------------------------------------------

func TestMCPServer_UnknownMethod_ReturnsMethodNotFound(t *testing.T) {
	pipeline := newTestPipeline()
	_, baseURL, stop := startServer(t, pipeline, "method-key")
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	resp := rpcCall(t, client, baseURL, "method-key", "unknown/method", nil)

	rpcErr, _ := resp["error"].(map[string]interface{})
	if rpcErr == nil {
		t.Fatalf("CHECK MCP-8 FAIL: expected error for unknown method, got nil")
	}
	code, _ := rpcErr["code"].(float64)
	if int(code) != -32601 {
		t.Fatalf("CHECK MCP-8 FAIL: code=%v want -32601", code)
	}

	t.Logf("CHECK MCP-8 PASS: unknown method → error code -32601")
}

// ---------------------------------------------------------------------------
// CHECK MCP-9: Port conflict — Start returns error (non-fatal, daemon continues)
// ---------------------------------------------------------------------------

func TestMCPServer_PortConflict_ReturnsError(t *testing.T) {
	pipeline := newTestPipeline()

	// Occupy a port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for port: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	// Attempt to start MCP on the same port.
	srv := mcp.New("127.0.0.1", port, []byte("conflict-key"), "src", pipeline, nil)
	startErr := srv.Start()
	if startErr == nil {
		_ = srv.Stop()
		t.Fatalf("CHECK MCP-9 FAIL: expected error on port conflict, got nil")
	}

	// Verify error is non-nil and descriptive.
	t.Logf("CHECK MCP-9 PASS: port conflict → non-nil error (%v) — daemon can continue", startErr)
}

// ---------------------------------------------------------------------------
// CHECK MCP-10: Self-test — equivalent of `bubblefish mcp test`
// ---------------------------------------------------------------------------

func TestMCPSelfTest_NexusStatus_ExitsSuccessWithin5Seconds(t *testing.T) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		done <- runSelfTest()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CHECK MCP-10 FAIL: self-test failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CHECK MCP-10 FAIL: self-test did not complete within 5 seconds")
	}

	t.Logf("CHECK MCP-10 PASS: self-test (nexus_status) completed within 5 seconds")
}

// runSelfTest starts a temporary MCP server with TestPipeline, calls
// nexus_status, and returns nil on success. This mirrors the logic of
// `bubblefish mcp test`.
func runSelfTest() error {
	// Find a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("find port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	const testKey = "self-test-mcp-key-phase7"
	pipeline := &mcp.TestPipeline{}
	srv := mcp.New("127.0.0.1", port, []byte(testKey), "test", pipeline, nil)
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	defer srv.Stop()

	url := "http://" + srv.Addr() + "/mcp"
	client := &http.Client{Timeout: 4 * time.Second}

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nexus_status","arguments":{}}}`
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call nexus_status: %w", err)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(b, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if result["error"] != nil {
		return fmt.Errorf("nexus_status returned error: %v", result["error"])
	}

	rpcResult, _ := result["result"].(map[string]interface{})
	content, _ := rpcResult["content"].([]interface{})
	if len(content) == 0 {
		return fmt.Errorf("empty content in nexus_status response")
	}
	block, _ := content[0].(map[string]interface{})
	text, _ := block["text"].(string)

	var statusResult map[string]interface{}
	if err := json.Unmarshal([]byte(text), &statusResult); err != nil {
		return fmt.Errorf("parse status result: %w", err)
	}
	if statusResult["status"] != "ok" {
		return fmt.Errorf("status=%v want ok", statusResult["status"])
	}

	return nil
}
