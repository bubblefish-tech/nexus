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

package safego

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestGo_PanicRecovered(t *testing.T) {
	t.Helper()
	tracker := NewStatusTracker()
	logger := slog.Default()

	var wg sync.WaitGroup
	wg.Add(1)

	Go("test-panic", logger, tracker, func() {
		defer wg.Done()
		panic("intentional test panic")
	})

	wg.Wait()
	time.Sleep(10 * time.Millisecond)

	if tracker.IsHealthy() {
		t.Fatal("expected tracker to report degraded after panic")
	}
	degraded := tracker.Degraded()
	if degraded["test-panic"] != "intentional test panic" {
		t.Fatalf("expected panic reason recorded, got %q", degraded["test-panic"])
	}
}

func TestGo_NoPanic(t *testing.T) {
	t.Helper()
	tracker := NewStatusTracker()
	done := make(chan struct{})

	Go("test-ok", slog.Default(), tracker, func() {
		close(done)
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("goroutine did not complete")
	}

	if !tracker.IsHealthy() {
		t.Fatal("expected tracker to be healthy after normal completion")
	}
}

func TestStatusTracker_Empty(t *testing.T) {
	t.Helper()
	tracker := NewStatusTracker()
	if !tracker.IsHealthy() {
		t.Fatal("new tracker should be healthy")
	}
	if len(tracker.Degraded()) != 0 {
		t.Fatal("new tracker should have no degraded subsystems")
	}
}

func TestStatusTracker_MultipleDegraded(t *testing.T) {
	t.Helper()
	tracker := NewStatusTracker()
	tracker.MarkDegraded("sub-a", "reason-a")
	tracker.MarkDegraded("sub-b", "reason-b")

	if tracker.IsHealthy() {
		t.Fatal("expected unhealthy")
	}
	d := tracker.Degraded()
	if len(d) != 2 {
		t.Fatalf("expected 2 degraded, got %d", len(d))
	}
	if d["sub-a"] != "reason-a" || d["sub-b"] != "reason-b" {
		t.Fatalf("wrong reasons: %v", d)
	}
}

func TestGo_NilTrackerDoesNotCrash(t *testing.T) {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	Go("test-nil-tracker", slog.Default(), nil, func() {
		defer wg.Done()
		panic("should not crash even with nil tracker")
	})
	wg.Wait()
}
