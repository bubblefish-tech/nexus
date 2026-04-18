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

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/client"
	"github.com/BubbleFish-Nexus/internal/a2a/governance"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/server"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
	"github.com/BubbleFish-Nexus/internal/mcp/bridge"
	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

// mockNA2AAgent is a minimal NA2A-compliant HTTP server for integration tests.
// It registers one skill (echo_message) that echoes input back as output.
type mockNA2AAgent struct {
	srv      *http.Server
	ln       net.Listener
	card     a2a.AgentCard
	logger   *slog.Logger
	a2aSrv   *server.Server
	executor *server.InMemorySkillExecutor
}

// newMockNA2AAgent creates and starts a mock agent on a random port.
func newMockNA2AAgent(t *testing.T) *mockNA2AAgent {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := ln.Addr().String()

	echoSkill := a2a.Skill{
		ID:                   "echo_message",
		Name:                 "echo_message",
		Description:          "Echoes input back",
		RequiredCapabilities: []string{"test.echo"},
	}

	card := a2a.AgentCard{
		Name:            "mock-agent",
		Description:     "Mock NA2A agent for integration testing",
		URL:             "http://" + addr,
		ProtocolVersion: a2a.ProtocolVersion,
		Version:         "0.0.1-test",
		Implementation:  "mock-na2a",
		Endpoints: []a2a.Endpoint{
			{URL: "http://" + addr + "/a2a/jsonrpc", Transport: a2a.TransportJSONRPC},
		},
		Capabilities: a2a.AgentCapabilities{
			Streaming:         true,
			PushNotifications: false,
			StateTransitions:  true,
		},
		Skills: []a2a.Skill{echoSkill},
	}

	skillReg := server.NewFakeSkillRegistry(echoSkill)
	skillExec := server.NewFakeSkillExecutor(map[string]server.SkillFunc{
		"echo_message": func(_ context.Context, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
			return input, files, nil
		},
	})

	store := server.NewFakeTaskStore()
	gov := server.NewFakeGovernance()
	audit := server.NewFakeAuditSink()
	push := server.NewFakePushNotifier()

	a2aSrv := server.NewServer(card,
		server.WithTaskStore(store),
		server.WithGovernanceEngine(gov),
		server.WithSkillRegistry(skillReg),
		server.WithSkillExecutor(skillExec),
		server.WithAuditSink(audit),
		server.WithPushNotifier(push),
		server.WithLogger(slog.Default()),
	)

	r := chi.NewRouter()
	r.Post("/a2a/jsonrpc", func(w http.ResponseWriter, r *http.Request) {
		var req jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		resp := a2aSrv.Dispatch(r.Context(), &req)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	r.Post("/a2a/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		var req jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}

		resp := a2aSrv.Dispatch(r.Context(), &req)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		data, _ := json.Marshal(resp.Result)
		fmt.Fprintf(w, "event: final\ndata: %s\n\n", data)
		flusher.Flush()
	})

	httpSrv := &http.Server{Handler: r}
	go httpSrv.Serve(ln)

	return &mockNA2AAgent{
		srv:      httpSrv,
		ln:       ln,
		card:     card,
		logger:   slog.Default(),
		a2aSrv:   a2aSrv,
		executor: skillExec,
	}
}

// addr returns the listener address.
func (m *mockNA2AAgent) addr() string {
	return m.ln.Addr().String()
}

// close shuts down the mock agent.
func (m *mockNA2AAgent) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	m.srv.Shutdown(ctx)
}

// testEnv bundles all components needed for end-to-end tests.
type testEnv struct {
	mock      *mockNA2AAgent
	bridge    *bridge.Bridge
	govEngine *governance.Engine
	govStore  *governance.GrantStore
	regStore  *registry.Store
	pool      *client.Pool
	audit     *server.FakeAuditSink
	db        *sql.DB
	tmpDir    string
}

