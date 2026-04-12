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
	"testing"
)

// TestCheckpoint_WriteAndFind writes entries with periodic checkpoints and
// verifies FindLatestCheckpoint returns the most recent one.
func TestCheckpoint_WriteAndFind(t *testing.T) {
	w, _ := openTestWAL(t)

	// Write 100 entries with a checkpoint every 25.
	for i := 0; i < 100; i++ {
		if err := w.Append(Entry{
			PayloadID:      fmt.Sprintf("cp-p%d", i),
			IdempotencyKey: fmt.Sprintf("cp-k%d", i),
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        json.RawMessage(`{"x":1}`),
		}); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
		if (i+1)%25 == 0 {
			hash := ComputeStateHash(int64(i+1), fmt.Sprintf("digest-%d", i+1))
			if err := w.WriteCheckpoint(int64(i+1), hash); err != nil {
				t.Fatalf("WriteCheckpoint at %d: %v", i+1, err)
			}
		}
	}

	cp, err := w.FindLatestCheckpoint()
	if err != nil {
		t.Fatalf("FindLatestCheckpoint: %v", err)
	}
	if cp == nil {
		t.Fatal("expected a checkpoint, got nil")
	}
	if cp.Data.AppliedCount != 100 {
		t.Errorf("checkpoint applied_count: want 100, got %d", cp.Data.AppliedCount)
	}
}

// TestCheckpoint_ReplayFromCheckpoint writes 100 entries with checkpoints,
// marks some delivered, then verifies that ReplayFromCheckpoint replays
// only entries after the checkpoint.
func TestCheckpoint_ReplayFromCheckpoint(t *testing.T) {
	w, dir := openTestWAL(t)

	// Write 50 entries, checkpoint, then 50 more.
	for i := 0; i < 50; i++ {
		if err := w.Append(Entry{
			PayloadID:      fmt.Sprintf("cp-p%d", i),
			IdempotencyKey: fmt.Sprintf("cp-k%d", i),
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        json.RawMessage(`{"x":1}`),
		}); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	// Mark first 50 as delivered so they won't be replayed.
	for i := 0; i < 50; i++ {
		if err := w.MarkDelivered(fmt.Sprintf("cp-p%d", i)); err != nil {
			t.Fatalf("MarkDelivered(%d): %v", i, err)
		}
	}

	hash := ComputeStateHash(50, "digest-50")
	if err := w.WriteCheckpoint(50, hash); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}

	// Write 50 more entries (these are PENDING).
	for i := 50; i < 100; i++ {
		if err := w.Append(Entry{
			PayloadID:      fmt.Sprintf("cp-p%d", i),
			IdempotencyKey: fmt.Sprintf("cp-k%d", i),
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        json.RawMessage(`{"x":1}`),
		}); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and replay from checkpoint — should get only the 50 PENDING entries.
	w2 := reopen(t, dir)
	cp, err := w2.FindLatestCheckpoint()
	if err != nil {
		t.Fatalf("FindLatestCheckpoint: %v", err)
	}
	if cp == nil {
		t.Fatal("expected checkpoint, got nil")
	}

	var replayed []Entry
	if err := w2.ReplayFromCheckpoint(cp, func(e Entry) {
		replayed = append(replayed, e)
	}); err != nil {
		t.Fatalf("ReplayFromCheckpoint: %v", err)
	}

	if len(replayed) != 50 {
		t.Errorf("want 50 entries after checkpoint, got %d", len(replayed))
	}

	// Verify all replayed entries have payload IDs >= 50.
	for _, e := range replayed {
		var id int
		if _, err := fmt.Sscanf(e.PayloadID, "cp-p%d", &id); err != nil {
			t.Errorf("parse payload ID %q: %v", e.PayloadID, err)
			continue
		}
		if id < 50 {
			t.Errorf("replayed entry %d should have been before checkpoint", id)
		}
	}
}

// TestCheckpoint_NoCheckpointFallsBackToFullReplay verifies that when no
// checkpoints exist, ReplayFromCheckpoint with nil falls back to Replay.
func TestCheckpoint_NoCheckpointFallsBackToFullReplay(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 20)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	cp, err := w2.FindLatestCheckpoint()
	if err != nil {
		t.Fatalf("FindLatestCheckpoint: %v", err)
	}
	if cp != nil {
		t.Fatal("expected nil checkpoint, got non-nil")
	}

	var replayed []Entry
	if err := w2.ReplayFromCheckpoint(nil, func(e Entry) {
		replayed = append(replayed, e)
	}); err != nil {
		t.Fatalf("ReplayFromCheckpoint(nil): %v", err)
	}

	if len(replayed) != 20 {
		t.Errorf("full replay: want 20, got %d", len(replayed))
	}
}

// TestCheckpoint_ReplaySkipsCheckpointEntries verifies that standard Replay
// skips checkpoint entries (they are not data entries).
func TestCheckpoint_ReplaySkipsCheckpointEntries(t *testing.T) {
	w, dir := openTestWAL(t)

	appendN(t, w, 5)
	if err := w.WriteCheckpoint(5, "hash"); err != nil {
		t.Fatalf("WriteCheckpoint: %v", err)
	}
	appendN(t, w, 0) // nothing extra

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	// Should have 5 data entries, checkpoint entry is skipped.
	if len(replayed) != 5 {
		t.Errorf("want 5 data entries (checkpoint skipped), got %d", len(replayed))
	}
}

// TestCheckpoint_ComputeStateHash verifies deterministic hash computation.
func TestCheckpoint_ComputeStateHash(t *testing.T) {
	h1 := ComputeStateHash(100, "abc")
	h2 := ComputeStateHash(100, "abc")
	h3 := ComputeStateHash(101, "abc")

	if h1 != h2 {
		t.Error("same inputs should produce same hash")
	}
	if h1 == h3 {
		t.Error("different inputs should produce different hash")
	}
	if len(h1) != 64 {
		t.Errorf("SHA-256 hex should be 64 chars, got %d", len(h1))
	}
}
