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

package bridge

import (
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a"
)

func TestMCPToNA2A_StringInput(t *testing.T) {
	t.Helper()
	args := map[string]interface{}{
		"input": "hello world",
		"agent": "test-agent",
	}
	msg, skill, cfg, err := MCPToNA2A(args, "source-1")
	if err != nil {
		t.Fatalf("MCPToNA2A: %v", err)
	}
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(msg.Parts))
	}
	if skill != "" {
		t.Errorf("expected empty skill, got %q", skill)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestMCPToNA2A_MissingInput_ReturnsError(t *testing.T) {
	t.Helper()
	args := map[string]interface{}{"agent": "test"}
	_, _, _, err := MCPToNA2A(args, "src")
	if err == nil {
		t.Fatal("expected error for missing input")
	}
}

func TestMCPToNA2A_WithSkillAndBlocking(t *testing.T) {
	t.Helper()
	args := map[string]interface{}{
		"input":    "query",
		"skill":    "search",
		"blocking": true,
		"timeout_ms": float64(5000),
	}
	_, skill, cfg, err := MCPToNA2A(args, "src")
	if err != nil {
		t.Fatalf("MCPToNA2A: %v", err)
	}
	if skill != "search" {
		t.Errorf("expected skill 'search', got %q", skill)
	}
	if !cfg.Blocking {
		t.Error("expected blocking=true")
	}
	if cfg.TimeoutMs != 5000 {
		t.Errorf("expected timeout 5000, got %d", cfg.TimeoutMs)
	}
}

func TestMCPToNA2A_ObjectInput(t *testing.T) {
	t.Helper()
	args := map[string]interface{}{
		"input": map[string]interface{}{"key": "value"},
	}
	msg, _, _, err := MCPToNA2A(args, "src")
	if err != nil {
		t.Fatalf("MCPToNA2A: %v", err)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("expected 1 data part, got %d", len(msg.Parts))
	}
}

func TestNA2AToMCP_BasicTask(t *testing.T) {
	t.Helper()
	task := &a2a.Task{
		TaskID: "task-123",
		Status: a2a.TaskStatus{
			State: a2a.TaskStateCompleted,
		},
	}
	result := NA2AToMCP(task)
	if result["task_id"] != "task-123" {
		t.Errorf("expected task_id 'task-123', got %v", result["task_id"])
	}
	if result["state"] != "completed" {
		t.Errorf("expected state 'completed', got %v", result["state"])
	}
}

func TestNA2AToMCP_StatusMessageText(t *testing.T) {
	t.Helper()
	msg := a2a.NewMessage(a2a.RoleAgent, a2a.NewTextPart("done"))
	task := &a2a.Task{
		TaskID: "t1",
		Status: a2a.TaskStatus{
			State:   a2a.TaskStateCompleted,
			Message: &msg,
		},
	}
	result := NA2AToMCP(task)
	if result["status_message"] != "done" {
		t.Errorf("expected status_message 'done', got %v", result["status_message"])
	}
}

func TestExtractTextFromMessage_MultiPart(t *testing.T) {
	t.Helper()
	msg := a2a.NewMessage(a2a.RoleAgent,
		a2a.NewTextPart("line 1"),
		a2a.NewTextPart("line 2"),
	)
	text := extractTextFromMessage(msg)
	if text != "line 1\nline 2" {
		t.Errorf("expected 'line 1\\nline 2', got %q", text)
	}
}

func TestExtractTextFromMessage_Empty(t *testing.T) {
	t.Helper()
	msg := a2a.NewMessage(a2a.RoleAgent)
	text := extractTextFromMessage(msg)
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
}
