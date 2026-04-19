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

package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a/jsonrpc"
)

// -----------------------------------------------------------------------
// TransportConfig validation tests
// -----------------------------------------------------------------------

func TestTransportConfigValidate(t *testing.T) {
	t.Helper()
	tests := []struct {
		name    string
		config  TransportConfig
		wantErr bool
	}{
		{
			name:    "empty kind",
			config:  TransportConfig{},
			wantErr: true,
		},
		{
			name:    "unknown kind",
			config:  TransportConfig{Kind: "grpc"},
			wantErr: true,
		},
		{
			name:    "http without url",
			config:  TransportConfig{Kind: "http"},
			wantErr: true,
		},
		{
			name:    "http with url",
			config:  TransportConfig{Kind: "http", URL: "http://localhost:8080"},
			wantErr: false,
		},
		{
			name:    "tunnel without url",
			config:  TransportConfig{Kind: "tunnel"},
			wantErr: true,
		},
		{
			name:    "tunnel with url",
			config:  TransportConfig{Kind: "tunnel", URL: "http://localhost:9090"},
			wantErr: false,
		},
		{
			name:    "wsl without url",
			config:  TransportConfig{Kind: "wsl"},
			wantErr: true,
		},
		{
			name:    "wsl with url",
			config:  TransportConfig{Kind: "wsl", URL: "http://172.20.0.1:8080"},
			wantErr: false,
		},
		{
			name:    "stdio without command",
			config:  TransportConfig{Kind: "stdio"},
			wantErr: true,
		},
		{
			name:    "stdio with command",
			config:  TransportConfig{Kind: "stdio", Command: "/usr/bin/echo"},
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

// -----------------------------------------------------------------------
// Get() transport factory tests
// -----------------------------------------------------------------------

func TestGet(t *testing.T) {
	t.Helper()
	tests := []struct {
		kind    string
		wantErr bool
	}{
		{"http", false},
		{"stdio", false},
		{"tunnel", false},
		{"unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			tr, err := Get(tt.kind)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tr == nil {
				t.Fatal("nil transport")
			}
		})
	}
}

func TestGetWSL(t *testing.T) {
	t.Helper()
	tr, err := Get("wsl")
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("expected wsl transport on windows, got error: %v", err)
		}
		if tr == nil {
			t.Fatal("nil wsl transport on windows")
		}
	} else {
		if err == nil {
			t.Fatal("expected error for wsl on non-windows")
		}
	}
}

// -----------------------------------------------------------------------
// HTTP transport roundtrip tests
// -----------------------------------------------------------------------

func TestHTTPRoundtrip(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start a server on a random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // Free the port for the HTTP listener.

	serverCfg := TransportConfig{Kind: "http", URL: addr}
	httpT := &HTTPTransport{}
	listener, err := httpT.Listen(ctx, serverCfg)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	// Set up a handler that echoes back the method name.
	hl := listener.(*httpListener)
	hl.SetHandler(handlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		resp, _ := jsonrpc.NewResponse(req.ID, map[string]string{"echo": req.Method})
		return resp
	}))

	// Dial.
	clientCfg := TransportConfig{Kind: "http", URL: "http://" + listener.Addr()}
	conn, err := httpT.Dial(ctx, clientCfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req, err := jsonrpc.NewRequest(jsonrpc.StringID("1"), "agent/ping", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := conn.Send(ctx, req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("response error: %v", resp.Error)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["echo"] != "agent/ping" {
		t.Errorf("got echo=%q, want %q", result["echo"], "agent/ping")
	}
}

func TestHTTPBearerAuth(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use a raw HTTP server to capture the Authorization header.
	var receivedAuth string
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAuth = r.Header.Get("Authorization")
			var req jsonrpc.Request
			json.NewDecoder(r.Body).Decode(&req)
			resp, _ := jsonrpc.NewResponse(req.ID, "ok")
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}),
	}
	go srv.Serve(ln)
	defer srv.Shutdown(ctx)

	httpT := &HTTPTransport{}
	clientCfg := TransportConfig{
		Kind:      "http",
		URL:       "http://" + ln.Addr().String(),
		AuthType:  "bearer",
		AuthToken: "test-token-123",
	}
	conn, err := httpT.Dial(ctx, clientCfg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("auth-1"), "agent/ping", nil)
	_, err = conn.Send(ctx, req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if receivedAuth != "Bearer test-token-123" {
		t.Errorf("got auth=%q, want %q", receivedAuth, "Bearer test-token-123")
	}
}

