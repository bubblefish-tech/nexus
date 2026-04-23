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
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotLastGood_CreatesFile(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nexus.db")

	// Create the DB file.
	content := []byte("test database content")
	if err := os.WriteFile(dbPath, content, 0600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	// Write the clean-shutdown marker.
	if err := writeCleanShutdownMarker(dir); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if err := snapshotLastGood(dir, dbPath); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	lastgood, err := os.ReadFile(dbPath + ".lastgood")
	if err != nil {
		t.Fatalf("read .lastgood: %v", err)
	}
	if string(lastgood) != string(content) {
		t.Errorf("lastgood content = %q, want %q", lastgood, content)
	}
}

func TestSnapshotLastGood_SkipsWhenNoDBExists(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nonexistent.db")

	// Write the clean-shutdown marker.
	if err := writeCleanShutdownMarker(dir); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if err := snapshotLastGood(dir, dbPath); err != nil {
		t.Fatalf("snapshot should succeed with no DB: %v", err)
	}

	if _, err := os.Stat(dbPath + ".lastgood"); !os.IsNotExist(err) {
		t.Error("expected no .lastgood file when DB does not exist")
	}
}

func TestSnapshotLastGood_SkipsWhenMarkerMissing(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "nexus.db")

	content := []byte("test database content")
	if err := os.WriteFile(dbPath, content, 0600); err != nil {
		t.Fatalf("write db: %v", err)
	}

	// No clean-shutdown marker — should skip.
	if err := snapshotLastGood(dir, dbPath); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	if _, err := os.Stat(dbPath + ".lastgood"); !os.IsNotExist(err) {
		t.Error("expected no .lastgood file when clean-shutdown marker missing")
	}
}
