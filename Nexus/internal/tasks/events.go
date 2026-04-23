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

package tasks

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

const evtRowInfo = "task-events-row"

// EventIDPrefix is the identifier prefix for task event IDs.
const EventIDPrefix = "evt_"

// Common event types emitted over a task's lifetime.
const (
	EventTypeCreated   = "task.created"
	EventTypeStarted   = "task.started"
	EventTypeProgress  = "task.progress"
	EventTypeCompleted = "task.completed"
	EventTypeFailed    = "task.failed"
	EventTypeCanceled  = "task.canceled"
	EventTypeComment   = "task.comment"
)

// TaskEvent is one entry in a task's append-only event log.
type TaskEvent struct {
	EventID   string
	TaskID    string
	EventType string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// NewEventID generates a fresh event_id with the "evt_" prefix.
func NewEventID() string {
	return EventIDPrefix + newULID()
}

// AppendEvent adds an event to the log for a task. TaskID and EventType are
// required. If the task does not exist, returns ErrNotFound.
func (s *Store) AppendEvent(ctx context.Context, e TaskEvent) (TaskEvent, error) {
	if e.TaskID == "" {
		return TaskEvent{}, fmt.Errorf("tasks: task_id required")
	}
	if e.EventType == "" {
		return TaskEvent{}, fmt.Errorf("tasks: event_type required")
	}
	if len(e.Payload) > 0 && !json.Valid(e.Payload) {
		return TaskEvent{}, fmt.Errorf("tasks: payload is not valid JSON")
	}
	if _, err := s.Get(ctx, e.TaskID); err != nil {
		return TaskEvent{}, err
	}
	if e.EventID == "" {
		e.EventID = NewEventID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}

	if s.mkm != nil && s.mkm.IsEnabled() && len(e.Payload) > 0 {
		subKey := s.mkm.SubKey("nexus-control-key-v1")
		rowKey, err := nexuscrypto.DeriveRowKey(subKey, e.EventID, evtRowInfo)
		if err != nil {
			return TaskEvent{}, fmt.Errorf("tasks: derive event row key: %w", err)
		}
		encPayload, err := nexuscrypto.SealAES256GCM(rowKey, e.Payload, []byte(e.EventID))
		if err != nil {
			return TaskEvent{}, fmt.Errorf("tasks: encrypt event payload: %w", err)
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO task_events (event_id, task_id, event_type, payload_json, created_at_ms,
			                        payload_json_encrypted, encryption_version)
			VALUES (?, ?, ?, NULL, ?, ?, 1)`,
			e.EventID, e.TaskID, e.EventType, e.CreatedAt.UnixMilli(),
			encPayload,
		)
		if err != nil {
			return TaskEvent{}, fmt.Errorf("tasks: append event: %w", err)
		}
	} else {
		var payloadStr *string
		if len(e.Payload) > 0 {
			v := string(e.Payload)
			payloadStr = &v
		}
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO task_events (event_id, task_id, event_type, payload_json, created_at_ms)
			VALUES (?, ?, ?, ?, ?)`,
			e.EventID, e.TaskID, e.EventType, payloadStr, e.CreatedAt.UnixMilli(),
		)
		if err != nil {
			return TaskEvent{}, fmt.Errorf("tasks: append event: %w", err)
		}
	}
	return e, nil
}

// ListEvents returns all events for taskID in chronological order (oldest
// first). Returns ErrNotFound if the task itself does not exist.
func (s *Store) ListEvents(ctx context.Context, taskID string) ([]TaskEvent, error) {
	if _, err := s.Get(ctx, taskID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_id, task_id, event_type, payload_json, created_at_ms,
		       payload_json_encrypted, encryption_version
		FROM task_events
		WHERE task_id = ?
		ORDER BY created_at_ms ASC, event_id ASC`, taskID)
	if err != nil {
		return nil, fmt.Errorf("tasks: list events: %w", err)
	}
	defer rows.Close()

	var (
		out    []TaskEvent
		subKey [32]byte
		subKeyLoaded bool
	)
	if s.mkm != nil && s.mkm.IsEnabled() {
		subKey = s.mkm.SubKey("nexus-control-key-v1")
		subKeyLoaded = true
	}

	for rows.Next() {
		var (
			e              TaskEvent
			payload        sql.NullString
			createdMs      int64
			payloadEncBlob []byte
			encVersion     int64
		)
		if err := rows.Scan(&e.EventID, &e.TaskID, &e.EventType, &payload, &createdMs,
			&payloadEncBlob, &encVersion); err != nil {
			return nil, fmt.Errorf("tasks: scan event: %w", err)
		}
		if encVersion == 1 && subKeyLoaded && len(payloadEncBlob) > 0 {
			rowKey, keyErr := nexuscrypto.DeriveRowKey(subKey, e.EventID, evtRowInfo)
			if keyErr != nil {
				return nil, fmt.Errorf("tasks: derive event row key: %w", keyErr)
			}
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, payloadEncBlob, []byte(e.EventID))
			if decErr != nil {
				return nil, fmt.Errorf("tasks: decrypt event payload: %w", decErr)
			}
			e.Payload = json.RawMessage(plain)
		} else if payload.Valid {
			e.Payload = json.RawMessage(payload.String)
		}
		e.CreatedAt = time.UnixMilli(createdMs)
		out = append(out, e)
	}
	return out, rows.Err()
}
