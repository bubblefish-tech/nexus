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

// Package migration provides a lightweight schema migration framework for SQL
// databases. It maintains a nexus_migrations table and applies numbered
// migrations exactly once, in version order.
//
// Usage:
//
//	mgr := migration.New(db)
//	err := mgr.Apply(ctx, []migration.Migration{
//	    {Version: 1, Description: "initial schema"},
//	    {Version: 2, Description: "add index", SQL: "CREATE INDEX ..."},
//	})
package migration

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Migration describes a single schema change.
// SQL is optional: a migration with empty SQL is recorded as applied but
// executes no DDL (useful for marking pre-existing schemas as v1).
type Migration struct {
	Version     int
	Description string
	SQL         string // empty = no-op marker
}

// Dialect selects the SQL placeholder style.
type Dialect int

const (
	// DialectQuestion uses ? placeholders (SQLite, MySQL, TiDB, Turso, CockroachDB via pgx).
	DialectQuestion Dialect = iota
	// DialectDollar uses $1 $2 placeholders (PostgreSQL).
	DialectDollar
)

// Manager applies versioned migrations against a *sql.DB.
// A nil Manager is safe: Apply is a no-op.
type Manager struct {
	db      *sql.DB
	dialect Dialect
}

// New returns a Manager backed by db using question-mark placeholders.
func New(db *sql.DB) *Manager {
	return NewWithDialect(db, DialectQuestion)
}

// NewWithDialect returns a Manager backed by db using the given placeholder dialect.
func NewWithDialect(db *sql.DB, d Dialect) *Manager {
	if db == nil {
		return nil
	}
	return &Manager{db: db, dialect: d}
}

// Apply ensures nexus_migrations exists, then applies any migrations whose
// version is not yet recorded. Migrations are applied in ascending version order.
// Each migration runs in its own transaction; a failure stops the run and
// returns an error without rolling back previously applied migrations.
func (m *Manager) Apply(ctx context.Context, migrations []Migration) error {
	if m == nil {
		return nil
	}
	if err := m.ensureTable(ctx); err != nil {
		return fmt.Errorf("migration: create table: %w", err)
	}
	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return fmt.Errorf("migration: list versions: %w", err)
	}
	for _, mig := range migrations {
		if applied[mig.Version] {
			continue
		}
		if err := m.applyOne(ctx, mig); err != nil {
			return err
		}
	}
	return nil
}

// Applied returns the set of migration versions that have been recorded.
func (m *Manager) Applied(ctx context.Context) (map[int]bool, error) {
	if m == nil {
		return nil, nil
	}
	if err := m.ensureTable(ctx); err != nil {
		return nil, fmt.Errorf("migration: create table: %w", err)
	}
	return m.appliedVersions(ctx)
}

func (m *Manager) ensureTable(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS nexus_migrations (
			version        INTEGER PRIMARY KEY,
			description    TEXT    NOT NULL,
			applied_at_ms  INTEGER NOT NULL
		)
	`)
	return err
}

func (m *Manager) appliedVersions(ctx context.Context) (map[int]bool, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT version FROM nexus_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func (m *Manager) applyOne(ctx context.Context, mig Migration) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migration v%d: begin tx: %w", mig.Version, err)
	}
	defer tx.Rollback()

	if mig.SQL != "" {
		if _, err := tx.ExecContext(ctx, mig.SQL); err != nil {
			return fmt.Errorf("migration v%d (%s): exec: %w", mig.Version, mig.Description, err)
		}
	}

	insertSQL := `INSERT INTO nexus_migrations (version, description, applied_at_ms) VALUES (?, ?, ?)`
	if m.dialect == DialectDollar {
		insertSQL = `INSERT INTO nexus_migrations (version, description, applied_at_ms) VALUES ($1, $2, $3)`
	}
	_, err = tx.ExecContext(ctx, insertSQL, mig.Version, mig.Description, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("migration v%d: record: %w", mig.Version, err)
	}
	return tx.Commit()
}
