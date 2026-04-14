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
	"sync"
	"testing"
	"time"
)

func newTestSessionManager(t *testing.T, idleTimeout time.Duration) *SessionManager {
	t.Helper()
	sm := NewSessionManager(idleTimeout, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	t.Cleanup(func() { sm.Stop() })
	return sm
}

func TestSession_TouchCreates(t *testing.T) {
	sm := newTestSessionManager(t, 30*time.Minute)

	s := sm.Touch("agent-1", "token-a", 100, 50)
	if s.AgentID != "agent-1" {
		t.Fatalf("expected agent_id %q, got %q", "agent-1", s.AgentID)
	}
	if s.RequestCount != 1 {
		t.Fatalf("expected request_count 1, got %d", s.RequestCount)
	}
	if s.BytesWritten != 100 {
		t.Fatalf("expected bytes_written 100, got %d", s.BytesWritten)
	}
	if s.BytesRead != 50 {
		t.Fatalf("expected bytes_read 50, got %d", s.BytesRead)
	}
}

func TestSession_TouchUpdates(t *testing.T) {
	sm := newTestSessionManager(t, 30*time.Minute)

	sm.Touch("agent-1", "token-a", 100, 50)
	s := sm.Touch("agent-1", "token-a", 200, 100)

	if s.RequestCount != 2 {
		t.Fatalf("expected request_count 2, got %d", s.RequestCount)
	}
	if s.BytesWritten != 300 {
		t.Fatalf("expected bytes_written 300, got %d", s.BytesWritten)
	}
	if s.BytesRead != 150 {
		t.Fatalf("expected bytes_read 150, got %d", s.BytesRead)
	}
}

func TestSession_DifferentTokens(t *testing.T) {
	sm := newTestSessionManager(t, 30*time.Minute)

	sm.Touch("agent-1", "token-a", 0, 0)
	sm.Touch("agent-1", "token-b", 0, 0)

	if sm.ActiveCount() != 2 {
		t.Fatalf("expected 2 active sessions, got %d", sm.ActiveCount())
	}
	if sm.AgentSessionCount("agent-1") != 2 {
		t.Fatalf("expected 2 sessions for agent-1, got %d", sm.AgentSessionCount("agent-1"))
	}
}

func TestSession_SessionsList(t *testing.T) {
	sm := newTestSessionManager(t, 30*time.Minute)

	sm.Touch("agent-1", "token-a", 0, 0)
	sm.Touch("agent-1", "token-b", 0, 0)
	sm.Touch("agent-2", "token-c", 0, 0)

	sessions := sm.Sessions("agent-1")
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions for agent-1, got %d", len(sessions))
	}

	sessions = sm.Sessions("agent-2")
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session for agent-2, got %d", len(sessions))
	}

	sessions = sm.Sessions("nonexistent")
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions for nonexistent, got %d", len(sessions))
	}
}

func TestSession_Expiration(t *testing.T) {
	sm := newTestSessionManager(t, 5*time.Minute)

	sm.Touch("agent-1", "token-a", 0, 0)
	sm.Touch("agent-1", "token-b", 0, 0)

	if sm.ActiveCount() != 2 {
		t.Fatalf("expected 2 before reap, got %d", sm.ActiveCount())
	}

	// Reap with a time 10 minutes in the future.
	sm.ReapNow(time.Now().Add(10 * time.Minute))

	if sm.ActiveCount() != 0 {
		t.Fatalf("expected 0 after reap, got %d", sm.ActiveCount())
	}
}

func TestSession_ExpirationPartial(t *testing.T) {
	sm := newTestSessionManager(t, 5*time.Minute)

	sm.Touch("agent-1", "token-old", 0, 0)

	// Wait a tiny bit, then touch a new session.
	sm.Touch("agent-1", "token-new", 0, 0)

	// Reap at a time that expires token-old but not token-new.
	// Both were created nearly simultaneously, so we manipulate directly.
	sm.mu.Lock()
	for k, s := range sm.sessions {
		if k.clientToken == "token-old" {
			s.LastActivity = time.Now().Add(-10 * time.Minute)
		}
	}
	sm.mu.Unlock()

	sm.ReapNow(time.Now())

	if sm.ActiveCount() != 1 {
		t.Fatalf("expected 1 after partial reap, got %d", sm.ActiveCount())
	}

	sessions := sm.Sessions("agent-1")
	if len(sessions) != 1 || sessions[0].ClientToken != "token-new" {
		t.Fatal("expected only token-new to survive")
	}
}

func TestSession_InvalidateAgent(t *testing.T) {
	sm := newTestSessionManager(t, 30*time.Minute)

	sm.Touch("agent-1", "token-a", 0, 0)
	sm.Touch("agent-1", "token-b", 0, 0)
	sm.Touch("agent-2", "token-c", 0, 0)

	removed := sm.InvalidateAgent("agent-1")
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	if sm.ActiveCount() != 1 {
		t.Fatalf("expected 1 remaining, got %d", sm.ActiveCount())
	}
	if sm.AgentSessionCount("agent-2") != 1 {
		t.Fatal("agent-2 session should still exist")
	}
}

func TestSession_StopIdempotent(t *testing.T) {
	sm := newTestSessionManager(t, 30*time.Minute)
	sm.Stop()
	sm.Stop() // Should not panic.
}

func TestSession_ConcurrentAccess(t *testing.T) {
	sm := newTestSessionManager(t, 30*time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			agentID := "agent-1"
			if i%2 == 0 {
				agentID = "agent-2"
			}
			sm.Touch(agentID, "token", int64(i), int64(i))
			sm.Sessions(agentID)
			sm.ActiveCount()
			sm.AgentSessionCount(agentID)
		}(i)
	}
	wg.Wait()

	// Just verify no panics and counts are sane.
	if sm.ActiveCount() > 2 {
		t.Fatalf("expected at most 2 sessions (one per agent), got %d", sm.ActiveCount())
	}
}
