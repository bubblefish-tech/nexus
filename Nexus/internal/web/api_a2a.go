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

package web

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/governance"
	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/a2a/server"
	"github.com/BubbleFish-Nexus/internal/a2a/store"
	"github.com/BubbleFish-Nexus/internal/a2a/transport"
)

// A2ADashboard serves A2A-specific dashboard API endpoints.
// It is independent of the main Dashboard and does not modify it.
type A2ADashboard struct {
	registry   *registry.Store
	governance *governance.Engine
	taskStore  *store.SQLiteTaskStore
	adminToken string
	logger     *slog.Logger
}

// NewA2ADashboard creates an A2ADashboard with the given dependencies.
func NewA2ADashboard(
	reg *registry.Store,
	gov *governance.Engine,
	ts *store.SQLiteTaskStore,
	adminToken string,
	logger *slog.Logger,
) *A2ADashboard {
	return &A2ADashboard{
		registry:   reg,
		governance: gov,
		taskStore:  ts,
		adminToken: adminToken,
		logger:     logger,
	}
}

// Handler returns an http.Handler that serves all /api/a2a/* routes.
func (d *A2ADashboard) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/a2a/agents", d.withAuth(d.handleAgents))
	mux.HandleFunc("/api/a2a/grants", d.withAuth(d.handleGrants))
	mux.HandleFunc("/api/a2a/grants/elevated", d.withAuth(d.handleGrantsElevated))
	mux.HandleFunc("/api/a2a/approvals", d.withAuth(d.handleApprovalsList))
	// Go 1.22+ ServeMux supports path patterns with wildcards, but to be
	// safe with older Go, we match the prefix and extract {id} manually.
	mux.HandleFunc("/api/a2a/approvals/", d.withAuth(d.handleApprovalsDecide))
	mux.HandleFunc("/api/a2a/audit", d.withAuth(d.handleAudit))
	mux.HandleFunc("/api/a2a/openclaw/status", d.withAuth(d.handleOpenClawStatus))

	return mux
}

// withAuth wraps a handler with admin token authentication.
// Uses subtle.ConstantTimeCompare to prevent timing attacks.
func (d *A2ADashboard) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if auth == token {
			writeA2AError(w, http.StatusUnauthorized, "unauthorized", "missing or malformed Authorization header")
			return
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(d.adminToken)) != 1 {
			writeA2AError(w, http.StatusUnauthorized, "unauthorized", "invalid admin token")
			return
		}

		next(w, r)
	}
}

// handleAgents dispatches GET/POST/DELETE for /api/a2a/agents.
func (d *A2ADashboard) handleAgents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.handleAgentsList(w, r)
	case http.MethodPost:
		d.handleAgentsCreate(w, r)
	case http.MethodDelete:
		d.handleAgentsDelete(w, r)
	default:
		writeA2AError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET, POST, or DELETE")
	}
}

func (d *A2ADashboard) handleAgentsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agents, err := d.registry.List(ctx, registry.ListFilter{})
	if err != nil {
		d.logger.Error("a2a: list agents", "error", err)
		writeA2AError(w, http.StatusInternalServerError, "internal", "failed to list agents")
		return
	}

	type agentResp struct {
		AgentID     string  `json:"agent_id"`
		Name        string  `json:"name"`
		DisplayName string  `json:"display_name"`
		Status      string  `json:"status"`
		Transport   string  `json:"transport"`
		LastSeenAt  *string `json:"last_seen_at,omitempty"`
		LastError   string  `json:"last_error,omitempty"`
	}

	out := make([]agentResp, 0, len(agents))
	for _, ag := range agents {
		resp := agentResp{
			AgentID:     ag.AgentID,
			Name:        ag.Name,
			DisplayName: ag.DisplayName,
			Status:      ag.Status,
			Transport:   ag.TransportConfig.Kind,
			LastError:   ag.LastError,
		}
		if ag.LastSeenAt != nil {
			s := ag.LastSeenAt.UTC().Format(time.RFC3339)
			resp.LastSeenAt = &s
		}
		out = append(out, resp)
	}

	writeA2AJSON(w, http.StatusOK, map[string]interface{}{"agents": out})
}

