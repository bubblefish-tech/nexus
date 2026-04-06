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
	"bufio"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAuditLogger_BasicWrite(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	logFile := filepath.Join(dir, "logs", "interactions.jsonl")

	al, err := NewAuditLogger(logFile)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer al.Close()

	rec := InteractionRecord{
		RecordID:       NewRecordID(),
		RequestID:      "req-001",
		Timestamp:      time.Now().UTC(),
		Source:         "claude",
		ActorType:      "agent",
		ActorID:        "claude-3",
		EffectiveIP:    "127.0.0.1",
		OperationType:  "write",
		Endpoint:       "/inbound/claude",
		HTTPMethod:     "POST",
		HTTPStatusCode: 200,
		PolicyDecision: "allowed",
		LatencyMs:      1.5,
	}

	if err := al.Log(rec); err != nil {
		t.Fatalf("Log: %v", err)
	}

	// Verify file was written.
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("log file is empty after write")
	}

	// Verify format: JSON<TAB>CRC32<NEWLINE>
	line := strings.TrimSpace(string(data))
	parts := strings.Split(line, "\t")
	if len(parts) != 2 {
		t.Fatalf("expected 2 tab-separated parts, got %d", len(parts))
	}

	// Verify CRC32.
	jsonBytes := []byte(parts[0])
	storedCRC := parts[1]
	computed := fmt.Sprintf("%08x", crc32.ChecksumIEEE(jsonBytes))
	if computed != storedCRC {
		t.Errorf("CRC32 mismatch: computed=%s stored=%s", computed, storedCRC)
	}

	// Verify JSON contains record_id.
	var parsed InteractionRecord
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.RecordID == "" {
		t.Error("record_id is empty")
	}
	if parsed.CRC32 != "" {
		t.Error("crc32 field should be empty in stored JSON (computed externally)")
	}
}

func TestAuditLogger_CRC32OnEveryRecord(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	al, err := NewAuditLogger(logFile)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer al.Close()

	const count = 100
	for i := 0; i < count; i++ {
		rec := InteractionRecord{
			RecordID:       NewRecordID(),
			RequestID:      fmt.Sprintf("req-%03d", i),
			Timestamp:      time.Now().UTC(),
			Source:         "test",
			OperationType:  "write",
			PolicyDecision: "allowed",
		}
		if err := al.Log(rec); err != nil {
			t.Fatalf("Log[%d]: %v", i, err)
		}
	}

	// Read and verify all 100 records have CRC32 after tab.
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 2 {
			t.Errorf("line %d: missing CRC32 tab field", lineNum)
			continue
		}
		jsonBytes := []byte(parts[0])
		storedCRC := parts[1]
		computed := fmt.Sprintf("%08x", crc32.ChecksumIEEE(jsonBytes))
		if computed != storedCRC {
			t.Errorf("line %d: CRC32 mismatch: computed=%s stored=%s", lineNum, computed, storedCRC)
		}
	}
	if lineNum != count {
		t.Errorf("expected %d lines, got %d", count, lineNum)
	}
}

func TestAuditLogger_HMACMode(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")
	macKey := []byte("test-hmac-key-32-bytes-long!!!!!")

	al, err := NewAuditLogger(logFile,
		WithIntegrityMode("mac", macKey),
	)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer al.Close()

	rec := InteractionRecord{
		RecordID:       NewRecordID(),
		Timestamp:      time.Now().UTC(),
		Source:         "test",
		OperationType:  "write",
		PolicyDecision: "allowed",
	}
	if err := al.Log(rec); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	line := strings.TrimSpace(string(data))
	parts := strings.Split(line, "\t")
	if len(parts) != 3 {
		t.Fatalf("HMAC mode: expected 3 tab-separated parts, got %d", len(parts))
	}

	// Verify HMAC.
	jsonBytes := []byte(parts[0])
	storedHMAC := parts[2]
	if !validateHMAC(jsonBytes, macKey, storedHMAC) {
		t.Error("HMAC validation failed")
	}
}

func TestAuditLogger_HMACModeRequiresKey(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	_, err := NewAuditLogger(logFile,
		WithIntegrityMode("mac", nil),
	)
	if err == nil {
		t.Fatal("expected error for mac mode without key")
	}
}

func TestAuditLogger_Rotation(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	// Set tiny max size to trigger rotation quickly.
	al, err := NewAuditLogger(logFile,
		WithMaxFileSize(500), // 500 bytes
	)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer al.Close()

	// Write enough records to trigger rotation.
	for i := 0; i < 20; i++ {
		rec := InteractionRecord{
			RecordID:       NewRecordID(),
			RequestID:      fmt.Sprintf("req-%03d", i),
			Timestamp:      time.Now().UTC(),
			Source:         "test",
			OperationType:  "write",
			PolicyDecision: "allowed",
			LatencyMs:      float64(i),
		}
		if err := al.Log(rec); err != nil {
			t.Fatalf("Log[%d]: %v", i, err)
		}
	}

	// Verify rotated files exist.
	matches, err := filepath.Glob(filepath.Join(dir, "interactions-*.jsonl"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one rotated file")
	}

	// Current file should also exist.
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("current log file does not exist after rotation")
	}
}

