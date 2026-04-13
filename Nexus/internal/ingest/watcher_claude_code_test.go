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
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func newTestClaudeCodeWatcher(t *testing.T) *ClaudeCodeWatcher {
	t.Helper()
	cfg := DefaultConfig()
	return NewClaudeCodeWatcher(cfg, slog.Default())
}

func testdataPath(t *testing.T, parts ...string) string {
	t.Helper()
	elems := append([]string{"testdata"}, parts...)
	return filepath.Join(elems...)
}

func TestClaudeCodeParse_SampleSession(t *testing.T) {
	w := newTestClaudeCodeWatcher(t)
	result, err := w.Parse(context.Background(), testdataPath(t, "claude_code", "sample_session.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}

	// sample_session.jsonl has 4 user/assistant lines + 1 assistant with array content
	// Lines: user, assistant, user, assistant, tool_use (skip), tool_result (skip), user, assistant(array)
	// = 6 user/assistant messages
	if len(result.Memories) != 6 {
		t.Fatalf("memories = %d, want 6", len(result.Memories))
	}

	// Check first memory.
	m := result.Memories[0]
	if m.Role != "user" {
		t.Errorf("first memory role = %q, want %q", m.Role, "user")
	}
	if m.Content != "What is the capital of France?" {
		t.Errorf("first memory content = %q", m.Content)
	}
	if m.SourceMeta["ingest_watcher"] != "claude_code" {
		t.Errorf("source_meta ingest_watcher = %q", m.SourceMeta["ingest_watcher"])
	}
	if m.SourceMeta["claude_session_id"] != "sess-001" {
		t.Errorf("session_id = %q", m.SourceMeta["claude_session_id"])
	}

	// Check second memory (assistant with model).
	m2 := result.Memories[1]
	if m2.Role != "assistant" {
		t.Errorf("second memory role = %q, want %q", m2.Role, "assistant")
	}
	if m2.Model != "claude-opus-4-6" {
		t.Errorf("model = %q, want %q", m2.Model, "claude-opus-4-6")
	}

	// Check last memory (assistant with array content).
	mLast := result.Memories[5]
	if mLast.Content != "You're welcome!" {
		t.Errorf("last memory content = %q, want %q", mLast.Content, "You're welcome!")
	}

	// NewOffset should be past the end of the file.
	if result.NewOffset <= 0 {
		t.Errorf("NewOffset = %d, want > 0", result.NewOffset)
	}
}

func TestClaudeCodeParse_FromOffset(t *testing.T) {
	w := newTestClaudeCodeWatcher(t)

	// First parse to get offset after first 2 user/assistant lines.
	full, err := w.Parse(context.Background(), testdataPath(t, "claude_code", "sample_session.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Memories) < 3 {
		t.Fatalf("need at least 3 memories to test offset, got %d", len(full.Memories))
	}

	// Use the offset of the 3rd memory to start a mid-file parse.
	midOffset := full.Memories[2].OriginalOffset
	partial, err := w.Parse(context.Background(), testdataPath(t, "claude_code", "sample_session.jsonl"), midOffset)
	if err != nil {
		t.Fatal(err)
	}

	// Should get fewer memories than the full parse.
	if len(partial.Memories) >= len(full.Memories) {
		t.Errorf("partial parse got %d memories, should be less than full %d", len(partial.Memories), len(full.Memories))
	}
	if len(partial.Memories) == 0 {
		t.Error("partial parse got 0 memories, expected some")
	}
}

func TestClaudeCodeParse_EmptyFile(t *testing.T) {
	w := newTestClaudeCodeWatcher(t)
	result, err := w.Parse(context.Background(), testdataPath(t, "claude_code", "empty.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Memories) != 0 {
		t.Errorf("memories = %d, want 0 for empty file", len(result.Memories))
	}
}

func TestClaudeCodeParse_MalformedLines(t *testing.T) {
	w := newTestClaudeCodeWatcher(t)
	result, err := w.Parse(context.Background(), testdataPath(t, "claude_code", "malformed.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}

	// malformed.jsonl has 3 good user/assistant lines and 2 bad lines.
	if len(result.Memories) != 3 {
		t.Errorf("memories = %d, want 3 (good lines only)", len(result.Memories))
	}
}

func TestClaudeCodeParse_TruncatedSession(t *testing.T) {
	w := newTestClaudeCodeWatcher(t)
	result, err := w.Parse(context.Background(), testdataPath(t, "claude_code", "truncated_session.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}

	// truncated_session.jsonl has 2 complete lines and 1 truncated line.
	// The truncated line should NOT be ingested.
	if len(result.Memories) != 2 {
		t.Errorf("memories = %d, want 2 (truncated line excluded)", len(result.Memories))
	}
}

func TestClaudeCodeParse_FileTooLarge(t *testing.T) {
	w := newTestClaudeCodeWatcher(t)
	w.cfg.MaxFileSize = 10 // 10 bytes — every real file exceeds this

	_, err := w.Parse(context.Background(), testdataPath(t, "claude_code", "sample_session.jsonl"), 0)
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestClaudeCodeParse_SymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	dir := t.TempDir()
	target := testdataPath(t, "claude_code", "sample_session.jsonl")
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	w := newTestClaudeCodeWatcher(t)
	_, err := w.Parse(context.Background(), link, 0)
	if err != ErrSymlinkRejected {
		t.Errorf("expected ErrSymlinkRejected, got %v", err)
	}
}

func TestClaudeCodeParse_FileGrowth(t *testing.T) {
	// Simulate file growing between two parses.
	dir := t.TempDir()
	path := filepath.Join(dir, "growing.jsonl")

	// Write initial content.
	line1 := `{"type":"user","message":{"role":"user","content":"First"},"timestamp":"2026-04-10T14:00:00.000Z","sessionId":"s1"}` + "\n"
	line2 := `{"type":"assistant","message":{"role":"assistant","content":"Reply","model":"m"},"timestamp":"2026-04-10T14:00:01.000Z","sessionId":"s1"}` + "\n"
	if err := os.WriteFile(path, []byte(line1+line2), 0600); err != nil {
		t.Fatal(err)
	}

	w := newTestClaudeCodeWatcher(t)
	r1, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Memories) != 2 {
		t.Fatalf("first parse: %d memories, want 2", len(r1.Memories))
	}

	// Append more content.
	line3 := `{"type":"user","message":{"role":"user","content":"Second question"},"timestamp":"2026-04-10T14:00:02.000Z","sessionId":"s1"}` + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(line3)
	f.Close()

	// Parse from previous offset — should only get the new line.
	r2, err := w.Parse(context.Background(), path, r1.NewOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Memories) != 1 {
		t.Errorf("second parse: %d memories, want 1 (only new line)", len(r2.Memories))
	}
	if len(r2.Memories) > 0 && r2.Memories[0].Content != "Second question" {
		t.Errorf("new memory content = %q", r2.Memories[0].Content)
	}
}

func TestClaudeCodeParse_TimestampParsing(t *testing.T) {
	w := newTestClaudeCodeWatcher(t)
	result, err := w.Parse(context.Background(), testdataPath(t, "claude_code", "sample_session.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Memories) == 0 {
		t.Fatal("no memories")
	}
	if result.Memories[0].Timestamp <= 0 {
		t.Errorf("timestamp = %d, want > 0", result.Memories[0].Timestamp)
	}
}

func TestClaudeCodeName(t *testing.T) {
	w := newTestClaudeCodeWatcher(t)
	if w.Name() != "claude_code" {
		t.Errorf("Name() = %q", w.Name())
	}
	if w.SourceName() != "ingest.claude_code" {
		t.Errorf("SourceName() = %q", w.SourceName())
	}
}

func TestClaudeCodeStateTransitions(t *testing.T) {
	w := newTestClaudeCodeWatcher(t)
	if w.State() != StateDisabled {
		t.Errorf("initial state = %v, want StateDisabled", w.State())
	}
	w.SetState(StateActive)
	if w.State() != StateActive {
		t.Errorf("state after SetState(Active) = %v", w.State())
	}
}
