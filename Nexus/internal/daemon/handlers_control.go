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

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/bubblefish-tech/nexus/internal/actions"
	"github.com/bubblefish-tech/nexus/internal/approvals"
	"github.com/bubblefish-tech/nexus/internal/audit"
	"github.com/bubblefish-tech/nexus/internal/grants"
	"github.com/bubblefish-tech/nexus/internal/tasks"
)

// ---------------------------------------------------------------------------
// Request / response DTOs
// ---------------------------------------------------------------------------

// grantCreateRequest is the POST /api/control/grants body.
type grantCreateRequest struct {
	AgentID      string          `json:"agent_id"`
	Capability   string          `json:"capability"`
	Scope        json.RawMessage `json:"scope,omitempty"`
	GrantedBy    string          `json:"granted_by,omitempty"`
	ExpiresAtMs  *int64          `json:"expires_at_ms,omitempty"`
}

// grantResponse serializes a grants.Grant.
type grantResponse struct {
	GrantID      string          `json:"grant_id"`
	AgentID      string          `json:"agent_id"`
	Capability   string          `json:"capability"`
	Scope        json.RawMessage `json:"scope"`
	GrantedBy    string          `json:"granted_by"`
	GrantedAtMs  int64           `json:"granted_at_ms"`
	ExpiresAtMs  *int64          `json:"expires_at_ms,omitempty"`
	RevokedAtMs  *int64          `json:"revoked_at_ms,omitempty"`
	RevokeReason string          `json:"revoke_reason,omitempty"`
}

func grantToResponse(g grants.Grant) grantResponse {
	resp := grantResponse{
		GrantID:      g.GrantID,
		AgentID:      g.AgentID,
		Capability:   g.Capability,
		Scope:        g.Scope,
		GrantedBy:    g.GrantedBy,
		GrantedAtMs:  g.GrantedAt.UnixMilli(),
		RevokeReason: g.RevokeReason,
	}
	if g.ExpiresAt != nil {
		v := g.ExpiresAt.UnixMilli()
		resp.ExpiresAtMs = &v
	}
	if g.RevokedAt != nil {
		v := g.RevokedAt.UnixMilli()
		resp.RevokedAtMs = &v
	}
	if len(resp.Scope) == 0 {
		resp.Scope = json.RawMessage("{}")
	}
	return resp
}

// grantRevokeRequest is the DELETE /api/control/grants/{id} body (optional
// payload carrying a reason string).
type grantRevokeRequest struct {
	Reason string `json:"reason,omitempty"`
}

// approvalCreateRequest is the POST /api/control/approvals body.
type approvalCreateRequest struct {
	AgentID    string          `json:"agent_id"`
	Capability string          `json:"capability"`
	Action     json.RawMessage `json:"action"`
}

// approvalResponse serializes an approvals.Request.
type approvalResponse struct {
	RequestID     string          `json:"request_id"`
	AgentID       string          `json:"agent_id"`
	Capability    string          `json:"capability"`
	Action        json.RawMessage `json:"action"`
	Status        string          `json:"status"`
	RequestedAtMs int64           `json:"requested_at_ms"`
	DecidedAtMs   *int64          `json:"decided_at_ms,omitempty"`
	DecidedBy     string          `json:"decided_by,omitempty"`
	Decision      string          `json:"decision,omitempty"`
	Reason        string          `json:"reason,omitempty"`
}

func approvalToResponse(r approvals.Request) approvalResponse {
	resp := approvalResponse{
		RequestID:     r.RequestID,
		AgentID:       r.AgentID,
		Capability:    r.Capability,
		Action:        r.Action,
		Status:        r.Status,
		RequestedAtMs: r.RequestedAt.UnixMilli(),
		DecidedBy:     r.DecidedBy,
		Decision:      r.Decision,
		Reason:        r.Reason,
	}
	if r.DecidedAt != nil {
		v := r.DecidedAt.UnixMilli()
		resp.DecidedAtMs = &v
	}
	return resp
}

// approvalDecideRequest is the POST /api/control/approvals/{id} body.
type approvalDecideRequest struct {
	Decision string `json:"decision"` // "approve" or "deny"
	Reason   string `json:"reason,omitempty"`
}

