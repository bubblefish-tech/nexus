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

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient_SetsBaseAndToken(t *testing.T) {
	t.Helper()
	c := NewClient("http://localhost:8080", "test-token")
	defer c.Close()
	if c.base != "http://localhost:8080" {
		t.Errorf("expected base http://localhost:8080, got %s", c.base)
	}
	if c.token != "test-token" {
		t.Errorf("expected token test-token, got %s", c.token)
	}
}

func TestHealth_ReturnsTrue(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.Error(w, "not found", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	defer c.Close()
	ok, err := c.Health()
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !ok {
		t.Fatal("expected healthy")
	}
}

func TestHealth_ReturnsFalseOnDegraded(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "degraded"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	defer c.Close()
	ok, err := c.Health()
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if ok {
		t.Fatal("expected not healthy")
	}
}

func TestStatus_AuthHeader(t *testing.T) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "version": "0.1.3"})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "my-secret")
	defer c.Close()
	_, _ = c.Status()
	if gotAuth != "Bearer my-secret" {
		t.Fatalf("expected Bearer my-secret, got %q", gotAuth)
	}
}

func TestGet_Non200ReturnsError(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	defer c.Close()
	_, err := c.Status()
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
}

func TestReady_ReturnsTrue(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")
	defer c.Close()
	ok, err := c.Ready()
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if !ok {
		t.Fatal("expected ready")
	}
}

func TestReady_ReturnsFalse(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "not_ready"})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")
	defer c.Close()
	ok, _ := c.Ready()
	if ok {
		t.Fatal("expected not ready")
	}
}

func TestLint_ReturnsFindings(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/lint" {
			http.Error(w, "not found", 404)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"findings": []map[string]string{{"rule": "test"}}})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")
	defer c.Close()
	resp, err := c.Lint()
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if len(resp.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(resp.Findings))
	}
}

func TestSecurityEvents_PassesLimit(t *testing.T) {
	t.Helper()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		json.NewEncoder(w).Encode(map[string]interface{}{"events": []interface{}{}})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")
	defer c.Close()
	_, _ = c.SecurityEvents(50)
	if gotPath != "/api/security/events?limit=50" {
		t.Fatalf("expected limit=50 in path, got %s", gotPath)
	}
}

func TestAuditLog_ReturnsRecords(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"records": []interface{}{}, "total_matching": 0})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")
	defer c.Close()
	resp, err := c.AuditLog(100)
	if err != nil {
		t.Fatalf("AuditLog: %v", err)
	}
	if resp.Records == nil {
		t.Fatal("expected non-nil records")
	}
}

func TestConfig_ReturnsConfig(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"daemon": map[string]interface{}{"port": 8080}})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")
	defer c.Close()
	resp, err := c.Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if resp.Daemon.Port != 8080 {
		t.Fatalf("expected port 8080, got %d", resp.Daemon.Port)
	}
}

func TestConflicts_WithLimit(t *testing.T) {
	t.Helper()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		json.NewEncoder(w).Encode(map[string]interface{}{"groups": []interface{}{}})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")
	defer c.Close()
	_, _ = c.Conflicts(ConflictOpts{Limit: 10})
	if gotPath != "/api/conflicts?limit=10" {
		t.Fatalf("expected limit=10, got %s", gotPath)
	}
}

func TestTimeTravel_QueryParams(t *testing.T) {
	t.Helper()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		json.NewEncoder(w).Encode(map[string]interface{}{"records": []interface{}{}})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "tok")
	defer c.Close()
	_, _ = c.TimeTravel(TimeTravelOpts{Subject: "test", Limit: 5})
	if gotPath == "/api/timetravel" {
		t.Fatal("expected query params in path")
	}
}

func TestNewClient_withToken(t *testing.T) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(StatusResponse{Status: "ok", Version: "0.1.3"})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "secret-token")
	defer c.Close()
	_, _ = c.Status()
	if gotAuth != "Bearer secret-token" {
		t.Errorf("expected Authorization: Bearer secret-token, got %q", gotAuth)
	}
}

func TestNewClient_withoutToken(t *testing.T) {
	t.Helper()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "")
	defer c.Close()
	_, _ = c.Health()
	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

func TestAddAuth_onlyAPIPaths(t *testing.T) {
	t.Helper()
	c := NewClient("http://localhost", "tok")
	defer c.Close()

	tests := []struct {
		path     string
		wantAuth bool
	}{
		{"/api/status", true},
		{"/api/config", true},
		{"/api/security/events", true},
		{"/health", false},
		{"/ready", false},
		{"/stream/retrieval", false},
		{"/oauth/jwks", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "http://localhost"+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			c.addAuth(req)
			got := req.Header.Get("Authorization")
			if tt.wantAuth && got == "" {
				t.Errorf("path %s: expected auth header, got none", tt.path)
			}
			if !tt.wantAuth && got != "" {
				t.Errorf("path %s: expected no auth header, got %q", tt.path, got)
			}
		})
	}
}

func TestResolveAdminToken_priority(t *testing.T) {
	t.Helper()
	tests := []struct {
		name    string
		cliFlag string
		envVal  string
		want    string
	}{
		{"cli beats env", "cli-tok", "env-tok", "cli-tok"},
		{"env when no cli", "", "env-tok", "env-tok"},
		{"empty when neither", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvAdminToken, tt.envVal)
			got := ResolveAdminToken(tt.cliFlag)
			if got != tt.want {
				t.Errorf("ResolveAdminToken(%q) = %q, want %q", tt.cliFlag, got, tt.want)
			}
		})
	}
}

func TestResolveBaseURL_priority(t *testing.T) {
	t.Helper()
	tests := []struct {
		name    string
		cliFlag string
		envVal  string
		want    string
	}{
		{"cli beats env", "http://cli:9000", "http://env:9001", "http://cli:9000"},
		{"env when no cli", "", "http://env:9001", "http://env:9001"},
		{"default when neither", "", "", DefaultAPIURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvAPIURL, tt.envVal)
			got := ResolveBaseURL(tt.cliFlag)
			if got != tt.want {
				t.Errorf("ResolveBaseURL(%q) = %q, want %q", tt.cliFlag, got, tt.want)
			}
		})
	}
}
