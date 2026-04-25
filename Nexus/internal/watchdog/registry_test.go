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

package watchdog

import (
	"io"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testConfig() RegistryConfig {
	return RegistryConfig{
		CheckInterval:  100 * time.Millisecond,
		DefaultTimeout: 500 * time.Millisecond,
	}
}

// ── SubsystemStatus.String ─────────────────────────────────────────────────

func TestSubsystemStatus_String(t *testing.T) {
	t.Helper()
	tests := []struct {
		status SubsystemStatus
		want   string
	}{
		{StatusHealthy, "healthy"},
		{StatusDegraded, "degraded"},
		{StatusUnknown, "unknown"},
		{SubsystemStatus(99), "SubsystemStatus(99)"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("SubsystemStatus(%d).String() = %q, want %q", int(tt.status), got, tt.want)
		}
	}
}

// ── New defaults ────────────────────────────────────────────────────────────

func TestNew_DefaultConfig(t *testing.T) {
	r := New(RegistryConfig{}, testLogger())
	if r.cfg.CheckInterval != 2*time.Second {
		t.Errorf("default CheckInterval = %v, want 2s", r.cfg.CheckInterval)
	}
	if r.cfg.DefaultTimeout != 10*time.Second {
		t.Errorf("default DefaultTimeout = %v, want 10s", r.cfg.DefaultTimeout)
	}
	if r.Count() != 0 {
		t.Errorf("Count() = %d, want 0", r.Count())
	}
}

// ── Register / Count ────────────────────────────────────────────────────────

func TestRegister_IncreasesCount(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", 10*time.Second)
	r.Register("wal", 5*time.Second)
	if r.Count() != 2 {
		t.Errorf("Count() = %d, want 2", r.Count())
	}
}

func TestRegister_DuplicatePreservesLastBeat(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", 10*time.Second)
	r.Beat("heartbeat")

	// Re-register with different timeout.
	r.Register("heartbeat", 20*time.Second)

	report := r.Status("heartbeat")
	if report == nil {
		t.Fatal("Status returned nil")
	}
	if report.Timeout != 20*time.Second {
		t.Errorf("Timeout = %v, want 20s", report.Timeout)
	}
	if report.Status != StatusHealthy {
		t.Errorf("Status = %v, want healthy (beat was preserved)", report.Status)
	}
	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1 (no duplicate)", r.Count())
	}
}

// ── Register with DefaultTimeouts ───────────────────────────────────────────

func TestRegister_UsesDefaultTimeouts(t *testing.T) {
	tests := []struct {
		name        string
		wantTimeout time.Duration
	}{
		{"heartbeat", 10 * time.Second},
		{"embedding", 30 * time.Second},
		{"wal", 5 * time.Second},
		{"mcp", 15 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(testConfig(), testLogger())
			r.Register(tt.name, 0) // 0 = use default
			report := r.Status(tt.name)
			if report == nil {
				t.Fatal("Status returned nil")
			}
			if report.Timeout != tt.wantTimeout {
				t.Errorf("Timeout = %v, want %v", report.Timeout, tt.wantTimeout)
			}
		})
	}
}

func TestRegister_UnknownNameUsesConfigDefault(t *testing.T) {
	cfg := testConfig()
	cfg.DefaultTimeout = 42 * time.Second
	r := New(cfg, testLogger())
	r.Register("custom-subsystem", 0)

	report := r.Status("custom-subsystem")
	if report == nil {
		t.Fatal("Status returned nil")
	}
	if report.Timeout != 42*time.Second {
		t.Errorf("Timeout = %v, want 42s", report.Timeout)
	}
}

// ── Unregister ──────────────────────────────────────────────────────────────

func TestUnregister_RemovesSubsystem(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", 10*time.Second)
	r.Register("wal", 5*time.Second)
	r.Unregister("heartbeat")

	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}
	if r.Status("heartbeat") != nil {
		t.Error("Status(heartbeat) should be nil after unregister")
	}
}

