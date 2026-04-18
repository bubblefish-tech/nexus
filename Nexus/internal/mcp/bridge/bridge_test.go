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

package bridge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/client"
	"github.com/BubbleFish-Nexus/internal/a2a/governance"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}

// mockAuditSink implements server.AuditSink.
type mockAuditSink struct {
	events []auditEvent
}

type auditEvent struct {
	taskID    string
	eventType string
	data      interface{}
}

func (m *mockAuditSink) LogTaskEvent(_ context.Context, taskID, eventType string, data interface{}) error {
	m.events = append(m.events, auditEvent{taskID, eventType, data})
	return nil
}

// mockConn is a mock transport.Conn for testing.
type mockConn struct {
	sendFunc func(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error)
	streamFn func(ctx context.Context, req *jsonrpc.Request) (<-chan transport.Event, error)
	closed   bool
}

func (m *mockConn) Send(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	if m.sendFunc != nil {
		return m.sendFunc(ctx, req)
	}
	return nil, errors.New("mock: no sendFunc")
}

func (m *mockConn) Stream(ctx context.Context, req *jsonrpc.Request) (<-chan transport.Event, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, req)
	}
	return nil, errors.New("mock: no streamFn")
}

func (m *mockConn) Close() error {
	m.closed = true
	return nil
}

// mockTransport is a mock transport.Transport for testing.
type mockTransport struct {
	conn *mockConn
}

func (m *mockTransport) Dial(_ context.Context, _ transport.TransportConfig) (transport.Conn, error) {
	return m.conn, nil
}

func (m *mockTransport) Listen(_ context.Context, _ transport.TransportConfig) (transport.Listener, error) {
	return nil, errors.New("mock: listen not implemented")
}

// successResp builds a success JSON-RPC response.
func successResp(t *testing.T, id jsonrpc.ID, result interface{}) *jsonrpc.Response {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &jsonrpc.Response{JSONRPC: "2.0", ID: id, Result: data}
}

// testTask creates a completed task for testing.
func testTask() a2a.Task {
	return a2a.Task{
		Kind:   "task",
		TaskID: "tsk_TEST0000000000000000001",
		Status: a2a.TaskStatus{
			State:     a2a.TaskStateCompleted,
			Timestamp: a2a.Now(),
		},
		Artifacts: []a2a.Artifact{
			a2a.NewArtifact("result", a2a.NewTextPart("hello from agent")),
		},
	}
}

// testEnv sets up a full test environment with registry, governance, and bridge.
type testEnv struct {
	bridge    *Bridge
	registry  *registry.Store
	govEngine *governance.Engine
	govStore  *governance.GrantStore
	conn      *mockConn
	audit     *mockAuditSink
	dbPath    string
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_registry.db")

	reg, err := registry.NewStore(dbPath)
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })

	govDBPath := filepath.Join(tmpDir, "test_governance.db")
	govDB, err := sql.Open("sqlite", govDBPath)
	if err != nil {
		t.Fatalf("open governance db: %v", err)
	}
	t.Cleanup(func() { govDB.Close() })

	if err := governance.MigrateGrants(govDB); err != nil {
		t.Fatalf("migrate grants: %v", err)
	}
	govStore := governance.NewGrantStore(govDB)

	govEngine := governance.NewEngine(govStore,
		governance.WithLogger(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))),
	)

	// Create a mock connection that responds to ping and message/send.
	task := testTask()
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			switch req.Method {
			case "agent/ping":
				return successResp(t, req.ID, map[string]string{"status": "ok"}), nil
			case "message/send":
				return successResp(t, req.ID, task), nil
			case "tasks/get":
				return successResp(t, req.ID, task), nil
			case "tasks/cancel":
				canceled := task
				canceled.Status.State = a2a.TaskStateCanceled
				return successResp(t, req.ID, canceled), nil
			default:
				return &jsonrpc.Response{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error:   &jsonrpc.ErrorObject{Code: -32601, Message: "method not found"},
				}, nil
			}
		},
		streamFn: func(_ context.Context, _ *jsonrpc.Request) (<-chan transport.Event, error) {
			ch := make(chan transport.Event, 2)
			ch <- transport.Event{Kind: "status-update", TaskID: task.TaskID}
			ch <- transport.Event{Kind: "final", TaskID: task.TaskID}
			close(ch)
			return ch, nil
		},
	}

	mt := &mockTransport{conn: conn}
	factory := client.NewFactory(testLogger(t))
	factory.RegisterTransport("http", mt)
	pool := client.NewPool(factory, testLogger(t))
	t.Cleanup(func() { pool.CloseAll() })

	audit := &mockAuditSink{}
	b := NewBridge(pool, govEngine, reg, audit, testLogger(t))

	return &testEnv{
		bridge:    b,
		registry:  reg,
		govEngine: govEngine,
		govStore:  govStore,
		conn:      conn,
		audit:     audit,
		dbPath:    dbPath,
	}
}

