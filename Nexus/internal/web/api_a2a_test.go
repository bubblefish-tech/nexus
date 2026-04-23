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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/governance"
	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/a2a/store"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
	"github.com/bubblefish-tech/nexus/web/dashboard"
)

const testAdminToken = "test-admin-key-a2a"

// newTestA2ADashboard creates an A2ADashboard backed by temp SQLite databases.
func newTestA2ADashboard(t *testing.T) *A2ADashboard {
	t.Helper()

	dir := t.TempDir()

	// Registry store.
	regPath := filepath.Join(dir, "registry.db")
	reg, err := registry.NewStore(regPath)
	if err != nil {
		t.Fatalf("registry store: %v", err)
	}
	t.Cleanup(func() { reg.Close() })

	// Task store (also provides the DB for governance).
	tsPath := filepath.Join(dir, "tasks.db")
	ts, err := store.NewSQLiteTaskStore(tsPath)
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { ts.Close() })

	// Governance store + engine.
	if err := governance.MigrateGrants(ts.DB()); err != nil {
		t.Fatalf("migrate grants: %v", err)
	}
	gs := governance.NewGrantStore(ts.DB())
	gov := governance.NewEngine(gs, governance.WithLogger(testLogger(t)))

	return NewA2ADashboard(reg, gov, ts, testAdminToken, testLogger(t))
}

func authReq(method, url string, body []byte) *http.Request {
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, url, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, url, nil)
	}
	r.Header.Set("Authorization", "Bearer "+testAdminToken)
	r.Header.Set("Content-Type", "application/json")
	return r
}

func noAuthReq(method, url string) *http.Request {
	return httptest.NewRequest(method, url, nil)
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return m
}

// --- Auth tests ---

func TestA2AAuthRequiredOnAllEndpoints(t *testing.T) {
	t.Helper()

	d := newTestA2ADashboard(t)
	handler := d.Handler()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/a2a/agents"},
		{http.MethodPost, "/api/a2a/agents"},
		{http.MethodDelete, "/api/a2a/agents?id=x"},
		{http.MethodGet, "/api/a2a/grants"},
		{http.MethodPost, "/api/a2a/grants"},
		{http.MethodDelete, "/api/a2a/grants?id=x"},
		{http.MethodPost, "/api/a2a/grants/elevated"},
		{http.MethodGet, "/api/a2a/approvals"},
		{http.MethodPost, "/api/a2a/approvals/test-id/decide"},
		{http.MethodGet, "/api/a2a/audit"},
		{http.MethodGet, "/api/a2a/openclaw/status"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path+" no auth", func(t *testing.T) {
			req := noAuthReq(ep.method, ep.path)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d for %s %s", rec.Code, ep.method, ep.path)
			}
		})

		t.Run(ep.method+" "+ep.path+" wrong token", func(t *testing.T) {
			req := noAuthReq(ep.method, ep.path)
			req.Header.Set("Authorization", "Bearer wrong-token")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d for %s %s", rec.Code, ep.method, ep.path)
			}
		})
	}
}

func TestA2AAuthMissingBearerPrefix(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/a2a/agents", nil)
	req.Header.Set("Authorization", testAdminToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without Bearer prefix, got %d", rec.Code)
	}
}

// --- Agents tests ---

func TestA2AAgentsListEmpty(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/agents", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	body := decodeJSON(t, rec)
	agents := body["agents"].([]interface{})
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestA2AAgentsCreateAndList(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Create agent.
	payload := `{"name":"test-agent","display_name":"Test Agent","url":"http://localhost:9000","transport":"http"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/agents", []byte(payload)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d, want 201, body: %s", rec.Code, rec.Body.String())
	}
	created := decodeJSON(t, rec)
	agentID, ok := created["agent_id"].(string)
	if !ok || agentID == "" {
		t.Fatal("expected agent_id in response")
	}

	// List agents.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, authReq(http.MethodGet, "/api/a2a/agents", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("list: got %d", rec2.Code)
	}
	body := decodeJSON(t, rec2)
	agents := body["agents"].([]interface{})
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}
}

func TestA2AAgentsCreateMissingName(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	payload := `{"url":"http://localhost:9000"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/agents", []byte(payload)))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rec.Code)
	}
}

func TestA2AAgentsCreateInvalidJSON(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/agents", []byte("not json")))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rec.Code)
	}
}

