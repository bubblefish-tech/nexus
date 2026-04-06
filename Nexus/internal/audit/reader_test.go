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
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeRawRecord writes a single raw JSONL+CRC32 line to a file, returning bytes written.
func writeRawRecord(t *testing.T, f *os.File, rec InteractionRecord) {
	t.Helper()
	rec.CRC32 = ""
	jsonBytes, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	checksum := crc32.ChecksumIEEE(jsonBytes)
	line := fmt.Sprintf("%s\t%08x\n", jsonBytes, checksum)
	if _, err := f.WriteString(line); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func makeRecord(source, opType, policyDecision string, ts time.Time) InteractionRecord {
	return InteractionRecord{
		RecordID:       NewRecordID(),
		RequestID:      NewRecordID(),
		Timestamp:      ts,
		Source:         source,
		ActorType:      "agent",
		ActorID:        "actor-" + source,
		OperationType:  opType,
		PolicyDecision: policyDecision,
	}
}

func TestAuditReader_BasicQuery(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	for i := range 10 {
		rec := makeRecord("claude", "write", "allowed", now.Add(time.Duration(i)*time.Second))
		writeRawRecord(t, f, rec)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 10 {
		t.Errorf("expected 10 total, got %d", result.TotalMatching)
	}
	if len(result.Records) != 10 {
		t.Errorf("expected 10 records, got %d", len(result.Records))
	}
}

func TestAuditReader_FilterBySource(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now))
	writeRawRecord(t, f, makeRecord("cursor", "write", "allowed", now.Add(time.Second)))
	writeRawRecord(t, f, makeRecord("claude", "query", "allowed", now.Add(2*time.Second)))
	writeRawRecord(t, f, makeRecord("cursor", "query", "denied", now.Add(3*time.Second)))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))

	result, err := reader.Query(AuditFilter{Source: "claude"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 2 {
		t.Errorf("expected 2 claude records, got %d", result.TotalMatching)
	}
	for _, rec := range result.Records {
		if rec.Source != "claude" {
			t.Errorf("expected source=claude, got %s", rec.Source)
		}
	}
}

func TestAuditReader_FilterByOperation(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now))
	writeRawRecord(t, f, makeRecord("claude", "query", "allowed", now.Add(time.Second)))
	writeRawRecord(t, f, makeRecord("claude", "admin", "allowed", now.Add(2*time.Second)))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{Operation: "query"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 1 {
		t.Errorf("expected 1 query record, got %d", result.TotalMatching)
	}
}

func TestAuditReader_FilterByPolicyDecision(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now))
	writeRawRecord(t, f, makeRecord("claude", "write", "denied", now.Add(time.Second)))
	writeRawRecord(t, f, makeRecord("claude", "query", "filtered", now.Add(2*time.Second)))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{PolicyDecision: "denied"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 1 {
		t.Errorf("expected 1 denied record, got %d", result.TotalMatching)
	}
}

