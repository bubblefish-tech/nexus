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

// Package store provides durable SQLite-backed storage for A2A tasks.
package store

import "database/sql"

// migrations is the ordered list of DDL statements for the task store schema.
var migrations = []string{
	`CREATE TABLE IF NOT EXISTS a2a_tasks (
		task_id TEXT PRIMARY KEY,
		context_id TEXT NOT NULL,
		skill TEXT NOT NULL,
		source_agent_id TEXT NOT NULL,
		target_agent_id TEXT NOT NULL,
		state TEXT NOT NULL,
		created_at_ms INTEGER NOT NULL,
		updated_at_ms INTEGER NOT NULL,
		final_task_json BLOB,
		governance_json BLOB,
		audit_id TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS idx_a2a_tasks_context ON a2a_tasks(context_id)`,
	`CREATE INDEX IF NOT EXISTS idx_a2a_tasks_state ON a2a_tasks(state)`,

	`CREATE TABLE IF NOT EXISTS a2a_task_events (
		task_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		kind TEXT NOT NULL,
		at_ms INTEGER NOT NULL,
		payload_json BLOB NOT NULL,
		PRIMARY KEY (task_id, seq)
	) WITHOUT ROWID`,

	`CREATE TABLE IF NOT EXISTS a2a_task_push_config (
		task_id TEXT PRIMARY KEY,
		url TEXT NOT NULL,
		auth_json BLOB,
		token TEXT NOT NULL,
		created_at_ms INTEGER NOT NULL
	)`,
}

// Migrate runs all CREATE TABLE/INDEX statements for the task store schema.
// It is safe to call multiple times (all statements use IF NOT EXISTS).
func Migrate(db *sql.DB) error {
	for _, stmt := range migrations {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}
