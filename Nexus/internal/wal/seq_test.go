// Copyright © 2026 BubbleFish Technologies, Inc.

package wal

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"sync/atomic"
	"testing"
	"time"
)

// testSeqFn returns a sequence function backed by an atomic counter.
func testSeqFn() (func() int64, *atomic.Int64) {
	var counter atomic.Int64
	return func() int64 { return counter.Add(1) }, &counter
}

// TestSeq_WALEntryCarriesSequence writes entries with a sequence function
// and verifies MonotonicSeq is preserved through write/replay.
func TestSeq_WALEntryCarriesSequence(t *testing.T) {
	dir := t.TempDir()
	seqFn, _ := testSeqFn()
	w, err := Open(dir, 50, testLogger(), WithSequence(seqFn))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	for i := 0; i < 5; i++ {
		entry := Entry{
			PayloadID:      fmt.Sprintf("seq-%d", i),
			IdempotencyKey: fmt.Sprintf("seq-idem-%d", i),
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        json.RawMessage(`{"seq":true}`),
		}
		if err := w.Append(entry); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Replay and verify sequences are preserved.
	w2 := reopen(t, dir)
	var replayed []Entry
	if err := w2.Replay(func(e Entry) { replayed = append(replayed, e) }); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(replayed) != 5 {
		t.Fatalf("want 5 entries, got %d", len(replayed))
	}
	for i, e := range replayed {
		expected := int64(i + 1)
		if e.MonotonicSeq != expected {
			t.Errorf("entry %d: want MonotonicSeq=%d, got %d", i, expected, e.MonotonicSeq)
		}
	}
}

// TestSeq_BackwardCompat loads v0.1.2 entries (no MonotonicSeq field) and
// verifies they replay correctly with MonotonicSeq=0.
func TestSeq_BackwardCompat(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seg := currentSegment(t, dir)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Write v0.1.2-style entries (old format, no MonotonicSeq).
	for i := 0; i < 5; i++ {
		entry := Entry{
			Version:        1,
			PayloadID:      fmt.Sprintf("v012-%d", i),
			IdempotencyKey: fmt.Sprintf("v012-idem-%d", i),
			Status:         StatusPending,
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        json.RawMessage(`{"old":true}`),
		}
		data, _ := json.Marshal(entry)
		crcHex := fmt.Sprintf("%08x", crc32.ChecksumIEEE(data))
		writeRawLine(t, seg, fmt.Sprintf("%s\t%s", data, crcHex))
	}

	w2 := reopen(t, dir)
	var replayed []Entry
	if err := w2.Replay(func(e Entry) { replayed = append(replayed, e) }); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(replayed) != 5 {
		t.Fatalf("want 5 entries, got %d", len(replayed))
	}
	for i, e := range replayed {
		// v0.1.2 entries have no MonotonicSeq — field is zero.
		if e.MonotonicSeq != 0 {
			t.Errorf("entry %d: v0.1.2 entry should have MonotonicSeq=0, got %d", i, e.MonotonicSeq)
		}
	}
}

// TestSeq_HighestSeq verifies that HighestSeq() scans correctly.
func TestSeq_HighestSeq(t *testing.T) {
	dir := t.TempDir()
	seqFn, _ := testSeqFn()
	w, err := Open(dir, 50, testLogger(), WithSequence(seqFn))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := 0; i < 10; i++ {
		if err := w.Append(Entry{
			PayloadID:      fmt.Sprintf("hs-%d", i),
			IdempotencyKey: fmt.Sprintf("hs-idem-%d", i),
			Payload:        json.RawMessage(`{}`),
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	highest, err := w2.HighestSeq()
	if err != nil {
		t.Fatalf("HighestSeq: %v", err)
	}
	if highest != 10 {
		t.Errorf("want HighestSeq=10, got %d", highest)
	}
}

// TestSeq_ReplayOrdering writes entries with intentionally out-of-order
// wall-clock times and verifies replay order matches sequence order.
func TestSeq_ReplayOrdering(t *testing.T) {
	dir := t.TempDir()
	seqFn, _ := testSeqFn()
	w, err := Open(dir, 50, testLogger(), WithSequence(seqFn))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Write entries with deliberately reversed timestamps.
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		entry := Entry{
			PayloadID:      fmt.Sprintf("order-%d", i),
			IdempotencyKey: fmt.Sprintf("order-idem-%d", i),
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			// Timestamps go backwards: entry 0 has the latest time.
			Timestamp: baseTime.Add(time.Duration(5-i) * time.Hour),
			Payload:   json.RawMessage(`{}`),
		}
		if err := w.Append(entry); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	var replayed []Entry
	if err := w2.Replay(func(e Entry) { replayed = append(replayed, e) }); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(replayed) != 5 {
		t.Fatalf("want 5, got %d", len(replayed))
	}

	// Verify MonotonicSeq is strictly increasing despite wall-clock going backwards.
	for i := 1; i < len(replayed); i++ {
		if replayed[i].MonotonicSeq <= replayed[i-1].MonotonicSeq {
			t.Errorf("entry %d: MonotonicSeq %d not > %d",
				i, replayed[i].MonotonicSeq, replayed[i-1].MonotonicSeq)
		}
	}

	// Verify wall-clock is NOT monotonic (intentionally reversed).
	wallClockMonotonic := true
	for i := 1; i < len(replayed); i++ {
		if replayed[i].Timestamp.Before(replayed[i-1].Timestamp) {
			wallClockMonotonic = false
			break
		}
	}
	if wallClockMonotonic {
		t.Error("expected wall-clock timestamps to be non-monotonic (test setup issue)")
	}
}
