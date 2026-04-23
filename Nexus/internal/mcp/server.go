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
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bubblefish-tech/nexus/internal/mcp/bridge"
	"github.com/bubblefish-tech/nexus/internal/version"
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
const mcpProtocolVersion = "2025-11-25"

// ---------------------------------------------------------------------------
// CORS constants
// ---------------------------------------------------------------------------

// corsAllowedHeaders lists all headers MCP clients may send.
const corsAllowedHeaders = "Content-Type, Accept, Authorization, Mcp-Session-Id, Last-Event-ID, X-Requested-With"

// corsAllowedMethods lists all HTTP methods the MCP endpoint handles.
const corsAllowedMethods = "POST, GET, DELETE, OPTIONS"

// ---------------------------------------------------------------------------
// JSON-RPC 2.0 wire types
// ---------------------------------------------------------------------------

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether this request is a JSON-RPC notification.
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
// SSE session registry
// ---------------------------------------------------------------------------

type sseSession struct {
	id     string
	events chan string
	done   chan struct{}
}

type sseRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*sseSession
}

func newSSERegistry() *sseRegistry {
	return &sseRegistry{sessions: make(map[string]*sseSession)}
}

func (reg *sseRegistry) add(sess *sseSession) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.sessions[sess.id] = sess
}

func (reg *sseRegistry) remove(id string) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	delete(reg.sessions, id)
}

func (reg *sseRegistry) get(id string) (*sseSession, bool) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	s, ok := reg.sessions[id]
	return s, ok
}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ---------------------------------------------------------------------------
// JWT validation interface (satisfied by *oauth.OAuthServer)
// ---------------------------------------------------------------------------

// JWTValidator validates JWT access tokens. When non-nil on the Server,
// authenticate() will accept valid JWTs in addition to static bfn_mcp_ keys.
type JWTValidator interface {
	ValidateAccessToken(tokenString string) bool
}

// HandlerRegistrar registers additional HTTP handlers on the MCP mux.
// Satisfied by *oauth.OAuthServer.
type HandlerRegistrar interface {
	RegisterHandlers(mux *http.ServeMux)
}

// ---------------------------------------------------------------------------
// Server
// ---------------------------------------------------------------------------

type Server struct {
	resolvedKey  []byte
	sourceName   string
	pipeline     Pipeline
	logger       *slog.Logger
	bind         string
	port         int
	oauthServer    JWTValidator    // optional; nil when OAuth is disabled
	oauthHandlers  HandlerRegistrar // optional; registers OAuth HTTP endpoints
	oauthIssuerURL string           // set when OAuth is enabled; used for WWW-Authenticate header

	// toolPolicyChecker evaluates per-agent tool-use policies before dispatch.
	// Nil when no tool policies are configured. Reference: AG.4.
	toolPolicyChecker ToolPolicyCheckerIface

	// coordinationProvider handles agent-to-agent coordination MCP tools.
	// Nil when coordination is not enabled. Reference: AG.5.
	coordinationProvider CoordinationProvider

	// a2aBridge dispatches A2A bridge tool calls. Nil when A2A is disabled.
	a2aBridge *bridge.Bridge

	// controlPlane handles governed control-plane MCP tools (MT.4).
	// Nil when the control plane is disabled.
	controlPlane ControlPlaneProvider

	// orchestrateProvider handles multi-agent orchestration MCP tools (DISC.4).
	// Nil when orchestration is not enabled.
	orchestrateProvider OrchestrateProvider

	tlsConfig *tls.Config // nil when TLS is disabled (default)

	subscribeStore SubscribeStore

	httpServer *http.Server
	listener   net.Listener
	addr       string
	stopOnce   sync.Once

	sseReg *sseRegistry

	// statusCacheJSON caches the serialized nexus_status response (5s TTL).
	statusCacheMu  sync.RWMutex
	statusCacheJSON []byte
	statusCacheAt  time.Time
}

