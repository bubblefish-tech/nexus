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

package supervisor

import (
	"fmt"
	"sync"
	"time"
)

// DegradationTier represents the current operating tier of the daemon.
type DegradationTier int

const (
	// TierNormal is the default operating mode — full functionality.
	TierNormal DegradationTier = iota
	// TierInstantRestart triggers an immediate restart (<3 failures in 60s).
	TierInstantRestart
	// TierReducedFeatures disables non-essential features (e.g., embedding).
	TierReducedFeatures
	// TierReadOnly puts the daemon in read-only mode — no writes accepted.
	TierReadOnly
	// TierEmergencyShutdown triggers a clean shutdown with no restart.
	TierEmergencyShutdown
)

// String returns a human-readable representation of the tier.
func (t DegradationTier) String() string {
	switch t {
	case TierNormal:
		return "T0:normal"
	case TierInstantRestart:
		return "T0:instant-restart"
	case TierReducedFeatures:
		return "T1:reduced-features"
	case TierReadOnly:
		return "T2:read-only"
	case TierEmergencyShutdown:
		return "T3:emergency-shutdown"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

// TierMachineConfig configures the degradation state machine.
type TierMachineConfig struct {
	// MaxT0Failures is the max failures in the T0 window before escalating.
	// Default: 3.
	MaxT0Failures int

	// T0Window is the sliding window for counting T0 failures.
	// Default: 60s.
	T0Window time.Duration

	// T1CooldownDuration is how long T1 lasts before checking if recovery
	// is possible. Default: 120s.
	T1CooldownDuration time.Duration

	// T2CooldownDuration is how long T2 lasts before escalating to T3.
	// Default: 300s.
	T2CooldownDuration time.Duration
}

// DefaultTierMachineConfig returns production defaults.
func DefaultTierMachineConfig() TierMachineConfig {
	return TierMachineConfig{
		MaxT0Failures:      3,
		T0Window:           60 * time.Second,
		T1CooldownDuration: 120 * time.Second,
		T2CooldownDuration: 300 * time.Second,
	}
}

// TierTransition records a tier change event.
type TierTransition struct {
	From      DegradationTier
	To        DegradationTier
	Reason    string
	Timestamp time.Time
}

// TierMachine is a thread-safe state machine for degradation tiers.
type TierMachine struct {
	mu          sync.Mutex
	current     DegradationTier
	cfg         TierMachineConfig
	failures    []time.Time
	transitions []TierTransition
	nowFunc     func() time.Time // injectable clock for testing
}

// NewTierMachine creates a new TierMachine starting at TierNormal.
func NewTierMachine(cfg TierMachineConfig) *TierMachine {
	if cfg.MaxT0Failures <= 0 {
		cfg.MaxT0Failures = 3
	}
	if cfg.T0Window <= 0 {
		cfg.T0Window = 60 * time.Second
	}
	if cfg.T1CooldownDuration <= 0 {
		cfg.T1CooldownDuration = 120 * time.Second
	}
	if cfg.T2CooldownDuration <= 0 {
		cfg.T2CooldownDuration = 300 * time.Second
	}
	return &TierMachine{
		current: TierNormal,
		cfg:     cfg,
		nowFunc: time.Now,
	}
}

// Current returns the current degradation tier.
func (m *TierMachine) Current() DegradationTier {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// Transitions returns a copy of all recorded tier transitions.
func (m *TierMachine) Transitions() []TierTransition {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TierTransition, len(m.transitions))
	copy(out, m.transitions)
	return out
}

// RecordFailure records a daemon failure and returns the new tier.
// The state machine transitions:
//
//	Normal → InstantRestart (first failure, within T0 window)
//	InstantRestart → ReducedFeatures (>= MaxT0Failures in T0Window)
//	ReducedFeatures → ReadOnly (failure while in reduced mode)
//	ReadOnly → EmergencyShutdown (failure while in read-only mode)
//	EmergencyShutdown → EmergencyShutdown (terminal state)
func (m *TierMachine) RecordFailure(reason string) DegradationTier {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.nowFunc()
	m.failures = append(m.failures, now)

	// Prune failures outside the T0 window.
	cutoff := now.Add(-m.cfg.T0Window)
	var recent []time.Time
	for _, f := range m.failures {
		if f.After(cutoff) {
			recent = append(recent, f)
		}
	}
	m.failures = recent

	prev := m.current

	switch m.current {
	case TierNormal:
		if len(m.failures) >= m.cfg.MaxT0Failures {
			m.current = TierReducedFeatures
		} else {
			m.current = TierInstantRestart
		}
	case TierInstantRestart:
		if len(m.failures) >= m.cfg.MaxT0Failures {
			m.current = TierReducedFeatures
		}
		// else stay at InstantRestart
	case TierReducedFeatures:
		m.current = TierReadOnly
	case TierReadOnly:
		m.current = TierEmergencyShutdown
	case TierEmergencyShutdown:
		// Terminal state — no further transitions.
	}

	if m.current != prev {
		m.transitions = append(m.transitions, TierTransition{
			From:      prev,
			To:        m.current,
			Reason:    reason,
			Timestamp: now,
		})
	}

	return m.current
}

// RecordSuccess records a successful daemon cycle. If currently at
// InstantRestart, transitions back to Normal.
func (m *TierMachine) RecordSuccess() DegradationTier {
	m.mu.Lock()
	defer m.mu.Unlock()

	prev := m.current

	if m.current == TierInstantRestart {
		m.current = TierNormal
		m.transitions = append(m.transitions, TierTransition{
			From:      prev,
			To:        m.current,
			Reason:    "daemon recovered",
			Timestamp: m.nowFunc(),
		})
	}

	return m.current
}

// Reset forces the machine back to TierNormal. Used for manual operator
// recovery.
func (m *TierMachine) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	prev := m.current
	m.current = TierNormal
	m.failures = nil

	if prev != TierNormal {
		m.transitions = append(m.transitions, TierTransition{
			From:      prev,
			To:        TierNormal,
			Reason:    "manual reset",
			Timestamp: m.nowFunc(),
		})
	}
}
