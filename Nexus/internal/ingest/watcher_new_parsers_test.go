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

package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeDesktopWatcher_ParseValid(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "conv.json")
	data := `{"title":"test","messages":[{"role":"human","content":"hello","timestamp":"1714000000"},{"role":"assistant","content":"hi there","timestamp":"1714000001"}]}`
	os.WriteFile(path, []byte(data), 0600)

	w := NewClaudeDesktopWatcher()
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(result.Memories))
	}
	if result.Memories[0].Role != "user" {
		t.Errorf("expected role 'user' for 'human', got %q", result.Memories[0].Role)
	}
	if result.Memories[0].Content != "hello" {
		t.Errorf("expected content 'hello', got %q", result.Memories[0].Content)
	}
}

func TestClaudeDesktopWatcher_ParseMalformed(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json at all"), 0600)

	w := NewClaudeDesktopWatcher()
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Memories) != 0 {
		t.Fatalf("expected 0 memories from malformed input, got %d", len(result.Memories))
	}
}

func TestChatGPTDesktopWatcher_ParseValid(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "conv.json")
	data := `{"title":"Chat","messages":[{"role":"user","content":"what is Go?","create_time":1714000000.5},{"role":"assistant","content":"A programming language","create_time":1714000001.0}]}`
	os.WriteFile(path, []byte(data), 0600)

	w := NewChatGPTDesktopWatcher()
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(result.Memories))
	}
	if result.Memories[0].Content != "what is Go?" {
		t.Errorf("expected 'what is Go?', got %q", result.Memories[0].Content)
	}
	if result.Memories[0].Timestamp == 0 {
		t.Error("expected non-zero timestamp")
	}
}

func TestChatGPTDesktopWatcher_ParseMalformed(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("{{{broken"), 0600)

	w := NewChatGPTDesktopWatcher()
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Memories) != 0 {
		t.Fatalf("expected 0 memories from malformed input, got %d", len(result.Memories))
	}
}

func TestChatGPTDesktopWatcher_ParseMapping(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "conv.json")
	data := `{"title":"T","mapping":{"a":{"message":{"author":{"role":"user"},"content":{"parts":["hello world"]},"create_time":1714000000}}}}`
	os.WriteFile(path, []byte(data), 0600)

	w := NewChatGPTDesktopWatcher()
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Memories) != 1 {
		t.Fatalf("expected 1 memory from mapping, got %d", len(result.Memories))
	}
	if result.Memories[0].Content != "hello world" {
		t.Errorf("expected 'hello world', got %q", result.Memories[0].Content)
	}
}

func TestOpenWebUIWatcher_ParseValid(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.json")
	data := `{"title":"Web Chat","messages":[{"role":"user","content":"test query","timestamp":"2026-04-21T12:00:00Z"},{"role":"assistant","content":"test answer"}]}`
	os.WriteFile(path, []byte(data), 0600)

	w := NewOpenWebUIWatcher()
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Memories) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(result.Memories))
	}
	if result.Memories[0].Content != "test query" {
		t.Errorf("expected 'test query', got %q", result.Memories[0].Content)
	}
}

func TestOpenWebUIWatcher_ParseMalformed(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte(""), 0600)

	w := NewOpenWebUIWatcher()
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Memories) != 0 {
		t.Fatalf("expected 0 memories from empty input, got %d", len(result.Memories))
	}
}

func TestPerplexityCometWatcher_ParseValid(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "thread.json")
	data := `{"title":"Search","entries":[{"query":"what is Go?","answer":"A systems language","timestamp":"1714000000"}]}`
	os.WriteFile(path, []byte(data), 0600)

	w := NewPerplexityCometWatcher()
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Memories) != 2 {
		t.Fatalf("expected 2 memories (query + answer), got %d", len(result.Memories))
	}
	if result.Memories[0].Role != "user" {
		t.Errorf("expected query role 'user', got %q", result.Memories[0].Role)
	}
	if result.Memories[1].Role != "assistant" {
		t.Errorf("expected answer role 'assistant', got %q", result.Memories[1].Role)
	}
}

func TestPerplexityCometWatcher_ParseMalformed(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json"), 0600)

	w := NewPerplexityCometWatcher()
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(result.Memories) != 0 {
		t.Fatalf("expected 0 memories from malformed input, got %d", len(result.Memories))
	}
}

func TestNormalizeRole(t *testing.T) {
	t.Helper()
	tests := []struct{ input, want string }{
		{"user", "user"},
		{"human", "user"},
		{"Human", "user"},
		{"assistant", "assistant"},
		{"ai", "assistant"},
		{"bot", "assistant"},
		{"model", "assistant"},
		{"system", "system"},
		{"", "user"},
		{"custom", "custom"},
	}
	for _, tt := range tests {
		got := normalizeRole(tt.input)
		if got != tt.want {
			t.Errorf("normalizeRole(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseTimestampMulti(t *testing.T) {
	t.Helper()
	tests := []struct {
		input string
		nonZero bool
	}{
		{"1714000000", true},
		{"1714000000000", true},
		{"2026-04-21T12:00:00Z", true},
		{"", false},
		{"not-a-time", false},
	}
	for _, tt := range tests {
		got := parseTimestampMulti(tt.input)
		if tt.nonZero && got == 0 {
			t.Errorf("parseTimestamp(%q) = 0, want non-zero", tt.input)
		}
		if !tt.nonZero && got != 0 {
			t.Errorf("parseTimestamp(%q) = %d, want 0", tt.input, got)
		}
	}
}
