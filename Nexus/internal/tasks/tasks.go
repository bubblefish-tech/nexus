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

// Package tasks stores durable governed-task records and an append-only event
// log per task. A task follows the state machine submitted → working →
// completed | failed | canceled, with terminal states being absorbing. Tasks
// can be nested via parent_task_id to model agent chain-of-thought and
// delegation. The control plane (MT.2+) drives state transitions; MT.1 is
// storage-only.
package tasks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// IDPrefix is the identifier prefix for task IDs. The existing A2A protocol
// uses the same "tsk_" prefix for its a2a_tasks table (see internal/a2a/ids.go);
// that is a different table with different semantics in the A2A protocol layer,
// while this package's tasks table is the control-plane view owned by MT.1+.
const IDPrefix = "tsk_"

// Task lifecycle states. Terminal states (completed, failed, canceled) are
// absorbing — further transitions return ErrTerminalState.
const (
	StateSubmitted = "submitted"
	StateWorking   = "working"
	StateCompleted = "completed"
	StateFailed    = "failed"
	StateCanceled  = "canceled"
)

// Errors returned by Store.
var (
	ErrNotFound       = errors.New("tasks: task not found")
	ErrInvalidState   = errors.New("tasks: invalid state")
	ErrTerminalState  = errors.New("tasks: task already in terminal state")
)

// Task is a durable governed-task record. Input and Output are opaque JSON
// blobs; policy evaluation and capability-specific handlers interpret them.
// ParentTaskID is the empty string for top-level tasks.
type Task struct {
	TaskID       string
	AgentID      string
	ParentTaskID string
	State        string
	Capability   string
	Input        json.RawMessage
	Output       json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CompletedAt  *time.Time
}

// IsTerminal reports whether state is absorbing.
func IsTerminal(state string) bool {
	switch state {
	case StateCompleted, StateFailed, StateCanceled:
		return true
	}
	return false
}

// IsValidState reports whether state is a recognized task state.
func IsValidState(state string) bool {
	switch state {
	case StateSubmitted, StateWorking, StateCompleted, StateFailed, StateCanceled:
		return true
	}
	return false
}

// Store persists Tasks and TaskEvents against a shared *sql.DB. The schema
// must already be initialized — typically by registry.InitSchema.
type Store struct {
	db *sql.DB
}

// NewStore wraps db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// NewID generates a fresh task_id with the "tsk_" prefix.
func NewID() string {
	return IDPrefix + newULID()
}

// Create inserts t. AgentID is required. If TaskID is empty a fresh ID is
// generated; if State is empty it defaults to submitted.
func (s *Store) Create(ctx context.Context, t Task) (Task, error) {
	if t.AgentID == "" {
		return Task{}, fmt.Errorf("tasks: agent_id required")
	}
	if t.State == "" {
		t.State = StateSubmitted
	}
	if !IsValidState(t.State) {
		return Task{}, fmt.Errorf("%w: %q", ErrInvalidState, t.State)
	}
	if len(t.Input) > 0 && !json.Valid(t.Input) {
		return Task{}, fmt.Errorf("tasks: input is not valid JSON")
	}
	if len(t.Output) > 0 && !json.Valid(t.Output) {
		return Task{}, fmt.Errorf("tasks: output is not valid JSON")
	}
	if t.TaskID == "" {
		t.TaskID = NewID()
	}
	now := time.Now()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}

	var inputStr, outputStr *string
	if len(t.Input) > 0 {
		v := string(t.Input)
		inputStr = &v
	}
	if len(t.Output) > 0 {
		v := string(t.Output)
		outputStr = &v
	}
	var parent *string
	if t.ParentTaskID != "" {
		v := t.ParentTaskID
		parent = &v
	}
	var completed *int64
	if t.CompletedAt != nil {
		v := t.CompletedAt.UnixMilli()
		completed = &v
	}
	var capability *string
	if t.Capability != "" {
		v := t.Capability
		capability = &v
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks (
			task_id, agent_id, parent_task_id, state, capability,
			input_json, output_json,
			created_at_ms, updated_at_ms, completed_at_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.TaskID, t.AgentID, parent, t.State, capability,
		inputStr, outputStr,
		t.CreatedAt.UnixMilli(), t.UpdatedAt.UnixMilli(), completed,
	)
	if err != nil {
		return Task{}, fmt.Errorf("tasks: insert: %w", err)
	}
	return t, nil
}

