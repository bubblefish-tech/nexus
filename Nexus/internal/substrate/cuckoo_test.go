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

package substrate

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Create substrate tables
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS substrate_cuckoo_filter (
			filter_id                INTEGER PRIMARY KEY,
			filter_bytes             BLOB NOT NULL,
			last_persisted           INTEGER NOT NULL,
			filter_bytes_encrypted   BLOB,
			filter_bytes_enc_version INTEGER NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			payload_id TEXT PRIMARY KEY,
			content TEXT
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// ─── Basic operations ───────────────────────────────────────────────────────

func TestCuckooInsertLookupDelete(t *testing.T) {
	o := NewCuckooOracle(1024)
	if err := o.Insert("mem-1"); err != nil {
		t.Fatal(err)
	}
	if !o.Lookup("mem-1") {
		t.Fatal("inserted memory should be found")
	}

	o.Delete("mem-1")
	if o.Lookup("mem-1") {
		t.Fatal("deleted memory should not be found")
	}
}

func TestCuckooLookupNonExistent(t *testing.T) {
	o := NewCuckooOracle(1024)
	// May return false or true (false positive). Just ensure no panic.
	_ = o.Lookup("does-not-exist")
}

func TestCuckooInsertMany(t *testing.T) {
	o := NewCuckooOracle(20000)
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("mem-%06d", i)
		if err := o.Insert(id); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	// All should be findable
	for i := 0; i < 10000; i++ {
		id := fmt.Sprintf("mem-%06d", i)
		if !o.Lookup(id) {
			t.Fatalf("inserted memory %s not found (false negative)", id)
		}
	}
	if o.Count() != 10000 {
		t.Fatalf("expected count=10000, got %d", o.Count())
	}
}

func TestCuckooDeleteHalf(t *testing.T) {
	o := NewCuckooOracle(20000)
	for i := 0; i < 10000; i++ {
		o.Insert(fmt.Sprintf("mem-%06d", i))
	}
	// Delete even-numbered
	for i := 0; i < 10000; i += 2 {
		o.Delete(fmt.Sprintf("mem-%06d", i))
	}
	// Odd should be present, even should be absent (with possible false positives)
	for i := 1; i < 10000; i += 2 {
		id := fmt.Sprintf("mem-%06d", i)
		if !o.Lookup(id) {
			t.Fatalf("odd memory %s should be present", id)
		}
	}
}

func TestCuckooStats(t *testing.T) {
	o := NewCuckooOracle(1024)
	o.Insert("a")
	o.Insert("b")
	o.Insert("c")
	o.Delete("b")

	stats := o.Stats()
	if stats.InsertCount != 3 {
		t.Fatalf("expected insert_count=3, got %d", stats.InsertCount)
	}
	if stats.DeleteCount != 1 {
		t.Fatalf("expected delete_count=1, got %d", stats.DeleteCount)
	}
	if stats.Count != 2 {
		t.Fatalf("expected count=2, got %d", stats.Count)
	}
}

// ─── False positive rate ────────────────────────────────────────────────────

func TestCuckooFalsePositiveRate(t *testing.T) {
	o := NewCuckooOracle(20000)
	// Insert 10,000 items
	for i := 0; i < 10000; i++ {
		o.Insert(fmt.Sprintf("inserted-%d", i))
	}
	// Test 100,000 non-inserted items
	falsePositives := 0
	total := 100000
	for i := 0; i < total; i++ {
		if o.Lookup(fmt.Sprintf("not-inserted-%d", i)) {
			falsePositives++
		}
	}
	fpRate := float64(falsePositives) / float64(total)
	t.Logf("false positive rate: %.4f%% (%d/%d)", fpRate*100, falsePositives, total)
	if fpRate > 0.05 {
		t.Fatalf("false positive rate too high: %.4f%%", fpRate*100)
	}
}

// ─── Concurrency ────────────────────────────────────────────────────────────

func TestCuckooConcurrency(t *testing.T) {
	o := NewCuckooOracle(20000)
	var wg sync.WaitGroup

	// 10 goroutines each inserting 100 unique IDs
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				id := fmt.Sprintf("g%d-mem-%d", g, i)
				o.Insert(id)
			}
		}(g)
	}

	// Concurrent lookups
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				o.Lookup(fmt.Sprintf("g%d-mem-%d", g, i%100))
			}
		}(g)
	}

	wg.Wait()
	// Should have ~1000 items (some may fail due to capacity)
	count := o.Count()
	if count < 900 {
		t.Fatalf("expected ~1000 items, got %d", count)
	}
}