func TestAuditReader_FilterByTimeRange(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", base))
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", base.Add(1*time.Hour)))
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", base.Add(2*time.Hour)))
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", base.Add(3*time.Hour)))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))

	// After base+30min, Before base+2h30min → should get 2 records (1h, 2h).
	result, err := reader.Query(AuditFilter{
		After:  base.Add(30 * time.Minute),
		Before: base.Add(2*time.Hour + 30*time.Minute),
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 2 {
		t.Errorf("expected 2 records in time range, got %d", result.TotalMatching)
	}
}

func TestAuditReader_Pagination(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	for i := range 25 {
		rec := makeRecord("test", "write", "allowed", now.Add(time.Duration(i)*time.Second))
		writeRawRecord(t, f, rec)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))

	// Page 1: offset=0, limit=10
	r1, err := reader.Query(AuditFilter{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("Query p1: %v", err)
	}
	if len(r1.Records) != 10 {
		t.Errorf("page 1: expected 10, got %d", len(r1.Records))
	}
	if !r1.HasMore {
		t.Error("page 1: expected has_more=true")
	}
	if r1.TotalMatching != 25 {
		t.Errorf("page 1: expected total=25, got %d", r1.TotalMatching)
	}

	// Page 2: offset=10, limit=10
	r2, err := reader.Query(AuditFilter{Limit: 10, Offset: 10})
	if err != nil {
		t.Fatalf("Query p2: %v", err)
	}
	if len(r2.Records) != 10 {
		t.Errorf("page 2: expected 10, got %d", len(r2.Records))
	}
	if !r2.HasMore {
		t.Error("page 2: expected has_more=true")
	}

	// Page 3: offset=20, limit=10
	r3, err := reader.Query(AuditFilter{Limit: 10, Offset: 20})
	if err != nil {
		t.Fatalf("Query p3: %v", err)
	}
	if len(r3.Records) != 5 {
		t.Errorf("page 3: expected 5, got %d", len(r3.Records))
	}
	if r3.HasMore {
		t.Error("page 3: expected has_more=false")
	}

	// Beyond range: offset=30
	r4, err := reader.Query(AuditFilter{Limit: 10, Offset: 30})
	if err != nil {
		t.Fatalf("Query p4: %v", err)
	}
	if len(r4.Records) != 0 {
		t.Errorf("page 4: expected 0, got %d", len(r4.Records))
	}
}

func TestAuditReader_LimitCappedAt1000(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	writeRawRecord(t, f, makeRecord("test", "write", "allowed", now))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{Limit: 5000})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.Limit != 1000 {
		t.Errorf("expected limit capped at 1000, got %d", result.Limit)
	}
}

func TestAuditReader_DefaultLimit100(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	writeRawRecord(t, f, makeRecord("test", "write", "allowed", time.Now().UTC()))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.Limit != 100 {
		t.Errorf("expected default limit 100, got %d", result.Limit)
	}
}

func TestAuditReader_CRC32Mismatch_SkipsCorruptEntry(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()

	// Write 3 valid records.
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now))
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now.Add(time.Second)))
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now.Add(2*time.Second)))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Corrupt the second line (flip a byte in the JSON).
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Find the second newline and corrupt a byte before it.
	lines := 0
	for i, b := range data {
		if b == '\n' {
			lines++
			if lines == 2 {
				// Corrupt a byte in the second line's JSON.
				data[i-20] ^= 0xFF
				break
			}
		}
	}
	if err := os.WriteFile(logFile, data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	// Should get 2 records (corrupt one skipped).
	if result.TotalMatching != 2 {
		t.Errorf("expected 2 valid records (1 corrupt skipped), got %d", result.TotalMatching)
	}
}

func TestAuditReader_MultipleRotatedFiles(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	now := time.Now().UTC()

	// Create two rotated files and one current file.
	rotated1 := filepath.Join(dir, "interactions-20260401-120000.jsonl")
	rotated2 := filepath.Join(dir, "interactions-20260401-130000.jsonl")

	// Rotated file 1 (oldest).
	f1, err := os.OpenFile(rotated1, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create rotated1: %v", err)
	}
	writeRawRecord(t, f1, makeRecord("source-a", "write", "allowed", now))
	writeRawRecord(t, f1, makeRecord("source-a", "write", "allowed", now.Add(time.Second)))
	if err := f1.Close(); err != nil {
		t.Logf("close rotated1: %v", err)
	}

	// Rotated file 2.
	f2, err := os.OpenFile(rotated2, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create rotated2: %v", err)
	}
	writeRawRecord(t, f2, makeRecord("source-b", "query", "allowed", now.Add(2*time.Second)))
	if err := f2.Close(); err != nil {
		t.Logf("close rotated2: %v", err)
	}

	// Current file.
	f3, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create current: %v", err)
	}
	writeRawRecord(t, f3, makeRecord("source-c", "admin", "denied", now.Add(3*time.Second)))
	writeRawRecord(t, f3, makeRecord("source-c", "write", "allowed", now.Add(4*time.Second)))
	if err := f3.Close(); err != nil {
		t.Logf("close current: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))

	// Query all — should see records from all 3 files.
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 5 {
		t.Errorf("expected 5 total records across 3 files, got %d", result.TotalMatching)
	}

	// Verify chronological order (rotated files oldest-first, then current).
	if result.Records[0].Source != "source-a" {
		t.Errorf("first record should be from source-a (oldest), got %s", result.Records[0].Source)
	}
	if result.Records[4].Source != "source-c" {
		t.Errorf("last record should be from source-c (newest), got %s", result.Records[4].Source)
	}
}

func TestAuditReader_Count(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now))
	writeRawRecord(t, f, makeRecord("cursor", "write", "denied", now.Add(time.Second)))
	writeRawRecord(t, f, makeRecord("claude", "query", "allowed", now.Add(2*time.Second)))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))

	count, err := reader.Count(AuditFilter{Source: "claude"})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 claude records, got %d", count)
	}

	count, err = reader.Count(AuditFilter{PolicyDecision: "denied"})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 denied record, got %d", count)
	}
}

