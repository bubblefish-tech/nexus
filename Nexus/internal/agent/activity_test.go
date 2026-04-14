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

func newTestActivityLog(t *testing.T) *ActivityLog {
	t.Helper()
	al := NewActivityLog(7*24*time.Hour, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { al.Stop() })
	return al
}

func TestActivity_Record(t *testing.T) {
	al := newTestActivityLog(t)

	al.Record(ActivityEvent{
		AgentID:   "agent-1",
		EventType: "write",
		Resource:  "nexus_write",
		LatencyMs: 5,
		Result:    "ok",
	})

	if al.Len() != 1 {
		t.Fatalf("expected 1 event, got %d", al.Len())
	}
}

func TestActivity_Query(t *testing.T) {
	al := newTestActivityLog(t)

	now := time.Now().UTC()
	al.Record(ActivityEvent{AgentID: "agent-1", EventType: "write", Timestamp: now.Add(-2 * time.Hour)})
	al.Record(ActivityEvent{AgentID: "agent-1", EventType: "search", Timestamp: now.Add(-1 * time.Hour)})
	al.Record(ActivityEvent{AgentID: "agent-2", EventType: "write", Timestamp: now})

	// Query all for agent-1.
	events := al.Query("agent-1", time.Time{}, 100)
	if len(events) != 2 {
		t.Fatalf("expected 2 events for agent-1, got %d", len(events))
	}
	// Should be in chronological order.
	if events[0].EventType != "write" || events[1].EventType != "search" {
		t.Fatal("events should be in chronological order")
	}

	// Query with since filter.
	events = al.Query("agent-1", now.Add(-90*time.Minute), 100)
	if len(events) != 1 {
		t.Fatalf("expected 1 event after 90min-ago filter, got %d", len(events))
	}

	// Query with limit.
	events = al.Query("agent-1", time.Time{}, 1)
	if len(events) != 1 {
		t.Fatalf("expected 1 event with limit=1, got %d", len(events))
	}

	// Query for nonexistent agent.
	events = al.Query("nonexistent", time.Time{}, 100)
	if len(events) != 0 {
		t.Fatalf("expected 0 events for nonexistent, got %d", len(events))
	}
}

func TestActivity_Retention(t *testing.T) {
	al := NewActivityLog(1*time.Hour, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	defer al.Stop()

	now := time.Now().UTC()
	al.Record(ActivityEvent{AgentID: "agent-1", EventType: "old", Timestamp: now.Add(-2 * time.Hour)})
	al.Record(ActivityEvent{AgentID: "agent-1", EventType: "new", Timestamp: now})

	if al.Len() != 2 {
		t.Fatalf("expected 2 before prune, got %d", al.Len())
	}

	al.PruneNow(now)

	if al.Len() != 1 {
		t.Fatalf("expected 1 after prune, got %d", al.Len())
	}

	events := al.Query("agent-1", time.Time{}, 100)
	if len(events) != 1 || events[0].EventType != "new" {
		t.Fatal("only the new event should survive")
	}
}

func TestActivity_AllAgentSummaries(t *testing.T) {
	al := newTestActivityLog(t)

	now := time.Now().UTC()
	al.Record(ActivityEvent{AgentID: "agent-1", Timestamp: now.Add(-1 * time.Hour)})
	al.Record(ActivityEvent{AgentID: "agent-1", Timestamp: now})
	al.Record(ActivityEvent{AgentID: "agent-2", Timestamp: now.Add(-30 * time.Minute)})

	summaries := al.AllAgentSummaries()
	if len(summaries) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(summaries))
	}
	if summaries["agent-1"].TotalEvents != 2 {
		t.Fatalf("expected 2 events for agent-1, got %d", summaries["agent-1"].TotalEvents)
	}
	if summaries["agent-2"].TotalEvents != 1 {
		t.Fatalf("expected 1 event for agent-2, got %d", summaries["agent-2"].TotalEvents)
	}
}

func TestActivity_StopIdempotent(t *testing.T) {
	al := newTestActivityLog(t)
	al.Stop()
	al.Stop() // should not panic
}
