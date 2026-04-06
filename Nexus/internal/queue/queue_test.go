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

package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/queue"
	"github.com/BubbleFish-Nexus/internal/wal"
)

// ── Test doubles ────────────────────────────────────────────────────────────

// successDest is a DestinationWriter that always succeeds and counts writes.
type successDest struct {
	mu     sync.Mutex
	writes []destination.TranslatedPayload
}

func (d *successDest) Write(p destination.TranslatedPayload) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, p)
	return nil
}
func (d *successDest) Ping() error                          { return nil }
func (d *successDest) Exists(_ string) (bool, error)        { return false, nil }
func (d *successDest) Close() error                         { return nil }
func (d *successDest) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.writes)
}

// failDest is a DestinationWriter that always returns an error.
type failDest struct{}

func (d *failDest) Write(_ destination.TranslatedPayload) error {
	return errors.New("dest: always fails")
}
func (d *failDest) Ping() error                       { return nil }
func (d *failDest) Exists(_ string) (bool, error)     { return false, nil }
func (d *failDest) Close() error                      { return nil }

// countingUpdater is a WALUpdater that records MarkDelivered and
// MarkPermanentFailure calls.
type countingUpdater struct {
	delivered atomic.Int64
	permanent atomic.Int64
}

func (u *countingUpdater) MarkDelivered(_ string) error {
	u.delivered.Add(1)
	return nil
}
func (u *countingUpdater) MarkDeliveredBatch(ids []string) error {
	u.delivered.Add(int64(len(ids)))
	return nil
}
func (u *countingUpdater) MarkPermanentFailure(_ string) error {
	u.permanent.Add(1)
	return nil
}

// noopUpdater never returns errors and discards all calls.
type noopUpdater struct{}

func (u *noopUpdater) MarkDelivered(_ string) error        { return nil }
func (u *noopUpdater) MarkDeliveredBatch(_ []string) error { return nil }
func (u *noopUpdater) MarkPermanentFailure(_ string) error { return nil }

// ── Helpers ──────────────────────────────────────────────────────────────────

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// makeEntry returns a valid WAL entry whose Payload is a JSON-encoded
// TranslatedPayload with the given id.
func makeEntry(t *testing.T, id string) wal.Entry {
	t.Helper()
	tp := destination.TranslatedPayload{
		PayloadID: id,
		Content:   "content for " + id,
		Timestamp: time.Now().UTC(),
	}
	raw, err := json.Marshal(tp)
	if err != nil {
		t.Fatalf("makeEntry: marshal: %v", err)
	}
	return wal.Entry{
		PayloadID:      id,
		IdempotencyKey: "idem-" + id,
		Status:         wal.StatusPending,
		Timestamp:      time.Now().UTC(),
		Payload:        raw,
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

// TestQueue_NilLoggerPanics verifies that constructing a Queue with a nil
// logger panics immediately with a clear message.
func TestQueue_NilLoggerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger, got none")
		}
	}()
	queue.New(queue.Config{}, nil, &successDest{}, &noopUpdater{})
}

