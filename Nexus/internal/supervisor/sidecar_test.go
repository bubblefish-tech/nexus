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
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ── Tier Machine Tests ─────────────────────────────────────────────────────

func TestTierMachine_StartsNormal(t *testing.T) {
	m := NewTierMachine(DefaultTierMachineConfig())
	if m.Current() != TierNormal {
		t.Errorf("want TierNormal, got %s", m.Current())
	}
}

func TestTierMachine_SingleFailureToInstantRestart(t *testing.T) {
	m := NewTierMachine(DefaultTierMachineConfig())
	tier := m.RecordFailure("crash")
	if tier != TierInstantRestart {
		t.Errorf("want TierInstantRestart, got %s", tier)
	}
}

func TestTierMachine_ThreeFailuresEscalateToReduced(t *testing.T) {
	cfg := DefaultTierMachineConfig()
	cfg.MaxT0Failures = 3
	m := NewTierMachine(cfg)
	m.RecordFailure("crash 1")
	m.RecordFailure("crash 2")
	tier := m.RecordFailure("crash 3")
	if tier != TierReducedFeatures {
		t.Errorf("want TierReducedFeatures, got %s", tier)
	}
}

func TestTierMachine_ReducedToReadOnly(t *testing.T) {
	cfg := DefaultTierMachineConfig()
	cfg.MaxT0Failures = 1
	m := NewTierMachine(cfg)
	m.RecordFailure("crash") // Normal → ReducedFeatures
	tier := m.RecordFailure("crash")
	if tier != TierReadOnly {
		t.Errorf("want TierReadOnly, got %s", tier)
	}
}

func TestTierMachine_ReadOnlyToEmergencyShutdown(t *testing.T) {
	cfg := DefaultTierMachineConfig()
	cfg.MaxT0Failures = 1
	m := NewTierMachine(cfg)
	m.RecordFailure("crash")   // → ReducedFeatures
	m.RecordFailure("crash 2") // → ReadOnly
	tier := m.RecordFailure("crash 3")
	if tier != TierEmergencyShutdown {
		t.Errorf("want TierEmergencyShutdown, got %s", tier)
	}
}

func TestTierMachine_EmergencyIsTerminal(t *testing.T) {
	cfg := DefaultTierMachineConfig()
	cfg.MaxT0Failures = 1
	m := NewTierMachine(cfg)
	m.RecordFailure("crash")
	m.RecordFailure("crash")
	m.RecordFailure("crash")
	tier := m.RecordFailure("again")
	if tier != TierEmergencyShutdown {
		t.Errorf("want TierEmergencyShutdown, got %s", tier)
	}
}

func TestTierMachine_SuccessRecovery(t *testing.T) {
	m := NewTierMachine(DefaultTierMachineConfig())
	m.RecordFailure("crash") // → InstantRestart
	tier := m.RecordSuccess()
	if tier != TierNormal {
		t.Errorf("want TierNormal after recovery, got %s", tier)
	}
}

func TestTierMachine_SuccessNoOpWhenNormal(t *testing.T) {
	m := NewTierMachine(DefaultTierMachineConfig())
	tier := m.RecordSuccess()
	if tier != TierNormal {
		t.Errorf("want TierNormal, got %s", tier)
	}
}

func TestTierMachine_Reset(t *testing.T) {
	cfg := DefaultTierMachineConfig()
	cfg.MaxT0Failures = 1
	m := NewTierMachine(cfg)
	m.RecordFailure("crash")
	m.RecordFailure("crash")
	m.Reset()
	if m.Current() != TierNormal {
		t.Errorf("want TierNormal after reset, got %s", m.Current())
	}
}

func TestTierMachine_TransitionsRecorded(t *testing.T) {
	m := NewTierMachine(DefaultTierMachineConfig())
	m.RecordFailure("boom")
	transitions := m.Transitions()
	if len(transitions) != 1 {
		t.Fatalf("want 1 transition, got %d", len(transitions))
	}
	if transitions[0].From != TierNormal || transitions[0].To != TierInstantRestart {
		t.Errorf("unexpected transition: %v → %v", transitions[0].From, transitions[0].To)
	}
	if transitions[0].Reason != "boom" {
		t.Errorf("want reason 'boom', got %q", transitions[0].Reason)
	}
}

func TestTierMachine_WindowExpiry(t *testing.T) {
	cfg := DefaultTierMachineConfig()
	cfg.MaxT0Failures = 3
	cfg.T0Window = 100 * time.Millisecond
	m := NewTierMachine(cfg)

	now := time.Now()
	m.nowFunc = func() time.Time { return now }

	m.RecordFailure("old 1")
	m.RecordFailure("old 2")

	// Advance past the window.
	now = now.Add(200 * time.Millisecond)

	tier := m.RecordFailure("new 1")
	// Should NOT be ReducedFeatures because the old failures expired.
	if tier != TierInstantRestart {
		t.Errorf("want TierInstantRestart (old failures expired), got %s", tier)
	}
}

