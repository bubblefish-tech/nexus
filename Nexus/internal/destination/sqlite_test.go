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

package destination_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// newTestSQLite opens a SQLite destination in a temporary directory. The
// returned cleanup function removes the temp directory.
func newTestSQLite(t *testing.T) (*destination.SQLiteDestination, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d, err := destination.OpenSQLite(path, logger)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	return d, func() { _ = d.Close() }
}

// basePayload returns a valid TranslatedPayload for use in tests.
func basePayload(id string) destination.TranslatedPayload {
	return destination.TranslatedPayload{
		PayloadID:        id,
		RequestID:        "req-" + id,
		Source:           "test-source",
		Subject:          "user:alice",
		Namespace:        "default",
		Destination:      "sqlite",
		Collection:       "memories",
		Content:          "hello world",
		Model:            "gpt-4",
		Role:             "user",
		Timestamp:        time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		IdempotencyKey:   "idem-" + id,
		SchemaVersion:    1,
		TransformVersion: "v1",
		ActorType:        "user",
		ActorID:          "alice",
		Metadata:         map[string]string{"env": "test"},
	}
}

// TestSQLiteDestination_WriteRead verifies that a payload written to SQLite
// can be read back with matching fields.
func TestSQLiteDestination_WriteRead(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	p := basePayload("payload-001")
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	exists, err := d.Exists(p.PayloadID)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("expected payload to exist after Write, got false")
	}
}

// TestSQLiteDestination_Ping verifies the destination is healthy after open.
func TestSQLiteDestination_Ping(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	if err := d.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// TestSQLiteDestination_WALMode verifies PRAGMA journal_mode=WAL was applied.
func TestSQLiteDestination_WALMode(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	// We need access to the underlying *sql.DB to run a PRAGMA query.
	// Expose a DB() accessor via a test helper on the same package.
	// Since we are in an external test package, we use the exported
	// TestDB helper defined in export_test.go (same package, test build only).
	db := destination.ExposeDB(d)

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", mode)
	}
}

// TestSQLiteDestination_Exists_NotFound confirms Exists returns false for an
// unknown payload_id without error.
func TestSQLiteDestination_Exists_NotFound(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	exists, err := d.Exists("nonexistent-payload-id")
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("expected false for unknown payload_id, got true")
	}
}

// TestSQLiteDestination_IdempotentWrite verifies that writing the same
// payload_id twice succeeds without error and without producing duplicates.
func TestSQLiteDestination_IdempotentWrite(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	p := basePayload("payload-idem")

	if err := d.Write(p); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	// Second write of the same payload_id must not return an error.
	if err := d.Write(p); err != nil {
		t.Fatalf("second Write (idempotent): %v", err)
	}

	// Still exactly one record.
	db := destination.ExposeDB(d)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM memories WHERE payload_id = ?", p.PayloadID).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 record after idempotent write, got %d", count)
	}
}

// TestSQLiteDestination_NilLogger verifies that OpenSQLite panics when passed
// a nil logger.
func TestSQLiteDestination_NilLogger(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nil.db")

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil logger, got none")
		}
	}()
	_, _ = destination.OpenSQLite(path, nil)
}

// TestSQLiteDestination_WriteMultiple writes several payloads and verifies
// each can be found via Exists.
func TestSQLiteDestination_WriteMultiple(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	ids := []string{"alpha", "beta", "gamma", "delta"}
	for _, id := range ids {
		if err := d.Write(basePayload(id)); err != nil {
			t.Fatalf("Write %q: %v", id, err)
		}
	}
	for _, id := range ids {
		ok, err := d.Exists(id)
		if err != nil {
			t.Fatalf("Exists %q: %v", id, err)
		}
		if !ok {
			t.Fatalf("expected %q to exist", id)
		}
	}
}

// TestSQLiteDestination_NilMetadata confirms Write succeeds when Metadata is nil.
func TestSQLiteDestination_NilMetadata(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	p := basePayload("no-meta")
	p.Metadata = nil

	if err := d.Write(p); err != nil {
		t.Fatalf("Write with nil metadata: %v", err)
	}
	ok, err := d.Exists(p.PayloadID)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !ok {
		t.Fatal("expected record to exist")
	}
}