// taskCreateRequest is the POST /api/control/tasks body.
type taskCreateRequest struct {
	AgentID      string          `json:"agent_id"`
	ParentTaskID string          `json:"parent_task_id,omitempty"`
	Capability   string          `json:"capability,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
}

// taskUpdateRequest is the PATCH /api/control/tasks/{id} body.
type taskUpdateRequest struct {
	State  string          `json:"state"`
	Output json.RawMessage `json:"output,omitempty"`
}

// taskResponse serializes a tasks.Task, optionally with an event log.
type taskResponse struct {
	TaskID         string           `json:"task_id"`
	AgentID        string           `json:"agent_id"`
	ParentTaskID   string           `json:"parent_task_id,omitempty"`
	State          string           `json:"state"`
	Capability     string           `json:"capability,omitempty"`
	Input          json.RawMessage  `json:"input,omitempty"`
	Output         json.RawMessage  `json:"output,omitempty"`
	CreatedAtMs    int64            `json:"created_at_ms"`
	UpdatedAtMs    int64            `json:"updated_at_ms"`
	CompletedAtMs  *int64           `json:"completed_at_ms,omitempty"`
	Events         []taskEventJSON  `json:"events,omitempty"`
}

type taskEventJSON struct {
	EventID     string          `json:"event_id"`
	TaskID      string          `json:"task_id"`
	EventType   string          `json:"event_type"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	CreatedAtMs int64           `json:"created_at_ms"`
}

func taskToResponse(t tasks.Task, events []tasks.TaskEvent) taskResponse {
	resp := taskResponse{
		TaskID:       t.TaskID,
		AgentID:      t.AgentID,
		ParentTaskID: t.ParentTaskID,
		State:        t.State,
		Capability:   t.Capability,
		Input:        t.Input,
		Output:       t.Output,
		CreatedAtMs:  t.CreatedAt.UnixMilli(),
		UpdatedAtMs:  t.UpdatedAt.UnixMilli(),
	}
	if t.CompletedAt != nil {
		v := t.CompletedAt.UnixMilli()
		resp.CompletedAtMs = &v
	}
	if len(events) > 0 {
		resp.Events = make([]taskEventJSON, len(events))
		for i, e := range events {
			resp.Events[i] = taskEventJSON{
				EventID:     e.EventID,
				TaskID:      e.TaskID,
				EventType:   e.EventType,
				Payload:     e.Payload,
				CreatedAtMs: e.CreatedAt.UnixMilli(),
			}
		}
	}
	return resp
}

// actionResponse serializes an actions.Action.
type actionResponse struct {
	ActionID       string `json:"action_id"`
	AgentID        string `json:"agent_id"`
	Capability     string `json:"capability"`
	Target         string `json:"target,omitempty"`
	GrantID        string `json:"grant_id,omitempty"`
	ApprovalID     string `json:"approval_id,omitempty"`
	PolicyDecision string `json:"policy_decision"`
	PolicyReason   string `json:"policy_reason,omitempty"`
	ExecutedAtMs   int64  `json:"executed_at_ms"`
	Result         string `json:"result,omitempty"`
	AuditHash      string `json:"audit_hash,omitempty"`
}

func actionToResponse(a actions.Action) actionResponse {
	return actionResponse{
		ActionID:       a.ActionID,
		AgentID:        a.AgentID,
		Capability:     a.Capability,
		Target:         a.Target,
		GrantID:        a.GrantID,
		ApprovalID:     a.ApprovalID,
		PolicyDecision: a.PolicyDecision,
		PolicyReason:   a.PolicyReason,
		ExecutedAtMs:   a.ExecutedAt.UnixMilli(),
		Result:         a.Result,
		AuditHash:      a.AuditHash,
	}
}

// ---------------------------------------------------------------------------
// Grants handlers
// ---------------------------------------------------------------------------

// handleControlGrantCreate serves POST /api/control/grants.
func (d *Daemon) handleControlGrantCreate(w http.ResponseWriter, r *http.Request) {
	if d.grantStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	var req grantCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_json", err.Error(), 0)
		return
	}
	g := grants.Grant{
		AgentID:    req.AgentID,
		Capability: req.Capability,
		Scope:      req.Scope,
		GrantedBy:  req.GrantedBy,
	}
	if g.GrantedBy == "" {
		g.GrantedBy = "admin"
	}
	if req.ExpiresAtMs != nil {
		t := time.UnixMilli(*req.ExpiresAtMs)
		g.ExpiresAt = &t
	}
	created, err := d.grantStore.Create(r.Context(), g)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "create_failed", err.Error(), 0)
		return
	}
	d.emitControlAudit(r, "grant.created", created.AgentID, created.Capability, "allowed", created.GrantID, "")
	d.writeJSON(w, http.StatusCreated, grantToResponse(created))
}

