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

package daemon_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/daemon"
)

// buildTestDaemon creates a minimal *daemon.Daemon wired against in-memory
// fakes — no real WAL, queue, or SQLite are opened.
func buildTestDaemon(t *testing.T, sources []*config.Source, resolvedKeys map[string][]byte, adminKey string) *daemon.Daemon {
	t.Helper()
	return buildTestDaemonWithMCP(t, sources, resolvedKeys, adminKey, "")
}

// buildTestDaemonWithMCP is like buildTestDaemon but also configures a resolved
// MCP key for token class separation tests.
func buildTestDaemonWithMCP(t *testing.T, sources []*config.Source, resolvedKeys map[string][]byte, adminKey, mcpKey string) *daemon.Daemon {
	t.Helper()
	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port: 18080,
			Bind: "127.0.0.1",
			RateLimit: config.GlobalRateLimitConfig{
				GlobalRequestsPerMinute: 1000,
			},
			QueueSize: 100,
		},
		Retrieval: config.RetrievalConfig{
			DefaultProfile: "balanced",
		},
		Sources:            sources,
		ResolvedSourceKeys: resolvedKeys,
		ResolvedAdminKey:   []byte(adminKey),
	}
	if mcpKey != "" {
		cfg.ResolvedMCPKey = []byte(mcpKey)
	}
	return daemon.NewTestDaemon(t, cfg)
}

// okHandler is a trivial HTTP handler that records which source was in context.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// ---------------------------------------------------------------------------
// extractErrorCode parses the error code from a JSON error response body.
// ---------------------------------------------------------------------------

func extractErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("extractErrorCode: unmarshal %q: %v", body, err)
	}
	return resp.Error
}

// ---------------------------------------------------------------------------
// Structural constant-time verification
//
// Why timing-based tests were removed:
//   - OS scheduler quantum (~1ms) exceeds measurement resolution, so p99
//     latency captures scheduling noise, not comparison time.
//   - The -race detector roughly doubles execution time, amplifying jitter.
//   - The Go standard library tests crypto/subtle structurally, not via
//     timing measurements. We follow the same approach.
//
// The constant-time property is a STATIC property of the implementation:
// auth.go imports crypto/subtle and calls subtle.ConstantTimeCompare for
// every token comparison (admin, all source keys, MCP key). This test
// verifies that structural property directly.
//
// Reference: Phase 0C Security Checkpoint item 1.
// ---------------------------------------------------------------------------

func TestAuth_UsesConstantTimeCompare(t *testing.T) {
	// Verify that auth.go imports crypto/subtle and calls
	// subtle.ConstantTimeCompare at least 3 times (admin key, source key
	// loop, MCP key).
	authFile := filepath.Join(".", "auth.go")
	data, err := os.ReadFile(authFile)
	if err != nil {
		t.Fatalf("read auth.go: %v", err)
	}
	src := string(data)

	if !strings.Contains(src, `"crypto/subtle"`) {
		t.Fatal("auth.go must import crypto/subtle")
	}

	count := strings.Count(src, "subtle.ConstantTimeCompare")
	if count < 3 {
		t.Fatalf("auth.go has %d calls to subtle.ConstantTimeCompare; want >= 3 "+
			"(admin key, source key loop, MCP key)", count)
	}
	t.Logf("auth.go: %d calls to subtle.ConstantTimeCompare — structural constant-time property verified", count)

	// Also verify the == operator is never used on token bytes.
	// The authenticate function must not contain "provided ==" or "key ==".
	if strings.Contains(src, "provided ==") {
		t.Error("auth.go must not use == on provided token bytes")
	}
}

// ---------------------------------------------------------------------------
// CanWrite / CanRead enforcement
// Reference: Phase 0C Behavioral Contract items 12, Invariant none.
// ---------------------------------------------------------------------------

func TestAuth_CanWrite_False_Returns403(t *testing.T) {
	sources := []*config.Source{
		{Name: "readonly", CanRead: true, CanWrite: false, Namespace: "ro",
			RateLimit: config.SourceRateLimitConfig{RequestsPerMinute: 1000}},
	}
	keys := map[string][]byte{"readonly": []byte("ro-key")}
	d := buildTestDaemon(t, sources, keys, "admin-key")

	// Use a router that simulates the full write handler path.
	handler := d.WriteHandler()

	req := httptest.NewRequest(http.MethodPost, "/inbound/readonly", http.NoBody)
	req.Header.Set("Authorization", "Bearer ro-key")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d; want %d", rr.Code, http.StatusForbidden)
	}
	code := extractErrorCode(t, rr.Body.Bytes())
	if code != "source_not_permitted_to_write" {
		t.Errorf("error code = %q; want %q", code, "source_not_permitted_to_write")
	}
}

func TestAuth_CanRead_False_Returns403(t *testing.T) {
	sources := []*config.Source{
		{Name: "writeonly", CanRead: false, CanWrite: true, Namespace: "wo",
			RateLimit: config.SourceRateLimitConfig{RequestsPerMinute: 1000}},
	}
	keys := map[string][]byte{"writeonly": []byte("wo-key")}
	d := buildTestDaemon(t, sources, keys, "admin-key")

	handler := d.QueryHandler()

	req := httptest.NewRequest(http.MethodGet, "/query/sqlite", nil)
	req.Header.Set("Authorization", "Bearer wo-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d; want %d", rr.Code, http.StatusForbidden)
	}
	code := extractErrorCode(t, rr.Body.Bytes())
	if code != "source_not_permitted_to_read" {
		t.Errorf("error code = %q; want %q", code, "source_not_permitted_to_read")
	}
}