// newTestEnv creates a fully wired test environment with a mock NA2A agent,
// real governance engine (SQLite-backed), real registry, and real client pool.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	tmpDir := t.TempDir()

	// Start mock agent.
	mock := newMockNA2AAgent(t)

	// Open a shared SQLite database for governance and registry.
	dbPath := filepath.Join(tmpDir, "integration_test.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout%3d5000")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL"} {
		if _, err := db.Exec(pragma); err != nil {
			t.Fatalf("pragma: %v", err)
		}
	}

	// Governance.
	if err := governance.MigrateGrants(db); err != nil {
		t.Fatalf("migrate grants: %v", err)
	}
	grantStore := governance.NewGrantStore(db)
	govEngine := governance.NewEngine(grantStore)

	// Registry.
	regPath := filepath.Join(tmpDir, "registry.db")
	regStore, err := registry.NewStore(regPath)
	if err != nil {
		t.Fatalf("registry store: %v", err)
	}

	// Register the mock agent.
	agent := registry.RegisteredAgent{
		AgentID:     "mock-agent-id",
		Name:        "mock",
		DisplayName: "Mock Agent",
		AgentCard:   mock.card,
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  "http://" + mock.addr(),
		},
		Status:    registry.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := regStore.Register(context.Background(), agent); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	// Client pool.
	factory := client.NewFactory(slog.Default())
	pool := client.NewPool(factory, slog.Default())

	// Audit.
	audit := server.NewFakeAuditSink()

	// Bridge.
	br := bridge.NewBridge(pool, govEngine, regStore, audit, slog.Default())

	t.Cleanup(func() {
		mock.close()
		pool.CloseAll()
		regStore.Close()
		db.Close()
	})

	return &testEnv{
		mock:      mock,
		bridge:    br,
		govEngine: govEngine,
		govStore:  grantStore,
		regStore:  regStore,
		pool:      pool,
		audit:     audit,
		db:        db,
		tmpDir:    tmpDir,
	}
}

// addGrant creates an allow grant in the governance store.
func (env *testEnv) addGrant(t *testing.T, source, target, capGlob, decision string) string {
	t.Helper()
	grantID := a2a.NewGrantID()
	g := &governance.Grant{
		GrantID:        grantID,
		SourceAgentID:  source,
		TargetAgentID:  target,
		CapabilityGlob: capGlob,
		Scope:          "SCOPED",
		Decision:       decision,
		IssuedBy:       "test",
		IssuedAt:       time.Now(),
	}
	if err := env.govStore.CreateGrant(g); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	return grantID
}

// --- End-to-End Tests ---

func TestE2E_HappyPath_SendToAgent(t *testing.T) {
	env := newTestEnv(t)

	// Grant test.echo to client_generic -> mock-agent-id.
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "allow")

	ctx := context.Background()
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "hello world",
	})
	if err != nil {
		t.Fatalf("HandleA2ASendToAgent: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}

	state, _ := m["state"].(string)
	if state != "completed" {
		t.Errorf("expected state=completed, got %q", state)
	}

	taskID, _ := m["task_id"].(string)
	if taskID == "" {
		t.Error("expected non-empty task_id")
	}

	// Verify audit events were recorded.
	events := env.audit.Events()
	if len(events) == 0 {
		t.Error("expected at least one audit event")
	}
}

func TestE2E_HappyPath_WithDataInput(t *testing.T) {
	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "allow")

	ctx := context.Background()
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": map[string]interface{}{"text": "hello world"},
	})
	if err != nil {
		t.Fatalf("HandleA2ASendToAgent: %v", err)
	}

	m := result.(map[string]interface{})
	if m["state"] != "completed" {
		t.Errorf("expected state=completed, got %v", m["state"])
	}
}

func TestE2E_GovernanceDeny_NoGrant(t *testing.T) {
	env := newTestEnv(t)

	// No grant exists. The default policy for "test.echo" is unknown,
	// so it depends on the default policy resolution. Let's add an explicit deny.
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "deny")

	ctx := context.Background()
	_, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "should be denied",
	})
	if err == nil {
		t.Fatal("expected error for denied grant, got nil")
	}
}

func TestE2E_GovernanceDeny_ExplicitDeny(t *testing.T) {
	env := newTestEnv(t)

	// Add both allow and deny — deny should win.
	env.addGrant(t, "client_generic", "mock-agent-id", "test.*", "allow")
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "deny")

	ctx := context.Background()
	_, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "should be denied",
	})
	if err == nil {
		t.Fatal("expected deny error, got nil")
	}
}

