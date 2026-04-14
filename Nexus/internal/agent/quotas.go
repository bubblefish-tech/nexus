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
	"sync"
	"time"
)

// QuotaConfig defines per-agent rate and quota limits.
type QuotaConfig struct {
	RequestsPerMinute int   `toml:"requests_per_minute" json:"requests_per_minute"`
	BytesPerSecond    int64 `toml:"bytes_per_second" json:"bytes_per_second"`
	WritesPerDay      int64 `toml:"writes_per_day" json:"writes_per_day"`
	ToolCallsPerDay   int64 `toml:"tool_calls_per_day" json:"tool_calls_per_day"`
}

// QuotaState is the persisted state for an agent's daily quotas.
type QuotaState struct {
	AgentID       string    `json:"agent_id"`
	WritesUsed    int64     `json:"writes_used"`
	ToolCallsUsed int64     `json:"tool_calls_used"`
	DayStart      time.Time `json:"day_start"`
}

// QuotaManager tracks per-agent rate limits and day-bounded quotas.
// Quota state is kept in memory and persisted hourly.
type QuotaManager struct {
	mu       sync.Mutex
	configs  map[string]*QuotaConfig // agent_id → config
	states   map[string]*QuotaState  // agent_id → daily state
	rpmState map[string]*rpmWindow   // agent_id → RPM window
	logger   *slog.Logger
	stateDir string
	stopCh   chan struct{}
	stopOnce sync.Once
}

type rpmWindow struct {
	count       int
	windowStart time.Time
}

// NewQuotaManager creates a quota manager that persists state to stateDir.
func NewQuotaManager(stateDir string, logger *slog.Logger) *QuotaManager {
	qm := &QuotaManager{
		configs:  make(map[string]*QuotaConfig),
		states:   make(map[string]*QuotaState),
		rpmState: make(map[string]*rpmWindow),
		logger:   logger,
		stateDir: stateDir,
		stopCh:   make(chan struct{}),
	}

	// Load persisted state if available.
	qm.loadState()

	// Start hourly persistence and midnight reset.
	go qm.persistLoop()

	return qm
}

// SetConfig sets the quota configuration for an agent.
func (qm *QuotaManager) SetConfig(agentID string, cfg *QuotaConfig) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	qm.configs[agentID] = cfg
}

// CheckRequest checks all applicable rate limits and quotas for a request.
// Returns (allowed, quotaType, retryAfterSeconds).
func (qm *QuotaManager) CheckRequest(agentID string) (bool, string, int) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	cfg, ok := qm.configs[agentID]
	if !ok {
		return true, "", 0 // no config = unlimited
	}

	now := time.Now()

	// RPM check.
	if cfg.RequestsPerMinute > 0 {
		w, ok := qm.rpmState[agentID]
		if !ok {
			qm.rpmState[agentID] = &rpmWindow{count: 1, windowStart: now}
		} else if now.Sub(w.windowStart) >= time.Minute {
			w.count = 1
			w.windowStart = now
		} else if w.count >= cfg.RequestsPerMinute {
			remaining := time.Until(w.windowStart.Add(time.Minute))
			return false, "requests_per_minute", int(remaining.Seconds()) + 1
		} else {
			w.count++
		}
	}

	return true, "", 0
}

// CheckWrite checks the daily write quota. Call after CheckRequest passes.
func (qm *QuotaManager) CheckWrite(agentID string) (bool, string) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	cfg, ok := qm.configs[agentID]
	if !ok || cfg.WritesPerDay <= 0 {
		return true, ""
	}

	state := qm.getOrCreateState(agentID)
	qm.maybeResetDay(state)

	if state.WritesUsed >= cfg.WritesPerDay {
		return false, "writes_per_day"
	}

	state.WritesUsed++
	return true, ""
}

// CheckToolCall checks the daily tool call quota.
func (qm *QuotaManager) CheckToolCall(agentID string) (bool, string) {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	cfg, ok := qm.configs[agentID]
	if !ok || cfg.ToolCallsPerDay <= 0 {
		return true, ""
	}

	state := qm.getOrCreateState(agentID)
	qm.maybeResetDay(state)

	if state.ToolCallsUsed >= cfg.ToolCallsPerDay {
		return false, "tool_calls_per_day"
	}

	state.ToolCallsUsed++
	return true, ""
}

// GetState returns a snapshot of quota state for an agent.
func (qm *QuotaManager) GetState(agentID string) *QuotaState {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	state, ok := qm.states[agentID]
	if !ok {
		return nil
	}
	copy := *state
	return &copy
}

// Stop halts the persistence goroutine. Persists state one final time.
func (qm *QuotaManager) Stop() {
	qm.stopOnce.Do(func() {
		close(qm.stopCh)
		qm.saveState()
	})
}

func (qm *QuotaManager) getOrCreateState(agentID string) *QuotaState {
	state, ok := qm.states[agentID]
	if !ok {
		state = &QuotaState{
			AgentID:  agentID,
			DayStart: utcMidnight(time.Now()),
		}
		qm.states[agentID] = state
	}
	return state
}

func (qm *QuotaManager) maybeResetDay(state *QuotaState) {
	today := utcMidnight(time.Now())
	if !state.DayStart.Equal(today) {
		state.WritesUsed = 0
		state.ToolCallsUsed = 0
		state.DayStart = today
	}
}

func utcMidnight(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func (qm *QuotaManager) persistLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-qm.stopCh:
			return
		case <-ticker.C:
			qm.saveState()
		}
	}
}

func (qm *QuotaManager) statePath() string {
	return filepath.Join(qm.stateDir, "quotas.state")
}

func (qm *QuotaManager) saveState() {
	qm.mu.Lock()
	states := make([]QuotaState, 0, len(qm.states))
	for _, s := range qm.states {
		states = append(states, *s)
	}
	qm.mu.Unlock()

	data, err := json.Marshal(states)
	if err != nil {
		qm.logger.Error("quotas: marshal state", "error", err)
		return
	}

	tmpPath := qm.statePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		qm.logger.Error("quotas: write state", "error", err)
		return
	}
	if err := os.Rename(tmpPath, qm.statePath()); err != nil {
		qm.logger.Error("quotas: rename state", "error", err)
	}
}

func (qm *QuotaManager) loadState() {
	data, err := os.ReadFile(qm.statePath())
	if err != nil {
		return // no state file — fresh start
	}

	var states []QuotaState
	if err := json.Unmarshal(data, &states); err != nil {
		qm.logger.Warn("quotas: corrupt state file, starting fresh", "error", err)
		return
	}

	for i := range states {
		s := states[i]
		qm.states[s.AgentID] = &s
	}
}
