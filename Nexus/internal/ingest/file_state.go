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
	"database/sql"
	"fmt"
	"time"
)

// FileStateStore persists (watcher, path, offset, hash, last_seen) for every
// file a watcher has ever ingested from. Backed by a dedicated SQLite table
// in the existing memories.db.
type FileStateStore struct {
	db *sql.DB
}

// NewFileStateStore creates the ingest_file_state table if absent and
// returns a ready-to-use store. The db must be an open *sql.DB pointing at
// the daemon's SQLite database.
func NewFileStateStore(db *sql.DB) (*FileStateStore, error) {
	if db == nil {
		return nil, fmt.Errorf("ingest: file state store requires non-nil db")
	}

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS ingest_file_state (
			watcher   TEXT NOT NULL,
			path      TEXT NOT NULL,
			offset    INTEGER NOT NULL,
			hash      BLOB NOT NULL,
			last_seen INTEGER NOT NULL,
			PRIMARY KEY (watcher, path)
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("ingest: create file_state table: %w", err)
	}

	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_ingest_file_state_watcher
		ON ingest_file_state(watcher)
	`)
	if err != nil {
		return nil, fmt.Errorf("ingest: create file_state index: %w", err)
	}

	return &FileStateStore{db: db}, nil
}

// Get returns the persisted offset and hash for a (watcher, path) pair.
// If no state exists, returns (0, zero-hash, nil).
func (s *FileStateStore) Get(watcher, path string) (int64, [32]byte, error) {
	var offset int64
	var hashBlob []byte
	err := s.db.QueryRow(
		`SELECT offset, hash FROM ingest_file_state WHERE watcher = ? AND path = ?`,
		watcher, path,
	).Scan(&offset, &hashBlob)
	if err == sql.ErrNoRows {
		return 0, [32]byte{}, nil
	}
	if err != nil {
		return 0, [32]byte{}, fmt.Errorf("ingest: get file state: %w", err)
	}
	var hash [32]byte
	copy(hash[:], hashBlob)
	return offset, hash, nil
}

// Set upserts the file state for a (watcher, path) pair.
func (s *FileStateStore) Set(watcher, path string, offset int64, hash [32]byte) error {
	_, err := s.db.Exec(`
		INSERT INTO ingest_file_state (watcher, path, offset, hash, last_seen)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (watcher, path) DO UPDATE SET
			offset = excluded.offset,
			hash = excluded.hash,
			last_seen = excluded.last_seen
	`, watcher, path, offset, hash[:], time.Now().Unix())
	if err != nil {
		return fmt.Errorf("ingest: set file state: %w", err)
	}
	return nil
}

// Forget deletes the file state for a (watcher, path) pair.
func (s *FileStateStore) Forget(watcher, path string) error {
	_, err := s.db.Exec(
		`DELETE FROM ingest_file_state WHERE watcher = ? AND path = ?`,
		watcher, path,
	)
	if err != nil {
		return fmt.Errorf("ingest: forget file state: %w", err)
	}
	return nil
}

// All returns every FileState for the named watcher.
func (s *FileStateStore) All(watcher string) ([]FileState, error) {
	rows, err := s.db.Query(
		`SELECT watcher, path, offset, hash, last_seen FROM ingest_file_state WHERE watcher = ?`,
		watcher,
	)
	if err != nil {
		return nil, fmt.Errorf("ingest: list file states: %w", err)
	}
	defer rows.Close()

	var states []FileState
	for rows.Next() {
		var fs FileState
		var hashBlob []byte
		if err := rows.Scan(&fs.Watcher, &fs.Path, &fs.Offset, &hashBlob, &fs.LastSeen); err != nil {
			return nil, fmt.Errorf("ingest: scan file state: %w", err)
		}
		copy(fs.Hash[:], hashBlob)
		states = append(states, fs)
	}
	return states, rows.Err()
}