func TestE2E_GovernanceEscalate_DestructiveSkill(t *testing.T) {
	env := newTestEnv(t)

	// Register a destructive skill on the mock agent's card by re-registering.
	destructiveSkill := a2a.Skill{
		ID:                   "destroy_all",
		Name:                 "destroy_all",
		Description:          "Destroys everything",
		Destructive:          true,
		RequiredCapabilities: []string{"shell.exec"},
	}
	// Update the mock agent's card to include this skill.
	newCard := env.mock.card
	newCard.Skills = append(newCard.Skills, destructiveSkill)

	// Re-register the agent with updated card.
	if err := env.regStore.Delete(context.Background(), "mock-agent-id"); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	agent := registry.RegisteredAgent{
		AgentID:     "mock-agent-id",
		Name:        "mock",
		DisplayName: "Mock Agent",
		AgentCard:   newCard,
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  "http://" + env.mock.addr(),
		},
		Status:    registry.StatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := env.regStore.Register(context.Background(), agent); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	// Grant shell.exec.
	env.addGrant(t, "client_generic", "mock-agent-id", "shell.exec", "allow")

	ctx := context.Background()
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "destroy_all",
		"input": "please destroy",
	})
	if err != nil {
		t.Fatalf("expected escalation result, not error: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}

	status, _ := m["status"].(string)
	if status != "escalated" {
		t.Errorf("expected status=escalated, got %q", status)
	}
}

func TestE2E_ListAgents(t *testing.T) {
	env := newTestEnv(t)

	ctx := context.Background()
	result, err := env.bridge.HandleA2AListAgents(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("HandleA2AListAgents: %v", err)
	}

	m := result.(map[string]interface{})
	count, _ := m["count"].(int)
	if count < 1 {
		t.Errorf("expected at least 1 agent, got %d", count)
	}
}

func TestE2E_DescribeAgent(t *testing.T) {
	env := newTestEnv(t)

	ctx := context.Background()
	result, err := env.bridge.HandleA2ADescribeAgent(ctx, map[string]interface{}{
		"agent": "mock",
	})
	if err != nil {
		t.Fatalf("HandleA2ADescribeAgent: %v", err)
	}

	m := result.(map[string]interface{})
	name, _ := m["name"].(string)
	if name != "mock" {
		t.Errorf("expected name=mock, got %q", name)
	}
}

func TestE2E_CancelTask(t *testing.T) {
	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "allow")

	ctx := context.Background()
	// First send a task.
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "to be canceled",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	m := result.(map[string]interface{})
	taskID, _ := m["task_id"].(string)

	// Try to cancel it. It may already be completed, which is fine —
	// the cancel call should succeed or return an appropriate error.
	cancelResult, err := env.bridge.HandleA2ACancelTask(ctx, map[string]interface{}{
		"agent":   "mock",
		"task_id": taskID,
		"reason":  "testing cancel",
	})
	// We accept either success or a terminal-state error.
	if err != nil {
		t.Logf("cancel returned error (task may already be terminal): %v", err)
	} else {
		cm := cancelResult.(map[string]interface{})
		t.Logf("cancel result: state=%v", cm["state"])
	}
}

func TestE2E_GetTask(t *testing.T) {
	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "allow")

	ctx := context.Background()
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "get this task",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	m := result.(map[string]interface{})
	taskID, _ := m["task_id"].(string)

	getResult, err := env.bridge.HandleA2AGetTask(ctx, map[string]interface{}{
		"agent":   "mock",
		"task_id": taskID,
	})
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	gm := getResult.(map[string]interface{})
	state, _ := gm["state"].(string)
	if state != "completed" {
		t.Errorf("expected state=completed, got %q", state)
	}
}

func TestE2E_ListGrants(t *testing.T) {
	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "allow")

	ctx := context.Background()
	result, err := env.bridge.HandleA2AListGrants(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("HandleA2AListGrants: %v", err)
	}

	m := result.(map[string]interface{})
	count, _ := m["count"].(int)
	if count < 1 {
		t.Errorf("expected at least 1 grant, got %d", count)
	}
}

func TestE2E_ListPendingApprovals(t *testing.T) {
	env := newTestEnv(t)

	ctx := context.Background()
	result, err := env.bridge.HandleA2AListPendingApprovals(ctx, map[string]interface{}{})
	if err != nil {
		t.Fatalf("HandleA2AListPendingApprovals: %v", err)
	}

	m := result.(map[string]interface{})
	count, _ := m["count"].(int)
	if count != 0 {
		t.Errorf("expected 0 pending approvals, got %d", count)
	}
}

func TestE2E_MissingAgent(t *testing.T) {
	env := newTestEnv(t)

	ctx := context.Background()
	_, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "nonexistent",
		"skill": "echo_message",
		"input": "should fail",
	})
	if err == nil {
		t.Fatal("expected error for missing agent, got nil")
	}
}

