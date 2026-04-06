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

package eventsink

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testMetrics is a simple Metrics implementation for tests.
type testMetrics struct {
	dropped   atomic.Int64
	delivered atomic.Int64
	failed    atomic.Int64
}

func (m *testMetrics) IncDropped()   { m.dropped.Add(1) }
func (m *testMetrics) IncDelivered() { m.delivered.Add(1) }
func (m *testMetrics) IncFailed()    { m.failed.Add(1) }

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestEmitDelivery(t *testing.T) {
	t.Helper()

	var received atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev Event
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			t.Errorf("decode: %v", err)
			w.WriteHeader(500)
			return
		}
		if ev.EventType != "memory_written" {
			t.Errorf("event_type = %q, want memory_written", ev.EventType)
		}
		if ev.PayloadID == "" {
			t.Error("payload_id is empty")
		}
		received.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         100,
		RetryBackoffSeconds: []int{0},
		Sinks: []SinkConfig{
			{Name: "test", URL: srv.URL, TimeoutSeconds: 5, MaxRetries: 1, Content: "summary"},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})
	s.Start()

	s.Emit(Event{
		EventType:   "memory_written",
		PayloadID:   "abc123",
		Source:      "claude",
		Subject:     "user:shawn",
		Destination: "sqlite",
		Timestamp:   time.Now().UTC(),
		ActorType:   "user",
		ActorID:     "shawn",
	})

	// Give time for delivery.
	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if got := received.Load(); got != 1 {
		t.Errorf("received = %d, want 1", got)
	}
	if got := m.delivered.Load(); got != 1 {
		t.Errorf("delivered = %d, want 1", got)
	}
	if got := m.dropped.Load(); got != 0 {
		t.Errorf("dropped = %d, want 0", got)
	}
}

func TestSummaryModeStripsContent(t *testing.T) {
	t.Helper()

	var receivedContent json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev Event
		json.NewDecoder(r.Body).Decode(&ev)
		receivedContent = ev.Content
		w.WriteHeader(200)
	}))
	defer srv.Close()

	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         10,
		RetryBackoffSeconds: []int{0},
		Sinks: []SinkConfig{
			{Name: "test", URL: srv.URL, TimeoutSeconds: 5, MaxRetries: 1, Content: "summary"},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})
	s.Start()

	s.Emit(Event{
		EventType: "memory_written",
		PayloadID: "abc123",
		Content:   json.RawMessage(`{"memory":"secret stuff"}`),
	})

	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if len(receivedContent) != 0 {
		t.Errorf("summary mode should strip content, got %s", receivedContent)
	}
}

func TestFullModeIncludesContent(t *testing.T) {
	t.Helper()

	var receivedContent json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev Event
		json.NewDecoder(r.Body).Decode(&ev)
		receivedContent = ev.Content
		w.WriteHeader(200)
	}))
	defer srv.Close()

	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         10,
		RetryBackoffSeconds: []int{0},
		Sinks: []SinkConfig{
			{Name: "test", URL: srv.URL, TimeoutSeconds: 5, MaxRetries: 1, Content: "full"},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})
	s.Start()

	content := json.RawMessage(`{"memory":"important data"}`)
	s.Emit(Event{
		EventType: "memory_written",
		PayloadID: "abc456",
		Content:   content,
	})

	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if len(receivedContent) == 0 {
		t.Error("full mode should include content")
	}
}

func TestDropOnFullChannel(t *testing.T) {
	t.Helper()

	// Use a very small channel to force drops.
	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         1,
		RetryBackoffSeconds: []int{0},
		Sinks: []SinkConfig{
			{Name: "slow", URL: "http://192.0.2.1:1/unreachable", TimeoutSeconds: 1, MaxRetries: 0},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})
	// Do NOT start — channel will fill up since nobody reads.

	for i := 0; i < 10; i++ {
		s.Emit(Event{EventType: "memory_written", PayloadID: "drop-test"})
	}

	if got := m.dropped.Load(); got < 9 {
		t.Errorf("expected at least 9 drops, got %d", got)
	}
}

func TestWritePathNeverBlocks(t *testing.T) {
	t.Helper()

	// Channel capacity 1, no consumer. Emit 1000 events in a tight loop.
	// Must complete within 1 second (proving non-blocking).
	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         1,
		RetryBackoffSeconds: []int{0},
		Sinks: []SinkConfig{
			{Name: "null", URL: "http://192.0.2.1:1/unreachable", TimeoutSeconds: 1, MaxRetries: 0},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			s.Emit(Event{EventType: "memory_written", PayloadID: "perf"})
		}
		close(done)
	}()

	select {
	case <-done:
		// OK — non-blocking.
	case <-time.After(1 * time.Second):
		t.Fatal("Emit blocked the write path — INVARIANT VIOLATION")
	}

	// Total = 1 accepted + 999 dropped.
	if got := m.dropped.Load(); got < 999 {
		t.Errorf("expected at least 999 drops, got %d", got)
	}
}

