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

// Package quarantine implements durable storage for Tier-0 immune-scanner
// interceptions. Quarantined memories never enter the memories table; they
// are held here until an administrator approves or rejects them.
package quarantine

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when the requested quarantine record does not exist.
var ErrNotFound = errors.New("quarantine: record not found")

// ReviewActionApproved and ReviewActionRejected are the valid review_action values.
const (
	ReviewActionApproved = "approved"
	ReviewActionRejected = "rejected"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS quarantine (
    id TEXT PRIMARY KEY,
    original_payload_id TEXT NOT NULL,
    content TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    source_name TEXT NOT NULL DEFAULT '',
    agent_id TEXT NOT NULL DEFAULT '',
    quarantine_reason TEXT NOT NULL DEFAULT '',
    rule_id TEXT NOT NULL DEFAULT '',
    quarantined_at_ms INTEGER NOT NULL,
    reviewed_at_ms INTEGER,
    review_action TEXT,
    reviewed_by TEXT
);
CREATE INDEX IF NOT EXISTS quarantine_source ON quarantine(source_name);
CREATE INDEX IF NOT EXISTS quarantine_unreviewed ON quarantine(review_action) WHERE review_action IS NULL;
`

// Record is a durable quarantine entry produced by the Tier-0 immune scanner.
type Record struct {
	ID                string
	OriginalPayloadID string
	Content           string
	MetadataJSON      string
	SourceName        string
	AgentID           string
	QuarantineReason  string
	RuleID            string
	QuarantinedAtMs   int64
	ReviewedAtMs      *int64
	ReviewAction      *string
	ReviewedBy        *string
}

// ListFilter constrains List results. Zero value returns all unreviewed records,
// up to Limit (default 1000, max 1000).
type ListFilter struct {
	SourceName    string // empty = any source
	IncludeReviewed bool  // false = unreviewed only
	Limit         int    // 0 = default 1000
}

// Store persists quarantine records to a dedicated SQLite database.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the quarantine database at dbPath and returns a ready
// Store. The caller is responsible for calling Close when done.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("quarantine: open db %q: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1) // SQLite WAL mode: single writer
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("quarantine: init schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Insert stores a new quarantine record. rec.ID must be pre-populated.
func (s *Store) Insert(rec Record) error {
	const q = `
INSERT INTO quarantine
    (id, original_payload_id, content, metadata_json, source_name, agent_id,
     quarantine_reason, rule_id, quarantined_at_ms)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.Exec(q,
		rec.ID,
		rec.OriginalPayloadID,
		rec.Content,
		rec.MetadataJSON,
		rec.SourceName,
		rec.AgentID,
		rec.QuarantineReason,
		rec.RuleID,
		rec.QuarantinedAtMs,
	)
	if err != nil {
		return fmt.Errorf("quarantine: insert %q: %w", rec.ID, err)
	}
	return nil
}

// Get returns the quarantine record for id or ErrNotFound.
func (s *Store) Get(id string) (Record, error) {
	const q = `
SELECT id, original_payload_id, content, metadata_json, source_name, agent_id,
       quarantine_reason, rule_id, quarantined_at_ms,
       reviewed_at_ms, review_action, reviewed_by
FROM quarantine WHERE id = ?`
	row := s.db.QueryRow(q, id)
	var rec Record
	if err := scanRecord(row, &rec); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, ErrNotFound
		}
		return Record{}, fmt.Errorf("quarantine: get %q: %w", id, err)
	}
	return rec, nil
}

// List returns quarantine records matching the filter. The result is ordered
// by quarantined_at_ms descending (newest first).
func (s *Store) List(f ListFilter) ([]Record, error) {
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}

	var args []any
	where := "1=1"
	if f.SourceName != "" {
		where += " AND source_name = ?"
		args = append(args, f.SourceName)
	}
	if !f.IncludeReviewed {
		where += " AND review_action IS NULL"
	}
	args = append(args, limit)

	q := fmt.Sprintf(`
SELECT id, original_payload_id, content, metadata_json, source_name, agent_id,
       quarantine_reason, rule_id, quarantined_at_ms,
       reviewed_at_ms, review_action, reviewed_by
FROM quarantine
WHERE %s
ORDER BY quarantined_at_ms DESC
LIMIT ?`, where)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("quarantine: list: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var rec Record
		if err := scanRecord(rows, &rec); err != nil {
			return nil, fmt.Errorf("quarantine: list scan: %w", err)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Decide records an approve or reject decision for record id.
// action must be ReviewActionApproved or ReviewActionRejected.
func (s *Store) Decide(id, action, reviewedBy string) error {
	if action != ReviewActionApproved && action != ReviewActionRejected {
		return fmt.Errorf("quarantine: invalid review_action %q", action)
	}
	nowMs := time.Now().UnixMilli()
	const q = `
UPDATE quarantine
SET review_action = ?, reviewed_at_ms = ?, reviewed_by = ?
WHERE id = ?`
	res, err := s.db.Exec(q, action, nowMs, reviewedBy, id)
	if err != nil {
		return fmt.Errorf("quarantine: decide %q: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("quarantine: decide rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Count returns the total number of quarantine records and the number of
// pending (unreviewed) records. Used by GET /api/quarantine/count.
func (s *Store) Count() (total, pending int, err error) {
	const q = `SELECT COUNT(*),
		SUM(CASE WHEN review_action IS NULL THEN 1 ELSE 0 END)
		FROM quarantine`
	var pendingNull *int
	if err = s.db.QueryRow(q).Scan(&total, &pendingNull); err != nil {
		return 0, 0, fmt.Errorf("quarantine: count: %w", err)
	}
	if pendingNull != nil {
		pending = *pendingNull
	}
	return total, pending, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// NewID returns a random hex-encoded 16-byte quarantine record identifier.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("quarantine: crypto/rand.Read failed: %v", err))
	}
	return "qtn_" + hex.EncodeToString(b)
}

// scanner abstracts *sql.Row and *sql.Rows for scanRecord.
type scanner interface {
	Scan(dest ...any) error
}

func scanRecord(s scanner, rec *Record) error {
	return s.Scan(
		&rec.ID,
		&rec.OriginalPayloadID,
		&rec.Content,
		&rec.MetadataJSON,
		&rec.SourceName,
		&rec.AgentID,
		&rec.QuarantineReason,
		&rec.RuleID,
		&rec.QuarantinedAtMs,
		&rec.ReviewedAtMs,
		&rec.ReviewAction,
		&rec.ReviewedBy,
	)
}