// ─── Persistence ────────────────────────────────────────────────────────────

func TestCuckooPersistAndLoad(t *testing.T) {
	db := newTestDB(t)

	o := NewCuckooOracle(1024)
	o.Insert("alpha")
	o.Insert("beta")
	o.Insert("gamma")

	if err := o.Persist(db); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCuckooOracle(db, 1024, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Lookup("alpha") {
		t.Fatal("loaded filter missing 'alpha'")
	}
	if !loaded.Lookup("beta") {
		t.Fatal("loaded filter missing 'beta'")
	}
	if !loaded.Lookup("gamma") {
		t.Fatal("loaded filter missing 'gamma'")
	}
}

func TestCuckooLoadMissingRow(t *testing.T) {
	db := newTestDB(t)
	_, err := LoadCuckooOracle(db, 1024, nil)
	if !errors.Is(err, ErrCuckooNotPersisted) {
		t.Fatalf("expected ErrCuckooNotPersisted, got %v", err)
	}
}

func TestCuckooLoadCorrupt(t *testing.T) {
	db := newTestDB(t)
	// Insert garbage bytes
	_, err := db.Exec(`INSERT INTO substrate_cuckoo_filter (filter_id, filter_bytes, last_persisted) VALUES (1, ?, 0)`,
		[]byte{0xFF, 0xFE, 0xFD})
	if err != nil {
		t.Fatal(err)
	}
	_, err = LoadCuckooOracle(db, 1024, nil)
	if err == nil {
		t.Fatal("expected error for corrupt data")
	}
	if !errors.Is(err, ErrCuckooCorrupt) {
		t.Fatalf("expected ErrCuckooCorrupt, got %v", err)
	}
}

func TestCuckooPersistIdempotent(t *testing.T) {
	db := newTestDB(t)
	o := NewCuckooOracle(1024)
	o.Insert("test")

	// Persist twice — should upsert without error
	if err := o.Persist(db); err != nil {
		t.Fatal(err)
	}
	o.Insert("test2")
	if err := o.Persist(db); err != nil {
		t.Fatal(err)
	}

	loaded, _ := LoadCuckooOracle(db, 1024, nil)
	if !loaded.Lookup("test2") {
		t.Fatal("second persist should update the row")
	}
}

// ─── Rebuild from DB ────────────────────────────────────────────────────────

func TestRebuildFromDBEmpty(t *testing.T) {
	db := newTestDB(t)
	oracle, err := RebuildFromDB(db, 1024, slog.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if oracle.Count() != 0 {
		t.Fatalf("empty DB should produce empty filter, got count=%d", oracle.Count())
	}
	stats := oracle.Stats()
	if stats.RebuildCount != 1 {
		t.Fatalf("expected rebuild_count=1, got %d", stats.RebuildCount)
	}
}

func TestRebuildFromDBWithMemories(t *testing.T) {
	db := newTestDB(t)

	// Insert 500 memories
	for i := 0; i < 500; i++ {
		_, err := db.Exec(`INSERT INTO memories (payload_id, content) VALUES (?, ?)`,
			fmt.Sprintf("mem-%03d", i), "content")
		if err != nil {
			t.Fatal(err)
		}
	}

	oracle, err := RebuildFromDB(db, 2000, slog.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if oracle.Count() != 500 {
		t.Fatalf("expected 500, got %d", oracle.Count())
	}

	// All should be findable
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("mem-%03d", i)
		if !oracle.Lookup(id) {
			t.Fatalf("rebuilt filter missing %s", id)
		}
	}
}

// ─── Capacity management ────────────────────────────────────────────────────

func TestCuckooMinCapacity(t *testing.T) {
	o := NewCuckooOracle(10) // below floor
	if o.capacity < 1024 {
		t.Fatalf("capacity should be floored to 1024, got %d", o.capacity)
	}
}
