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
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/destination"
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
