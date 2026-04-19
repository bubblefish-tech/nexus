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
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/server"
	_ "modernc.org/sqlite"
)

// newTestStore creates a temporary SQLite task store for testing.
func newTestStore(t *testing.T) *SQLiteTaskStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteTaskStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteTaskStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// makeTask creates a minimal task for testing.
func makeTask(t *testing.T) *a2a.Task {
	t.Helper()
	task := a2a.NewTask()
	task.ContextID = a2a.NewContextID()
	return &task
}

func TestCreateAndGetTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.TaskID != task.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, task.TaskID)
	}
	if got.ContextID != task.ContextID {
		t.Errorf("ContextID = %q, want %q", got.ContextID, task.ContextID)
	}
	if got.Status.State != a2a.TaskStateSubmitted {
		t.Errorf("State = %q, want %q", got.Status.State, a2a.TaskStateSubmitted)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetTask(ctx, "tsk_NONEXISTENT0000000000000")
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
	a2aErr, ok := err.(*a2a.Error)
	if !ok {
		t.Fatalf("expected *a2a.Error, got %T", err)
	}
	if a2aErr.Code != a2a.CodeTaskNotFound {
		t.Errorf("Code = %d, want %d", a2aErr.Code, a2a.CodeTaskNotFound)
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	newStatus := a2a.TaskStatus{
		State:     a2a.TaskStateWorking,
		Timestamp: a2a.Now(),
	}
	if err := s.UpdateTaskStatus(ctx, task.TaskID, newStatus); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status.State != a2a.TaskStateWorking {
		t.Errorf("State = %q, want %q", got.Status.State, a2a.TaskStateWorking)
	}
}

func TestUpdateTaskStatus_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	err := s.UpdateTaskStatus(ctx, "tsk_NONEXISTENT0000000000000", a2a.TaskStatus{
		State:     a2a.TaskStateWorking,
		Timestamp: a2a.Now(),
	})
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestAddArtifact(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	art := a2a.NewArtifact("test-artifact", a2a.NewTextPart("hello"))
	if err := s.AddArtifact(ctx, task.TaskID, art); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}

	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("len(Artifacts) = %d, want 1", len(got.Artifacts))
	}
	if got.Artifacts[0].Name != "test-artifact" {
		t.Errorf("Artifact.Name = %q, want %q", got.Artifacts[0].Name, "test-artifact")
	}
}

func TestAddArtifact_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	art := a2a.NewArtifact("x")
	err := s.AddArtifact(ctx, "tsk_NONEXISTENT0000000000000", art)
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestAddHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	if err := s.AddHistory(ctx, task.TaskID, msg); err != nil {
		t.Fatalf("AddHistory: %v", err)
	}

	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.History) != 1 {
		t.Fatalf("len(History) = %d, want 1", len(got.History))
	}
	if got.History[0].MessageID != msg.MessageID {
		t.Errorf("History[0].MessageID = %q, want %q", got.History[0].MessageID, msg.MessageID)
	}
}

func TestAddHistory_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("x"))
	err := s.AddHistory(ctx, "tsk_NONEXISTENT0000000000000", msg)
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestAddMultipleArtifacts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	for i := 0; i < 5; i++ {
		art := a2a.NewArtifact(fmt.Sprintf("art-%d", i))
		if err := s.AddArtifact(ctx, task.TaskID, art); err != nil {
			t.Fatalf("AddArtifact(%d): %v", i, err)
		}
	}

	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.Artifacts) != 5 {
		t.Errorf("len(Artifacts) = %d, want 5", len(got.Artifacts))
	}
}

func TestAddMultipleHistory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	for i := 0; i < 5; i++ {
		msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart(fmt.Sprintf("msg-%d", i)))
		if err := s.AddHistory(ctx, task.TaskID, msg); err != nil {
			t.Fatalf("AddHistory(%d): %v", i, err)
		}
	}

	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if len(got.History) != 5 {
		t.Errorf("len(History) = %d, want 5", len(got.History))
	}
}

func TestListTasks_NoFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		task := makeTask(t)
		if err := s.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask(%d): %v", i, err)
		}
	}

	tasks, err := s.ListTasks(ctx, server.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("len(tasks) = %d, want 3", len(tasks))
	}
}

func TestListTasks_FilterByState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task1 := makeTask(t)
	if err := s.CreateTask(ctx, task1); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	task2 := makeTask(t)
	if err := s.CreateTask(ctx, task2); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskStatus(ctx, task2.TaskID, a2a.TaskStatus{
		State:     a2a.TaskStateCompleted,
		Timestamp: a2a.Now(),
	}); err != nil {
		t.Fatalf("UpdateTaskStatus: %v", err)
	}

	tasks, err := s.ListTasks(ctx, server.TaskFilter{State: a2a.TaskStateCompleted})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(tasks))
	}
	if len(tasks) > 0 && tasks[0].TaskID != task2.TaskID {
		t.Errorf("TaskID = %q, want %q", tasks[0].TaskID, task2.TaskID)
	}
}