// handleControlGrantList serves GET /api/control/grants.
func (d *Daemon) handleControlGrantList(w http.ResponseWriter, r *http.Request) {
	if d.grantStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	filter := grants.ListFilter{
		AgentID:    r.URL.Query().Get("agent_id"),
		Capability: r.URL.Query().Get("capability"),
		OnlyActive: r.URL.Query().Get("only_active") == "true",
		Limit:      parseListLimit(r, 1000),
	}
	list, err := d.grantStore.List(r.Context(), filter)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "list_failed", err.Error(), 0)
		return
	}
	resp := make([]grantResponse, len(list))
	for i, g := range list {
		resp[i] = grantToResponse(g)
	}
	d.writeJSON(w, http.StatusOK, map[string]any{"grants": resp})
}

// handleControlGrantRevoke serves DELETE /api/control/grants/{id}.
func (d *Daemon) handleControlGrantRevoke(w http.ResponseWriter, r *http.Request) {
	if d.grantStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_id", "grant id required", 0)
		return
	}
	var req grantRevokeRequest
	// Body is optional.
	if r.ContentLength > 0 {
		_ = decodeJSON(r, &req)
	}
	err := d.grantStore.Revoke(r.Context(), id, req.Reason)
	if errors.Is(err, grants.ErrNotFound) {
		d.writeErrorResponse(w, r, http.StatusNotFound, "not_found", "grant not found", 0)
		return
	}
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "revoke_failed", err.Error(), 0)
		return
	}
	g, err := d.grantStore.Get(r.Context(), id)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "fetch_failed", err.Error(), 0)
		return
	}
	d.emitControlAudit(r, "grant.revoked", g.AgentID, g.Capability, "allowed", g.GrantID, req.Reason)
	d.writeJSON(w, http.StatusOK, grantToResponse(*g))
}

// ---------------------------------------------------------------------------
// Approvals handlers
// ---------------------------------------------------------------------------

// handleControlApprovalCreate serves POST /api/control/approvals.
func (d *Daemon) handleControlApprovalCreate(w http.ResponseWriter, r *http.Request) {
	if d.approvalStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	var req approvalCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_json", err.Error(), 0)
		return
	}
	created, err := d.approvalStore.Create(r.Context(), approvals.Request{
		AgentID:    req.AgentID,
		Capability: req.Capability,
		Action:     req.Action,
	})
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "create_failed", err.Error(), 0)
		return
	}
	d.emitControlAudit(r, "approval.requested", created.AgentID, created.Capability, "pending", "", created.RequestID)
	d.writeJSON(w, http.StatusCreated, approvalToResponse(created))
}

// handleControlApprovalList serves GET /api/control/approvals.
func (d *Daemon) handleControlApprovalList(w http.ResponseWriter, r *http.Request) {
	if d.approvalStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	// Default to pending-only (matches plan: "list pending approvals"); an
	// explicit ?status= override returns that status.
	status := r.URL.Query().Get("status")
	if status == "" {
		status = approvals.StatusPending
	}
	filter := approvals.ListFilter{
		AgentID:    r.URL.Query().Get("agent_id"),
		Status:     status,
		Capability: r.URL.Query().Get("capability"),
		Limit:      parseListLimit(r, 1000),
	}
	list, err := d.approvalStore.List(r.Context(), filter)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "list_failed", err.Error(), 0)
		return
	}
	resp := make([]approvalResponse, len(list))
	for i, a := range list {
		resp[i] = approvalToResponse(a)
	}
	d.writeJSON(w, http.StatusOK, map[string]any{"approvals": resp})
}