func TestA2AAgentsDelete(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Create.
	payload := `{"name":"del-agent","url":"http://localhost:9000"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/agents", []byte(payload)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d", rec.Code)
	}
	agentID := decodeJSON(t, rec)["agent_id"].(string)

	// Delete.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, authReq(http.MethodDelete, "/api/a2a/agents?id="+agentID, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("delete: got %d", rec2.Code)
	}

	// Verify deleted.
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, authReq(http.MethodGet, "/api/a2a/agents", nil))
	body := decodeJSON(t, rec3)
	agents := body["agents"].([]interface{})
	if len(agents) != 0 {
		t.Errorf("expected 0 agents after delete, got %d", len(agents))
	}
}

func TestA2AAgentsDeleteMissingID(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodDelete, "/api/a2a/agents", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rec.Code)
	}
}

func TestA2AAgentsDeleteNotFound(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodDelete, "/api/a2a/agents?id=nonexistent", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

// --- Grants tests ---

func TestA2AGrantsListEmpty(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/grants", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	grants := body["grants"].([]interface{})
	if len(grants) != 0 {
		t.Errorf("expected 0 grants, got %d", len(grants))
	}
}

func TestA2AGrantsCreateAndList(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	payload := `{"source_agent_id":"agent-a","target_agent_id":"agent-b","decision":"allow","reason":"test"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/grants", []byte(payload)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create grant: got %d, body: %s", rec.Code, rec.Body.String())
	}

	// List.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, authReq(http.MethodGet, "/api/a2a/grants", nil))
	body := decodeJSON(t, rec2)
	grants := body["grants"].([]interface{})
	if len(grants) != 1 {
		t.Errorf("expected 1 grant, got %d", len(grants))
	}
}

func TestA2AGrantsCreateMissingFields(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	payload := `{"source_agent_id":"agent-a"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/grants", []byte(payload)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rec.Code)
	}
}

func TestA2AGrantsRevoke(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Create.
	payload := `{"source_agent_id":"agent-a","target_agent_id":"agent-b","decision":"allow"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/grants", []byte(payload)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d", rec.Code)
	}
	grantID := decodeJSON(t, rec)["grantId"].(string)

	// Revoke.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, authReq(http.MethodDelete, "/api/a2a/grants?id="+grantID, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("revoke: got %d, body: %s", rec2.Code, rec2.Body.String())
	}
}

func TestA2AGrantsRevokeNotFound(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodDelete, "/api/a2a/grants?id=gnt_nonexistent", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", rec.Code)
	}
}

func TestA2AGrantsRevokeMissingID(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodDelete, "/api/a2a/grants", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rec.Code)
	}
}

func TestA2AGrantsFilterBySource(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Create two grants with different sources.
	for _, src := range []string{"agent-x", "agent-y"} {
		p := fmt.Sprintf(`{"source_agent_id":"%s","target_agent_id":"agent-z","decision":"allow"}`, src)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/grants", []byte(p)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: got %d", rec.Code)
		}
	}

	// Filter by source.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/grants?source=agent-x", nil))
	body := decodeJSON(t, rec)
	grants := body["grants"].([]interface{})
	if len(grants) != 1 {
		t.Errorf("expected 1 filtered grant, got %d", len(grants))
	}
}