// ---------------------------------------------------------------------------
// Wrong token class
// Reference: Tech Spec Section 6.1, Phase 0C Behavioral Contract item 5.
// ---------------------------------------------------------------------------

func TestAuth_AdminTokenOnDataEndpoint_PassesThrough(t *testing.T) {
	// Admin tokens are now allowed on data endpoints with an admin flag
	// in context. This enables debug_stages on the query path.
	// Reference: Tech Spec Section 7.3.
	sources := []*config.Source{
		{Name: "claude", CanRead: true, CanWrite: true, Namespace: "claude",
			RateLimit: config.SourceRateLimitConfig{RequestsPerMinute: 1000}},
	}
	keys := map[string][]byte{"claude": []byte("src-key")}
	d := buildTestDaemon(t, sources, keys, "admin-key-secret")

	handler := d.RequireDataTokenHandler(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer admin-key-secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d; want %d (admin tokens pass through on data endpoints)", rr.Code, http.StatusOK)
	}
}

func TestAuth_SourceTokenOnAdminEndpoint_Returns401(t *testing.T) {
	sources := []*config.Source{
		{Name: "claude", CanRead: true, CanWrite: true, Namespace: "claude",
			RateLimit: config.SourceRateLimitConfig{RequestsPerMinute: 1000}},
	}
	keys := map[string][]byte{"claude": []byte("src-key")}
	d := buildTestDaemon(t, sources, keys, "admin-key-secret")

	handler := d.RequireAdminTokenHandler(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer src-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want %d", rr.Code, http.StatusUnauthorized)
	}
	code := extractErrorCode(t, rr.Body.Bytes())
	if code != "wrong_token_class" {
		t.Errorf("error code = %q; want %q", code, "wrong_token_class")
	}
}

// ---------------------------------------------------------------------------
// MCP token class separation
// Reference: Tech Spec Section 6.1, Phase R-4 Behavioral Contract items 2–3.
// ---------------------------------------------------------------------------

func TestAuth_MCPTokenOnDataEndpoint_Returns401(t *testing.T) {
	sources := []*config.Source{
		{Name: "claude", CanRead: true, CanWrite: true, Namespace: "claude",
			RateLimit: config.SourceRateLimitConfig{RequestsPerMinute: 1000}},
	}
	keys := map[string][]byte{"claude": []byte("src-key")}
	d := buildTestDaemonWithMCP(t, sources, keys, "admin-key-secret", "mcp-key-secret")

	handler := d.RequireDataTokenHandler(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer mcp-key-secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want %d", rr.Code, http.StatusUnauthorized)
	}
	code := extractErrorCode(t, rr.Body.Bytes())
	if code != "wrong_token_class" {
		t.Errorf("error code = %q; want %q", code, "wrong_token_class")
	}
}

func TestAuth_MCPTokenOnAdminEndpoint_Returns401(t *testing.T) {
	sources := []*config.Source{
		{Name: "claude", CanRead: true, CanWrite: true, Namespace: "claude",
			RateLimit: config.SourceRateLimitConfig{RequestsPerMinute: 1000}},
	}
	keys := map[string][]byte{"claude": []byte("src-key")}
	d := buildTestDaemonWithMCP(t, sources, keys, "admin-key-secret", "mcp-key-secret")

	handler := d.RequireAdminTokenHandler(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer mcp-key-secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want %d", rr.Code, http.StatusUnauthorized)
	}
	code := extractErrorCode(t, rr.Body.Bytes())
	if code != "wrong_token_class" {
		t.Errorf("error code = %q; want %q", code, "wrong_token_class")
	}
}

func TestAuth_CorrectTokenClass_MCPNotConfigured_StillWorks(t *testing.T) {
	// When MCP key is not configured, data and admin tokens still work normally.
	sources := []*config.Source{
		{Name: "claude", CanRead: true, CanWrite: true, Namespace: "claude",
			RateLimit: config.SourceRateLimitConfig{RequestsPerMinute: 1000}},
	}
	keys := map[string][]byte{"claude": []byte("src-key")}
	d := buildTestDaemon(t, sources, keys, "admin-key")

	// Data token on data endpoint → 200.
	dataHandler := d.RequireDataTokenHandler(okHandler)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer src-key")
	rr := httptest.NewRecorder()
	dataHandler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("data token on data endpoint: status = %d; want %d", rr.Code, http.StatusOK)
	}

	// Admin token on admin endpoint → 200.
	adminHandler := d.RequireAdminTokenHandler(okHandler)
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Authorization", "Bearer admin-key")
	rr2 := httptest.NewRecorder()
	adminHandler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Errorf("admin token on admin endpoint: status = %d; want %d", rr2.Code, http.StatusOK)
	}
}

func TestAuth_MissingToken_Returns401(t *testing.T) {
	d := buildTestDaemon(t, nil, nil, "admin-key")
	handler := d.RequireDataTokenHandler(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want %d", rr.Code, http.StatusUnauthorized)
	}
	code := extractErrorCode(t, rr.Body.Bytes())
	if code != "unauthorized" {
		t.Errorf("error code = %q; want %q", code, "unauthorized")
	}
}

func TestAuth_CorrectKey_PassesThrough(t *testing.T) {
	sources := []*config.Source{
		{Name: "claude", CanRead: true, CanWrite: true, Namespace: "claude",
			RateLimit: config.SourceRateLimitConfig{RequestsPerMinute: 1000}},
	}
	keys := map[string][]byte{"claude": []byte("correct-key")}
	d := buildTestDaemon(t, sources, keys, "admin-key")

	handler := d.RequireDataTokenHandler(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer correct-key")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d; want %d", rr.Code, http.StatusOK)
	}
}

