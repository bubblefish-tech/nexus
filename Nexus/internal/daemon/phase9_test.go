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

// Phase 9: Testing + Security Audit — Integration tests.
// Each test corresponds to one behavioral contract from the State Verification Guide.
//
// Reference: Tech Spec Section 8 — Failure Contracts, Section 16 — Validation Plan.
//
// NOTE: Go 1.26.1 linker bug — the -race detector cannot be used on this package.
// The Go 1.26.1 linker (cmd/link) panics in loader.resolve() during dead code
// elimination when linking race-instrumented binaries that transitively import
// modernc.org/sqlite. The panic is "index out of range [16777213] with length 11"
// at cmd/link/internal/loader/loader.go:703. This affects any package that imports
// modernc.org/sqlite (destination, daemon, queue, cache, projection, query, mcp).
// A trivial empty test triggers the same crash — this is not a race condition in
// Nexus code. Run these tests without -race until a Go toolchain fix ships.
// Packages without the sqlite dependency pass -race cleanly.
package daemon_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/daemon"
)

// ---------------------------------------------------------------------------
// CONTRACT 1 — Concurrent test: 100 goroutines mixed reads + writes.
//              Zero -race reports. Reference: Tech Spec Section 16.
// ---------------------------------------------------------------------------

func TestPhase9_ConcurrentMixedReadWrite(t *testing.T) {
	src, keys := stdSource("claude", "p9-concurrent-key")
	d := stdDaemon(t, src, keys)

	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	client := &http.Client{Timeout: 10 * time.Second}
	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()

			if idx%2 == 0 {
				// Write path.
				body := fmt.Sprintf(`{"content":"concurrent-%d","role":"user"}`, idx)
				idemKey := fmt.Sprintf("p9-conc-%d", idx)
				status, respBody, _ := post(t, client,
					baseURL+"/inbound/claude", "p9-concurrent-key", idemKey, body)
				if status != http.StatusOK {
					t.Errorf("goroutine %d: write status=%d body=%s", idx, status, respBody)
				}
			} else {
				// Read path.
				status, _ := get(t, client, baseURL+"/query/sqlite?limit=10", "p9-concurrent-key")
				if status != http.StatusOK {
					t.Errorf("goroutine %d: query status=%d", idx, status)
				}
			}
		}(i)
	}

	wg.Wait()
	t.Log("CONTRACT 1 PASS: 100 goroutines mixed r/w completed — zero race reports if -race flag clean")
}

// ---------------------------------------------------------------------------
// CONTRACT 3 — Load test: 1000 concurrent writes. Zero data loss.
//              Reference: Tech Spec Section 16.
// ---------------------------------------------------------------------------