// handleControlApprovalDecide serves POST /api/control/approvals/{id}.
func (d *Daemon) handleControlApprovalDecide(w http.ResponseWriter, r *http.Request) {
	if d.approvalStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_id", "approval id required", 0)
		return
	}
	var req approvalDecideRequest
	if err := decodeJSON(r, &req); err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_json", err.Error(), 0)
		return
	}
	decidedBy := req.Reason // optional display — we still require an admin actor
	_ = decidedBy
	err := d.approvalStore.Decide(r.Context(), id, approvals.DecideInput{
		Decision:  req.Decision,
		DecidedBy: "admin",
		Reason:    req.Reason,
	})
	if errors.Is(err, approvals.ErrNotFound) {
		d.writeErrorResponse(w, r, http.StatusNotFound, "not_found", "approval not found", 0)
		return
	}
	if errors.Is(err, approvals.ErrAlreadyDecided) {
		d.writeErrorResponse(w, r, http.StatusConflict, "already_decided", "approval already decided", 0)
		return
	}
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "decide_failed", err.Error(), 0)
		return
	}
	got, err := d.approvalStore.Get(r.Context(), id)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "fetch_failed", err.Error(), 0)
		return
	}
	policyDecision := "allowed"
	if req.Decision == approvals.DecisionDeny {
		policyDecision = "denied"
	}
	d.emitControlAudit(r, "approval.decided", got.AgentID, got.Capability, policyDecision, "", got.RequestID)
	d.writeJSON(w, http.StatusOK, approvalToResponse(*got))
}

// ---------------------------------------------------------------------------
// Tasks handlers
// ---------------------------------------------------------------------------

// handleControlTaskCreate serves POST /api/control/tasks.
func (d *Daemon) handleControlTaskCreate(w http.ResponseWriter, r *http.Request) {
	if d.taskStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	var req taskCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_json", err.Error(), 0)
		return
	}
	created, err := d.taskStore.Create(r.Context(), tasks.Task{
		AgentID:      req.AgentID,
		ParentTaskID: req.ParentTaskID,
		Capability:   req.Capability,
		Input:        req.Input,
	})
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "create_failed", err.Error(), 0)
		return
	}
	d.emitControlAudit(r, "task.created", created.AgentID, created.Capability, "allowed", "", created.TaskID)
	d.writeJSON(w, http.StatusCreated, taskToResponse(created, nil))
}

// handleControlTaskGet serves GET /api/control/tasks/{id}.
func (d *Daemon) handleControlTaskGet(w http.ResponseWriter, r *http.Request) {
	if d.taskStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_id", "task id required", 0)
		return
	}
	t, err := d.taskStore.Get(r.Context(), id)
	if errors.Is(err, tasks.ErrNotFound) {
		d.writeErrorResponse(w, r, http.StatusNotFound, "not_found", "task not found", 0)
		return
	}
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "fetch_failed", err.Error(), 0)
		return
	}
	events, err := d.taskStore.ListEvents(r.Context(), id)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "events_failed", err.Error(), 0)
		return
	}
	d.writeJSON(w, http.StatusOK, taskToResponse(*t, events))
}

// handleControlTaskList serves GET /api/control/tasks.
func (d *Daemon) handleControlTaskList(w http.ResponseWriter, r *http.Request) {
	if d.taskStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	filter := tasks.ListFilter{
		AgentID:      r.URL.Query().Get("agent_id"),
		State:        r.URL.Query().Get("state"),
		ParentTaskID: r.URL.Query().Get("parent_task_id"),
		TopLevelOnly: r.URL.Query().Get("top_level") == "true",
		Limit:        parseListLimit(r, 1000),
	}
	list, err := d.taskStore.List(r.Context(), filter)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "list_failed", err.Error(), 0)
		return
	}
	resp := make([]taskResponse, len(list))
	for i, t := range list {
		resp[i] = taskToResponse(t, nil)
	}
	d.writeJSON(w, http.StatusOK, map[string]any{"tasks": resp})
}

// handleControlTaskUpdate serves PATCH /api/control/tasks/{id}.
func (d *Daemon) handleControlTaskUpdate(w http.ResponseWriter, r *http.Request) {
	if d.taskStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_id", "task id required", 0)
		return
	}
	var req taskUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_json", err.Error(), 0)
		return
	}
	updated, err := d.taskStore.Update(r.Context(), id, tasks.UpdateInput{
		State:  req.State,
		Output: req.Output,
	})
	if errors.Is(err, tasks.ErrNotFound) {
		d.writeErrorResponse(w, r, http.StatusNotFound, "not_found", "task not found", 0)
		return
	}
	if errors.Is(err, tasks.ErrTerminalState) {
		d.writeErrorResponse(w, r, http.StatusConflict, "terminal_state", "task is in a terminal state", 0)
		return
	}
	if errors.Is(err, tasks.ErrInvalidState) {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_state", err.Error(), 0)
		return
	}
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "update_failed", err.Error(), 0)
		return
	}
	d.emitControlAudit(r, "task.updated", updated.AgentID, updated.Capability, "allowed", "", updated.TaskID)
	d.writeJSON(w, http.StatusOK, taskToResponse(*updated, nil))
}

