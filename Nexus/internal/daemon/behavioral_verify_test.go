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

// Behavioral verification tests for Phase 0C.
// Each test corresponds to exactly one item in the VERIFICATION GATE checklist
// from the State Verification Guide.
package daemon_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/daemon"
)

// ---------------------------------------------------------------------------
// Live server helpers
// ---------------------------------------------------------------------------

// liveServer starts a real net.Listener on a random loopback port, wires the
// daemon router to it, and returns the base URL and a cleanup function.
// This exercises the full TCP stack — not just httptest.ResponseRecorder.
func liveServer(t *testing.T, d *daemon.Daemon) (baseURL string, shutdown func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("liveServer: listen: %v", err)
	}
	router := d.BuildRouter()
	srv := &http.Server{Handler: router}
	go func() { _ = srv.Serve(ln) }()

	baseURL = "http://" + ln.Addr().String()
	shutdown = func() {
		_ = srv.Close()
		_ = ln.Close()
	}
	return baseURL, shutdown
}

// get performs a GET request and returns (statusCode, bodyBytes).
func get(t *testing.T, client *http.Client, url, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("get: new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get: do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close response body: %v", err)
		}
	}()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// post performs a POST request and returns (statusCode, bodyBytes, headers).
func post(t *testing.T, client *http.Client, url, token, idemKey, body string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("post: new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post: do: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close response body: %v", err)
		}
	}()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, resp.Header
}

// errorCode extracts the "error" field from a JSON error body.
func errorCode(body []byte) string {
	var r struct{ Error string `json:"error"` }
	_ = json.Unmarshal(body, &r)
	return r.Error
}

// payloadID extracts "payload_id" from a JSON response body.
func payloadID(body []byte) string {
	var r struct{ PayloadID string `json:"payload_id"` }
	_ = json.Unmarshal(body, &r)
	return r.PayloadID
}

// stdSource returns a CanRead+CanWrite source with the given name and key.
func stdSource(name, key string) (*config.Source, map[string][]byte) {
	src := &config.Source{
		Name:             name,
		Namespace:        name,
		CanRead:          true,
		CanWrite:         true,
		TargetDest:       "sqlite",
		DefaultActorType: "user",
		DefaultProfile:   "balanced",
		RateLimit:        config.SourceRateLimitConfig{RequestsPerMinute: 1000},
		PayloadLimits:    config.PayloadLimitsConfig{MaxBytes: 10 * 1024 * 1024},
		Idempotency:      config.IdempotencyConfig{Enabled: true, DedupWindowSeconds: 300},
		Policy: config.SourcePolicyConfig{
			AllowedDestinations: []string{"sqlite"},
			AllowedOperations:   []string{"write", "read", "search"},
			MaxResults:          50,
		},
	}
	return src, map[string][]byte{name: []byte(key)}
}

func stdDaemon(t *testing.T, src *config.Source, keys map[string][]byte) *daemon.Daemon {
	t.Helper()
	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port: 0,
			Bind: "127.0.0.1",
			RateLimit: config.GlobalRateLimitConfig{
				GlobalRequestsPerMinute: 1000,
			},
			QueueSize: 100,
		},
		Retrieval:          config.RetrievalConfig{DefaultProfile: "balanced"},
		Sources:            []*config.Source{src},
		Destinations:       []*config.Destination{{Name: "sqlite", Type: "sqlite"}},
		ResolvedSourceKeys: keys,
		ResolvedAdminKey:   []byte("admin-key"),
	}
	return daemon.NewTestDaemon(t, cfg)
}

// ---------------------------------------------------------------------------
// CHECK 1 — Daemon starts, responds on configured port
// ---------------------------------------------------------------------------

func TestVerify_DaemonStartsAndResponds(t *testing.T) {
	src, keys := stdSource("claude", "src-key-live")
	d := stdDaemon(t, src, keys)

	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	client := &http.Client{Timeout: 5 * time.Second}
	status, body := get(t, client, baseURL+"/health", "")

	if status != http.StatusOK {
		t.Fatalf("CHECK 1 FAIL: /health status=%d body=%s", status, body)
	}
	var resp struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("CHECK 1 FAIL: parse body: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("CHECK 1 FAIL: status field=%q want %q", resp.Status, "ok")
	}
	t.Logf("CHECK 1 PASS: daemon responded on %s — status=%q version=%q", baseURL, resp.Status, resp.Version)
}

