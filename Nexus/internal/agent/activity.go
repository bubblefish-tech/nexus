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

// EntryTypeAgentActivity is the WAL entry type for agent activity events.
const EntryTypeAgentActivity = "agent_activity"

// ActivityEvent represents a single agent activity record.
type ActivityEvent struct {
	AgentID   string        `json:"agent_id"`
	Timestamp time.Time     `json:"timestamp"`
	EventType string        `json:"event_type"` // write, search, tool_call, broadcast, etc.
	Resource  string        `json:"resource"`   // tool name, endpoint, etc.
	LatencyMs int64         `json:"latency_ms"`
	Result    string        `json:"result"` // ok, error, denied
}

// ActivityLog stores agent activity events in memory with configurable retention.
type ActivityLog struct {
	mu        sync.RWMutex
	events    []ActivityEvent
	retention time.Duration
	logger    *slog.Logger
	stopCh    chan struct{}
	stopOnce  sync.Once
}

// NewActivityLog creates an activity log with the given retention period.
// A background goroutine prunes old events periodically.
func NewActivityLog(retention time.Duration, logger *slog.Logger) *ActivityLog {
	if retention <= 0 {
		retention = 7 * 24 * time.Hour // default 7 days
	}
	al := &ActivityLog{
		events:    make([]ActivityEvent, 0, 1024),
		retention: retention,
		logger:    logger,
		stopCh:    make(chan struct{}),
	}
	go al.pruneLoop()
	return al
}

// Record adds an activity event to the log.
func (al *ActivityLog) Record(event ActivityEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	al.mu.Lock()
	al.events = append(al.events, event)
	al.mu.Unlock()
}

// Query returns activity events for the given agent since the specified time,
// up to the given limit.
func (al *ActivityLog) Query(agentID string, since time.Time, limit int) []ActivityEvent {
	if limit <= 0 {
		limit = 100
	}

	al.mu.RLock()
	defer al.mu.RUnlock()

	var result []ActivityEvent
	for i := len(al.events) - 1; i >= 0 && len(result) < limit; i-- {
		e := al.events[i]
		if e.AgentID != agentID {
			continue
		}
		if !since.IsZero() && e.Timestamp.Before(since) {
			break // events are chronological; stop when we pass 'since'
		}
		result = append(result, e)
	}

	// Reverse to chronological order.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return result
}

// AllAgentSummaries returns a summary of recent activity per agent.
func (al *ActivityLog) AllAgentSummaries() map[string]AgentActivitySummary {
	al.mu.RLock()
	defer al.mu.RUnlock()

	summaries := make(map[string]AgentActivitySummary)
	for _, e := range al.events {
		s := summaries[e.AgentID]
		s.TotalEvents++
		if e.Timestamp.After(s.LastActivity) {
			s.LastActivity = e.Timestamp
		}
		summaries[e.AgentID] = s
	}
	return summaries
}

// AgentActivitySummary is a brief summary of an agent's activity.
type AgentActivitySummary struct {
	TotalEvents  int       `json:"total_events"`
	LastActivity time.Time `json:"last_activity"`
}

// Stop halts the prune goroutine.
func (al *ActivityLog) Stop() {
	al.stopOnce.Do(func() {
		close(al.stopCh)
	})
}

func (al *ActivityLog) pruneLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-al.stopCh:
			return
		case now := <-ticker.C:
			al.prune(now)
		}
	}
}

func (al *ActivityLog) prune(now time.Time) {
	cutoff := now.Add(-al.retention)

	al.mu.Lock()
	defer al.mu.Unlock()

	// Find first event after cutoff.
	idx := 0
	for idx < len(al.events) && al.events[idx].Timestamp.Before(cutoff) {
		idx++
	}

	if idx > 0 {
		al.logger.Debug("activity: pruned old events",
			"pruned", idx,
			"remaining", len(al.events)-idx,
		)
		al.events = al.events[idx:]
	}
}

// PruneNow runs the pruner immediately with the given time. Exported for testing.
func (al *ActivityLog) PruneNow(now time.Time) {
	al.prune(now)
}

// Len returns the total number of stored events (for testing).
func (al *ActivityLog) Len() int {
	al.mu.RLock()
	defer al.mu.RUnlock()
	return len(al.events)
}