func TestTierString_AllValues(t *testing.T) {
	tests := []struct {
		tier DegradationTier
		want string
	}{
		{TierNormal, "T0:normal"},
		{TierInstantRestart, "T0:instant-restart"},
		{TierReducedFeatures, "T1:reduced-features"},
		{TierReadOnly, "T2:read-only"},
		{TierEmergencyShutdown, "T3:emergency-shutdown"},
		{DegradationTier(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.tier.String(); got != tt.want {
			t.Errorf("DegradationTier(%d).String() = %q, want %q", int(tt.tier), got, tt.want)
		}
	}
}

func TestTierMachine_DefaultConfigDefaults(t *testing.T) {
	m := NewTierMachine(TierMachineConfig{})
	if m.cfg.MaxT0Failures != 3 {
		t.Errorf("want default MaxT0Failures=3, got %d", m.cfg.MaxT0Failures)
	}
	if m.cfg.T0Window != 60*time.Second {
		t.Errorf("want default T0Window=60s, got %v", m.cfg.T0Window)
	}
}

// ── Pipe Tests ─────────────────────────────────────────────────────────────

func TestPipePair_SendRecv(t *testing.T) {
	a, b := pipePair()
	defer a.Close()
	defer b.Close()

	msg := PipeMsg{
		Type:      PipeMsgHeartbeat,
		Timestamp: time.Now().Truncate(time.Second),
	}

	go func() {
		if err := a.Send(msg); err != nil {
			t.Errorf("Send: %v", err)
		}
	}()

	got, err := b.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if got.Type != PipeMsgHeartbeat {
		t.Errorf("want heartbeat, got %s", got.Type)
	}
}

func TestPipePair_CloseReturnsErrClosed(t *testing.T) {
	a, b := pipePair()
	a.Close()

	_, err := b.Recv()
	if !errors.Is(err, ErrPipeClosed) {
		t.Errorf("want ErrPipeClosed, got %v", err)
	}
}

func TestPipePair_BidirectionalMessages(t *testing.T) {
	a, b := pipePair()
	defer a.Close()
	defer b.Close()

	go func() {
		_ = a.Send(PipeMsg{Type: PipeMsgReady, Timestamp: time.Now()})
	}()

	msg, err := b.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if msg.Type != PipeMsgReady {
		t.Errorf("want ready, got %s", msg.Type)
	}

	go func() {
		_ = b.Send(PipeMsg{Type: PipeMsgHeartbeat, Timestamp: time.Now()})
	}()

	msg2, err := a.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if msg2.Type != PipeMsgHeartbeat {
		t.Errorf("want heartbeat, got %s", msg2.Type)
	}
}

func TestPipeMsg_PayloadRoundtrip(t *testing.T) {
	a, b := pipePair()
	defer a.Close()
	defer b.Close()

	msg := PipeMsg{
		Type:      PipeMsgError,
		Payload:   "something went wrong",
		Timestamp: time.Now().Truncate(time.Second),
	}

	go func() {
		_ = a.Send(msg)
	}()

	got, err := b.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if got.Payload != "something went wrong" {
		t.Errorf("want payload 'something went wrong', got %q", got.Payload)
	}
}

// ── Sidecar Tests ──────────────────────────────────────────────────────────

func fastSidecarConfig() SidecarConfig {
	return SidecarConfig{
		TierConfig: TierMachineConfig{
			MaxT0Failures:      3,
			T0Window:           5 * time.Second,
			T1CooldownDuration: 100 * time.Millisecond,
			T2CooldownDuration: 100 * time.Millisecond,
		},
		HeartbeatInterval: 50 * time.Millisecond,
		HeartbeatTimeout:  200 * time.Millisecond,
		MaxRestartDelay:   100 * time.Millisecond,
	}
}

func TestSidecar_DaemonExitsCleanly(t *testing.T) {
	spawner := func(ctx context.Context, tier DegradationTier, pipe Pipe) error {
		_ = pipe.Send(PipeMsg{Type: PipeMsgReady, Timestamp: time.Now()})
		<-ctx.Done()
		return nil
	}

	s := NewSidecar(fastSidecarConfig(), spawner, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- s.Run(context.Background())
	}()

	time.Sleep(100 * time.Millisecond)
	s.Stop()

	err := <-done
	if err != nil {
		t.Errorf("want nil error on clean stop, got %v", err)
	}
}

func TestSidecar_DaemonCrashEscalates(t *testing.T) {
	var calls atomic.Int32
	spawner := func(ctx context.Context, tier DegradationTier, pipe Pipe) error {
		calls.Add(1)
		return errors.New("daemon crashed")
	}

	cfg := fastSidecarConfig()
	cfg.TierConfig.MaxT0Failures = 1
	s := NewSidecar(cfg, spawner, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.Run(ctx)
	if err == nil || err.Error() != "emergency shutdown: too many failures" {
		t.Errorf("want emergency shutdown error, got %v", err)
	}

	// Should have reached EmergencyShutdown.
	if s.CurrentTier() != TierEmergencyShutdown {
		t.Errorf("want TierEmergencyShutdown, got %s", s.CurrentTier())
	}
}

func TestSidecar_HeartbeatKeepsAlive(t *testing.T) {
	spawner := func(ctx context.Context, tier DegradationTier, pipe Pipe) error {
		_ = pipe.Send(PipeMsg{Type: PipeMsgReady, Timestamp: time.Now()})

		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				_ = pipe.Send(PipeMsg{Type: PipeMsgHeartbeat, Timestamp: time.Now()})
			}
		}
	}

	s := NewSidecar(fastSidecarConfig(), spawner, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- s.Run(context.Background())
	}()

	// Let it run for a bit to verify no heartbeat timeout.
	time.Sleep(500 * time.Millisecond)
	s.Stop()

	err := <-done
	if err != nil {
		t.Errorf("want nil error, got %v", err)
	}
	if s.CurrentTier() != TierNormal {
		t.Errorf("want TierNormal, got %s", s.CurrentTier())
	}
}

func TestSidecar_TierPassedToSpawner(t *testing.T) {
	var mu sync.Mutex
	var seenTiers []DegradationTier
	var calls atomic.Int32

	spawner := func(ctx context.Context, tier DegradationTier, pipe Pipe) error {
		mu.Lock()
		seenTiers = append(seenTiers, tier)
		mu.Unlock()
		n := calls.Add(1)
		if n <= 2 {
			return errors.New("fail")
		}
		<-ctx.Done()
		return nil
	}

	cfg := fastSidecarConfig()
	cfg.TierConfig.MaxT0Failures = 3
	cfg.MaxRestartDelay = 50 * time.Millisecond // fast backoff for test
	s := NewSidecar(cfg, spawner, discardLogger())
	s.restartDelay = 10 * time.Millisecond // start with very short delay

	done := make(chan error, 1)
	go func() {
		done <- s.Run(context.Background())
	}()

	time.Sleep(2 * time.Second)
	s.Stop()

	<-done

	mu.Lock()
	tiers := make([]DegradationTier, len(seenTiers))
	copy(tiers, seenTiers)
	mu.Unlock()

	// First call should be at TierNormal, second at TierInstantRestart.
	if len(tiers) < 2 {
		t.Fatalf("want at least 2 spawner calls, got %d", len(tiers))
	}
	if tiers[0] != TierNormal {
		t.Errorf("first call tier: want TierNormal, got %s", tiers[0])
	}
	if tiers[1] != TierInstantRestart {
		t.Errorf("second call tier: want TierInstantRestart, got %s", tiers[1])
	}
}

func TestSidecar_ContextCancellation(t *testing.T) {
	spawner := func(ctx context.Context, tier DegradationTier, pipe Pipe) error {
		<-ctx.Done()
		return ctx.Err()
	}

	s := NewSidecar(fastSidecarConfig(), spawner, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := s.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want DeadlineExceeded, got %v", err)
	}
}

func TestSidecar_DefaultConfig(t *testing.T) {
	cfg := DefaultSidecarConfig()
	if cfg.HeartbeatInterval != 5*time.Second {
		t.Errorf("want 5s heartbeat interval, got %v", cfg.HeartbeatInterval)
	}
	if cfg.HeartbeatTimeout != 15*time.Second {
		t.Errorf("want 15s heartbeat timeout, got %v", cfg.HeartbeatTimeout)
	}
	if cfg.MaxRestartDelay != 60*time.Second {
		t.Errorf("want 60s max restart delay, got %v", cfg.MaxRestartDelay)
	}
}

func TestSidecar_StopIsIdempotent(t *testing.T) {
	spawner := func(ctx context.Context, tier DegradationTier, pipe Pipe) error {
		<-ctx.Done()
		return nil
	}

	s := NewSidecar(fastSidecarConfig(), spawner, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- s.Run(context.Background())
	}()

	time.Sleep(50 * time.Millisecond)
	s.Stop()
	s.Stop() // Should not panic or block.
	<-done
}

func TestSidecar_MachineAccessor(t *testing.T) {
	spawner := func(ctx context.Context, tier DegradationTier, pipe Pipe) error {
		<-ctx.Done()
		return nil
	}

	s := NewSidecar(fastSidecarConfig(), spawner, discardLogger())
	if s.Machine() == nil {
		t.Error("Machine() should not be nil")
	}
	if s.Machine() != s.machine {
		t.Error("Machine() should return the same instance")
	}
}

func TestTierMachine_ResetClearsFailures(t *testing.T) {
	cfg := DefaultTierMachineConfig()
	cfg.MaxT0Failures = 2
	m := NewTierMachine(cfg)
	m.RecordFailure("crash 1")
	m.Reset()
	// After reset, a single failure should only go to InstantRestart, not Reduced.
	tier := m.RecordFailure("crash after reset")
	if tier != TierInstantRestart {
		t.Errorf("want TierInstantRestart after reset, got %s", tier)
	}
}

func TestTierMachine_ResetFromNormalNoTransition(t *testing.T) {
	m := NewTierMachine(DefaultTierMachineConfig())
	m.Reset()
	if len(m.Transitions()) != 0 {
		t.Errorf("want 0 transitions from normal reset, got %d", len(m.Transitions()))
	}
}
