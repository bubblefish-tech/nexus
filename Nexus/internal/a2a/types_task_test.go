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

package a2a

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskStateString(t *testing.T) {
	tests := []struct {
		state TaskState
		want  string
	}{
		{TaskStateSubmitted, "submitted"},
		{TaskStateWorking, "working"},
		{TaskStateInputRequired, "input-required"},
		{TaskStateCompleted, "completed"},
		{TaskStateFailed, "failed"},
		{TaskStateCanceled, "canceled"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTaskState(t *testing.T) {
	for _, ts := range AllTaskStates() {
		got, ok := ParseTaskState(ts.String())
		if !ok {
			t.Errorf("ParseTaskState(%q) returned false", ts.String())
		}
		if got != ts {
			t.Errorf("ParseTaskState(%q) = %q, want %q", ts.String(), got, ts)
		}
	}
	// Invalid
	_, ok := ParseTaskState("invalid")
	if ok {
		t.Error("ParseTaskState(\"invalid\") should return false")
	}
}

func TestValidTaskState(t *testing.T) {
	for _, ts := range AllTaskStates() {
		if !ValidTaskState(ts) {
			t.Errorf("ValidTaskState(%q) = false", ts)
		}
	}
	if ValidTaskState("bogus") {
		t.Error("ValidTaskState(\"bogus\") should be false")
	}
}

func TestTaskStateIsTerminal(t *testing.T) {
	tests := []struct {
		state    TaskState
		terminal bool
	}{
		{TaskStateSubmitted, false},
		{TaskStateWorking, false},
		{TaskStateInputRequired, false},
		{TaskStateCompleted, true},
		{TaskStateFailed, true},
		{TaskStateCanceled, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsTerminal(); got != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}

func TestAllTaskStatesCount(t *testing.T) {
	states := AllTaskStates()
	if len(states) != 6 {
		t.Errorf("expected 6 task states, got %d", len(states))
	}
}

func TestTaskRoundtrip(t *testing.T) {
	task := NewTask()
	task.ContextID = NewContextID()
	task.History = []Message{NewMessage(RoleUser, NewTextPart("do something"))}
	task.Artifacts = []Artifact{NewArtifact("result", NewTextPart("done"))}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.Kind != "task" {
		t.Errorf("kind = %q, want %q", got.Kind, "task")
	}
	if got.TaskID != task.TaskID {
		t.Errorf("taskId mismatch")
	}
	if got.Status.State != TaskStateSubmitted {
		t.Errorf("state = %q, want %q", got.Status.State, TaskStateSubmitted)
	}
	if len(got.History) != 1 {
		t.Fatalf("history len = %d, want 1", len(got.History))
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("artifacts len = %d, want 1", len(got.Artifacts))
	}
}

func TestTaskStatusWithMessage(t *testing.T) {
	msg := NewMessage(RoleAgent, NewTextPart("working on it"))
	status := TaskStatus{
		State:     TaskStateWorking,
		Message:   &msg,
		Timestamp: Now(),
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got TaskStatus
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Message == nil {
		t.Fatal("message should not be nil")
	}
	if got.Message.MessageID != msg.MessageID {
		t.Errorf("message ID mismatch")
	}
}

func TestTaskStatusWithoutMessage(t *testing.T) {
	status := TaskStatus{
		State:     TaskStateCompleted,
		Timestamp: Now(),
	}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"message"`) {
		t.Errorf("null message should be omitted: %s", data)
	}
}

func TestTaskJSONFieldNames(t *testing.T) {
	task := NewTask()
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, field := range []string{`"kind"`, `"taskId"`, `"status"`} {
		if !strings.Contains(s, field) {
			t.Errorf("expected field %s in JSON: %s", field, s)
		}
	}
}

func TestTaskWithExtensions(t *testing.T) {
	task := NewTask()
	task.Extensions = json.RawMessage(`{"sh.bubblefish.nexus.governance/v1":{"decision":"allow"}}`)
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Task
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got.Extensions) != string(task.Extensions) {
		t.Errorf("extensions mismatch: %s vs %s", got.Extensions, task.Extensions)
	}
}

func TestArtifactRoundtrip(t *testing.T) {
	art := NewArtifact("output", NewTextPart("result text"))
	art.Description = "The final result"
	art.Metadata = json.RawMessage(`{"format":"text"}`)

	data, err := json.Marshal(art)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Artifact
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ArtifactID != art.ArtifactID {
		t.Error("artifactId mismatch")
	}
	if got.Name != "output" {
		t.Errorf("name = %q, want %q", got.Name, "output")
	}
	if got.Description != "The final result" {
		t.Errorf("description mismatch")
	}
}

func TestTaskStateJSON(t *testing.T) {
	// TaskState should serialize as a plain string
	status := TaskStatus{State: TaskStateInputRequired, Timestamp: Now()}
	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"input-required"`) {
		t.Errorf("expected input-required in JSON: %s", data)
	}
}