// ---------------------------------------------------------------------------
// Actions handler
// ---------------------------------------------------------------------------

// handleControlActionQuery serves GET /api/control/actions.
func (d *Daemon) handleControlActionQuery(w http.ResponseWriter, r *http.Request) {
	if d.actionStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	q := r.URL.Query()
	filter := actions.QueryFilter{
		AgentID:        q.Get("agent_id"),
		Capability:     q.Get("capability"),
		PolicyDecision: q.Get("policy_decision"),
	}
	if sinceMs := q.Get("since_ms"); sinceMs != "" {
		v, err := strconv.ParseInt(sinceMs, 10, 64)
		if err != nil {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_since_ms", err.Error(), 0)
			return
		}
		filter.Since = time.UnixMilli(v)
	}
	if untilMs := q.Get("until_ms"); untilMs != "" {
		v, err := strconv.ParseInt(untilMs, 10, 64)
		if err != nil {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_until_ms", err.Error(), 0)
			return
		}
		filter.Until = time.UnixMilli(v)
	}
	if limitStr := q.Get("limit"); limitStr != "" {
		v, err := strconv.Atoi(limitStr)
		if err != nil || v < 0 {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_limit", "limit must be a non-negative integer", 0)
			return
		}
		filter.Limit = v
	}
	list, err := d.actionStore.Query(r.Context(), filter)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "query_failed", err.Error(), 0)
		return
	}
	resp := make([]actionResponse, len(list))
	for i, a := range list {
		resp[i] = actionToResponse(a)
	}
	d.writeJSON(w, http.StatusOK, map[string]any{"actions": resp})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// decodeJSON reads r.Body and unmarshals into v. Caps the body at 1 MiB to
// defend against accidental large payloads.
func decodeJSON(r *http.Request, v any) error {
	const maxBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxBody {
		return fmt.Errorf("body exceeds %d bytes", maxBody)
	}
	if len(body) == 0 {
		return fmt.Errorf("empty body")
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("parse json: %w", err)
	}
	return nil
}

// parseListLimit returns the ?limit= query param as an int, falling back to
// defaultVal when the param is absent. A value of 0 means "no cap". Negative
// or non-integer values are treated as the default.
func parseListLimit(r *http.Request, defaultVal int) int {
	const maxLimit = 1000
	s := r.URL.Query().Get("limit")
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return defaultVal
	}
	if v > maxLimit {
		return maxLimit
	}
	return v
}

// emitControlAudit emits an InteractionRecord for a control-plane mutation.
// Never returns an error — audit failure must not break the request path.
// Safe to call when auditLogger is nil.
func (d *Daemon) emitControlAudit(r *http.Request, op, agentID, capability, decision, grantID, refID string) {
	if d.auditLogger == nil && d.auditWAL == nil {
		return
	}
	rec := audit.InteractionRecord{
		RecordID:       audit.NewRecordID(),
		RequestID:      middleware.GetReqID(r.Context()),
		Timestamp:      time.Now().UTC(),
		ActorType:      "admin",
		ActorID:        agentID,
		EffectiveIP:    effectiveClientIPFromContext(r.Context()),
		OperationType:  op,
		Endpoint:       r.URL.Path,
		HTTPMethod:     r.Method,
		HTTPStatusCode: http.StatusOK,
		Subject:        capability,
		PolicyDecision: decision,
		PolicyReason:   refID,
	}
	if grantID != "" {
		rec.IdempotencyKey = grantID
	}
	d.emitAuditRecord(rec)
}