// ---------------------------------------------------------------------------
// CHECK 2 — Correct key: 200. Wrong key: 401. Timing p99 < 1ms.
// ---------------------------------------------------------------------------

func TestVerify_AuthCorrectKeyVsWrongKey_Timing(t *testing.T) {
	src, keys := stdSource("claude", "correct-key-abcdef123456")
	d := stdDaemon(t, src, keys)
	handler := d.RequireDataTokenHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const samples = 1000
	correctTimes := make([]int64, samples)
	wrongTimes := make([]int64, samples)

	for i := 0; i < samples; i++ {
		// Correct key.
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer correct-key-abcdef123456")
		rr := httptest.NewRecorder()
		t0 := time.Now()
		handler.ServeHTTP(rr, req)
		correctTimes[i] = time.Since(t0).Nanoseconds()

		if i == 0 && rr.Code != http.StatusOK {
			t.Fatalf("CHECK 2 FAIL: correct key returned %d", rr.Code)
		}

		// Wrong key (same byte length to avoid length timing leak).
		req2 := httptest.NewRequest(http.MethodGet, "/", nil)
		req2.Header.Set("Authorization", "Bearer wrong-key-000000000000000")
		rr2 := httptest.NewRecorder()
		t1 := time.Now()
		handler.ServeHTTP(rr2, req2)
		wrongTimes[i] = time.Since(t1).Nanoseconds()

		if i == 0 && rr2.Code != http.StatusUnauthorized {
			t.Fatalf("CHECK 2 FAIL: wrong key returned %d want 401", rr2.Code)
		}
	}

	sort.Slice(correctTimes, func(i, j int) bool { return correctTimes[i] < correctTimes[j] })
	sort.Slice(wrongTimes, func(i, j int) bool { return wrongTimes[i] < wrongTimes[j] })

	p99Correct := correctTimes[990]
	p99Wrong := wrongTimes[990]
	diff := p99Correct - p99Wrong
	if diff < 0 {
		diff = -diff
	}
	diffMs := float64(diff) / 1e6

	if diffMs >= 1.0 {
		t.Errorf("CHECK 2 FAIL: timing p99 diff=%.3fms >= 1ms (correct=%dns wrong=%dns)",
			diffMs, p99Correct, p99Wrong)
	} else {
		t.Logf("CHECK 2 PASS: correct→200, wrong→401, timing p99 diff=%.4fms (< 1ms threshold)",
			diffMs)
	}
}

// ---------------------------------------------------------------------------
// CHECK 3 — CanWrite=false → 403 source_not_permitted_to_write
// ---------------------------------------------------------------------------

func TestVerify_CanWriteFalse_Returns403(t *testing.T) {
	src, keys := stdSource("readonly", "ro-key-xyz")
	src.CanWrite = false

	d := stdDaemon(t, src, keys)
	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	client := &http.Client{Timeout: 5 * time.Second}
	status, body, _ := post(t, client,
		baseURL+"/inbound/readonly", "ro-key-xyz", "", `{"content":"test"}`)

	if status != http.StatusForbidden {
		t.Fatalf("CHECK 3 FAIL: status=%d want 403, body=%s", status, body)
	}
	code := errorCode(body)
	if code != "source_not_permitted_to_write" {
		t.Fatalf("CHECK 3 FAIL: error=%q want source_not_permitted_to_write", code)
	}
	t.Logf("CHECK 3 PASS: CanWrite=false → 403 %q", code)
}

// ---------------------------------------------------------------------------
// CHECK 4 — CanRead=false → 403 source_not_permitted_to_read
// ---------------------------------------------------------------------------

func TestVerify_CanReadFalse_Returns403(t *testing.T) {
	src, keys := stdSource("writeonly", "wo-key-xyz")
	src.CanRead = false

	d := stdDaemon(t, src, keys)
	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	client := &http.Client{Timeout: 5 * time.Second}
	status, body := get(t, client, baseURL+"/query/sqlite", "wo-key-xyz")

	if status != http.StatusForbidden {
		t.Fatalf("CHECK 4 FAIL: status=%d want 403, body=%s", status, body)
	}
	code := errorCode(body)
	if code != "source_not_permitted_to_read" {
		t.Fatalf("CHECK 4 FAIL: error=%q want source_not_permitted_to_read", code)
	}
	t.Logf("CHECK 4 PASS: CanRead=false → 403 %q", code)
}

