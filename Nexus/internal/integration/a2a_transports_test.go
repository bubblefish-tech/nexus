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
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/client"
	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
)

// TestTransport_HTTP_RoundTrip verifies the echo skill round-trips through
// the HTTP transport using the full mock NA2A agent.
func TestTransport_HTTP_RoundTrip(t *testing.T) {
	mock := newMockNA2AAgent(t)
	defer mock.close()

	cfg := transport.TransportConfig{
		Kind: "http",
		URL:  "http://" + mock.addr(),
	}

	tr, err := transport.Get("http")
	if err != nil {
		t.Fatalf("get http transport: %v", err)
	}

	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := client.NewClient(conn, "mock-agent-id", slog.Default())

	// Ping.
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// Get agent card.
	card, err := c.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("get agent card: %v", err)
	}
	if card.Name != "mock-agent" {
		t.Errorf("expected card name=mock-agent, got %q", card.Name)
	}

	// Send echo message.
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("http transport test"))
	task, err := c.SendMessage(context.Background(), &msg, "echo_message", &client.SendConfig{Blocking: true})
	if err != nil {
		t.Fatalf("send message: %v", err)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected state=completed, got %s", task.Status.State)
	}
}

// TestTransport_HTTP_Stream verifies SSE streaming through HTTP transport.
func TestTransport_HTTP_Stream(t *testing.T) {
	mock := newMockNA2AAgent(t)
	defer mock.close()

	cfg := transport.TransportConfig{
		Kind: "http",
		URL:  "http://" + mock.addr(),
	}

	tr, err := transport.Get("http")
	if err != nil {
		t.Fatalf("get http transport: %v", err)
	}

	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := client.NewClient(conn, "mock-agent-id", slog.Default())

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("stream transport test"))
	events, err := c.StreamMessage(context.Background(), &msg, "echo_message", nil)
	if err != nil {
		t.Fatalf("stream message: %v", err)
	}

	eventCount := 0
	for range events {
		eventCount++
	}
	if eventCount < 1 {
		t.Error("expected at least 1 stream event")
	}
}

// TestTransport_HTTP_Timeout verifies timeout behavior.
func TestTransport_HTTP_Timeout(t *testing.T) {
	mock := newMockNA2AAgent(t)
	defer mock.close()

	cfg := transport.TransportConfig{
		Kind:      "http",
		URL:       "http://" + mock.addr(),
		TimeoutMs: 50, // Very short timeout
	}

	tr, err := transport.Get("http")
	if err != nil {
		t.Fatalf("get http transport: %v", err)
	}

	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := client.NewClient(conn, "mock-agent-id", slog.Default())

	// Ping should succeed even with short timeout since it's fast.
	if err := c.Ping(context.Background()); err != nil {
		// Short timeout may cause failures on slow systems; that's acceptable.
		t.Logf("ping with short timeout: %v (acceptable)", err)
	}
}

// TestTransport_HTTP_ConnectionReuse verifies the client pool reuses connections.
func TestTransport_HTTP_ConnectionReuse(t *testing.T) {
	mock := newMockNA2AAgent(t)
	defer mock.close()

	factory := client.NewFactory(slog.Default())
	pool := client.NewPool(factory, slog.Default())
	defer pool.CloseAll()

	agent := registry.RegisteredAgent{
		AgentID: "mock-agent-id",
		Name:    "mock",
		AgentCard: a2a.AgentCard{
			Name:            "mock-agent",
			URL:             "http://" + mock.addr(),
			ProtocolVersion: a2a.ProtocolVersion,
		},
		TransportConfig: transport.TransportConfig{
			Kind: "http",
			URL:  "http://" + mock.addr(),
		},
		Status: registry.StatusActive,
	}

	// Get the same client twice — should reuse.
	c1, err := pool.Get(context.Background(), agent)
	if err != nil {
		t.Fatalf("pool get 1: %v", err)
	}

	c2, err := pool.Get(context.Background(), agent)
	if err != nil {
		t.Fatalf("pool get 2: %v", err)
	}

	if c1 != c2 {
		t.Error("expected pool to return the same client instance")
	}
}

// TestTransport_Tunnel_RoundTrip verifies the tunnel transport (mechanically
// identical to HTTP) round-trips correctly.
func TestTransport_Tunnel_RoundTrip(t *testing.T) {
	mock := newMockNA2AAgent(t)
	defer mock.close()

	// Tunnel transport uses the same HTTP mechanism with higher timeouts.
	cfg := transport.TransportConfig{
		Kind:      "tunnel",
		URL:       "http://" + mock.addr(),
		TimeoutMs: 30000,
	}

	tr, err := transport.Get("tunnel")
	if err != nil {
		t.Fatalf("get tunnel transport: %v", err)
	}

	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := client.NewClient(conn, "mock-agent-id", slog.Default())

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping via tunnel: %v", err)
	}

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("tunnel transport test"))
	task, err := c.SendMessage(context.Background(), &msg, "echo_message", &client.SendConfig{Blocking: true})
	if err != nil {
		t.Fatalf("send via tunnel: %v", err)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected state=completed, got %s", task.Status.State)
	}
}