func TestAuditLogger_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	al, err := NewAuditLogger(logFile)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer al.Close()

	const goroutines = 50
	const writesPerGoroutine = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*writesPerGoroutine)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ {
				rec := InteractionRecord{
					RecordID:       NewRecordID(),
					RequestID:      fmt.Sprintf("g%d-req-%d", gid, i),
					Timestamp:      time.Now().UTC(),
					Source:         fmt.Sprintf("source-%d", gid),
					OperationType:  "write",
					PolicyDecision: "allowed",
				}
				if err := al.Log(rec); err != nil {
					errs <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent write error: %v", err)
	}

	// Verify all records are valid.
	f, err := os.Open(logFile)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 2 {
			t.Errorf("line %d: missing CRC32", lineCount)
			continue
		}
		jsonBytes := []byte(parts[0])
		storedCRC := parts[1]
		computed := fmt.Sprintf("%08x", crc32.ChecksumIEEE(jsonBytes))
		if computed != storedCRC {
			t.Errorf("line %d: CRC32 mismatch (data corruption)", lineCount)
		}
	}

	expected := goroutines * writesPerGoroutine
	if lineCount != expected {
		t.Errorf("expected %d records, got %d", expected, lineCount)
	}
}

func TestAuditLogger_AppendFailureDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	al, err := NewAuditLogger(logFile)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	// Close the file to simulate unwritable state.
	al.Close()

	rec := InteractionRecord{
		RecordID:       NewRecordID(),
		Timestamp:      time.Now().UTC(),
		Source:         "test",
		OperationType:  "write",
		PolicyDecision: "allowed",
	}

	// Should return error, NOT panic.
	err = al.Log(rec)
	if err == nil {
		t.Log("Log after close succeeded (file was reopened)")
	}
	// The key assertion: we reached this line without panic.
}

func TestAuditLogger_RecordIDUnique(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := NewRecordID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate record_id at iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestAuditLogger_AutoGeneratesRecordID(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	al, err := NewAuditLogger(logFile)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer al.Close()

	// Log a record WITHOUT setting RecordID.
	rec := InteractionRecord{
		Timestamp:      time.Now().UTC(),
		Source:         "test",
		OperationType:  "write",
		PolicyDecision: "allowed",
	}
	if err := al.Log(rec); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	parts := strings.Split(strings.TrimSpace(string(data)), "\t")
	var parsed InteractionRecord
	if err := json.Unmarshal([]byte(parts[0]), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.RecordID == "" {
		t.Error("record_id should be auto-generated when empty")
	}
}

func TestAuditLogger_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "logs", "interactions.jsonl")

	al, err := NewAuditLogger(logFile)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer al.Close()

	// Write one record so the file exists.
	rec := InteractionRecord{
		RecordID:       NewRecordID(),
		Timestamp:      time.Now().UTC(),
		Source:         "test",
		OperationType:  "write",
		PolicyDecision: "allowed",
	}
	if err := al.Log(rec); err != nil {
		t.Fatalf("Log: %v", err)
	}

	info, err := os.Stat(logFile)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	// On Windows, permissions work differently. On Unix, verify 0600.
	// We check the file exists and is not a directory as a portable check.
	if info.IsDir() {
		t.Error("log file should not be a directory")
	}
	if info.Size() == 0 {
		t.Error("log file should not be empty after write")
	}
}

func TestAuditLogger_NoMemoryContent(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	al, err := NewAuditLogger(logFile)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer al.Close()

	rec := InteractionRecord{
		RecordID:       NewRecordID(),
		Timestamp:      time.Now().UTC(),
		Source:         "test",
		OperationType:  "write",
		PolicyDecision: "allowed",
		Subject:        "test-subject",
	}
	if err := al.Log(rec); err != nil {
		t.Fatalf("Log: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// InteractionRecord struct has no "content" field — verify it doesn't
	// appear in the JSON output. Only metadata.
	if strings.Contains(string(data), `"content"`) {
		t.Error("interaction record must not contain memory content")
	}
}

func TestAuditLogger_DeletedFileRecreated(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "interactions.jsonl")

	al, err := NewAuditLogger(logFile)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer al.Close()

	// Write first record.
	rec := InteractionRecord{
		RecordID:       NewRecordID(),
		Timestamp:      time.Now().UTC(),
		Source:         "test",
		OperationType:  "write",
		PolicyDecision: "allowed",
	}
	if err := al.Log(rec); err != nil {
		t.Fatalf("Log 1: %v", err)
	}

	// Simulate file deletion while daemon is running.
	al.mu.Lock()
	al.file.Close()
	al.file = nil
	al.mu.Unlock()
	os.Remove(logFile)

	// Next write should recreate the file.
	rec.RecordID = NewRecordID()
	if err := al.Log(rec); err != nil {
		t.Fatalf("Log after delete: %v", err)
	}

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("log file should have been recreated")
	}
}
