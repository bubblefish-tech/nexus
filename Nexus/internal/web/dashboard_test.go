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
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestDashboardAuthRequired(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: true,
		AdminKey:    []byte("test-admin-key"),
		Logger:      testLogger(t),
	})

	handler := d.withAuth(d.handleStatus)

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{"no auth", "", http.StatusUnauthorized},
		{"wrong token", "Bearer wrong-key", http.StatusUnauthorized},
		{"correct token", "Bearer test-admin-key", http.StatusOK},
		{"missing bearer prefix", "test-admin-key", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/dashboard/status", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("auth=%q: got status %d, want %d", tt.authHeader, rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestDashboardAuthDisabled(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		AdminKey:    []byte("test-admin-key"),
		Logger:      testLogger(t),
	})

	handler := d.withAuth(d.handleStatus)

	// No auth header, but auth is disabled — should succeed.
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/status", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("auth disabled: got status %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestDashboardStatusEndpoint(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		Logger:      testLogger(t),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/status", nil)
	rec := httptest.NewRecorder()
	d.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("got Content-Type %q, want application/json", ct)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body["status"])
	}
	if body["version"] == nil {
		t.Error("expected version field in response")
	}
}

func TestDashboardIndexHTML(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		Logger:      testLogger(t),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	d.handleIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "BubbleFish Nexus") {
		t.Error("expected dashboard HTML to contain 'BubbleFish Nexus'")
	}
	// INVARIANT: No innerHTML anywhere in the dashboard.
	if strings.Contains(body, "innerHTML") {
		t.Error("dashboard HTML must NEVER use innerHTML (XSS prevention)")
	}
	// Verify textContent is used.
	if !strings.Contains(body, "textContent") {
		t.Error("dashboard HTML must use textContent for dynamic content")
	}
}

func TestDashboardIndexNotFound(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		Logger:      testLogger(t),
	})

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()
	d.handleIndex(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("got status %d, want 404", rec.Code)
	}
}

// mockSecurityProvider implements SecurityProvider for tests.
type mockSecurityProvider struct{}

func (m *mockSecurityProvider) SourcePolicies() []SourcePolicyInfo {
	return []SourcePolicyInfo{
		{
			Name:                "claude-desktop",
			CanRead:             true,
			CanWrite:            true,
			AllowedDestinations: []string{"sqlite-local"},
			MaxResults:          20,
			MaxResponseBytes:    16384,
			RateLimit:           60,
		},
	}
}

func (m *mockSecurityProvider) AuthFailures(limit int) []AuthFailureInfo {
	return []AuthFailureInfo{
		{
			Timestamp:  "2026-04-06T10:00:00Z",
			Source:     "unknown",
			IP:        "127.0.0.1",
			Endpoint:   "/inbound/test",
			TokenClass: "unknown",
			StatusCode: 401,
		},
	}
}

func (m *mockSecurityProvider) LintFindings() []LintFinding {
	return []LintFinding{
		{
			Severity: "warn",
			Check:    "literal_key",
			Message:  "admin_token is a literal value; use env: or file: reference for production",
		},
	}
}