func TestHTTPConnectionClosed(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	httpT := &HTTPTransport{}

	conn, err := httpT.Dial(ctx, TransportConfig{
		Kind: "http",
		URL:  "http://127.0.0.1:1", // will not connect, but that's fine
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("1"), "test", nil)
	_, err = conn.Send(ctx, req)
	if err == nil {
		t.Fatal("expected error after close")
	}
}

func TestHTTPStreamClosed(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	httpT := &HTTPTransport{}

	conn, err := httpT.Dial(ctx, TransportConfig{
		Kind: "http",
		URL:  "http://127.0.0.1:1",
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("1"), "test", nil)
	_, err = conn.Stream(ctx, req)
	if err == nil {
		t.Fatal("expected error after close")
	}
}

func TestHTTPMultipleRequests(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	httpT := &HTTPTransport{}
	listener, err := httpT.Listen(ctx, TransportConfig{Kind: "http", URL: addr})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	var counter int
	hl := listener.(*httpListener)
	hl.SetHandler(handlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		counter++
		resp, _ := jsonrpc.NewResponse(req.ID, counter)
		return resp
	}))

	conn, err := httpT.Dial(ctx, TransportConfig{Kind: "http", URL: "http://" + listener.Addr()})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for i := 0; i < 5; i++ {
		req, _ := jsonrpc.NewRequest(jsonrpc.NumberID(int64(i)), "test", nil)
		resp, err := conn.Send(ctx, req)
		if err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		if resp.Error != nil {
			t.Fatalf("send %d: response error: %v", i, resp.Error)
		}
	}

	if counter != 5 {
		t.Errorf("expected 5 requests handled, got %d", counter)
	}
}

