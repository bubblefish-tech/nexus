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

// Phase R-5: Provenance Fields — Behavioral verification tests.
//
// Verification gate from the State Verification Guide:
//   - [x] Write with X-Actor-Type: agent. WAL and DB have actor_type='agent'.
//   - [x] Query with ?actor_type=user: only user-type returned.
//   - [x] Schema migration: existing data survives with empty defaults.
//   - [x] Invalid actor_type returns 400.
package daemon_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/daemon"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// postWithActorType performs a POST with X-Actor-Type and X-Actor-ID headers.
func postWithActorType(t *testing.T, handler http.Handler, url, token, body, actorType, actorID string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if actorType != "" {
		req.Header.Set("X-Actor-Type", actorType)
	}
	if actorID != "" {
		req.Header.Set("X-Actor-ID", actorID)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	resp := rr.Result()
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close response body: %v", err)
		}
	}()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func queryWithActorType(t *testing.T, handler http.Handler, url, token string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	resp := rr.Result()
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close response body: %v", err)
		}
	}()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// provenanceDaemon creates a daemon with a real SQLite destination for
// end-to-end provenance testing.
func provenanceDaemon(t *testing.T) (*daemon.Daemon, string) {
	t.Helper()
	src := &config.Source{
		Name:             "provtest",
		Namespace:        "provtest",
		CanRead:          true,
		CanWrite:         true,
		TargetDest:       "sqlite",
		DefaultActorType: "user",
		DefaultProfile:   "balanced",
		RateLimit:        config.SourceRateLimitConfig{RequestsPerMinute: 1000},
		PayloadLimits:    config.PayloadLimitsConfig{MaxBytes: 10 * 1024 * 1024},
		Idempotency:      config.IdempotencyConfig{Enabled: true, DedupWindowSeconds: 300},
		Mapping: map[string]string{
			"content": "content",
		},
		Policy: config.SourcePolicyConfig{
			AllowedDestinations: []string{"sqlite"},
			AllowedOperations:   []string{"write", "read"},
			MaxResults:          50,
		},
	}
	key := "prov-key-001"
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
		ResolvedSourceKeys: map[string][]byte{"provtest": []byte(key)},
		ResolvedAdminKey:   []byte("admin-key"),
	}
	d, _ := daemon.NewTestDaemonWithSQLite(t, cfg)
	return d, key
}

// ---------------------------------------------------------------------------
// CHECK R-5.1 — Invalid actor_type returns 400
// ---------------------------------------------------------------------------

func TestPhaseR5_InvalidActorType_Returns400(t *testing.T) {
	d, key := provenanceDaemon(t)
	handler := d.WriteHandler()

	invalidTypes := []string{"admin", "robot", "USER", "Agent", "SYSTEM", "foo"}

	for _, at := range invalidTypes {
		status, body := postWithActorType(t, handler,
			"/inbound/provtest",
			key,
			`{"content":"test"}`,
			at, "actor-1",
		)
		if status != http.StatusBadRequest {
			t.Errorf("actor_type=%q: want 400, got %d body=%s", at, status, body)
			continue
		}
		var errResp struct{ Error string `json:"error"` }
		_ = json.Unmarshal(body, &errResp)
		if errResp.Error != "invalid_actor_type" {
			t.Errorf("actor_type=%q: want error=invalid_actor_type, got %q", at, errResp.Error)
		}
	}
}

// ---------------------------------------------------------------------------
// CHECK R-5.2 — Valid actor_type values accepted (user, agent, system)
// ---------------------------------------------------------------------------

func TestPhaseR5_ValidActorTypes_Accepted(t *testing.T) {
	d, key := provenanceDaemon(t)
	handler := d.WriteHandler()

	for _, at := range []string{"user", "agent", "system"} {
		status, body := postWithActorType(t, handler,
			"/inbound/provtest",
			key,
			`{"content":"provenance test `+at+`"}`,
			at, at+"-actor",
		)
		if status != http.StatusOK {
			t.Errorf("actor_type=%q: want 200, got %d body=%s", at, status, body)
		}
	}
}

// ---------------------------------------------------------------------------
// CHECK R-5.3 — Write with X-Actor-Type: agent, DB has actor_type='agent'
// ---------------------------------------------------------------------------

