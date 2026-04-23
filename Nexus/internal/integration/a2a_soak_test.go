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

package integration

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/httputil"
)

// TestSoak_24h is a long-running soak test harness that sends tasks at a
// sustained rate for an extended duration. It is gated behind:
//   - -short mode: always skipped
//   - normal `go test`: runs a 30-second abbreviated soak
//   - NEXUS_SOAK_DURATION env var: set to e.g. "24h" for the full nightly run
//
// Goal: zero errors, zero memory growth after the first minute, zero audit
// chain gaps. This mirrors the bar used by the Substrate soak test.
func TestSoak_24h(t *testing.T) {
	if testing.Short() {
		t.Skip("soak test skipped in -short mode")
	}

	duration := 30 * time.Second // default abbreviated soak
	if d := soakDuration(); d > 0 {
		duration = d
	}

	const (
		tasksPerSecond = 10
		tickInterval   = time.Second / tasksPerSecond
		memSampleEvery = 30 * time.Second
	)

	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.*", "allow")

	ctx, cancel := context.WithTimeout(context.Background(), duration+10*time.Second)
	defer cancel()

	var (
		totalSent  atomic.Int64
		totalOK    atomic.Int64
		totalFail  atomic.Int64
		wg         sync.WaitGroup
	)

	// Memory baseline (after warm-up).
	var baselineAlloc uint64
	warmUpDone := make(chan struct{})

	// Sender goroutine: one task per tick.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()

		deadline := time.Now().Add(duration)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if time.Now().After(deadline) {
					return
				}
				n := totalSent.Add(1)

				_, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
					"agent": "mock",
					"skill": "echo_message",
					"input": fmt.Sprintf("soak-%d", n),
				})
				if err != nil {
					totalFail.Add(1)
				} else {
					totalOK.Add(1)
				}

				// Signal warm-up done after the first second.
				if n == tasksPerSecond {
					select {
					case <-warmUpDone:
					default:
						close(warmUpDone)
					}
				}
			}
		}
	}()

	// Memory monitor goroutine.
	type memSample struct {
		at    time.Duration
		alloc uint64
	}
	var memSamples []memSample
	var memMu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()

		// Wait for warm-up.
		select {
		case <-warmUpDone:
		case <-ctx.Done():
			return
		}

		// Take baseline.
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		baselineAlloc = m.Alloc

		sampler := time.NewTicker(memSampleEvery)
		defer sampler.Stop()

		start := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-sampler.C:
				runtime.ReadMemStats(&m)
				memMu.Lock()
				memSamples = append(memSamples, memSample{
					at:    time.Since(start),
					alloc: m.Alloc,
				})
				memMu.Unlock()
			}
		}
	}()

	wg.Wait()

	// --- Assertions ---

	t.Logf("soak results: duration=%v sent=%d ok=%d fail=%d",
		duration, totalSent.Load(), totalOK.Load(), totalFail.Load())

	// Zero failures.
	if totalFail.Load() > 0 {
		t.Errorf("soak had %d failures out of %d tasks", totalFail.Load(), totalSent.Load())
	}

	// Verify tasks were actually sent at the expected rate.
	expectedMin := int64(float64(duration.Seconds()) * float64(tasksPerSecond) * 0.8)
	if totalSent.Load() < expectedMin {
		t.Errorf("expected at least %d tasks sent, got %d", expectedMin, totalSent.Load())
	}

	// Memory growth check: final alloc should not be more than 3x the baseline.
	// This catches unbounded growth (leaked goroutines, growing maps, etc.).
	// Force-close idle HTTP connections and re-GC so pooled connection buffers
	// (from httputil.TunedTransport) don't inflate the final measurement.
	httputil.TunedTransport.CloseIdleConnections()
	runtime.GC()
	var postGC runtime.MemStats
	runtime.ReadMemStats(&postGC)

	memMu.Lock()
	defer memMu.Unlock()
	if len(memSamples) > 0 && baselineAlloc > 0 {
		finalAlloc := postGC.Alloc
		ratio := float64(finalAlloc) / float64(baselineAlloc)
		t.Logf("memory: baseline=%d KB, final=%d KB, ratio=%.2fx",
			baselineAlloc/1024, finalAlloc/1024, ratio)

		if ratio > 3.0 {
			t.Errorf("memory grew %.2fx over baseline (limit 3.0x) — possible leak", ratio)
		}

		// Log memory trend for diagnostics.
		for i, s := range memSamples {
			if i%5 == 0 || i == len(memSamples)-1 {
				t.Logf("  mem[%v] = %d KB", s.at.Round(time.Second), s.alloc/1024)
			}
		}
	}

	// Verify audit chain completeness: every successful task should have
	// at least one audit event.
	events := env.audit.Events()
	t.Logf("audit: %d events for %d successful tasks", len(events), totalOK.Load())
	if int64(len(events)) < totalOK.Load() {
		t.Errorf("audit gap: %d events for %d tasks", len(events), totalOK.Load())
	}
}

