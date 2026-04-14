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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
	"github.com/go-chi/chi/v5"
)

// HTTPTransport implements Transport over HTTP/HTTPS.
type HTTPTransport struct{}

// Dial opens an HTTP client connection.
func (t *HTTPTransport) Dial(ctx context.Context, config TransportConfig) (Conn, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	timeout := 30 * time.Second
	if config.TimeoutMs > 0 {
		timeout = time.Duration(config.TimeoutMs) * time.Millisecond
	}
	return &httpClientConn{
		url:       strings.TrimSuffix(config.URL, "/"),
		authType:  config.AuthType,
		authToken: config.AuthToken,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Listen starts an HTTP server listener.
func (t *HTTPTransport) Listen(ctx context.Context, config TransportConfig) (Listener, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return newHTTPListener(ctx, config)
}

// httpClientConn is an HTTP-based client connection.
type httpClientConn struct {
	url       string
	authType  string
	authToken string
	client    *http.Client
	closed    bool
	mu        sync.Mutex
}

// Send posts a JSON-RPC request over HTTP.
func (c *httpClientConn) Send(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("transport: connection closed")
	}
	c.mu.Unlock()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("transport: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/a2a/jsonrpc", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("transport: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setAuth(httpReq)

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("transport: http request: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		return nil, fmt.Errorf("transport: http status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var resp jsonrpc.Response
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("transport: decode response: %w", err)
	}
	return &resp, nil
}

// Stream opens an SSE stream for a JSON-RPC request.
func (c *httpClientConn) Stream(ctx context.Context, req *jsonrpc.Request) (<-chan Event, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("transport: connection closed")
	}
	c.mu.Unlock()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("transport: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/a2a/stream", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("transport: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	c.setAuth(httpReq)

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("transport: http request: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		httpResp.Body.Close()
		return nil, fmt.Errorf("transport: http status %d", httpResp.StatusCode)
	}

	ch := make(chan Event, 16)
	go func() {
		defer httpResp.Body.Close()
		defer close(ch)
		c.readSSE(ctx, httpResp.Body, ch)
	}()
	return ch, nil
}

// readSSE reads Server-Sent Events from a reader into the event channel.
func (c *httpClientConn) readSSE(ctx context.Context, r io.Reader, ch chan<- Event) {
	scanner := bufio.NewScanner(r)
	var eventType string
	var dataBuf bytes.Buffer

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = end of event
			if dataBuf.Len() > 0 {
				evt := parseSSEData(eventType, dataBuf.Bytes())
				select {
				case ch <- evt:
				case <-ctx.Done():
					return
				}
				dataBuf.Reset()
				eventType = ""
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimSpace(data)
			dataBuf.WriteString(data)
		}
		// Ignore comments (lines starting with ':') and unknown fields.
	}
}

// parseSSEData parses the data field of an SSE event into an Event.
func parseSSEData(eventType string, data []byte) Event {
	evt := Event{
		Kind:    eventType,
		Payload: json.RawMessage(data),
	}
	// Try to extract taskID from payload.
	var peek struct {
		TaskID string `json:"taskId"`
		ID     string `json:"id"`
	}
	if json.Unmarshal(data, &peek) == nil {
		if peek.TaskID != "" {
			evt.TaskID = peek.TaskID
		} else if peek.ID != "" {
			evt.TaskID = peek.ID
		}
	}
	return evt
}

func (c *httpClientConn) setAuth(req *http.Request) {
	switch c.authType {
	case "bearer":
		if c.authToken != "" {
			req.Header.Set("Authorization", "Bearer "+c.authToken)
		}
	}
}

// Close marks the connection as closed.
func (c *httpClientConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.client.CloseIdleConnections()
	return nil
}

// httpListener is an HTTP-based server listener.
type httpListener struct {
	ln      net.Listener
	srv     *http.Server
	router  *chi.Mux
	handler Handler
	mu      sync.Mutex
	conns   chan Conn
	done    chan struct{}
	logger  *slog.Logger
}

// Handler is an optional handler that the HTTP listener delegates JSON-RPC
// requests to. If not set, the listener buffers connections for Accept().
type Handler interface {
	HandleRequest(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response
}

func newHTTPListener(_ context.Context, config TransportConfig) (*httpListener, error) {
	addr := config.URL
	// If URL looks like a full URL, extract host:port.
	if strings.HasPrefix(addr, "http://") {
		addr = strings.TrimPrefix(addr, "http://")
	} else if strings.HasPrefix(addr, "https://") {
		addr = strings.TrimPrefix(addr, "https://")
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("transport: listen %s: %w", addr, err)
	}

	hl := &httpListener{
		ln:     ln,
		conns:  make(chan Conn, 16),
		done:   make(chan struct{}),
		logger: slog.Default(),
	}

	r := chi.NewRouter()
	r.Post("/a2a/jsonrpc", hl.handleJSONRPC)
	r.Post("/a2a/stream", hl.handleStream)
	hl.router = r

	hl.srv = &http.Server{
		Handler: r,
	}

	go func() {
		if err := hl.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			hl.logger.Error("http listener serve error", "error", err)
		}
	}()

	return hl, nil
}

// SetHandler sets the JSON-RPC handler for incoming requests.
func (hl *httpListener) SetHandler(h Handler) {
	hl.mu.Lock()
	defer hl.mu.Unlock()
	hl.handler = h
}

func (hl *httpListener) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	var req jsonrpc.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	hl.mu.Lock()
	h := hl.handler
	hl.mu.Unlock()

	if h == nil {
		// No handler set; create a pipe-based conn for Accept().
		sc := newServerConn()
		sc.incomingReq = &req

		select {
		case hl.conns <- sc:
		case <-hl.done:
			http.Error(w, "server closed", http.StatusServiceUnavailable)
			return
		}

		// Wait for response.
		select {
		case resp := <-sc.responseCh:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		case <-r.Context().Done():
			return
		case <-hl.done:
			http.Error(w, "server closed", http.StatusServiceUnavailable)
		}
		return
	}

	resp := h.HandleRequest(r.Context(), &req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (hl *httpListener) handleStream(w http.ResponseWriter, r *http.Request) {
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

	hl.mu.Lock()
	h := hl.handler
	hl.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if h != nil {
		// Direct handler mode: send a single response as an SSE event.
		resp := h.HandleRequest(r.Context(), &req)
		data, _ := json.Marshal(resp)
		fmt.Fprintf(w, "event: final\ndata: %s\n\n", data)
		flusher.Flush()
		return
	}

	// Accept mode: create a server conn with streaming support.
	sc := newServerConn()
	sc.incomingReq = &req
	sc.streamWriter = w
	sc.streamFlusher = flusher
	sc.streamCtx = r.Context()

	select {
	case hl.conns <- sc:
	case <-hl.done:
		return
	}

	// Block until the stream is done.
	select {
	case <-sc.streamDone:
	case <-r.Context().Done():
	case <-hl.done:
	}
}

// Accept returns the next incoming connection.
func (hl *httpListener) Accept(ctx context.Context) (Conn, error) {
	select {
	case conn := <-hl.conns:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-hl.done:
		return nil, fmt.Errorf("transport: listener closed")
	}
}

// Close shuts down the HTTP listener.
func (hl *httpListener) Close() error {
	select {
	case <-hl.done:
		return nil // already closed
	default:
		close(hl.done)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return hl.srv.Shutdown(ctx)
}

// Addr returns the listener address.
func (hl *httpListener) Addr() string {
	return hl.ln.Addr().String()
}

// serverConn is a server-side connection wrapping an HTTP request/response pair.
type serverConn struct {
	incomingReq  *jsonrpc.Request
	responseCh   chan *jsonrpc.Response
	streamWriter http.ResponseWriter
	streamFlusher http.Flusher
	streamCtx    context.Context
	streamDone   chan struct{}
	consumed     bool
	mu           sync.Mutex
}

func newServerConn() *serverConn {
	return &serverConn{
		responseCh: make(chan *jsonrpc.Response, 1),
		streamDone: make(chan struct{}),
	}
}

// Send reads the buffered incoming request (first call) or sends a response.
// For server-side conns, the first Send is actually receiving the client request,
// so we model it as: the conn holds the request, and Send sends the response back.
func (sc *serverConn) Send(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.consumed && sc.incomingReq != nil {
		// The caller is sending a response to the original request.
		// This doesn't quite fit the Conn interface for server-side usage.
		// For server conns, use the Response method directly.
		return nil, fmt.Errorf("transport: server conn: use Respond() to send responses")
	}
	return nil, fmt.Errorf("transport: server conn does not support outgoing requests")
}

// IncomingRequest returns the request that arrived on this connection.
func (sc *serverConn) IncomingRequest() *jsonrpc.Request {
	return sc.incomingReq
}

// Respond sends a JSON-RPC response back to the HTTP client.
func (sc *serverConn) Respond(resp *jsonrpc.Response) {
	select {
	case sc.responseCh <- resp:
	default:
	}
}

// SendEvent sends an SSE event to the streaming client.
func (sc *serverConn) SendEvent(evt Event) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.streamWriter == nil {
		return fmt.Errorf("transport: not a streaming connection")
	}
	data, err := json.Marshal(evt.Payload)
	if err != nil {
		return fmt.Errorf("transport: marshal event: %w", err)
	}
	_, err = fmt.Fprintf(sc.streamWriter, "event: %s\ndata: %s\n\n", evt.Kind, data)
	if err != nil {
		return err
	}
	sc.streamFlusher.Flush()
	return nil
}

// CloseStream closes the SSE stream.
func (sc *serverConn) CloseStream() {
	select {
	case <-sc.streamDone:
	default:
		close(sc.streamDone)
	}
}

// Stream is not supported on server-side connections (use Accept + IncomingRequest).
func (sc *serverConn) Stream(ctx context.Context, req *jsonrpc.Request) (<-chan Event, error) {
	return nil, fmt.Errorf("transport: server conn does not support outgoing streams")
}

// Close closes the server connection.
func (sc *serverConn) Close() error {
	sc.CloseStream()
	return nil
}