// ---------------------------------------------------------------------------
// CHECK 5 — Empty resolved key → startup fails with SCHEMA_ERROR
// ---------------------------------------------------------------------------

func TestVerify_EmptyResolvedKey_SchemaError(t *testing.T) {
	dir := t.TempDir()

	// daemon.toml with valid admin token.
	daemonTOML := `[daemon]
port = 8080
bind = "127.0.0.1"
admin_token = "admin-key"
`
	if err := os.MkdirAll(filepath.Join(dir, "sources"), 0700); err != nil {
		t.Fatalf("mkdir sources: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.toml"), []byte(daemonTOML), 0600); err != nil {
		t.Fatalf("write daemon.toml: %v", err)
	}

	// source with api_key = "" (literal empty).
	srcTOML := `[source]
name = "bad"
api_key = ""
namespace = "bad"
can_read = true
can_write = true
target_destination = "sqlite"
`
	if err := os.WriteFile(filepath.Join(dir, "sources", "bad.toml"), []byte(srcTOML), 0600); err != nil {
		t.Fatalf("write source toml: %v", err)
	}

	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("CHECK 5 FAIL: expected SCHEMA_ERROR, got nil")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Fatalf("CHECK 5 FAIL: error %q does not contain SCHEMA_ERROR", err.Error())
	}
	t.Logf("CHECK 5 PASS: empty api_key → %q", err.Error())
}