func TestDashboardSecurityEndpoint(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:             0,
		RequireAuth:      false,
		Logger:           testLogger(t),
		SecurityProvider: &mockSecurityProvider{},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/security", nil)
	rec := httptest.NewRecorder()
	d.handleSecurity(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("got Content-Type %q, want application/json", ct)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify sources are present.
	sources, ok := body["sources"].([]interface{})
	if !ok || len(sources) != 1 {
		t.Errorf("expected 1 source, got %v", body["sources"])
	}

	// Verify auth_failures are present.
	failures, ok := body["auth_failures"].([]interface{})
	if !ok || len(failures) != 1 {
		t.Errorf("expected 1 auth failure, got %v", body["auth_failures"])
	}

	// Verify lint_findings are present.
	findings, ok := body["lint_findings"].([]interface{})
	if !ok || len(findings) != 1 {
		t.Errorf("expected 1 lint finding, got %v", body["lint_findings"])
	}
}

func TestDashboardSecurityEndpointNoProvider(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		Logger:      testLogger(t),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/security", nil)
	rec := httptest.NewRecorder()
	d.handleSecurity(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// When no provider, should return empty arrays.
	sources, ok := body["sources"].([]interface{})
	if !ok || len(sources) != 0 {
		t.Errorf("expected empty sources, got %v", body["sources"])
	}
}

func TestDashboardSecurityTabInHTML(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		Logger:      testLogger(t),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	d.handleIndex(rec, req)

	body := rec.Body.String()

	// Security tab must be present.
	if !strings.Contains(body, "security-tab") {
		t.Error("dashboard HTML must contain security-tab panel")
	}

	// Source policies table.
	if !strings.Contains(body, "sec-policies-body") {
		t.Error("dashboard HTML must contain source policies table")
	}

	// Auth failures table.
	if !strings.Contains(body, "sec-failures-body") {
		t.Error("dashboard HTML must contain auth failures table")
	}

	// Lint warnings table.
	if !strings.Contains(body, "sec-lint-body") {
		t.Error("dashboard HTML must contain lint warnings table")
	}

	// INVARIANT: No innerHTML.
	if strings.Contains(body, "innerHTML") {
		t.Error("dashboard HTML must NEVER use innerHTML (XSS prevention)")
	}
}

// mockAuditProvider implements AuditProvider for tests.
type mockAuditProvider struct{}

func (m *mockAuditProvider) RecentInteractions(limit int) []AuditRecordInfo {
	return []AuditRecordInfo{
		{
			RecordID:       "rec-001",
			Timestamp:      "2026-04-06T12:00:00Z",
			Source:         "claude-desktop",
			ActorType:      "agent",
			ActorID:        "claude-3-opus",
			OperationType:  "write",
			Endpoint:       "/inbound/claude",
			HTTPStatusCode: 200,
			PolicyDecision: "allowed",
			LatencyMs:      4.2,
		},
	}
}

func (m *mockAuditProvider) InteractionsByActor(actorID string, limit int) []AuditRecordInfo {
	if actorID == "claude-3-opus" {
		return m.RecentInteractions(limit)
	}
	return nil
}

func (m *mockAuditProvider) PolicyDenials(limit int) []AuditRecordInfo {
	return []AuditRecordInfo{
		{
			RecordID:       "rec-002",
			Timestamp:      "2026-04-06T12:01:00Z",
			Source:         "untrusted",
			ActorType:      "agent",
			ActorID:        "rogue-agent",
			OperationType:  "query",
			Endpoint:       "/query/sqlite",
			HTTPStatusCode: 403,
			PolicyDecision: "denied",
			PolicyReason:   "tier_exceeds_maximum",
			LatencyMs:      0.5,
		},
	}
}

func (m *mockAuditProvider) AuditStats() AuditStatsInfo {
	return AuditStatsInfo{
		TotalRecords:      42,
		InteractionsPerHr: map[string]int{"write": 10, "query": 25},
		DenialRate:        0.05,
		FilterRate:        0.02,
		TopSources:        map[string]int{"claude-desktop": 30, "cursor": 12},
		TopActors:         map[string]int{"claude-3-opus": 20},
		ByOperation:       map[string]int{"write": 15, "query": 27},
		ByDecision:        map[string]int{"allowed": 38, "denied": 2, "filtered": 2},
	}
}

func TestDashboardAuditTabInHTML(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		Logger:      testLogger(t),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	d.handleIndex(rec, req)

	body := rec.Body.String()

	// Audit tab must be present.
	if !strings.Contains(body, "audit-tab") {
		t.Error("dashboard HTML must contain audit-tab panel")
	}

	// Audit records table.
	if !strings.Contains(body, "audit-records-body") {
		t.Error("dashboard HTML must contain audit records table")
	}

	// Audit stats elements.
	if !strings.Contains(body, "audit-total") {
		t.Error("dashboard HTML must contain audit-total stats element")
	}
	if !strings.Contains(body, "audit-denial-rate") {
		t.Error("dashboard HTML must contain audit-denial-rate stats element")
	}

	// INVARIANT: No innerHTML.
	if strings.Contains(body, "innerHTML") {
		t.Error("dashboard HTML must NEVER use innerHTML (XSS prevention)")
	}
}

func TestDashboardAuditEndpoint(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:          0,
		RequireAuth:   false,
		Logger:        testLogger(t),
		AuditProvider: &mockAuditProvider{},
	})

	tests := []struct {
		name  string
		query string
		check func(t *testing.T, body map[string]interface{})
	}{
		{
			name:  "recent",
			query: "?view=recent",
			check: func(t *testing.T, body map[string]interface{}) {
				t.Helper()
				records, ok := body["records"].([]interface{})
				if !ok || len(records) != 1 {
					t.Errorf("expected 1 recent record, got %v", body["records"])
				}
			},
		},
		{
			name:  "denials",
			query: "?view=denials",
			check: func(t *testing.T, body map[string]interface{}) {
				t.Helper()
				records, ok := body["records"].([]interface{})
				if !ok || len(records) != 1 {
					t.Errorf("expected 1 denial record, got %v", body["records"])
				}
			},
		},
		{
			name:  "agent timeline",
			query: "?view=agent&actor_id=claude-3-opus",
			check: func(t *testing.T, body map[string]interface{}) {
				t.Helper()
				records, ok := body["records"].([]interface{})
				if !ok || len(records) != 1 {
					t.Errorf("expected 1 agent record, got %v", body["records"])
				}
			},
		},
		{
			name:  "stats",
			query: "?view=stats",
			check: func(t *testing.T, body map[string]interface{}) {
				t.Helper()
				total, ok := body["total_records"].(float64)
				if !ok || total != 42 {
					t.Errorf("expected total_records=42, got %v", body["total_records"])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/dashboard/audit"+tt.query, nil)
			w := httptest.NewRecorder()
			d.handleAudit(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("got status %d, want 200", w.Code)
			}

			ct := w.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Fatalf("got Content-Type %q, want application/json", ct)
			}

			var body map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			tt.check(t, body)
		})
	}
}

func TestDashboardAuditEndpointNoProvider(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		Logger:      testLogger(t),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/audit?view=recent", nil)
	rec := httptest.NewRecorder()
	d.handleAudit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	records, ok := body["records"].([]interface{})
	if !ok || len(records) != 0 {
		t.Errorf("expected empty records, got %v", body["records"])
	}
}

func TestDashboardAuditStatsNoProvider(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		Logger:      testLogger(t),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/audit?view=stats", nil)
	rec := httptest.NewRecorder()
	d.handleAudit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rec.Code)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	total, ok := body["total_records"].(float64)
	if !ok || total != 0 {
		t.Errorf("expected total_records=0, got %v", body["total_records"])
	}
}

func TestDashboardSSEHeaders(t *testing.T) {
	t.Helper()

	d := New(Config{
		Port:        0,
		RequireAuth: false,
		Logger:      testLogger(t),
	})

	// Create a request with a cancellable context so the SSE handler exits.
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		d.handleSSE(rec, req)
		close(done)
	}()

	// Cancel immediately so the handler exits.
	cancel()
	<-done

	ct := rec.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("got Content-Type %q, want text/event-stream", ct)
	}
}
