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

package registry

import (
	"database/sql"
	"fmt"
	"strings"
)

// MigrateEncryptionColumns adds the CU.0.4 encrypted-column set to all
// control-plane tables in an existing database. It is safe to call on a
// database created from the current SchemaSQL (which already includes these
// columns) — duplicate-column errors from SQLite are silently ignored.
// New installations never need this call; it exists for databases created
// before CU.0.4 was merged.
func MigrateEncryptionColumns(db *sql.DB) error {
	stmts := []string{
		// grants
		`ALTER TABLE grants ADD COLUMN scope_json_encrypted    BLOB`,
		`ALTER TABLE grants ADD COLUMN revoke_reason_encrypted BLOB`,
		`ALTER TABLE grants ADD COLUMN encryption_version      INTEGER NOT NULL DEFAULT 0`,
		// approval_requests
		`ALTER TABLE approval_requests ADD COLUMN action_json_encrypted BLOB`,
		`ALTER TABLE approval_requests ADD COLUMN reason_encrypted      BLOB`,
		`ALTER TABLE approval_requests ADD COLUMN encryption_version    INTEGER NOT NULL DEFAULT 0`,
		// tasks
		`ALTER TABLE tasks ADD COLUMN input_json_encrypted  BLOB`,
		`ALTER TABLE tasks ADD COLUMN output_json_encrypted BLOB`,
		`ALTER TABLE tasks ADD COLUMN encryption_version    INTEGER NOT NULL DEFAULT 0`,
		// task_events
		`ALTER TABLE task_events ADD COLUMN payload_json_encrypted BLOB`,
		`ALTER TABLE task_events ADD COLUMN encryption_version     INTEGER NOT NULL DEFAULT 0`,
		// action_log
		`ALTER TABLE action_log ADD COLUMN policy_reason_encrypted BLOB`,
		`ALTER TABLE action_log ADD COLUMN result_encrypted        BLOB`,
		`ALTER TABLE action_log ADD COLUMN encryption_version      INTEGER NOT NULL DEFAULT 0`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("registry: encrypt migration: %w", err)
		}
	}
	return nil
}
