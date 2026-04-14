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

package agent

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestQuotaManager(t *testing.T) *QuotaManager {
	t.Helper()
	dir := t.TempDir()
	qm := NewQuotaManager(dir, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { qm.Stop() })
	return qm
}

func TestQuota_NoConfig(t *testing.T) {
	qm := newTestQuotaManager(t)
	allowed, _, _ := qm.CheckRequest("unknown")
	if !allowed {
		t.Fatal("no config should allow all requests")
	}
}

func TestQuota_RPM(t *testing.T) {
	qm := newTestQuotaManager(t)
	qm.SetConfig("agent-1", &QuotaConfig{RequestsPerMinute: 3})

	for i := 0; i < 3; i++ {
		allowed, _, _ := qm.CheckRequest("agent-1")
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	allowed, quotaType, retryAfter := qm.CheckRequest("agent-1")
	if allowed {
		t.Fatal("4th request should be denied")
	}
	if quotaType != "requests_per_minute" {
		t.Fatalf("expected requests_per_minute, got %s", quotaType)
	}
	if retryAfter <= 0 {
		t.Fatalf("expected positive retryAfter, got %d", retryAfter)
	}
}

func TestQuota_WritesPerDay(t *testing.T) {
	qm := newTestQuotaManager(t)
	qm.SetConfig("agent-1", &QuotaConfig{WritesPerDay: 2})

	for i := 0; i < 2; i++ {
		allowed, _ := qm.CheckWrite("agent-1")
		if !allowed {
			t.Fatalf("write %d should be allowed", i+1)
		}
	}

	allowed, quotaType := qm.CheckWrite("agent-1")
	if allowed {
		t.Fatal("3rd write should be denied")
	}
	if quotaType != "writes_per_day" {
		t.Fatalf("expected writes_per_day, got %s", quotaType)
	}
}

func TestQuota_ToolCallsPerDay(t *testing.T) {
	qm := newTestQuotaManager(t)
	qm.SetConfig("agent-1", &QuotaConfig{ToolCallsPerDay: 2})

	for i := 0; i < 2; i++ {
		allowed, _ := qm.CheckToolCall("agent-1")
		if !allowed {
			t.Fatalf("tool call %d should be allowed", i+1)
		}
	}

	allowed, quotaType := qm.CheckToolCall("agent-1")
	if allowed {
		t.Fatal("3rd tool call should be denied")
	}
	if quotaType != "tool_calls_per_day" {
		t.Fatalf("expected tool_calls_per_day, got %s", quotaType)
	}
}

func TestQuota_NoLimitWritesPerDay(t *testing.T) {
	qm := newTestQuotaManager(t)
	qm.SetConfig("agent-1", &QuotaConfig{WritesPerDay: 0}) // 0 = unlimited

	for i := 0; i < 100; i++ {
		allowed, _ := qm.CheckWrite("agent-1")
		if !allowed {
			t.Fatal("0 limit should mean unlimited")
		}
	}
}

func TestQuota_DayReset(t *testing.T) {
	qm := newTestQuotaManager(t)
	qm.SetConfig("agent-1", &QuotaConfig{WritesPerDay: 1})

	allowed, _ := qm.CheckWrite("agent-1")
	if !allowed {
		t.Fatal("first write should be allowed")
	}

	allowed, _ = qm.CheckWrite("agent-1")
	if allowed {
		t.Fatal("second write should be denied")
	}

	// Simulate day change by manipulating state.
	qm.mu.Lock()
	state := qm.states["agent-1"]
	state.DayStart = state.DayStart.Add(-24 * time.Hour)
	qm.mu.Unlock()

	// After day change, should be allowed again.
	allowed, _ = qm.CheckWrite("agent-1")
	if !allowed {
		t.Fatal("write should be allowed after day reset")
	}
}

func TestQuota_PersistAndLoad(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create manager, use some quota, stop.
	qm1 := NewQuotaManager(dir, logger)
	qm1.SetConfig("agent-1", &QuotaConfig{WritesPerDay: 10})
	qm1.CheckWrite("agent-1")
	qm1.CheckWrite("agent-1")
	qm1.Stop()

	// Verify state file exists.
	statePath := filepath.Join(dir, "quotas.state")
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("state file should exist: %v", err)
	}

	var states []QuotaState
	if err := json.Unmarshal(data, &states); err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 state, got %d", len(states))
	}
	if states[0].WritesUsed != 2 {
		t.Fatalf("expected 2 writes used, got %d", states[0].WritesUsed)
	}

	// Create new manager — should load state.
	qm2 := NewQuotaManager(dir, logger)
	defer qm2.Stop()

	state := qm2.GetState("agent-1")
	if state == nil {
		t.Fatal("state should be loaded")
	}
	if state.WritesUsed != 2 {
		t.Fatalf("expected 2 writes used after reload, got %d", state.WritesUsed)
	}
}

func TestQuota_GetState_Nil(t *testing.T) {
	qm := newTestQuotaManager(t)
	state := qm.GetState("nonexistent")
	if state != nil {
		t.Fatal("expected nil for nonexistent agent")
	}
}

func TestQuota_StopIdempotent(t *testing.T) {
	qm := newTestQuotaManager(t)
	qm.Stop()
	qm.Stop() // should not panic
}

func TestUtcMidnight(t *testing.T) {
	now := time.Date(2026, 4, 13, 15, 30, 45, 0, time.UTC)
	midnight := utcMidnight(now)
	expected := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)
	if !midnight.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, midnight)
	}
}
