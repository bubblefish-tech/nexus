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

package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/observability"
)

func TestDeadManSwitch_SuccessfulPing(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %s", ct)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dm := observability.NewDeadManSwitch(observability.DeadManConfig{
		URL:     srv.URL,
		Timeout: 5 * time.Second,
	})

	err := dm.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if dm.ConsecutiveFailures() != 0 {
		t.Errorf("expected 0 failures, got %d", dm.ConsecutiveFailures())
	}
	if dm.TotalPosts() != 1 {
		t.Errorf("expected 1 total post, got %d", dm.TotalPosts())
	}
	if dm.LastPostTime().IsZero() {
		t.Error("last post time should not be zero after success")
	}
}

func TestDeadManSwitch_FailureCounting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dm := observability.NewDeadManSwitch(observability.DeadManConfig{
		URL:         srv.URL,
		MaxFailures: 3,
		Timeout:     5 * time.Second,
	})

	for i := 0; i < 5; i++ {
		_ = dm.Ping(context.Background())
	}

	if dm.ConsecutiveFailures() != 5 {
		t.Errorf("expected 5 consecutive failures, got %d", dm.ConsecutiveFailures())
	}
	if dm.TotalFailures() != 5 {
		t.Errorf("expected 5 total failures, got %d", dm.TotalFailures())
	}
	if !dm.LastPostTime().IsZero() {
		t.Error("last post time should be zero with no successes")
	}
}

func TestDeadManSwitch_FailureResetOnSuccess(t *testing.T) {
	var failCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failCount.Add(1) <= 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dm := observability.NewDeadManSwitch(observability.DeadManConfig{
		URL:         srv.URL,
		MaxFailures: 3,
		Timeout:     5 * time.Second,
	})

	// 3 failures.
	for i := 0; i < 3; i++ {
		_ = dm.Ping(context.Background())
	}
	if dm.ConsecutiveFailures() != 3 {
		t.Errorf("expected 3 failures, got %d", dm.ConsecutiveFailures())
	}

	// 1 success resets consecutive counter.
	err := dm.Ping(context.Background())
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if dm.ConsecutiveFailures() != 0 {
		t.Errorf("expected 0 after success, got %d", dm.ConsecutiveFailures())
	}
	// Total failures should still be 3.
	if dm.TotalFailures() != 3 {
		t.Errorf("expected 3 total failures, got %d", dm.TotalFailures())
	}
}

func TestDeadManSwitch_InvalidURL(t *testing.T) {
	dm := observability.NewDeadManSwitch(observability.DeadManConfig{
		URL:         "http://127.0.0.1:1", // nothing listening
		MaxFailures: 3,
		Timeout:     1 * time.Second,
	})

	err := dm.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
	if dm.ConsecutiveFailures() != 1 {
		t.Errorf("expected 1 failure, got %d", dm.ConsecutiveFailures())
	}
}

func TestDeadManSwitch_StopIdempotent(t *testing.T) {
	dm := observability.NewDeadManSwitch(observability.DeadManConfig{
		URL: "http://127.0.0.1:1",
	})

	// Stop multiple times — should not panic (sync.Once).
	dm.Stop()
	dm.Stop()
	dm.Stop()
}

func TestDeadManSwitch_StartAndStop(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dm := observability.NewDeadManSwitch(observability.DeadManConfig{
		URL:      srv.URL,
		Interval: 50 * time.Millisecond,
		Timeout:  5 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dm.Start(ctx)

	// Wait a bit for at least the initial heartbeat.
	time.Sleep(200 * time.Millisecond)
	dm.Stop()

	// Should have at least 1 hit (the immediate first heartbeat).
	if hits.Load() < 1 {
		t.Errorf("expected at least 1 heartbeat hit, got %d", hits.Load())
	}
}

func TestDeadManSwitch_Defaults(t *testing.T) {
	dm := observability.NewDeadManSwitch(observability.DeadManConfig{
		URL: "http://example.com/heartbeat",
	})
	defer dm.Stop()

	// Verify defaults by checking that the switch was created successfully.
	if dm == nil {
		t.Fatal("NewDeadManSwitch returned nil")
	}
	if dm.TotalPosts() != 0 {
		t.Errorf("expected 0 total posts initially, got %d", dm.TotalPosts())
	}
}
