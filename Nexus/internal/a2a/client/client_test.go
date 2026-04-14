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

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
)

// ---------------------------------------------------------------------------
// Mock transport.Conn
// ---------------------------------------------------------------------------

type mockConn struct {
	mu       sync.Mutex
	sendFunc func(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error)
	streamFn func(ctx context.Context, req *jsonrpc.Request) (<-chan transport.Event, error)
	closed   bool
}

func (m *mockConn) Send(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendFunc != nil {
		return m.sendFunc(ctx, req)
	}
	return nil, errors.New("mock: no sendFunc")
}

func (m *mockConn) Stream(ctx context.Context, req *jsonrpc.Request) (<-chan transport.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.streamFn != nil {
		return m.streamFn(ctx, req)
	}
	return nil, errors.New("mock: no streamFn")
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// ---------------------------------------------------------------------------
// Mock transport.Transport
// ---------------------------------------------------------------------------

type mockTransport struct {
	dialFunc func(ctx context.Context, cfg transport.TransportConfig) (transport.Conn, error)
}

func (m *mockTransport) Dial(ctx context.Context, cfg transport.TransportConfig) (transport.Conn, error) {
	if m.dialFunc != nil {
		return m.dialFunc(ctx, cfg)
	}
	return nil, errors.New("mock: no dialFunc")
}

func (m *mockTransport) Listen(_ context.Context, _ transport.TransportConfig) (transport.Listener, error) {
	return nil, errors.New("mock: listen not implemented")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func successResponse(t *testing.T, id jsonrpc.ID, result interface{}) *jsonrpc.Response {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	return &jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  data,
	}
}

func errorResponse(id jsonrpc.ID, code int, msg string) *jsonrpc.Response {
	return &jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonrpc.ErrorObject{Code: code, Message: msg},
	}
}

func testLogger() *slog.Logger {
	return slog.Default()
}

func testTask() a2a.Task {
	return a2a.Task{
		Kind:   "task",
		TaskID: "tsk_TEST0000000000000000001",
		Status: a2a.TaskStatus{
			State:     a2a.TaskStateCompleted,
			Timestamp: a2a.Now(),
		},
	}
}

func testAgentCard() a2a.AgentCard {
	return a2a.AgentCard{
		Name:            "test-agent",
		URL:             "http://localhost:9999",
		ProtocolVersion: "0.1.0",
		Capabilities:    a2a.AgentCapabilities{Streaming: true},
	}
}

func testMessage() *a2a.Message {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	return &msg
}

// ---------------------------------------------------------------------------
// Client tests
// ---------------------------------------------------------------------------