// emitControlEvent writes a typed ControlEventRecord to the WAL audit chain.
// Never returns an error — audit failure must not break the request path.
func (d *Daemon) emitControlEvent(eventType, actor, targetID, targetType, agentID, capability, decision, reason string, entity interface{}) {
	if d.auditWAL == nil {
		return
	}
	var entityJSON json.RawMessage
	if entity != nil {
		if b, err := json.Marshal(entity); err == nil {
			entityJSON = b
		}
	}
	rec := audit.ControlEventRecord{
		RecordID:   audit.NewRecordID(),
		EventType:  eventType,
		Actor:      actor,
		ActorType:  "admin",
		TargetID:   targetID,
		TargetType: targetType,
		AgentID:    agentID,
		Capability: capability,
		EntityJSON: entityJSON,
		Decision:   decision,
		Reason:     reason,
		Timestamp:  time.Now().UTC(),
	}
	if err := d.auditWAL.SubmitControl(rec); err != nil {
		d.logger.Warn("control event audit write failed", "event_type", eventType, "error", err)
	}
}

// ---------------------------------------------------------------------------
// Lineage handler
// ---------------------------------------------------------------------------

// lineageResponse is the full provenance chain for a task.
type lineageResponse struct {
	TaskID      string              `json:"task_id"`
	Task        *taskResponse       `json:"task,omitempty"`
	Actions     []actionResponse    `json:"actions"`
	Grants      []grantResponse     `json:"grants"`
	Approvals   []approvalResponse  `json:"approvals"`
	AgentID     string              `json:"agent_id,omitempty"`
	AuditHashes []string            `json:"audit_hashes"`
}

// handleControlLineage serves GET /api/control/lineage/{id}.
// Returns the full provenance chain: task → actions → grants → approvals.
func (d *Daemon) handleControlLineage(w http.ResponseWriter, r *http.Request) {
	if d.taskStore == nil || d.actionStore == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "control_unavailable", "control plane not initialized", 0)
		return
	}
	taskID := chi.URLParam(r, "id")
	if taskID == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_id", "task id required", 0)
		return
	}

	task, err := d.taskStore.Get(r.Context(), taskID)
	if errors.Is(err, tasks.ErrNotFound) {
		d.writeErrorResponse(w, r, http.StatusNotFound, "not_found", "task not found", 0)
		return
	}
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "fetch_failed", err.Error(), 0)
		return
	}

	// Collect actions related to this task's agent + capability.
	actionList, err := d.actionStore.Query(r.Context(), actions.QueryFilter{
		AgentID:    task.AgentID,
		Capability: task.Capability,
		Limit:      100,
	})
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "actions_failed", err.Error(), 0)
		return
	}

	// Collect unique grants and approvals referenced by those actions.
	grantSeen := map[string]bool{}
	approvalSeen := map[string]bool{}
	var grantList []grantResponse
	var approvalList []approvalResponse
	var auditHashes []string

	for _, a := range actionList {
		if a.AuditHash != "" {
			auditHashes = append(auditHashes, a.AuditHash)
		}
		if a.GrantID != "" && !grantSeen[a.GrantID] && d.grantStore != nil {
			grantSeen[a.GrantID] = true
			if g, err := d.grantStore.Get(r.Context(), a.GrantID); err == nil {
				grantList = append(grantList, grantToResponse(*g))
			}
		}
		if a.ApprovalID != "" && !approvalSeen[a.ApprovalID] && d.approvalStore != nil {
			approvalSeen[a.ApprovalID] = true
			if ap, err := d.approvalStore.Get(r.Context(), a.ApprovalID); err == nil {
				approvalList = append(approvalList, approvalToResponse(*ap))
			}
		}
	}

	actionResponses := make([]actionResponse, len(actionList))
	for i, a := range actionList {
		actionResponses[i] = actionToResponse(a)
	}

	tr := taskToResponse(*task, nil)
	resp := lineageResponse{
		TaskID:      taskID,
		Task:        &tr,
		Actions:     actionResponses,
		Grants:      grantList,
		Approvals:   approvalList,
		AgentID:     task.AgentID,
		AuditHashes: auditHashes,
	}
	if resp.Actions == nil {
		resp.Actions = []actionResponse{}
	}
	if resp.Grants == nil {
		resp.Grants = []grantResponse{}
	}
	if resp.Approvals == nil {
		resp.Approvals = []approvalResponse{}
	}
	if resp.AuditHashes == nil {
		resp.AuditHashes = []string{}
	}
	d.writeJSON(w, http.StatusOK, resp)
}
