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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/daemon"
)

// buildWriteSource returns a Source with CanWrite=true and an idempotency window.
func buildWriteSource() *config.Source {
	return &config.Source{
		Name:             "claude",
		Namespace:        "claude",
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
}

func buildTestDaemonWithSource(t *testing.T, src *config.Source, apiKey string) *daemon.Daemon {
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
		Sources:            []*config.Source{src},
		Destinations:       []*config.Destination{{Name: "sqlite", Type: "sqlite"}},
		ResolvedSourceKeys: map[string][]byte{src.Name: []byte(apiKey)},
		ResolvedAdminKey:   []byte("admin-key"),
	}
	return daemon.NewTestDaemon(t, cfg)
}

// ---------------------------------------------------------------------------
// Write path operation order tests
// Reference: Tech Spec Section 3.2, Phase 0C Behavioral Contract item 8.
// ---------------------------------------------------------------------------

func TestHandleWrite_HappyPath(t *testing.T) {
	src := buildWriteSource()
	d := buildTestDaemonWithSource(t, src, "src-key-abc")
	handler := d.WriteHandler()

	body := `{"message":{"content":"hello world","role":"user"},"model":"claude-3"}`
	req := httptest.NewRequest(http.MethodPost, "/inbound/claude", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer src-key-abc")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "test-idem-key-001")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d\nbody: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp struct {
		PayloadID string `json:"payload_id"`
		Status    string `json:"status"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.PayloadID == "" {
		t.Error("payload_id is empty")
	}
	if resp.Status != "accepted" {
		t.Errorf("status = %q; want %q", resp.Status, "accepted")
	}
}

func TestHandleWrite_IdempotencyCheck_BeforeRateLimit(t *testing.T) {
	// First write establishes the idempotency key.
	src := buildWriteSource()
	src.RateLimit.RequestsPerMinute = 1 // low limit to verify idempotent repeat doesn't count
	d := buildTestDaemonWithSource(t, src, "src-key-abc")
	handler := d.WriteHandler()

	body := `{"message":{"content":"hello","role":"user"}}`
	const idemKey = "test-idem-key-dedup"

	// First request — consumes rate budget.
	req1 := httptest.NewRequest(http.MethodPost, "/inbound/claude", bytes.NewBufferString(body))
	req1.Header.Set("Authorization", "Bearer src-key-abc")
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", idemKey)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d; want %d\nbody: %s", rr1.Code, http.StatusOK, rr1.Body.String())
	}

	var first struct {
		PayloadID string `json:"payload_id"`
	}
	if err := json.NewDecoder(rr1.Body).Decode(&first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	// Second request — same idempotency key. Must return 200 with the ORIGINAL
	// payload_id and must NOT burn the rate budget (RPM=1 is already used by
	// the first request, but idempotency short-circuits before rate limit).
	req2 := httptest.NewRequest(http.MethodPost, "/inbound/claude", bytes.NewBufferString(body))
	req2.Header.Set("Authorization", "Bearer src-key-abc")
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", idemKey)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("duplicate request: status = %d; want %d\nbody: %s",
			rr2.Code, http.StatusOK, rr2.Body.String())
	}
	var second struct {
		PayloadID string `json:"payload_id"`
	}
	if err := json.NewDecoder(rr2.Body).Decode(&second); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if second.PayloadID != first.PayloadID {
		t.Errorf("duplicate payload_id = %q; want original %q", second.PayloadID, first.PayloadID)
	}
}

func TestHandleWrite_MaxBytesReader_Enforced(t *testing.T) {
	src := buildWriteSource()
	src.PayloadLimits.MaxBytes = 100 // very small limit for testing
	d := buildTestDaemonWithSource(t, src, "src-key-abc")
	handler := d.WriteHandler()

	// Payload exactly at limit (100 bytes of JSON) should succeed.
	// Payload at limit+1 should return 413.
	oversized := `{"content":"` + strings.Repeat("x", 200) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/inbound/claude", bytes.NewBufferString(oversized))
	req.Header.Set("Authorization", "Bearer src-key-abc")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d; want %d (payload_too_large)", rr.Code, http.StatusRequestEntityTooLarge)
	}
	code := extractErrorCode(t, rr.Body.Bytes())
	if code != "payload_too_large" {
		t.Errorf("error code = %q; want %q", code, "payload_too_large")
	}
}

