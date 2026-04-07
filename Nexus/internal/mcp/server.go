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

package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/BubbleFish-Nexus/internal/version"
)

// JSON-RPC 2.0 error codes.
// Reference: https://www.jsonrpc.org/specification#error_object
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
	rpcInternalError  = -32603

	// rpcAuthError is a BubbleFish-specific code for authentication failures.
	rpcAuthError = -32001
)

// MCP protocol version this server advertises.
const mcpProtocolVersion = "2024-11-05"

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 wire types
// ---------------------------------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // int | string | null per spec; ABSENT for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether this request is a JSON-RPC notification.
//
// Per JSON-RPC 2.0 spec section 4.1: "A Notification is a Request object
// without an 'id' member." Servers MUST NOT respond to notifications.
//
// We distinguish "absent id" (notification) from "id: null" (response to a
// malformed request) by checking the length of the RawMessage. encoding/json
// leaves req.ID as a zero-length slice when the field is absent from the
// input, and as the four bytes "null" when the field is present with a null
// value.
func (r *rpcRequest) isNotification() bool {
	return len(r.ID) == 0
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// ---------------------------------------------------------------------------
// MCP-specific request/response shapes
// ---------------------------------------------------------------------------

type initializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	ServerInfo      serverInfo             `json:"serverInfo"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolsListResult struct {
	Tools []toolDef `json:"tools"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolCallResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

// Server is the MCP JSON-RPC 2.0 HTTP server.
//
// Invariants:
//   - NEVER binds to 0.0.0.0. Only 127.0.0.1.
//   - All auth uses subtle.ConstantTimeCompare.
//   - Startup failure does NOT crash the daemon (non-fatal Start error).
//   - Notifications (requests without 'id') NEVER receive a response body.
//
// Reference: Tech Spec Section 14.3.
type Server struct {
	resolvedKey []byte
	sourceName  string
	pipeline    Pipeline
	logger      *slog.Logger
	bind        string
	port        int

	httpServer *http.Server
	listener   net.Listener
	addr       string // actual bound addr after Start()
	stopOnce   sync.Once
}

// New creates a new MCP Server. The resolvedKey must already be the decoded
// byte value (not an env:/file: reference). The bind address MUST be
// 127.0.0.1 — callers that pass another value will get an error on Start().
//
// Reference: Tech Spec Section 14.3 — Invariant 1.
func New(bind string, port int, resolvedKey []byte, sourceName string, pipeline Pipeline, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		resolvedKey: resolvedKey,
		sourceName:  sourceName,
		pipeline:    pipeline,
		logger:      logger,
		bind:        bind,
		port:        port,
	}
}

// Start binds to the configured address and begins serving MCP requests in a
// background goroutine. Returns an error immediately if the listener cannot be
// created (e.g. port conflict). Does NOT block.
//
// Callers must treat a non-nil error as non-fatal and log a WARN — the daemon
// MUST continue running even if MCP fails to start.
//
// Reference: Tech Spec Section 14.3 — "Startup failure does NOT crash daemon."
func (s *Server) Start() error {
	// INVARIANT: bind MUST be 127.0.0.1. Reject anything else.
	// Reference: Tech Spec Section 14.3 — "Bind: 127.0.0.1 ONLY. Never 0.0.0.0."
	if s.bind != "127.0.0.1" {
		return fmt.Errorf("mcp: bind address must be 127.0.0.1, got %q", s.bind)
	}

	addr := fmt.Sprintf("%s:%d", s.bind, s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mcp: listen %s: %w", addr, err)
	}
	s.listener = ln
	s.addr = ln.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleRPC)

	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Warn("mcp: server exited unexpectedly",
				"component", "mcp",
				"error", err,
			)
		}
	}()

	s.logger.Info("mcp: server started",
		"component", "mcp",
		"addr", s.addr,
	)

	return nil
}

// Stop gracefully shuts down the MCP server. Safe to call multiple times;
// only the first call has effect (sync.Once).
func (s *Server) Stop() error {
	var firstErr error
	s.stopOnce.Do(func() {
		if s.httpServer == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			firstErr = fmt.Errorf("mcp: shutdown: %w", err)
		}
		s.logger.Info("mcp: server stopped", "component", "mcp")
	})
	return firstErr
}

