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
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// testMACKey is a 32-byte HMAC-SHA256 key used in tests.
var testMACKey = []byte("01234567890123456789012345678901")

// openTestWALWithMAC opens a WAL in integrity=mac mode with the test key.
func openTestWALWithMAC(t *testing.T) (*WAL, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := Open(dir, 50, testLogger(),
		WithIntegrity(IntegrityModeMAC, testMACKey),
	)
	if err != nil {
		t.Fatalf("Open with MAC: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Logf("close WAL: %v", err)
		}
	})
	return w, dir
}

// reopenWithMAC reopens a WAL directory in integrity=mac mode.
func reopenWithMAC(t *testing.T, dir string) *WAL {
	t.Helper()
	w, err := Open(dir, 50, testLogger(),
		WithIntegrity(IntegrityModeMAC, testMACKey),
	)
	if err != nil {
		t.Fatalf("reopen with MAC: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Logf("close WAL: %v", err)
		}
	})
	return w
}

// ── HMAC write format ────────────────────────────────────────────────────────

// TestIntegrityMACWriteFormat verifies that when integrity=mac, each WAL line
// is in sentinel format with JSON, CRC32, and HMAC fields.
func TestIntegrityMACWriteFormat(t *testing.T) {
	w, dir := openTestWALWithMAC(t)
	appendN(t, w, 10)
	if err := w.Close(); err != nil {
		t.Logf("close WAL: %v", err)
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
		wl := parseWALLine(line)
		if wl == nil {
			t.Errorf("line %d: could not parse", i)
			continue
		}
		if !wl.HasSentinels {
			t.Errorf("line %d: missing sentinels", i)
		}
		if wl.SentinelErr != nil {
			t.Errorf("line %d: sentinel error: %v", i, wl.SentinelErr)
		}
		if wl.StoredHMAC == "" {
			t.Errorf("line %d: missing HMAC in MAC mode", i)
			continue
		}

		// Verify CRC32.
		wantCRC := fmt.Sprintf("%08x", crc32.ChecksumIEEE(wl.JSONBytes))
		if wl.StoredCRC != wantCRC {
			t.Errorf("line %d: CRC mismatch: stored=%s want=%s", i, wl.StoredCRC, wantCRC)
		}

		// Verify HMAC.
		wantHMAC := computeHMAC(wl.JSONBytes, testMACKey)
		if wl.StoredHMAC != wantHMAC {
			t.Errorf("line %d: HMAC mismatch: stored=%s want=%s", i, wl.StoredHMAC, wantHMAC)
		}

		// HMAC hex should be 64 chars (SHA-256 = 32 bytes = 64 hex chars).
		if len(wl.StoredHMAC) != 64 {
			t.Errorf("line %d: HMAC not 64 chars: %q (len=%d)", i, wl.StoredHMAC, len(wl.StoredHMAC))
		}
	}
}

// ── HMAC replay: tamper detection ────────────────────────────────────────────

// TestIntegrityMACTamperDetected writes 10 entries with integrity=mac, tampers
// with one entry's JSON, and verifies that:
// - Exactly 9 entries are replayed (tampered one skipped).
// - integrityFailures == 1.
// - A wal_tamper_detected security event was emitted with segment and line number.
func TestIntegrityMACTamperDetected(t *testing.T) {
	w, dir := openTestWALWithMAC(t)
	appendN(t, w, 10)
	if err := w.Close(); err != nil { t.Fatalf("close: %v", err) }

	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Tamper with line 5 (0-indexed=4): change a byte in JSON, recompute CRC
	// so CRC passes but HMAC fails. This simulates intentional tampering.
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	wl := parseWALLine(lines[4])
	if wl == nil || !wl.HasSentinels {
		t.Fatalf("expected sentinel format on line 4")
	}
	tampered := make([]byte, len(wl.JSONBytes))
	copy(tampered, wl.JSONBytes)
	tampered[10] ^= 0xFF // flip a byte
	// Recompute CRC over tampered data (attacker can do this).
	newCRC := fmt.Sprintf("%08x", crc32.ChecksumIEEE(tampered))
	// Keep old HMAC — it won't match the tampered data.
	lines[4] = fmt.Sprintf("%s\t%s\t%s\t%s\t%s", StartSentinel, tampered, newCRC, wl.StoredHMAC, EndSentinel)
	if err := os.WriteFile(seg, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Track security events.
	var mu sync.Mutex
	var events []string
	var eventSegment string
	var eventLine int

	w2, err := Open(dir, 50, testLogger(),
		WithIntegrity(IntegrityModeMAC, testMACKey),
		WithSecurityEvent(func(eventType string, attrs ...slog.Attr) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, eventType)
			for _, a := range attrs {
				switch a.Key {
				case "segment_file":
					eventSegment = a.Value.String()
				case "line_number":
					eventLine = int(a.Value.Int64())
				}
			}
		}),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := w2.Close(); err != nil {
			t.Logf("close w2: %v", err)
		}
	}()

	replayed := replayAll(t, w2)

	if len(replayed) != 9 {
		t.Errorf("want 9 replayed entries, got %d", len(replayed))
	}
	if w2.IntegrityFailures() != 1 {
		t.Errorf("want IntegrityFailures==1, got %d", w2.IntegrityFailures())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || events[0] != "wal_tamper_detected" {
		t.Errorf("want 1 wal_tamper_detected event, got %v", events)
	}
	if eventSegment == "" {
		t.Error("security event missing segment_file")
	}
	if eventLine != 5 { // line 5 (1-indexed)
		t.Errorf("want security event line_number=5, got %d", eventLine)
	}
}