func TestRetryThenSucceed(t *testing.T) {
	t.Helper()

	var attempts atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         10,
		RetryBackoffSeconds: []int{0, 0, 0}, // zero-delay for test speed
		Sinks: []SinkConfig{
			{Name: "flaky", URL: srv.URL, TimeoutSeconds: 5, MaxRetries: 5},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})
	s.Start()

	s.Emit(Event{EventType: "memory_written", PayloadID: "retry-test"})

	time.Sleep(200 * time.Millisecond)
	s.Stop()

	if got := attempts.Load(); got < 3 {
		t.Errorf("expected at least 3 attempts, got %d", got)
	}
	if got := m.delivered.Load(); got != 1 {
		t.Errorf("delivered = %d, want 1", got)
	}
}

func TestRetryExhausted(t *testing.T) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         10,
		RetryBackoffSeconds: []int{0},
		Sinks: []SinkConfig{
			{Name: "down", URL: srv.URL, TimeoutSeconds: 5, MaxRetries: 2},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})
	s.Start()

	s.Emit(Event{EventType: "memory_written", PayloadID: "fail-test"})

	time.Sleep(200 * time.Millisecond)
	s.Stop()

	if got := m.failed.Load(); got != 1 {
		t.Errorf("failed = %d, want 1", got)
	}
	if got := m.delivered.Load(); got != 0 {
		t.Errorf("delivered = %d, want 0", got)
	}
}

func TestMultipleSinks(t *testing.T) {
	t.Helper()

	var received1, received2 atomic.Int64
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received1.Add(1)
		w.WriteHeader(200)
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received2.Add(1)
		w.WriteHeader(200)
	}))
	defer srv2.Close()

	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         10,
		RetryBackoffSeconds: []int{0},
		Sinks: []SinkConfig{
			{Name: "sink1", URL: srv1.URL, TimeoutSeconds: 5, MaxRetries: 1, Content: "summary"},
			{Name: "sink2", URL: srv2.URL, TimeoutSeconds: 5, MaxRetries: 1, Content: "summary"},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})
	s.Start()

	s.Emit(Event{EventType: "memory_written", PayloadID: "multi"})

	time.Sleep(100 * time.Millisecond)
	s.Stop()

	if got := received1.Load(); got != 1 {
		t.Errorf("sink1 received = %d, want 1", got)
	}
	if got := received2.Load(); got != 1 {
		t.Errorf("sink2 received = %d, want 1", got)
	}
	if got := m.delivered.Load(); got != 2 {
		t.Errorf("delivered = %d, want 2 (one per sink)", got)
	}
}

func TestStopDrainsChannel(t *testing.T) {
	t.Helper()

	var received atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         100,
		RetryBackoffSeconds: []int{0},
		Sinks: []SinkConfig{
			{Name: "drain", URL: srv.URL, TimeoutSeconds: 5, MaxRetries: 1},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})

	// Fill the channel before starting.
	for i := 0; i < 5; i++ {
		s.ch <- Event{EventType: "memory_written", PayloadID: "drain-test"}
	}

	s.Start()
	// Immediately stop — should drain the 5 buffered events.
	time.Sleep(50 * time.Millisecond)
	s.Stop()

	if got := received.Load(); got < 5 {
		t.Errorf("expected at least 5 received after drain, got %d", got)
	}
}

func TestConcurrentEmit(t *testing.T) {
	t.Helper()

	var received atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	m := &testMetrics{}
	s := New(Config{
		MaxInFlight:         500,
		RetryBackoffSeconds: []int{0},
		Sinks: []SinkConfig{
			{Name: "concurrent", URL: srv.URL, TimeoutSeconds: 5, MaxRetries: 1},
		},
		Metrics: m,
		Logger:  testLogger(t),
	})
	s.Start()

	const goroutines = 10
	const perGoroutine = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				s.Emit(Event{EventType: "memory_written", PayloadID: "conc"})
			}
		}()
	}
	wg.Wait()

	time.Sleep(500 * time.Millisecond)
	s.Stop()

	total := int64(goroutines * perGoroutine)
	got := received.Load() + m.dropped.Load()
	if got != total {
		t.Errorf("received(%d) + dropped(%d) = %d, want %d", received.Load(), m.dropped.Load(), got, total)
	}
}

func TestStopIdempotent(t *testing.T) {
	t.Helper()
	m := &testMetrics{}
	s := New(Config{
		MaxInFlight: 10,
		Sinks:       []SinkConfig{},
		Metrics:     m,
		Logger:      testLogger(t),
	})
	s.Start()
	s.Stop()
	s.Stop() // Second call must not panic.
}