func (d *A2ADashboard) handleAgentsCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		URL         string `json:"url"`
		Transport   string `json:"transport"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Name == "" {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "name is required")
		return
	}
	if req.Transport == "" {
		req.Transport = "http"
	}

	agentID := a2a.NewTaskID() // reuse ULID generator; prefix doesn't matter for registry IDs
	// Use a simple agent ID format.
	agentID = "agent_" + agentID[4:] // strip tsk_ prefix, add agent_

	agent := registry.RegisteredAgent{
		AgentID:     agentID,
		Name:        req.Name,
		DisplayName: req.DisplayName,
		AgentCard: a2a.AgentCard{
			Name:            req.Name,
			URL:             req.URL,
			ProtocolVersion: "0.1.0",
			Endpoints: []a2a.Endpoint{
				{URL: req.URL, Transport: a2a.TransportKind(req.Transport)},
			},
		},
		TransportConfig: makeTransportConfig(req.Transport, req.URL),
		Status:          registry.StatusActive,
	}

	ctx := r.Context()
	if err := d.registry.Register(ctx, agent); err != nil {
		d.logger.Error("a2a: register agent", "error", err)
		writeA2AError(w, http.StatusInternalServerError, "internal", "failed to register agent")
		return
	}

	writeA2AJSON(w, http.StatusCreated, map[string]interface{}{
		"agent_id": agentID,
		"name":     req.Name,
		"status":   registry.StatusActive,
	})
}

func (d *A2ADashboard) handleAgentsDelete(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("id")
	if agentID == "" {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "id query parameter is required")
		return
	}

	ctx := r.Context()
	if err := d.registry.Delete(ctx, agentID); err != nil {
		d.logger.Error("a2a: delete agent", "error", err)
		writeA2AError(w, http.StatusNotFound, "not_found", "agent not found")
		return
	}

	writeA2AJSON(w, http.StatusOK, map[string]interface{}{"deleted": agentID})
}

// handleGrants dispatches GET/POST/DELETE for /api/a2a/grants.
func (d *A2ADashboard) handleGrants(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		d.handleGrantsList(w, r)
	case http.MethodPost:
		d.handleGrantsCreate(w, r)
	case http.MethodDelete:
		d.handleGrantsRevoke(w, r)
	default:
		writeA2AError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET, POST, or DELETE")
	}
}

func (d *A2ADashboard) handleGrantsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	grants, err := d.governance.ListGrants(ctx)
	if err != nil {
		d.logger.Error("a2a: list grants", "error", err)
		writeA2AError(w, http.StatusInternalServerError, "internal", "failed to list grants")
		return
	}

	// Optional filtering by source/target query params.
	source := r.URL.Query().Get("source")
	target := r.URL.Query().Get("target")

	filtered := make([]server.Grant, 0, len(grants))
	for _, g := range grants {
		if source != "" && g.SourceAgentID != source {
			continue
		}
		if target != "" && g.TargetAgentID != target {
			continue
		}
		filtered = append(filtered, g)
	}

	writeA2AJSON(w, http.StatusOK, map[string]interface{}{"grants": filtered})
}

func (d *A2ADashboard) handleGrantsCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceAgentID        string   `json:"source_agent_id"`
		TargetAgentID        string   `json:"target_agent_id"`
		Skill                string   `json:"skill,omitempty"`
		RequiredCapabilities []string `json:"required_capabilities,omitempty"`
		Decision             string   `json:"decision"`
		Reason               string   `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.SourceAgentID == "" || req.TargetAgentID == "" {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "source_agent_id and target_agent_id are required")
		return
	}
	if req.Decision == "" {
		req.Decision = "allow"
	}

	sg := server.Grant{
		GrantID:              a2a.NewGrantID(),
		SourceAgentID:        req.SourceAgentID,
		TargetAgentID:        req.TargetAgentID,
		Skill:                req.Skill,
		RequiredCapabilities: req.RequiredCapabilities,
		Decision:             req.Decision,
		Reason:               req.Reason,
	}

	ctx := r.Context()
	created, err := d.governance.CreateGrant(ctx, sg)
	if err != nil {
		d.logger.Error("a2a: create grant", "error", err)
		writeA2AError(w, http.StatusInternalServerError, "internal", "failed to create grant")
		return
	}

	writeA2AJSON(w, http.StatusCreated, created)
}