func TestUnregister_UnknownNameIsNoop(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Unregister("nonexistent") // should not panic
	if r.Count() != 0 {
		t.Errorf("Count() = %d, want 0", r.Count())
	}
}

// ── Beat ────────────────────────────────────────────────────────────────────

func TestBeat_UpdatesLastBeat(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", time.Second)

	before := time.Now()
	r.Beat("heartbeat")
	after := time.Now()

	report := r.Status("heartbeat")
	if report == nil {
		t.Fatal("Status returned nil")
	}
	if report.LastBeat.Before(before) || report.LastBeat.After(after) {
		t.Errorf("LastBeat = %v, want between %v and %v", report.LastBeat, before, after)
	}
}

func TestBeat_UnregisteredIsIgnored(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Beat("nonexistent") // should not panic
}

// ── Status ──────────────────────────────────────────────────────────────────

func TestStatus_UnknownBeforeBeat(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", time.Second)

	report := r.Status("heartbeat")
	if report == nil {
		t.Fatal("Status returned nil")
	}
	if report.Status != StatusUnknown {
		t.Errorf("Status = %v, want unknown (no beat yet)", report.Status)
	}
	if report.Age != 0 {
		t.Errorf("Age = %v, want 0 (no beat yet)", report.Age)
	}
}

func TestStatus_HealthyAfterBeat(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", time.Second)
	r.Beat("heartbeat")

	report := r.Status("heartbeat")
	if report == nil {
		t.Fatal("Status returned nil")
	}
	if report.Status != StatusHealthy {
		t.Errorf("Status = %v, want healthy", report.Status)
	}
}

func TestStatus_DegradedAfterTimeout(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", 50*time.Millisecond)
	r.Beat("heartbeat")

	// Wait for the timeout to expire.
	time.Sleep(100 * time.Millisecond)

	report := r.Status("heartbeat")
	if report == nil {
		t.Fatal("Status returned nil")
	}
	if report.Status != StatusDegraded {
		t.Errorf("Status = %v, want degraded", report.Status)
	}
	if report.Age < 50*time.Millisecond {
		t.Errorf("Age = %v, want >= 50ms", report.Age)
	}
}

func TestStatus_UnknownName(t *testing.T) {
	r := New(testConfig(), testLogger())
	if r.Status("nonexistent") != nil {
		t.Error("Status of unregistered subsystem should be nil")
	}
}

func TestStatus_ReportFields(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("wal", 5*time.Second)
	r.Beat("wal")

	report := r.Status("wal")
	if report.Name != "wal" {
		t.Errorf("Name = %q, want %q", report.Name, "wal")
	}
	if report.Timeout != 5*time.Second {
		t.Errorf("Timeout = %v, want 5s", report.Timeout)
	}
}

// ── AllStatus ───────────────────────────────────────────────────────────────

func TestAllStatus_SortedByName(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("wal", 5*time.Second)
	r.Register("heartbeat", 10*time.Second)
	r.Register("mcp", 15*time.Second)
	r.Register("embedding", 30*time.Second)

	reports := r.AllStatus()
	if len(reports) != 4 {
		t.Fatalf("AllStatus() returned %d reports, want 4", len(reports))
	}

	names := make([]string, len(reports))
	for i, r := range reports {
		names[i] = r.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("AllStatus() not sorted: %v", names)
	}
}

func TestAllStatus_EmptyRegistry(t *testing.T) {
	r := New(testConfig(), testLogger())
	reports := r.AllStatus()
	if len(reports) != 0 {
		t.Errorf("AllStatus() = %d reports, want 0", len(reports))
	}
}

// ── IsHealthy ───────────────────────────────────────────────────────────────

func TestIsHealthy_AllBeating(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", time.Second)
	r.Register("wal", time.Second)
	r.Beat("heartbeat")
	r.Beat("wal")

	if !r.IsHealthy() {
		t.Error("IsHealthy() = false, want true")
	}
}