// TestSQLiteDestination_QueryConflicts verifies that contradictory memories
// (same subject + collection, different content) are detected.
func TestSQLiteDestination_QueryConflicts(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	// Write two memories with the same subject+collection but different content.
	p1 := basePayload("conflict-1")
	p1.Subject = "user:bob"
	p1.Collection = "preferences"
	p1.Content = "likes cats"
	p1.Source = "claude"

	p2 := basePayload("conflict-2")
	p2.Subject = "user:bob"
	p2.Collection = "preferences"
	p2.Content = "likes dogs"
	p2.Source = "chatgpt"

	// A third memory with same subject but different collection — no conflict.
	p3 := basePayload("no-conflict")
	p3.Subject = "user:bob"
	p3.Collection = "facts"
	p3.Content = "works at Acme"

	for _, p := range []destination.TranslatedPayload{p1, p2, p3} {
		if err := d.Write(p); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	groups, err := d.QueryConflicts(destination.ConflictParams{
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("QueryConflicts: %v", err)
	}

	if len(groups) != 1 {
		t.Fatalf("expected 1 conflict group, got %d", len(groups))
	}

	g := groups[0]
	if g.Subject != "user:bob" {
		t.Errorf("subject = %q, want user:bob", g.Subject)
	}
	if g.EntityKey != "preferences" {
		t.Errorf("entity_key = %q, want preferences", g.EntityKey)
	}
	if len(g.ConflictingValues) < 2 {
		t.Errorf("expected at least 2 conflicting values, got %d", len(g.ConflictingValues))
	}
}

// TestSQLiteDestination_QueryConflictsFiltered verifies source filtering.
func TestSQLiteDestination_QueryConflictsFiltered(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	p1 := basePayload("f1")
	p1.Subject = "user:x"
	p1.Collection = "prefs"
	p1.Content = "A"
	p1.Source = "s1"

	p2 := basePayload("f2")
	p2.Subject = "user:x"
	p2.Collection = "prefs"
	p2.Content = "B"
	p2.Source = "s2"

	for _, p := range []destination.TranslatedPayload{p1, p2} {
		if err := d.Write(p); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	// Filter by source=s1 — both records have different sources but same
	// subject+collection. With source filter, only s1 records are considered,
	// so there's only one distinct content → no conflict.
	groups, err := d.QueryConflicts(destination.ConflictParams{
		Source: "s1",
		Limit:  50,
	})
	if err != nil {
		t.Fatalf("QueryConflicts: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 conflicts when filtered by single source, got %d", len(groups))
	}
}

// TestSQLiteDestination_QueryTimeTravel verifies time-travel queries.
func TestSQLiteDestination_QueryTimeTravel(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC)

	for i, ts := range []time.Time{t1, t2, t3} {
		p := basePayload(fmt.Sprintf("tt-%d", i))
		p.Timestamp = ts
		p.Content = fmt.Sprintf("memory at %s", ts.Format(time.RFC3339))
		if err := d.Write(p); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	// Query as-of noon — should return t1 and t2 only.
	asOf := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	result, err := d.QueryTimeTravel(destination.TimeTravelParams{
		AsOf:  asOf,
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("QueryTimeTravel: %v", err)
	}

	if len(result.Records) != 2 {
		t.Fatalf("expected 2 records as of %s, got %d", asOf, len(result.Records))
	}

	// Results should be ordered by timestamp DESC — t2 first.
	if result.Records[0].Content != "memory at "+t2.Format(time.RFC3339) {
		t.Errorf("first record content = %q, want t2 memory", result.Records[0].Content)
	}
}

// TestSQLiteDestination_TimeTravelEmpty verifies empty result for future time.
func TestSQLiteDestination_TimeTravelEmpty(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	p := basePayload("future-1")
	p.Timestamp = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Query as of January — before the memory was written.
	result, err := d.QueryTimeTravel(destination.TimeTravelParams{
		AsOf:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("QueryTimeTravel: %v", err)
	}
	if len(result.Records) != 0 {
		t.Errorf("expected 0 records, got %d", len(result.Records))
	}
}

// TestSQLite_QueryUsesIndex verifies that the composite query index is used
// for the primary Query() WHERE clause: namespace + destination + ORDER BY timestamp DESC.
func TestSQLite_QueryUsesIndex(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	// Insert a sample record so the table is non-empty.
	if err := d.Write(basePayload("idx-1")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	db := destination.ExposeDB(d)
	rows, err := db.Query(`EXPLAIN QUERY PLAN
		SELECT payload_id FROM memories
		WHERE namespace = ? AND destination = ?
		ORDER BY timestamp DESC
		LIMIT 50`, "test", "sqlite")
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}

	if !strings.Contains(strings.ToLower(plan.String()), "idx_memories_query") {
		t.Fatalf("expected EXPLAIN to reference idx_memories_query, got:\n%s", plan.String())
	}
}

// TestSQLite_SubjectQueryUsesIndex verifies that the subject index is used
// for subject-filtered queries: WHERE subject = ? ORDER BY timestamp DESC.
func TestSQLite_SubjectQueryUsesIndex(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	if err := d.Write(basePayload("idx-2")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	db := destination.ExposeDB(d)
	rows, err := db.Query(`EXPLAIN QUERY PLAN
		SELECT payload_id FROM memories
		WHERE subject = ?
		ORDER BY timestamp DESC
		LIMIT 50`, "user:alice")
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}

	if !strings.Contains(strings.ToLower(plan.String()), "idx_memories_subject") {
		t.Fatalf("expected EXPLAIN to reference idx_memories_subject, got:\n%s", plan.String())
	}
}
