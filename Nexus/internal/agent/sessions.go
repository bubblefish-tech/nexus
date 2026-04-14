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
	"sync"
	"time"
)

// SessionState tracks a single agent session.
type SessionState struct {
	AgentID      string    `json:"agent_id"`
	ClientToken  string    `json:"client_token"`
	StartedAt    time.Time `json:"started_at"`
	LastActivity time.Time `json:"last_activity"`
	RequestCount int64     `json:"request_count"`
	BytesWritten int64     `json:"bytes_written"`
	BytesRead    int64     `json:"bytes_read"`
}

// sessionKey combines agent_id and client_token for map lookup.
type sessionKey struct {
	agentID     string
	clientToken string
}

// SessionManager tracks active agent sessions in memory. Sessions are
// ephemeral — they do not survive daemon restart by design.
type SessionManager struct {
	mu          sync.RWMutex
	sessions    map[sessionKey]*SessionState
	idleTimeout time.Duration
	logger      *slog.Logger
	stopCh      chan struct{}
	stopOnce    sync.Once
}

// NewSessionManager creates a session manager with the given idle timeout.
// The reap goroutine runs immediately; call Stop() to shut it down.
func NewSessionManager(idleTimeout time.Duration, logger *slog.Logger) *SessionManager {
	sm := &SessionManager{
		sessions:    make(map[sessionKey]*SessionState),
		idleTimeout: idleTimeout,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
	go sm.reapLoop()
	return sm
}

// Touch creates or updates a session for the given agent/token pair.
// Returns the current SessionState after the update.
func (sm *SessionManager) Touch(agentID, clientToken string, bytesWritten, bytesRead int64) *SessionState {
	key := sessionKey{agentID: agentID, clientToken: clientToken}
	now := time.Now()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.sessions[key]
	if !ok {
		s = &SessionState{
			AgentID:     agentID,
			ClientToken: clientToken,
			StartedAt:   now,
		}
		sm.sessions[key] = s
	}

	s.LastActivity = now
	s.RequestCount++
	s.BytesWritten += bytesWritten
	s.BytesRead += bytesRead

	return s
}

// Sessions returns a snapshot of active sessions for the given agent.
func (sm *SessionManager) Sessions(agentID string) []SessionState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var result []SessionState
	for k, s := range sm.sessions {
		if k.agentID == agentID {
			result = append(result, *s)
		}
	}
	return result
}

// ActiveCount returns the total number of active sessions across all agents.
func (sm *SessionManager) ActiveCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// AgentSessionCount returns the number of active sessions for a specific agent.
func (sm *SessionManager) AgentSessionCount(agentID string) int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	count := 0
	for k := range sm.sessions {
		if k.agentID == agentID {
			count++
		}
	}
	return count
}

// InvalidateAgent removes all sessions for a given agent (e.g., on suspend).
func (sm *SessionManager) InvalidateAgent(agentID string) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	removed := 0
	for k := range sm.sessions {
		if k.agentID == agentID {
			delete(sm.sessions, k)
			removed++
		}
	}
	return removed
}

// Stop halts the reap goroutine. Safe to call multiple times.
func (sm *SessionManager) Stop() {
	sm.stopOnce.Do(func() {
		close(sm.stopCh)
	})
}

// reapLoop periodically removes sessions that exceed the idle timeout.
func (sm *SessionManager) reapLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sm.stopCh:
			return
		case now := <-ticker.C:
			sm.reap(now)
		}
	}
}

// reap removes expired sessions. Separated for testability.
func (sm *SessionManager) reap(now time.Time) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for k, s := range sm.sessions {
		if now.Sub(s.LastActivity) > sm.idleTimeout {
			sm.logger.Debug("session expired",
				"agent_id", k.agentID,
				"duration", now.Sub(s.StartedAt).Round(time.Second),
				"requests", s.RequestCount,
			)
			delete(sm.sessions, k)
		}
	}
}

// ReapNow runs the reaper immediately with the given time. Exported for testing.
func (sm *SessionManager) ReapNow(now time.Time) {
	sm.reap(now)
}