func (d *A2ADashboard) handleGrantsRevoke(w http.ResponseWriter, r *http.Request) {
	grantID := r.URL.Query().Get("id")
	if grantID == "" {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "id query parameter is required")
		return
	}

	ctx := r.Context()
	if err := d.governance.RevokeGrant(ctx, grantID); err != nil {
		d.logger.Error("a2a: revoke grant", "error", err)
		writeA2AError(w, http.StatusNotFound, "not_found", "grant not found or already revoked")
		return
	}

	writeA2AJSON(w, http.StatusOK, map[string]interface{}{"revoked": grantID})
}

// handleGrantsElevated creates an ALL grant with extra authentication.
func (d *A2ADashboard) handleGrantsElevated(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeA2AError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}

	// Validate X-Nexus-Reauth-Token (must match admin token).
	reauthToken := r.Header.Get("X-Nexus-Reauth-Token")
	if subtle.ConstantTimeCompare([]byte(reauthToken), []byte(d.adminToken)) != 1 {
		writeA2AError(w, http.StatusForbidden, "reauth_required", "X-Nexus-Reauth-Token is missing or invalid")
		return
	}

	// Validate X-Nexus-Consent-Ticket (must be non-empty).
	consentTicket := r.Header.Get("X-Nexus-Consent-Ticket")
	if consentTicket == "" {
		writeA2AError(w, http.StatusForbidden, "consent_required", "X-Nexus-Consent-Ticket is required")
		return
	}

	var req struct {
		SourceAgentID        string   `json:"source_agent_id"`
		TargetAgentID        string   `json:"target_agent_id"`
		RequiredCapabilities []string `json:"required_capabilities,omitempty"`
		Decision             string   `json:"decision"`
		Reason               string   `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}

	// Elevated endpoint requires capability * (ALL grant).
	hasAll := false
	for _, cap := range req.RequiredCapabilities {
		if cap == "*" {
			hasAll = true
			break
		}
	}
	if !hasAll {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "elevated grants must include capability '*'")
		return
	}

	if req.Decision == "" {
		req.Decision = "allow"
	}

	sg := server.Grant{
		GrantID:              a2a.NewGrantID(),
		SourceAgentID:        req.SourceAgentID,
		TargetAgentID:        req.TargetAgentID,
		RequiredCapabilities: req.RequiredCapabilities,
		Decision:             req.Decision,
		Reason:               req.Reason,
	}

	ctx := r.Context()
	created, err := d.governance.CreateGrant(ctx, sg)
	if err != nil {
		d.logger.Error("a2a: create elevated grant", "error", err)
		writeA2AError(w, http.StatusInternalServerError, "internal", "failed to create elevated grant")
		return
	}

	writeA2AJSON(w, http.StatusCreated, created)
}

// handleApprovalsList lists pending approvals.
func (d *A2ADashboard) handleApprovalsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeA2AError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}

	ctx := r.Context()
	approvals, err := d.governance.ListApprovals(ctx)
	if err != nil {
		d.logger.Error("a2a: list approvals", "error", err)
		writeA2AError(w, http.StatusInternalServerError, "internal", "failed to list approvals")
		return
	}

	writeA2AJSON(w, http.StatusOK, map[string]interface{}{"approvals": approvals})
}

// handleApprovalsDecide handles POST /api/a2a/approvals/{id}/decide.
func (d *A2ADashboard) handleApprovalsDecide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeA2AError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}

	// Extract approval ID from path: /api/a2a/approvals/{id}/decide
	path := strings.TrimPrefix(r.URL.Path, "/api/a2a/approvals/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "decide" || parts[0] == "" {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "expected path /api/a2a/approvals/{id}/decide")
		return
	}
	approvalID := parts[0]

	var req struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if req.Decision != "approved" && req.Decision != "denied" {
		writeA2AError(w, http.StatusBadRequest, "bad_request", "decision must be 'approved' or 'denied'")
		return
	}

	ctx := r.Context()
	if err := d.governance.DecideApproval(ctx, approvalID, req.Decision, req.Reason); err != nil {
		d.logger.Error("a2a: decide approval", "error", err)
		writeA2AError(w, http.StatusNotFound, "not_found", "approval not found or already resolved")
		return
	}

	writeA2AJSON(w, http.StatusOK, map[string]interface{}{
		"approval_id": approvalID,
		"decision":    req.Decision,
	})
}

// handleAudit returns governance audit events.
func (d *A2ADashboard) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeA2AError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}

	// For now, return the grants + approvals as the audit trail.
	// The governance engine's ListGrants + ListApprovals serve as the audit log.
	ctx := r.Context()

	grants, err := d.governance.ListGrants(ctx)
	if err != nil {
		d.logger.Error("a2a: audit grants", "error", err)
		writeA2AError(w, http.StatusInternalServerError, "internal", "failed to query audit")
		return
	}

	approvals, err := d.governance.ListApprovals(ctx)
	if err != nil {
		d.logger.Error("a2a: audit approvals", "error", err)
		writeA2AError(w, http.StatusInternalServerError, "internal", "failed to query audit")
		return
	}

	// Build audit events from grants and approvals.
	type auditEvent struct {
		EventType string      `json:"event_type"`
		Timestamp string      `json:"timestamp"`
		Data      interface{} `json:"data"`
	}

	events := make([]auditEvent, 0, len(grants)+len(approvals))
	for _, g := range grants {
		events = append(events, auditEvent{
			EventType: "grant",
			Timestamp: g.CreatedAt,
			Data:      g,
		})
	}
	for _, ap := range approvals {
		events = append(events, auditEvent{
			EventType: "approval",
			Timestamp: ap.CreatedAt,
			Data:      ap,
		})
	}

	writeA2AJSON(w, http.StatusOK, map[string]interface{}{"events": events})
}

// handleOpenClawStatus returns enriched status for the OpenClaw agent.
func (d *A2ADashboard) handleOpenClawStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeA2AError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
		return
	}

	ctx := r.Context()

	// Look up OpenClaw agent by name.
	agent, err := d.registry.GetByName(ctx, "openclaw")
	if err != nil {
		// Try alternative names.
		agent, err = d.registry.GetByName(ctx, "OpenClaw")
	}

	status := map[string]interface{}{
		"connected": false,
		"agent":     nil,
		"skills":    []interface{}{},
		"grants":    []interface{}{},
	}

	if err == nil && agent != nil {
		status["connected"] = agent.Status == registry.StatusActive
		status["agent"] = map[string]interface{}{
			"agent_id":     agent.AgentID,
			"name":         agent.Name,
			"display_name": agent.DisplayName,
			"status":       agent.Status,
			"transport":    agent.TransportConfig.Kind,
			"version":      agent.AgentCard.Version,
			"last_error":   agent.LastError,
		}
		if agent.LastSeenAt != nil {
			status["last_heartbeat"] = agent.LastSeenAt.UTC().Format(time.RFC3339)
		}

		// Skills from agent card.
		skills := make([]map[string]interface{}, 0, len(agent.AgentCard.Skills))
		for _, sk := range agent.AgentCard.Skills {
			skills = append(skills, map[string]interface{}{
				"id":                    sk.ID,
				"name":                  sk.Name,
				"description":           sk.Description,
				"required_capabilities": sk.RequiredCapabilities,
				"destructive":           sk.Destructive,
			})
		}
		status["skills"] = skills

		// Grants for this agent.
		grants, gErr := d.governance.ListGrants(ctx)
		if gErr == nil {
			agentGrants := make([]server.Grant, 0)
			for _, g := range grants {
				if g.SourceAgentID == agent.AgentID || g.TargetAgentID == agent.AgentID {
					agentGrants = append(agentGrants, g)
				}
			}
			status["grants"] = agentGrants
		}
	}

	writeA2AJSON(w, http.StatusOK, status)
}

// makeTransportConfig builds a transport.TransportConfig from kind and URL.
func makeTransportConfig(kind, url string) transport.TransportConfig {
	return transport.TransportConfig{Kind: kind, URL: url}
}

// writeA2AJSON writes a JSON response with the given status code.
func writeA2AJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeA2AError writes a standard JSON error response.
func writeA2AError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error":   code,
		"message": message,
	})
}

