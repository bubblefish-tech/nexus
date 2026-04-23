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

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeControlServer starts an httptest server that handles control-plane API
// calls with canned JSON responses. The provided handlers map overrides default
// empty-list responses for specific paths.
func fakeControlServer(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, *controlClient) {
	t.Helper()
	mux := http.NewServeMux()
	// Defaults: return empty lists for all known paths.
	defaults := map[string]interface{}{
		"/api/control/grants":    map[string]interface{}{"grants": []interface{}{}},
		"/api/control/approvals": map[string]interface{}{"approvals": []interface{}{}},
		"/api/control/tasks":     map[string]interface{}{"tasks": []interface{}{}},
		"/api/control/actions":   map[string]interface{}{"actions": []interface{}{}},
	}
	for path, body := range defaults {
		p, b := path, body
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			if h, ok := handlers[r.Method+" "+r.URL.Path]; ok {
				h(w, r)
				return
			}
			if h, ok := handlers[r.URL.Path]; ok {
				h(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(b)
		})
	}
	// Allow any unmapped path to be handled via handlers.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if h, ok := handlers[r.Method+" "+r.URL.Path]; ok {
			h(w, r)
			return
		}
		if h, ok := handlers[r.URL.Path]; ok {
			h(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_found"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cl := &controlClient{
		http:    &http.Client{},
		baseURL: srv.URL,
		token:   "test-token",
		out:     &bytes.Buffer{},
		errOut:  &bytes.Buffer{},
	}
	return srv, cl
}

// ---------------------------------------------------------------------------
// grant list
// ---------------------------------------------------------------------------

func TestGrantList_Empty(t *testing.T) {
	_, cl := fakeControlServer(t, nil)
	var out bytes.Buffer
	cl.out = &out
	if err := doGrantList(cl, nil); err != nil {
		t.Fatalf("doGrantList: %v", err)
	}
	if !strings.Contains(out.String(), "No grants") {
		t.Errorf("output = %q; want 'No grants.'", out.String())
	}
}

func TestGrantList_TableOutput(t *testing.T) {
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"/api/control/grants": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"grants": []map[string]interface{}{
					{
						"grant_id":      "g-001",
						"agent_id":      "agent-abc",
						"capability":    "nexus_write",
						"granted_by":    "cli",
						"granted_at_ms": 1700000000000,
					},
				},
			})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	if err := doGrantList(cl, nil); err != nil {
		t.Fatalf("doGrantList: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, "g-001") {
		t.Errorf("output missing grant_id; got %q", body)
	}
	if !strings.Contains(body, "nexus_write") {
		t.Errorf("output missing capability; got %q", body)
	}
}

func TestGrantList_JSON(t *testing.T) {
	_, cl := fakeControlServer(t, nil)
	var out bytes.Buffer
	cl.out = &out
	if err := doGrantList(cl, []string{"--json"}); err != nil {
		t.Fatalf("doGrantList --json: %v", err)
	}
	// Output must be valid JSON.
	var v interface{}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Errorf("--json output is not valid JSON: %v\n%s", err, out.String())
	}
}

// ---------------------------------------------------------------------------
// grant create
// ---------------------------------------------------------------------------

func TestGrantCreate_SendsCorrectBody(t *testing.T) {
	var gotBody map[string]interface{}
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"POST /api/control/grants": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"grant": map[string]interface{}{
					"grant_id":   "g-new",
					"agent_id":   "agent-xyz",
					"capability": "nexus_write",
					"granted_by": "cli",
				},
			})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	err := doGrantCreate(cl, []string{"--agent", "agent-xyz", "--capability", "nexus_write"})
	if err != nil {
		t.Fatalf("doGrantCreate: %v", err)
	}
	if gotBody["agent_id"] != "agent-xyz" {
		t.Errorf("body agent_id = %v; want agent-xyz", gotBody["agent_id"])
	}
	if gotBody["capability"] != "nexus_write" {
		t.Errorf("body capability = %v; want nexus_write", gotBody["capability"])
	}
}

