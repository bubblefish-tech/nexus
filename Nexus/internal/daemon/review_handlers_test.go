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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/daemon"
)

const (
	testReviewListToken = "bfn_review_list_TESTTOKEN123456"
	testReviewReadToken = "bfn_review_read_TESTTOKEN123456"
)

// buildReviewTestDaemon constructs a test daemon with review tokens configured.
func buildReviewTestDaemon(t *testing.T) *daemon.Daemon {
	t.Helper()
	cfg := &config.Config{
		Daemon: config.DaemonConfig{
			Port:      18081,
			Bind:      "127.0.0.1",
			QueueSize: 100,
			RateLimit: config.GlobalRateLimitConfig{
				GlobalRequestsPerMinute: 1000,
			},
		},
		Retrieval: config.RetrievalConfig{
			DefaultProfile: "balanced",
		},
		Sources: []*config.Source{{
			Name:     "test-source",
			CanRead:  true,
			CanWrite: true,
		}},
		ResolvedSourceKeys:    map[string][]byte{"test-source": []byte("bfn_data_testkey")},
		ResolvedAdminKey:      []byte("bfn_admin_adminkey"),
		ResolvedReviewListKey: []byte(testReviewListToken),
		ResolvedReviewReadKey: []byte(testReviewReadToken),
	}
	return daemon.NewTestDaemon(t, cfg)
}

func TestReview_ListEndpoint_RequiresReviewListToken(t *testing.T) {
	t.Helper()
	d := buildReviewTestDaemon(t)
	server := httptest.NewServer(d.BuildRouter())
	defer server.Close()

	tests := []struct {
		name     string
		token    string
		wantCode int
	}{
		{"no_token", "", http.StatusUnauthorized},
		{"data_token", "bfn_data_testkey", http.StatusUnauthorized},
		{"admin_token", "bfn_admin_adminkey", http.StatusUnauthorized},
		{"review_list_token", testReviewListToken, http.StatusOK},
		{"review_read_token", testReviewReadToken, http.StatusUnauthorized},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/review/quarantine", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantCode {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("token=%q: got %d, want %d (body: %s)", tc.token, resp.StatusCode, tc.wantCode, body)
			}
		})
	}
}

func TestReview_ReadEndpoint_RequiresReviewToken(t *testing.T) {
	t.Helper()
	d := buildReviewTestDaemon(t)
	server := httptest.NewServer(d.BuildRouter())
	defer server.Close()

	tests := []struct {
		name     string
		token    string
		wantCode int
	}{
		{"no_token", "", http.StatusUnauthorized},
		{"data_token", "bfn_data_testkey", http.StatusUnauthorized},
		{"admin_token", "bfn_admin_adminkey", http.StatusUnauthorized},
		// Both review tokens work on the read endpoint (list token subsumes read).
		{"review_list_token", testReviewListToken, http.StatusNotFound},
		{"review_read_token", testReviewReadToken, http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/review/quarantine/some-memory-id", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantCode {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("token=%q: got %d, want %d (body: %s)", tc.token, resp.StatusCode, tc.wantCode, body)
			}
		})
	}
}

func TestReview_ListReturnsJSON(t *testing.T) {
	t.Helper()
	d := buildReviewTestDaemon(t)
	server := httptest.NewServer(d.BuildRouter())
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/review/quarantine", nil)
	req.Header.Set("Authorization", "Bearer "+testReviewListToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["quarantined_ids"]; !ok {
		t.Error("response missing quarantined_ids field")
	}
	if _, ok := body["count"]; !ok {
		t.Error("response missing count field")
	}
}

// TestReview_ReviewTokensRejectedOnDataEndpoints verifies that review tokens
// cannot access data endpoints (write/query).
func TestReview_ReviewTokensRejectedOnDataEndpoints(t *testing.T) {
	t.Helper()
	d := buildReviewTestDaemon(t)
	server := httptest.NewServer(d.BuildRouter())
	defer server.Close()

	for _, token := range []string{testReviewListToken, testReviewReadToken} {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/query/test-dest", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("review token on query endpoint: got %d, want 401", resp.StatusCode)
		}
	}
}
