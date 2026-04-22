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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_PreflightReturns204(t *testing.T) {
	t.Helper()
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called on OPTIONS preflight")
	}))
	req := httptest.NewRequest(http.MethodOptions, "/api/status", nil)
	req.Header.Set("Origin", "http://localhost:8081")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://localhost:8081" {
		t.Fatalf("expected Allow-Origin to match request origin, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("expected Allow-Credentials: true")
	}
}

func TestCORS_LocalhostOriginAllowed(t *testing.T) {
	t.Helper()
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://localhost:8081")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://localhost:8081" {
		t.Fatalf("expected localhost origin allowed, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_127001OriginAllowed(t *testing.T) {
	t.Helper()
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:3000" {
		t.Fatalf("expected 127.0.0.1 origin allowed, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_ExternalOriginRejected(t *testing.T) {
	t.Helper()
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no Allow-Origin for external origin, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_NoOriginNoHeaders(t *testing.T) {
	t.Helper()
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no CORS headers when no Origin present")
	}
}

func TestIsAllowedOrigin(t *testing.T) {
	t.Helper()
	tests := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:8081", true},
		{"http://localhost:3000", true},
		{"http://127.0.0.1:8080", true},
		{"https://LOCALHOST:443", true},
		{"https://evil.com", false},
		{"", false},
		{"http://example.com", false},
	}
	for _, tt := range tests {
		got := isAllowedOrigin(tt.origin)
		if got != tt.want {
			t.Errorf("isAllowedOrigin(%q) = %v, want %v", tt.origin, got, tt.want)
		}
	}
}