func TestA2AGrantsFilterByTarget(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Create two grants with different targets.
	for _, tgt := range []string{"agent-p", "agent-q"} {
		p := fmt.Sprintf(`{"source_agent_id":"agent-a","target_agent_id":"%s","decision":"allow"}`, tgt)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/grants", []byte(p)))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: got %d", rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/grants?target=agent-q", nil))
	body := decodeJSON(t, rec)
	grants := body["grants"].([]interface{})
	if len(grants) != 1 {
		t.Errorf("expected 1 filtered grant, got %d", len(grants))
	}
}

// --- Elevated grant tests ---

func TestA2AGrantsElevatedHappyPath(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	payload := `{"source_agent_id":"agent-a","target_agent_id":"agent-b","required_capabilities":["*"],"decision":"allow"}`
	req := authReq(http.MethodPost, "/api/a2a/grants/elevated", []byte(payload))
	req.Header.Set("X-Nexus-Reauth-Token", testAdminToken)
	req.Header.Set("X-Nexus-Consent-Ticket", "ticket-123")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("elevated grant: got %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestA2AGrantsElevatedMissingReauth(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	payload := `{"source_agent_id":"agent-a","target_agent_id":"agent-b","required_capabilities":["*"],"decision":"allow"}`
	req := authReq(http.MethodPost, "/api/a2a/grants/elevated", []byte(payload))
	// No X-Nexus-Reauth-Token.
	req.Header.Set("X-Nexus-Consent-Ticket", "ticket-123")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["error"] != "reauth_required" {
		t.Errorf("expected error=reauth_required, got %v", body["error"])
	}
}

func TestA2AGrantsElevatedWrongReauth(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	payload := `{"source_agent_id":"agent-a","target_agent_id":"agent-b","required_capabilities":["*"],"decision":"allow"}`
	req := authReq(http.MethodPost, "/api/a2a/grants/elevated", []byte(payload))
	req.Header.Set("X-Nexus-Reauth-Token", "wrong-token")
	req.Header.Set("X-Nexus-Consent-Ticket", "ticket-123")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestA2AGrantsElevatedMissingConsentTicket(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	payload := `{"source_agent_id":"agent-a","target_agent_id":"agent-b","required_capabilities":["*"],"decision":"allow"}`
	req := authReq(http.MethodPost, "/api/a2a/grants/elevated", []byte(payload))
	req.Header.Set("X-Nexus-Reauth-Token", testAdminToken)
	// No X-Nexus-Consent-Ticket.

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["error"] != "consent_required" {
		t.Errorf("expected error=consent_required, got %v", body["error"])
	}
}

func TestA2AGrantsElevatedNonAllRejected(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Request without * capability.
	payload := `{"source_agent_id":"agent-a","target_agent_id":"agent-b","required_capabilities":["read"],"decision":"allow"}`
	req := authReq(http.MethodPost, "/api/a2a/grants/elevated", []byte(payload))
	req.Header.Set("X-Nexus-Reauth-Token", testAdminToken)
	req.Header.Set("X-Nexus-Consent-Ticket", "ticket-123")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-ALL grant, got %d", rec.Code)
	}
}

func TestA2AGrantsElevatedMethodNotAllowed(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/grants/elevated", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Approvals tests ---

func TestA2AApprovalsListEmpty(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/approvals", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	approvals := body["approvals"].([]interface{})
	if len(approvals) != 0 {
		t.Errorf("expected 0 approvals, got %d", len(approvals))
	}
}

func TestA2AApprovalsDecideApproved(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Create a pending approval directly in the governance store.
	approvalID := a2a.NewApprovalID()
	ap := &governance.PendingApproval{
		ApprovalID:           approvalID,
		TaskID:               a2a.NewTaskID(),
		SourceAgentID:        "agent-a",
		TargetAgentID:        "agent-b",
		Skill:                "test-skill",
		RequiredCapabilities: []string{"read"},
		InputPreview:         "test input",
	}
	// We need to insert directly via the governance store. Use the engine's store.
	// Since we can't access the store directly from the engine, create a separate one.
	gs := governance.NewGrantStore(d.taskStore.DB())
	ap.CreatedAt = time.Now()
	if err := gs.CreateApproval(ap); err != nil {
		t.Fatalf("create approval: %v", err)
	}

	// Verify it shows in list.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/approvals", nil))
	body := decodeJSON(t, rec)
	approvals := body["approvals"].([]interface{})
	if len(approvals) != 1 {
		t.Fatalf("expected 1 approval, got %d", len(approvals))
	}

	// Decide: approved.
	payload := `{"decision":"approved"}`
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, authReq(http.MethodPost, "/api/a2a/approvals/"+approvalID+"/decide", []byte(payload)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("decide: got %d, body: %s", rec2.Code, rec2.Body.String())
	}
	result := decodeJSON(t, rec2)
	if result["decision"] != "approved" {
		t.Errorf("expected decision=approved, got %v", result["decision"])
	}
}

func TestA2AApprovalsDecideDenied(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	approvalID := a2a.NewApprovalID()
	gs := governance.NewGrantStore(d.taskStore.DB())
	ap := &governance.PendingApproval{
		ApprovalID:           approvalID,
		TaskID:               a2a.NewTaskID(),
		SourceAgentID:        "agent-a",
		TargetAgentID:        "agent-b",
		Skill:                "test-skill",
		RequiredCapabilities: []string{"write"},
	}
	ap.CreatedAt = time.Now()
	if err := gs.CreateApproval(ap); err != nil {
		t.Fatalf("create approval: %v", err)
	}

	payload := `{"decision":"denied"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/approvals/"+approvalID+"/decide", []byte(payload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("decide: got %d, body: %s", rec.Code, rec.Body.String())
	}
	result := decodeJSON(t, rec)
	if result["decision"] != "denied" {
		t.Errorf("expected decision=denied, got %v", result["decision"])
	}
}

func TestA2AApprovalsDecideInvalidDecision(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	payload := `{"decision":"maybe"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/approvals/some-id/decide", []byte(payload)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestA2AApprovalsDecideBadPath(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/approvals/", []byte(`{"decision":"approved"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad path, got %d", rec.Code)
	}
}

func TestA2AApprovalsDecideNotFound(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	payload := `{"decision":"approved"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/approvals/nonexistent/decide", []byte(payload)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestA2AApprovalsDecideMethodNotAllowed(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/approvals/some-id/decide", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Audit tests ---

func TestA2AAuditEmpty(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/audit", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	events := body["events"].([]interface{})
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestA2AAuditWithGrants(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Create a grant.
	payload := `{"source_agent_id":"agent-a","target_agent_id":"agent-b","decision":"allow"}`
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/grants", []byte(payload)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: got %d", rec.Code)
	}

	// Audit should show grant event.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, authReq(http.MethodGet, "/api/a2a/audit", nil))
	body := decodeJSON(t, rec2)
	events := body["events"].([]interface{})
	if len(events) < 1 {
		t.Errorf("expected at least 1 audit event, got %d", len(events))
	}
	// First event should be a grant event.
	first := events[0].(map[string]interface{})
	if first["event_type"] != "grant" {
		t.Errorf("expected event_type=grant, got %v", first["event_type"])
	}
}

func TestA2AAuditMethodNotAllowed(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/audit", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- OpenClaw status tests ---

func TestA2AOpenClawStatusNoAgent(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/openclaw/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["connected"] != false {
		t.Error("expected connected=false when no agent")
	}
}

func TestA2AOpenClawStatusWithAgent(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Register an agent named "openclaw".
	agent := registry.RegisteredAgent{
		AgentID:     "agent_openclaw_001",
		Name:        "openclaw",
		DisplayName: "OpenClaw",
		AgentCard: a2a.AgentCard{
			Name:            "openclaw",
			URL:             "http://localhost:5000",
			ProtocolVersion: "0.1.0",
			Version:         "1.0.0",
			Endpoints:       []a2a.Endpoint{{URL: "http://localhost:5000", Transport: a2a.TransportHTTP}},
			Skills: []a2a.Skill{
				{
					ID:                   "code-review",
					Name:                 "Code Review",
					Description:          "Reviews code",
					RequiredCapabilities: []string{"read"},
				},
			},
		},
		TransportConfig: transport.TransportConfig{Kind: "http", URL: "http://localhost:5000"},
		Status:          registry.StatusActive,
	}
	ctx := context.Background()
	if err := d.registry.Register(ctx, agent); err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodGet, "/api/a2a/openclaw/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["connected"] != true {
		t.Error("expected connected=true")
	}
	skills := body["skills"].([]interface{})
	if len(skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(skills))
	}
}

func TestA2AOpenClawStatusMethodNotAllowed(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/openclaw/status", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- JSON response format tests ---

func TestA2AResponsesAreJSON(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/a2a/agents"},
		{http.MethodGet, "/api/a2a/grants"},
		{http.MethodGet, "/api/a2a/approvals"},
		{http.MethodGet, "/api/a2a/audit"},
		{http.MethodGet, "/api/a2a/openclaw/status"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, authReq(ep.method, ep.path, nil))
			ct := rec.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("got Content-Type %q, want application/json", ct)
			}
		})
	}
}

func TestA2AErrorResponseFormat(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	// Trigger a 401.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, noAuthReq(http.MethodGet, "/api/a2a/agents"))
	body := decodeJSON(t, rec)
	if _, ok := body["error"]; !ok {
		t.Error("expected 'error' field in error response")
	}
	if _, ok := body["message"]; !ok {
		t.Error("expected 'message' field in error response")
	}
}

// --- HTML tests ---

func TestA2APermissionsHTMLNoInnerHTML(t *testing.T) {
	body := dashboard.A2APermissionsHTML

	if strings.Contains(body, "innerHTML") {
		t.Error("a2a_permissions.html must NEVER use innerHTML (XSS prevention)")
	}
	if !strings.Contains(body, "textContent") {
		t.Error("a2a_permissions.html must use textContent for dynamic content")
	}
	if !strings.Contains(body, "BubbleFish Nexus") {
		t.Error("expected 'BubbleFish Nexus' in HTML")
	}
}

func TestOpenClawHTMLNoInnerHTML(t *testing.T) {
	body := dashboard.OpenClawHTML

	if strings.Contains(body, "innerHTML") {
		t.Error("openclaw.html must NEVER use innerHTML (XSS prevention)")
	}
	if !strings.Contains(body, "textContent") {
		t.Error("openclaw.html must use textContent for dynamic content")
	}
	if !strings.Contains(body, "OpenClaw") {
		t.Error("expected 'OpenClaw' in HTML")
	}
}

// --- Agents method not allowed ---

func TestA2AAgentsMethodNotAllowed(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPut, "/api/a2a/agents", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Grants method not allowed ---

func TestA2AGrantsMethodNotAllowed(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPut, "/api/a2a/grants", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// --- Approvals list method not allowed ---

func TestA2AApprovalsListMethodNotAllowed(t *testing.T) {
	d := newTestA2ADashboard(t)
	handler := d.Handler()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authReq(http.MethodPost, "/api/a2a/approvals", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}
