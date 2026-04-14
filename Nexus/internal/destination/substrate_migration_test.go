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

package destination

import (
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func openTestSQLite(t *testing.T) *SQLiteDestination {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenSQLite(path, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// TestSubstrateMigrationCreatesColumns verifies that the substrate columns
// are added to the memories table after OpenSQLite().
func TestSubstrateMigrationCreatesColumns(t *testing.T) {
	d := openTestSQLite(t)

	columns := []string{"sketch", "embedding_ciphertext", "embedding_nonce"}
	for _, col := range columns {
		row := d.db.QueryRow("SELECT " + col + " FROM memories LIMIT 1")
		var dummy interface{}
		err := row.Scan(&dummy)
		if err != nil && err != sql.ErrNoRows {
			t.Fatalf("column %q should exist: %v", col, err)
		}
	}
}

// TestSubstrateMigrationCreatesTables verifies that the substrate tables
// are created.
func TestSubstrateMigrationCreatesTables(t *testing.T) {
	d := openTestSQLite(t)

	tables := []string{
		"substrate_ratchet_states",
		"substrate_memory_state",
		"substrate_canonical_whitening",
		"substrate_cuckoo_filter",
	}
	for _, tbl := range tables {
		var name string
		err := d.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q should exist: %v", tbl, err)
		}
	}
}

// TestSubstrateMigrationIdempotent verifies that opening the same database
// twice does not error (all migrations are idempotent).
func TestSubstrateMigrationIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	d1, err := OpenSQLite(path, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	d1.Close()

	// Open again — should not error
	d2, err := OpenSQLite(path, slog.Default())
	if err != nil {
		t.Fatalf("idempotent open failed: %v", err)
	}
	d2.Close()
}

// TestSubstrateColumnsNullable verifies that new columns allow NULL.
func TestSubstrateColumnsNullable(t *testing.T) {
	d := openTestSQLite(t)

	// Write a record without substrate data
	p := TranslatedPayload{
		PayloadID:      "test-null-substrate",
		IdempotencyKey: "idem-1",
		Namespace:      "ns",
		Source:         "src",
		Destination:    "dst",
		Subject:        "subj",
		Content:        "test content",
		Metadata:       map[string]string{},
		Timestamp:      time.Now(),
	}
	if err := d.Write(p); err != nil {
		t.Fatal(err)
	}

	var sketch, ciphertext, nonce interface{}
	err := d.db.QueryRow(
		"SELECT sketch, embedding_ciphertext, embedding_nonce FROM memories WHERE payload_id = ?",
		"test-null-substrate",
	).Scan(&sketch, &ciphertext, &nonce)
	if err != nil {
		t.Fatal(err)
	}
	if sketch != nil || ciphertext != nil || nonce != nil {
		t.Fatal("substrate columns should be NULL for records without substrate data")
	}
}

// TestSubstrateRatchetStatesTableInsert verifies the ratchet states table
// accepts inserts and auto-increments.
func TestSubstrateRatchetStatesTableInsert(t *testing.T) {
	d := openTestSQLite(t)

	result, err := d.db.Exec(`INSERT INTO substrate_ratchet_states
		(created_at, state_bytes, canonical_dim, sketch_bits, signature)
		VALUES (?, ?, ?, ?, ?)`,
		1000, make([]byte, 32), 1024, 1, []byte{})
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	if id != 1 {
		t.Fatalf("expected auto-increment ID=1, got %d", id)
	}
}
