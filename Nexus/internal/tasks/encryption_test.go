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
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/bubblefish-tech/nexus/internal/tasks"
)

func newMKMTasks(t *testing.T, password string) *crypto.MasterKeyManager {
	t.Helper()
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager(password, saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	return mkm
}

func newEncDBTasks(t *testing.T) *sql.DB {
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
	return db
}

func TestTaskEncryption_InputOutputRoundTrip(t *testing.T) {
	db := newEncDBTasks(t)
	s := tasks.NewStore(db)
	s.SetEncryption(newMKMTasks(t, "taskpw"))
	ctx := context.Background()
	input := json.RawMessage(`{"query":"find all"}`)
	output := json.RawMessage(`{"count":42}`)
	tk, err := s.Create(ctx, tasks.Task{
		AgentID:    "agt_test",
		Capability: "nexus_search",
		Input:      input,
		Output:     output,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Input) != string(input) {
		t.Errorf("Input: got %s, want %s", got.Input, input)
	}
	if string(got.Output) != string(output) {
		t.Errorf("Output: got %s, want %s", got.Output, output)
	}
}

func TestTaskEncryption_PlaintextColumnsNullOnCreate(t *testing.T) {
	db := newEncDBTasks(t)
	s := tasks.NewStore(db)
	s.SetEncryption(newMKMTasks(t, "pw"))
	ctx := context.Background()
	tk, err := s.Create(ctx, tasks.Task{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Input:      json.RawMessage(`{"secret":"input"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	var rawInput sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT input_json FROM tasks WHERE task_id = ?`, tk.TaskID).Scan(&rawInput); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawInput.Valid && rawInput.String != "" {
		t.Errorf("plaintext input_json should be NULL, got %q", rawInput.String)
	}
}

func TestTaskEncryption_WrongKeyFails(t *testing.T) {
	db := newEncDBTasks(t)
	sA := tasks.NewStore(db)
	sA.SetEncryption(newMKMTasks(t, "key-A"))
	ctx := context.Background()
	tk, err := sA.Create(ctx, tasks.Task{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Input:      json.RawMessage(`{"x":1}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sB := tasks.NewStore(db)
	sB.SetEncryption(newMKMTasks(t, "key-B"))
	_, err = sB.Get(ctx, tk.TaskID)
	if err == nil {
		t.Fatal("expected decrypt error with wrong key, got nil")
	}
}

func TestTaskEncryption_BackwardCompat(t *testing.T) {
	db := newEncDBTasks(t)
	sPlain := tasks.NewStore(db)
	ctx := context.Background()
	tk, err := sPlain.Create(ctx, tasks.Task{
		AgentID:    "agt_test",
		Capability: "nexus_read",
		Input:      json.RawMessage(`{"legacy":"true"}`),
	})
	if err != nil {
		t.Fatalf("Create (plaintext): %v", err)
	}
	sEnc := tasks.NewStore(db)
	sEnc.SetEncryption(newMKMTasks(t, "pw"))
	got, err := sEnc.Get(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("Get (encrypted store, old row): %v", err)
	}
	if string(got.Input) != `{"legacy":"true"}` {
		t.Errorf("Input: got %s", got.Input)
	}
}

func TestTaskEncryption_UpdateOutputRoundTrip(t *testing.T) {
	db := newEncDBTasks(t)
	s := tasks.NewStore(db)
	s.SetEncryption(newMKMTasks(t, "updpw"))
	ctx := context.Background()
	tk, err := s.Create(ctx, tasks.Task{
		AgentID:    "agt_test",
		Capability: "nexus_write",
		Input:      json.RawMessage(`{"op":"create"}`),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := s.Update(ctx, tk.TaskID, tasks.UpdateInput{
		State:  tasks.StateCompleted,
		Output: json.RawMessage(`{"result":"success"}`),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if string(updated.Output) != `{"result":"success"}` {
		t.Errorf("Output after Update: got %s", updated.Output)
	}
	// Verify plaintext column is NULL after encrypted update.
	var rawOutput sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT output_json FROM tasks WHERE task_id = ?`, tk.TaskID).Scan(&rawOutput); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawOutput.Valid && rawOutput.String != "" {
		t.Errorf("plaintext output_json should be NULL after encrypted update, got %q", rawOutput.String)
	}
}

func TestTaskEncryption_DisabledMKMNoOp(t *testing.T) {
	saltPath := filepath.Join(t.TempDir(), "crypto.salt")
	mkm, err := crypto.NewMasterKeyManager("", saltPath)
	if err != nil {
		t.Fatalf("NewMasterKeyManager: %v", err)
	}
	if mkm.IsEnabled() {
		t.Skip("NEXUS_PASSWORD env var set; skipping disabled-MKM test")
	}
	db := newEncDBTasks(t)
	s := tasks.NewStore(db)
	s.SetEncryption(mkm)
	ctx := context.Background()
	input := json.RawMessage(`{"plain":"input"}`)
	tk, err := s.Create(ctx, tasks.Task{
		AgentID:    "agt_test",
		Capability: "nexus_read",
		Input:      input,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Input) != string(input) {
		t.Errorf("Input: got %s, want %s", got.Input, input)
	}
}

func TestTaskEncryption_EventPayloadRoundTrip(t *testing.T) {
	db := newEncDBTasks(t)
	s := tasks.NewStore(db)
	s.SetEncryption(newMKMTasks(t, "evtpw"))
	ctx := context.Background()
	tk, err := s.Create(ctx, tasks.Task{
		AgentID:    "agt_test",
		Capability: "nexus_write",
	})
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}
	payload := json.RawMessage(`{"progress":50}`)
	evt, err := s.AppendEvent(ctx, tasks.TaskEvent{
		TaskID:    tk.TaskID,
		EventType: tasks.EventTypeProgress,
		Payload:   payload,
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	evts, err := s.ListEvents(ctx, tk.TaskID)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("want 1 event, got %d", len(evts))
	}
	if string(evts[0].Payload) != string(payload) {
		t.Errorf("Payload: got %s, want %s", evts[0].Payload, payload)
	}
	_ = evt
}

func TestTaskEncryption_EventPayloadPlaintextNull(t *testing.T) {
	db := newEncDBTasks(t)
	s := tasks.NewStore(db)
	s.SetEncryption(newMKMTasks(t, "pw"))
	ctx := context.Background()
	tk, err := s.Create(ctx, tasks.Task{AgentID: "agt_test", Capability: "nexus_write"})
	if err != nil {
		t.Fatalf("Create task: %v", err)
	}
	evt, err := s.AppendEvent(ctx, tasks.TaskEvent{
		TaskID:    tk.TaskID,
		EventType: tasks.EventTypeProgress,
		Payload:   json.RawMessage(`{"secret":"data"}`),
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	var rawPayload sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT payload_json FROM task_events WHERE event_id = ?`, evt.EventID).Scan(&rawPayload); err != nil {
		t.Fatalf("raw query: %v", err)
	}
	if rawPayload.Valid {
		t.Errorf("plaintext payload_json should be NULL, got %q", rawPayload.String)
	}
}

func TestTaskEncryption_ListDecrypts(t *testing.T) {
	db := newEncDBTasks(t)
	s := tasks.NewStore(db)
	s.SetEncryption(newMKMTasks(t, "listpw"))
	ctx := context.Background()
	input := json.RawMessage(`{"list":"test"}`)
	if _, err := s.Create(ctx, tasks.Task{
		AgentID:    "agt_list",
		Capability: "nexus_write",
		Input:      input,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	list, err := s.List(ctx, tasks.ListFilter{AgentID: "agt_list"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 task, got %d", len(list))
	}
	if string(list[0].Input) != string(input) {
		t.Errorf("Input: got %s, want %s", list[0].Input, input)
	}
}