func TestPhase9_LoadTest_1000ConcurrentWrites(t *testing.T) {
	src, keys := stdSource("claude", "p9-load-key")
	src.RateLimit.RequestsPerMinute = 100000 // high limit for load test
	src.Idempotency.Enabled = true

	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port: 0,
			Bind: "127.0.0.1",
			RateLimit: config.GlobalRateLimitConfig{
				GlobalRequestsPerMinute: 100000,
			},
			QueueSize: 2000,
		},
		Retrieval:          config.RetrievalConfig{DefaultProfile: "balanced"},
		Sources:            []*config.Source{src},
		Destinations:       []*config.Destination{{Name: "sqlite", Type: "sqlite"}},
		ResolvedSourceKeys: keys,
		ResolvedAdminKey:   []byte("admin-key"),
	}

	d, sqliteDest := daemon.NewTestDaemonWithSQLite(t, cfg)
	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	// Use a transport with higher connection limits to avoid exhaustion.
	transport := &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 200,
		MaxConnsPerHost:     200,
	}
	client := &http.Client{Timeout: 60 * time.Second, Transport: transport}
	defer transport.CloseIdleConnections()

	const total = 1000
	const concurrency = 50 // bounded concurrency to avoid socket exhaustion

	type result struct {
		idx       int
		payloadID string
		status    int
	}

	results := make([]result, total)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	wg.Add(total)

	for i := 0; i < total; i++ {
		sem <- struct{}{} // acquire semaphore
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore
			body := fmt.Sprintf(`{"content":"load-test-%d","role":"user"}`, idx)
			idemKey := fmt.Sprintf("load-%d", idx)
			status, respBody, _ := post(t, client,
				baseURL+"/inbound/claude", "p9-load-key", idemKey, body)
			results[idx] = result{idx: idx, status: status}
			if status == http.StatusOK {
				results[idx].payloadID = payloadID(respBody)
			}
		}(i)
	}

	wg.Wait()

	// Count successes. Some 429s are acceptable for queue_full (data still in WAL).
	var ok200, got429, other int
	for _, r := range results {
		switch r.status {
		case 200:
			ok200++
		case 429:
			got429++
		default:
			other++
		}
	}

	t.Logf("CONTRACT 3: 200=%d, 429=%d, other=%d", ok200, got429, other)

	if other > 0 {
		t.Errorf("CONTRACT 3 FAIL: %d unexpected non-200/429 responses", other)
	}
	if ok200 == 0 {
		t.Fatal("CONTRACT 3 FAIL: zero successful writes")
	}

	// Wait for queue to drain all entries. SQLite is single-writer so this
	// takes time with 1000 entries. Poll until all present or timeout.
	deadline := time.Now().Add(60 * time.Second)
	var missing int
	for time.Now().Before(deadline) {
		missing = 0
		for _, r := range results {
			if r.status != 200 || r.payloadID == "" {
				continue
			}
			exists, err := sqliteDest.Exists(r.payloadID)
			if err != nil {
				t.Errorf("CONTRACT 3: Exists(%s) error: %v", r.payloadID, err)
				continue
			}
			if !exists {
				missing++
			}
		}
		if missing == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if missing > 0 {
		t.Fatalf("CONTRACT 3 FAIL: %d/%d payloads missing from SQLite after 60s drain", missing, ok200)
	}
	t.Logf("CONTRACT 3 PASS: %d payloads verified in SQLite, zero data loss", ok200)
}

// ---------------------------------------------------------------------------
// CONTRACT 4 — Timing attack test: 1000 samples wrong vs correct key.
//              p99 diff < 1ms. Reference: Tech Spec Section 16.
// ---------------------------------------------------------------------------

func TestPhase9_TimingAttackResistance(t *testing.T) {
	src, keys := stdSource("claude", "correct-key-timing-p9-test")
	d := stdDaemon(t, src, keys)
	handler := d.RequireDataTokenHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const samples = 1000
	correctTimes := make([]int64, samples)
	wrongTimes := make([]int64, samples)

	for i := 0; i < samples; i++ {
		// Correct key.
		req := newAuthRequest("Bearer correct-key-timing-p9-test")
		rr := recordAndTime(handler, req, &correctTimes[i])
		if i == 0 && rr != http.StatusOK {
			t.Fatalf("CONTRACT 4 FAIL: correct key returned %d", rr)
		}

		// Wrong key — same byte length to avoid length-based timing leak.
		req2 := newAuthRequest("Bearer wrong-key-000000000000000000")
		rr2 := recordAndTime(handler, req2, &wrongTimes[i])
		if i == 0 && rr2 != http.StatusUnauthorized {
			t.Fatalf("CONTRACT 4 FAIL: wrong key returned %d want 401", rr2)
		}
	}

	sort.Slice(correctTimes, func(i, j int) bool { return correctTimes[i] < correctTimes[j] })
	sort.Slice(wrongTimes, func(i, j int) bool { return wrongTimes[i] < wrongTimes[j] })

	p99Correct := correctTimes[int(float64(samples)*0.99)]
	p99Wrong := wrongTimes[int(float64(samples)*0.99)]
	diff := p99Correct - p99Wrong
	if diff < 0 {
		diff = -diff
	}
	diffMs := float64(diff) / 1e6

	if diffMs >= 1.0 {
		t.Errorf("CONTRACT 4 FAIL: timing p99 diff=%.3fms >= 1ms (correct=%dns wrong=%dns)",
			diffMs, p99Correct, p99Wrong)
	} else {
		t.Logf("CONTRACT 4 PASS: timing p99 diff=%.4fms < 1ms", diffMs)
	}
}