func TestHTTPSSEStream(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	httpT := &HTTPTransport{}
	listener, err := httpT.Listen(ctx, TransportConfig{Kind: "http", URL: addr})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	// Use Accept mode: a goroutine accepts the stream conn and pushes 4 SSE events.
	go func() {
		sconn, err := listener.Accept(ctx)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		sc := sconn.(*serverConn)
		for i := 0; i < 3; i++ {
			payload, _ := json.Marshal(map[string]interface{}{
				"taskId": "task-1",
				"index":  i,
			})
			if err := sc.SendEvent(Event{Kind: "status-update", TaskID: "task-1", Payload: json.RawMessage(payload)}); err != nil {
				t.Errorf("send event %d: %v", i, err)
				return
			}
		}
		finalPayload, _ := json.Marshal(map[string]interface{}{
			"taskId": "task-1",
			"result": "done",
		})
		sc.SendEvent(Event{Kind: "final", Payload: json.RawMessage(finalPayload)}) //nolint:errcheck
		sc.CloseStream()
	}()

	conn, err := httpT.Dial(ctx, TransportConfig{Kind: "http", URL: "http://" + listener.Addr()})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("stream-1"), "message/stream", nil)
	ch, err := conn.Stream(ctx, req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var events []Event
	for evt := range ch {
		events = append(events, evt)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	// First 3 should be status-update.
	for i := 0; i < 3; i++ {
		if events[i].Kind != "status-update" {
			t.Errorf("event %d: kind=%q, want %q", i, events[i].Kind, "status-update")
		}
		if events[i].TaskID != "task-1" {
			t.Errorf("event %d: taskID=%q, want %q", i, events[i].TaskID, "task-1")
		}
	}

	// Last should be final.
	if events[3].Kind != "final" {
		t.Errorf("last event kind=%q, want %q", events[3].Kind, "final")
	}
}

func TestHTTPListenerAddr(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	httpT := &HTTPTransport{}
	listener, err := httpT.Listen(ctx, TransportConfig{Kind: "http", URL: addr})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	got := listener.Addr()
	if got == "" {
		t.Fatal("empty addr")
	}
}

func TestHTTPDialInvalidURL(t *testing.T) {
	t.Helper()
	httpT := &HTTPTransport{}
	_, err := httpT.Dial(context.Background(), TransportConfig{Kind: "http"})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestHTTPListenInvalidURL(t *testing.T) {
	t.Helper()
	httpT := &HTTPTransport{}
	_, err := httpT.Listen(context.Background(), TransportConfig{Kind: "http"})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestHTTPUnreachableTarget(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	httpT := &HTTPTransport{}
	conn, err := httpT.Dial(ctx, TransportConfig{
		Kind:      "http",
		URL:       "http://127.0.0.1:1",
		TimeoutMs: 1000,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("1"), "test", nil)
	_, err = conn.Send(ctx, req)
	if err == nil {
		t.Fatal("expected error for unreachable target")
	}
}

func TestHTTPAcceptMode(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	httpT := &HTTPTransport{}
	listener, err := httpT.Listen(ctx, TransportConfig{Kind: "http", URL: addr})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	// No handler set, so Accept mode is used.
	done := make(chan struct{})
	go func() {
		defer close(done)
		sconn, err := listener.Accept(ctx)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		sc := sconn.(*serverConn)
		req := sc.IncomingRequest()
		if req.Method != "agent/ping" {
			t.Errorf("method=%q, want %q", req.Method, "agent/ping")
		}
		resp, _ := jsonrpc.NewResponse(req.ID, "pong")
		sc.Respond(resp)
	}()

	conn, err := httpT.Dial(ctx, TransportConfig{Kind: "http", URL: "http://" + listener.Addr()})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("accept-1"), "agent/ping", nil)
	resp, err := conn.Send(ctx, req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	var result string
	json.Unmarshal(resp.Result, &result)
	if result != "pong" {
		t.Errorf("got %q, want %q", result, "pong")
	}

	<-done
}

func TestHTTPServerConnClose(t *testing.T) {
	t.Helper()
	sc := newServerConn()
	if err := sc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Double close should be safe.
	if err := sc.Close(); err != nil {
		t.Fatalf("double close: %v", err)
	}
}

func TestHTTPServerConnSendError(t *testing.T) {
	t.Helper()
	sc := newServerConn()
	sc.incomingReq = &jsonrpc.Request{}
	_, err := sc.Send(context.Background(), &jsonrpc.Request{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHTTPServerConnStreamError(t *testing.T) {
	t.Helper()
	sc := newServerConn()
	_, err := sc.Stream(context.Background(), &jsonrpc.Request{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHTTPServerConnSendEventNoWriter(t *testing.T) {
	t.Helper()
	sc := newServerConn()
	err := sc.SendEvent(Event{Kind: "test"})
	if err == nil {
		t.Fatal("expected error for non-streaming conn")
	}
}

func TestHTTPListenerDoubleClose(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	httpT := &HTTPTransport{}
	listener, err := httpT.Listen(ctx, TransportConfig{Kind: "http", URL: addr})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	if err := listener.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// -----------------------------------------------------------------------
// Stdio transport tests
// -----------------------------------------------------------------------

// TestStdioEchoServer is a test that, when invoked as a subprocess, acts as
// an echo server reading JSON-RPC requests from stdin and writing responses to stdout.
func TestStdioEchoServer(t *testing.T) {
	t.Helper()
	if os.Getenv("A2A_STDIO_ECHO") != "1" {
		t.Skip("only runs as subprocess")
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req jsonrpc.Request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		resp, _ := jsonrpc.NewResponse(req.ID, map[string]string{"echo": req.Method})
		data, _ := json.Marshal(resp)
		fmt.Fprintf(os.Stdout, "%s\n", data)
	}
}

func TestStdioRoundtrip(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find the test binary.
	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	// Verify the echo server test exists in this binary.
	cmd := exec.CommandContext(ctx, testBin, "-test.run=^TestStdioEchoServer$", "-test.v")
	cmd.Env = append(os.Environ(), "A2A_STDIO_ECHO=1")

	// Check that we can actually run the binary.
	if _, err := exec.LookPath(testBin); err != nil {
		t.Skipf("cannot find test binary: %v", err)
	}

	stdioT := &StdioTransport{}
	config := TransportConfig{
		Kind:    "stdio",
		Command: testBin,
		Args:    []string{"-test.run=^TestStdioEchoServer$", "-test.v"},
	}

	// Override env for the subprocess.
	origEnv := os.Environ()
	os.Setenv("A2A_STDIO_ECHO", "1")
	defer func() {
		os.Unsetenv("A2A_STDIO_ECHO")
		for _, e := range origEnv {
			// Restore environment (best effort).
			_ = e
		}
	}()

	conn, err := stdioT.Dial(ctx, config)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("stdio-1"), "agent/ping", nil)
	resp, err := conn.Send(ctx, req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("response error: %v", resp.Error)
	}

	var result map[string]string
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result["echo"] != "agent/ping" {
		t.Errorf("got echo=%q, want %q", result["echo"], "agent/ping")
	}
}

func TestStdioMultipleMessages(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	os.Setenv("A2A_STDIO_ECHO", "1")
	defer os.Unsetenv("A2A_STDIO_ECHO")

	stdioT := &StdioTransport{}
	conn, err := stdioT.Dial(ctx, TransportConfig{
		Kind:    "stdio",
		Command: testBin,
		Args:    []string{"-test.run=^TestStdioEchoServer$", "-test.v"},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for i := 0; i < 5; i++ {
		req, _ := jsonrpc.NewRequest(jsonrpc.NumberID(int64(i)), fmt.Sprintf("method/%d", i), nil)
		resp, err := conn.Send(ctx, req)
		if err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		var result map[string]string
		json.Unmarshal(resp.Result, &result)
		expected := fmt.Sprintf("method/%d", i)
		if result["echo"] != expected {
			t.Errorf("message %d: got echo=%q, want %q", i, result["echo"], expected)
		}
	}
}

func TestStdioStreamUnsupported(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	os.Setenv("A2A_STDIO_ECHO", "1")
	defer os.Unsetenv("A2A_STDIO_ECHO")

	stdioT := &StdioTransport{}
	conn, err := stdioT.Dial(ctx, TransportConfig{
		Kind:    "stdio",
		Command: testBin,
		Args:    []string{"-test.run=^TestStdioEchoServer$", "-test.v"},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("1"), "test", nil)
	_, err = conn.Stream(ctx, req)
	if err == nil {
		t.Fatal("expected error for stdio streaming")
	}
}

func TestStdioDialInvalidConfig(t *testing.T) {
	t.Helper()
	stdioT := &StdioTransport{}
	_, err := stdioT.Dial(context.Background(), TransportConfig{Kind: "stdio"})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestStdioConnectionClose(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testBin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	os.Setenv("A2A_STDIO_ECHO", "1")
	defer os.Unsetenv("A2A_STDIO_ECHO")

	stdioT := &StdioTransport{}
	conn, err := stdioT.Dial(ctx, TransportConfig{
		Kind:    "stdio",
		Command: testBin,
		Args:    []string{"-test.run=^TestStdioEchoServer$", "-test.v"},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Double close should be safe.
	if err := conn.Close(); err != nil {
		t.Fatalf("double close: %v", err)
	}

	// Send after close should fail.
	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("1"), "test", nil)
	_, err = conn.Send(ctx, req)
	if err == nil {
		t.Fatal("expected error after close")
	}
}

func TestStdioListenerAccept(t *testing.T) {
	t.Helper()
	stdioT := &StdioTransport{}
	listener, err := stdioT.Listen(context.Background(), TransportConfig{Kind: "stdio"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	if listener.Addr() != "stdio" {
		t.Errorf("addr=%q, want %q", listener.Addr(), "stdio")
	}
}

func TestStdioListenerClose(t *testing.T) {
	t.Helper()
	stdioT := &StdioTransport{}
	listener, err := stdioT.Listen(context.Background(), TransportConfig{Kind: "stdio"})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	listener.Close()

	// Accept after close should fail.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = listener.Accept(ctx)
	if err == nil {
		t.Fatal("expected error after close")
	}
}

func TestStdioServerConnWriteResponse(t *testing.T) {
	t.Helper()
	sc := &stdioServerConn{
		reader: nil,
		writer: &discardWriter{},
		done:   make(chan struct{}),
	}

	resp, _ := jsonrpc.NewResponse(jsonrpc.StringID("1"), "test")
	err := sc.WriteResponse(resp)
	if err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func TestStdioServerConnStreamError(t *testing.T) {
	t.Helper()
	sc := &stdioServerConn{
		done: make(chan struct{}),
	}
	_, err := sc.Stream(context.Background(), &jsonrpc.Request{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStdioServerConnClose(t *testing.T) {
	t.Helper()
	sc := &stdioServerConn{
		done: make(chan struct{}),
	}
	if err := sc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Double close should be safe.
	if err := sc.Close(); err != nil {
		t.Fatalf("double close: %v", err)
	}
}

// -----------------------------------------------------------------------
// Tunnel transport tests
// -----------------------------------------------------------------------

func TestTunnelRoundtrip(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	httpT := &HTTPTransport{}
	listener, err := httpT.Listen(ctx, TransportConfig{Kind: "http", URL: addr})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	hl := listener.(*httpListener)
	hl.SetHandler(handlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		resp, _ := jsonrpc.NewResponse(req.ID, "tunnel-ok")
		return resp
	}))

	tunnelT := &TunnelTransport{}
	conn, err := tunnelT.Dial(ctx, TransportConfig{Kind: "tunnel", URL: "http://" + listener.Addr()})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("t-1"), "test", nil)
	resp, err := conn.Send(ctx, req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	var result string
	json.Unmarshal(resp.Result, &result)
	if result != "tunnel-ok" {
		t.Errorf("got %q, want %q", result, "tunnel-ok")
	}
}

func TestTunnelDialInvalid(t *testing.T) {
	t.Helper()
	tunnelT := &TunnelTransport{}
	_, err := tunnelT.Dial(context.Background(), TransportConfig{Kind: "tunnel"})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestTunnelListenInvalid(t *testing.T) {
	t.Helper()
	tunnelT := &TunnelTransport{}
	_, err := tunnelT.Listen(context.Background(), TransportConfig{Kind: "tunnel"})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestTunnelClose(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	httpT := &HTTPTransport{}
	listener, err := httpT.Listen(ctx, TransportConfig{Kind: "http", URL: addr})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	hl := listener.(*httpListener)
	hl.SetHandler(handlerFunc(func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
		resp, _ := jsonrpc.NewResponse(req.ID, "ok")
		return resp
	}))

	tunnelT := &TunnelTransport{}
	conn, err := tunnelT.Dial(ctx, TransportConfig{Kind: "tunnel", URL: "http://" + listener.Addr()})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestTunnelBackoff(t *testing.T) {
	t.Helper()
	for i := 1; i <= 5; i++ {
		d := tunnelBackoff(i)
		if d <= 0 {
			t.Errorf("attempt %d: non-positive backoff: %v", i, d)
		}
		if d > tunnelMaxBackoff+time.Second {
			t.Errorf("attempt %d: backoff %v exceeds max", i, d)
		}
	}
}

// -----------------------------------------------------------------------
// WSL transport tests (run on all platforms, but behavior differs)
// -----------------------------------------------------------------------

func TestWSLGetTransport(t *testing.T) {
	t.Helper()
	tr, err := newWSLTransport()
	if runtime.GOOS == "windows" {
		if err != nil {
			t.Fatalf("expected success on windows, got: %v", err)
		}
		if tr == nil {
			t.Fatal("nil transport")
		}
	} else {
		if err == nil {
			t.Fatal("expected error on non-windows")
		}
	}
}

// -----------------------------------------------------------------------
// parseSSEData tests
// -----------------------------------------------------------------------

func TestParseSSEData(t *testing.T) {
	t.Helper()
	tests := []struct {
		name      string
		eventType string
		data      []byte
		wantKind  string
		wantTask  string
	}{
		{
			name:      "status update with taskId",
			eventType: "status-update",
			data:      []byte(`{"taskId":"t1","state":"running"}`),
			wantKind:  "status-update",
			wantTask:  "t1",
		},
		{
			name:      "final with id field",
			eventType: "final",
			data:      []byte(`{"id":"t2","result":"done"}`),
			wantKind:  "final",
			wantTask:  "t2",
		},
		{
			name:      "no task id",
			eventType: "artifact-update",
			data:      []byte(`{"data":"binary"}`),
			wantKind:  "artifact-update",
			wantTask:  "",
		},
		{
			name:      "invalid json",
			eventType: "error",
			data:      []byte(`not json`),
			wantKind:  "error",
			wantTask:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := parseSSEData(tt.eventType, tt.data)
			if evt.Kind != tt.wantKind {
				t.Errorf("kind=%q, want %q", evt.Kind, tt.wantKind)
			}
			if evt.TaskID != tt.wantTask {
				t.Errorf("taskID=%q, want %q", evt.TaskID, tt.wantTask)
			}
		})
	}
}

// -----------------------------------------------------------------------
// Event type tests
// -----------------------------------------------------------------------

func TestEventMarshal(t *testing.T) {
	t.Helper()
	evt := Event{
		Kind:    "status-update",
		TaskID:  "task-123",
		Payload: json.RawMessage(`{"state":"running"}`),
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Kind != evt.Kind {
		t.Errorf("kind=%q, want %q", decoded.Kind, evt.Kind)
	}
	if decoded.TaskID != evt.TaskID {
		t.Errorf("taskID=%q, want %q", decoded.TaskID, evt.TaskID)
	}
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

type handlerFunc func(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response

func (f handlerFunc) HandleRequest(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
	return f(ctx, req)
}

type discardWriter struct{}

func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// -----------------------------------------------------------------------
// Bearer token env resolution tests
// -----------------------------------------------------------------------

func TestBearerTokenEnv_HeaderSent(t *testing.T) {
	t.Helper()

	const testToken = "secret-bearer-value-12345"

	// Capture the Authorization header the client sends.
	var gotAuth string
	srv := http.Server{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/a2a/jsonrpc", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		// Return a valid agent/ping response.
		resp := jsonrpc.Response{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`{"status":"ok"}`),
			ID:      jsonrpc.NumberID(1),
		}
		json.NewEncoder(w).Encode(resp)
	})
	srv.Handler = mux
	go srv.Serve(ln)
	defer srv.Close()

	t.Setenv("TEST_A2A_TOKEN", testToken)

	cfg := TransportConfig{
		Kind:           "http",
		URL:            "http://" + ln.Addr().String(),
		AuthType:       "bearer",
		BearerTokenEnv: "TEST_A2A_TOKEN",
	}

	tr := &HTTPTransport{}
	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.NumberID(1), "agent/ping", nil)
	_, err = conn.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	expected := "Bearer " + testToken
	if gotAuth != expected {
		t.Errorf("Authorization header = %q, want %q", gotAuth, expected)
	}
}

func TestBearerTokenEnv_MissingEnvVar_FailsClosed(t *testing.T) {
	t.Helper()

	// Ensure the env var is NOT set.
	t.Setenv("TEST_A2A_TOKEN_MISSING", "")
	os.Unsetenv("TEST_A2A_TOKEN_MISSING")

	cfg := TransportConfig{
		Kind:           "http",
		URL:            "http://127.0.0.1:9999",
		AuthType:       "bearer",
		BearerTokenEnv: "TEST_A2A_TOKEN_MISSING",
	}

	tr := &HTTPTransport{}
	_, err := tr.Dial(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when bearer_token_env is set but env var is empty, got nil")
	}

	// Error message should name the missing env var.
	errMsg := err.Error()
	if !contains(errMsg, "TEST_A2A_TOKEN_MISSING") {
		t.Errorf("error should mention env var name, got: %s", errMsg)
	}
}

func TestBearerTokenEnv_DirectToken_StillWorks(t *testing.T) {
	t.Helper()

	const testToken = "direct-token-value"

	var gotAuth string
	srv := http.Server{}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/a2a/jsonrpc", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		resp := jsonrpc.Response{
			JSONRPC: "2.0",
			Result:  json.RawMessage(`{"status":"ok"}`),
			ID:      jsonrpc.NumberID(1),
		}
		json.NewEncoder(w).Encode(resp)
	})
	srv.Handler = mux
	go srv.Serve(ln)
	defer srv.Close()

	cfg := TransportConfig{
		Kind:      "http",
		URL:       "http://" + ln.Addr().String(),
		AuthType:  "bearer",
		AuthToken: testToken,
	}

	tr := &HTTPTransport{}
	conn, err := tr.Dial(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	req, _ := jsonrpc.NewRequest(jsonrpc.NumberID(1), "agent/ping", nil)
	_, err = conn.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	expected := "Bearer " + testToken
	if gotAuth != expected {
		t.Errorf("Authorization header = %q, want %q", gotAuth, expected)
	}
}

func TestResolveBearerToken_NonBearerAuth_ReturnsEmpty(t *testing.T) {
	cfg := TransportConfig{
		Kind:           "http",
		URL:            "http://localhost:9999",
		AuthType:       "none",
		BearerTokenEnv: "SHOULD_NOT_MATTER",
	}
	token, err := cfg.ResolveBearerToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "" {
		t.Errorf("expected empty token for non-bearer auth, got %q", token)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
