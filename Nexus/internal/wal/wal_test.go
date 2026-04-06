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
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func openTestWAL(t *testing.T) (*WAL, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return w, dir
}

func appendN(t *testing.T, w *WAL, n int) []Entry {
	t.Helper()
	entries := make([]Entry, n)
	for i := range entries {
		entries[i] = Entry{
			PayloadID:      fmt.Sprintf("payload-%d", i),
			IdempotencyKey: fmt.Sprintf("idem-%d", i),
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        json.RawMessage(`{"x":1}`),
		}
		if err := w.Append(entries[i]); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	return entries
}

func reopen(t *testing.T, dir string) *WAL {
	t.Helper()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("reopen WAL: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return w
}

func replayAll(t *testing.T, w *WAL) []Entry {
	t.Helper()
	var out []Entry
	if err := w.Replay(func(e Entry) { out = append(out, e) }); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return out
}

// writeRawLine writes a raw WAL line directly to the segment, bypassing the
// WAL struct. Used only in tests to simulate crash/corrupt scenarios.
func writeRawLine(t *testing.T, segPath, line string) {
	t.Helper()
	f, err := os.OpenFile(segPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("writeRawLine open: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	}()
	if _, err := fmt.Fprintln(f, line); err != nil {
		t.Fatalf("writeRawLine write: %v", err)
	}
}

func currentSegment(t *testing.T, dir string) string {
	t.Helper()
	segs, err := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	if err != nil || len(segs) == 0 {
		t.Fatalf("currentSegment: no segments found in %s", dir)
	}
	return segs[len(segs)-1]
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestWALAppendCRC32 verifies every written line contains a tab-separated
// 8-char hex CRC32 over the JSON bytes.
func TestWALAppendCRC32(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 10)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 10 {
		t.Fatalf("want 10 lines, got %d", len(lines))
	}

	for i, line := range lines {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			t.Errorf("line %d: no tab separator", i)
			continue
		}
		jsonBytes := []byte(parts[0])
		storedCRC := parts[1]
		wantCRC := fmt.Sprintf("%08x", crc32.ChecksumIEEE(jsonBytes))
		if storedCRC != wantCRC {
			t.Errorf("line %d: CRC mismatch: stored=%s want=%s", i, storedCRC, wantCRC)
		}
		if len(storedCRC) != 8 {
			t.Errorf("line %d: CRC not 8 chars: %q", i, storedCRC)
		}
	}
}

// TestWALReplayCorruptEntry writes 10 entries, corrupts the 5th, and verifies
// that exactly 9 entries are replayed and crcFailures == 1.
func TestWALReplayCorruptEntry(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 10)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Flip a byte in the middle of the 5th line's JSON region.
	lines := strings.Split(string(data), "\n")
	if len(lines) < 10 {
		t.Fatalf("expected >=10 lines")
	}
	// Corrupt the JSON bytes of line 4 (0-indexed) by flipping a byte.
	parts := strings.SplitN(lines[4], "\t", 2)
	if len(parts) == 2 {
		corrupted := []byte(parts[0])
		corrupted[10] ^= 0xFF
		lines[4] = string(corrupted) + "\t" + parts[1]
	}
	if err := os.WriteFile(seg, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	if len(replayed) != 9 {
		t.Errorf("want 9 replayed entries, got %d", len(replayed))
	}
	if w2.CRCFailures() != 1 {
		t.Errorf("want CRCFailures==1, got %d", w2.CRCFailures())
	}
}

// TestWALReplayAllDelivered writes 10 entries, marks all delivered, restarts,
// and expects zero entries replayed.
func TestWALReplayAllDelivered(t *testing.T) {
	w, dir := openTestWAL(t)
	entries := appendN(t, w, 10)

	for _, e := range entries {
		if err := w.MarkDelivered(e.PayloadID); err != nil {
			t.Fatalf("MarkDelivered(%s): %v", e.PayloadID, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	if len(replayed) != 0 {
		t.Errorf("want 0 replayed entries, got %d", len(replayed))
	}
}

// TestWALReplayPartialDelivered writes 10, marks 5 delivered, restarts, and
// expects exactly 5 entries replayed.
func TestWALReplayPartialDelivered(t *testing.T) {
	w, dir := openTestWAL(t)
	entries := appendN(t, w, 10)

	for _, e := range entries[:5] {
		if err := w.MarkDelivered(e.PayloadID); err != nil {
			t.Fatalf("MarkDelivered: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	if len(replayed) != 5 {
		t.Errorf("want 5 replayed entries, got %d", len(replayed))
	}
}

// TestWALReplayEmpty verifies that an empty WAL produces zero entries and no error.
func TestWALReplayEmpty(t *testing.T) {
	w, _ := openTestWAL(t)
	replayed := replayAll(t, w)
	if len(replayed) != 0 {
		t.Errorf("want 0 entries from empty WAL, got %d", len(replayed))
	}
}

// TestWALCrashMidWrite simulates a power loss mid-write by appending partial
// JSON (no CRC, no newline) to the segment. Replay must skip the partial line
// and return all complete entries.
func TestWALCrashMidWrite(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 5)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	seg := currentSegment(t, dir)

	// Append a partial line: valid JSON start, no tab, no newline.
	f, err := os.OpenFile(seg, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open segment: %v", err)
	}
	_, _ = fmt.Fprint(f, `{"version":2,"payload_id":"partial","status":"PENDING"`)
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	if len(replayed) != 5 {
		t.Errorf("want 5 complete entries, got %d", len(replayed))
	}
}

// TestWALLargeEntry writes and replays an entry larger than the default 64KB
// bufio.Scanner buffer. Verifies the 10MB buffer is in effect.
func TestWALLargeEntry(t *testing.T) {
	w, dir := openTestWAL(t)

	largeContent := strings.Repeat("A", 70000) // ~70 KB — exceeds default 64 KB buffer
	entry := Entry{
		PayloadID:      "large-1",
		IdempotencyKey: "large-idem-1",
		Source:         "src",
		Destination:    "dst",
		Subject:        "sub",
		Payload:        json.RawMessage(fmt.Sprintf(`{"content":%q}`, largeContent)),
	}
	if err := w.Append(entry); err != nil {
		t.Fatalf("Append large entry: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	if len(replayed) != 1 {
		t.Fatalf("want 1 replayed entry, got %d", len(replayed))
	}
	if replayed[0].PayloadID != "large-1" {
		t.Errorf("wrong payload_id: %s", replayed[0].PayloadID)
	}
}

// TestWALCrashMidRotation simulates a crash during segment rotation by creating
// two segments on disk. Both must be replayed; duplicate idempotency keys are
// deduplicated.
func TestWALCrashMidRotation(t *testing.T) {
	w, dir := openTestWAL(t)
	// Write 5 entries to the first segment.
	appendN(t, w, 5)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Simulate a second segment (created during rotation before crash).
	// Entry 3 has the same idempotency key as the one written above — it must
	// be deduplicated on replay.
	time.Sleep(2 * time.Millisecond) // ensure UnixNano-named segment sorts after first
	seg2 := filepath.Join(dir, fmt.Sprintf("wal-%d.jsonl", time.Now().UnixNano()))
	f, err := os.OpenFile(seg2, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create seg2: %v", err)
	}
	// 5 new entries + 1 duplicate (idem-3 matches entry written above).
	extraEntries := []Entry{
		{PayloadID: "extra-0", IdempotencyKey: "extra-idem-0", Source: "s", Destination: "d", Subject: "sub", Payload: json.RawMessage(`{}`)},
		{PayloadID: "extra-1", IdempotencyKey: "extra-idem-1", Source: "s", Destination: "d", Subject: "sub", Payload: json.RawMessage(`{}`)},
		{PayloadID: "extra-2", IdempotencyKey: "extra-idem-2", Source: "s", Destination: "d", Subject: "sub", Payload: json.RawMessage(`{}`)},
		{PayloadID: "extra-3", IdempotencyKey: "extra-idem-3", Source: "s", Destination: "d", Subject: "sub", Payload: json.RawMessage(`{}`)},
		{PayloadID: "extra-4", IdempotencyKey: "extra-idem-4", Source: "s", Destination: "d", Subject: "sub", Payload: json.RawMessage(`{}`)},
		// Duplicate: same idempotency key as the entry with idem-3 from segment 1.
		{PayloadID: "dup-payload", IdempotencyKey: "idem-3", Source: "s", Destination: "d", Subject: "sub", Payload: json.RawMessage(`{}`)},
	}
	for _, e := range extraEntries {
		e.Version = walVersion
		e.Status = StatusPending
		e.Timestamp = time.Now().UTC()
		data, _ := json.Marshal(e)
		crc := fmt.Sprintf("%08x", crc32.ChecksumIEEE(data))
		if _, err := fmt.Fprintf(f, "%s\t%s\n", data, crc); err != nil {
			t.Fatalf("write seg2 entry: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Logf("close seg2: %v", err)
	}

	// Reopen: WAL will use seg2 as the active segment, seg1 as an older segment.
	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	// Expect: 5 from seg1 + 5 from seg2 (1 duplicate deduplicated) = 10.
	if len(replayed) != 10 {
		t.Errorf("want 10 unique entries, got %d", len(replayed))
	}

	// Verify no duplicate payload IDs in output.
	seen := make(map[string]bool)
	for _, e := range replayed {
		if seen[e.IdempotencyKey] {
			t.Errorf("duplicate idempotency key in replay output: %s", e.IdempotencyKey)
		}
		seen[e.IdempotencyKey] = true
	}
}

// TestWALMarkDeliveredTempFileLocation verifies that after a successful
// MarkDelivered, no .tmp files remain in the WAL directory (temp file was
// properly renamed and not leaked to os.TempDir()).
func TestWALMarkDeliveredTempFileLocation(t *testing.T) {
	w, dir := openTestWAL(t)
	entries := appendN(t, w, 3)

	if err := w.MarkDelivered(entries[1].PayloadID); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	// No .tmp files should remain in the WAL directory.
	tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("leftover temp files in WAL directory: %v", tmps)
	}

	// The rewritten segment must show entries[1] as DELIVERED.
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"DELIVERED"`) {
		t.Error("segment does not contain DELIVERED status after MarkDelivered")
	}
}

// TestWALMarkDeliveredNotFound verifies MarkDelivered returns an error (not
// panics) for an unknown payloadID. Callers must log this at WARN level.
func TestWALMarkDeliveredNotFound(t *testing.T) {
	w, _ := openTestWAL(t)
	appendN(t, w, 3)

	err := w.MarkDelivered("nonexistent-id")
	if err == nil {
		t.Error("expected error for nonexistent payloadID, got nil")
	}
}

// TestWALFilePermissions verifies that WAL segment files are created with 0600
// permissions. Skipped on Windows where mode bits are not enforced by the OS.
func TestWALFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}

	w, dir := openTestWAL(t)
	appendN(t, w, 1)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	seg := currentSegment(t, dir)
	info, err := os.Stat(seg)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("want segment perm 0600, got %04o", perm)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Errorf("want WAL dir perm 0700, got %04o", perm)
	}
}

// TestWALNilLoggerPanics verifies that Open panics when logger is nil.
func TestWALNilLoggerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil logger, got none")
		}
	}()
	dir := t.TempDir()
	_, _ = Open(dir, 50, nil) // intentional nil logger to trigger panic
}

// TestWALAppendStatusAlwaysPending verifies that Append always writes PENDING
// regardless of the Status set on the input Entry.
func TestWALAppendStatusAlwaysPending(t *testing.T) {
	w, dir := openTestWAL(t)

	entry := Entry{
		PayloadID:      "p1",
		IdempotencyKey: "k1",
		Status:         StatusDelivered, // should be overridden
		Payload:        json.RawMessage(`{}`),
	}
	if err := w.Append(entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)
	if len(replayed) != 1 {
		t.Fatalf("want 1 entry, got %d", len(replayed))
	}
	if replayed[0].Status != StatusPending {
		t.Errorf("want status PENDING, got %s", replayed[0].Status)
	}
}

// TestWALReplayMalformedJSON writes a line with valid CRC but malformed JSON
// and verifies it is skipped with no error.
func TestWALReplayMalformedJSON(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 3)
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	seg := currentSegment(t, dir)

	// Append a line with valid CRC but invalid JSON.
	badJSON := []byte(`{not valid json`)
	crc := fmt.Sprintf("%08x", crc32.ChecksumIEEE(badJSON))
	f, err := os.OpenFile(seg, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open segment for bad JSON append: %v", err)
	}
	if _, err := fmt.Fprintf(f, "%s\t%s\n", badJSON, crc); err != nil {
		t.Fatalf("write bad JSON line: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Logf("close segment: %v", err)
	}

	w2 := reopen(t, dir)
	replayed := replayAll(t, w2)

	// The 3 valid entries should be replayed; the bad JSON line skipped.
	if len(replayed) != 3 {
		t.Errorf("want 3 entries, got %d", len(replayed))
	}
}

// ---------------------------------------------------------------------------
// PendingCount tests — Reference: Tech Spec Section 4.4.
// ---------------------------------------------------------------------------

// TestPendingCount_AppendIncrements verifies that PendingCount increases with
// each successful Append.
func TestPendingCount_AppendIncrements(t *testing.T) {
	w, _ := openTestWAL(t)

	if got := w.PendingCount(); got != 0 {
		t.Fatalf("fresh WAL: want PendingCount=0, got %d", got)
	}

	appendN(t, w, 5)

	if got := w.PendingCount(); got != 5 {
		t.Fatalf("after 5 appends: want PendingCount=5, got %d", got)
	}
}

// TestPendingCount_MarkDeliveredDecrements verifies that MarkDelivered
// decrements PendingCount.
func TestPendingCount_MarkDeliveredDecrements(t *testing.T) {
	w, _ := openTestWAL(t)

	for i := 0; i < 3; i++ {
		if err := w.Append(Entry{
			PayloadID:      fmt.Sprintf("p%d", i),
			IdempotencyKey: fmt.Sprintf("k%d", i),
			Payload:        json.RawMessage(`{}`),
		}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	if got := w.PendingCount(); got != 3 {
		t.Fatalf("after 3 appends: want PendingCount=3, got %d", got)
	}

	if err := w.MarkDelivered("p1"); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	if got := w.PendingCount(); got != 2 {
		t.Fatalf("after MarkDelivered: want PendingCount=2, got %d", got)
	}
}

// TestPendingCount_MarkPermanentFailureDecrements verifies that
// MarkPermanentFailure decrements PendingCount.
func TestPendingCount_MarkPermanentFailureDecrements(t *testing.T) {
	w, _ := openTestWAL(t)

	if err := w.Append(Entry{
		PayloadID:      "pfail",
		IdempotencyKey: "kfail",
		Payload:        json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if err := w.MarkPermanentFailure("pfail"); err != nil {
		t.Fatalf("MarkPermanentFailure: %v", err)
	}

	if got := w.PendingCount(); got != 0 {
		t.Fatalf("after MarkPermanentFailure: want PendingCount=0, got %d", got)
	}
}

// TestPendingCount_ReplayInitialises verifies that PendingCount is set
// correctly during Replay from existing WAL segments.
func TestPendingCount_ReplayInitialises(t *testing.T) {
	w, dir := openTestWAL(t)

	// Append 5 entries, deliver 2.
	for i := 0; i < 5; i++ {
		if err := w.Append(Entry{
			PayloadID:      fmt.Sprintf("p%d", i),
			IdempotencyKey: fmt.Sprintf("k%d", i),
			Payload:        json.RawMessage(`{}`),
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := w.MarkDelivered("p0"); err != nil {
		t.Fatalf("MarkDelivered p0: %v", err)
	}
	if err := w.MarkDelivered("p1"); err != nil {
		t.Fatalf("MarkDelivered p1: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen — PendingCount starts at 0, Replay should set it to 3.
	w2 := reopen(t, dir)
	if got := w2.PendingCount(); got != 0 {
		t.Fatalf("before Replay: want PendingCount=0, got %d", got)
	}

	replayed := replayAll(t, w2)
	if len(replayed) != 3 {
		t.Fatalf("want 3 replayed entries, got %d", len(replayed))
	}
	if got := w2.PendingCount(); got != 3 {
		t.Fatalf("after Replay: want PendingCount=3, got %d", got)
	}
}