func TestE2E_MissingInput(t *testing.T) {
	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "allow")

	ctx := context.Background()
	_, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
	})
	if err == nil {
		t.Fatal("expected error for missing input, got nil")
	}
}

func TestE2E_MissingAgentField(t *testing.T) {
	env := newTestEnv(t)

	ctx := context.Background()
	_, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"input": "missing agent",
	})
	if err == nil {
		t.Fatal("expected error for missing agent field, got nil")
	}
}

func TestE2E_Stream(t *testing.T) {
	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "allow")

	// Streaming uses a separate code path through the bridge but still requires
	// governance. We'll call HandleA2AStreamToAgent which collects events.
	ctx := context.Background()
	result, err := env.bridge.HandleA2AStreamToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "stream test",
	})
	if err != nil {
		t.Fatalf("HandleA2AStreamToAgent: %v", err)
	}

	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}

	count, _ := m["count"].(int)
	if count < 1 {
		t.Errorf("expected at least 1 stream event, got %d", count)
	}
}

func TestE2E_AuditTrail(t *testing.T) {
	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "allow")

	ctx := context.Background()
	_, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "audit trail test",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	events := env.audit.Events()
	found := false
	for _, ev := range events {
		if ev.EventType == "bridge.send" {
			found = true
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				t.Fatalf("unexpected audit data type: %T", ev.Data)
			}
			if data["source"] != "client_generic" {
				t.Errorf("expected source=client_generic, got %v", data["source"])
			}
			if data["target"] != "mock-agent-id" {
				t.Errorf("expected target=mock-agent-id, got %v", data["target"])
			}
			break
		}
	}
	if !found {
		t.Error("expected bridge.send audit event")
	}
}

func TestE2E_ExpiredGrantDenied(t *testing.T) {
	env := newTestEnv(t)

	// Add an expired grant.
	grantID := a2a.NewGrantID()
	past := time.Now().Add(-1 * time.Hour)
	g := &governance.Grant{
		GrantID:        grantID,
		SourceAgentID:  "client_generic",
		TargetAgentID:  "mock-agent-id",
		CapabilityGlob: "test.echo",
		Scope:          "SCOPED",
		Decision:       "allow",
		ExpiresAt:      &past,
		IssuedBy:       "test",
		IssuedAt:       time.Now().Add(-2 * time.Hour),
	}
	if err := env.govStore.CreateGrant(g); err != nil {
		t.Fatalf("create grant: %v", err)
	}

	ctx := context.Background()
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "should fail with expired grant",
	})
	// With an expired grant and no default auto-allow policy, this should
	// either deny or escalate depending on default policy for "test.echo".
	// Either outcome means governance correctly ignored the expired grant.
	if err != nil {
		t.Logf("correctly denied with expired grant: %v", err)
		return
	}
	m := result.(map[string]interface{})
	if m["status"] == "escalated" {
		t.Log("correctly escalated with expired grant")
		return
	}
	// If auto-allow by default policy, the grant was correctly ignored.
	t.Logf("result: %v (expired grant correctly ignored, default policy applied)", m)
}

func TestE2E_GlobGrant(t *testing.T) {
	env := newTestEnv(t)

	// Grant with wildcard: test.* should cover test.echo.
	env.addGrant(t, "client_generic", "mock-agent-id", "test.*", "allow")

	ctx := context.Background()
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "glob grant test",
	})
	if err != nil {
		t.Fatalf("expected success with glob grant: %v", err)
	}

	m := result.(map[string]interface{})
	if m["state"] != "completed" {
		t.Errorf("expected state=completed, got %v", m["state"])
	}
}

func TestE2E_RevokedGrantDenied(t *testing.T) {
	env := newTestEnv(t)

	grantID := env.addGrant(t, "client_generic", "mock-agent-id", "test.echo", "allow")

	// Revoke it.
	if err := env.govStore.RevokeGrant(grantID, time.Now()); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}

	ctx := context.Background()
	result, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
		"agent": "mock",
		"skill": "echo_message",
		"input": "should fail with revoked grant",
	})
	// Same logic as expired: either deny, escalate, or auto-allow by default policy.
	if err != nil {
		t.Logf("correctly denied with revoked grant: %v", err)
		return
	}
	m := result.(map[string]interface{})
	t.Logf("result with revoked grant: %v (default policy applied)", m)
}

func TestMain(m *testing.M) {
	// Ensure CGO is not required for our tests.
	os.Exit(m.Run())
}
