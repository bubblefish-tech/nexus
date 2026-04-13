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

func newTestCursorWatcher(t *testing.T) *CursorWatcher {
	t.Helper()
	return NewCursorWatcher(DefaultConfig(), slog.Default())
}

func TestCursorParse_SampleChat(t *testing.T) {
	w := newTestCursorWatcher(t)
	result, err := w.Parse(context.Background(), testdataPath(t, "cursor", "sample_chat.json"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Memories) != 4 {
		t.Fatalf("memories = %d, want 4", len(result.Memories))
	}

	m := result.Memories[0]
	if m.Role != "user" {
		t.Errorf("first role = %q, want user", m.Role)
	}
	if m.Content != "Explain goroutines vs threads" {
		t.Errorf("first content = %q", m.Content)
	}
	if m.SourceMeta["cursor_chat_id"] != "chat-001" {
		t.Errorf("chat_id = %q", m.SourceMeta["cursor_chat_id"])
	}
	if m.SourceMeta["cursor_title"] != "Go concurrency patterns" {
		t.Errorf("title = %q", m.SourceMeta["cursor_title"])
	}
	if m.Timestamp <= 0 {
		t.Error("expected positive timestamp")
	}
}

func TestCursorParse_EmptyChat(t *testing.T) {
	w := newTestCursorWatcher(t)
	result, err := w.Parse(context.Background(), testdataPath(t, "cursor", "empty_chat.json"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Memories) != 0 {
		t.Errorf("memories = %d, want 0", len(result.Memories))
	}
}

func TestCursorParse_Malformed(t *testing.T) {
	w := newTestCursorWatcher(t)
	_, err := w.Parse(context.Background(), testdataPath(t, "cursor", "malformed.json"), 0)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestCursorParse_FileTooLarge(t *testing.T) {
	w := newTestCursorWatcher(t)
	w.cfg.MaxFileSize = 5
	_, err := w.Parse(context.Background(), testdataPath(t, "cursor", "sample_chat.json"), 0)
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestCursorParse_SymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(testdataPath(t, "cursor", "sample_chat.json"), link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	w := newTestCursorWatcher(t)
	_, err := w.Parse(context.Background(), link, 0)
	if err != ErrSymlinkRejected {
		t.Errorf("expected ErrSymlinkRejected, got %v", err)
	}
}

func TestCursorParse_FileRewrite(t *testing.T) {
	// Simulate Cursor rewriting the whole file (different content, same path).
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.json")

	v1 := `{"id":"c1","title":"V1","messages":[{"role":"user","content":"Hello","timestamp":"2026-04-10T10:00:00.000Z"}]}`
	if err := os.WriteFile(path, []byte(v1), 0600); err != nil {
		t.Fatal(err)
	}

	w := newTestCursorWatcher(t)
	r1, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Memories) != 1 {
		t.Fatalf("v1 memories = %d, want 1", len(r1.Memories))
	}

	// Rewrite with more messages.
	v2 := `{"id":"c1","title":"V2","messages":[{"role":"user","content":"Hello","timestamp":"2026-04-10T10:00:00.000Z"},{"role":"assistant","content":"Hi there","timestamp":"2026-04-10T10:00:01.000Z"}]}`
	if err := os.WriteFile(path, []byte(v2), 0600); err != nil {
		t.Fatal(err)
	}

	r2, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Memories) != 2 {
		t.Fatalf("v2 memories = %d, want 2", len(r2.Memories))
	}

	// Hash should differ between versions.
	if r1.LastHash == r2.LastHash {
		t.Error("hashes should differ after file rewrite")
	}
}

func TestCursorName(t *testing.T) {
	w := newTestCursorWatcher(t)
	if w.Name() != "cursor" {
		t.Errorf("Name() = %q", w.Name())
	}
	if w.SourceName() != "ingest.cursor" {
		t.Errorf("SourceName() = %q", w.SourceName())
	}
}