// TestIntegrityMACCRCFailsBeforeHMAC verifies that when CRC is invalid, the
// entry is skipped at the CRC stage and HMAC is never computed. This ensures
// CRC is validated first (cheap) before HMAC (expensive).
func TestIntegrityMACCRCFailsBeforeHMAC(t *testing.T) {
	w, dir := openTestWALWithMAC(t)
	appendN(t, w, 5)
	if err := w.Close(); err != nil { t.Fatalf("close: %v", err) }

	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Corrupt line 3 (0-indexed=2): change JSON but do NOT recompute CRC.
	// This means CRC will fail, and HMAC should never be checked.
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	wl := parseWALLine(lines[2])
	if wl == nil || !wl.HasSentinels {
		t.Fatalf("expected sentinel format on line 2")
	}
	corrupted := make([]byte, len(wl.JSONBytes))
	copy(corrupted, wl.JSONBytes)
	corrupted[5] ^= 0xFF
	lines[2] = fmt.Sprintf("%s\t%s\t%s\t%s\t%s", StartSentinel, corrupted, wl.StoredCRC, wl.StoredHMAC, EndSentinel)
	if err := os.WriteFile(seg, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	securityEventCalled := false
	w2, err := Open(dir, 50, testLogger(),
		WithIntegrity(IntegrityModeMAC, testMACKey),
		WithSecurityEvent(func(eventType string, attrs ...slog.Attr) {
			securityEventCalled = true
		}),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		if err := w2.Close(); err != nil {
			t.Logf("close w2: %v", err)
		}
	}()

	replayed := replayAll(t, w2)

	if len(replayed) != 4 {
		t.Errorf("want 4 replayed entries, got %d", len(replayed))
	}
	// CRC failure should be counted, not integrity failure.
	if w2.CRCFailures() != 1 {
		t.Errorf("want CRCFailures==1, got %d", w2.CRCFailures())
	}
	if w2.IntegrityFailures() != 0 {
		t.Errorf("want IntegrityFailures==0, got %d", w2.IntegrityFailures())
	}
	if securityEventCalled {
		t.Error("security event should not be emitted for CRC-only failure")
	}
}

// ── Fail-fast: missing key ──────────────────────────────────────────────────

// TestIntegrityMACMissingKey verifies that Open fails when integrity=mac
// but no MAC key is provided. The daemon MUST refuse to start.
func TestIntegrityMACMissingKey(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir, 50, testLogger(),
		WithIntegrity(IntegrityModeMAC, nil),
	)
	if err == nil {
		t.Fatal("expected error when integrity=mac with nil key, got nil")
	}
	if !strings.Contains(err.Error(), "non-empty mac key") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestIntegrityMACEmptyKey verifies that Open fails when integrity=mac
// but the MAC key is empty.
func TestIntegrityMACEmptyKey(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir, 50, testLogger(),
		WithIntegrity(IntegrityModeMAC, []byte{}),
	)
	if err == nil {
		t.Fatal("expected error when integrity=mac with empty key, got nil")
	}
}

// ── CRC-only mode (default): no HMAC overhead ──────────────────────────────

// TestIntegrityCRC32OnlyFormat verifies that when integrity=crc32 (default),
// WAL lines are in sentinel format with JSON and CRC32 but no HMAC.
func TestIntegrityCRC32OnlyFormat(t *testing.T) {
	w, dir := openTestWAL(t)
	appendN(t, w, 5)
	if err := w.Close(); err != nil { t.Fatalf("close: %v", err) }

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
			t.Errorf("line %d: missing sentinels", i)
		}
		if wl.StoredHMAC != "" {
			t.Errorf("line %d: unexpected HMAC in CRC-only mode", i)
		}
	}
}

// ── MarkDelivered with integrity=mac ────────────────────────────────────────