func TestListTasks_FilterByLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		task := makeTask(t)
		if err := s.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask(%d): %v", i, err)
		}
	}

	tasks, err := s.ListTasks(ctx, server.TaskFilter{Limit: 3})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("len(tasks) = %d, want 3", len(tasks))
	}
}

func TestListTasks_FilterBySince(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task1 := makeTask(t)
	if err := s.CreateTask(ctx, task1); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Tasks created just now should be after a time in the past.
	tasks, err := s.ListTasks(ctx, server.TaskFilter{Since: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(tasks))
	}

	// Tasks created just now should not be after a time in the future.
	tasks, err = s.ListTasks(ctx, server.TaskFilter{Since: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(tasks))
	}
}

func TestListTasks_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	tasks, err := s.ListTasks(ctx, server.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(tasks))
	}
}

func TestTaskEvents_SequenceNumbering(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	for i := 0; i < 5; i++ {
		payload, _ := json.Marshal(map[string]int{"step": i})
		if err := s.AddTaskEvent(ctx, task.TaskID, "status_change", payload); err != nil {
			t.Fatalf("AddTaskEvent(%d): %v", i, err)
		}
	}

	events, err := s.ListTaskEvents(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("len(events) = %d, want 5", len(events))
	}

	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Errorf("events[%d].Seq = %d, want %d", i, e.Seq, i+1)
		}
		if e.Kind != "status_change" {
			t.Errorf("events[%d].Kind = %q, want %q", i, e.Kind, "status_change")
		}
		if e.TaskID != task.TaskID {
			t.Errorf("events[%d].TaskID = %q, want %q", i, e.TaskID, task.TaskID)
		}
	}
}

func TestTaskEvents_Empty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	events, err := s.ListTaskEvents(ctx, "tsk_NONEXISTENT0000000000000")
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("len(events) = %d, want 0", len(events))
	}
}

func TestPushConfig_SetAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	authJSON := []byte(`{"type":"bearer","token":"secret"}`)
	if err := s.SetPushConfig(ctx, task.TaskID, "https://example.com/callback", authJSON, "tok_123"); err != nil {
		t.Fatalf("SetPushConfig: %v", err)
	}

	pc, err := s.GetPushConfig(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetPushConfig: %v", err)
	}
	if pc.URL != "https://example.com/callback" {
		t.Errorf("URL = %q, want %q", pc.URL, "https://example.com/callback")
	}
	if pc.Token != "tok_123" {
		t.Errorf("Token = %q, want %q", pc.Token, "tok_123")
	}
	if string(pc.AuthJSON) != string(authJSON) {
		t.Errorf("AuthJSON = %q, want %q", pc.AuthJSON, authJSON)
	}
}

func TestPushConfig_NotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.GetPushConfig(ctx, "tsk_NONEXISTENT0000000000000")
	if err == nil {
		t.Fatal("expected error for non-existent push config")
	}
}

func TestPushConfig_Overwrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.SetPushConfig(ctx, task.TaskID, "https://old.example.com", nil, "tok_old"); err != nil {
		t.Fatalf("SetPushConfig: %v", err)
	}
	if err := s.SetPushConfig(ctx, task.TaskID, "https://new.example.com", nil, "tok_new"); err != nil {
		t.Fatalf("SetPushConfig: %v", err)
	}

	pc, err := s.GetPushConfig(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetPushConfig: %v", err)
	}
	if pc.URL != "https://new.example.com" {
		t.Errorf("URL = %q, want %q", pc.URL, "https://new.example.com")
	}
	if pc.Token != "tok_new" {
		t.Errorf("Token = %q, want %q", pc.Token, "tok_new")
	}
}

func TestConcurrency_10Goroutines(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 30) // 10 status + 10 artifact + 10 history

	for i := 0; i < 10; i++ {
		wg.Add(3)
		i := i

		go func() {
			defer wg.Done()
			status := a2a.TaskStatus{
				State:     a2a.TaskStateWorking,
				Timestamp: a2a.Now(),
			}
			if err := s.UpdateTaskStatus(ctx, task.TaskID, status); err != nil {
				errs <- fmt.Errorf("UpdateTaskStatus(%d): %w", i, err)
			}
		}()

		go func() {
			defer wg.Done()
			art := a2a.NewArtifact(fmt.Sprintf("artifact-%d", i))
			if err := s.AddArtifact(ctx, task.TaskID, art); err != nil {
				errs <- fmt.Errorf("AddArtifact(%d): %w", i, err)
			}
		}()

		go func() {
			defer wg.Done()
			msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart(fmt.Sprintf("msg-%d", i)))
			if err := s.AddHistory(ctx, task.TaskID, msg); err != nil {
				errs <- fmt.Errorf("AddHistory(%d): %w", i, err)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	// Verify the task can still be retrieved.
	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask after concurrent mutations: %v", err)
	}
	if got.TaskID != task.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, task.TaskID)
	}
}

