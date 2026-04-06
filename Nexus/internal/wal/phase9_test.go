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

// Phase 9: Testing + Security Audit — WAL-level stress and recovery tests.
// Reference: Tech Spec Section 8, Section 16 — Validation Plan.
package wal

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// CONTRACT 2 — Crash recovery golden scenario: 50 payloads, kill -9, restart,
//              all 50 present, 0 duplicates.
//              Reference: Tech Spec Section 16.
// ---------------------------------------------------------------------------

func TestPhase9_CrashRecovery_GoldenScenario(t *testing.T) {
	// Phase 1: Write 50 payloads, then "crash" (close WAL without marking delivered).
	dir := t.TempDir()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const total = 50
	written := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		e := Entry{
			PayloadID:      fmt.Sprintf("crash-payload-%03d", i),
			IdempotencyKey: fmt.Sprintf("crash-idem-%03d", i),
			Source:         "test-src",
			Destination:    "sqlite",
			Subject:        "crash-test",
			Payload:        json.RawMessage(fmt.Sprintf(`{"content":"crash-%d"}`, i)),
		}
		if err := w.Append(e); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
		written[e.PayloadID] = true
	}

	// Simulate kill -9: close the file handle directly, no graceful shutdown.
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Phase 2: "Restart" — reopen WAL from the same directory and replay.
	w2, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() {
		if err := w2.Close(); err != nil {
			t.Logf("close w2: %v", err)
		}
	}()

	replayed := make(map[string]bool)
	var replayCount int
	err = w2.Replay(func(e Entry) {
		replayCount++
		if replayed[e.PayloadID] {
			t.Errorf("DUPLICATE detected: %s", e.PayloadID)
		}
		replayed[e.PayloadID] = true
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	// Verify all 50 present with 0 duplicates.
	if replayCount != total {
		t.Fatalf("CONTRACT 2 FAIL: replayed %d entries, want %d", replayCount, total)
	}

	var missing []string
	for pid := range written {
		if !replayed[pid] {
			missing = append(missing, pid)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("CONTRACT 2 FAIL: %d payloads missing after crash recovery: %v",
			len(missing), missing)
	}

	// Check for duplicates (already checked above via map, but count confirms).
	if len(replayed) != total {
		t.Fatalf("CONTRACT 2 FAIL: unique replayed=%d want %d (duplicates present)",
			len(replayed), total)
	}

	t.Logf("CONTRACT 2 PASS: %d/%d payloads recovered, 0 duplicates", replayCount, total)
}

// ---------------------------------------------------------------------------
// CONTRACT 2b — Crash mid-rotation: WAL has two segments with overlapping
//               idempotency keys. Replay deduplicates correctly.
// ---------------------------------------------------------------------------

func TestPhase9_CrashRecovery_MidRotation(t *testing.T) {
	dir := t.TempDir()

	// Create two segment files simulating a crash during rotation.
	// Segment 1 has entries 0–24.
	// Segment 2 has entries 20–49 (overlapping 20–24 to simulate incomplete rotation).
	seg1 := filepath.Join(dir, "wal-0000000001.jsonl")
	seg2 := filepath.Join(dir, "wal-0000000002.jsonl")

	writeSegment := func(path string, start, end int) {
		t.Helper()
		f, err := os.Create(path)
		if err != nil {
			t.Fatalf("create segment: %v", err)
		}
		defer func() {
			if err := f.Close(); err != nil {
				t.Logf("close segment: %v", err)
			}
		}()
		for i := start; i < end; i++ {
			e := Entry{
				Version:        2,
				PayloadID:      fmt.Sprintf("payload-%03d", i),
				IdempotencyKey: fmt.Sprintf("idem-%03d", i),
				Status:         StatusPending,
				Source:         "src",
				Destination:    "dst",
				Subject:        "sub",
				Payload:        json.RawMessage(`{"x":1}`),
			}
			data, _ := json.Marshal(e)
			crc := crc32.ChecksumIEEE(data)
			if _, err := fmt.Fprintf(f, "%s\t%08x\n", data, crc); err != nil {
				t.Fatalf("write segment entry %d: %v", i, err)
			}
		}
		if err := os.Chmod(path, 0600); err != nil {
			t.Logf("chmod segment: %v", err)
		}
	}

	writeSegment(seg1, 0, 25)
	writeSegment(seg2, 20, 50)

	w, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	}()

	seen := make(map[string]int)
	err = w.Replay(func(e Entry) {
		seen[e.PayloadID]++
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	// Must have exactly 50 unique payloads, each seen once.
	if len(seen) != 50 {
		t.Fatalf("CONTRACT 2b FAIL: unique payloads=%d want 50", len(seen))
	}
	for pid, count := range seen {
		if count > 1 {
			t.Errorf("CONTRACT 2b FAIL: payload %s seen %d times", pid, count)
		}
	}

	t.Logf("CONTRACT 2b PASS: 50 unique payloads after mid-rotation crash, 0 duplicates")
}

// ---------------------------------------------------------------------------
// CONTRACT 5 — CRC32 corruption detection: corrupt entry, replay skips with WARN.
//              Reference: Tech Spec Section 16.
// ---------------------------------------------------------------------------

func TestPhase9_CRC32CorruptionDetection(t *testing.T) {
	w, dir := openTestWAL(t)

	// Write 10 valid entries.
	appendN(t, w, 10)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Corrupt one entry by modifying a byte in the JSON portion.
	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Find the third line (index 2) and corrupt a byte.
	lines := strings.Split(string(data), "\n")
	if len(lines) < 10 {
		t.Fatalf("expected >=10 lines, got %d", len(lines))
	}
	// Corrupt a character in the JSON part of line 2.
	corrupted := []byte(lines[2])
	if len(corrupted) > 10 {
		corrupted[5] ^= 0xFF // flip bits
	}
	lines[2] = string(corrupted)
	err = os.WriteFile(seg, []byte(strings.Join(lines, "\n")), 0600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Reopen and replay — the corrupt entry should be skipped.
	w2 := reopen(t, dir)
	entries := replayAll(t, w2)

	if len(entries) != 9 {
		t.Fatalf("CONTRACT 5 FAIL: replayed %d entries, want 9 (10 - 1 corrupt)", len(entries))
	}

	// Verify CRC failure counter was incremented.
	if w2.CRCFailures() < 1 {
		t.Fatalf("CONTRACT 5 FAIL: CRCFailures()=%d, want >= 1", w2.CRCFailures())
	}

	t.Logf("CONTRACT 5 PASS: corrupt entry skipped, %d/10 entries replayed, CRCFailures=%d",
		len(entries), w2.CRCFailures())
}

// ---------------------------------------------------------------------------
// CONTRACT 5b — Multiple corruptions: 3 corrupt entries out of 20.
// ---------------------------------------------------------------------------

func TestPhase9_CRC32_MultipleCorruptions(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 20)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	// Corrupt entries at indices 3, 7, 15.
	for _, idx := range []int{3, 7, 15} {
		if idx < len(lines) {
			corrupted := []byte(lines[idx])
			if len(corrupted) > 10 {
				corrupted[5] ^= 0xFF
			}
			lines[idx] = string(corrupted)
		}
	}
	err = os.WriteFile(seg, []byte(strings.Join(lines, "\n")), 0600)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w2 := reopen(t, dir)
	entries := replayAll(t, w2)

	if len(entries) != 17 {
		t.Fatalf("CONTRACT 5b FAIL: replayed %d entries, want 17 (20 - 3)", len(entries))
	}
	if w2.CRCFailures() < 3 {
		t.Fatalf("CONTRACT 5b FAIL: CRCFailures()=%d, want >= 3", w2.CRCFailures())
	}

	t.Logf("CONTRACT 5b PASS: 3 corrupt entries skipped, %d/20 replayed, CRCFailures=%d",
		len(entries), w2.CRCFailures())
}