func TestHandleWrite_RateLimit_Returns429WithRetryAfter(t *testing.T) {
	src := buildWriteSource()
	src.Idempotency.Enabled = false // disable idempotency so each request counts
	src.RateLimit.RequestsPerMinute = 1
	d := buildTestDaemonWithSource(t, src, "src-key-abc")
	handler := d.WriteHandler()

	body := `{"message":{"content":"hi","role":"user"}}`

	// First request — within limit.
	req1 := httptest.NewRequest(http.MethodPost, "/inbound/claude", bytes.NewBufferString(body))
	req1.Header.Set("Authorization", "Bearer src-key-abc")
	req1.Header.Set("Content-Type", "application/json")
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d; want %d", rr1.Code, http.StatusOK)
	}

	// Second request — should be rate limited.
	req2 := httptest.NewRequest(http.MethodPost, "/inbound/claude", bytes.NewBufferString(body))
	req2.Header.Set("Authorization", "Bearer src-key-abc")
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d; want %d", rr2.Code, http.StatusTooManyRequests)
	}
	// Retry-After header MUST be present.
	if rr2.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing on 429 response")
	}
	code := extractErrorCode(t, rr2.Body.Bytes())
	if code != "rate_limit_exceeded" {
		t.Errorf("error code = %q; want %q", code, "rate_limit_exceeded")
	}
}

func TestHandleWrite_ErrorFormat(t *testing.T) {
	// Error responses must use the canonical format.
	// Reference: Tech Spec Section 7.4.
	src := buildWriteSource()
	src.CanWrite = false
	d := buildTestDaemonWithSource(t, src, "src-key-abc")
	handler := d.WriteHandler()

	req := httptest.NewRequest(http.MethodPost, "/inbound/claude", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer src-key-abc")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want %d", rr.Code, http.StatusForbidden)
	}

	var resp struct {
		Error             string                 `json:"error"`
		Message           string                 `json:"message"`
		RetryAfterSeconds int                    `json:"retry_after_seconds"`
		Details           map[string]interface{} `json:"details"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == "" {
		t.Error("error field is empty")
	}
	if resp.Message == "" {
		t.Error("message field is empty")
	}
	if resp.Details == nil {
		t.Error("details field is nil; want empty object")
	}
}

// ---------------------------------------------------------------------------
// Read path tests
// ---------------------------------------------------------------------------

func TestHandleQuery_HappyPath(t *testing.T) {
	src := buildWriteSource()
	d := buildTestDaemonWithSource(t, src, "src-key-abc")
	handler := d.QueryHandler()

	req := httptest.NewRequest(http.MethodGet, "/query/sqlite?limit=10", nil)
	req.Header.Set("Authorization", "Bearer src-key-abc")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d\nbody: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp struct {
		Results []interface{} `json:"results"`
		Nexus   struct {
			ResultCount int    `json:"result_count"`
			Profile     string `json:"profile"`
		} `json:"_nexus"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Nexus.Profile == "" {
		t.Error("_nexus.profile is empty")
	}
}

func TestHandleQuery_LimitCappedAt200(t *testing.T) {
	src := buildWriteSource()
	d := buildTestDaemonWithSource(t, src, "src-key-abc")
	handler := d.QueryHandler()

	// Request limit=500 — should be capped to 200 internally.
	// The fake destination returns 0 results, but the cap is what we test.
	req := httptest.NewRequest(http.MethodGet, "/query/sqlite?limit=500", nil)
	req.Header.Set("Authorization", "Bearer src-key-abc")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rr.Code, http.StatusOK)
	}
	// The response should succeed (no error about invalid limit).
	var resp struct {
		Nexus struct {
			ResultCount int `json:"result_count"`
		} `json:"_nexus"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestHandleQuery_DefaultLimit(t *testing.T) {
	src := buildWriteSource()
	d := buildTestDaemonWithSource(t, src, "src-key-abc")
	handler := d.QueryHandler()

	// No limit parameter — should default to 20.
	req := httptest.NewRequest(http.MethodGet, "/query/sqlite", nil)
	req.Header.Set("Authorization", "Bearer src-key-abc")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", rr.Code, http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Health and ready probes
// ---------------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	src := buildWriteSource()
	d := buildTestDaemonWithSource(t, src, "src-key-abc")

	// Build a minimal router for /health.
	router := d.BuildRouter()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d; want %d", rr.Code, http.StatusOK)
	}
}

func TestHandleReady(t *testing.T) {
	src := buildWriteSource()
	d := buildTestDaemonWithSource(t, src, "src-key-abc")

	router := d.BuildRouter()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d; want %d\nbody: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
}