func TestAuditReader_FilterByActorID(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	rec1 := makeRecord("claude", "write", "allowed", now)
	rec1.ActorID = "user-alice"
	rec2 := makeRecord("claude", "write", "allowed", now.Add(time.Second))
	rec2.ActorID = "user-bob"
	rec3 := makeRecord("claude", "query", "allowed", now.Add(2*time.Second))
	rec3.ActorID = "user-alice"
	writeRawRecord(t, f, rec1)
	writeRawRecord(t, f, rec2)
	writeRawRecord(t, f, rec3)
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{ActorID: "user-alice"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 2 {
		t.Errorf("expected 2 alice records, got %d", result.TotalMatching)
	}
}

func TestAuditReader_FilterBySubjectAndDestination(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	rec1 := makeRecord("claude", "write", "allowed", now)
	rec1.Subject = "project-alpha"
	rec1.Destination = "sqlite"
	rec2 := makeRecord("claude", "write", "allowed", now.Add(time.Second))
	rec2.Subject = "project-beta"
	rec2.Destination = "postgres"
	writeRawRecord(t, f, rec1)
	writeRawRecord(t, f, rec2)
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))

	result, err := reader.Query(AuditFilter{Subject: "project-alpha"})
	if err != nil {
		t.Fatalf("Query subject: %v", err)
	}
	if result.TotalMatching != 1 {
		t.Errorf("expected 1 alpha record, got %d", result.TotalMatching)
	}

	result, err = reader.Query(AuditFilter{Destination: "postgres"})
	if err != nil {
		t.Fatalf("Query dest: %v", err)
	}
	if result.TotalMatching != 1 {
		t.Errorf("expected 1 postgres record, got %d", result.TotalMatching)
	}
}

func TestAuditReader_HMACValidation(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")
	macKey := []byte("test-hmac-key-32-bytes-long!!!!!")

	// Write records with HMAC using the logger.
	al, err := NewAuditLogger(logFile,
		WithIntegrityMode("mac", macKey),
		WithDualWrite(false),
	)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	now := time.Now().UTC()
	for i := range 5 {
		rec := InteractionRecord{
			RecordID:       NewRecordID(),
			Timestamp:      now.Add(time.Duration(i) * time.Second),
			Source:         "test",
			OperationType:  "write",
			PolicyDecision: "allowed",
		}
		if err := al.Log(rec); err != nil {
			t.Fatalf("Log[%d]: %v", i, err)
		}
	}
	if err := al.Close(); err != nil {
		t.Logf("close: %v", err)
	}

	// Read with correct key.
	reader := NewAuditReader(logFile,
		WithReaderIntegrity("mac", macKey),
		WithReaderDualWrite(false),
	)
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 5 {
		t.Errorf("expected 5 records with valid HMAC, got %d", result.TotalMatching)
	}

	// Now corrupt an HMAC — tamper with the file.
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Find and corrupt the HMAC of line 3 (flip a hex char).
	lines := 0
	for i, b := range data {
		if b == '\n' {
			lines++
			if lines == 3 {
				// The HMAC is the last field before newline. Flip a byte.
				data[i-1] ^= 0x01
				break
			}
		}
	}
	if err := os.WriteFile(logFile, data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	result, err = reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query after tamper: %v", err)
	}
	if result.TotalMatching != 4 {
		t.Errorf("expected 4 records (1 tampered skipped), got %d", result.TotalMatching)
	}
}

func TestAuditReader_EmptyLogFile(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	// Create empty file.
	f, err := os.Create(logFile)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 0 {
		t.Errorf("expected 0 records, got %d", result.TotalMatching)
	}
	if len(result.Records) != 0 {
		t.Errorf("expected empty records slice, got %d", len(result.Records))
	}
}

func TestAuditReader_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	// Do NOT create the file.
	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query on missing file should not error: %v", err)
	}
	if result.TotalMatching != 0 {
		t.Errorf("expected 0 records, got %d", result.TotalMatching)
	}
}

