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
	"strings"
	"testing"
	"time"
)

// benchEntry returns a WAL entry with a payload of approximately targetBytes.
func benchEntry(id string, targetBytes int) Entry {
	content := strings.Repeat("x", targetBytes)
	payload, _ := json.Marshal(map[string]string{
		"payload_id": id,
		"content":    content,
	})
	return Entry{
		PayloadID:      id,
		IdempotencyKey: "idem-" + id,
		Status:         StatusPending,
		Timestamp:      time.Now().UTC(),
		Source:         "bench",
		Destination:    "sqlite",
		Subject:        "user:bench",
		Payload:        payload,
	}
}

// BenchmarkWAL_Append_SmallEntry measures the cost of appending a single ~256
// byte entry. fsync is always on (not configurable) — this is the real number.
func BenchmarkWAL_Append_SmallEntry(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { w.Close() })

	entry := benchEntry("small", 200) // ~256 bytes total with metadata

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.PayloadID = fmt.Sprintf("small-%d", i)
		entry.IdempotencyKey = fmt.Sprintf("idem-small-%d", i)
		if err := w.Append(entry); err != nil {
			b.Fatalf("append: %v", err)
		}
	}
}

// BenchmarkWAL_Append_LargeEntry measures the cost of appending a single ~4 KB entry.
func BenchmarkWAL_Append_LargeEntry(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { w.Close() })

	entry := benchEntry("large", 4000) // ~4 KB total

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.PayloadID = fmt.Sprintf("large-%d", i)
		entry.IdempotencyKey = fmt.Sprintf("idem-large-%d", i)
		if err := w.Append(entry); err != nil {
			b.Fatalf("append: %v", err)
		}
	}
}

// BenchmarkWAL_Append_Batch100 measures 100 sequential appends per iteration.
// No batch API exists; this loops 100 single appends inside one b.N iteration.
func BenchmarkWAL_Append_Batch100(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { w.Close() })

	entry := benchEntry("batch", 200)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			entry.PayloadID = fmt.Sprintf("batch-%d-%d", i, j)
			entry.IdempotencyKey = fmt.Sprintf("idem-batch-%d-%d", i, j)
			if err := w.Append(entry); err != nil {
				b.Fatalf("append: %v", err)
			}
		}
	}
}

// BenchmarkWAL_Replay_1000Entries pre-populates a WAL with 1000 entries, then
// measures replay time per iteration. Each iteration reopens from disk and
// replays the full segment.
func BenchmarkWAL_Replay_1000Entries(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()

	// Setup: write 1000 entries.
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	entry := benchEntry("replay", 200)
	for i := 0; i < 1000; i++ {
		entry.PayloadID = fmt.Sprintf("replay-%d", i)
		entry.IdempotencyKey = fmt.Sprintf("idem-replay-%d", i)
		if err := w.Append(entry); err != nil {
			b.Fatalf("append setup: %v", err)
		}
	}
	w.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w2, err := Open(dir, 50, testLogger())
		if err != nil {
			b.Fatalf("reopen: %v", err)
		}
		count := 0
		if err := w2.Replay(func(_ Entry) { count++ }); err != nil {
			b.Fatalf("replay: %v", err)
		}
		w2.Close()
		if count != 1000 {
			b.Fatalf("expected 1000 entries, got %d", count)
		}
	}
}

// BenchmarkWAL_MarkStatus measures the cost of MarkDelivered through the public API.
func BenchmarkWAL_MarkStatus(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	w, err := Open(dir, 50, testLogger())
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	b.Cleanup(func() { w.Close() })

	// Pre-populate entries to mark.
	entry := benchEntry("mark", 200)
	for i := 0; i < b.N; i++ {
		entry.PayloadID = fmt.Sprintf("mark-%d", i)
		entry.IdempotencyKey = fmt.Sprintf("idem-mark-%d", i)
		if err := w.Append(entry); err != nil {
			b.Fatalf("append: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := w.MarkDelivered(fmt.Sprintf("mark-%d", i)); err != nil {
			b.Fatalf("mark: %v", err)
		}
	}
}
