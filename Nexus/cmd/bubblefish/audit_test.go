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

package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/audit"
)

func TestComputeStats(t *testing.T) {
	t.Helper()

	now := time.Now().UTC()
	records := []audit.InteractionRecord{
		{
			RecordID:       "rec-1",
			Timestamp:      now.Add(-30 * time.Minute),
			Source:         "claude",
			ActorID:        "agent-1",
			OperationType:  "write",
			PolicyDecision: "allowed",
		},
		{
			RecordID:       "rec-2",
			Timestamp:      now.Add(-10 * time.Minute),
			Source:         "claude",
			ActorID:        "agent-1",
			OperationType:  "query",
			PolicyDecision: "denied",
		},
		{
			RecordID:       "rec-3",
			Timestamp:      now.Add(-5 * time.Minute),
			Source:         "cursor",
			ActorID:        "agent-2",
			OperationType:  "query",
			PolicyDecision: "allowed",
		},
		{
			RecordID:       "rec-4",
			Timestamp:      now.Add(-2 * time.Hour), // Outside 1hr window
			Source:         "cursor",
			ActorID:        "",
			OperationType:  "admin",
			PolicyDecision: "denied",
		},
	}

	stats := computeStats(records, 4)

	if stats.TotalRecords != 4 {
		t.Errorf("TotalRecords = %d, want 4", stats.TotalRecords)
	}

	// Denial rate: 2 denied / 4 total = 0.5
	if stats.DenialRate != 0.5 {
		t.Errorf("DenialRate = %f, want 0.5", stats.DenialRate)
	}

	// ByOperation
	if stats.ByOperation["write"] != 1 {
		t.Errorf("ByOperation[write] = %d, want 1", stats.ByOperation["write"])
	}
	if stats.ByOperation["query"] != 2 {
		t.Errorf("ByOperation[query] = %d, want 2", stats.ByOperation["query"])
	}
	if stats.ByOperation["admin"] != 1 {
		t.Errorf("ByOperation[admin] = %d, want 1", stats.ByOperation["admin"])
	}

	// ByDecision
	if stats.ByDecision["allowed"] != 2 {
		t.Errorf("ByDecision[allowed] = %d, want 2", stats.ByDecision["allowed"])
	}
	if stats.ByDecision["denied"] != 2 {
		t.Errorf("ByDecision[denied] = %d, want 2", stats.ByDecision["denied"])
	}

	// TopSources
	if stats.TopSources["claude"] != 2 {
		t.Errorf("TopSources[claude] = %d, want 2", stats.TopSources["claude"])
	}
	if stats.TopSources["cursor"] != 2 {
		t.Errorf("TopSources[cursor] = %d, want 2", stats.TopSources["cursor"])
	}

	// TopActors — rec-4 has empty ActorID, should be skipped.
	if stats.TopActors["agent-1"] != 2 {
		t.Errorf("TopActors[agent-1] = %d, want 2", stats.TopActors["agent-1"])
	}
	if stats.TopActors["agent-2"] != 1 {
		t.Errorf("TopActors[agent-2] = %d, want 1", stats.TopActors["agent-2"])
	}
	if _, ok := stats.TopActors[""]; ok {
		t.Error("TopActors should not include empty actor_id")
	}

	// InteractionsPerHr — only 3 records within last hour (rec-4 is 2hr old).
	perHrTotal := 0
	for _, v := range stats.InteractionsPerHr {
		perHrTotal += v
	}
	if perHrTotal != 3 {
		t.Errorf("InteractionsPerHr total = %d, want 3", perHrTotal)
	}
}

func TestComputeStats_Empty(t *testing.T) {
	t.Helper()

	stats := computeStats(nil, 0)

	if stats.TotalRecords != 0 {
		t.Errorf("TotalRecords = %d, want 0", stats.TotalRecords)
	}
	if stats.DenialRate != 0 {
		t.Errorf("DenialRate = %f, want 0", stats.DenialRate)
	}
}

func TestRecordToCSVRow(t *testing.T) {
	t.Helper()

	ts := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	rec := audit.InteractionRecord{
		RecordID:       "abc123",
		RequestID:      "req-456",
		Timestamp:      ts,
		Source:         "claude",
		ActorType:      "agent",
		ActorID:        "agent-x",
		EffectiveIP:    "127.0.0.1",
		OperationType:  "write",
		Endpoint:       "/inbound/claude",
		HTTPMethod:     "POST",
		HTTPStatusCode: 200,
		PayloadID:      "pl-1",
		Destination:    "sqlite",
		Subject:        "test",
		PolicyDecision: "allowed",
		PolicyReason:   "",
		LatencyMs:      1.234,
	}

	row := recordToCSVRow(rec)

	if len(row) != len(csvHeaders) {
		t.Fatalf("row length = %d, want %d (matching csvHeaders)", len(row), len(csvHeaders))
	}
	if row[0] != "abc123" {
		t.Errorf("row[0] record_id = %q, want %q", row[0], "abc123")
	}
	if row[3] != "claude" {
		t.Errorf("row[3] source = %q, want %q", row[3], "claude")
	}
	if row[10] != "200" {
		t.Errorf("row[10] http_status_code = %q, want %q", row[10], "200")
	}
	if row[16] != "1.234" {
		t.Errorf("row[16] latency_ms = %q, want %q", row[16], "1.234")
	}
}

func TestWriteCSV(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "export.csv")

	ts := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	records := []audit.InteractionRecord{
		{
			RecordID:       "r1",
			Timestamp:      ts,
			Source:         "claude",
			OperationType:  "write",
			PolicyDecision: "allowed",
			LatencyMs:      0.5,
		},
		{
			RecordID:       "r2",
			Timestamp:      ts.Add(time.Second),
			Source:         "cursor",
			OperationType:  "query",
			PolicyDecision: "denied",
			LatencyMs:      2.0,
		},
	}

	if err := writeCSV(outPath, records); err != nil {
		t.Fatalf("writeCSV: %v", err)
	}

	// Read back and verify.
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Logf("close: %v", err)
		}
	}()

	cr := csv.NewReader(f)
	rows, err := cr.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}

	// Header + 2 data rows.
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want 3 (header + 2 data)", len(rows))
	}

	// Verify header matches csvHeaders.
	for i, h := range csvHeaders {
		if rows[0][i] != h {
			t.Errorf("header[%d] = %q, want %q", i, rows[0][i], h)
		}
	}

	// Verify first data row.
	if rows[1][0] != "r1" {
		t.Errorf("row 1 record_id = %q, want %q", rows[1][0], "r1")
	}
	if rows[1][3] != "claude" {
		t.Errorf("row 1 source = %q, want %q", rows[1][3], "claude")
	}
}

func TestWriteJSONFile(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "export.json")

	records := []audit.InteractionRecord{
		{
			RecordID:       "r1",
			Timestamp:      time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC),
			Source:         "claude",
			OperationType:  "write",
			PolicyDecision: "allowed",
		},
	}

	if err := writeJSONFile(outPath, records); err != nil {
		t.Fatalf("writeJSONFile: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	// Should be valid JSON containing the record_id.
	if len(data) == 0 {
		t.Fatal("output file is empty")
	}

	content := string(data)
	if !contains(content, "r1") {
		t.Error("output does not contain record_id 'r1'")
	}
	if !contains(content, "claude") {
		t.Error("output does not contain source 'claude'")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