func TestIsHealthy_OneDegraded(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", 50*time.Millisecond)
	r.Register("wal", time.Second)
	r.Beat("heartbeat")
	r.Beat("wal")

	time.Sleep(100 * time.Millisecond)
	// heartbeat timed out, wal still ok

	if r.IsHealthy() {
		t.Error("IsHealthy() = true, want false (heartbeat degraded)")
	}
}

func TestIsHealthy_UnknownCountsAsUnhealthy(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", time.Second)
	// Never beat — status is Unknown which is not Healthy.

	if r.IsHealthy() {
		t.Error("IsHealthy() = true, want false (unknown = not healthy)")
	}
}

func TestIsHealthy_EmptyRegistryIsHealthy(t *testing.T) {
	r := New(testConfig(), testLogger())
	if !r.IsHealthy() {
		t.Error("IsHealthy() = false, want true (no subsystems = healthy)")
	}
}

// ── DegradedSubsystems ──────────────────────────────────────────────────────

func TestDegradedSubsystems_ReturnsOnlyDegraded(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("fast", 50*time.Millisecond)
	r.Register("slow", time.Second)
	r.Beat("fast")
	r.Beat("slow")

	time.Sleep(100 * time.Millisecond)

	degraded := r.DegradedSubsystems()
	if len(degraded) != 1 {
		t.Fatalf("DegradedSubsystems() = %v, want [fast]", degraded)
	}
	if degraded[0] != "fast" {
		t.Errorf("degraded[0] = %q, want %q", degraded[0], "fast")
	}
}

func TestDegradedSubsystems_SortedByName(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("zzz", 50*time.Millisecond)
	r.Register("aaa", 50*time.Millisecond)
	r.Beat("zzz")
	r.Beat("aaa")

	time.Sleep(100 * time.Millisecond)

	degraded := r.DegradedSubsystems()
	if !sort.StringsAreSorted(degraded) {
		t.Errorf("DegradedSubsystems() not sorted: %v", degraded)
	}
}

func TestDegradedSubsystems_EmptyWhenAllHealthy(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", time.Second)
	r.Beat("heartbeat")

	degraded := r.DegradedSubsystems()
	if len(degraded) != 0 {
		t.Errorf("DegradedSubsystems() = %v, want empty", degraded)
	}
}

// ── OnDegraded callback ────────────────────────────────────────────────────

func TestOnDegraded_CallbackFired(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", 100*time.Millisecond)
	r.Beat("heartbeat")

	var called atomic.Int32
	var calledName atomic.Value
	r.OnDegraded(func(name string, age time.Duration) {
		called.Add(1)
		calledName.Store(name)
	})

	r.Start()
	defer func() {
		r.Shutdown()
		r.Stop()
	}()

	// Wait for timeout + at least one check.
	time.Sleep(300 * time.Millisecond)

	if called.Load() == 0 {
		t.Error("OnDegraded callback was never called")
	}
	if name, ok := calledName.Load().(string); !ok || name != "heartbeat" {
		t.Errorf("OnDegraded name = %q, want %q", name, "heartbeat")
	}
}

// ── Shutdown ────────────────────────────────────────────────────────────────

func TestShutdown_SuppressesChecks(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", 100*time.Millisecond)
	r.Beat("heartbeat")

	var degradedCount atomic.Int32
	r.OnDegraded(func(name string, age time.Duration) {
		degradedCount.Add(1)
	})

	r.Shutdown() // before start — shutdown flag takes effect
	r.Start()

	time.Sleep(300 * time.Millisecond) // past timeout
	r.Stop()

	if degradedCount.Load() != 0 {
		t.Errorf("OnDegraded called %d times during shutdown, want 0", degradedCount.Load())
	}
}

// ── Start / Stop lifecycle ──────────────────────────────────────────────────