func TestSendMessage_HappyPath(t *testing.T) {
	task := testTask()
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			if req.Method != "message/send" {
				t.Errorf("expected method message/send, got %s", req.Method)
			}
			return successResponse(t, req.ID, task), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	got, err := c.SendMessage(context.Background(), testMessage(), "echo", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TaskID != task.TaskID {
		t.Errorf("task ID = %s, want %s", got.TaskID, task.TaskID)
	}
}

func TestSendMessage_WithConfig(t *testing.T) {
	task := testTask()
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			var params sendMessageParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("unmarshal params: %v", err)
			}
			if !params.Blocking {
				t.Error("expected blocking=true")
			}
			if params.TimeoutMs != 5000 {
				t.Errorf("timeoutMs = %d, want 5000", params.TimeoutMs)
			}
			return successResponse(t, req.ID, task), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	cfg := &SendConfig{Blocking: true, TimeoutMs: 5000}
	got, err := c.SendMessage(context.Background(), testMessage(), "echo", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TaskID != task.TaskID {
		t.Errorf("task ID = %s, want %s", got.TaskID, task.TaskID)
	}
}

func TestSendMessage_ErrorResponse(t *testing.T) {
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			return errorResponse(req.ID, a2a.CodeSkillNotFound, "skill not found"), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	_, err := c.SendMessage(context.Background(), testMessage(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var rpcErr *jsonrpc.ErrorObject
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected jsonrpc.ErrorObject, got %T: %v", err, err)
	}
	if rpcErr.Code != a2a.CodeSkillNotFound {
		t.Errorf("code = %d, want %d", rpcErr.Code, a2a.CodeSkillNotFound)
	}
}

func TestSendMessage_TransportError(t *testing.T) {
	conn := &mockConn{
		sendFunc: func(_ context.Context, _ *jsonrpc.Request) (*jsonrpc.Response, error) {
			return nil, errors.New("connection reset")
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	_, err := c.SendMessage(context.Background(), testMessage(), "echo", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetTask_Found(t *testing.T) {
	task := testTask()
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			if req.Method != "tasks/get" {
				t.Errorf("method = %s, want tasks/get", req.Method)
			}
			var params getTaskParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if params.TaskID != task.TaskID {
				t.Errorf("taskId = %s, want %s", params.TaskID, task.TaskID)
			}
			if !params.IncludeHistory {
				t.Error("expected includeHistory=true")
			}
			return successResponse(t, req.ID, task), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	got, err := c.GetTask(context.Background(), task.TaskID, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TaskID != task.TaskID {
		t.Errorf("task ID = %s, want %s", got.TaskID, task.TaskID)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			return errorResponse(req.ID, a2a.CodeTaskNotFound, "task not found"), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	_, err := c.GetTask(context.Background(), "tsk_NONEXISTENT0000000000001", false)
	if err == nil {
		t.Fatal("expected error")
	}
	var rpcErr *jsonrpc.ErrorObject
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected ErrorObject, got %T", err)
	}
	if rpcErr.Code != a2a.CodeTaskNotFound {
		t.Errorf("code = %d, want %d", rpcErr.Code, a2a.CodeTaskNotFound)
	}
}

func TestCancelTask_Active(t *testing.T) {
	task := testTask()
	task.Status.State = a2a.TaskStateCanceled
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			if req.Method != "tasks/cancel" {
				t.Errorf("method = %s, want tasks/cancel", req.Method)
			}
			return successResponse(t, req.ID, task), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	got, err := c.CancelTask(context.Background(), task.TaskID, "no longer needed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status.State != a2a.TaskStateCanceled {
		t.Errorf("state = %s, want canceled", got.Status.State)
	}
}

func TestCancelTask_Terminal(t *testing.T) {
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			return errorResponse(req.ID, a2a.CodeTaskNotCancelable, "task already completed"), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	_, err := c.CancelTask(context.Background(), "tsk_TEST0000000000000000001", "reason")
	if err == nil {
		t.Fatal("expected error")
	}
	var rpcErr *jsonrpc.ErrorObject
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected ErrorObject, got %T", err)
	}
	if rpcErr.Code != a2a.CodeTaskNotCancelable {
		t.Errorf("code = %d, want %d", rpcErr.Code, a2a.CodeTaskNotCancelable)
	}
}

func TestGetAgentCard(t *testing.T) {
	card := testAgentCard()
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			if req.Method != "agent/card" {
				t.Errorf("method = %s, want agent/card", req.Method)
			}
			return successResponse(t, req.ID, card), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	got, err := c.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != card.Name {
		t.Errorf("name = %s, want %s", got.Name, card.Name)
	}
}

func TestPing_Success(t *testing.T) {
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			if req.Method != "agent/ping" {
				t.Errorf("method = %s, want agent/ping", req.Method)
			}
			return successResponse(t, req.ID, map[string]string{"status": "ok"}), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPing_Failure(t *testing.T) {
	conn := &mockConn{
		sendFunc: func(_ context.Context, _ *jsonrpc.Request) (*jsonrpc.Response, error) {
			return nil, errors.New("connection refused")
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_Close(t *testing.T) {
	conn := &mockConn{}
	c := NewClient(conn, "agent-1", testLogger())
	if err := c.Close(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !conn.closed {
		t.Error("expected conn to be closed")
	}
}

func TestClient_AgentID(t *testing.T) {
	conn := &mockConn{}
	c := NewClient(conn, "agent-42", testLogger())
	if c.AgentID() != "agent-42" {
		t.Errorf("AgentID = %s, want agent-42", c.AgentID())
	}
}

// ---------------------------------------------------------------------------
// Stream tests
// ---------------------------------------------------------------------------

func TestStreamMessage_HappyPath(t *testing.T) {
	conn := &mockConn{
		streamFn: func(_ context.Context, req *jsonrpc.Request) (<-chan transport.Event, error) {
			if req.Method != "message/stream" {
				t.Errorf("method = %s, want message/stream", req.Method)
			}
			ch := make(chan transport.Event, 3)
			ch <- transport.Event{Kind: "status-update", TaskID: "tsk_1"}
			ch <- transport.Event{Kind: "artifact-update", TaskID: "tsk_1"}
			ch <- transport.Event{Kind: "final", TaskID: "tsk_1"}
			close(ch)
			return ch, nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	ch, err := c.StreamMessage(context.Background(), testMessage(), "echo", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []transport.Event
	for ev := range ch {
		events = append(events, ev)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	if events[2].Kind != "final" {
		t.Errorf("last event kind = %s, want final", events[2].Kind)
	}
}

func TestStreamMessage_Error(t *testing.T) {
	conn := &mockConn{
		streamFn: func(_ context.Context, _ *jsonrpc.Request) (<-chan transport.Event, error) {
			return nil, errors.New("stream not supported")
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	_, err := c.StreamMessage(context.Background(), testMessage(), "echo", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Factory tests
// ---------------------------------------------------------------------------

func TestFactory_NewClient_HappyPath(t *testing.T) {
	pingCount := 0
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			if req.Method == "agent/ping" {
				pingCount++
				return successResponse(t, req.ID, map[string]string{"status": "ok"}), nil
			}
			return errorResponse(req.ID, -32601, "method not found"), nil
		},
	}

	mt := &mockTransport{
		dialFunc: func(_ context.Context, _ transport.TransportConfig) (transport.Conn, error) {
			return conn, nil
		},
	}

	f := NewFactory(testLogger())
	f.RegisterTransport("http", mt)

	agent := registry.RegisteredAgent{
		AgentID: "openclaw",
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  "http://localhost:8080",
		},
	}

	c, err := f.NewClient(context.Background(), agent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer c.Close()

	if c.AgentID() != "openclaw" {
		t.Errorf("AgentID = %s, want openclaw", c.AgentID())
	}
	if pingCount != 1 {
		t.Errorf("ping called %d times, want 1", pingCount)
	}
}

func TestFactory_UnknownTransport(t *testing.T) {
	f := NewFactory(testLogger())
	// Clear all transports so none are found.
	f.transports = make(map[string]transport.Transport)

	agent := registry.RegisteredAgent{
		AgentID: "agent-x",
		TransportConfig: transport.TransportConfig{
			Kind: "quantum",
			URL:  "q://localhost",
		},
	}

	// Should fail at Validate since "quantum" is not a known kind.
	_, err := f.NewClient(context.Background(), agent)
	if err == nil {
		t.Fatal("expected error for unknown transport")
	}
}

func TestFactory_DialError(t *testing.T) {
	mt := &mockTransport{
		dialFunc: func(_ context.Context, _ transport.TransportConfig) (transport.Conn, error) {
			return nil, errors.New("connection refused")
		},
	}

	f := NewFactory(testLogger())
	f.RegisterTransport("http", mt)

	agent := registry.RegisteredAgent{
		AgentID: "agent-1",
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  "http://localhost:9999",
		},
	}

	_, err := f.NewClient(context.Background(), agent)
	if err == nil {
		t.Fatal("expected dial error")
	}
}

func TestFactory_PingFail(t *testing.T) {
	conn := &mockConn{
		sendFunc: func(_ context.Context, _ *jsonrpc.Request) (*jsonrpc.Response, error) {
			return nil, errors.New("ping timeout")
		},
	}

	mt := &mockTransport{
		dialFunc: func(_ context.Context, _ transport.TransportConfig) (transport.Conn, error) {
			return conn, nil
		},
	}

	f := NewFactory(testLogger())
	f.RegisterTransport("http", mt)

	agent := registry.RegisteredAgent{
		AgentID: "agent-1",
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  "http://localhost:9999",
		},
	}

	_, err := f.NewClient(context.Background(), agent)
	if err == nil {
		t.Fatal("expected ping error")
	}
	// Verify conn was closed after ping failure.
	if !conn.closed {
		t.Error("expected conn to be closed after ping failure")
	}
}

// ---------------------------------------------------------------------------
// Pool tests
// ---------------------------------------------------------------------------

func newTestPool(t *testing.T) (*Pool, *mockConn) {
	t.Helper()
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			if req.Method == "agent/ping" {
				return successResponse(t, req.ID, map[string]string{"status": "ok"}), nil
			}
			return successResponse(t, req.ID, testTask()), nil
		},
	}

	mt := &mockTransport{
		dialFunc: func(_ context.Context, _ transport.TransportConfig) (transport.Conn, error) {
			return conn, nil
		},
	}

	f := NewFactory(testLogger())
	f.RegisterTransport("http", mt)
	return NewPool(f, testLogger()), conn
}

func testAgent(id string) registry.RegisteredAgent {
	return registry.RegisteredAgent{
		AgentID: id,
		Name:    id,
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  fmt.Sprintf("http://localhost:%s", id),
		},
	}
}

func TestPool_GetReuse(t *testing.T) {
	pool, _ := newTestPool(t)
	defer pool.CloseAll()

	agent := testAgent("agent-1")

	c1, err := pool.Get(context.Background(), agent)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}

	c2, err := pool.Get(context.Background(), agent)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}

	if c1 != c2 {
		t.Error("expected same client instance on reuse")
	}
}

func TestPool_GetDifferentAgents(t *testing.T) {
	pool, _ := newTestPool(t)
	defer pool.CloseAll()

	c1, err := pool.Get(context.Background(), testAgent("agent-1"))
	if err != nil {
		t.Fatalf("Get agent-1: %v", err)
	}

	c2, err := pool.Get(context.Background(), testAgent("agent-2"))
	if err != nil {
		t.Fatalf("Get agent-2: %v", err)
	}

	if c1 == c2 {
		t.Error("expected different client instances for different agents")
	}
}

func TestPool_Close(t *testing.T) {
	pool, conn := newTestPool(t)

	agent := testAgent("agent-1")
	_, err := pool.Get(context.Background(), agent)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	pool.Close("agent-1")

	if !conn.closed {
		t.Error("expected conn to be closed")
	}

	// Getting again should create a new client (will fail because conn is reused in test).
	// Just verify the old one was removed.
	pool.mu.Lock()
	_, found := pool.clients["agent-1"]
	pool.mu.Unlock()
	if found {
		t.Error("expected client to be removed from pool")
	}
}

func TestPool_CloseAll(t *testing.T) {
	pool, _ := newTestPool(t)

	_, err := pool.Get(context.Background(), testAgent("agent-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	pool.CloseAll()

	pool.mu.Lock()
	count := len(pool.clients)
	pool.mu.Unlock()
	if count != 0 {
		t.Errorf("pool has %d clients after CloseAll, want 0", count)
	}
}

func TestPool_CloseNonexistent(t *testing.T) {
	pool, _ := newTestPool(t)
	// Should not panic.
	pool.Close("nonexistent-agent")
}

// ---------------------------------------------------------------------------
// Unique ID generation test
// ---------------------------------------------------------------------------

func TestNextID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := nextID()
		s := id.String()
		if seen[s] {
			t.Fatalf("duplicate ID: %s", s)
		}
		seen[s] = true
	}
}

// ---------------------------------------------------------------------------
// SendMessage with accepted output modes
// ---------------------------------------------------------------------------

func TestSendMessage_AcceptedOutputModes(t *testing.T) {
	task := testTask()
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			var params sendMessageParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(params.AcceptedOutputModes) != 2 {
				t.Errorf("accepted output modes = %d, want 2", len(params.AcceptedOutputModes))
			}
			return successResponse(t, req.ID, task), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	cfg := &SendConfig{AcceptedOutputModes: []string{"text", "data"}}
	_, err := c.SendMessage(context.Background(), testMessage(), "echo", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Cancel with reason verification
// ---------------------------------------------------------------------------

func TestCancelTask_ReasonPropagated(t *testing.T) {
	task := testTask()
	task.Status.State = a2a.TaskStateCanceled
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			var params cancelTaskParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if params.Reason != "budget exceeded" {
				t.Errorf("reason = %q, want %q", params.Reason, "budget exceeded")
			}
			return successResponse(t, req.ID, task), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	_, err := c.CancelTask(context.Background(), task.TaskID, "budget exceeded")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetTask without history
// ---------------------------------------------------------------------------

func TestGetTask_NoHistory(t *testing.T) {
	task := testTask()
	conn := &mockConn{
		sendFunc: func(_ context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
			var params getTaskParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if params.IncludeHistory {
				t.Error("expected includeHistory=false")
			}
			return successResponse(t, req.ID, task), nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	_, err := c.GetTask(context.Background(), task.TaskID, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// StreamMessage with config
// ---------------------------------------------------------------------------

func TestStreamMessage_WithConfig(t *testing.T) {
	conn := &mockConn{
		streamFn: func(_ context.Context, req *jsonrpc.Request) (<-chan transport.Event, error) {
			var params sendMessageParams
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if params.TimeoutMs != 10000 {
				t.Errorf("timeoutMs = %d, want 10000", params.TimeoutMs)
			}
			ch := make(chan transport.Event, 1)
			ch <- transport.Event{Kind: "final", TaskID: "tsk_1"}
			close(ch)
			return ch, nil
		},
	}

	c := NewClient(conn, "agent-1", testLogger())
	cfg := &SendConfig{TimeoutMs: 10000}
	ch, err := c.StreamMessage(context.Background(), testMessage(), "echo", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for range ch {
		count++
	}
	if count != 1 {
		t.Errorf("got %d events, want 1", count)
	}
}

// ---------------------------------------------------------------------------
// Client nil logger
// ---------------------------------------------------------------------------

func TestNewClient_NilLogger(t *testing.T) {
	conn := &mockConn{}
	c := NewClient(conn, "agent-1", nil)
	if c.logger == nil {
		t.Error("expected default logger when nil is passed")
	}
}

// ---------------------------------------------------------------------------
// Factory nil logger
// ---------------------------------------------------------------------------

func TestNewFactory_NilLogger(t *testing.T) {
	f := NewFactory(nil)
	if f.logger == nil {
		t.Error("expected default logger when nil is passed")
	}
}

// ---------------------------------------------------------------------------
// Pool nil logger
// ---------------------------------------------------------------------------

func TestNewPool_NilLogger(t *testing.T) {
	f := NewFactory(nil)
	p := NewPool(f, nil)
	if p.logger == nil {
		t.Error("expected default logger when nil is passed")
	}
}
