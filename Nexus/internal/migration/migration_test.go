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

package migration_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/migration"
	_ "modernc.org/sqlite"
)

func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open :memory: db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNew_NilDB(t *testing.T) {
	mgr := migration.New(nil)
	if mgr != nil {
		t.Fatal("New(nil) should return nil Manager")
	}
}

func TestApply_NilManager(t *testing.T) {
	var mgr *migration.Manager
	if err := mgr.Apply(context.Background(), nil); err != nil {
		t.Fatalf("nil Manager.Apply should not error: %v", err)
	}
}

func TestApply_Empty(t *testing.T) {
	db := openMemDB(t)
	mgr := migration.New(db)
	if err := mgr.Apply(context.Background(), nil); err != nil {
		t.Fatalf("Apply(nil) should not error: %v", err)
	}
}

func TestApply_NoopMigration(t *testing.T) {
	db := openMemDB(t)
	mgr := migration.New(db)
	migs := []migration.Migration{
		{Version: 1, Description: "initial schema"},
	}
	if err := mgr.Apply(context.Background(), migs); err != nil {
		t.Fatalf("Apply no-op: %v", err)
	}
	applied, err := mgr.Applied(context.Background())
	if err != nil {
		t.Fatalf("Applied: %v", err)
	}
	if !applied[1] {
		t.Error("version 1 should be marked applied")
	}
}

func TestApply_RealSQL(t *testing.T) {
	db := openMemDB(t)
	mgr := migration.New(db)
	migs := []migration.Migration{
		{Version: 1, Description: "create test table", SQL: `CREATE TABLE test_tbl (id INTEGER PRIMARY KEY)`},
	}
	if err := mgr.Apply(context.Background(), migs); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Table should exist.
	if _, err := db.Exec(`INSERT INTO test_tbl (id) VALUES (1)`); err != nil {
		t.Fatalf("table not created: %v", err)
	}
}

func TestApply_Idempotent(t *testing.T) {
	db := openMemDB(t)
	mgr := migration.New(db)
	migs := []migration.Migration{
		{Version: 1, Description: "create test table", SQL: `CREATE TABLE test_tbl2 (id INTEGER PRIMARY KEY)`},
	}
	// Apply twice — second call should not error (table already created, migration already recorded).
	if err := mgr.Apply(context.Background(), migs); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := mgr.Apply(context.Background(), migs); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
}

func TestApply_MultipleMigrations(t *testing.T) {
	db := openMemDB(t)
	mgr := migration.New(db)
	migs := []migration.Migration{
		{Version: 1, Description: "create tbl", SQL: `CREATE TABLE m_tbl (id INTEGER PRIMARY KEY)`},
		{Version: 2, Description: "add col", SQL: `ALTER TABLE m_tbl ADD COLUMN name TEXT`},
		{Version: 3, Description: "marker only"},
	}
	if err := mgr.Apply(context.Background(), migs); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	applied, err := mgr.Applied(context.Background())
	if err != nil {
		t.Fatalf("Applied: %v", err)
	}
	for _, v := range []int{1, 2, 3} {
		if !applied[v] {
			t.Errorf("version %d should be applied", v)
		}
	}
}

func TestApply_SkipsAlreadyApplied(t *testing.T) {
	db := openMemDB(t)
	mgr := migration.New(db)

	// Apply v1 + v2.
	if err := mgr.Apply(context.Background(), []migration.Migration{
		{Version: 1, Description: "create tbl", SQL: `CREATE TABLE skip_tbl (id INTEGER PRIMARY KEY)`},
		{Version: 2, Description: "marker"},
	}); err != nil {
		t.Fatalf("first Apply: %v", err)
	}

	// Apply v1 + v2 + v3 — only v3 should run.
	if err := mgr.Apply(context.Background(), []migration.Migration{
		{Version: 1, Description: "create tbl", SQL: `CREATE TABLE skip_tbl (id INTEGER PRIMARY KEY)`}, // would fail if re-run
		{Version: 2, Description: "marker"},
		{Version: 3, Description: "add col", SQL: `ALTER TABLE skip_tbl ADD COLUMN name TEXT`},
	}); err != nil {
		t.Fatalf("second Apply: %v", err)
	}

	applied, _ := mgr.Applied(context.Background())
	if !applied[3] {
		t.Error("version 3 should be applied after second Apply")
	}
}

func TestApply_BadSQL(t *testing.T) {
	db := openMemDB(t)
	mgr := migration.New(db)
	migs := []migration.Migration{
		{Version: 1, Description: "bad sql", SQL: `THIS IS NOT VALID SQL`},
	}
	if err := mgr.Apply(context.Background(), migs); err == nil {
		t.Fatal("expected error for bad SQL migration")
	}
}

func TestApplied_NilManager(t *testing.T) {
	var mgr *migration.Manager
	applied, err := mgr.Applied(context.Background())
	if err != nil {
		t.Fatalf("nil Manager.Applied should not error: %v", err)
	}
	if len(applied) != 0 {
		t.Fatal("nil Manager.Applied should return empty map")
	}
}