func TestGrantCreate_MissingRequired(t *testing.T) {
	_, cl := fakeControlServer(t, nil)
	if err := doGrantCreate(cl, []string{"--agent", "x"}); err == nil {
		t.Error("expected error for missing --capability")
	}
}

// ---------------------------------------------------------------------------
// grant revoke
// ---------------------------------------------------------------------------

func TestGrantRevoke_SendsDelete(t *testing.T) {
	var gotMethod string
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"/api/control/grants/g-999": func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	if err := doGrantRevoke(cl, []string{"--id", "g-999", "--reason", "expired"}); err != nil {
		t.Fatalf("doGrantRevoke: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s; want DELETE", gotMethod)
	}
	if !strings.Contains(out.String(), "g-999") {
		t.Errorf("output missing grant id; got %q", out.String())
	}
}

// ---------------------------------------------------------------------------
// approval list
// ---------------------------------------------------------------------------

func TestApprovalList_Pending(t *testing.T) {
	var gotURL string
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"/api/control/approvals": func(w http.ResponseWriter, r *http.Request) {
			gotURL = r.URL.String()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"approvals": []interface{}{}})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	if err := doApprovalList(cl, []string{"--status", "pending"}); err != nil {
		t.Fatalf("doApprovalList: %v", err)
	}
	if !strings.Contains(gotURL, "status=pending") {
		t.Errorf("URL %q missing status=pending filter", gotURL)
	}
}

func TestApprovalList_TableOutput(t *testing.T) {
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"/api/control/approvals": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"approvals": []map[string]interface{}{
					{
						"request_id":      "req-001",
						"agent_id":        "agent-abc",
						"capability":      "nexus_delete",
						"status":          "pending",
						"requested_at_ms": 1700000000000,
					},
				},
			})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	if err := doApprovalList(cl, nil); err != nil {
		t.Fatalf("doApprovalList: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, "req-001") {
		t.Errorf("output missing request_id; got %q", body)
	}
}

// ---------------------------------------------------------------------------
// approval decide
// ---------------------------------------------------------------------------

func TestApprovalDecide_SendsPost(t *testing.T) {
	var gotBody map[string]interface{}
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"/api/control/approvals/req-001": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	err := doApprovalDecide(cl, []string{"--id", "req-001", "--decision", "approve", "--reason", "looks good"})
	if err != nil {
		t.Fatalf("doApprovalDecide: %v", err)
	}
	if gotBody["decision"] != "approve" {
		t.Errorf("body decision = %v; want approve", gotBody["decision"])
	}
}

func TestApprovalDecide_InvalidDecision(t *testing.T) {
	_, cl := fakeControlServer(t, nil)
	err := doApprovalDecide(cl, []string{"--id", "req-001", "--decision", "maybe"})
	if err == nil {
		t.Error("expected error for invalid decision value")
	}
}

// ---------------------------------------------------------------------------
// task list
// ---------------------------------------------------------------------------

func TestTaskList_Empty(t *testing.T) {
	_, cl := fakeControlServer(t, nil)
	var out bytes.Buffer
	cl.out = &out
	if err := doTaskList(cl, nil); err != nil {
		t.Fatalf("doTaskList: %v", err)
	}
	if !strings.Contains(out.String(), "No tasks") {
		t.Errorf("output = %q; want 'No tasks.'", out.String())
	}
}

func TestTaskList_AgentFilter(t *testing.T) {
	var gotURL string
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"/api/control/tasks": func(w http.ResponseWriter, r *http.Request) {
			gotURL = r.URL.String()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"tasks": []interface{}{}})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	if err := doTaskList(cl, []string{"--agent", "agent-001"}); err != nil {
		t.Fatalf("doTaskList: %v", err)
	}
	if !strings.Contains(gotURL, "agent_id=agent-001") {
		t.Errorf("URL %q missing agent filter", gotURL)
	}
}

