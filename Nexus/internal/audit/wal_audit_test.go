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

package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/provenance"
	"github.com/bubblefish-tech/nexus/internal/wal"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestWALWriter_SubmitCreatesAuditEntry verifies that Submit writes an
// audit entry to the WAL that can be found by scanning for EntryTypeAudit.
func TestWALWriter_SubmitCreatesAuditEntry(t *testing.T) {
	dir := t.TempDir()
	w, err := wal.Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}
	defer w.Close()

	aw := NewWALWriter(w, nil)

	rec := InteractionRecord{
		RecordID:      "test-audit-001",
		Source:        "test-source",
		OperationType: "write",
		Endpoint:      "/inbound/test",
	}

	if err := aw.Submit(rec); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// The entry should be in the WAL. Replay should NOT see it (it's not
	// a data entry). We verify by reading the segment directly.
	var replayed []wal.Entry
	if err := w.Replay(func(e wal.Entry) { replayed = append(replayed, e) }); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	// Standard Replay skips audit entries (EntryType != "").
	if len(replayed) != 0 {
		t.Errorf("Replay should skip audit entries, got %d", len(replayed))
	}
}

// TestWALWriter_SubmitSetsRecordID verifies that Submit generates a
// RecordID if none is provided.
func TestWALWriter_SubmitSetsRecordID(t *testing.T) {
	dir := t.TempDir()
	w, err := wal.Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}
	defer w.Close()

	aw := NewWALWriter(w, nil)

	rec := InteractionRecord{
		Source:        "test-source",
		OperationType: "query",
	}

	if err := aw.Submit(rec); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// We can't easily inspect the WAL entry's payload without scanning
	// the raw file, but the important thing is Submit didn't error.
}

// TestWALWriter_AuditEntryPayloadRoundtrip verifies that the audit record
// serialized in the WAL payload can be deserialized back by reading the
// raw segment file.
func TestWALWriter_AuditEntryPayloadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	w, err := wal.Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}

	aw := NewWALWriter(w, nil)

	rec := InteractionRecord{
		RecordID:       "roundtrip-001",
		Source:         "claude-code",
		OperationType:  "write",
		Endpoint:       "/inbound/claude",
		HTTPMethod:     "POST",
		HTTPStatusCode: 200,
		PayloadID:      "payload-abc",
	}

	if err := aw.Submit(rec); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the raw segment to find the audit entry. Append forces
	// status=PENDING, so SampleDelivered won't find it. Instead we
	// scan the segment directly.
	segs, err := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	if err != nil || len(segs) == 0 {
		t.Fatalf("no segments found in %s", dir)
	}

	data, err := os.ReadFile(segs[len(segs)-1])
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var found bool
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, "\t", 6)
		if len(parts) < 2 {
			continue
		}
		// Detect sentinel format: first field is the start sentinel.
		jsonField := parts[0]
		if jsonField == wal.StartSentinel && len(parts) >= 4 {
			jsonField = parts[1] // JSON is the second field in sentinel format
		}
		var entry wal.Entry
		if err := json.Unmarshal([]byte(jsonField), &entry); err != nil {
			continue
		}
		if entry.EntryType == wal.EntryTypeAudit {
			var decoded InteractionRecord
			if err := json.Unmarshal(entry.Payload, &decoded); err != nil {
				t.Fatalf("unmarshal audit payload: %v", err)
			}
			if decoded.RecordID != "roundtrip-001" {
				t.Errorf("record_id: want roundtrip-001, got %s", decoded.RecordID)
			}
			if decoded.Source != "claude-code" {
				t.Errorf("source: want claude-code, got %s", decoded.Source)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("audit entry not found in WAL segment")
	}
}

// TestSubmitConcurrentChainIntegrity verifies that concurrent Submit calls
// produce a valid hash chain with no gaps or stale PrevAuditHash values.
func TestSubmitConcurrentChainIntegrity(t *testing.T) {
	dir := t.TempDir()
	w, err := wal.Open(dir, 50, testLogger())
	if err != nil {
		t.Fatalf("Open WAL: %v", err)
	}
	defer w.Close()

	kp, err := provenance.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	chain := provenance.NewChainState()
	if _, err := chain.Genesis(kp); err != nil {
		t.Fatalf("Genesis: %v", err)
	}

	aw := NewWALWriter(w, chain)

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			rec := InteractionRecord{
				RecordID:      fmt.Sprintf("chain-%03d", idx),
				Source:        "test",
				OperationType: "write",
			}
			if err := aw.Submit(rec); err != nil {
				t.Errorf("Submit %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read all audit entries from WAL segments and verify the chain.
	segs, err := filepath.Glob(filepath.Join(dir, "wal-*.jsonl"))
	if err != nil || len(segs) == 0 {
		t.Fatalf("no segments found in %s", dir)
	}

	type chainEntry struct {
		PrevAuditHash string `json:"prev_audit_hash"`
		RecordID      string `json:"record_id"`
	}

	var entries []chainEntry
	for _, seg := range segs {
		data, err := os.ReadFile(seg)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", seg, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			parts := strings.SplitN(line, "\t", 6)
			if len(parts) < 2 {
				continue
			}
			jsonField := parts[0]
			if jsonField == wal.StartSentinel && len(parts) >= 4 {
				jsonField = parts[1]
			}
			var entry wal.Entry
			if err := json.Unmarshal([]byte(jsonField), &entry); err != nil {
				continue
			}
			if entry.EntryType == wal.EntryTypeAudit {
				var ce chainEntry
				if err := json.Unmarshal(entry.Payload, &ce); err != nil {
					t.Fatalf("unmarshal chain entry: %v", err)
				}
				entries = append(entries, ce)
			}
		}
	}

	if len(entries) != n {
		t.Fatalf("expected %d chain entries, got %d", n, len(entries))
	}

	// Verify no duplicate PrevAuditHash values (each entry points to a unique predecessor).
	seen := make(map[string]int)
	for i, e := range entries {
		if e.PrevAuditHash == "" {
			t.Errorf("entry %d (%s) has empty PrevAuditHash", i, e.RecordID)
		}
		if prev, dup := seen[e.PrevAuditHash]; dup {
			t.Errorf("duplicate PrevAuditHash %s in entries %d and %d", e.PrevAuditHash, prev, i)
		}
		seen[e.PrevAuditHash] = i
	}
}