// ToolPolicyCheckerIface is the interface for tool-use policy enforcement.
// Implemented by policy.ToolPolicyChecker.
type ToolPolicyCheckerIface interface {
	Check(agentID, toolName string, args json.RawMessage) ToolPolicyDecision
}

// ToolPolicyDecision mirrors policy.ToolPolicyDecision to avoid circular imports.
type ToolPolicyDecision struct {
	Allowed bool
	Reason  string
}

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
		sseReg:      newSSERegistry(),
	}
}

// SetOAuthServer configures an optional JWT validator for OAuth access tokens.
// When set, authenticate() accepts valid JWTs in addition to static bfn_mcp_ keys.
// Pass nil to disable JWT authentication.
func (s *Server) SetOAuthServer(v JWTValidator) {
	s.oauthServer = v
}

// SetOAuthHandlers configures an optional handler registrar for OAuth HTTP
// endpoints. Must be called before Start(). The registrar's RegisterHandlers
// method will be called on the MCP server's HTTP mux.
func (s *Server) SetOAuthHandlers(reg HandlerRegistrar) {
	s.oauthHandlers = reg
}

// SetToolPolicyChecker configures the per-agent tool-use policy checker.
// When set, every tools/call request is checked against the agent's policy
// before dispatch. Must be called before Start(). Reference: AG.4.
func (s *Server) SetToolPolicyChecker(checker ToolPolicyCheckerIface) {
	s.toolPolicyChecker = checker
}

// SetOAuthIssuerURL sets the OAuth issuer URL used in WWW-Authenticate headers
// on 401 responses. Must be called before Start() when OAuth is enabled.
func (s *Server) SetOAuthIssuerURL(url string) {
	s.oauthIssuerURL = url
}

// SetBridge configures the A2A bridge. When set, the 9 a2a_* MCP tools
// are advertised in tools/list and dispatched through the bridge.
// Must be called before Start(). Pass nil to disable A2A tools.
func (s *Server) SetBridge(b *bridge.Bridge) {
	s.a2aBridge = b
}

// SetTLSConfig enables TLS on the MCP server. Must be called before Start().
// When set, the server wraps its TCP listener with the provided TLS config.
func (s *Server) SetTLSConfig(cfg *tls.Config) {
	s.tlsConfig = cfg
}

func (s *Server) Start() error {
	if s.bind != "127.0.0.1" {
		return fmt.Errorf("mcp: bind address must be 127.0.0.1, got %q", s.bind)
	}

	addr := fmt.Sprintf("%s:%d", s.bind, s.port)
	rawLn, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("mcp: listen %s: %w", addr, err)
	}
	var ln net.Listener = rawLn
	if s.tlsConfig != nil {
		ln = tls.NewListener(rawLn, s.tlsConfig)
	}
	s.listener = ln
	s.addr = rawLn.Addr().String()

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)

	// Register OAuth endpoints on the same mux if configured.
	if s.oauthHandlers != nil {
		s.oauthHandlers.RegisterHandlers(mux)
	}

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

func (s *Server) Addr() string {
	return s.addr
}

// ---------------------------------------------------------------------------
// CORS helpers
// ---------------------------------------------------------------------------

func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", corsAllowedMethods)
	w.Header().Set("Access-Control-Allow-Headers", corsAllowedHeaders)
	w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id, Content-Type")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

// ---------------------------------------------------------------------------
// Transport helpers
// ---------------------------------------------------------------------------

func wantsSSE(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func writeJSON(w http.ResponseWriter, r *http.Request, v interface{}) error {
	setCORSHeaders(w)

	b, err := json.Marshal(v)
	if err != nil {
		return err
	}

	if wantsSSE(r) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		_, err = fmt.Fprintf(w, "data: %s\n\n", b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(b)
	return err
}

// ---------------------------------------------------------------------------
// Top-level HTTP dispatcher
// ---------------------------------------------------------------------------

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)

	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)

	case http.MethodGet:
		s.handleSSEStream(w, r)

	case http.MethodPost:
		s.handleRPC(w, r)

	case http.MethodDelete:
		s.handleSessionDelete(w, r)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// GET handler — SSE transport
