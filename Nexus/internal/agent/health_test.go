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
	"log/slog"
	"os"
	"testing"
	"time"
)

func newTestHealthTracker(t *testing.T) *HealthTracker {
	t.Helper()
	ht := NewHealthTracker(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { ht.Stop() })
	return ht
}

func TestHealth_Active(t *testing.T) {
	ht := newTestHealthTracker(t)
	ht.Touch("agent-1")

	state := ht.State("agent-1")
	if state != HealthActive {
		t.Fatalf("expected active, got %s", state)
	}
}

func TestHealth_Unknown(t *testing.T) {
	ht := newTestHealthTracker(t)
	state := ht.State("unknown")
	if state != HealthDormant {
		t.Fatalf("expected dormant for unknown agent, got %s", state)
	}
}

func TestHealth_Stale(t *testing.T) {
	ht := newTestHealthTracker(t)

	ht.mu.Lock()
	ht.lastSeen["agent-1"] = time.Now().Add(-6 * time.Minute)
	ht.mu.Unlock()

	state := ht.State("agent-1")
	if state != HealthStale {
		t.Fatalf("expected stale, got %s", state)
	}
}

func TestHealth_Inactive(t *testing.T) {
	ht := newTestHealthTracker(t)

	ht.mu.Lock()
	ht.lastSeen["agent-1"] = time.Now().Add(-2 * time.Hour)
	ht.mu.Unlock()

	state := ht.State("agent-1")
	if state != HealthInactive {
		t.Fatalf("expected inactive, got %s", state)
	}
}

func TestHealth_Dormant(t *testing.T) {
	ht := newTestHealthTracker(t)

	ht.mu.Lock()
	ht.lastSeen["agent-1"] = time.Now().Add(-25 * time.Hour)
	ht.mu.Unlock()

	state := ht.State("agent-1")
	if state != HealthDormant {
		t.Fatalf("expected dormant, got %s", state)
	}
}

func TestHealth_Heartbeat(t *testing.T) {
	ht := newTestHealthTracker(t)

	// Start as dormant (6min old).
	ht.mu.Lock()
	ht.lastSeen["agent-1"] = time.Now().Add(-6 * time.Minute)
	ht.mu.Unlock()

	if ht.State("agent-1") != HealthStale {
		t.Fatal("should be stale before heartbeat")
	}

	ht.Heartbeat("agent-1")

	if ht.State("agent-1") != HealthActive {
		t.Fatal("should be active after heartbeat")
	}
}

func TestHealth_AllStates(t *testing.T) {
	ht := newTestHealthTracker(t)

	ht.Touch("active-agent")
	ht.mu.Lock()
	ht.lastSeen["stale-agent"] = time.Now().Add(-6 * time.Minute)
	ht.lastSeen["dormant-agent"] = time.Now().Add(-25 * time.Hour)
	ht.mu.Unlock()

	states := ht.AllStates()
	if len(states) != 3 {
		t.Fatalf("expected 3 states, got %d", len(states))
	}
	if states["active-agent"] != HealthActive {
		t.Fatalf("expected active, got %s", states["active-agent"])
	}
	if states["stale-agent"] != HealthStale {
		t.Fatalf("expected stale, got %s", states["stale-agent"])
	}
	if states["dormant-agent"] != HealthDormant {
		t.Fatalf("expected dormant, got %s", states["dormant-agent"])
	}
}

func TestHealth_ClassifyHealth(t *testing.T) {
	tests := []struct {
		elapsed time.Duration
		want    HealthState
	}{
		{0, HealthActive},
		{1 * time.Minute, HealthActive},
		{4 * time.Minute, HealthActive},
		{5 * time.Minute, HealthStale},
		{30 * time.Minute, HealthStale},
		{59 * time.Minute, HealthStale},
		{1 * time.Hour, HealthInactive},
		{12 * time.Hour, HealthInactive},
		{23 * time.Hour, HealthInactive},
		{24 * time.Hour, HealthDormant},
		{48 * time.Hour, HealthDormant},
	}

	for _, tt := range tests {
		got := classifyHealth(tt.elapsed)
		if got != tt.want {
			t.Errorf("classifyHealth(%v) = %s, want %s", tt.elapsed, got, tt.want)
		}
	}
}

func TestHealth_StopIdempotent(t *testing.T) {
	ht := newTestHealthTracker(t)
	ht.Stop()
	ht.Stop() // should not panic
}
