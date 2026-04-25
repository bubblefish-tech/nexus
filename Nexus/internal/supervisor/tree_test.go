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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fastTreeConfig() TreeConfig {
	return TreeConfig{
		MaxRestartIntensity: 10,
		RestartWindow:       5 * time.Second,
		ShutdownTimeout:     2 * time.Second,
	}
}

func fastBreakerConfig() *BreakerConfig {
	return &BreakerConfig{
		MaxFailures: 5,
		Window:      5 * time.Second,
		OpenTimeout: 200 * time.Millisecond,
	}
}

// ── Basic Lifecycle ────────────────────────────────────────────────────────

func TestTree_StartAndStop(t *testing.T) {
	var started atomic.Int32

	specs := []ChildSpec{
		{
			Name: "worker",
			Start: func(ctx context.Context) error {
				started.Add(1)
				<-ctx.Done()
				return nil
			},
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(100 * time.Millisecond)
	if started.Load() < 1 {
		t.Error("child was not started")
	}

	tree.Stop()
	err := <-done
	if err != nil {
		t.Errorf("want nil, got %v", err)
	}
}

func TestTree_MultipleChildren(t *testing.T) {
	var count atomic.Int32

	specs := []ChildSpec{
		{
			Name:          "child-a",
			Start:         func(ctx context.Context) error { count.Add(1); <-ctx.Done(); return nil },
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		},
		{
			Name:          "child-b",
			Start:         func(ctx context.Context) error { count.Add(1); <-ctx.Done(); return nil },
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		},
		{
			Name:          "child-c",
			Start:         func(ctx context.Context) error { count.Add(1); <-ctx.Done(); return nil },
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(100 * time.Millisecond)
	if got := count.Load(); got != 3 {
		t.Errorf("want 3 children started, got %d", got)
	}

	tree.Stop()
	<-done
}

// ── Restart Policies ───────────────────────────────────────────────────────

func TestTree_RestartOnFailure(t *testing.T) {
	var calls atomic.Int32

	specs := []ChildSpec{
		{
			Name: "failing",
			Start: func(ctx context.Context) error {
				n := calls.Add(1)
				if n <= 2 {
					return errors.New("boom")
				}
				<-ctx.Done()
				return nil
			},
			RestartPolicy: RestartOnFailure,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(500 * time.Millisecond)
	if got := calls.Load(); got < 3 {
		t.Errorf("want at least 3 calls (2 failures + 1 success), got %d", got)
	}

	tree.Stop()
	<-done
}

func TestTree_RestartNever(t *testing.T) {
	var calls atomic.Int32

	specs := []ChildSpec{
		{
			Name: "once",
			Start: func(ctx context.Context) error {
				calls.Add(1)
				return errors.New("fail")
			},
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(300 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Errorf("want exactly 1 call, got %d", got)
	}

	tree.Stop()
	<-done
}

func TestTree_RestartAlways(t *testing.T) {
	var calls atomic.Int32

	specs := []ChildSpec{
		{
			Name: "always",
			Start: func(ctx context.Context) error {
				n := calls.Add(1)
				if n < 3 {
					return nil // clean exit, should still restart
				}
				<-ctx.Done()
				return nil
			},
			RestartPolicy: RestartAlways,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(500 * time.Millisecond)
	if got := calls.Load(); got < 3 {
		t.Errorf("want at least 3 calls, got %d", got)
	}

	tree.Stop()
	<-done
}

// ── Circuit Breaker ────────────────────────────────────────────────────────

func TestTree_CircuitBreakerOpens(t *testing.T) {
	var calls atomic.Int32

	bcfg := &BreakerConfig{
		MaxFailures: 3,
		Window:      5 * time.Second,
		OpenTimeout: 5 * time.Second, // long enough we won't see a half-open retry
	}

	specs := []ChildSpec{
		{
			Name: "fragile",
			Start: func(ctx context.Context) error {
				calls.Add(1)
				return errors.New("crash")
			},
			RestartPolicy: RestartOnFailure,
			BreakerConfig: bcfg,
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	// Let it crash enough times to trip the breaker.
	time.Sleep(1 * time.Second)

	// After breaker opens, restarts should stop.
	countBefore := calls.Load()
	time.Sleep(500 * time.Millisecond)
	countAfter := calls.Load()

	// There should be no new calls once breaker is open.
	if countAfter > countBefore+1 {
		t.Errorf("expected restarts to stop after breaker opens, got %d more calls", countAfter-countBefore)
	}

	tree.Stop()
	<-done
}

func TestTree_CircuitBreakerRecovery(t *testing.T) {
	var calls atomic.Int32

	bcfg := &BreakerConfig{
		MaxFailures: 2,
		Window:      5 * time.Second,
		OpenTimeout: 200 * time.Millisecond, // short for test
	}

	specs := []ChildSpec{
		{
			Name: "recoverable",
			Start: func(ctx context.Context) error {
				n := calls.Add(1)
				if n <= 3 {
					return errors.New("crash")
				}
				<-ctx.Done()
				return nil
			},
			RestartPolicy: RestartOnFailure,
			BreakerConfig: bcfg,
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	// Wait long enough for breaker to open, half-open, and restart.
	time.Sleep(2 * time.Second)

	if got := calls.Load(); got < 4 {
		t.Errorf("want at least 4 calls (3 fail + 1 success after recovery), got %d", got)
	}

	tree.Stop()
	<-done
}

// ── Restart Intensity ──────────────────────────────────────────────────────

func TestTree_RestartIntensityExceeded(t *testing.T) {
	specs := []ChildSpec{
		{
			Name: "crasher",
			Start: func(ctx context.Context) error {
				return errors.New("always fails")
			},
			RestartPolicy: RestartOnFailure,
			BreakerConfig: &BreakerConfig{
				MaxFailures: 100, // very high so breaker doesn't trip first
				Window:      60 * time.Second,
				OpenTimeout: 30 * time.Second,
			},
		},
	}

	cfg := TreeConfig{
		MaxRestartIntensity: 3,
		RestartWindow:       5 * time.Second,
		ShutdownTimeout:     2 * time.Second,
	}

	tree := NewTree(cfg, specs, discardLogger())

	err := tree.Run(context.Background())
	if err == nil || !errors.Is(err, errors.New("")) && err.Error() != "supervision tree: restart intensity exceeded" {
		// Check the error message directly.
		if err == nil {
			t.Error("want restart intensity error, got nil")
		} else if err.Error() != "supervision tree: restart intensity exceeded" {
			t.Errorf("want restart intensity error, got %v", err)
		}
	}
}

// ── Context Cancellation ───────────────────────────────────────────────────

func TestTree_ContextCancellation(t *testing.T) {
	specs := []ChildSpec{
		{
			Name: "waiter",
			Start: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := tree.Run(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want DeadlineExceeded, got %v", err)
	}
}

// ── Status ─────────────────────────────────────────────────────────────────

func TestTree_Status(t *testing.T) {
	specs := []ChildSpec{
		{
			Name: "running-child",
			Start: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
			RestartPolicy: RestartAlways,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(100 * time.Millisecond)

	statuses := tree.Status()
	if len(statuses) != 1 {
		t.Fatalf("want 1 status, got %d", len(statuses))
	}
	if statuses[0].Name != "running-child" {
		t.Errorf("want name 'running-child', got %q", statuses[0].Name)
	}
	if !statuses[0].Running {
		t.Error("expected child to be running")
	}
	if statuses[0].BreakerState != "closed" {
		t.Errorf("want breaker closed, got %q", statuses[0].BreakerState)
	}

	tree.Stop()
	<-done
}

// ── Stop ───────────────────────────────────────────────────────────────────

func TestTree_StopIsIdempotent(t *testing.T) {
	specs := []ChildSpec{
		{
			Name: "worker",
			Start: func(ctx context.Context) error {
				<-ctx.Done()
				return nil
			},
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(50 * time.Millisecond)
	tree.Stop()
	tree.Stop() // Should not panic.
	<-done
}

// ── Config Defaults ────────────────────────────────────────────────────────

func TestDefaultTreeConfig(t *testing.T) {
	cfg := DefaultTreeConfig()
	if cfg.MaxRestartIntensity != 10 {
		t.Errorf("want 10, got %d", cfg.MaxRestartIntensity)
	}
	if cfg.RestartWindow != 60*time.Second {
		t.Errorf("want 60s, got %v", cfg.RestartWindow)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("want 30s, got %v", cfg.ShutdownTimeout)
	}
}

func TestDefaultBreakerConfig(t *testing.T) {
	cfg := DefaultBreakerConfig()
	if cfg.MaxFailures != 5 {
		t.Errorf("want 5, got %d", cfg.MaxFailures)
	}
	if cfg.Window != 60*time.Second {
		t.Errorf("want 60s, got %v", cfg.Window)
	}
	if cfg.OpenTimeout != 30*time.Second {
		t.Errorf("want 30s, got %v", cfg.OpenTimeout)
	}
}

func TestTreeConfig_ZeroDefaults(t *testing.T) {
	tree := NewTree(TreeConfig{}, nil, discardLogger())
	if tree.cfg.MaxRestartIntensity != 10 {
		t.Errorf("want default 10, got %d", tree.cfg.MaxRestartIntensity)
	}
}

// ── RestartPolicy String ───────────────────────────────────────────────────

func TestRestartPolicy_String(t *testing.T) {
	tests := []struct {
		policy RestartPolicy
		want   string
	}{
		{RestartAlways, "always"},
		{RestartOnFailure, "on_failure"},
		{RestartNever, "never"},
		{RestartPolicy(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.policy.String(); got != tt.want {
			t.Errorf("RestartPolicy(%d).String() = %q, want %q", int(tt.policy), got, tt.want)
		}
	}
}

// ── Empty Tree ─────────────────────────────────────────────────────────────

func TestTree_EmptySpecs(t *testing.T) {
	tree := NewTree(fastTreeConfig(), nil, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(100 * time.Millisecond)
	tree.Stop()

	err := <-done
	if err != nil {
		t.Errorf("want nil, got %v", err)
	}
}

// ── Mixed Policies ─────────────────────────────────────────────────────────

func TestTree_MixedPolicies(t *testing.T) {
	var alwaysCount, failCount, neverCount atomic.Int32

	specs := []ChildSpec{
		{
			Name: "always-child",
			Start: func(ctx context.Context) error {
				n := alwaysCount.Add(1)
				if n < 3 {
					return nil
				}
				<-ctx.Done()
				return nil
			},
			RestartPolicy: RestartAlways,
			BreakerConfig: fastBreakerConfig(),
		},
		{
			Name: "fail-child",
			Start: func(ctx context.Context) error {
				n := failCount.Add(1)
				if n < 2 {
					return errors.New("fail")
				}
				<-ctx.Done()
				return nil
			},
			RestartPolicy: RestartOnFailure,
			BreakerConfig: fastBreakerConfig(),
		},
		{
			Name: "never-child",
			Start: func(ctx context.Context) error {
				neverCount.Add(1)
				return errors.New("one-shot fail")
			},
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(1 * time.Second)

	if got := alwaysCount.Load(); got < 3 {
		t.Errorf("always-child: want >= 3 calls, got %d", got)
	}
	if got := failCount.Load(); got < 2 {
		t.Errorf("fail-child: want >= 2 calls, got %d", got)
	}
	if got := neverCount.Load(); got != 1 {
		t.Errorf("never-child: want exactly 1 call, got %d", got)
	}

	tree.Stop()
	<-done
}

// ── Child Error Propagation ────────────────────────────────────────────────

func TestTree_ChildErrorRecorded(t *testing.T) {
	specs := []ChildSpec{
		{
			Name: "error-child",
			Start: func(ctx context.Context) error {
				return errors.New("specific error")
			},
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		},
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(200 * time.Millisecond)

	statuses := tree.Status()
	if len(statuses) != 1 {
		t.Fatalf("want 1 status, got %d", len(statuses))
	}
	if statuses[0].LastError == nil || statuses[0].LastError.Error() != "specific error" {
		t.Errorf("want 'specific error', got %v", statuses[0].LastError)
	}

	tree.Stop()
	<-done
}

// ── Shutdown Order ─────────────────────────────────────────────────────────

func TestTree_ShutdownReverseOrder(t *testing.T) {
	var shutdownOrder []string
	var mu sync.Mutex

	makeChild := func(name string) ChildSpec {
		return ChildSpec{
			Name: name,
			Start: func(ctx context.Context) error {
				<-ctx.Done()
				mu.Lock()
				shutdownOrder = append(shutdownOrder, name)
				mu.Unlock()
				return nil
			},
			RestartPolicy: RestartNever,
			BreakerConfig: fastBreakerConfig(),
		}
	}

	specs := []ChildSpec{
		makeChild("first"),
		makeChild("second"),
		makeChild("third"),
	}

	tree := NewTree(fastTreeConfig(), specs, discardLogger())

	done := make(chan error, 1)
	go func() {
		done <- tree.Run(context.Background())
	}()

	time.Sleep(100 * time.Millisecond)
	tree.Stop()
	<-done

	time.Sleep(50 * time.Millisecond) // let goroutines finish

	mu.Lock()
	defer mu.Unlock()

	if len(shutdownOrder) != 3 {
		t.Fatalf("want 3 shutdowns, got %d: %v", len(shutdownOrder), shutdownOrder)
	}
	// Reverse order: third, second, first.
	if shutdownOrder[0] != "third" || shutdownOrder[1] != "second" || shutdownOrder[2] != "first" {
		t.Errorf("want shutdown order [third, second, first], got %v", shutdownOrder)
	}
}