func TestAuditReader_CombinedFilters(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now))
	writeRawRecord(t, f, makeRecord("claude", "query", "denied", now.Add(time.Second)))
	writeRawRecord(t, f, makeRecord("cursor", "write", "allowed", now.Add(2*time.Second)))
	writeRawRecord(t, f, makeRecord("claude", "write", "denied", now.Add(3*time.Second)))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))

	// Source=claude AND operation=write AND decision=denied
	result, err := reader.Query(AuditFilter{
		Source:         "claude",
		Operation:      "write",
		PolicyDecision: "denied",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 1 {
		t.Errorf("expected 1 combined-filter match, got %d", result.TotalMatching)
	}
}

// ── Hardening Tests (Update U1.3–U1.5) ─────────────────────────────────────

func TestAuditReader_ShadowFallback_CorruptPrimary(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")
	shadowFile := filepath.Join(dir, "interactions-shadow.jsonl")

	// Write 5 records via logger (dual-write).
	al, err := NewAuditLogger(logFile, WithDualWrite(true))
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	now := time.Now().UTC()
	for i := range 5 {
		rec := InteractionRecord{
			RecordID:       NewRecordID(),
			Timestamp:      now.Add(time.Duration(i) * time.Second),
			Source:         "test",
			OperationType:  "write",
			PolicyDecision: "allowed",
		}
		if err := al.Log(rec); err != nil {
			t.Fatalf("Log[%d]: %v", i, err)
		}
	}
	if err := al.Close(); err != nil {
		t.Logf("close: %v", err)
	}

	// Corrupt one record in primary (flip a byte in line 3).
	data, _ := os.ReadFile(logFile)
	lines := 0
	for i, b := range data {
		if b == '\n' {
			lines++
			if lines == 3 {
				data[i-20] ^= 0xFF
				break
			}
		}
	}
	if err := os.WriteFile(logFile, data, 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Shadow should still be intact.
	reader := NewAuditReader(logFile, WithReaderDualWrite(true))
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 5 {
		t.Errorf("expected 5 records (1 recovered from shadow), got %d", result.TotalMatching)
	}
	if reader.ShadowRecoveries() != 1 {
		t.Errorf("expected 1 shadow recovery, got %d", reader.ShadowRecoveries())
	}

	// Verify shadow file is intact.
	_, err = os.Stat(shadowFile)
	if os.IsNotExist(err) {
		t.Fatal("shadow file should exist")
	}
}

func TestAuditReader_BothCorrupt_SkipsEntry(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")
	shadowFile := filepath.Join(dir, "interactions-shadow.jsonl")

	al, err := NewAuditLogger(logFile, WithDualWrite(true))
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	now := time.Now().UTC()
	for i := range 5 {
		rec := InteractionRecord{
			RecordID:       NewRecordID(),
			Timestamp:      now.Add(time.Duration(i) * time.Second),
			Source:         "test",
			OperationType:  "write",
			PolicyDecision: "allowed",
		}
		if err := al.Log(rec); err != nil {
			t.Fatalf("Log[%d]: %v", i, err)
		}
	}
	if err := al.Close(); err != nil {
		t.Logf("close: %v", err)
	}

	// Corrupt the same record (line 3) in BOTH primary and shadow.
	for _, path := range []string{logFile, shadowFile} {
		data, _ := os.ReadFile(path)
		lines := 0
		for i, b := range data {
			if b == '\n' {
				lines++
				if lines == 3 {
					data[i-20] ^= 0xFF
					break
				}
			}
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatalf("WriteFile %s: %v", path, err)
		}
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(true))
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 4 {
		t.Errorf("expected 4 records (1 skipped, both corrupt), got %d", result.TotalMatching)
	}
	if reader.CRCFailures() != 1 {
		t.Errorf("expected 1 CRC failure, got %d", reader.CRCFailures())
	}
}

func TestAuditReader_RotationMarkerSkipped(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	// Write records and a rotation_marker manually.
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now := time.Now().UTC()
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now))
	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now.Add(time.Second)))

	// Write a rotation_marker.
	marker := InteractionRecord{
		RecordID:       NewRecordID(),
		Timestamp:      now.Add(2 * time.Second),
		OperationType:  "rotation_marker",
		PolicyDecision: "allowed",
	}
	writeRawRecord(t, f, marker)

	writeRawRecord(t, f, makeRecord("claude", "write", "allowed", now.Add(3*time.Second)))
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	// rotation_marker should be skipped.
	if result.TotalMatching != 3 {
		t.Errorf("expected 3 records (rotation_marker skipped), got %d", result.TotalMatching)
	}
	for _, rec := range result.Records {
		if rec.OperationType == "rotation_marker" {
			t.Error("rotation_marker should not appear in query results")
		}
	}
}