// TestQueue_ConcurrentEnqueue launches 100 goroutines that each call Enqueue
// once. The channel is large enough that none should load-shed. There must be
// zero panics and zero data-race reports (enforced by -race).
func TestQueue_ConcurrentEnqueue(t *testing.T) {
	const goroutines = 100
	dest := &successDest{}
	upd := &noopUpdater{}
	q := queue.New(
		queue.Config{Size: goroutines * 2, Workers: 2},
		testLogger(t),
		dest,
		upd,
	)

	var wg sync.WaitGroup
	var shed atomic.Int64
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			entry := makeEntry(t, fmt.Sprintf("conc-%03d", n))
			if err := q.Enqueue(entry); err != nil {
				if errors.Is(err, queue.ErrLoadShed) {
					shed.Add(1)
				} else {
					t.Errorf("Enqueue: unexpected error: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q.DrainWithContext(ctx)

	if shed.Load() > 0 {
		t.Logf("note: %d entries load-shed (channel may have been momentarily full)", shed.Load())
	}
	// No panics → test passes.
}

// TestQueue_DrainMultipleCalls verifies that calling Drain() three times in a
// row never panics (sync.Once protection).
func TestQueue_DrainMultipleCalls(t *testing.T) {
	q := queue.New(
		queue.Config{Size: 10, Workers: 1},
		testLogger(t),
		&successDest{},
		&noopUpdater{},
	)

	// Should not panic.
	q.Drain()
	q.Drain()
	q.Drain()
}

// TestQueue_DrainWithContextTimeout verifies that DrainWithContext returns
// within a reasonable window even when the channel has entries being processed.
// The test uses a 1-second context deadline and asserts the call returns in
// under 2 seconds.
func TestQueue_DrainWithContextTimeout(t *testing.T) {
	// Use a failDest so the worker retries and doesn't drain quickly. The
	// backoff starts at 1s so the worker will be sleeping during drain.
	q := queue.New(
		queue.Config{Size: 50, Workers: 1},
		testLogger(t),
		&failDest{},
		&noopUpdater{},
	)

	// Enqueue one entry to keep the worker busy.
	_ = q.Enqueue(makeEntry(t, "busy-entry"))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	result := q.DrainWithContext(ctx)
	elapsed := time.Since(start)

	// DrainWithContext returns false because ctx expired before drain completed.
	if result {
		t.Log("note: drain completed before context expired (worker was fast)")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("DrainWithContext took %v, expected < 2s", elapsed)
	}
}

// TestQueue_LoadShed verifies that Enqueue on a full channel returns
// ErrLoadShed immediately (within 1ms) rather than blocking.
func TestQueue_LoadShed(t *testing.T) {
	// Size 0 is replaced with default. Use Size=1 and block the worker so the
	// channel fills up immediately.
	blockCh := make(chan struct{})
	blockDest := &blockingDest{block: blockCh}

	q := queue.New(
		queue.Config{Size: 1, Workers: 1},
		testLogger(t),
		blockDest,
		&noopUpdater{},
	)

	// Fill the channel with one entry (worker is blocked so it won't consume).
	entry1 := makeEntry(t, "fill-1")
	// Give the worker time to pick up the first entry and block on the dest.
	// We enqueue two entries: the first goes to the worker, the second sits in
	// the buffer; the third causes the load-shed.
	_ = q.Enqueue(entry1)

	// Small yield to let the worker dequeue entry1.
	time.Sleep(10 * time.Millisecond)

	// Fill the buffer.
	_ = q.Enqueue(makeEntry(t, "fill-buf"))

	// Now the channel is full — next Enqueue must return ErrLoadShed fast.
	start := time.Now()
	err := q.Enqueue(makeEntry(t, "shed-me"))
	elapsed := time.Since(start)

	if !errors.Is(err, queue.ErrLoadShed) {
		t.Fatalf("expected ErrLoadShed, got %v", err)
	}
	if elapsed > time.Millisecond {
		t.Fatalf("Enqueue took %v, expected < 1ms", elapsed)
	}

	// Unblock the worker and drain.
	close(blockCh)
	q.Drain()
}

// TestQueue_DeliverSuccess verifies that a successfully written entry results
// in MarkDelivered being called on the WAL updater.
func TestQueue_DeliverSuccess(t *testing.T) {
	dest := &successDest{}
	upd := &countingUpdater{}
	q := queue.New(
		queue.Config{Size: 10, Workers: 1},
		testLogger(t),
		dest,
		upd,
	)

	_ = q.Enqueue(makeEntry(t, "deliver-me"))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	q.DrainWithContext(ctx)

	if upd.delivered.Load() != 1 {
		t.Fatalf("expected MarkDelivered called once, got %d", upd.delivered.Load())
	}
	if dest.count() != 1 {
		t.Fatalf("expected 1 write to dest, got %d", dest.count())
	}
}

// TestQueue_MalformedPayloadPermanent verifies that an entry with an
// unmarshalable Payload is immediately classified as PERMANENT_FAILURE.
func TestQueue_MalformedPayloadPermanent(t *testing.T) {
	upd := &countingUpdater{}
	q := queue.New(
		queue.Config{Size: 10, Workers: 1},
		testLogger(t),
		&successDest{},
		upd,
	)

	bad := wal.Entry{
		PayloadID: "bad-json",
		Payload:   []byte("not json"),
		Status:    wal.StatusPending,
		Timestamp: time.Now().UTC(),
	}
	_ = q.Enqueue(bad)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	q.DrainWithContext(ctx)

	if upd.permanent.Load() != 1 {
		t.Fatalf("expected MarkPermanentFailure called once, got %d", upd.permanent.Load())
	}
}

// ── blockingDest ──────────────────────────────────────────────────────────────

// blockingDest blocks on Write until its block channel is closed. Used to keep
// a worker occupied so the queue channel can be filled.
type blockingDest struct {
	block chan struct{}
}

func (d *blockingDest) Write(_ destination.TranslatedPayload) error {
	<-d.block
	return nil
}
func (d *blockingDest) Ping() error                    { return nil }
func (d *blockingDest) Exists(_ string) (bool, error)  { return false, nil }
func (d *blockingDest) Close() error                   { return nil }
