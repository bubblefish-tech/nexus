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
	"fmt"
	"time"
)

// ApplySQLitePRAGMAs configures SQLite for high-throughput daemon workloads.
// Must be called immediately after sql.Open, before any queries.
// DO NOT apply to the audit WAL — that uses synchronous=FULL for durability.
func ApplySQLitePRAGMAs(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA mmap_size=268435456",
		"PRAGMA cache_size=-131072",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA wal_autocheckpoint=10000",
		"PRAGMA journal_size_limit=67108864",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	return nil
}

// ApplySQLitePoolDefaults sets connection pool parameters for daemon workloads.
func ApplySQLitePoolDefaults(db *sql.DB) {
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
}