func TestPhaseR5_WriteAgentActorType_PersistedInDB(t *testing.T) {
	d, key := provenanceDaemon(t)
	writeHandler := d.WriteHandler()
	queryHandler := d.QueryHandler()

	// Write with actor_type=agent.
	status, body := postWithActorType(t, writeHandler,
		"/inbound/provtest",
		key,
		`{"content":"agent memory"}`,
		"agent", "claude-3",
	)
	if status != http.StatusOK {
		t.Fatalf("write: want 200, got %d body=%s", status, body)
	}

	// Allow queue to process.
	time.Sleep(1 * time.Second)

	// Query all records and verify actor_type=agent is persisted.
	status, body = queryWithActorType(t, queryHandler,
		"/query/sqlite?limit=10",
		key,
	)
	if status != http.StatusOK {
		t.Fatalf("query: want 200, got %d body=%s", status, body)
	}

	var resp struct {
		Results []struct {
			ActorType string `json:"actor_type"`
			ActorID   string `json:"actor_id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, body)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected at least 1 record, got 0")
	}

	found := false
	for _, rec := range resp.Results {
		if rec.ActorType == "agent" && rec.ActorID == "claude-3" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected record with actor_type=agent, actor_id=claude-3; got %+v", resp.Results)
	}
}

// ---------------------------------------------------------------------------
// CHECK R-5.4 — Query with ?actor_type=user: only user-type returned
// ---------------------------------------------------------------------------

func TestPhaseR5_QueryActorTypeFilter(t *testing.T) {
	d, key := provenanceDaemon(t)
	writeHandler := d.WriteHandler()
	queryHandler := d.QueryHandler()

	// Write records with different actor types.
	for _, at := range []string{"user", "agent", "system"} {
		status, body := postWithActorType(t, writeHandler,
			"/inbound/provtest",
			key,
			`{"content":"memory from `+at+`"}`,
			at, at+"-id",
		)
		if status != http.StatusOK {
			t.Fatalf("write(%s): want 200, got %d body=%s", at, status, body)
		}
	}

	// Allow queue to process.
	time.Sleep(1 * time.Second)

	// Query with actor_type=user filter.
	status, body := queryWithActorType(t, queryHandler,
		"/query/sqlite?limit=10&actor_type=user",
		key,
	)
	if status != http.StatusOK {
		t.Fatalf("query: want 200, got %d body=%s", status, body)
	}

	var resp struct {
		Results []struct {
			ActorType string `json:"actor_type"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 record with actor_type=user, got %d", len(resp.Results))
	}
	if resp.Results[0].ActorType != "user" {
		t.Fatalf("expected actor_type=user, got %q", resp.Results[0].ActorType)
	}
}

// ---------------------------------------------------------------------------
// CHECK R-5.5 — Invalid actor_type in query returns 400
// ---------------------------------------------------------------------------

func TestPhaseR5_QueryInvalidActorType_Returns400(t *testing.T) {
	d, key := provenanceDaemon(t)
	queryHandler := d.QueryHandler()

	status, body := queryWithActorType(t, queryHandler,
		"/query/sqlite?actor_type=invalid",
		key,
	)
	if status != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid query actor_type, got %d body=%s", status, body)
	}
	var errResp struct{ Error string `json:"error"` }
	_ = json.Unmarshal(body, &errResp)
	if errResp.Error != "invalid_actor_type" {
		t.Fatalf("want error=invalid_actor_type, got %q", errResp.Error)
	}
}

// ---------------------------------------------------------------------------
// CHECK R-5.6 — Default actor_type from source config when header omitted
// ---------------------------------------------------------------------------

func TestPhaseR5_DefaultActorType_FromSourceConfig(t *testing.T) {
	d, key := provenanceDaemon(t)
	writeHandler := d.WriteHandler()
	queryHandler := d.QueryHandler()

	// Write WITHOUT X-Actor-Type header — should default to "user" from config.
	status, body := postWithActorType(t, writeHandler,
		"/inbound/provtest",
		key,
		`{"content":"default actor type"}`,
		"", "", // no actor headers
	)
	if status != http.StatusOK {
		t.Fatalf("write: want 200, got %d body=%s", status, body)
	}

	// Allow queue to process.
	time.Sleep(1 * time.Second)

	// Query and verify actor_type defaulted to "user".
	status, body = queryWithActorType(t, queryHandler,
		"/query/sqlite?limit=10",
		key,
	)
	if status != http.StatusOK {
		t.Fatalf("query: want 200, got %d body=%s", status, body)
	}

	var resp struct {
		Results []struct {
			ActorType string `json:"actor_type"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if len(resp.Results) == 0 {
		t.Fatal("expected at least 1 record, got 0")
	}
	if resp.Results[0].ActorType != "user" {
		t.Fatalf("expected default actor_type=user, got %q", resp.Results[0].ActorType)
	}
}
