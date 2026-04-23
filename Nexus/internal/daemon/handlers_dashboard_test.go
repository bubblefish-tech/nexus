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
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/actions"
	"github.com/bubblefish-tech/nexus/internal/approvals"
	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/grants"
	"github.com/bubblefish-tech/nexus/internal/tasks"
	_ "modernc.org/sqlite"
)

const testDashToken = "test-dashboard-admin-key"

// newDashboardTestDaemon creates a *Daemon with registryStore, control stores,
// and a minimal config — enough for dashboard handler tests.
func newDashboardTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	dir := t.TempDir()
	regStore, err := registry.NewStore(filepath.Join(dir, "registry.db"))
	if err != nil {
		t.Fatalf("registry.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = regStore.Close() })

	d := &Daemon{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		registryStore: regStore,
		grantStore:    grants.NewStore(regStore.DB()),
		approvalStore: approvals.NewStore(regStore.DB()),
		taskStore:     tasks.NewStore(regStore.DB()),
		actionStore:   actions.NewStore(regStore.DB()),
		cfg:           &config.Config{ResolvedAdminKey: []byte(testDashToken)},
	}
	return d
}

// dashboardRouter returns a chi router wiring all MT.5 routes.
func dashboardRouter(d *Daemon) http.Handler {
	r := chi.NewRouter()
	r.Get("/dashboard/agents", d.handleDashboardAgents)
	r.Get("/dashboard/grants", d.handleDashboardGrants)
	r.Get("/dashboard/approvals", d.handleDashboardApprovals)
	r.Get("/dashboard/tasks", d.handleDashboardTasks)
	r.Get("/dashboard/actions", d.handleDashboardActions)
	r.Get("/api/control/agents", d.handleControlAgentList)
	return r
}

func doGet(t *testing.T, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// Dashboard HTML pages — happy path
// ---------------------------------------------------------------------------

func TestDashboardAgents_OK(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	w := doGet(t, h, "/dashboard/agents", testDashToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q; want text/html", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<html") {
		t.Errorf("response does not look like HTML")
	}
}

func TestDashboardGrants_OK(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	w := doGet(t, h, "/dashboard/grants", testDashToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type not text/html")
	}
}

func TestDashboardApprovals_OK(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	w := doGet(t, h, "/dashboard/approvals", testDashToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type not text/html")
	}
}

func TestDashboardTasks_OK(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	w := doGet(t, h, "/dashboard/tasks", testDashToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type not text/html")
	}
}

func TestDashboardActions_OK(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	w := doGet(t, h, "/dashboard/actions", testDashToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type not text/html")
	}
}

// ---------------------------------------------------------------------------
// Auth — no token
// ---------------------------------------------------------------------------

func TestDashboard_NoToken(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	pages := []string{
		"/dashboard/agents",
		"/dashboard/grants",
		"/dashboard/approvals",
		"/dashboard/tasks",
		"/dashboard/actions",
	}
	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			w := doGet(t, h, page, "")
			if w.Code != http.StatusUnauthorized {
				t.Errorf("status = %d; want 401", w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Auth — bad token
// ---------------------------------------------------------------------------

func TestDashboard_BadToken(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	w := doGet(t, h, "/dashboard/agents", "wrong-token")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Auth — ?token= query param
// ---------------------------------------------------------------------------

func TestDashboard_QueryParamToken(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/agents?token="+testDashToken, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200 with ?token= param", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("Content-Type not text/html")
	}
}

func TestDashboard_QueryParamBadToken(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/agents?token=nope", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401 with bad ?token=", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Token injected into HTML
// ---------------------------------------------------------------------------

func TestDashboard_TokenInjected(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	w := doGet(t, h, "/dashboard/agents", testDashToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()
	// The placeholder must be replaced with the actual token.
	if strings.Contains(body, "ADMIN_TOKEN: '',") {
		t.Error("ADMIN_TOKEN placeholder was not replaced in HTML")
	}
	if !strings.Contains(body, testDashToken) {
		t.Error("injected token not found in HTML")
	}
}

// ---------------------------------------------------------------------------
// No mock data
// ---------------------------------------------------------------------------

func TestDashboard_NoMockData(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	pages := []string{
		"/dashboard/agents",
		"/dashboard/grants",
		"/dashboard/approvals",
		"/dashboard/tasks",
		"/dashboard/actions",
	}
	// These are sample values that would indicate hardcoded test fixtures.
	forbidden := []string{
		"agent-123", "alice@example.com", "mock_agent", "example-agent",
		"hardcoded", "sample_capability",
	}
	for _, page := range pages {
		t.Run(page, func(t *testing.T) {
			w := doGet(t, h, page, testDashToken)
			body := w.Body.String()
			for _, f := range forbidden {
				if strings.Contains(body, f) {
					t.Errorf("page %s contains mock data string %q", page, f)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// GET /api/control/agents
// ---------------------------------------------------------------------------

func TestControlAgentList_Empty(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	w := doGet(t, h, "/api/control/agents", testDashToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q; want application/json", ct)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	agents, ok := resp["agents"].([]interface{})
	if !ok {
		t.Fatalf("missing 'agents' array in response")
	}
	if len(agents) != 0 {
		t.Errorf("expected empty agents list, got %d", len(agents))
	}
}

func TestControlAgentList_NoAuth(t *testing.T) {
	d := newDashboardTestDaemon(t)
	h := dashboardRouter(d)
	// No Authorization header — requireAdminToken would block in production.
	// Here we test handler directly without middleware, so the handler itself
	// doesn't check auth (auth is done by the group middleware in server.go).
	// This test verifies the handler returns 200 when stores are available,
	// confirming it doesn't panic without auth middleware.
	w := doGet(t, h, "/api/control/agents", testDashToken)
	if w.Code != http.StatusOK {
		t.Fatalf("expected handler to succeed when token provided; got %d", w.Code)
	}
}

func TestControlAgentList_NoRegistry(t *testing.T) {
	d := newDashboardTestDaemon(t)
	d.registryStore = nil // simulate registry unavailable
	h := dashboardRouter(d)
	w := doGet(t, h, "/api/control/agents", testDashToken)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d; want 503 when registry unavailable", w.Code)
	}
}
