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

package vizpipe

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testDrop struct{ n atomic.Int64 }

func (d *testDrop) Inc()        { d.n.Add(1) }
func (d *testDrop) Count() int64 { return d.n.Load() }

func tlog(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestEmitAndSubscribe(t *testing.T) {
	t.Helper()
	drop := &testDrop{}
	p := New(100, drop, tlog(t))
	p.Start()
	defer p.Stop()

	ch, unsub := p.Subscribe()
	defer unsub()

	p.Emit(Event{
		RequestID:   "req-1",
		Stage:       "exact_cache",
		DurationMs:  1.5,
		HitMiss:     "hit",
		ResultCount: 5,
		Source:      "claude",
	})

	select {
	case e := <-ch:
		if e.RequestID != "req-1" {
			t.Errorf("request_id = %q, want req-1", e.RequestID)
		}
		if e.Stage != "exact_cache" {
			t.Errorf("stage = %q, want exact_cache", e.Stage)
		}
		if e.HitMiss != "hit" {
			t.Errorf("hit_miss = %q, want hit", e.HitMiss)
		}
		if e.ResultCount != 5 {
			t.Errorf("result_count = %d, want 5", e.ResultCount)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestDropOnFullChannel(t *testing.T) {
	t.Helper()
	drop := &testDrop{}
	p := New(1, drop, tlog(t)) // capacity 1
	// Don't start dispatcher — channel fills immediately.

	for i := 0; i < 10; i++ {
		p.Emit(Event{RequestID: "drop"})
	}

	if got := drop.Count(); got < 9 {
		t.Errorf("expected at least 9 drops, got %d", got)
	}
}

func TestNeverBlocksHotPath(t *testing.T) {
	t.Helper()
	drop := &testDrop{}
	p := New(1, drop, tlog(t))
	// Don't start — test that Emit never blocks.

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			p.Emit(Event{RequestID: "perf"})
		}
		close(done)
	}()

	select {
	case <-done:
		// OK — non-blocking.
	case <-time.After(time.Second):
		t.Fatal("Emit blocked — INVARIANT VIOLATION")
	}
}

func TestMultipleSubscribers(t *testing.T) {
	t.Helper()
	drop := &testDrop{}
	p := New(100, drop, tlog(t))
	p.Start()
	defer p.Stop()

	ch1, unsub1 := p.Subscribe()
	defer unsub1()
	ch2, unsub2 := p.Subscribe()
	defer unsub2()

	p.Emit(Event{RequestID: "multi", Stage: "db_query"})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case e := <-ch:
			if e.RequestID != "multi" {
				t.Errorf("request_id = %q, want multi", e.RequestID)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber timed out")
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	t.Helper()
	drop := &testDrop{}
	p := New(100, drop, tlog(t))
	p.Start()
	defer p.Stop()

	ch, unsub := p.Subscribe()
	unsub() // Unsubscribe immediately.

	p.Emit(Event{RequestID: "after-unsub"})
	time.Sleep(50 * time.Millisecond)

	select {
	case <-ch:
		t.Error("received event after unsubscribe")
	default:
		// OK — no event received.
	}
}

func TestTimestampAutoSet(t *testing.T) {
	t.Helper()
	drop := &testDrop{}
	p := New(100, drop, tlog(t))
	p.Start()
	defer p.Stop()

	ch, unsub := p.Subscribe()
	defer unsub()

	before := time.Now().UTC()
	p.Emit(Event{RequestID: "ts-test"})
	after := time.Now().UTC()

	select {
	case e := <-ch:
		if e.Timestamp.Before(before) || e.Timestamp.After(after) {
			t.Errorf("timestamp %v not between %v and %v", e.Timestamp, before, after)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}

func TestMarshalSSE(t *testing.T) {
	t.Helper()
	e := Event{
		RequestID:   "req-1",
		Stage:       "exact_cache",
		DurationMs:  1.5,
		HitMiss:     "hit",
		ResultCount: 3,
		Timestamp:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	data, err := MarshalSSE(e)
	if err != nil {
		t.Fatalf("MarshalSSE: %v", err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "data: ") {
		t.Errorf("SSE data should start with 'data: ', got %q", s)
	}
	if !strings.HasSuffix(s, "\n\n") {
		t.Errorf("SSE data should end with two newlines")
	}
	// Verify JSON is valid.
	jsonPart := strings.TrimPrefix(s, "data: ")
	jsonPart = strings.TrimSuffix(jsonPart, "\n\n")
	var decoded Event
	if err := json.Unmarshal([]byte(jsonPart), &decoded); err != nil {
		t.Fatalf("SSE data JSON is invalid: %v", err)
	}
	if decoded.RequestID != "req-1" {
		t.Errorf("decoded request_id = %q, want req-1", decoded.RequestID)
	}
}

func TestConcurrentEmit(t *testing.T) {
	t.Helper()
	drop := &testDrop{}
	p := New(500, drop, tlog(t))
	p.Start()
	defer p.Stop()

	ch, unsub := p.Subscribe()
	defer unsub()

	const goroutines = 10
	const perGoroutine = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				p.Emit(Event{RequestID: "conc", Stage: "test"})
			}
		}()
	}
	wg.Wait()

	// Drain some events to verify we got at least some.
	time.Sleep(100 * time.Millisecond)
	received := 0
	for {
		select {
		case <-ch:
			received++
		default:
			goto done
		}
	}
done:
	total := int64(goroutines*perGoroutine) - drop.Count()
	if int64(received) < total {
		// It's OK if we don't get all — subscriber channel can also drop.
		// Just verify we got at least some.
		if received == 0 {
			t.Error("expected at least some events from concurrent emitters")
		}
	}
}

func TestStopIdempotent(t *testing.T) {
	t.Helper()
	drop := &testDrop{}
	p := New(10, drop, tlog(t))
	p.Start()
	p.Stop()
	p.Stop() // Must not panic.
}