func TestStartStop_NoLeak(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", time.Second)
	r.Start()

	// Beat a few times.
	for i := 0; i < 5; i++ {
		r.Beat("heartbeat")
		time.Sleep(20 * time.Millisecond)
	}

	r.Shutdown()
	r.Stop()

	// Verify double-stop is safe.
	r.Stop()
}

func TestStart_HealthyWithContinuousBeats(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", 200*time.Millisecond)
	r.Start()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10; i++ {
			r.Beat("heartbeat")
			time.Sleep(50 * time.Millisecond)
		}
	}()
	<-done

	if !r.IsHealthy() {
		t.Error("IsHealthy() = false, want true after continuous beats")
	}

	r.Shutdown()
	r.Stop()
}

// ── Concurrent access ──────────────────────────────────────────────────────

func TestConcurrent_RegisterBeatStatus(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Start()
	defer func() {
		r.Shutdown()
		r.Stop()
	}()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		name := "sub-" + string(rune('a'+i))
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			r.Register(n, 200*time.Millisecond)
			for j := 0; j < 20; j++ {
				r.Beat(n)
				time.Sleep(5 * time.Millisecond)
			}
			_ = r.Status(n)
			_ = r.AllStatus()
			_ = r.IsHealthy()
			_ = r.DegradedSubsystems()
		}(name)
	}
	wg.Wait()

	if r.Count() != 10 {
		t.Errorf("Count() = %d, want 10", r.Count())
	}
}

// ── DefaultTimeouts map ─────────────────────────────────────────────────────

func TestDefaultTimeouts_ContainsExpectedKeys(t *testing.T) {
	expected := map[string]time.Duration{
		"heartbeat": 10 * time.Second,
		"embedding": 30 * time.Second,
		"wal":       5 * time.Second,
		"mcp":       15 * time.Second,
	}
	for name, want := range expected {
		got, ok := DefaultTimeouts[name]
		if !ok {
			t.Errorf("DefaultTimeouts missing key %q", name)
			continue
		}
		if got != want {
			t.Errorf("DefaultTimeouts[%q] = %v, want %v", name, got, want)
		}
	}
	if len(DefaultTimeouts) != len(expected) {
		t.Errorf("DefaultTimeouts has %d keys, want %d", len(DefaultTimeouts), len(expected))
	}
}

// ── Recovery from degraded ─────────────────────────────────────────────────

func TestRecovery_DegradedToHealthy(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("heartbeat", 100*time.Millisecond)
	r.Beat("heartbeat")

	// Let it degrade.
	time.Sleep(150 * time.Millisecond)
	report := r.Status("heartbeat")
	if report.Status != StatusDegraded {
		t.Fatalf("expected degraded before recovery, got %v", report.Status)
	}

	// Beat again to recover.
	r.Beat("heartbeat")
	report = r.Status("heartbeat")
	if report.Status != StatusHealthy {
		t.Errorf("Status = %v after recovery beat, want healthy", report.Status)
	}
}

// ── Multiple subsystems mixed status ────────────────────────────────────────

func TestMixedStatus_MultipleSubsystems(t *testing.T) {
	r := New(testConfig(), testLogger())
	r.Register("wal", 50*time.Millisecond)
	r.Register("heartbeat", time.Second)
	r.Register("mcp", time.Second)

	// Only beat heartbeat and mcp, let wal never beat.
	r.Beat("heartbeat")
	r.Beat("mcp")
	// wal is StatusUnknown

	reports := r.AllStatus()
	statusMap := make(map[string]SubsystemStatus)
	for _, rp := range reports {
		statusMap[rp.Name] = rp.Status
	}

	if statusMap["heartbeat"] != StatusHealthy {
		t.Errorf("heartbeat = %v, want healthy", statusMap["heartbeat"])
	}
	if statusMap["mcp"] != StatusHealthy {
		t.Errorf("mcp = %v, want healthy", statusMap["mcp"])
	}
	if statusMap["wal"] != StatusUnknown {
		t.Errorf("wal = %v, want unknown", statusMap["wal"])
	}
}