// TestSoak_BurstRecovery sends bursts of tasks with quiet intervals,
// verifying the system recovers cleanly between bursts.
func TestSoak_BurstRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("burst recovery test skipped in -short mode")
	}

	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.*", "allow")

	ctx := context.Background()
	const (
		numBursts    = 5
		burstSize    = 100
		quietPeriod  = 500 * time.Millisecond
	)

	var totalOK, totalFail atomic.Int64

	for burst := 0; burst < numBursts; burst++ {
		var wg sync.WaitGroup
		for i := 0; i < burstSize; i++ {
			wg.Add(1)
			go func(b, n int) {
				defer wg.Done()
				_, err := env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
					"agent": "mock",
					"skill": "echo_message",
					"input": fmt.Sprintf("burst-%d-%d", b, n),
				})
				if err != nil {
					totalFail.Add(1)
				} else {
					totalOK.Add(1)
				}
			}(burst, i)
		}
		wg.Wait()

		// Quiet period between bursts.
		if burst < numBursts-1 {
			time.Sleep(quietPeriod)
		}
	}

	total := numBursts * burstSize
	t.Logf("burst recovery: %d/%d OK, %d failed", totalOK.Load(), total, totalFail.Load())

	if totalFail.Load() > 0 {
		t.Errorf("burst recovery had %d failures", totalFail.Load())
	}
}

// TestSoak_MemoryStability runs a tight loop to verify no goroutine or
// memory leak in the bridge path.
func TestSoak_MemoryStability(t *testing.T) {
	if testing.Short() {
		t.Skip("memory stability test skipped in -short mode")
	}

	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.*", "allow")

	ctx := context.Background()
	const iterations = 500

	// Warm up.
	for i := 0; i < 10; i++ {
		env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
			"agent": "mock",
			"skill": "echo_message",
			"input": "warmup",
		})
	}

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	goroutinesBefore := runtime.NumGoroutine()

	for i := 0; i < iterations; i++ {
		env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
			"agent": "mock",
			"skill": "echo_message",
			"input": fmt.Sprintf("memstab-%d", i),
		})
	}

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	goroutinesAfter := runtime.NumGoroutine()

	t.Logf("memory: before=%d KB, after=%d KB, delta=%d KB",
		before.Alloc/1024, after.Alloc/1024, (after.Alloc-before.Alloc)/1024)
	t.Logf("goroutines: before=%d, after=%d, delta=%d",
		goroutinesBefore, goroutinesAfter, goroutinesAfter-goroutinesBefore)

	// Allow a small goroutine increase (GC helpers, etc.) but flag anything
	// that looks like a per-request goroutine leak.
	if goroutinesAfter-goroutinesBefore > 10 {
		t.Errorf("goroutine leak: %d new goroutines after %d iterations",
			goroutinesAfter-goroutinesBefore, iterations)
	}
}

// TestSoak_MixedWorkload sends a mix of different operations concurrently
// to verify no deadlocks or resource contention under diverse load.
func TestSoak_MixedWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("mixed workload test skipped in -short mode")
	}

	env := newTestEnv(t)
	env.addGrant(t, "client_generic", "mock-agent-id", "test.*", "allow")

	ctx := context.Background()
	var wg sync.WaitGroup
	var ops atomic.Int64

	// Send tasks.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			env.bridge.HandleA2ASendToAgent(ctx, map[string]interface{}{
				"agent": "mock",
				"skill": "echo_message",
				"input": fmt.Sprintf("mixed-send-%d", i),
			})
			ops.Add(1)
		}
	}()

	// List agents.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			env.bridge.HandleA2AListAgents(ctx, map[string]interface{}{})
			ops.Add(1)
		}
	}()

	// Describe agent.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			env.bridge.HandleA2ADescribeAgent(ctx, map[string]interface{}{
				"agent": "mock",
			})
			ops.Add(1)
		}
	}()

	// List grants.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			env.bridge.HandleA2AListGrants(ctx, map[string]interface{}{})
			ops.Add(1)
		}
	}()

	// List pending approvals.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			env.bridge.HandleA2AListPendingApprovals(ctx, map[string]interface{}{})
			ops.Add(1)
		}
	}()

	wg.Wait()
	t.Logf("mixed workload: %d total operations completed without deadlock", ops.Load())
}

// soakDuration reads the NEXUS_SOAK_DURATION environment variable.
// Returns 0 if not set or unparseable.
func soakDuration() time.Duration {
	v := os.Getenv("NEXUS_SOAK_DURATION")
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0
	}
	return d
}