// ---------------------------------------------------------------------------

func (s *Server) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(r) {
		setCORSHeaders(w)
		s.setWWWAuthenticate(w)
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

	sessionID, err := newSessionID()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sess := &sseSession{
		id:     sessionID,
		events: make(chan string, 64),
		done:   make(chan struct{}),
	}
	s.sseReg.add(sess)
	defer func() {
		s.sseReg.remove(sessionID)
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	setCORSHeaders(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Mcp-Session-Id", sessionID)
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "event: endpoint\ndata: /mcp?session_id=%s\n\n", sessionID)
	flusher.Flush()

	s.logger.Info("mcp: SSE session opened",
		"component", "mcp",
		"session_id", sessionID,
		"remote", r.RemoteAddr,
	)

	keepalive := time.NewTicker(30 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case event := <-sess.events:
			fmt.Fprint(w, event)
			flusher.Flush()

		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()

		case <-r.Context().Done():
			s.logger.Info("mcp: SSE session closed by client",
				"component", "mcp",
				"session_id", sessionID,
			)
			return
		}
	}
}

// ---------------------------------------------------------------------------
// DELETE handler — session termination
// ---------------------------------------------------------------------------

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(r) {
		setCORSHeaders(w)
		s.setWWWAuthenticate(w)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	sessionID := r.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = r.URL.Query().Get("session_id")
	}

	if sessionID == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	sess, ok := s.sseReg.get(sessionID)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	select {
	case <-sess.done:
	default:
		close(sess.done)
	}

	s.logger.Info("mcp: SSE session deleted by client",
		"component", "mcp",
		"session_id", sessionID,
	)

	setCORSHeaders(w)
	w.WriteHeader(http.StatusOK)
}

