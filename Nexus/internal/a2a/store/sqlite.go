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

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/server"
	_ "modernc.org/sqlite" // SQLite driver
)

// Compile-time check that SQLiteTaskStore implements server.TaskStore.
var _ server.TaskStore = (*SQLiteTaskStore)(nil)

// SQLiteTaskStore is a durable TaskStore backed by SQLite via modernc.org/sqlite.
type SQLiteTaskStore struct {
	db *sql.DB
}

// NewSQLiteTaskStore opens (or creates) a SQLite database at path, configures
// WAL mode and synchronous=FULL, runs migrations, and returns a ready store.
func NewSQLiteTaskStore(path string) (*SQLiteTaskStore, error) {
	dsn := path + "?_pragma=busy_timeout%3d5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open %q: %w", path, err)
	}

	// Serialize writes to avoid SQLITE_BUSY with modernc driver.
	db.SetMaxOpenConns(1)

	// Enable WAL mode for concurrency and durability.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", pragma, err)
		}
	}

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	return &SQLiteTaskStore{db: db}, nil
}

// NewSQLiteTaskStoreFromDB wraps an existing *sql.DB (which must already be
// migrated and configured). This is useful for tests that share a database.
func NewSQLiteTaskStoreFromDB(db *sql.DB) *SQLiteTaskStore {
	return &SQLiteTaskStore{db: db}
}

// Close closes the underlying database connection.
func (s *SQLiteTaskStore) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for use by other packages sharing this database.
func (s *SQLiteTaskStore) DB() *sql.DB {
	return s.db
}

// CreateTask persists a new task. The task's metadata is stored as the
// final_task_json column for full fidelity round-tripping.
func (s *SQLiteTaskStore) CreateTask(ctx context.Context, task *a2a.Task) error {
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("store: marshal task: %w", err)
	}

	nowMs := time.Now().UnixMilli()

	// Extract source/target/skill from extensions if present; fall back to
	// empty strings. The governance extension is embedded by the server layer.
	source, target, skill := extractGovFields(task)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO a2a_tasks
			(task_id, context_id, skill, source_agent_id, target_agent_id,
			 state, created_at_ms, updated_at_ms, final_task_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.TaskID, task.ContextID, skill, source, target,
		string(task.Status.State), nowMs, nowMs, taskJSON,
	)
	if err != nil {
		return fmt.Errorf("store: insert task: %w", err)
	}
	return nil
}

// GetTask loads a task by ID, deserializing the full JSON blob.
func (s *SQLiteTaskStore) GetTask(ctx context.Context, taskID string) (*a2a.Task, error) {
	var blob []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT final_task_json FROM a2a_tasks WHERE task_id = ?`, taskID,
	).Scan(&blob)
	if err == sql.ErrNoRows {
		return nil, a2a.NewError(a2a.CodeTaskNotFound, fmt.Sprintf("task %q not found", taskID))
	}
	if err != nil {
		return nil, fmt.Errorf("store: get task: %w", err)
	}

	var task a2a.Task
	if err := json.Unmarshal(blob, &task); err != nil {
		return nil, fmt.Errorf("store: unmarshal task: %w", err)
	}
	return &task, nil
}

// UpdateTaskStatus updates the task's status and re-serializes the full JSON.
func (s *SQLiteTaskStore) UpdateTaskStatus(ctx context.Context, taskID string, status a2a.TaskStatus) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	task.Status = status
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("store: marshal task: %w", err)
	}

	nowMs := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx,
		`UPDATE a2a_tasks SET state = ?, updated_at_ms = ?, final_task_json = ? WHERE task_id = ?`,
		string(status.State), nowMs, taskJSON, taskID,
	)
	if err != nil {
		return fmt.Errorf("store: update status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return a2a.NewError(a2a.CodeTaskNotFound, fmt.Sprintf("task %q not found", taskID))
	}
	return nil
}

// AddArtifact appends an artifact to the task and re-serializes.
func (s *SQLiteTaskStore) AddArtifact(ctx context.Context, taskID string, artifact a2a.Artifact) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	task.Artifacts = append(task.Artifacts, artifact)
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("store: marshal task: %w", err)
	}

	nowMs := time.Now().UnixMilli()
	_, err = s.db.ExecContext(ctx,
		`UPDATE a2a_tasks SET updated_at_ms = ?, final_task_json = ? WHERE task_id = ?`,
		nowMs, taskJSON, taskID,
	)
	if err != nil {
		return fmt.Errorf("store: add artifact: %w", err)
	}
	return nil
}

// AddHistory appends a message to the task's history and re-serializes.
func (s *SQLiteTaskStore) AddHistory(ctx context.Context, taskID string, msg a2a.Message) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}

	task.History = append(task.History, msg)
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("store: marshal task: %w", err)
	}

	nowMs := time.Now().UnixMilli()
	_, err = s.db.ExecContext(ctx,
		`UPDATE a2a_tasks SET updated_at_ms = ?, final_task_json = ? WHERE task_id = ?`,
		nowMs, taskJSON, taskID,
	)
	if err != nil {
		return fmt.Errorf("store: add history: %w", err)
	}
	return nil
}

// ListTasks returns tasks matching the given filter criteria.
func (s *SQLiteTaskStore) ListTasks(ctx context.Context, filter server.TaskFilter) ([]*a2a.Task, error) {
	var (
		clauses []string
		args    []interface{}
	)

	if filter.SourceAgentID != "" {
		clauses = append(clauses, "source_agent_id = ?")
		args = append(args, filter.SourceAgentID)
	}
	if filter.TargetAgentID != "" {
		clauses = append(clauses, "target_agent_id = ?")
		args = append(args, filter.TargetAgentID)
	}
	if filter.State != "" {
		clauses = append(clauses, "state = ?")
		args = append(args, string(filter.State))
	}
	if !filter.Since.IsZero() {
		clauses = append(clauses, "created_at_ms >= ?")
		args = append(args, filter.Since.UnixMilli())
	}

	query := "SELECT final_task_json FROM a2a_tasks"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at_ms DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*a2a.Task
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return nil, fmt.Errorf("store: scan task: %w", err)
		}
		var task a2a.Task
		if err := json.Unmarshal(blob, &task); err != nil {
			return nil, fmt.Errorf("store: unmarshal task: %w", err)
		}
		tasks = append(tasks, &task)
	}
	return tasks, rows.Err()
}

// AddTaskEvent appends a sequenced event to the a2a_task_events table.
// The sequence number is auto-assigned as max(seq)+1 for the given task.
func (s *SQLiteTaskStore) AddTaskEvent(ctx context.Context, taskID string, kind string, payload json.RawMessage) error {
	nowMs := time.Now().UnixMilli()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO a2a_task_events (task_id, seq, kind, at_ms, payload_json)
		 VALUES (?, COALESCE((SELECT MAX(seq) FROM a2a_task_events WHERE task_id = ?), 0) + 1, ?, ?, ?)`,
		taskID, taskID, kind, nowMs, payload,
	)
	if err != nil {
		return fmt.Errorf("store: add task event: %w", err)
	}
	return nil
}

