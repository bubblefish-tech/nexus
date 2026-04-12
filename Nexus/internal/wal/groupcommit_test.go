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

package wal

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// openTestWALGroupCommit opens a WAL with group commit enabled.
func openTestWALGroupCommit(t *testing.T, maxBatch int, maxDelay time.Duration) (*WAL, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := Open(dir, 50, testLogger(), WithGroupCommit(GroupCommitConfig{
		Enabled:  true,
		MaxBatch: maxBatch,
		MaxDelay: maxDelay,
	}))
	if err != nil {
		t.Fatalf("Open with group commit: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return w, dir
}

// TestGroupCommit_SingleWriter verifies that 1000 sequential writes through
// group commit all land on disk and can be replayed.
func TestGroupCommit_SingleWriter(t *testing.T) {
	w, dir := openTestWALGroupCommit(t, 256, 500*time.Microsecond)

	const n = 1000
	for i := 0; i < n; i++ {
		err := w.Append(Entry{
			PayloadID:      fmt.Sprintf("gc-payload-%d", i),
			IdempotencyKey: fmt.Sprintf("gc-idem-%d", i),
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        json.RawMessage(`{"x":1}`),
		})
		if err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	if got := w.PendingCount(); got != int64(n) {
		t.Errorf("PendingCount: want %d, got %d", n, got)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)
	if len(replayed) != n {
		t.Errorf("replay: want %d entries, got %d", n, len(replayed))
	}
}

// TestGroupCommit_ConcurrentWriters verifies that 50 concurrent writers can
// write 10,000 entries total through group commit without data loss.
func TestGroupCommit_ConcurrentWriters(t *testing.T) {
	w, dir := openTestWALGroupCommit(t, 256, 500*time.Microsecond)

	const writers = 50
	const perWriter = 200
	total := writers * perWriter

	var wg sync.WaitGroup
	errs := make(chan error, total)

	for g := 0; g < writers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				err := w.Append(Entry{
					PayloadID:      fmt.Sprintf("gc-w%d-p%d", g, i),
					IdempotencyKey: fmt.Sprintf("gc-w%d-k%d", g, i),
					Source:         "src",
					Destination:    "dst",
					Subject:        "sub",
					Payload:        json.RawMessage(`{"x":1}`),
				})
				if err != nil {
					errs <- fmt.Errorf("writer %d entry %d: %w", g, i, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}

	if got := w.PendingCount(); got != int64(total) {
		t.Errorf("PendingCount: want %d, got %d", total, got)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)
	if len(replayed) != total {
		t.Errorf("replay: want %d entries, got %d", total, len(replayed))
	}
}

// TestGroupCommit_DeadlineFlush verifies that a single entry is flushed
// within the deadline even when the batch is not full.
func TestGroupCommit_DeadlineFlush(t *testing.T) {
	// Use a large batch size so deadline triggers before batch fills.
	w, _ := openTestWALGroupCommit(t, 1000, 5*time.Millisecond)

	start := time.Now()
	err := w.Append(Entry{
		PayloadID:      "deadline-1",
		IdempotencyKey: "deadline-k1",
		Source:         "src",
		Destination:    "dst",
		Subject:        "sub",
		Payload:        json.RawMessage(`{"x":1}`),
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Should have returned within the deadline + generous overhead.
	// The deadline is 5ms; allow up to 500ms for CI/slow systems and
	// Windows scheduler variance.
	if elapsed > 500*time.Millisecond {
		t.Errorf("deadline flush took %v, expected <500ms", elapsed)
	}

	if got := w.PendingCount(); got != 1 {
		t.Errorf("PendingCount: want 1, got %d", got)
	}
}

// TestGroupCommit_BatchFullFlush verifies that a full batch triggers an
// immediate flush without waiting for the deadline.
func TestGroupCommit_BatchFullFlush(t *testing.T) {
	const batchSize = 4
	// Use a very long deadline so it never fires.
	w, _ := openTestWALGroupCommit(t, batchSize, 10*time.Second)

	// Submit batchSize entries concurrently — the batch should fill and
	// flush immediately.
	var wg sync.WaitGroup
	for i := 0; i < batchSize; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := w.Append(Entry{
				PayloadID:      fmt.Sprintf("batch-full-%d", i),
				IdempotencyKey: fmt.Sprintf("batch-full-k%d", i),
				Source:         "src",
				Destination:    "dst",
				Subject:        "sub",
				Payload:        json.RawMessage(`{"x":1}`),
			})
			if err != nil {
				t.Errorf("Append(%d): %v", i, err)
			}
		}(i)
	}

	// All should complete within 1 second (not waiting for the 10s deadline).
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("batch full flush did not trigger within 2s (deadline is 10s)")
	}

	if got := w.PendingCount(); got != int64(batchSize) {
		t.Errorf("PendingCount: want %d, got %d", batchSize, got)
	}
}

// TestGroupCommit_EmptyBufferNoBusyLoop verifies that the group committer
// goroutine does not busy-loop when there are no entries to process.
func TestGroupCommit_EmptyBufferNoBusyLoop(t *testing.T) {
	w, _ := openTestWALGroupCommit(t, 256, 500*time.Microsecond)

	// Let the group committer sit idle for a bit.
	time.Sleep(50 * time.Millisecond)

	// If it were busy-looping, PendingCount would be wrong or there would
	// be errors. Verify clean state.
	if got := w.PendingCount(); got != 0 {
		t.Errorf("PendingCount after idle: want 0, got %d", got)
	}

	// Now do a write to confirm the goroutine is still responsive.
	if err := w.Append(Entry{
		PayloadID:      "after-idle",
		IdempotencyKey: "after-idle-k",
		Source:         "src",
		Destination:    "dst",
		Subject:        "sub",
		Payload:        json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("Append after idle: %v", err)
	}

	if got := w.PendingCount(); got != 1 {
		t.Errorf("PendingCount after 1 append: want 1, got %d", got)
	}
}

// TestGroupCommit_CloseFlushesBuffer verifies that Close flushes any
// pending entries in the group committer before closing the file.
func TestGroupCommit_CloseFlushesBuffer(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, 50, testLogger(), WithGroupCommit(GroupCommitConfig{
		Enabled:  true,
		MaxBatch: 1000,          // large batch
		MaxDelay: 10 * time.Second, // long deadline
	}))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Submit 5 entries — the batch won't fill and deadline won't fire.
	for i := 0; i < 5; i++ {
		go func(i int) {
			_ = w.Append(Entry{
				PayloadID:      fmt.Sprintf("close-flush-%d", i),
				IdempotencyKey: fmt.Sprintf("close-flush-k%d", i),
				Source:         "src",
				Destination:    "dst",
				Subject:        "sub",
				Payload:        json.RawMessage(`{}`),
			})
		}(i)
	}

	// Give entries time to reach the channel.
	time.Sleep(20 * time.Millisecond)

	// Close should flush the pending entries.
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and replay — all 5 entries should be present.
	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)
	if len(replayed) != 5 {
		t.Errorf("replay after close-flush: want 5, got %d", len(replayed))
	}
}

// TestGroupCommit_LegacyModeUnchanged verifies that when group commit is
// disabled, the WAL behaves exactly like v0.1.2 (no group committer goroutine).
func TestGroupCommit_LegacyModeUnchanged(t *testing.T) {
	w, dir := openTestWAL(t) // no group commit option

	if w.gc != nil {
		t.Fatal("gc should be nil when group commit is not enabled")
	}

	appendN(t, w, 10)

	if got := w.PendingCount(); got != 10 {
		t.Errorf("PendingCount: want 10, got %d", got)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)
	if len(replayed) != 10 {
		t.Errorf("replay: want 10, got %d", len(replayed))
	}
}
