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

package ingest

import (
	"crypto/sha256"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNewFileStateStoreNilDB(t *testing.T) {
	_, err := NewFileStateStore(nil)
	if err == nil {
		t.Error("expected error for nil db")
	}
}

func TestFileStateStoreRoundTrip(t *testing.T) {
	db := openTestDB(t)
	store, err := NewFileStateStore(db)
	if err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256([]byte("hello"))
	if err := store.Set("claude_code", "/tmp/test.jsonl", 1024, hash); err != nil {
		t.Fatal(err)
	}

	offset, gotHash, err := store.Get("claude_code", "/tmp/test.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if offset != 1024 {
		t.Errorf("offset = %d, want 1024", offset)
	}
	if gotHash != hash {
		t.Errorf("hash mismatch")
	}
}

func TestFileStateStoreGetMissing(t *testing.T) {
	db := openTestDB(t)
	store, err := NewFileStateStore(db)
	if err != nil {
		t.Fatal(err)
	}

	offset, hash, err := store.Get("nonexistent", "/no/such/file")
	if err != nil {
		t.Fatal(err)
	}
	if offset != 0 {
		t.Errorf("offset = %d, want 0 for missing entry", offset)
	}
	if hash != [32]byte{} {
		t.Error("hash should be zero for missing entry")
	}
}

func TestFileStateStoreUpsert(t *testing.T) {
	db := openTestDB(t)
	store, err := NewFileStateStore(db)
	if err != nil {
		t.Fatal(err)
	}

	hash1 := sha256.Sum256([]byte("v1"))
	hash2 := sha256.Sum256([]byte("v2"))

	if err := store.Set("w", "/f", 100, hash1); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("w", "/f", 200, hash2); err != nil {
		t.Fatal(err)
	}

	offset, gotHash, err := store.Get("w", "/f")
	if err != nil {
		t.Fatal(err)
	}
	if offset != 200 {
		t.Errorf("offset = %d, want 200 after upsert", offset)
	}
	if gotHash != hash2 {
		t.Error("hash should be v2 after upsert")
	}
}

func TestFileStateStoreForget(t *testing.T) {
	db := openTestDB(t)
	store, err := NewFileStateStore(db)
	if err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256([]byte("data"))
	if err := store.Set("w", "/f", 100, hash); err != nil {
		t.Fatal(err)
	}
	if err := store.Forget("w", "/f"); err != nil {
		t.Fatal(err)
	}

	offset, _, err := store.Get("w", "/f")
	if err != nil {
		t.Fatal(err)
	}
	if offset != 0 {
		t.Errorf("offset = %d, want 0 after forget", offset)
	}
}

func TestFileStateStoreAll(t *testing.T) {
	db := openTestDB(t)
	store, err := NewFileStateStore(db)
	if err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256([]byte("x"))
	store.Set("w1", "/a", 10, hash)
	store.Set("w1", "/b", 20, hash)
	store.Set("w2", "/c", 30, hash)

	states, err := store.All("w1")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("All(w1) returned %d states, want 2", len(states))
	}

	states2, err := store.All("w2")
	if err != nil {
		t.Fatal(err)
	}
	if len(states2) != 1 {
		t.Fatalf("All(w2) returned %d states, want 1", len(states2))
	}

	states3, err := store.All("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(states3) != 0 {
		t.Fatalf("All(nonexistent) returned %d states, want 0", len(states3))
	}
}
