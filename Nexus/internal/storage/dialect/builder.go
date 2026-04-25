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

// Package dialect provides SQL dialect-aware query building for the storage
// abstraction layer. SQLite uses ? placeholders; PostgreSQL uses $1,$2,$3.
//
// Reference: Bombproof Build Plan Phase BP.0.3.
package dialect

import "fmt"

// Dialect identifies the SQL backend.
type Dialect int

const (
	SQLite   Dialect = iota
	Postgres
)

// Builder produces dialect-correct SQL fragments.
type Builder struct {
	Dialect Dialect
}

// Placeholder returns the positional parameter marker for the given 1-based
// index. SQLite returns "?", PostgreSQL returns "$1", "$2", etc.
func (b *Builder) Placeholder(n int) string {
	switch b.Dialect {
	case Postgres:
		return fmt.Sprintf("$%d", n)
	default:
		return "?"
	}
}

// UpsertSuffix returns the ON CONFLICT clause for an upsert.
// SQLite: INSERT OR IGNORE / ON CONFLICT(...) DO UPDATE SET ...
// PostgreSQL: ON CONFLICT(...) DO UPDATE SET ...
// Both use the same SQL syntax for ON CONFLICT; the difference is in
// placeholder numbering (handled by Placeholder).
func (b *Builder) UpsertSuffix(conflictCols []string) string {
	// Both SQLite and PostgreSQL support ON CONFLICT(...) DO NOTHING / DO UPDATE.
	// The caller builds the full clause using Placeholder for values.
	// This method is a placeholder for future dialect-specific upsert logic.
	// TODO(BP.9): Expand when PostgreSQL backend is implemented.
	return ""
}

// BoolLiteral returns the dialect-correct boolean literal.
// SQLite uses 0/1; PostgreSQL uses TRUE/FALSE.
func (b *Builder) BoolLiteral(v bool) string {
	switch b.Dialect {
	case Postgres:
		if v {
			return "TRUE"
		}
		return "FALSE"
	default:
		if v {
			return "1"
		}
		return "0"
	}
}

// TimestampFunc returns the dialect-correct function for current UTC timestamp.
// SQLite: datetime('now'); PostgreSQL: NOW() AT TIME ZONE 'UTC'.
func (b *Builder) TimestampFunc() string {
	switch b.Dialect {
	case Postgres:
		return "NOW() AT TIME ZONE 'UTC'"
	default:
		return "datetime('now')"
	}
}