// TaskEvent is a single event in a task's event log.
type TaskEvent struct {
	TaskID  string          `json:"taskId"`
	Seq     int64           `json:"seq"`
	Kind    string          `json:"kind"`
	AtMs    int64           `json:"atMs"`
	Payload json.RawMessage `json:"payload"`
}

// ListTaskEvents returns all events for a task, ordered by sequence.
func (s *SQLiteTaskStore) ListTaskEvents(ctx context.Context, taskID string) ([]TaskEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT task_id, seq, kind, at_ms, payload_json
		 FROM a2a_task_events WHERE task_id = ? ORDER BY seq`, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: list events: %w", err)
	}
	defer rows.Close()

	var events []TaskEvent
	for rows.Next() {
		var e TaskEvent
		if err := rows.Scan(&e.TaskID, &e.Seq, &e.Kind, &e.AtMs, &e.Payload); err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// SetPushConfig stores push notification configuration for a task.
func (s *SQLiteTaskStore) SetPushConfig(ctx context.Context, taskID string, url string, authJSON []byte, token string) error {
	nowMs := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO a2a_task_push_config (task_id, url, auth_json, token, created_at_ms)
		 VALUES (?, ?, ?, ?, ?)`,
		taskID, url, authJSON, token, nowMs,
	)
	if err != nil {
		return fmt.Errorf("store: set push config: %w", err)
	}
	return nil
}

// PushConfig holds push notification configuration for a task.
type PushConfig struct {
	TaskID    string `json:"taskId"`
	URL       string `json:"url"`
	AuthJSON  []byte `json:"authJson,omitempty"`
	Token     string `json:"token"`
	CreatedMs int64  `json:"createdAtMs"`
}

// GetPushConfig retrieves the push notification configuration for a task.
func (s *SQLiteTaskStore) GetPushConfig(ctx context.Context, taskID string) (*PushConfig, error) {
	var pc PushConfig
	err := s.db.QueryRowContext(ctx,
		`SELECT task_id, url, auth_json, token, created_at_ms
		 FROM a2a_task_push_config WHERE task_id = ?`, taskID,
	).Scan(&pc.TaskID, &pc.URL, &pc.AuthJSON, &pc.Token, &pc.CreatedMs)
	if err == sql.ErrNoRows {
		return nil, a2a.NewError(a2a.CodeTaskNotFound, fmt.Sprintf("no push config for task %q", taskID))
	}
	if err != nil {
		return nil, fmt.Errorf("store: get push config: %w", err)
	}
	return &pc, nil
}

// extractGovFields extracts governance extension fields from a task, if present.
func extractGovFields(task *a2a.Task) (source, target, skill string) {
	if task.Extensions == nil {
		return "", "", ""
	}
	var ext struct {
		Gov *a2a.GovernanceExtension `json:"sh.nexus.nexus.governance/v1"`
	}
	if err := json.Unmarshal(task.Extensions, &ext); err != nil || ext.Gov == nil {
		return "", "", ""
	}
	return ext.Gov.SourceAgentID, ext.Gov.TargetAgentID, ""
}