// Get retrieves a task by ID.
func (s *Store) Get(ctx context.Context, taskID string) (*Task, error) {
	row := s.db.QueryRowContext(ctx, selectCols+` FROM tasks WHERE task_id = ?`, taskID)
	t, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// ListFilter narrows List results.
type ListFilter struct {
	AgentID      string
	State        string
	ParentTaskID string // use "" for no filter; use the sentinel returned by NoParent() to restrict to top-level tasks
	TopLevelOnly bool   // if true, only return tasks where parent_task_id IS NULL
	Limit        int    // max rows to return; 0 = no cap
}

// List returns tasks matching filter, ordered by created_at_ms DESC.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]Task, error) {
	query := selectCols + ` FROM tasks WHERE 1=1`
	var args []any
	if filter.AgentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, filter.AgentID)
	}
	if filter.State != "" {
		query += ` AND state = ?`
		args = append(args, filter.State)
	}
	if filter.TopLevelOnly {
		query += ` AND parent_task_id IS NULL`
	} else if filter.ParentTaskID != "" {
		query += ` AND parent_task_id = ?`
		args = append(args, filter.ParentTaskID)
	}
	query += ` ORDER BY created_at_ms DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("tasks: list: %w", err)
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		t, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// UpdateInput carries arguments to Update.
type UpdateInput struct {
	State  string          // required; must be a valid state
	Output json.RawMessage // optional; replaces output_json if non-nil
}

// Update transitions a task to the target state. Transitions from any non-
// terminal state to any other valid state are permitted; attempting to
// transition from a terminal state returns ErrTerminalState. completed_at_ms
// is automatically set when transitioning to a terminal state.
func (s *Store) Update(ctx context.Context, taskID string, in UpdateInput) (*Task, error) {
	if !IsValidState(in.State) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidState, in.State)
	}
	if len(in.Output) > 0 && !json.Valid(in.Output) {
		return nil, fmt.Errorf("tasks: output is not valid JSON")
	}

	current, err := s.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if IsTerminal(current.State) {
		return nil, ErrTerminalState
	}

	now := time.Now()
	var completed *int64
	if IsTerminal(in.State) {
		v := now.UnixMilli()
		completed = &v
	}

	var outputStr sql.NullString
	if len(in.Output) > 0 {
		outputStr = sql.NullString{String: string(in.Output), Valid: true}
	} else if len(current.Output) > 0 {
		outputStr = sql.NullString{String: string(current.Output), Valid: true}
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, output_json = ?, updated_at_ms = ?,
		    completed_at_ms = COALESCE(?, completed_at_ms)
		WHERE task_id = ?`,
		in.State, outputStr, now.UnixMilli(), completed, taskID)
	if err != nil {
		return nil, fmt.Errorf("tasks: update: %w", err)
	}
	return s.Get(ctx, taskID)
}

const selectCols = `SELECT task_id, agent_id, parent_task_id, state, capability,
	input_json, output_json, created_at_ms, updated_at_ms, completed_at_ms`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(r rowScanner) (*Task, error) {
	var (
		t           Task
		parent      sql.NullString
		capability  sql.NullString
		input       sql.NullString
		output      sql.NullString
		createdMs   int64
		updatedMs   int64
		completedMs sql.NullInt64
	)
	err := r.Scan(
		&t.TaskID, &t.AgentID, &parent, &t.State, &capability,
		&input, &output,
		&createdMs, &updatedMs, &completedMs,
	)
	if err != nil {
		return nil, err
	}
	if parent.Valid {
		t.ParentTaskID = parent.String
	}
	if capability.Valid {
		t.Capability = capability.String
	}
	if input.Valid {
		t.Input = json.RawMessage(input.String)
	}
	if output.Valid {
		t.Output = json.RawMessage(output.String)
	}
	t.CreatedAt = time.UnixMilli(createdMs)
	t.UpdatedAt = time.UnixMilli(updatedMs)
	if completedMs.Valid {
		v := time.UnixMilli(completedMs.Int64)
		t.CompletedAt = &v
	}
	return &t, nil
}

var entropyPool = sync.Pool{
	New: func() any { return ulid.Monotonic(rand.Reader, 0) },
}

func newULID() string {
	e := entropyPool.Get().(*ulid.MonotonicEntropy)
	defer entropyPool.Put(e)
	id, err := ulid.New(ulid.Timestamp(time.Now()), e)
	if err != nil {
		id = ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader)
	}
	return id.String()
}
