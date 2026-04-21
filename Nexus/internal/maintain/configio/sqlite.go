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

// SQLite config reader: read-only. Never writes. The schema is tool-specific
// and a bad write can corrupt the database.
package configio

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // register "sqlite" driver
)

// openSQLite opens a SQLite database read-only and reads key-value pairs from
// common schema patterns. Results are keyed as table.key. Returns an empty map
// on any read error — detection continues without crashing.
func openSQLite(path string) (any, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return map[string]any{}, nil // DB unavailable — return empty gracefully
	}
	defer db.Close()

	result := map[string]any{}
	tables, err := listSQLiteTables(db)
	if err != nil {
		return result, nil
	}
	for _, table := range tables {
		kv, err := readSQLiteKeyValue(db, table)
		if err != nil || len(kv) == 0 {
			continue
		}
		result[table] = kv
	}
	return result, nil
}

func listSQLiteTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// readSQLiteKeyValue tries common key/value column name pairs used by AI tool
// state databases (VS Code's ItemTable, etc.).
func readSQLiteKeyValue(db *sql.DB, table string) (map[string]any, error) {
	pairs := [][2]string{
		{"key", "value"},
		{"name", "value"},
		{"key", "data"},
	}
	for _, pair := range pairs {
		q := fmt.Sprintf("SELECT %s, %s FROM %q LIMIT 1000", pair[0], pair[1], table)
		rows, err := db.Query(q)
		if err != nil {
			continue
		}
		result := map[string]any{}
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err == nil {
				result[k] = v
			}
		}
		rows.Close()
		if len(result) > 0 {
			return result, nil
		}
	}
	return nil, nil
}