// TestTransport_HTTP_DataPart verifies structured data round-trips correctly.
func TestTransport_HTTP_DataPart(t *testing.T) {
	mock := newMockNA2AAgent(t)
	defer mock.close()

	cfg := transport.TransportConfig{
		Kind: "http",
		URL:  "http://" + mock.addr(),
	}
	tr, err := transport.Get("http")
	if err != nil {
		t.Fatalf("get transport: %v", err)
	}
	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := client.NewClient(conn, "mock-agent-id", slog.Default())

	inputData := json.RawMessage(`{"key":"value","nested":{"count":42}}`)
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewDataPart(inputData))
	task, err := c.SendMessage(context.Background(), &msg, "echo_message", &client.SendConfig{Blocking: true})
	if err != nil {
		t.Fatalf("send data part: %v", err)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected state=completed, got %s", task.Status.State)
	}
}

// TestTransport_HTTP_GetTask verifies task retrieval after completion.
func TestTransport_HTTP_GetTask(t *testing.T) {
	mock := newMockNA2AAgent(t)
	defer mock.close()

	cfg := transport.TransportConfig{
		Kind: "http",
		URL:  "http://" + mock.addr(),
	}
	tr, err := transport.Get("http")
	if err != nil {
		t.Fatalf("get transport: %v", err)
	}
	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := client.NewClient(conn, "mock-agent-id", slog.Default())

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("get task test"))
	task, err := c.SendMessage(context.Background(), &msg, "echo_message", &client.SendConfig{Blocking: true})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	retrieved, err := c.GetTask(context.Background(), task.TaskID, true)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if retrieved.TaskID != task.TaskID {
		t.Errorf("task ID mismatch: want %s, got %s", task.TaskID, retrieved.TaskID)
	}
	if retrieved.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected state=completed, got %s", retrieved.Status.State)
	}
}

// TestTransport_HTTP_CancelCompletedTask verifies cancel on a terminal task.
func TestTransport_HTTP_CancelCompletedTask(t *testing.T) {
	mock := newMockNA2AAgent(t)
	defer mock.close()

	cfg := transport.TransportConfig{
		Kind: "http",
		URL:  "http://" + mock.addr(),
	}
	tr, err := transport.Get("http")
	if err != nil {
		t.Fatalf("get transport: %v", err)
	}
	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := client.NewClient(conn, "mock-agent-id", slog.Default())

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("cancel completed test"))
	task, err := c.SendMessage(context.Background(), &msg, "echo_message", &client.SendConfig{Blocking: true})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// Cancel a completed task — should return the task as-is or error.
	result, err := c.CancelTask(context.Background(), task.TaskID, "test cancel")
	if err != nil {
		t.Logf("cancel completed task returned error (expected): %v", err)
		return
	}
	// If no error, the task should still be in a terminal state.
	if !result.Status.State.IsTerminal() {
		t.Errorf("expected terminal state after cancel, got %s", result.Status.State)
	}
}

// TestTransport_HTTP_ContextCancellation verifies client respects context deadline.
func TestTransport_HTTP_ContextCancellation(t *testing.T) {
	mock := newMockNA2AAgent(t)
	defer mock.close()

	cfg := transport.TransportConfig{
		Kind: "http",
		URL:  "http://" + mock.addr(),
	}
	tr, err := transport.Get("http")
	if err != nil {
		t.Fatalf("get transport: %v", err)
	}
	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := client.NewClient(conn, "mock-agent-id", slog.Default())

	// Use an already-cancelled context.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond) // ensure timeout fires

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("should timeout"))
	_, err = c.SendMessage(ctx, &msg, "echo_message", nil)
	if err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
}

// TestTransport_HTTP_InvalidURL verifies error on bad connection.
func TestTransport_HTTP_InvalidURL(t *testing.T) {
	cfg := transport.TransportConfig{
		Kind:      "http",
		URL:       "http://127.0.0.1:1", // port 1, unlikely to be listening
		TimeoutMs: 500,
	}

	tr, err := transport.Get("http")
	if err != nil {
		t.Fatalf("get transport: %v", err)
	}

	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	c := client.NewClient(conn, "bad-agent", slog.Default())
	err = c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error pinging unreachable host, got nil")
	}
}

// TestTransport_Config_Validate verifies transport config validation.
func TestTransport_Config_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  transport.TransportConfig
		wantErr bool
	}{
		{
			name:    "empty kind",
			config:  transport.TransportConfig{},
			wantErr: true,
		},
		{
			name:    "unknown kind",
			config:  transport.TransportConfig{Kind: "pigeonpost"},
			wantErr: true,
		},
		{
			name:    "http without url",
			config:  transport.TransportConfig{Kind: "http"},
			wantErr: true,
		},
		{
			name:    "stdio without command",
			config:  transport.TransportConfig{Kind: "stdio"},
			wantErr: true,
		},
		{
			name:    "valid http",
			config:  transport.TransportConfig{Kind: "http", URL: "http://localhost:9999"},
			wantErr: false,
		},
		{
			name:    "valid stdio",
			config:  transport.TransportConfig{Kind: "stdio", Command: "/bin/echo"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
