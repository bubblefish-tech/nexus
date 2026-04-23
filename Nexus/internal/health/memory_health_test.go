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

package health

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS interaction_log (
			record_id TEXT PRIMARY KEY,
			timestamp_ms INTEGER NOT NULL,
			source TEXT NOT NULL,
			operation_type TEXT NOT NULL,
			policy_decision TEXT NOT NULL DEFAULT 'allowed'
		);
		CREATE TABLE IF NOT EXISTS quarantine (
			id TEXT PRIMARY KEY,
			intercepted_at INTEGER NOT NULL
		);
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCalculateMemoryHealth_EmptyDB(t *testing.T) {
	db := testDB(t)
	h, err := CalculateMemoryHealth(db)
	if err != nil {
		t.Fatal(err)
	}
	if h.ContinuityScore != 1.0 {
		t.Errorf("expected continuity_score=1.0 on empty DB, got %f", h.ContinuityScore)
	}
	if h.TotalMemories7d != 0 {
		t.Errorf("expected total_memories_7d=0, got %d", h.TotalMemories7d)
	}
}

func TestCalculateMemoryHealth_NilDB(t *testing.T) {
	h, err := CalculateMemoryHealth(nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.ContinuityScore != 1.0 {
		t.Errorf("expected continuity_score=1.0 with nil DB, got %f", h.ContinuityScore)
	}
}

func TestCalculateMemoryHealth_WithWrites(t *testing.T) {
	db := testDB(t)
	now := time.Now().UnixMilli()

	for i := 0; i < 10; i++ {
		source := "agent-1"
		if i >= 7 {
			source = "agent-2"
		}
		if i >= 9 {
			source = "agent-3"
		}
		db.Exec(`INSERT INTO interaction_log (record_id, timestamp_ms, source, operation_type, policy_decision) VALUES (?, ?, ?, 'write', 'allowed')`,
			"rec-"+string(rune('a'+i)), now-int64(i*1000), source)
	}

	for i := 0; i < 5; i++ {
		source := "agent-1"
		if i >= 3 {
			source = "agent-2"
		}
		db.Exec(`INSERT INTO interaction_log (record_id, timestamp_ms, source, operation_type, policy_decision) VALUES (?, ?, ?, 'query', 'allowed')`,
			"qrec-"+string(rune('a'+i)), now-int64(i*1000), source)
	}

	h, err := CalculateMemoryHealth(db)
	if err != nil {
		t.Fatal(err)
	}
	if h.TotalMemories7d != 10 {
		t.Errorf("expected 10 writes, got %d", h.TotalMemories7d)
	}
	if h.ContinuityScore != 1.0 {
		t.Errorf("expected score 1.0, got %f", h.ContinuityScore)
	}
	if h.CrossAgentCoverage.WritingAgents < 2 {
		t.Errorf("expected at least 2 writing agents, got %d", h.CrossAgentCoverage.WritingAgents)
	}
	if h.CrossAgentCoverage.ReadingAgents < 1 {
		t.Errorf("expected at least 1 reading agent, got %d", h.CrossAgentCoverage.ReadingAgents)
	}
}

func TestCalculateMemoryHealth_Quarantine(t *testing.T) {
	db := testDB(t)
	now := time.Now().UnixMilli()

	db.Exec(`INSERT INTO quarantine (id, intercepted_at) VALUES ('q1', ?)`, now)
	db.Exec(`INSERT INTO quarantine (id, intercepted_at) VALUES ('q2', ?)`, now-1000)
	db.Exec(`INSERT INTO quarantine (id, intercepted_at) VALUES ('q3', ?)`, now-int64(8*24*60*60*1000))

	h, err := CalculateMemoryHealth(db)
	if err != nil {
		t.Fatal(err)
	}
	if h.QuarantineCount7d != 2 {
		t.Errorf("expected 2 quarantined in 7d, got %d", h.QuarantineCount7d)
	}
}

func TestFormatRelative(t *testing.T) {
	tests := []struct {
		dur  time.Duration
		want string
	}{
		{30 * time.Second, "30s ago"},
		{5 * time.Minute, "5m ago"},
		{2 * time.Hour, "2h ago"},
		{3 * 24 * time.Hour, "3d ago"},
	}
	for _, tt := range tests {
		got := formatRelative(time.Now().Add(-tt.dur))
		if got != tt.want {
			t.Errorf("formatRelative(-%v) = %q, want %q", tt.dur, got, tt.want)
		}
	}
}
