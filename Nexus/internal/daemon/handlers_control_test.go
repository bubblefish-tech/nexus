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
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/actions"
	"github.com/BubbleFish-Nexus/internal/approvals"
	"github.com/BubbleFish-Nexus/internal/grants"
	"github.com/BubbleFish-Nexus/internal/tasks"
	_ "modernc.org/sqlite"
)

// newControlTestDaemon builds a *Daemon with the four control-plane stores
// attached to an in-memory SQLite DB. No auth, no metrics, no audit logger —
// handler behavior is tested in isolation.
func newControlTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := registry.InitSchema(db); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return &Daemon{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		grantStore:    grants.NewStore(db),
		approvalStore: approvals.NewStore(db),
		taskStore:     tasks.NewStore(db),
		actionStore:   actions.NewStore(db),
	}
}

// routeThrough builds a minimal chi router wiring exactly the MT.2 routes
// with the daemon's handlers. Test requests go through the router so URL
// parameters (chi.URLParam) populate correctly.
func routeThrough(d *Daemon) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/control/grants", d.handleControlGrantCreate)
	r.Get("/api/control/grants", d.handleControlGrantList)
	r.Delete("/api/control/grants/{id}", d.handleControlGrantRevoke)
	r.Post("/api/control/approvals", d.handleControlApprovalCreate)
	r.Get("/api/control/approvals", d.handleControlApprovalList)
	r.Post("/api/control/approvals/{id}", d.handleControlApprovalDecide)
	r.Post("/api/control/tasks", d.handleControlTaskCreate)
	r.Get("/api/control/tasks/{id}", d.handleControlTaskGet)
	r.Get("/api/control/tasks", d.handleControlTaskList)
	r.Patch("/api/control/tasks/{id}", d.handleControlTaskUpdate)
	r.Get("/api/control/actions", d.handleControlActionQuery)
	return r
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("decode body: %v — body was %q", err, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Grants
// ---------------------------------------------------------------------------

func TestControl_CreateGrant_Success(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodPost, "/api/control/grants", map[string]any{
		"agent_id":   "agent-1",
		"capability": "nexus_write",
		"granted_by": "admin",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp grantResponse
	decodeBody(t, w, &resp)
	if !strings.HasPrefix(resp.GrantID, grants.IDPrefix) {
		t.Fatalf("GrantID = %q", resp.GrantID)
	}
	if resp.AgentID != "agent-1" || resp.Capability != "nexus_write" {
		t.Fatalf("unexpected body: %+v", resp)
	}
	if string(resp.Scope) != "{}" {
		t.Fatalf("Scope = %q, want {}", resp.Scope)
	}
}

func TestControl_CreateGrant_MissingAgent(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodPost, "/api/control/grants", map[string]any{
		"capability": "x",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestControl_CreateGrant_InvalidJSON(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	r := httptest.NewRequest(http.MethodPost, "/api/control/grants", strings.NewReader(`{not json`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestControl_CreateGrant_NoBody(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	r := httptest.NewRequest(http.MethodPost, "/api/control/grants", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestControl_CreateGrant_DefaultsGrantedBy(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodPost, "/api/control/grants", map[string]any{
		"agent_id":   "agent-1",
		"capability": "nexus_write",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", w.Code, w.Body.String())
	}
	var resp grantResponse
	decodeBody(t, w, &resp)
	if resp.GrantedBy != "admin" {
		t.Fatalf("GrantedBy = %q, want 'admin'", resp.GrantedBy)
	}
}

func TestControl_ListGrants_ReturnsAll(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	for i := 0; i < 3; i++ {
		_, err := d.grantStore.Create(context.Background(), grants.Grant{
			AgentID: "a", Capability: "c", GrantedBy: "admin",
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	w := doJSON(t, h, http.MethodGet, "/api/control/grants", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct{ Grants []grantResponse }
	decodeBody(t, w, &resp)
	if len(resp.Grants) != 3 {
		t.Fatalf("got %d grants, want 3", len(resp.Grants))
	}
}

func TestControl_ListGrants_FilterByAgent(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	for _, a := range []string{"a1", "a1", "a2"} {
		_, _ = d.grantStore.Create(context.Background(), grants.Grant{
			AgentID: a, Capability: "c", GrantedBy: "admin",
		})
	}
	w := doJSON(t, h, http.MethodGet, "/api/control/grants?agent_id=a1", nil)
	var resp struct{ Grants []grantResponse }
	decodeBody(t, w, &resp)
	if len(resp.Grants) != 2 {
		t.Fatalf("got %d, want 2", len(resp.Grants))
	}
}

func TestControl_ListGrants_OnlyActiveExcludesRevoked(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	g1, _ := d.grantStore.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "admin"})
	_, _ = d.grantStore.Create(ctx, grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "admin"})
	_ = d.grantStore.Revoke(ctx, g1.GrantID, "test")
	w := doJSON(t, h, http.MethodGet, "/api/control/grants?only_active=true", nil)
	var resp struct{ Grants []grantResponse }
	decodeBody(t, w, &resp)
	if len(resp.Grants) != 1 {
		t.Fatalf("got %d active, want 1", len(resp.Grants))
	}
}

func TestControl_RevokeGrant_Success(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	g, _ := d.grantStore.Create(context.Background(), grants.Grant{AgentID: "a", Capability: "c", GrantedBy: "admin"})
	w := doJSON(t, h, http.MethodDelete, "/api/control/grants/"+g.GrantID, map[string]any{"reason": "compromised"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp grantResponse
	decodeBody(t, w, &resp)
	if resp.RevokedAtMs == nil {
		t.Fatal("RevokedAtMs nil after revoke")
	}
	if resp.RevokeReason != "compromised" {
		t.Fatalf("RevokeReason = %q", resp.RevokeReason)
	}
}

func TestControl_RevokeGrant_NotFound(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodDelete, "/api/control/grants/gnt_ghost", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Approvals
// ---------------------------------------------------------------------------

func TestControl_CreateApproval_Success(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodPost, "/api/control/approvals", map[string]any{
		"agent_id":   "agent-1",
		"capability": "nexus_delete",
		"action":     map[string]any{"target": "mem-1", "op": "delete"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp approvalResponse
	decodeBody(t, w, &resp)
	if resp.Status != approvals.StatusPending {
		t.Fatalf("Status = %q, want pending", resp.Status)
	}
}

func TestControl_CreateApproval_MissingAction(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodPost, "/api/control/approvals", map[string]any{
		"agent_id":   "agent-1",
		"capability": "c",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestControl_ListApprovals_DefaultsToPending(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	r1, _ := d.approvalStore.Create(ctx, approvals.Request{
		AgentID: "a", Capability: "c", Action: json.RawMessage(`{"x":1}`),
	})
	_, _ = d.approvalStore.Create(ctx, approvals.Request{
		AgentID: "a", Capability: "c", Action: json.RawMessage(`{"x":2}`),
	})
	_ = d.approvalStore.Decide(ctx, r1.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionApprove, DecidedBy: "admin",
	})
	w := doJSON(t, h, http.MethodGet, "/api/control/approvals", nil)
	var resp struct{ Approvals []approvalResponse }
	decodeBody(t, w, &resp)
	if len(resp.Approvals) != 1 {
		t.Fatalf("got %d pending, want 1", len(resp.Approvals))
	}
}

func TestControl_ListApprovals_FilterByStatus(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	r, _ := d.approvalStore.Create(ctx, approvals.Request{
		AgentID: "a", Capability: "c", Action: json.RawMessage(`{}`),
	})
	_ = d.approvalStore.Decide(ctx, r.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionApprove, DecidedBy: "admin",
	})
	w := doJSON(t, h, http.MethodGet, "/api/control/approvals?status=approved", nil)
	var resp struct{ Approvals []approvalResponse }
	decodeBody(t, w, &resp)
	if len(resp.Approvals) != 1 {
		t.Fatalf("got %d approved, want 1", len(resp.Approvals))
	}
}

func TestControl_DecideApproval_Approve(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	r, _ := d.approvalStore.Create(ctx, approvals.Request{
		AgentID: "a", Capability: "c", Action: json.RawMessage(`{}`),
	})
	w := doJSON(t, h, http.MethodPost, "/api/control/approvals/"+r.RequestID, map[string]any{
		"decision": "approve",
		"reason":   "ok",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp approvalResponse
	decodeBody(t, w, &resp)
	if resp.Status != approvals.StatusApproved {
		t.Fatalf("Status = %q", resp.Status)
	}
}

func TestControl_DecideApproval_Deny(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	r, _ := d.approvalStore.Create(ctx, approvals.Request{
		AgentID: "a", Capability: "c", Action: json.RawMessage(`{}`),
	})
	w := doJSON(t, h, http.MethodPost, "/api/control/approvals/"+r.RequestID, map[string]any{
		"decision": "deny",
		"reason":   "unsafe",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp approvalResponse
	decodeBody(t, w, &resp)
	if resp.Status != approvals.StatusDenied {
		t.Fatalf("Status = %q", resp.Status)
	}
}

func TestControl_DecideApproval_NotFound(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodPost, "/api/control/approvals/apr_ghost", map[string]any{
		"decision": "approve",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestControl_DecideApproval_AlreadyDecided(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	r, _ := d.approvalStore.Create(ctx, approvals.Request{
		AgentID: "a", Capability: "c", Action: json.RawMessage(`{}`),
	})
	_ = d.approvalStore.Decide(ctx, r.RequestID, approvals.DecideInput{
		Decision: approvals.DecisionApprove, DecidedBy: "admin",
	})
	w := doJSON(t, h, http.MethodPost, "/api/control/approvals/"+r.RequestID, map[string]any{
		"decision": "deny",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

func TestControl_DecideApproval_InvalidDecision(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	r, _ := d.approvalStore.Create(ctx, approvals.Request{
		AgentID: "a", Capability: "c", Action: json.RawMessage(`{}`),
	})
	w := doJSON(t, h, http.MethodPost, "/api/control/approvals/"+r.RequestID, map[string]any{
		"decision": "maybe",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Tasks
// ---------------------------------------------------------------------------

func TestControl_CreateTask_Success(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodPost, "/api/control/tasks", map[string]any{
		"agent_id":   "agent-1",
		"capability": "nexus_write",
		"input":      map[string]any{"memory": "hello"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp taskResponse
	decodeBody(t, w, &resp)
	if resp.State != tasks.StateSubmitted {
		t.Fatalf("State = %q", resp.State)
	}
}

func TestControl_CreateTask_MissingAgent(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodPost, "/api/control/tasks", map[string]any{
		"capability": "c",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestControl_GetTask_Success(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	tk, _ := d.taskStore.Create(context.Background(), tasks.Task{AgentID: "a", Capability: "c"})
	_, _ = d.taskStore.AppendEvent(context.Background(), tasks.TaskEvent{
		TaskID: tk.TaskID, EventType: tasks.EventTypeCreated,
	})
	w := doJSON(t, h, http.MethodGet, "/api/control/tasks/"+tk.TaskID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp taskResponse
	decodeBody(t, w, &resp)
	if resp.TaskID != tk.TaskID {
		t.Fatalf("TaskID = %q", resp.TaskID)
	}
	if len(resp.Events) != 1 || resp.Events[0].EventType != tasks.EventTypeCreated {
		t.Fatalf("events = %+v", resp.Events)
	}
}

func TestControl_GetTask_NotFound(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodGet, "/api/control/tasks/tsk_ghost", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestControl_ListTasks_FilterByState(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	t1, _ := d.taskStore.Create(ctx, tasks.Task{AgentID: "a"})
	_, _ = d.taskStore.Create(ctx, tasks.Task{AgentID: "a"})
	_, _ = d.taskStore.Update(ctx, t1.TaskID, tasks.UpdateInput{State: tasks.StateWorking})
	w := doJSON(t, h, http.MethodGet, "/api/control/tasks?state=working", nil)
	var resp struct{ Tasks []taskResponse }
	decodeBody(t, w, &resp)
	if len(resp.Tasks) != 1 {
		t.Fatalf("got %d, want 1", len(resp.Tasks))
	}
}

func TestControl_ListTasks_FilterByAgent(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	for _, a := range []string{"a", "a", "b"} {
		_, _ = d.taskStore.Create(ctx, tasks.Task{AgentID: a})
	}
	w := doJSON(t, h, http.MethodGet, "/api/control/tasks?agent_id=a", nil)
	var resp struct{ Tasks []taskResponse }
	decodeBody(t, w, &resp)
	if len(resp.Tasks) != 2 {
		t.Fatalf("got %d, want 2", len(resp.Tasks))
	}
}

func TestControl_UpdateTask_SubmittedToWorking(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	tk, _ := d.taskStore.Create(context.Background(), tasks.Task{AgentID: "a"})
	w := doJSON(t, h, http.MethodPatch, "/api/control/tasks/"+tk.TaskID, map[string]any{
		"state": "working",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp taskResponse
	decodeBody(t, w, &resp)
	if resp.State != tasks.StateWorking {
		t.Fatalf("State = %q", resp.State)
	}
}

func TestControl_UpdateTask_ToCompletedWithOutput(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	tk, _ := d.taskStore.Create(context.Background(), tasks.Task{AgentID: "a"})
	w := doJSON(t, h, http.MethodPatch, "/api/control/tasks/"+tk.TaskID, map[string]any{
		"state":  "completed",
		"output": map[string]any{"payload_id": "abc"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	var resp taskResponse
	decodeBody(t, w, &resp)
	if resp.State != tasks.StateCompleted {
		t.Fatalf("State = %q", resp.State)
	}
	if resp.CompletedAtMs == nil {
		t.Fatal("CompletedAtMs not set")
	}
}

func TestControl_UpdateTask_TerminalStateConflict(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	tk, _ := d.taskStore.Create(ctx, tasks.Task{AgentID: "a"})
	_, _ = d.taskStore.Update(ctx, tk.TaskID, tasks.UpdateInput{State: tasks.StateCompleted})
	w := doJSON(t, h, http.MethodPatch, "/api/control/tasks/"+tk.TaskID, map[string]any{
		"state": "working",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

func TestControl_UpdateTask_InvalidState(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	tk, _ := d.taskStore.Create(context.Background(), tasks.Task{AgentID: "a"})
	w := doJSON(t, h, http.MethodPatch, "/api/control/tasks/"+tk.TaskID, map[string]any{
		"state": "flatulent",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestControl_UpdateTask_NotFound(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodPatch, "/api/control/tasks/tsk_ghost", map[string]any{
		"state": "working",
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

func TestControl_QueryActions_Empty(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodGet, "/api/control/actions", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct{ Actions []actionResponse }
	decodeBody(t, w, &resp)
	if len(resp.Actions) != 0 {
		t.Fatalf("got %d, want 0", len(resp.Actions))
	}
}

func TestControl_QueryActions_FilterByAgent(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	for _, a := range []string{"a", "a", "b"} {
		_, _ = d.actionStore.Record(ctx, actions.Action{
			AgentID: a, Capability: "c", PolicyDecision: "allow",
		})
	}
	w := doJSON(t, h, http.MethodGet, "/api/control/actions?agent_id=a", nil)
	var resp struct{ Actions []actionResponse }
	decodeBody(t, w, &resp)
	if len(resp.Actions) != 2 {
		t.Fatalf("got %d, want 2", len(resp.Actions))
	}
}

func TestControl_QueryActions_FilterByCapability(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	_, _ = d.actionStore.Record(ctx, actions.Action{AgentID: "a", Capability: "write", PolicyDecision: "allow"})
	_, _ = d.actionStore.Record(ctx, actions.Action{AgentID: "a", Capability: "delete", PolicyDecision: "allow"})
	w := doJSON(t, h, http.MethodGet, "/api/control/actions?capability=delete", nil)
	var resp struct{ Actions []actionResponse }
	decodeBody(t, w, &resp)
	if len(resp.Actions) != 1 {
		t.Fatalf("got %d, want 1", len(resp.Actions))
	}
}

func TestControl_QueryActions_InvalidLimit(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodGet, "/api/control/actions?limit=abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestControl_QueryActions_InvalidSince(t *testing.T) {
	h := routeThrough(newControlTestDaemon(t))
	w := doJSON(t, h, http.MethodGet, "/api/control/actions?since_ms=notanumber", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestControl_QueryActions_Limit(t *testing.T) {
	d := newControlTestDaemon(t)
	h := routeThrough(d)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		_, _ = d.actionStore.Record(ctx, actions.Action{
			AgentID: "a", Capability: "c", PolicyDecision: "allow",
		})
	}
	w := doJSON(t, h, http.MethodGet, "/api/control/actions?limit=2", nil)
	var resp struct{ Actions []actionResponse }
	decodeBody(t, w, &resp)
	if len(resp.Actions) != 2 {
		t.Fatalf("got %d, want 2", len(resp.Actions))
	}
}

// ---------------------------------------------------------------------------
// Unavailable (stores nil) behavior
// ---------------------------------------------------------------------------

func TestControl_Unavailable_Returns503(t *testing.T) {
	// Daemon with nil stores should return 503 on all control routes.
	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := routeThrough(d)
	paths := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/control/grants"},
		{http.MethodGet, "/api/control/grants"},
		{http.MethodDelete, "/api/control/grants/gnt_x"},
		{http.MethodPost, "/api/control/approvals"},
		{http.MethodGet, "/api/control/approvals"},
		{http.MethodPost, "/api/control/approvals/apr_x"},
		{http.MethodPost, "/api/control/tasks"},
		{http.MethodGet, "/api/control/tasks/tsk_x"},
		{http.MethodGet, "/api/control/tasks"},
		{http.MethodPatch, "/api/control/tasks/tsk_x"},
		{http.MethodGet, "/api/control/actions"},
	}
	for _, p := range paths {
		t.Run(p.method+"_"+p.path, func(t *testing.T) {
			var body io.Reader
			if p.method == http.MethodPost || p.method == http.MethodPatch {
				body = strings.NewReader(`{}`)
			}
			r := httptest.NewRequest(p.method, p.path, body)
			if body != nil {
				r.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", w.Code)
			}
		})
	}
}