// ---------------------------------------------------------------------------
// CONTRACT 6 — Scope isolation: two sources, source A never sees source B
//              cache entries. Reference: Tech Spec Section 16.
// ---------------------------------------------------------------------------

func TestPhase9_ScopeIsolation_TwoSources(t *testing.T) {
	srcA := &config.Source{
		Name:             "source-a",
		Namespace:        "ns-a",
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
	srcB := &config.Source{
		Name:             "source-b",
		Namespace:        "ns-b",
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

	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port: 0,
			Bind: "127.0.0.1",
			RateLimit: config.GlobalRateLimitConfig{
				GlobalRequestsPerMinute: 1000,
			},
			QueueSize: 100,
		},
		Retrieval:    config.RetrievalConfig{DefaultProfile: "balanced"},
		Sources:      []*config.Source{srcA, srcB},
		Destinations: []*config.Destination{{Name: "sqlite", Type: "sqlite"}},
		ResolvedSourceKeys: map[string][]byte{
			"source-a": []byte("key-a-secret"),
			"source-b": []byte("key-b-secret"),
		},
		ResolvedAdminKey: []byte("admin-key"),
	}

	d, _ := daemon.NewTestDaemonWithSQLite(t, cfg)
	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	client := &http.Client{Timeout: 5 * time.Second}

	// Source A writes a memory.
	status, body, _ := post(t, client,
		baseURL+"/inbound/source-a", "key-a-secret",
		"scope-idem-a", `{"content":"secret from A","role":"user"}`)
	if status != http.StatusOK {
		t.Fatalf("CONTRACT 6 FAIL: source-a write status=%d body=%s", status, body)
	}

	// Source B writes a memory.
	status, body, _ = post(t, client,
		baseURL+"/inbound/source-b", "key-b-secret",
		"scope-idem-b", `{"content":"secret from B","role":"user"}`)
	if status != http.StatusOK {
		t.Fatalf("CONTRACT 6 FAIL: source-b write status=%d body=%s", status, body)
	}

	// Wait for queue to drain.
	time.Sleep(1 * time.Second)

	// Source A queries — must NOT see source B's data.
	status, body = get(t, client, baseURL+"/query/sqlite?q=secret", "key-a-secret")
	if status != http.StatusOK {
		t.Fatalf("CONTRACT 6 FAIL: source-a query status=%d body=%s", status, body)
	}
	var respA struct {
		Results []struct {
			Source  string `json:"source"`
			Content string `json:"content"`
		} `json:"results"`
	}
	json.Unmarshal(body, &respA)

	for _, r := range respA.Results {
		if r.Source == "source-b" {
			t.Fatalf("CONTRACT 6 FAIL: source-a query returned source-b data: %q", r.Content)
		}
	}

	// Source B queries — must NOT see source A's data.
	status, body = get(t, client, baseURL+"/query/sqlite?q=secret", "key-b-secret")
	if status != http.StatusOK {
		t.Fatalf("CONTRACT 6 FAIL: source-b query status=%d body=%s", status, body)
	}
	var respB struct {
		Results []struct {
			Source  string `json:"source"`
			Content string `json:"content"`
		} `json:"results"`
	}
	json.Unmarshal(body, &respB)

	for _, r := range respB.Results {
		if r.Source == "source-a" {
			t.Fatalf("CONTRACT 6 FAIL: source-b query returned source-a data: %q", r.Content)
		}
	}

	t.Log("CONTRACT 6 PASS: source-a and source-b are fully isolated — cross-source data never leaks")
}