// TestIntegrityMACMarkDelivered verifies that MarkDelivered recomputes BOTH
// CRC32 and HMAC over the new JSON bytes with status=DELIVERED.
func TestIntegrityMACMarkDelivered(t *testing.T) {
	w, dir := openTestWALWithMAC(t)
	entries := appendN(t, w, 5)

	// Mark entries[2] as delivered.
	if err := w.MarkDelivered(entries[2].PayloadID); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	seg := currentSegment(t, dir)
	data, err := os.ReadFile(seg)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("want 5 lines, got %d", len(lines))
	}

	// Find the DELIVERED line and verify its CRC and HMAC.
	for i, line := range lines {
		wl := parseWALLine(line)
		if wl == nil {
			t.Errorf("line %d: could not parse", i)
			continue
		}

		var entry Entry
		if err := json.Unmarshal(wl.JSONBytes, &entry); err != nil {
			t.Errorf("line %d: unmarshal: %v", i, err)
			continue
		}

		if entry.Status != StatusDelivered {
			continue
		}

		// Found the DELIVERED entry. Verify CRC and HMAC over new JSON.
		wantCRC := fmt.Sprintf("%08x", crc32.ChecksumIEEE(wl.JSONBytes))
		if wl.StoredCRC != wantCRC {
			t.Errorf("DELIVERED entry CRC mismatch: stored=%s want=%s", wl.StoredCRC, wantCRC)
		}

		wantHMAC := computeHMAC(wl.JSONBytes, testMACKey)
		if wl.StoredHMAC != wantHMAC {
			t.Errorf("DELIVERED entry HMAC mismatch: stored=%s want=%s", wl.StoredHMAC, wantHMAC)
		}

		// Verify the HMAC is valid.
		if !validateHMAC(wl.JSONBytes, testMACKey, wl.StoredHMAC) {
			t.Error("DELIVERED entry HMAC validation failed")
		}

		// Verify the entry is actually DELIVERED.
		if entry.PayloadID != entries[2].PayloadID {
			t.Errorf("wrong payload_id: want %s got %s", entries[2].PayloadID, entry.PayloadID)
		}
		return
	}
	t.Error("no DELIVERED entry found in segment")
}

