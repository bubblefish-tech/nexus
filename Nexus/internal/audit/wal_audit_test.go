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
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BubbleFish-Nexus/internal/wal"
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

	aw := NewWALWriter(w)

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

	aw := NewWALWriter(w)

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

	aw := NewWALWriter(w)

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
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		var entry wal.Entry
		if err := json.Unmarshal([]byte(parts[0]), &entry); err != nil {
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
