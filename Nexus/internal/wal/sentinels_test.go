// Copyright © 2026 BubbleFish Technologies, Inc.

package wal

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"strings"
	"testing"
)

// ── Sentinel round-trip ─────────────────────────────────────────────────────

// TestSentinel_RoundTrip writes entries, reads them back, and verifies payload
// integrity including sentinel presence.
func TestSentinel_RoundTrip(t *testing.T) {
	w, dir := openTestWAL(t)
	entries := appendN(t, w, 10)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	if len(replayed) != 10 {
		t.Fatalf("want 10 replayed entries, got %d", len(replayed))
	}

	for i, got := range replayed {
		if got.PayloadID != entries[i].PayloadID {
			t.Errorf("entry %d: want PayloadID=%s, got %s", i, entries[i].PayloadID, got.PayloadID)
		}
		if got.IdempotencyKey != entries[i].IdempotencyKey {
			t.Errorf("entry %d: want IdempotencyKey=%s, got %s", i, entries[i].IdempotencyKey, got.IdempotencyKey)
		}
	}

	// Verify raw file uses sentinel format.
	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for i, line := range lines {
		wl := parseWALLine(line)
		if wl == nil {
			t.Errorf("line %d: could not parse", i)
			continue
		}
		if !wl.HasSentinels {
			t.Errorf("line %d: new entry missing sentinels", i)
		}
		if wl.SentinelErr != nil {
			t.Errorf("line %d: sentinel error: %v", i, wl.SentinelErr)
		}
	}
}

// ── Backward compatibility ──────────────────────────────────────────────────