// Addr returns the actual bound address (e.g. "127.0.0.1:7474"). Empty string
// before Start() is called successfully.
func (s *Server) Addr() string {
	return s.addr
}

// ---------------------------------------------------------------------------
// HTTP handler
// ---------------------------------------------------------------------------

// handleRPC is the single HTTP POST handler for all MCP JSON-RPC messages.
//
// Notifications (requests without an 'id' field) are processed for side
// effects but receive HTTP 204 No Content with an empty body — never a
// JSON-RPC response. This is required by the JSON-RPC 2.0 spec and by MCP
// clients (Claude Desktop's Zod schema rejects responses to notifications).
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Authenticate via dedicated mcp_key.
	// INVARIANT: constant-time comparison; no early exit.
	// Reference: Tech Spec Section 14.3 — "Auth: Dedicated mcp_key."
	if !s.authenticate(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(rpcResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error: &rpcError{
				Code:    rpcAuthError,
				Message: "unauthorized: invalid or missing MCP key",
			},
		})
		return
	}

	// Decode the JSON-RPC request.
	var req rpcRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1*1024*1024))
	if err := dec.Decode(&req); err != nil {
		s.writeRPCError(w, json.RawMessage("null"), rpcParseError, "parse error: "+err.Error())
		return
	}

	if req.JSONRPC != "2.0" {
		// Even for notifications, we cannot signal errors back. Log and drop.
		if req.isNotification() {
			s.logger.Warn("mcp: notification with invalid jsonrpc field",
				"component", "mcp",
				"method", req.Method,
			)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.writeRPCError(w, req.ID, rpcInvalidRequest, "jsonrpc field must be '2.0'")
		return
	}

	// Notifications: process for side effects, return HTTP 204, never a body.
	// Per JSON-RPC 2.0 spec section 4.1: servers MUST NOT respond to notifications.
	if req.isNotification() {
		s.handleNotification(req)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Dispatch regular requests to the appropriate method handler.
	switch req.Method {
	case "initialize":
		s.handleInitialize(w, req)
	case "tools/list":
		s.handleToolsList(w, req)
	case "tools/call":
		s.handleToolsCall(w, r.Context(), req)
	case "ping":
		s.writeRPCResult(w, req.ID, map[string]interface{}{})
	default:
		s.writeRPCError(w, req.ID, rpcMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

// handleNotification processes JSON-RPC notifications (no response body).
//
// Currently recognized notifications:
//   - notifications/initialized: client confirms it received initialize result
//   - notifications/cancelled: client cancels an in-flight request (logged only)
//
// Unknown notifications are logged at DEBUG and silently dropped, per spec.
func (s *Server) handleNotification(req rpcRequest) {
	switch req.Method {
	case "notifications/initialized":
		s.logger.Debug("mcp: client initialized",
			"component", "mcp",
		)
	case "notifications/cancelled":
		s.logger.Debug("mcp: client cancelled request",
			"component", "mcp",
		)
	default:
		s.logger.Debug("mcp: unknown notification dropped",
			"component", "mcp",
			"method", req.Method,
		)
	}
}

// authenticate validates the MCP request using a constant-time comparison of
// the Bearer token against the server's resolved MCP key.
//
// INVARIANT: uses subtle.ConstantTimeCompare — never ==.
// Reference: Tech Spec Section 14.3, CLAUDE.md critical rule.
func (s *Server) authenticate(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	provided := []byte(strings.TrimSpace(h[len(prefix):]))
	return subtle.ConstantTimeCompare(provided, s.resolvedKey) == 1
}

// ---------------------------------------------------------------------------
// Method handlers
// ---------------------------------------------------------------------------

func (s *Server) handleInitialize(w http.ResponseWriter, req rpcRequest) {
	s.writeRPCResult(w, req.ID, initializeResult{
		ProtocolVersion: mcpProtocolVersion,
		Capabilities: map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		ServerInfo: serverInfo{
			Name:    "bubblefish-nexus",
			Version: version.Version,
		},
	})
}

func (s *Server) handleToolsList(w http.ResponseWriter, req rpcRequest) {
	s.writeRPCResult(w, req.ID, toolsListResult{Tools: toolList()})
}

func (s *Server) handleToolsCall(w http.ResponseWriter, ctx context.Context, req rpcRequest) {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeRPCError(w, req.ID, rpcInvalidParams, "invalid params: "+err.Error())
		return
	}

	switch params.Name {
	case "nexus_write":
		s.callNexusWrite(w, ctx, req, params.Arguments)
	case "nexus_search":
		s.callNexusSearch(w, ctx, req, params.Arguments)
	case "nexus_status":
		s.callNexusStatus(w, ctx, req)
	default:
		s.writeRPCError(w, req.ID, rpcMethodNotFound, fmt.Sprintf("unknown tool %q", params.Name))
	}
}

func (s *Server) callNexusWrite(w http.ResponseWriter, ctx context.Context, req rpcRequest, args json.RawMessage) {
	var a struct {
		Content     string `json:"content"`
		Subject     string `json:"subject"`
		Collection  string `json:"collection"`
		Destination string `json:"destination"`
		ActorType   string `json:"actor_type"`
		ActorID     string `json:"actor_id"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, req.ID, rpcInvalidParams, "invalid nexus_write arguments: "+err.Error())
			return
		}
	}

	if a.Content == "" {
		s.writeToolError(w, req.ID, "nexus_write requires 'content' argument")
		return
	}

	result, err := s.pipeline.Write(ctx, WriteParams{
		Source:      s.sourceName,
		Content:     a.Content,
		Subject:     a.Subject,
		Collection:  a.Collection,
		Destination: a.Destination,
		ActorType:   a.ActorType,
		ActorID:     a.ActorID,
	})
	if err != nil {
		s.logger.Error("mcp: nexus_write pipeline error",
			"component", "mcp",
			"error", err,
		)
		s.writeToolError(w, req.ID, "write failed: "+err.Error())
		return
	}

	out, _ := json.Marshal(result)
	s.writeRPCResult(w, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

func (s *Server) callNexusSearch(w http.ResponseWriter, ctx context.Context, req rpcRequest, args json.RawMessage) {
	var a struct {
		Q           string `json:"q"`
		Destination string `json:"destination"`
		Subject     string `json:"subject"`
		Limit       int    `json:"limit"`
		Profile     string `json:"profile"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, req.ID, rpcInvalidParams, "invalid nexus_search arguments: "+err.Error())
			return
		}
	}

	result, err := s.pipeline.Search(ctx, SearchParams{
		Source:      s.sourceName,
		Q:           a.Q,
		Destination: a.Destination,
		Subject:     a.Subject,
		Limit:       a.Limit,
		Profile:     a.Profile,
	})
	if err != nil {
		s.logger.Error("mcp: nexus_search pipeline error",
			"component", "mcp",
			"error", err,
		)
		s.writeToolError(w, req.ID, "search failed: "+err.Error())
		return
	}

	out, _ := json.Marshal(result)
	s.writeRPCResult(w, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

func (s *Server) callNexusStatus(w http.ResponseWriter, ctx context.Context, req rpcRequest) {
	result, err := s.pipeline.Status(ctx)
	if err != nil {
		s.logger.Error("mcp: nexus_status pipeline error",
			"component", "mcp",
			"error", err,
		)
		s.writeToolError(w, req.ID, "status failed: "+err.Error())
		return
	}

	out, _ := json.Marshal(result)
	s.writeRPCResult(w, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func (s *Server) writeRPCResult(w http.ResponseWriter, id json.RawMessage, result interface{}) {
	w.Header().Set("Content-Type", "application/json")
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("mcp: encode response", "component", "mcp", "error", err)
	}
}

func (s *Server) writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("mcp: encode error response", "component", "mcp", "error", err)
	}
}

func (s *Server) writeToolError(w http.ResponseWriter, id json.RawMessage, msg string) {
	s.writeRPCResult(w, id, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: msg}},
		IsError: true,
	})
}