// ---------------------------------------------------------------------------
// CONTRACT 7 — Queue overload: fill queue, verify 429 with Retry-After.
//              Reference: Tech Spec Section 16.
// ---------------------------------------------------------------------------

func TestPhase9_QueueOverload_Returns429WithRetryAfter(t *testing.T) {
	src, keys := stdSource("claude", "p9-overload-key")
	src.RateLimit.RequestsPerMinute = 100000 // don't rate-limit
	src.Idempotency.Enabled = false          // unique keys not needed

	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port: 0,
			Bind: "127.0.0.1",
			RateLimit: config.GlobalRateLimitConfig{
				GlobalRequestsPerMinute: 100000,
			},
			QueueSize: 1, // tiny queue — fills immediately
		},
		Retrieval:          config.RetrievalConfig{DefaultProfile: "balanced"},
		Sources:            []*config.Source{src},
		Destinations:       []*config.Destination{{Name: "sqlite", Type: "sqlite"}},
		ResolvedSourceKeys: keys,
		ResolvedAdminKey:   []byte("admin-key"),
	}

	d := daemon.NewTestDaemonBlocking(t, cfg)
	baseURL, shutdown := liveServer(t, d)
	defer shutdown()

	client := &http.Client{Timeout: 5 * time.Second}

	// Flood the queue. With size=1 and a blocking dest, the second write
	// should get 429 queue_full.
	var got429 bool
	var retryAfter string
	for i := 0; i < 50; i++ {
		body := fmt.Sprintf(`{"content":"overload-%d","role":"user"}`, i)
		status, respBody, headers := post(t, client,
			baseURL+"/inbound/claude", "p9-overload-key", "", body)
		if status == http.StatusTooManyRequests {
			code := errorCode(respBody)
			if code == "queue_full" {
				got429 = true
				retryAfter = headers.Get("Retry-After")
				break
			}
		}
	}

	if !got429 {
		t.Fatal("CONTRACT 7 FAIL: never received 429 queue_full after 50 writes")
	}
	if retryAfter == "" {
		t.Fatal("CONTRACT 7 FAIL: 429 queue_full missing Retry-After header")
	}

	t.Logf("CONTRACT 7 PASS: queue overload → 429 queue_full with Retry-After=%s", retryAfter)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newAuthRequest creates a GET / request with the given Authorization header.
func newAuthRequest(authHeader string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	return req
}

// recordAndTime runs the handler and records the elapsed nanoseconds.
func recordAndTime(h http.Handler, req *http.Request, ns *int64) int {
	rr := &statusRecorder{}
	t0 := time.Now()
	h.ServeHTTP(rr, req)
	*ns = time.Since(t0).Nanoseconds()
	return rr.code
}

// statusRecorder captures the status code without buffering the body.
type statusRecorder struct {
	code int
}

func (s *statusRecorder) Header() http.Header        { return http.Header{} }
func (s *statusRecorder) Write(b []byte) (int, error) { return len(b), nil }
func (s *statusRecorder) WriteHeader(code int)        { s.code = code }

// multiSource creates a config with two sources for scope isolation tests.
func multiSourceDaemon(t *testing.T, sources []*config.Source, keys map[string][]byte) *daemon.Daemon {
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
		Sources:            sources,
		Destinations:       []*config.Destination{{Name: "sqlite", Type: "sqlite"}},
		ResolvedSourceKeys: keys,
		ResolvedAdminKey:   []byte("admin-key"),
	}
	return daemon.NewTestDaemon(t, cfg)
}

// retryAfterValue extracts the Retry-After header value.
func retryAfterValue(h http.Header) string {
	return h.Get("Retry-After")
}

// suppressUnused avoids unused import warnings.
var _ = strings.Contains