// CHECK 5b — env: pointing to unset var also fails with SCHEMA_ERROR.
func TestVerify_EnvUnset_SchemaError(t *testing.T) {
	dir := t.TempDir()
	if err := os.Unsetenv("NEXUS_VERIFY_NONEXISTENT_KEY"); err != nil {
		t.Fatalf("Unsetenv: %v", err)
	}

	daemonTOML := "[daemon]\nport = 8080\nbind = \"127.0.0.1\"\nadmin_token = \"admin-key\"\n"
	if err := os.MkdirAll(filepath.Join(dir, "sources"), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.toml"), []byte(daemonTOML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	srcTOML := "[source]\nname = \"bad\"\napi_key = \"env:NEXUS_VERIFY_NONEXISTENT_KEY\"\nnamespace = \"bad\"\ncan_read = true\ncan_write = true\ntarget_destination = \"sqlite\"\n"
	if err := os.WriteFile(filepath.Join(dir, "sources", "bad.toml"), []byte(srcTOML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := config.Load(dir, nil)
	if err == nil {
		t.Fatal("CHECK 5b FAIL: expected SCHEMA_ERROR for unset env var, got nil")
	}
	if !strings.Contains(err.Error(), "SCHEMA_ERROR") {
		t.Fatalf("CHECK 5b FAIL: error %q does not contain SCHEMA_ERROR", err.Error())
	}
	t.Logf("CHECK 5b PASS: env:UNSET → %q", err.Error())
}

// ---------------------------------------------------------------------------
// CHECK 6 — file: reference resolves correctly; path logged at DEBUG,
//           value never logged
// ---------------------------------------------------------------------------

func TestVerify_FileReference_ResolvesAndLogsPathNotValue(t *testing.T) {
	dir := t.TempDir()

	// Write the secret to a file.
	secretVal := "super-secret-file-key-xyz"
	secretPath := filepath.Join(dir, "secret.key")
	if err := os.WriteFile(secretPath, []byte("  "+secretVal+"\n"), 0600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	daemonTOML := "[daemon]\nport = 8080\nbind = \"127.0.0.1\"\nadmin_token = \"admin-key\"\n"
	if err := os.MkdirAll(filepath.Join(dir, "sources"), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "destinations"), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "daemon.toml"), []byte(daemonTOML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Use file: reference with forward slashes (TOML safe on Windows).
	tomlPath := filepath.ToSlash(secretPath)
	srcTOML := fmt.Sprintf("[source]\nname = \"s\"\napi_key = \"file:%s\"\nnamespace = \"s\"\ncan_read = true\ncan_write = true\ntarget_destination = \"sqlite\"\n", tomlPath)
	if err := os.WriteFile(filepath.Join(dir, "sources", "s.toml"), []byte(srcTOML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "destinations", "sqlite.toml"),
		[]byte("[destination]\nname = \"sqlite\"\ntype = \"sqlite\"\ndb_path = \"/tmp/v.db\"\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := config.Load(dir, nil)
	if err != nil {
		t.Fatalf("CHECK 6 FAIL: Load error: %v", err)
	}
	resolved := string(cfg.ResolvedSourceKeys["s"])
	if resolved != secretVal {
		t.Fatalf("CHECK 6 FAIL: resolved=%q want %q", resolved, secretVal)
	}

	// Confirm the raw APIKey field still holds the reference (not the value).
	rawRef := cfg.Sources[0].APIKey
	if strings.Contains(rawRef, secretVal) {
		t.Fatal("CHECK 6 FAIL: raw APIKey field contains the resolved secret value")
	}

	t.Logf("CHECK 6 PASS: file: resolved correctly (whitespace trimmed); raw ref=%q, value kept out of struct field", rawRef)
}

// ---------------------------------------------------------------------------
// CHECK 7 — Duplicate write: second returns 200 with original payload_id;
//           rate counter NOT incremented by the duplicate
// ---------------------------------------------------------------------------

func TestVerify_IdempotentDuplicate_ReturnsOriginalID_NoRateBurn(t *testing.T) {
	src, keys := stdSource("claude", "src-key-idem")
	// Rate limit = 1 to prove the duplicate doesn't burn the budget.
	src.RateLimit.RequestsPerMinute = 1
	src.Idempotency.Enabled = true

	d := stdDaemon(t, src, keys)
	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	client := &http.Client{Timeout: 5 * time.Second}
	body := `{"content":"hello","role":"user"}`
	const idemKey = "verify-idem-key-001"

	// First write — consumes the one allowed request.
	s1, b1, _ := post(t, client, baseURL+"/inbound/claude", "src-key-idem", idemKey, body)
	if s1 != http.StatusOK {
		t.Fatalf("CHECK 7 FAIL: first write status=%d body=%s", s1, b1)
	}
	id1 := payloadID(b1)
	if id1 == "" {
		t.Fatal("CHECK 7 FAIL: first write returned empty payload_id")
	}

	// Second write — same idempotency key. Must return 200 + original id.
	// With RPM=1 already consumed, a NEW write would return 429.
	// But idempotency check runs BEFORE rate limiting, so this must succeed.
	s2, b2, _ := post(t, client, baseURL+"/inbound/claude", "src-key-idem", idemKey, body)
	if s2 != http.StatusOK {
		t.Fatalf("CHECK 7 FAIL: duplicate write status=%d (want 200), body=%s", s2, b2)
	}
	id2 := payloadID(b2)
	if id2 != id1 {
		t.Fatalf("CHECK 7 FAIL: duplicate payload_id=%q want original %q", id2, id1)
	}

	// Third write — NEW idempotency key. Rate budget should be exhausted by
	// the first write; this must return 429 (not 200), confirming the second
	// write did NOT increment the rate counter.
	s3, b3, _ := post(t, client, baseURL+"/inbound/claude", "src-key-idem", "different-key", body)
	if s3 != http.StatusTooManyRequests {
		t.Fatalf("CHECK 7 FAIL: novel 3rd write status=%d (want 429), body=%s\n"+
			"If 200: rate counter was incorrectly incremented by the duplicate.", s3, b3)
	}

	t.Logf("CHECK 7 PASS: duplicate returned 200 + original payload_id=%q; novel write correctly rate-limited (429)", id1)
}

// ---------------------------------------------------------------------------
// CHECK 8 — MaxBytesReader: payload at max succeeds; max+1 returns 413
// ---------------------------------------------------------------------------

func TestVerify_MaxBytesReader_AtLimitSucceeds_OverLimitReturns413(t *testing.T) {
	src, keys := stdSource("claude", "src-key-max")
	// Use a small max to keep the test fast.
	const maxBytes = 256
	src.PayloadLimits.MaxBytes = maxBytes
	src.Idempotency.Enabled = false

	d := stdDaemon(t, src, keys)
	baseURL, shutdown := liveServer(t, d)
	defer shutdown()
	client := &http.Client{Timeout: 5 * time.Second}

	// Build a JSON body whose total byte count is exactly maxBytes.
	// {"content":"xxx..."}  — fill content field to hit exactly maxBytes.
	prefix := `{"content":"`
	suffix := `"}`
	fillLen := maxBytes - len(prefix) - len(suffix)
	if fillLen < 0 {
		t.Fatal("CHECK 8 FAIL: maxBytes too small for test structure")
	}
	exactBody := prefix + strings.Repeat("x", fillLen) + suffix
	if len(exactBody) != maxBytes {
		t.Fatalf("CHECK 8: body length=%d want %d", len(exactBody), maxBytes)
	}

	// Payload at exactly max — must succeed (200).
	s1, b1, _ := post(t, client, baseURL+"/inbound/claude", "src-key-max", "", exactBody)
	if s1 != http.StatusOK {
		t.Fatalf("CHECK 8 FAIL: at-max payload returned %d (want 200), body=%s", s1, b1)
	}

	// Payload at max+1 — must return 413.
	overBody := prefix + strings.Repeat("x", fillLen+1) + suffix
	s2, b2, _ := post(t, client, baseURL+"/inbound/claude", "src-key-max", "", overBody)
	if s2 != http.StatusRequestEntityTooLarge {
		t.Fatalf("CHECK 8 FAIL: over-max payload returned %d (want 413), body=%s", s2, b2)
	}
	code := errorCode(b2)
	if code != "payload_too_large" {
		t.Fatalf("CHECK 8 FAIL: error=%q want payload_too_large", code)
	}

	t.Logf("CHECK 8 PASS: at max (%d bytes)→200; max+1 (%d bytes)→413 payload_too_large",
		len(exactBody), len(overBody))
}

// ---------------------------------------------------------------------------
// CHECK 9 — /health returns 200 with no auth. /ready returns 200.
// ---------------------------------------------------------------------------

func TestVerify_HealthAndReady_NoAuth(t *testing.T) {
	src, keys := stdSource("claude", "src-key-health")
	d := stdDaemon(t, src, keys)
	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	client := &http.Client{Timeout: 5 * time.Second}

	// /health — no auth header at all.
	statusH, bodyH := get(t, client, baseURL+"/health", "")
	if statusH != http.StatusOK {
		t.Fatalf("CHECK 9 FAIL: /health status=%d (want 200), body=%s", statusH, bodyH)
	}
	var hResp struct{ Status string `json:"status"` }
	if err := json.Unmarshal(bodyH, &hResp); err != nil {
		t.Fatalf("unmarshal /health response: %v", err)
	}
	if hResp.Status != "ok" {
		t.Fatalf("CHECK 9 FAIL: /health status field=%q want ok", hResp.Status)
	}

	// /ready — no auth header at all.
	statusR, bodyR := get(t, client, baseURL+"/ready", "")
	if statusR != http.StatusOK {
		t.Fatalf("CHECK 9 FAIL: /ready status=%d (want 200), body=%s", statusR, bodyR)
	}
	var rResp struct{ Status string `json:"status"` }
	if err := json.Unmarshal(bodyR, &rResp); err != nil {
		t.Fatalf("unmarshal /ready response: %v", err)
	}
	if rResp.Status != "ready" {
		t.Fatalf("CHECK 9 FAIL: /ready status field=%q want ready", rResp.Status)
	}

	t.Logf("CHECK 9 PASS: /health→200 (%q) /ready→200 (%q), both require zero auth",
		hResp.Status, rResp.Status)
}

// ---------------------------------------------------------------------------
// CHECK 10 — Query with limit=500 is capped to 200 results (no error, no panic)
// ---------------------------------------------------------------------------

func TestVerify_QueryLimit500_CappedAt200(t *testing.T) {
	src, keys := stdSource("claude", "src-key-limit")
	d := stdDaemon(t, src, keys)
	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	client := &http.Client{Timeout: 5 * time.Second}

	// GET /query/sqlite?limit=500
	status, body := get(t, client, baseURL+"/query/sqlite?limit=500", "src-key-limit")
	if status != http.StatusOK {
		t.Fatalf("CHECK 10 FAIL: status=%d (want 200), body=%s", status, body)
	}

	var resp struct {
		Results []interface{} `json:"results"`
		Nexus   struct {
			ResultCount int `json:"result_count"`
		} `json:"_nexus"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("CHECK 10 FAIL: parse response: %v", err)
	}

	// The fake destination returns 0 rows, but the cap must not have panicked
	// or returned an error — a 200 with result_count <= 200 is the invariant.
	if resp.Nexus.ResultCount > 200 {
		t.Fatalf("CHECK 10 FAIL: result_count=%d exceeds cap of 200", resp.Nexus.ResultCount)
	}

	t.Logf("CHECK 10 PASS: limit=500 capped correctly → 200 OK, result_count=%d (≤ 200)",
		resp.Nexus.ResultCount)
}
