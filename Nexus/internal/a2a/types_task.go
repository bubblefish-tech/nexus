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

import "encoding/json"

// TaskState represents the lifecycle state of a Task.
type TaskState string

const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input-required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateFailed        TaskState = "failed"
	TaskStateCanceled      TaskState = "canceled"
)

// allTaskStates is the canonical list of valid task states.
var allTaskStates = []TaskState{
	TaskStateSubmitted,
	TaskStateWorking,
	TaskStateInputRequired,
	TaskStateCompleted,
	TaskStateFailed,
	TaskStateCanceled,
}

// AllTaskStates returns all valid TaskState values.
func AllTaskStates() []TaskState {
	out := make([]TaskState, len(allTaskStates))
	copy(out, allTaskStates)
	return out
}

// String returns the string representation of a TaskState.
func (ts TaskState) String() string {
	return string(ts)
}

// ParseTaskState converts a string to a TaskState.
// Returns the state and true if valid, or empty string and false if unknown.
func ParseTaskState(s string) (TaskState, bool) {
	for _, ts := range allTaskStates {
		if string(ts) == s {
			return ts, true
		}
	}
	return "", false
}

// ValidTaskState returns true if ts is a known TaskState value.
func ValidTaskState(ts TaskState) bool {
	_, ok := ParseTaskState(string(ts))
	return ok
}

// IsTerminal returns true if the task state is a terminal state
// (completed, failed, or canceled).
func (ts TaskState) IsTerminal() bool {
	return ts == TaskStateCompleted || ts == TaskStateFailed || ts == TaskStateCanceled
}

// TaskStatus represents the current status of a task.
type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *Message  `json:"message,omitempty"`
	Timestamp string    `json:"timestamp"`
}

// Artifact is a named output produced by a task.
type Artifact struct {
	ArtifactID  string          `json:"artifactId"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Parts       []PartWrapper   `json:"parts,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// Task is the top-level task object in the A2A protocol.
type Task struct {
	Kind       string          `json:"kind"`
	TaskID     string          `json:"taskId"`
	ContextID  string          `json:"contextId,omitempty"`
	Status     TaskStatus      `json:"status"`
	History    []Message       `json:"history,omitempty"`
	Artifacts  []Artifact      `json:"artifacts,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
	Extensions json.RawMessage `json:"extensions,omitempty"`
}

// NewTask creates a Task with a generated ID and initial submitted state.
func NewTask() Task {
	return Task{
		Kind:   "task",
		TaskID: NewTaskID(),
		Status: TaskStatus{
			State:     TaskStateSubmitted,
			Timestamp: Now(),
		},
	}
}

// NewArtifact creates an Artifact with a generated ID.
func NewArtifact(name string, parts ...Part) Artifact {
	wrappers := make([]PartWrapper, len(parts))
	for i, p := range parts {
		wrappers[i] = PartWrapper{Part: p}
	}
	return Artifact{
		ArtifactID: NewArtifactID(),
		Name:       name,
		Parts:      wrappers,
	}
}
