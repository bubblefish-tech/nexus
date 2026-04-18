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

package tasks_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a/registry"
	"github.com/BubbleFish-Nexus/internal/tasks"
	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *tasks.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := registry.InitSchema(db); err != nil {
		t.Fatalf("InitSchema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return tasks.NewStore(db)
}

func TestNewID_HasPrefix(t *testing.T) {
	id := tasks.NewID()
	if !strings.HasPrefix(id, tasks.IDPrefix) {
		t.Fatalf("NewID = %q, missing prefix %q", id, tasks.IDPrefix)
	}
}

func TestCreate_AssignsIDAndDefaultsSubmitted(t *testing.T) {
	s := newTestStore(t)
	out, err := s.Create(context.Background(), tasks.Task{AgentID: "a1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !strings.HasPrefix(out.TaskID, tasks.IDPrefix) {
		t.Fatalf("TaskID = %q", out.TaskID)
	}
	if out.State != tasks.StateSubmitted {
		t.Fatalf("State = %q, want submitted", out.State)
	}
}

func TestCreate_RejectsEmptyAgentID(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(context.Background(), tasks.Task{})
	if err == nil {
		t.Fatal("expected error for empty agent_id")
	}
}

func TestCreate_RejectsInvalidState(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(context.Background(), tasks.Task{AgentID: "a", State: "exploding"})
	if !errors.Is(err, tasks.ErrInvalidState) {
		t.Fatalf("err = %v, want ErrInvalidState", err)
	}
}

func TestCreate_RejectsInvalidJSONInput(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Create(context.Background(), tasks.Task{
		AgentID: "a", Input: json.RawMessage(`{not json`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON input")
	}
}

func TestCreate_RoundtripsAllFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	parent, _ := s.Create(ctx, tasks.Task{AgentID: "a1", Capability: "parent_cap"})
	out, err := s.Create(ctx, tasks.Task{
		AgentID:      "a1",
		ParentTaskID: parent.TaskID,
		Capability:   "nexus_write",
		Input:        json.RawMessage(`{"memory":"hello"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, out.TaskID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ParentTaskID != parent.TaskID {
		t.Fatalf("ParentTaskID = %q, want %q", got.ParentTaskID, parent.TaskID)
	}
	if got.Capability != "nexus_write" {
		t.Fatalf("Capability = %q", got.Capability)
	}
	if string(got.Input) != `{"memory":"hello"}` {
		t.Fatalf("Input = %q", got.Input)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Get(context.Background(), "tsk_nope")
	if !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestList_FilterByAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, a := range []string{"a", "a", "b"} {
		_, _ = s.Create(ctx, tasks.Task{AgentID: a})
	}
	got, _ := s.List(ctx, tasks.ListFilter{AgentID: "a"})
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
}

func TestList_FilterByState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t1, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	_, _ = s.Create(ctx, tasks.Task{AgentID: "a"})
	_, _ = s.Update(ctx, t1.TaskID, tasks.UpdateInput{State: tasks.StateWorking})
	got, _ := s.List(ctx, tasks.ListFilter{State: tasks.StateWorking})
	if len(got) != 1 {
		t.Fatalf("got %d working, want 1", len(got))
	}
}

func TestList_TopLevelOnly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	parent, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	_, _ = s.Create(ctx, tasks.Task{AgentID: "a", ParentTaskID: parent.TaskID})
	got, _ := s.List(ctx, tasks.ListFilter{TopLevelOnly: true})
	if len(got) != 1 {
		t.Fatalf("got %d top-level, want 1", len(got))
	}
	if got[0].TaskID != parent.TaskID {
		t.Fatalf("got %q, want parent %q", got[0].TaskID, parent.TaskID)
	}
}

func TestList_FilterByParent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	parent, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	_, _ = s.Create(ctx, tasks.Task{AgentID: "a", ParentTaskID: parent.TaskID})
	_, _ = s.Create(ctx, tasks.Task{AgentID: "a", ParentTaskID: parent.TaskID})
	got, _ := s.List(ctx, tasks.ListFilter{ParentTaskID: parent.TaskID})
	if len(got) != 2 {
		t.Fatalf("got %d children, want 2", len(got))
	}
}

func TestUpdate_SubmittedToWorking(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	got, err := s.Update(ctx, t0.TaskID, tasks.UpdateInput{State: tasks.StateWorking})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.State != tasks.StateWorking {
		t.Fatalf("State = %q", got.State)
	}
	if got.CompletedAt != nil {
		t.Fatalf("CompletedAt set prematurely: %v", got.CompletedAt)
	}
}

func TestUpdate_ToCompletedSetsCompletedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	before := time.Now().Add(-1 * time.Millisecond)
	got, err := s.Update(ctx, t0.TaskID, tasks.UpdateInput{
		State:  tasks.StateCompleted,
		Output: json.RawMessage(`{"payload_id":"abc"}`),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.CompletedAt == nil {
		t.Fatal("CompletedAt not set on terminal transition")
	}
	if got.CompletedAt.Before(before) {
		t.Fatalf("CompletedAt = %v, want after %v", got.CompletedAt, before)
	}
	if string(got.Output) != `{"payload_id":"abc"}` {
		t.Fatalf("Output = %q", got.Output)
	}
}

func TestUpdate_FromTerminalFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	_, _ = s.Update(ctx, t0.TaskID, tasks.UpdateInput{State: tasks.StateCompleted})
	_, err := s.Update(ctx, t0.TaskID, tasks.UpdateInput{State: tasks.StateWorking})
	if !errors.Is(err, tasks.ErrTerminalState) {
		t.Fatalf("err = %v, want ErrTerminalState", err)
	}
}

func TestUpdate_RejectsInvalidState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	_, err := s.Update(ctx, t0.TaskID, tasks.UpdateInput{State: "haywire"})
	if !errors.Is(err, tasks.ErrInvalidState) {
		t.Fatalf("err = %v, want ErrInvalidState", err)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.Update(context.Background(), "tsk_nope", tasks.UpdateInput{State: tasks.StateWorking})
	if !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAppendEvent_RoundtripsPayload(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	payload := json.RawMessage(`{"step":"search","matches":3}`)
	e, err := s.AppendEvent(ctx, tasks.TaskEvent{
		TaskID:    t0.TaskID,
		EventType: tasks.EventTypeProgress,
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if !strings.HasPrefix(e.EventID, tasks.EventIDPrefix) {
		t.Fatalf("EventID = %q", e.EventID)
	}
	if string(e.Payload) != string(payload) {
		t.Fatalf("Payload = %q, want %q", e.Payload, payload)
	}
}

func TestAppendEvent_RejectsMissingTask(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AppendEvent(context.Background(), tasks.TaskEvent{
		TaskID: "tsk_nope", EventType: tasks.EventTypeStarted,
	})
	if !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAppendEvent_RejectsEmptyType(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	_, err := s.AppendEvent(ctx, tasks.TaskEvent{TaskID: t0.TaskID})
	if err == nil {
		t.Fatal("expected error for empty event_type")
	}
}

func TestListEvents_OrderedChronologically(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	_, _ = s.AppendEvent(ctx, tasks.TaskEvent{TaskID: t0.TaskID, EventType: tasks.EventTypeCreated})
	time.Sleep(2 * time.Millisecond)
	_, _ = s.AppendEvent(ctx, tasks.TaskEvent{TaskID: t0.TaskID, EventType: tasks.EventTypeStarted})
	time.Sleep(2 * time.Millisecond)
	_, _ = s.AppendEvent(ctx, tasks.TaskEvent{TaskID: t0.TaskID, EventType: tasks.EventTypeCompleted})

	events, err := s.ListEvents(ctx, t0.TaskID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	want := []string{tasks.EventTypeCreated, tasks.EventTypeStarted, tasks.EventTypeCompleted}
	for i, w := range want {
		if events[i].EventType != w {
			t.Fatalf("events[%d].EventType = %q, want %q", i, events[i].EventType, w)
		}
	}
}

func TestListEvents_EmptyForNewTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	t0, _ := s.Create(ctx, tasks.Task{AgentID: "a"})
	events, err := s.ListEvents(ctx, t0.TaskID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0", len(events))
	}
}

func TestListEvents_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.ListEvents(context.Background(), "tsk_nope")
	if !errors.Is(err, tasks.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestList_Limit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for range 5 {
		_, _ = s.Create(ctx, tasks.Task{AgentID: "a"})
	}
	got, err := s.List(ctx, tasks.ListFilter{Limit: 3})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3 (limit)", len(got))
	}
}

func TestIsTerminal_TruthTable(t *testing.T) {
	cases := []struct {
		state    string
		terminal bool
	}{
		{tasks.StateSubmitted, false},
		{tasks.StateWorking, false},
		{tasks.StateCompleted, true},
		{tasks.StateFailed, true},
		{tasks.StateCanceled, true},
		{"unknown", false},
	}
	for _, tc := range cases {
		if got := tasks.IsTerminal(tc.state); got != tc.terminal {
			t.Errorf("IsTerminal(%q) = %v, want %v", tc.state, got, tc.terminal)
		}
	}
}