func TestAuditReader_CrashMidRotation_DedupByRecordID(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	now := time.Now().UTC()

	// Simulate crash mid-rotation: same records in both old segment and new current.
	rotated := filepath.Join(dir, "interactions-20260401-120000.jsonl")

	// Shared record IDs to test dedup.
	sharedIDs := []string{NewRecordID(), NewRecordID(), NewRecordID()}

	// Write to "old" segment (pre-rotation).
	f1, _ := os.OpenFile(rotated, os.O_CREATE|os.O_WRONLY, 0600)
	for i, id := range sharedIDs {
		rec := InteractionRecord{
			RecordID:       id,
			Timestamp:      now.Add(time.Duration(i) * time.Second),
			Source:         "test",
			OperationType:  "write",
			PolicyDecision: "allowed",
		}
		writeRawRecord(t, f1, rec)
	}
	if err := f1.Close(); err != nil {
		t.Logf("close rotated: %v", err)
	}

	// Write the SAME records to "new" current file (crash replay scenario).
	f2, _ := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY, 0600)
	for i, id := range sharedIDs {
		rec := InteractionRecord{
			RecordID:       id,
			Timestamp:      now.Add(time.Duration(i) * time.Second),
			Source:         "test",
			OperationType:  "write",
			PolicyDecision: "allowed",
		}
		writeRawRecord(t, f2, rec)
	}
	// Also add one unique record in current file.
	unique := InteractionRecord{
		RecordID:       NewRecordID(),
		Timestamp:      now.Add(10 * time.Second),
		Source:         "test",
		OperationType:  "write",
		PolicyDecision: "allowed",
	}
	writeRawRecord(t, f2, unique)
	if err := f2.Close(); err != nil {
		t.Logf("close current: %v", err)
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	// 3 shared (deduped) + 1 unique = 4.
	if result.TotalMatching != 4 {
		t.Errorf("expected 4 records (3 deduped + 1 unique), got %d", result.TotalMatching)
	}
}

func TestAuditReader_EncryptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")
	encKey := make([]byte, 32)
	for i := range encKey {
		encKey[i] = byte(i + 50)
	}

	al, err := NewAuditLogger(logFile,
		WithEncryption(encKey),
		WithDualWrite(false),
	)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	now := time.Now().UTC()
	for i := range 10 {
		rec := InteractionRecord{
			RecordID:       NewRecordID(),
			Timestamp:      now.Add(time.Duration(i) * time.Second),
			Source:         fmt.Sprintf("src-%d", i),
			OperationType:  "write",
			PolicyDecision: "allowed",
		}
		if err := al.Log(rec); err != nil {
			t.Fatalf("Log[%d]: %v", i, err)
		}
	}
	if err := al.Close(); err != nil {
		t.Logf("close: %v", err)
	}

	reader := NewAuditReader(logFile,
		WithReaderEncryption(encKey),
		WithReaderDualWrite(false),
	)
	result, err := reader.Query(AuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result.TotalMatching != 10 {
		t.Errorf("expected 10 records, got %d", result.TotalMatching)
	}

	// Verify CRC is valid on encrypted data by checking no CRC failures.
	if reader.CRCFailures() != 0 {
		t.Errorf("expected 0 CRC failures, got %d", reader.CRCFailures())
	}
}

func TestAuditReader_ShadowExcludedFromPrimaryDiscovery(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	// Create primary, shadow, and rotated shadow files.
	for _, name := range []string{
		"interactions.jsonl",
		"interactions-shadow.jsonl",
		"interactions-20260401-120000.jsonl",
		"interactions-shadow-20260401-120000.jsonl",
	} {
		f, _ := os.Create(filepath.Join(dir, name))
		now := time.Now().UTC()
		rec := makeRecord("test", "write", "allowed", now)
		writeRawRecord(t, f, rec)
		if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	}

	reader := NewAuditReader(logFile, WithReaderDualWrite(false))
	files, err := reader.discoverPrimaryFiles()
	if err != nil {
		t.Fatalf("discoverPrimaryFiles: %v", err)
	}

	// Should only contain primary files, not shadow.
	for _, f := range files {
		base := filepath.Base(f)
		if strings.Contains(base, "shadow") {
			t.Errorf("shadow file should not appear in primary discovery: %s", base)
		}
	}
	if len(files) != 2 {
		t.Errorf("expected 2 primary files, got %d", len(files))
	}
}