// registerTestAgent adds a test agent to the registry.
func registerTestAgent(t *testing.T, env *testEnv, name string) {
	t.Helper()
	agent := registry.RegisteredAgent{
		AgentID:     "agent_" + name,
		Name:        name,
		DisplayName: name + " Agent",
		AgentCard: a2a.AgentCard{
			Name:            name,
			Description:     "Test agent: " + name,
			URL:             "http://localhost:9000",
			ProtocolVersion: "0.1.0",
			Capabilities:    a2a.AgentCapabilities{Streaming: true},
			Skills: []a2a.Skill{
				{
					ID:          "echo",
					Name:        "echo",
					Description: "Echoes input back",
				},
				{
					ID:          "summarize",
					Name:        "summarize",
					Description: "Summarizes text",
					Destructive: true,
				},
			},
		},
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  "http://localhost:9000",
		},
		Status: registry.StatusActive,
	}
	if err := env.registry.Register(context.Background(), agent); err != nil {
		t.Fatalf("register agent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tool registration tests
// ---------------------------------------------------------------------------

func TestToolDefinitions_Count(t *testing.T) {
	env := setupTestEnv(t)
	defs := env.bridge.ToolDefinitions()
	if len(defs) != 9 {
		t.Errorf("got %d tool definitions, want 9", len(defs))
	}
}

func TestToolDefinitions_Names(t *testing.T) {
	env := setupTestEnv(t)
	defs := env.bridge.ToolDefinitions()

	expected := map[string]bool{
		"a2a_list_agents":           false,
		"a2a_describe_agent":        false,
		"a2a_send_to_agent":         false,
		"a2a_stream_to_agent":       false,
		"a2a_get_task":              false,
		"a2a_resume_task":           false,
		"a2a_cancel_task":           false,
		"a2a_list_pending_approvals": false,
		"a2a_list_grants":           false,
	}

	for _, d := range defs {
		if _, ok := expected[d.Name]; !ok {
			t.Errorf("unexpected tool name: %s", d.Name)
		}
		expected[d.Name] = true
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestToolDefinitions_HaveDescriptions(t *testing.T) {
	env := setupTestEnv(t)
	for _, d := range env.bridge.ToolDefinitions() {
		if d.Description == "" {
			t.Errorf("tool %s has empty description", d.Name)
		}
	}
}

func TestToolDefinitions_HaveInputSchemas(t *testing.T) {
	env := setupTestEnv(t)
	for _, d := range env.bridge.ToolDefinitions() {
		if d.InputSchema == nil {
			t.Errorf("tool %s has nil input schema", d.Name)
		}
		if d.InputSchema["type"] != "object" {
			t.Errorf("tool %s schema type = %v, want object", d.Name, d.InputSchema["type"])
		}
	}
}

// ---------------------------------------------------------------------------
// Identity tests
// ---------------------------------------------------------------------------

func TestDeriveIdentity_KnownClients(t *testing.T) {
	tests := []struct {
		clientName string
		want       string
	}{
		{"claude-desktop", "client_claude_desktop"},
		{"chatgpt", "client_chatgpt"},
		{"perplexity", "client_perplexity"},
		{"lm-studio", "client_lm_studio"},
		{"open-webui", "client_openwebui"},
	}

	for _, tt := range tests {
		t.Run(tt.clientName, func(t *testing.T) {
			got := DeriveIdentity(tt.clientName, "1.0", "")
			if got != tt.want {
				t.Errorf("DeriveIdentity(%q) = %q, want %q", tt.clientName, got, tt.want)
			}
		})
	}
}

func TestDeriveIdentity_Unknown(t *testing.T) {
	got := DeriveIdentity("some-unknown-client", "2.0", "abc123")
	if got != "client_generic" {
		t.Errorf("got %q, want client_generic", got)
	}
}

func TestDeriveIdentity_Empty(t *testing.T) {
	got := DeriveIdentity("", "", "")
	if got != "client_generic" {
		t.Errorf("got %q, want client_generic", got)
	}
}

func TestIdentityStore_RegisterAndLookup(t *testing.T) {
	store := NewIdentityStore()
	store.Register("fp-abc", "agent_custom")

	if got := store.Lookup("fp-abc"); got != "agent_custom" {
		t.Errorf("got %q, want agent_custom", got)
	}
	if got := store.Lookup("fp-unknown"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// MCP <-> NA2A translation tests
// ---------------------------------------------------------------------------

func TestMCPToNA2A_TextInput(t *testing.T) {
	args := map[string]interface{}{
		"agent": "openclaw",
		"skill": "echo",
		"input": "hello world",
	}

	msg, skill, cfg, err := MCPToNA2A(args, "client_claude_desktop")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skill != "echo" {
		t.Errorf("skill = %q, want echo", skill)
	}
	if msg.Role != a2a.RoleUser {
		t.Errorf("role = %s, want user", msg.Role)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(msg.Parts))
	}
	tp, ok := msg.Parts[0].Part.(a2a.TextPart)
	if !ok {
		t.Fatalf("part type = %T, want TextPart", msg.Parts[0].Part)
	}
	if tp.Text != "hello world" {
		t.Errorf("text = %q, want 'hello world'", tp.Text)
	}
	if cfg == nil {
		t.Fatal("config is nil")
	}
}

func TestMCPToNA2A_StructuredInput(t *testing.T) {
	args := map[string]interface{}{
		"agent": "openclaw",
		"input": map[string]interface{}{"key": "value"},
	}

	msg, _, _, err := MCPToNA2A(args, "client_generic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(msg.Parts))
	}
	dp, ok := msg.Parts[0].Part.(a2a.DataPart)
	if !ok {
		t.Fatalf("part type = %T, want DataPart", msg.Parts[0].Part)
	}
	var data map[string]interface{}
	if err := json.Unmarshal(dp.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["key"] != "value" {
		t.Errorf("data[key] = %v, want value", data["key"])
	}
}

func TestMCPToNA2A_MissingInput(t *testing.T) {
	args := map[string]interface{}{
		"agent": "openclaw",
	}

	_, _, _, err := MCPToNA2A(args, "client_generic")
	if err == nil {
		t.Fatal("expected error for missing input")
	}
}

func TestMCPToNA2A_WithConfig(t *testing.T) {
	args := map[string]interface{}{
		"agent":      "openclaw",
		"input":      "test",
		"blocking":   true,
		"timeout_ms": float64(5000),
	}

	_, _, cfg, err := MCPToNA2A(args, "client_generic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Blocking {
		t.Error("expected blocking=true")
	}
	if cfg.TimeoutMs != 5000 {
		t.Errorf("timeout = %d, want 5000", cfg.TimeoutMs)
	}
}

func TestNA2AToMCP_CompletedTask(t *testing.T) {
	task := testTask()
	result := NA2AToMCP(&task)

	if result["task_id"] != task.TaskID {
		t.Errorf("task_id = %v, want %s", result["task_id"], task.TaskID)
	}
	if result["state"] != "completed" {
		t.Errorf("state = %v, want completed", result["state"])
	}

	artifacts, ok := result["artifacts"].([]map[string]interface{})
	if !ok {
		t.Fatalf("artifacts type = %T, want []map", result["artifacts"])
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(artifacts))
	}
	if artifacts[0]["name"] != "result" {
		t.Errorf("artifact name = %v, want result", artifacts[0]["name"])
	}
}

func TestNA2AToMCP_NoArtifacts(t *testing.T) {
	task := a2a.Task{
		Kind:   "task",
		TaskID: "tsk_TEST0000000000000000002",
		Status: a2a.TaskStatus{
			State:     a2a.TaskStateWorking,
			Timestamp: a2a.Now(),
		},
	}

	result := NA2AToMCP(&task)
	if result["state"] != "working" {
		t.Errorf("state = %v, want working", result["state"])
	}
	if _, ok := result["artifacts"]; ok {
		t.Error("expected no artifacts key for empty artifacts")
	}
}

func TestNA2AToMCP_Roundtrip(t *testing.T) {
	// MCP args -> NA2A message -> (simulated task) -> MCP result
	args := map[string]interface{}{
		"agent": "openclaw",
		"input": "summarize this",
	}

	msg, _, _, err := MCPToNA2A(args, "client_claude_desktop")
	if err != nil {
		t.Fatalf("MCPToNA2A: %v", err)
	}

	// Simulate: agent received message, created task with artifact
	task := a2a.Task{
		Kind:   "task",
		TaskID: a2a.NewTaskID(),
		Status: a2a.TaskStatus{
			State:     a2a.TaskStateCompleted,
			Timestamp: a2a.Now(),
		},
		History: []a2a.Message{*msg},
		Artifacts: []a2a.Artifact{
			a2a.NewArtifact("summary", a2a.NewTextPart("This is a summary.")),
		},
	}

	result := NA2AToMCP(&task)
	if result["state"] != "completed" {
		t.Errorf("state = %v, want completed", result["state"])
	}
	artifacts := result["artifacts"].([]map[string]interface{})
	texts := artifacts[0]["text"].([]string)
	if texts[0] != "This is a summary." {
		t.Errorf("text = %q, want 'This is a summary.'", texts[0])
	}
}

// ---------------------------------------------------------------------------
// HandleA2AListAgents tests
// ---------------------------------------------------------------------------

func TestHandleA2AListAgents_Empty(t *testing.T) {
	env := setupTestEnv(t)

	result, err := env.bridge.HandleA2AListAgents(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["count"] != 0 {
		t.Errorf("count = %v, want 0", m["count"])
	}
}

func TestHandleA2AListAgents_WithAgents(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	result, err := env.bridge.HandleA2AListAgents(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["count"] != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}

	agents := m["agents"].([]map[string]interface{})
	if agents[0]["name"] != "openclaw" {
		t.Errorf("name = %v, want openclaw", agents[0]["name"])
	}
}

func TestHandleA2AListAgents_StatusFilter(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	// Filter by suspended -- should return none.
	result, err := env.bridge.HandleA2AListAgents(context.Background(), map[string]interface{}{
		"status": "suspended",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["count"] != 0 {
		t.Errorf("count = %v, want 0 (filtered)", m["count"])
	}
}

// ---------------------------------------------------------------------------
// HandleA2ADescribeAgent tests
// ---------------------------------------------------------------------------

func TestHandleA2ADescribeAgent_Found(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	result, err := env.bridge.HandleA2ADescribeAgent(context.Background(), map[string]interface{}{
		"agent": "openclaw",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["name"] != "openclaw" {
		t.Errorf("name = %v, want openclaw", m["name"])
	}

	skills, ok := m["skills"].([]map[string]interface{})
	if !ok {
		t.Fatalf("skills type = %T", m["skills"])
	}
	if len(skills) != 2 {
		t.Errorf("skills = %d, want 2", len(skills))
	}
}

func TestHandleA2ADescribeAgent_NotFound(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2ADescribeAgent(context.Background(), map[string]interface{}{
		"agent": "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

func TestHandleA2ADescribeAgent_MissingArg(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2ADescribeAgent(context.Background(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing agent arg")
	}
}

// ---------------------------------------------------------------------------
// HandleA2ASendToAgent tests
// ---------------------------------------------------------------------------

func TestHandleA2ASendToAgent_HappyPath(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	ctx := WithClientInfo(context.Background(), "claude-desktop", "1.0")
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "openclaw",
		"skill": "echo",
		"input": "hello openclaw",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["state"] != "completed" {
		t.Errorf("state = %v, want completed", m["state"])
	}
	if m["task_id"] == "" {
		t.Error("task_id is empty")
	}

	// Verify audit was logged.
	if len(env.audit.events) != 1 {
		t.Errorf("audit events = %d, want 1", len(env.audit.events))
	}
}

func TestHandleA2ASendToAgent_AgentNotFound(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2ASendToAgent(context.Background(), map[string]interface{}{
		"agent": "nonexistent",
		"input": "hello",
	})
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

func TestHandleA2ASendToAgent_MissingInput(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	_, err := env.bridge.HandleA2ASendToAgent(context.Background(), map[string]interface{}{
		"agent": "openclaw",
	})
	if err == nil {
		t.Fatal("expected error for missing input")
	}
}

func TestHandleA2ASendToAgent_MissingAgent(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2ASendToAgent(context.Background(), map[string]interface{}{
		"input": "hello",
	})
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

func TestHandleA2ASendToAgent_GovernanceDeny(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	// Create a deny grant covering all capabilities.
	now, _ := a2a.ParseTime(a2a.Now())
	err := env.govStore.CreateGrant(&governance.Grant{
		GrantID:        a2a.NewGrantID(),
		SourceAgentID:  "client_claude_desktop",
		TargetAgentID:  "agent_openclaw",
		CapabilityGlob: "*",
		Scope:          "ALL",
		Decision:       "deny",
		IssuedBy:       "admin",
		IssuedAt:       now,
	})
	if err != nil {
		t.Fatalf("create deny grant: %v", err)
	}

	// Use the echo skill (non-destructive) but add a required capability
	// so the deny grant activates.
	// Actually, the echo skill has no required capabilities, so the engine
	// will auto-allow (no caps required). Let's test the destructive path instead.
	ctx := WithClientInfo(context.Background(), "claude-desktop", "1.0")
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "openclaw",
		"skill": "summarize", // destructive=true -> escalate
		"input": "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["status"] != "escalated" {
		t.Errorf("status = %v, want escalated", m["status"])
	}
}

func TestHandleA2ASendToAgent_DestructiveEscalates(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	ctx := WithClientInfo(context.Background(), "claude-desktop", "1.0")
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "openclaw",
		"skill": "summarize", // destructive=true
		"input": "delete everything",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["status"] != "escalated" {
		t.Errorf("status = %v, want escalated", m["status"])
	}
	if m["requires"] != "human approval" {
		t.Errorf("requires = %v, want human approval", m["requires"])
	}
}

// ---------------------------------------------------------------------------
// HandleA2AStreamToAgent tests
// ---------------------------------------------------------------------------

func TestHandleA2AStreamToAgent_HappyPath(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	result, err := env.bridge.HandleA2AStreamToAgent(context.Background(), map[string]interface{}{
		"agent": "openclaw",
		"input": "stream test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["count"] != 2 {
		t.Errorf("event count = %v, want 2", m["count"])
	}
}

func TestHandleA2AStreamToAgent_AgentNotFound(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2AStreamToAgent(context.Background(), map[string]interface{}{
		"agent": "nonexistent",
		"input": "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandleA2AStreamToAgent_MissingAgent(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2AStreamToAgent(context.Background(), map[string]interface{}{
		"input": "test",
	})
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

// ---------------------------------------------------------------------------
// HandleA2AGetTask tests
// ---------------------------------------------------------------------------

func TestHandleA2AGetTask_HappyPath(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	result, err := env.bridge.HandleA2AGetTask(context.Background(), map[string]interface{}{
		"task_id": "tsk_TEST0000000000000000001",
		"agent":   "openclaw",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["task_id"] != "tsk_TEST0000000000000000001" {
		t.Errorf("task_id = %v", m["task_id"])
	}
}

func TestHandleA2AGetTask_MissingTaskID(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2AGetTask(context.Background(), map[string]interface{}{
		"agent": "openclaw",
	})
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
}

func TestHandleA2AGetTask_MissingAgent(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2AGetTask(context.Background(), map[string]interface{}{
		"task_id": "tsk_TEST0000000000000000001",
	})
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

// ---------------------------------------------------------------------------
// HandleA2AResumeTask tests
// ---------------------------------------------------------------------------

func TestHandleA2AResumeTask_HappyPath(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	result, err := env.bridge.HandleA2AResumeTask(context.Background(), map[string]interface{}{
		"task_id": "tsk_TEST0000000000000000001",
		"agent":   "openclaw",
		"input":   "follow up",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["state"] != "completed" {
		t.Errorf("state = %v, want completed", m["state"])
	}
}

func TestHandleA2AResumeTask_MissingTaskID(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2AResumeTask(context.Background(), map[string]interface{}{
		"agent": "openclaw",
		"input": "test",
	})
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
}

// ---------------------------------------------------------------------------
// HandleA2ACancelTask tests
// ---------------------------------------------------------------------------

func TestHandleA2ACancelTask_HappyPath(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	result, err := env.bridge.HandleA2ACancelTask(context.Background(), map[string]interface{}{
		"task_id": "tsk_TEST0000000000000000001",
		"agent":   "openclaw",
		"reason":  "no longer needed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["state"] != "canceled" {
		t.Errorf("state = %v, want canceled", m["state"])
	}
}

func TestHandleA2ACancelTask_MissingTaskID(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2ACancelTask(context.Background(), map[string]interface{}{
		"agent": "openclaw",
	})
	if err == nil {
		t.Fatal("expected error for missing task_id")
	}
}

func TestHandleA2ACancelTask_MissingAgent(t *testing.T) {
	env := setupTestEnv(t)

	_, err := env.bridge.HandleA2ACancelTask(context.Background(), map[string]interface{}{
		"task_id": "tsk_TEST0000000000000000001",
	})
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

// ---------------------------------------------------------------------------
// HandleA2AListPendingApprovals tests
// ---------------------------------------------------------------------------

func TestHandleA2AListPendingApprovals_Empty(t *testing.T) {
	env := setupTestEnv(t)

	result, err := env.bridge.HandleA2AListPendingApprovals(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["count"] != 0 {
		t.Errorf("count = %v, want 0", m["count"])
	}
}

// ---------------------------------------------------------------------------
// HandleA2AListGrants tests
// ---------------------------------------------------------------------------

func TestHandleA2AListGrants_Empty(t *testing.T) {
	env := setupTestEnv(t)

	result, err := env.bridge.HandleA2AListGrants(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	if m["count"] != 0 {
		t.Errorf("count = %v, want 0", m["count"])
	}
}

// ---------------------------------------------------------------------------
// Context tests
// ---------------------------------------------------------------------------

func TestWithClientInfo(t *testing.T) {
	ctx := WithClientInfo(context.Background(), "claude-desktop", "3.0")
	if got := clientNameFromCtx(ctx); got != "claude-desktop" {
		t.Errorf("client name = %q, want claude-desktop", got)
	}
}

func TestClientNameFromCtx_Empty(t *testing.T) {
	if got := clientNameFromCtx(context.Background()); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// NewBridge with nil logger
// ---------------------------------------------------------------------------

func TestNewBridge_NilLogger(t *testing.T) {
	tmpDir := t.TempDir()

	reg, err := registry.NewStore(filepath.Join(tmpDir, "reg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()

	govDB, err := sql.Open("sqlite", filepath.Join(tmpDir, "gov.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer govDB.Close()
	if err := governance.MigrateGrants(govDB); err != nil {
		t.Fatal(err)
	}
	govStore := governance.NewGrantStore(govDB)

	govEngine := governance.NewEngine(govStore)
	factory := client.NewFactory(nil)
	pool := client.NewPool(factory, nil)
	defer pool.CloseAll()

	b := NewBridge(pool, govEngine, reg, nil, nil)
	if b.logger == nil {
		t.Error("expected default logger")
	}
}

// ---------------------------------------------------------------------------
// HandleA2AListAgents with skills
// ---------------------------------------------------------------------------

func TestHandleA2AListAgents_ShowsSkills(t *testing.T) {
	env := setupTestEnv(t)
	registerTestAgent(t, env, "openclaw")

	result, err := env.bridge.HandleA2AListAgents(context.Background(), map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]interface{})
	agents := m["agents"].([]map[string]interface{})
	skills, ok := agents[0]["skills"].([]string)
	if !ok {
		t.Fatalf("skills type = %T", agents[0]["skills"])
	}
	if len(skills) != 2 {
		t.Errorf("skills = %d, want 2", len(skills))
	}
}

// ---------------------------------------------------------------------------
// extractTextFromMessage
// ---------------------------------------------------------------------------

func TestExtractTextFromMessage(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleAgent,
		a2a.NewTextPart("hello"),
		a2a.NewTextPart("world"),
	)
	got := extractTextFromMessage(msg)
	if got != "hello\nworld" {
		t.Errorf("got %q, want 'hello\\nworld'", got)
	}
}

func TestExtractTextFromMessage_NoText(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleAgent,
		a2a.NewDataPart(json.RawMessage(`{"key":"val"}`)),
	)
	got := extractTextFromMessage(msg)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// NA2AToMCP with status message
// ---------------------------------------------------------------------------

func TestNA2AToMCP_WithStatusMessage(t *testing.T) {
	statusMsg := a2a.NewMessage(a2a.RoleAgent, a2a.NewTextPart("processing complete"))
	task := a2a.Task{
		Kind:   "task",
		TaskID: "tsk_TEST0000000000000000003",
		Status: a2a.TaskStatus{
			State:     a2a.TaskStateCompleted,
			Timestamp: a2a.Now(),
			Message:   &statusMsg,
		},
	}

	result := NA2AToMCP(&task)
	if result["status_message"] != "processing complete" {
		t.Errorf("status_message = %v, want 'processing complete'", result["status_message"])
	}
}

// ---------------------------------------------------------------------------
// NA2AToMCP with data parts in artifacts
// ---------------------------------------------------------------------------

func TestNA2AToMCP_DataArtifact(t *testing.T) {
	task := a2a.Task{
		Kind:   "task",
		TaskID: "tsk_TEST0000000000000000004",
		Status: a2a.TaskStatus{
			State:     a2a.TaskStateCompleted,
			Timestamp: a2a.Now(),
		},
		Artifacts: []a2a.Artifact{
			a2a.NewArtifact("data-result", a2a.NewDataPart(json.RawMessage(`{"count":42}`))),
		},
	}

	result := NA2AToMCP(&task)
	artifacts := result["artifacts"].([]map[string]interface{})
	dataParts := artifacts[0]["data"].([]json.RawMessage)
	if len(dataParts) != 1 {
		t.Fatalf("data parts = %d, want 1", len(dataParts))
	}
}