// ---------------------------------------------------------------------------
// task inspect
// ---------------------------------------------------------------------------

func TestTaskInspect_TableOutput(t *testing.T) {
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"/api/control/tasks/t-001": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"task_id":        "t-001",
				"agent_id":       "agent-abc",
				"capability":     "nexus_write",
				"state":          "completed",
				"created_at_ms":  1700000000000,
			})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	if err := doTaskInspect(cl, []string{"--id", "t-001"}); err != nil {
		t.Fatalf("doTaskInspect: %v", err)
	}
	body := out.String()
	if !strings.Contains(body, "t-001") {
		t.Errorf("output missing task_id; got %q", body)
	}
	if !strings.Contains(body, "completed") {
		t.Errorf("output missing state; got %q", body)
	}
}

func TestTaskInspect_MissingID(t *testing.T) {
	_, cl := fakeControlServer(t, nil)
	if err := doTaskInspect(cl, nil); err == nil {
		t.Error("expected error for missing --id")
	}
}

// ---------------------------------------------------------------------------
// action log
// ---------------------------------------------------------------------------

func TestActionLog_Empty(t *testing.T) {
	_, cl := fakeControlServer(t, nil)
	var out bytes.Buffer
	cl.out = &out
	if err := doActionLog(cl, nil); err != nil {
		t.Fatalf("doActionLog: %v", err)
	}
	if !strings.Contains(out.String(), "No actions") {
		t.Errorf("output = %q; want 'No actions.'", out.String())
	}
}

func TestActionLog_Filters(t *testing.T) {
	var gotURL string
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"/api/control/actions": func(w http.ResponseWriter, r *http.Request) {
			gotURL = r.URL.String()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"actions": []interface{}{}})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	if err := doActionLog(cl, []string{"--agent", "agent-001", "--capability", "nexus_write"}); err != nil {
		t.Fatalf("doActionLog: %v", err)
	}
	if !strings.Contains(gotURL, "agent_id=agent-001") {
		t.Errorf("URL %q missing agent filter", gotURL)
	}
	if !strings.Contains(gotURL, "capability=nexus_write") {
		t.Errorf("URL %q missing capability filter", gotURL)
	}
}

func TestActionLog_SinceDuration(t *testing.T) {
	var gotURL string
	_, cl := fakeControlServer(t, map[string]http.HandlerFunc{
		"/api/control/actions": func(w http.ResponseWriter, r *http.Request) {
			gotURL = r.URL.String()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"actions": []interface{}{}})
		},
	})
	var out bytes.Buffer
	cl.out = &out
	if err := doActionLog(cl, []string{"--since", "1h"}); err != nil {
		t.Fatalf("doActionLog --since 1h: %v", err)
	}
	if !strings.Contains(gotURL, "since_ms=") {
		t.Errorf("URL %q missing since_ms param", gotURL)
	}
}

func TestActionLog_JSON(t *testing.T) {
	_, cl := fakeControlServer(t, nil)
	var out bytes.Buffer
	cl.out = &out
	if err := doActionLog(cl, []string{"--json"}); err != nil {
		t.Fatalf("doActionLog --json: %v", err)
	}
	var v interface{}
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Errorf("--json output is not valid JSON: %v\n%s", err, out.String())
	}
}

// ---------------------------------------------------------------------------
// parseFlags
// ---------------------------------------------------------------------------

func TestParseFlags_UnknownFlag(t *testing.T) {
	var s string
	err := parseFlags([]string{"--unknown"}, map[string]*string{"known": &s}, nil)
	if err == nil {
		t.Error("expected error for unknown flag")
	}
}

func TestParseFlags_EqualsSyntax(t *testing.T) {
	var s string
	if err := parseFlags([]string{"--key=value"}, map[string]*string{"key": &s}, nil); err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if s != "value" {
		t.Errorf("s = %q; want value", s)
	}
}
