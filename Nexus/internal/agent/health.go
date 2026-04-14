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

// HealthState represents an agent's liveness state.
type HealthState string

const (
	HealthActive   HealthState = "active"   // seen within last 5 minutes
	HealthStale    HealthState = "stale"    // no activity for 5 minutes
	HealthInactive HealthState = "inactive" // no activity for 1 hour
	HealthDormant  HealthState = "dormant"  // no activity for 24 hours
)

// Thresholds for state transitions.
const (
	staleThreshold    = 5 * time.Minute
	inactiveThreshold = 1 * time.Hour
	dormantThreshold  = 24 * time.Hour
)

// HealthInfo represents the health status of a single agent.
type HealthInfo struct {
	AgentID      string      `json:"agent_id"`
	Name         string      `json:"name"`
	Health       HealthState `json:"health"`
	Status       Status      `json:"status"` // registration status
	LastSeenAt   time.Time   `json:"last_seen_at"`
	SessionCount int         `json:"session_count"`
}

// HealthTracker monitors agent liveness based on last-seen timestamps.
type HealthTracker struct {
	mu          sync.RWMutex
	lastSeen    map[string]time.Time // agent_id → last activity time
	logger      *slog.Logger
	stopCh      chan struct{}
	stopOnce    sync.Once
	onTransition func(agentID string, from, to HealthState) // optional callback
}

// NewHealthTracker creates a health tracker.
func NewHealthTracker(logger *slog.Logger) *HealthTracker {
	return &HealthTracker{
		lastSeen: make(map[string]time.Time),
		logger:   logger,
		stopCh:   make(chan struct{}),
	}
}

// SetTransitionCallback registers a function called on state transitions.
// Used for audit WAL logging.
func (ht *HealthTracker) SetTransitionCallback(fn func(agentID string, from, to HealthState)) {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	ht.onTransition = fn
}

// Touch updates the last-seen time for an agent.
func (ht *HealthTracker) Touch(agentID string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	ht.lastSeen[agentID] = time.Now()
}

// Heartbeat is an explicit heartbeat from an agent. Same as Touch
// but semantically distinct.
func (ht *HealthTracker) Heartbeat(agentID string) {
	ht.Touch(agentID)
}

// State returns the current health state for an agent.
func (ht *HealthTracker) State(agentID string) HealthState {
	ht.mu.RLock()
	lastSeen, ok := ht.lastSeen[agentID]
	ht.mu.RUnlock()

	if !ok {
		return HealthDormant
	}

	return classifyHealth(time.Since(lastSeen))
}

// AllStates returns health states for all tracked agents.
func (ht *HealthTracker) AllStates() map[string]HealthState {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	states := make(map[string]HealthState, len(ht.lastSeen))
	now := time.Now()
	for id, ls := range ht.lastSeen {
		states[id] = classifyHealth(now.Sub(ls))
	}
	return states
}

// Stop halts the tracker.
func (ht *HealthTracker) Stop() {
	ht.stopOnce.Do(func() {
		close(ht.stopCh)
	})
}

func classifyHealth(elapsed time.Duration) HealthState {
	switch {
	case elapsed >= dormantThreshold:
		return HealthDormant
	case elapsed >= inactiveThreshold:
		return HealthInactive
	case elapsed >= staleThreshold:
		return HealthStale
	default:
		return HealthActive
	}
}
