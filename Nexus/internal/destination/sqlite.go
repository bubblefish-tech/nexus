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
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	// Pure-Go SQLite driver (modernc.org/sqlite). No CGO required for production.
	// Registers the "sqlite" driver name with database/sql.
	_ "modernc.org/sqlite"
)

const (
	// sqliteDriverName is the driver name registered by modernc.org/sqlite.
	sqliteDriverName = "sqlite"

	// createMemoriesTable is the DDL for the memories table. Uses
	// IF NOT EXISTS so it is idempotent across restarts.
	createMemoriesTable = `
CREATE TABLE IF NOT EXISTS memories (
    payload_id        TEXT    PRIMARY KEY,
    request_id        TEXT    NOT NULL DEFAULT '',
    source            TEXT    NOT NULL DEFAULT '',
    subject           TEXT    NOT NULL DEFAULT '',
    namespace         TEXT    NOT NULL DEFAULT '',
    destination       TEXT    NOT NULL DEFAULT '',
    collection        TEXT    NOT NULL DEFAULT '',
    content           TEXT    NOT NULL DEFAULT '',
    model             TEXT    NOT NULL DEFAULT '',
    role              TEXT    NOT NULL DEFAULT '',
    timestamp         TEXT    NOT NULL DEFAULT '',
    idempotency_key   TEXT    NOT NULL DEFAULT '',
    schema_version    INTEGER NOT NULL DEFAULT 0,
    transform_version TEXT    NOT NULL DEFAULT '',
    actor_type        TEXT    NOT NULL DEFAULT '',
    actor_id          TEXT    NOT NULL DEFAULT '',
    metadata          TEXT    NOT NULL DEFAULT '{}',
    created_at        TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
)`

	createIdempotencyKeyIndex = `
CREATE INDEX IF NOT EXISTS idx_memories_idempotency_key
    ON memories (idempotency_key)`
)

// SQLiteDestination writes TranslatedPayload records to a SQLite database.
// The database is opened with PRAGMA journal_mode=WAL and
// PRAGMA busy_timeout=5000 to maximise concurrent read throughput and avoid
// "database is locked" errors under moderate write load.
//
// All SQL queries use parameterized statements — string concatenation for SQL
// is never used. Write is idempotent: INSERT OR IGNORE means re-delivering a
// payload_id is a no-op.
//
// All state is held in struct fields; there are no package-level variables.
type SQLiteDestination struct {
	db     *sql.DB
	path   string
	logger *slog.Logger
}

// OpenSQLite opens (or creates) a SQLite database at path, applies the
// required PRAGMAs, and creates the memories schema if absent. The parent
// directory is created with 0700 permissions; the database file is created
// with 0600 permissions.
//
// Panics if logger is nil. Returns an error if the database cannot be opened
// or the schema cannot be applied.
func OpenSQLite(path string, logger *slog.Logger) (*SQLiteDestination, error) {
	if logger == nil {
		panic("destination: SQLite logger must not be nil")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("destination: sqlite: create directory %q: %w", dir, err)
	}

	db, err := sql.Open(sqliteDriverName, path)
	if err != nil {
		return nil, fmt.Errorf("destination: sqlite: open %q: %w", path, err)
	}

	// SQLite is not safe for concurrent writes from multiple connections when
	// using the modernc driver unless we serialise through a single connection.
	// A pool of 1 write connection avoids "database is locked" errors from
	// concurrent goroutines hitting the same file handle.
	db.SetMaxOpenConns(1)

	d := &SQLiteDestination{
		db:     db,
		path:   path,
		logger: logger,
	}

	if err := d.applyPragmasAndSchema(); err != nil {
		db.Close()
		return nil, err
	}

	// Ensure the database file has restricted permissions (0600).
	// Best-effort on platforms where chmod is not supported (e.g. some Windows
	// configurations). The parent directory (0700) still limits access.
	_ = os.Chmod(path, 0600)

	logger.Info("destination: sqlite opened",
		"component", "destination",
		"path", path,
	)

	return d, nil
}

// applyPragmasAndSchema configures WAL journal mode, busy timeout, and
// creates the memories table + index.
func (d *SQLiteDestination) applyPragmasAndSchema() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := d.db.Exec(p); err != nil {
			return fmt.Errorf("destination: sqlite: exec %q: %w", p, err)
		}
	}

	if _, err := d.db.Exec(createMemoriesTable); err != nil {
		return fmt.Errorf("destination: sqlite: create memories table: %w", err)
	}
	if _, err := d.db.Exec(createIdempotencyKeyIndex); err != nil {
		return fmt.Errorf("destination: sqlite: create idempotency_key index: %w", err)
	}
	return nil
}

// Write persists p to the memories table. The operation is idempotent:
// INSERT OR IGNORE silently discards a write whose payload_id already exists.
// All values are bound via parameterized placeholders — no string interpolation.
func (d *SQLiteDestination) Write(p TranslatedPayload) error {
	metadataJSON, err := marshalMetadata(p.Metadata)
	if err != nil {
		return fmt.Errorf("destination: sqlite: marshal metadata: %w", err)
	}

	const query = `
INSERT OR IGNORE INTO memories (
    payload_id, request_id, source, subject, namespace, destination,
    collection, content, model, role, timestamp, idempotency_key,
    schema_version, transform_version, actor_type, actor_id, metadata
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = d.db.Exec(query,
		p.PayloadID,
		p.RequestID,
		p.Source,
		p.Subject,
		p.Namespace,
		p.Destination,
		p.Collection,
		p.Content,
		p.Model,
		p.Role,
		p.Timestamp.UTC().Format("2006-01-02T15:04:05.999999999Z"),
		p.IdempotencyKey,
		p.SchemaVersion,
		p.TransformVersion,
		p.ActorType,
		p.ActorID,
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("destination: sqlite: write payload_id %q: %w", p.PayloadID, err)
	}

	d.logger.Debug("destination: sqlite: write",
		"component", "destination",
		"payload_id", p.PayloadID,
		"source", p.Source,
	)
	return nil
}

// Ping verifies the database connection is alive by executing a lightweight
// query. Used by the doctor command and /ready health endpoint.
func (d *SQLiteDestination) Ping() error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("destination: sqlite: ping: %w", err)
	}
	return nil
}

// Exists reports whether a record with payloadID exists in the memories table.
// Used by consistency assertions (Phase R-10).
func (d *SQLiteDestination) Exists(payloadID string) (bool, error) {
	const query = `SELECT 1 FROM memories WHERE payload_id = ? LIMIT 1`
	var dummy int
	err := d.db.QueryRow(query, payloadID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("destination: sqlite: exists %q: %w", payloadID, err)
	}
	return true, nil
}

// Close closes the underlying database connection. Safe to call once.
func (d *SQLiteDestination) Close() error {
	if err := d.db.Close(); err != nil {
		return fmt.Errorf("destination: sqlite: close: %w", err)
	}
	return nil
}

// marshalMetadata serialises metadata to JSON. Returns "{}" for nil maps.
func marshalMetadata(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
