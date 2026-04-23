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
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, val TEXT)")
	return db
}

func TestNewStmtCache_Success(t *testing.T) {
	t.Helper()
	db := openTestDB(t)
	defer db.Close()

	c, err := NewStmtCache(db, "INSERT INTO test(val) VALUES(?)", "SELECT val FROM test WHERE id=?", "SELECT * FROM test WHERE id=?")
	if err != nil {
		t.Fatalf("NewStmtCache: %v", err)
	}
	defer c.Close()

	if c.Write() == nil {
		t.Error("Write() is nil")
	}
	if c.Search() == nil {
		t.Error("Search() is nil")
	}
	if c.Read() == nil {
		t.Error("Read() is nil")
	}
}

func TestStmtCache_Close(t *testing.T) {
	t.Helper()
	db := openTestDB(t)
	defer db.Close()

	c, err := NewStmtCache(db, "INSERT INTO test(val) VALUES(?)", "", "")
	if err != nil {
		t.Fatalf("NewStmtCache: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestStmtCache_WriteExecutes(t *testing.T) {
	t.Helper()
	db := openTestDB(t)
	defer db.Close()

	c, err := NewStmtCache(db, "INSERT INTO test(val) VALUES(?)", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Write().Exec("hello"); err != nil {
		t.Fatalf("write exec: %v", err)
	}

	var val string
	if err := db.QueryRow("SELECT val FROM test WHERE id=1").Scan(&val); err != nil {
		t.Fatalf("query: %v", err)
	}
	if val != "hello" {
		t.Errorf("val = %q, want %q", val, "hello")
	}
}

func TestStmtCache_SearchReturns(t *testing.T) {
	t.Helper()
	db := openTestDB(t)
	defer db.Close()

	_, _ = db.Exec("INSERT INTO test(val) VALUES('world')")
	c, err := NewStmtCache(db, "", "SELECT val FROM test WHERE id=?", "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var val string
	if err := c.Search().QueryRow(1).Scan(&val); err != nil {
		t.Fatalf("search: %v", err)
	}
	if val != "world" {
		t.Errorf("val = %q, want %q", val, "world")
	}
}