// ---------------------------------------------------------------------------
// POST handler — JSON-RPC dispatch
// ---------------------------------------------------------------------------

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if !s.authenticate(r) {
		setCORSHeaders(w)
		s.setWWWAuthenticate(w)
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

	var req rpcRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1*1024*1024))
	if err := dec.Decode(&req); err != nil {
		s.writeRPCError(w, r, json.RawMessage("null"), rpcParseError, "parse error: "+err.Error())
		return
	}

	if req.JSONRPC != "2.0" {
		if req.isNotification() {
			s.logger.Warn("mcp: notification with invalid jsonrpc field",
				"component", "mcp",
				"method", req.Method,
			)
			setCORSHeaders(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.writeRPCError(w, r, req.ID, rpcInvalidRequest, "jsonrpc field must be '2.0'")
		return
	}

	if req.isNotification() {
		s.handleNotification(req)
		setCORSHeaders(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID != "" {
		if sess, ok := s.sseReg.get(sessionID); ok {
			s.handleRPCForSession(w, r, req, sess)
			return
		}
		s.logger.Warn("mcp: unknown session_id in POST, falling back to direct response",
			"component", "mcp",
			"session_id", sessionID,
		)
	}

	s.dispatchRPC(w, r, req)
}

func (s *Server) handleRPCForSession(w http.ResponseWriter, r *http.Request, req rpcRequest, sess *sseSession) {
	buf := &responseBuffer{}
	s.dispatchRPC(buf, r, req)

	if len(buf.body) > 0 {
		event := fmt.Sprintf("data: %s\n\n", buf.body)
		select {
		case sess.events <- event:
		default:
			s.logger.Warn("mcp: SSE session event buffer full, dropping response",
				"component", "mcp",
				"session_id", sess.id,
			)
		}
	}

	setCORSHeaders(w)
	w.WriteHeader(http.StatusAccepted)
}

type responseBuffer struct {
	header http.Header
	status int
	body   []byte
}

func (b *responseBuffer) Header() http.Header {
	if b.header == nil {
		b.header = make(http.Header)
	}
	return b.header
}

func (b *responseBuffer) Write(p []byte) (int, error) {
	b.body = append(b.body, p...)
	return len(p), nil
}

func (b *responseBuffer) WriteHeader(status int) {
	b.status = status
}

func (s *Server) dispatchRPC(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	switch req.Method {
	case "initialize":
		s.handleInitialize(w, r, req)
	case "tools/list":
		s.handleToolsList(w, r, req)
	case "tools/call":
		s.handleToolsCall(w, r, req)
	case "ping":
		s.writeRPCResult(w, r, req.ID, map[string]interface{}{})
	default:
		s.writeRPCError(w, r, req.ID, rpcMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

func (s *Server) handleNotification(req rpcRequest) {
	switch req.Method {
	case "notifications/initialized":
		s.logger.Debug("mcp: client initialized", "component", "mcp")
	case "notifications/cancelled":
		s.logger.Debug("mcp: client cancelled request", "component", "mcp")
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
// Accepts Bearer token in Authorization header OR ?key= query param as a
// fallback for remote clients (Claude Web UI, Perplexity Comet) that embed
// the key in the URL rather than sending an Authorization header.
//
// INVARIANT: uses subtle.ConstantTimeCompare — never ==.
func (s *Server) authenticate(r *http.Request) bool {
	token := s.extractBearerToken(r)
	if token == "" {
		return false
	}

	// Path 1: Static bfn_mcp_ key (existing — unchanged)
	if strings.HasPrefix(token, "bfn_mcp_") {
		provided := []byte(token)
		return subtle.ConstantTimeCompare(provided, s.resolvedKey) == 1
	}

	// Path 2: JWT access token (new — OAuth clients)
	if s.oauthServer != nil {
		return s.oauthServer.ValidateAccessToken(token)
	}

	// Non-prefixed token but no OAuth configured — try static key comparison
	// as fallback for legacy keys that don't have the bfn_mcp_ prefix.
	provided := []byte(token)
	return subtle.ConstantTimeCompare(provided, s.resolvedKey) == 1
}

// setWWWAuthenticate adds a WWW-Authenticate header to 401 responses when OAuth
// is enabled, per RFC 6750 §3 and the MCP OAuth specification.
func (s *Server) setWWWAuthenticate(w http.ResponseWriter) {
	if s.oauthServer != nil && s.oauthIssuerURL != "" {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(
			`Bearer realm="BubbleFish Nexus", authorization_uri="%s/oauth/authorize", resource_metadata="%s/.well-known/oauth-authorization-server"`,
			s.oauthIssuerURL, s.oauthIssuerURL,
		))
	}
}

// extractBearerToken retrieves the bearer token from the Authorization header
// or the ?key= query parameter.
func (s *Server) extractBearerToken(r *http.Request) string {
	// Try Authorization: Bearer header first.
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if strings.HasPrefix(h, prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	// Fallback: ?key= query param for clients that embed auth in the URL.
	return r.URL.Query().Get("key")
}

// ---------------------------------------------------------------------------
// Method handlers
// ---------------------------------------------------------------------------

func (s *Server) handleInitialize(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	s.writeRPCResult(w, r, req.ID, initializeResult{
		ProtocolVersion: mcpProtocolVersion,
		Capabilities: map[string]interface{}{
			"tools": map[string]interface{}{
				"listChanged": true,
			},
		},
		ServerInfo: serverInfo{
			Name:    "nexus-nexus",
			Version: version.Version,
		},
	})
}

func (s *Server) handleToolsList(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	tools := toolList()
	if s.a2aBridge != nil {
		tools = append(tools, a2aToolDefs(s.a2aBridge)...)
	}
	if s.controlPlane != nil {
		tools = append(tools, controlToolDefs()...)
	}
	if s.orchestrateProvider != nil {
		tools = append(tools, orchestrateToolDefs()...)
	}
	s.writeRPCResult(w, r, req.ID, toolsListResult{Tools: tools})
}

func (s *Server) handleToolsCall(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid params: "+err.Error())
		return
	}

	// Tool-use policy enforcement (AG.4). Check before dispatch.
	if s.toolPolicyChecker != nil {
		agentID := r.Header.Get("X-Agent-ID")
		decision := s.toolPolicyChecker.Check(agentID, params.Name, params.Arguments)
		if !decision.Allowed {
			s.logger.Warn("mcp: tool-use policy denied",
				"component", "mcp",
				"agent_id", agentID,
				"tool", params.Name,
				"reason", decision.Reason,
			)
			s.writeRPCError(w, r, req.ID, rpcAuthError, decision.Reason)
			return
		}
	}

	switch params.Name {
	case "nexus_write":
		s.callNexusWrite(w, r, req, params.Arguments)
	case "nexus_search":
		s.callNexusSearch(w, r, req, params.Arguments)
	case "nexus_status":
		s.callNexusStatus(w, r, req)
	case "agent_broadcast":
		s.callAgentBroadcast(w, r, req, params.Arguments)
	case "agent_pull_signals":
		s.callAgentPullSignals(w, r, req, params.Arguments)
	case "agent_status_query":
		s.callAgentStatusQuery(w, r, req, params.Arguments)
	case "a2a_list_agents", "a2a_describe_agent", "a2a_send_to_agent",
		"a2a_stream_to_agent", "a2a_get_task", "a2a_resume_task",
		"a2a_cancel_task", "a2a_list_pending_approvals", "a2a_list_grants":
		s.callA2ABridgeTool(w, r, req, params.Name, params.Arguments)
	case "nexus_grant_list":
		s.callNexusGrantList(w, r, req)
	case "nexus_approval_request":
		s.callNexusApprovalRequest(w, r, req, params.Arguments)
	case "nexus_approval_status":
		s.callNexusApprovalStatus(w, r, req, params.Arguments)
	case "nexus_task_create":
		s.callNexusTaskCreate(w, r, req, params.Arguments)
	case "nexus_task_status":
		s.callNexusTaskStatus(w, r, req, params.Arguments)
	case "nexus_action_log":
		s.callNexusActionLog(w, r, req, params.Arguments)
	case "nexus_list_agents":
		s.callNexusListAgents(w, r, req)
	case "nexus_orchestrate":
		s.callNexusOrchestrate(w, r, req, params.Arguments)
	case "nexus_council":
		s.callNexusCouncil(w, r, req, params.Arguments)
	case "nexus_broadcast":
		s.callNexusBroadcast(w, r, req, params.Arguments)
	case "nexus_collect":
		s.callNexusCollect(w, r, req, params.Arguments)
	case "nexus_subscribe":
		s.callNexusSubscribe(w, r, req, params.Arguments)
	case "nexus_unsubscribe":
		s.callNexusUnsubscribe(w, r, req, params.Arguments)
	case "nexus_subscriptions":
		s.callNexusSubscriptions(w, r, req)
	default:
		s.writeRPCError(w, r, req.ID, rpcMethodNotFound, fmt.Sprintf("unknown tool %q", params.Name))
	}
}

func (s *Server) callNexusWrite(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	var a struct {
		Content        string `json:"content"`
		Subject        string `json:"subject"`
		Collection     string `json:"collection"`
		Destination    string `json:"destination"`
		ActorType      string `json:"actor_type"`
		ActorID        string `json:"actor_id"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_write arguments: "+err.Error())
			return
		}
	}

	if a.Content == "" {
		s.writeToolError(w, r, req.ID, "nexus_write requires 'content' argument")
		return
	}

	// Auto-generate idempotency key if not provided by the client.
	// SHA-256(session_id || content || timestamp_second) ensures that
	// identical content within the same second from the same session is
	// deduplicated, preventing duplicate writes from network retries.
	// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.5.
	idemKey := a.IdempotencyKey
	if idemKey == "" {
		sessionID := r.Header.Get("Mcp-Session-Id")
		if sessionID == "" {
			sessionID = r.URL.Query().Get("session_id")
		}
		idemKey = generateIdempotencyKey(sessionID, a.Content)
	}

	result, err := s.pipeline.Write(r.Context(), WriteParams{
		Source:         s.sourceName,
		Content:        a.Content,
		Subject:        a.Subject,
		Collection:     a.Collection,
		Destination:    a.Destination,
		ActorType:      a.ActorType,
		ActorID:        a.ActorID,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		s.logger.Error("mcp: nexus_write pipeline error", "component", "mcp", "error", err)
		s.writeToolError(w, r, req.ID, "write failed: "+err.Error())
		return
	}

	out, _ := json.Marshal(result)
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

func (s *Server) callNexusSearch(w http.ResponseWriter, r *http.Request, req rpcRequest, args json.RawMessage) {
	var a struct {
		Q           string `json:"q"`
		Destination string `json:"destination"`
		Subject     string `json:"subject"`
		Limit       int    `json:"limit"`
		Profile     string `json:"profile"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &a); err != nil {
			s.writeRPCError(w, r, req.ID, rpcInvalidParams, "invalid nexus_search arguments: "+err.Error())
			return
		}
	}

	result, err := s.pipeline.Search(r.Context(), SearchParams{
		Source:      s.sourceName,
		Q:           a.Q,
		Destination: a.Destination,
		Subject:     a.Subject,
		Limit:       a.Limit,
		Profile:     a.Profile,
	})
	if err != nil {
		s.logger.Error("mcp: nexus_search pipeline error", "component", "mcp", "error", err)
		s.writeToolError(w, r, req.ID, "search failed: "+err.Error())
		return
	}

	out, _ := json.Marshal(result)
	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

func (s *Server) callNexusStatus(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	// Serve from cache if fresh (status changes slowly).
	s.statusCacheMu.RLock()
	if s.statusCacheJSON != nil && time.Since(s.statusCacheAt) < 5*time.Second {
		cached := s.statusCacheJSON
		s.statusCacheMu.RUnlock()
		s.writeRPCResult(w, r, req.ID, toolCallResult{
			Content: []contentBlock{{Type: "text", Text: string(cached)}},
		})
		return
	}
	s.statusCacheMu.RUnlock()

	result, err := s.pipeline.Status(r.Context())
	if err != nil {
		s.logger.Error("mcp: nexus_status pipeline error", "component", "mcp", "error", err)
		s.writeToolError(w, r, req.ID, "status failed: "+err.Error())
		return
	}

	out, _ := json.Marshal(result)

	s.statusCacheMu.Lock()
	s.statusCacheJSON = out
	s.statusCacheAt = time.Now()
	s.statusCacheMu.Unlock()

	s.writeRPCResult(w, r, req.ID, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: string(out)}},
	})
}

// ---------------------------------------------------------------------------
// Response helpers
// ---------------------------------------------------------------------------

func (s *Server) writeRPCResult(w http.ResponseWriter, r *http.Request, id json.RawMessage, result interface{}) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	if err := writeJSON(w, r, resp); err != nil {
		s.logger.Error("mcp: encode response", "component", "mcp", "error", err)
	}
}

func (s *Server) writeRPCError(w http.ResponseWriter, r *http.Request, id json.RawMessage, code int, msg string) {
	resp := rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: msg},
	}
	if err := writeJSON(w, r, resp); err != nil {
		s.logger.Error("mcp: encode error response", "component", "mcp", "error", err)
	}
}

func (s *Server) writeToolError(w http.ResponseWriter, r *http.Request, id json.RawMessage, msg string) {
	s.writeRPCResult(w, r, id, toolCallResult{
		Content: []contentBlock{{Type: "text", Text: msg}},
		IsError: true,
	})
}

// generateIdempotencyKey produces a deterministic idempotency key from the
// session ID and content. The timestamp is truncated to the current second
// so identical content within the same second from the same session
// produces the same key (preventing duplicate writes from network retries).
//
// Returns hex(SHA-256(sessionID || content || timestamp_second))[:64].
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.5.
func generateIdempotencyKey(sessionID, content string) string {
	ts := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	h := sha256.Sum256([]byte(sessionID + content + ts))
	return hex.EncodeToString(h[:])
}
