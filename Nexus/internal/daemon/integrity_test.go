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

package daemon

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestCheckSQLiteIntegrity_HealthyDB(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := checkSQLiteIntegrity(db); err != nil {
		t.Fatalf("expected healthy db to pass integrity check: %v", err)
	}
}

func TestCheckSQLiteIntegrity_NilDB(t *testing.T) {
	t.Helper()
	if err := checkSQLiteIntegrity(nil); err != nil {
		t.Fatalf("expected nil db to pass: %v", err)
	}
}

func TestCheckAuditChain_NoTable(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := checkAuditChain(db); err != nil {
		t.Fatalf("expected no-table to pass: %v", err)
	}
}

func TestCheckAuditChain_NilDB(t *testing.T) {
	t.Helper()
	if err := checkAuditChain(nil); err != nil {
		t.Fatalf("expected nil db to pass: %v", err)
	}
}

func TestCheckEncryptionCanary_NilMKM(t *testing.T) {
	t.Helper()
	if err := checkEncryptionCanary(nil); err != nil {
		t.Fatalf("expected nil mkm to pass: %v", err)
	}
}

func TestRunIntegrityCheck_HealthyDB(t *testing.T) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := RunIntegrityCheck(db, nil, nil); err != nil {
		t.Fatalf("expected healthy db to pass full integrity check: %v", err)
	}
}

func TestRunIntegrityCheck_CorruptDB(t *testing.T) {
	t.Helper()
	// Create a DB, close it, and corrupt the file.
	dir := t.TempDir()
	dbPath := dir + "/corrupt.db"
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	// Create a table so the DB file is non-empty.
	db.Exec("CREATE TABLE test (id INTEGER)")
	db.Exec("INSERT INTO test VALUES (1)")
	db.Close()

	// Truncate the file to simulate corruption.
	if err := truncateFile(dbPath, 100); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	db2, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer db2.Close()

	err = RunIntegrityCheck(db2, nil, nil)
	if err == nil {
		t.Fatal("expected corrupt db to fail integrity check")
	}
	if !strings.Contains(err.Error(), "integrity") {
		t.Errorf("expected 'integrity' in error, got: %v", err)
	}
}

func truncateFile(path string, size int64) error {
	f, err := openFileForTruncate(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(size)
}