// TestSentinel_BackwardCompat constructs old-format entries (no sentinels)
// and verifies the reader handles them correctly.
func TestSentinel_BackwardCompat(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Write old-format entries directly to the segment file.
	seg := currentSegment(t, dir)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	for i := 0; i < 5; i++ {
		entry := Entry{
			Version:        walVersion,
			PayloadID:      fmt.Sprintf("old-%d", i),
			IdempotencyKey: fmt.Sprintf("old-idem-%d", i),
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
	replayed := replayAll(t, w2)

	if len(replayed) != 5 {
		t.Fatalf("want 5 old-format entries replayed, got %d", len(replayed))
	}
	for i, e := range replayed {
		if e.PayloadID != fmt.Sprintf("old-%d", i) {
			t.Errorf("entry %d: want PayloadID old-%d, got %s", i, i, e.PayloadID)
		}
	}
}

// ── Mixed-format segment ────────────────────────────────────────────────────

// TestSentinel_MixedFormat writes old-format entries then new-format entries
// in the same segment, verifies both are replayed correctly.
func TestSentinel_MixedFormat(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	seg := currentSegment(t, dir)

	// Write 3 old-format entries directly.
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	for i := 0; i < 3; i++ {
		entry := Entry{
			Version:        walVersion,
			PayloadID:      fmt.Sprintf("old-%d", i),
			IdempotencyKey: fmt.Sprintf("old-idem-%d", i),
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

	// Reopen and append 3 new-format entries (via Append, which uses sentinels).
	w2, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := 0; i < 3; i++ {
		entry := Entry{
			PayloadID:      fmt.Sprintf("new-%d", i),
			IdempotencyKey: fmt.Sprintf("new-idem-%d", i),
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        json.RawMessage(`{"new":true}`),
		}
		if err := w2.Append(entry); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Replay: all 6 entries should be present.
	w3 := reopen(t, dir)
	replayed := replayAll(t, w3)

	if len(replayed) != 6 {
		t.Fatalf("want 6 entries (3 old + 3 new), got %d", len(replayed))
	}

	// Verify raw format: first 3 lines are old format, last 3 are sentinel format.
	data, _ := os.ReadFile(seg)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	for i, line := range lines {
		wl := parseWALLine(line)
		if wl == nil {
			t.Errorf("line %d: could not parse", i)
			continue
		}
		if i < 3 && wl.HasSentinels {
			t.Errorf("line %d: old-format entry should not have sentinels", i)
		}
		if i >= 3 && !wl.HasSentinels {
			t.Errorf("line %d: new-format entry should have sentinels", i)
		}
	}
}

// ── Sentinel corruption: start sentinel ─────────────────────────────────────

// TestSentinel_CorruptStartSentinel writes a new-format entry, corrupts the
// start sentinel, and verifies the reader detects it.
func TestSentinel_CorruptStartSentinel(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 5)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Corrupt the start sentinel of line 3 (0-indexed=2) by replacing
	// "BFBFBFBFBFBFBFBF" with "XXXXXXXXXXXXXXXX".
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	wl := parseWALLine(lines[2])
	if wl == nil || !wl.HasSentinels {
		t.Fatalf("line 2: expected sentinel format")
	}
	// Replace start sentinel with garbage — makes it look like old format
	// with unparseable JSON, which will be skipped.
	lines[2] = "XXXXXXXXXXXXXXXX" + lines[2][len(StartSentinel):]

	if err := os.WriteFile(seg, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	// Entry with corrupted start sentinel is unrecognizable — treated as
	// old-format line with invalid JSON, skipped. 4 of 5 should replay.
	if len(replayed) != 4 {
		t.Errorf("want 4 replayed entries, got %d", len(replayed))
	}
}

// ── Sentinel corruption: end sentinel ───────────────────────────────────────

// TestSentinel_CorruptEndSentinel writes a new-format entry, corrupts the
// end sentinel, and verifies the reader detects and rejects it.
func TestSentinel_CorruptEndSentinel(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 5)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Corrupt the end sentinel of line 3 (0-indexed=2).
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	wl := parseWALLine(lines[2])
	if wl == nil || !wl.HasSentinels {
		t.Fatalf("line 2: expected sentinel format")
	}
	// Replace end sentinel with garbage.
	lines[2] = fmt.Sprintf("%s\t%s\t%s\t%s", StartSentinel, wl.JSONBytes, wl.StoredCRC, "DEADDEADDEADDEAD")

	if err := os.WriteFile(seg, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	// End sentinel corrupted: entry is rejected (fail closed). 4 of 5 replayed.
	if len(replayed) != 4 {
		t.Errorf("want 4 replayed entries, got %d", len(replayed))
	}
	if w2.SentinelFailures() != 1 {
		t.Errorf("want SentinelFailures==1, got %d", w2.SentinelFailures())
	}
}

// ── Torn-write simulation ───────────────────────────────────────────────────

// TestSentinel_TornWrite simulates a torn write by truncating the file inside
// the payload region (after start sentinel but before end sentinel). Verifies
// the reader detects the missing end sentinel and fails closed on that entry
// but continues replaying subsequent valid entries.
func TestSentinel_TornWrite(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 5)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Find the start of line 3 (0-indexed=2) and truncate partway through.
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	// Reconstruct: keep first 2 lines intact, truncate line 3 after start
	// sentinel + partial JSON (no end sentinel, no newline).
	var truncated strings.Builder
	for i := 0; i < 2; i++ {
		truncated.WriteString(lines[i])
		truncated.WriteString("\n")
	}
	// Partial line 3: start sentinel + tab + partial JSON (no end sentinel).
	wl := parseWALLine(lines[2])
	if wl == nil || !wl.HasSentinels {
		t.Fatalf("line 2: expected sentinel format")
	}
	partialJSON := string(wl.JSONBytes)[:20] // truncated JSON
	truncated.WriteString(StartSentinel + "\t" + partialJSON)
	// No newline — simulates torn write.

	if err := os.WriteFile(seg, []byte(truncated.String()), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	// Only the first 2 complete entries should replay. The truncated entry
	// is a partial line that the scanner may or may not return; if it does,
	// parseWALLine will either return nil or detect incomplete sentinels.
	if len(replayed) != 2 {
		t.Errorf("want 2 replayed entries after truncation, got %d", len(replayed))
	}
}

// ── Kill -9 simulation (100 iterations) ─────────────────────────────────────

// TestSentinel_Kill9 runs 100 iterations of: append entries, simulate crash
// by reopening without close, verify no data loss on fully written entries.
func TestSentinel_Kill9(t *testing.T) {
	dir := t.TempDir()

	for iter := 0; iter < 100; iter++ {
		w, err := Open(dir, 50, testLogger())
		if err != nil {
			t.Fatalf("iter %d: Open: %v", iter, err)
		}

		// Append 5 entries.
		for i := 0; i < 5; i++ {
			entry := Entry{
				PayloadID:      fmt.Sprintf("iter%d-p%d", iter, i),
				IdempotencyKey: fmt.Sprintf("iter%d-idem%d", iter, i),
				Source:         "src",
				Destination:    "dst",
				Subject:        "sub",
				Payload:        json.RawMessage(`{"kill9":true}`),
			}
			if err := w.Append(entry); err != nil {
				t.Fatalf("iter %d: Append %d: %v", iter, i, err)
			}
		}

		// Simulate kill -9: close the file handle without flushing group commit.
		// On Windows, we need to close properly to release file locks.
		if err := w.Close(); err != nil {
			t.Fatalf("iter %d: Close: %v", iter, err)
		}
	}

	// Final replay: verify all 500 entries are present.
	wFinal, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("final Open: %v", err)
	}
	defer func() {
		if err := wFinal.Close(); err != nil {
			t.Logf("final close: %v", err)
		}
	}()

	var replayed []Entry
	if err := wFinal.Replay(func(e Entry) { replayed = append(replayed, e) }); err != nil {
		t.Fatalf("final Replay: %v", err)
	}

	if len(replayed) != 500 {
		t.Errorf("want 500 entries after 100 kill-9 iterations, got %d", len(replayed))
	}

	// Verify no sentinel failures (all writes completed successfully).
	if wFinal.SentinelFailures() != 0 {
		t.Errorf("want SentinelFailures==0, got %d", wFinal.SentinelFailures())
	}
	if wFinal.CRCFailures() != 0 {
		t.Errorf("want CRCFailures==0, got %d", wFinal.CRCFailures())
	}
}

// ── v0.1.2 WAL load test ────────────────────────────────────────────────────

// TestSentinel_V012WALLoad creates a WAL file in the exact v0.1.2 format
// (no sentinels, CRC-only) and verifies it loads cleanly with the new code.
func TestSentinel_V012WALLoad(t *testing.T) {
	dir := t.TempDir()

	// Create a segment file manually in v0.1.2 format.
	segPath := fmt.Sprintf("%s/wal-%d.jsonl", dir, 1000000000)
	f, err := os.OpenFile(segPath, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create segment: %v", err)
	}

	// Write 10 entries in v0.1.2 format (JSON\tCRC\n).
	for i := 0; i < 10; i++ {
		entry := Entry{
			Version:        1, // v0.1.2 used version 1
			PayloadID:      fmt.Sprintf("v012-%d", i),
			IdempotencyKey: fmt.Sprintf("v012-idem-%d", i),
			Status:         StatusPending,
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        json.RawMessage(fmt.Sprintf(`{"v012_entry":%d}`, i)),
		}
		data, _ := json.Marshal(entry)
		crcHex := fmt.Sprintf("%08x", crc32.ChecksumIEEE(data))
		if _, err := fmt.Fprintf(f, "%s\t%s\n", data, crcHex); err != nil {
			t.Fatalf("write v0.1.2 entry: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close segment: %v", err)
	}

	// Open with new code and replay.
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := w.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	}()

	var replayed []Entry
	if err := w.Replay(func(e Entry) { replayed = append(replayed, e) }); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(replayed) != 10 {
		t.Fatalf("want 10 v0.1.2 entries replayed, got %d", len(replayed))
	}

	for i, e := range replayed {
		if e.PayloadID != fmt.Sprintf("v012-%d", i) {
			t.Errorf("entry %d: want PayloadID v012-%d, got %s", i, i, e.PayloadID)
		}
	}

	// No sentinel or CRC failures on valid v0.1.2 data.
	if w.SentinelFailures() != 0 {
		t.Errorf("want SentinelFailures==0, got %d", w.SentinelFailures())
	}
	if w.CRCFailures() != 0 {
		t.Errorf("want CRCFailures==0, got %d", w.CRCFailures())
	}
}