// TestIntegrityMACMarkDeliveredReplayCorrect verifies the full round-trip:
// write with MAC, mark delivered, reopen, replay yields only non-delivered.
func TestIntegrityMACMarkDeliveredReplayCorrect(t *testing.T) {
	w, dir := openTestWALWithMAC(t)
	entries := appendN(t, w, 5)

	// Mark first 3 as delivered.
	for _, e := range entries[:3] {
		if err := w.MarkDelivered(e.PayloadID); err != nil {
			t.Fatalf("MarkDelivered: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	w2 := reopenWithMAC(t, dir)
	replayed := replayAll(t, w2)

	if len(replayed) != 2 {
		t.Errorf("want 2 replayed entries, got %d", len(replayed))
	}
	if w2.IntegrityFailures() != 0 {
		t.Errorf("want IntegrityFailures==0, got %d", w2.IntegrityFailures())
	}
}

// ── Backward compatibility: pre-upgrade entries ─────────────────────────────

// TestIntegrityMACPreUpgradeEntries verifies that when upgrading from crc32
// to mac mode, old 2-field entries (no HMAC) are treated as valid.
func TestIntegrityMACPreUpgradeEntries(t *testing.T) {
	// Write entries in CRC-only mode.
	dir := t.TempDir()
	w1, err := Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open crc32: %v", err)
	}
	appendN(t, w1, 5)
	if err := w1.Close(); err != nil {
		t.Logf("close w1: %v", err)
	}

	// Reopen in MAC mode. Old entries have no HMAC field — they must still be
	// replayed without error or integrity failure.
	w2 := reopenWithMAC(t, dir)
	replayed := replayAll(t, w2)

	if len(replayed) != 5 {
		t.Errorf("want 5 replayed pre-upgrade entries, got %d", len(replayed))
	}
	if w2.IntegrityFailures() != 0 {
		t.Errorf("want IntegrityFailures==0 for pre-upgrade entries, got %d", w2.IntegrityFailures())
	}
}

// ── HMAC computation unit tests ─────────────────────────────────────────────

// TestComputeAndValidateHMAC verifies the round-trip of computeHMAC and
// validateHMAC.
func TestComputeAndValidateHMAC(t *testing.T) {
	key := []byte("test-key-32-bytes-long-exactly!!")
	data := []byte(`{"hello":"world"}`)

	mac := computeHMAC(data, key)
	if len(mac) != 64 {
		t.Errorf("HMAC hex length: want 64, got %d", len(mac))
	}

	if !validateHMAC(data, key, mac) {
		t.Error("validateHMAC returned false for valid HMAC")
	}

	// Wrong data.
	if validateHMAC([]byte(`{"hello":"tampered"}`), key, mac) {
		t.Error("validateHMAC returned true for tampered data")
	}

	// Wrong key.
	if validateHMAC(data, []byte("wrong-key-32-bytes-long-exactly!"), mac) {
		t.Error("validateHMAC returned true for wrong key")
	}

	// Invalid hex.
	if validateHMAC(data, key, "not-hex") {
		t.Error("validateHMAC returned true for invalid hex")
	}
}

// ── Mixed: write MAC entries then append CRC-only entry ─────────────────────

// TestIntegrityMACMixedReplay verifies that a segment with both 3-field (MAC)
// and 2-field (CRC-only) lines replays correctly in MAC mode.
func TestIntegrityMACMixedReplay(t *testing.T) {
	w, dir := openTestWALWithMAC(t)
	appendN(t, w, 3) // 3 MAC entries
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Manually append a 2-field (CRC-only) line to simulate pre-upgrade entry.
	seg := currentSegment(t, dir)
	entry := Entry{
		Version:        walVersion,
		PayloadID:      "legacy-1",
		IdempotencyKey: "legacy-idem-1",
		Status:         StatusPending,
		Source:         "src",
		Destination:    "dst",
		Subject:        "sub",
		Payload:        json.RawMessage(`{"legacy":true}`),
	}
	data, _ := json.Marshal(entry)
	crcHex := fmt.Sprintf("%08x", crc32.ChecksumIEEE(data))
	writeRawLine(t, seg, fmt.Sprintf("%s\t%s", data, crcHex))

	w2 := reopenWithMAC(t, dir)
	replayed := replayAll(t, w2)

	// 3 MAC entries + 1 legacy entry = 4 total.
	if len(replayed) != 4 {
		t.Errorf("want 4 replayed entries, got %d", len(replayed))
	}
	if w2.IntegrityFailures() != 0 {
		t.Errorf("want IntegrityFailures==0, got %d", w2.IntegrityFailures())
	}
}

// ── MarkPermanentFailure with MAC ───────────────────────────────────────────

// TestIntegrityMACMarkPermanentFailure verifies MarkPermanentFailure also
// recomputes HMAC correctly.
func TestIntegrityMACMarkPermanentFailure(t *testing.T) {
	w, dir := openTestWALWithMAC(t)
	entries := appendN(t, w, 3)

	if err := w.MarkPermanentFailure(entries[1].PayloadID); err != nil {
		t.Fatalf("MarkPermanentFailure: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

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
		if wl.StoredHMAC == "" {
			t.Errorf("line %d: missing HMAC", i)
			continue
		}
		if !validateHMAC(wl.JSONBytes, testMACKey, wl.StoredHMAC) {
			t.Errorf("HMAC validation failed for line %d", i)
		}
	}
}

// ── Segment rotation with MAC ───────────────────────────────────────────────

// TestIntegrityMACSegmentRotation verifies that after segment rotation,
// both segments have valid HMAC entries.
func TestIntegrityMACSegmentRotation(t *testing.T) {
	dir := t.TempDir()
	// Use tiny max size to force rotation.
	w, err := Open(dir, 1, testLogger(), // 1 MB max
		WithIntegrity(IntegrityModeMAC, testMACKey),
	)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Write enough entries to trigger rotation (large payloads).
	bigPayload := json.RawMessage(fmt.Sprintf(`{"data":%q}`, strings.Repeat("X", 200000)))
	for i := 0; i < 10; i++ {
		entry := Entry{
			PayloadID:      fmt.Sprintf("big-%d", i),
			IdempotencyKey: fmt.Sprintf("big-idem-%d", i),
			Source:         "src",
			Destination:    "dst",
			Subject:        "sub",
			Payload:        bigPayload,
		}
		if err := w.Append(entry); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Verify all segments have valid 3-field lines.
	segs, _ := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	if len(segs) < 2 {
		t.Skipf("rotation did not occur (only %d segments); test not meaningful", len(segs))
	}

	for _, seg := range segs {
		data, err := os.ReadFile(seg)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", seg, err)
		}
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if line == "" {
				continue
			}
			wl := parseWALLine(line)
			if wl == nil {
				t.Errorf("segment %s: could not parse line", seg)
				continue
			}
			if !wl.HasSentinels {
				t.Errorf("segment %s: missing sentinels", seg)
			}
			if wl.StoredHMAC == "" {
				t.Errorf("segment %s: missing HMAC in MAC mode", seg)
			}
		}
	}

	// Replay must return all 10 entries.
	w2 := reopenWithMAC(t, dir)
	replayed := replayAll(t, w2)
	if len(replayed) != 10 {
		t.Errorf("want 10 replayed entries, got %d", len(replayed))
	}
}