func TestWALMode(t *testing.T) {
	s := newTestStore(t)

	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}

	var sync string
	if err := s.db.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	// synchronous=FULL is value 2.
	if sync != "2" {
		t.Errorf("synchronous = %q, want %q", sync, "2")
	}
}

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	// Run migrations twice; second call should be a no-op.
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate(1): %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate(2): %v", err)
	}
}

func TestNewSQLiteTaskStoreFromDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	s := NewSQLiteTaskStoreFromDB(db)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.TaskID != task.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, task.TaskID)
	}
}

func TestCreateTask_DuplicateID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Second create with same ID should fail.
	err := s.CreateTask(ctx, task)
	if err == nil {
		t.Fatal("expected error on duplicate task ID")
	}
}

func TestStatusTransitions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	transitions := []a2a.TaskState{
		a2a.TaskStateWorking,
		a2a.TaskStateInputRequired,
		a2a.TaskStateWorking,
		a2a.TaskStateCompleted,
	}

	for _, state := range transitions {
		status := a2a.TaskStatus{State: state, Timestamp: a2a.Now()}
		if err := s.UpdateTaskStatus(ctx, task.TaskID, status); err != nil {
			t.Fatalf("UpdateTaskStatus(%s): %v", state, err)
		}
		got, err := s.GetTask(ctx, task.TaskID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if got.Status.State != state {
			t.Errorf("State = %q, want %q", got.Status.State, state)
		}
	}
}

func TestTaskRoundTrip_WithMetadata(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)
	task.Metadata = json.RawMessage(`{"custom":"field","number":42}`)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	if string(got.Metadata) != string(task.Metadata) {
		t.Errorf("Metadata = %s, want %s", got.Metadata, task.Metadata)
	}
}

func TestTaskEvents_DifferentKinds(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	kinds := []string{"status_change", "artifact_added", "history_added", "governance_decision"}
	for _, kind := range kinds {
		payload, _ := json.Marshal(map[string]string{"kind": kind})
		if err := s.AddTaskEvent(ctx, task.TaskID, kind, payload); err != nil {
			t.Fatalf("AddTaskEvent(%s): %v", kind, err)
		}
	}

	events, err := s.ListTaskEvents(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	if len(events) != len(kinds) {
		t.Fatalf("len(events) = %d, want %d", len(events), len(kinds))
	}
	for i, e := range events {
		if e.Kind != kinds[i] {
			t.Errorf("events[%d].Kind = %q, want %q", i, e.Kind, kinds[i])
		}
	}
}

func TestListTasks_FilterBySourceAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create a task with known source agent by using extensions.
	task := makeTask(t)
	task.Extensions = json.RawMessage(`{"sh.bubblefish.nexus.governance/v1":{"sourceAgentId":"agent-A","targetAgentId":"agent-B","decision":"allow"}}`)
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	task2 := makeTask(t)
	task2.Extensions = json.RawMessage(`{"sh.bubblefish.nexus.governance/v1":{"sourceAgentId":"agent-C","targetAgentId":"agent-D","decision":"allow"}}`)
	if err := s.CreateTask(ctx, task2); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	tasks, err := s.ListTasks(ctx, server.TaskFilter{SourceAgentID: "agent-A"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(tasks))
	}
}

func TestListTasks_FilterByTargetAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	task := makeTask(t)
	task.Extensions = json.RawMessage(`{"sh.bubblefish.nexus.governance/v1":{"sourceAgentId":"agent-A","targetAgentId":"agent-B","decision":"allow"}}`)
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	tasks, err := s.ListTasks(ctx, server.TaskFilter{TargetAgentID: "agent-B"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("len(tasks) = %d, want 1", len(tasks))
	}

	tasks, err = s.ListTasks(ctx, server.TaskFilter{TargetAgentID: "agent-X"})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(tasks))
	}
}

func TestDB_Method(t *testing.T) {
	s := newTestStore(t)
	if s.DB() == nil {
		t.Fatal("DB() returned nil")
	}
}

func TestConcurrentTaskEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]int{"goroutine": i})
			if err := s.AddTaskEvent(ctx, task.TaskID, "concurrent", payload); err != nil {
				errs <- fmt.Errorf("AddTaskEvent(%d): %w", i, err)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent event error: %v", err)
	}

	events, err := s.ListTaskEvents(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	if len(events) != 10 {
		t.Errorf("len(events) = %d, want 10", len(events))
	}
}

func TestCreateMultipleTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	ids := make(map[string]bool)
	for i := 0; i < 20; i++ {
		task := makeTask(t)
		if err := s.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask(%d): %v", i, err)
		}
		ids[task.TaskID] = true
	}

	if len(ids) != 20 {
		t.Errorf("unique IDs = %d, want 20", len(ids))
	}

	tasks, err := s.ListTasks(ctx, server.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 20 {
		t.Errorf("len(tasks) = %d, want 20", len(tasks))
	}
}

func TestTaskPreservesKindField(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := makeTask(t)

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Kind != "task" {
		t.Errorf("Kind = %q, want %q", got.Kind, "task")
	}
}
