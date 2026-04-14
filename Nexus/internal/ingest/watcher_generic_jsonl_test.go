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

func newTestGenericJSONLWatcher(t *testing.T) *GenericJSONLWatcher {
	t.Helper()
	return NewGenericJSONLWatcher(DefaultConfig(), slog.Default())
}

func TestGenericJSONLParse_Sample(t *testing.T) {
	w := newTestGenericJSONLWatcher(t)
	result, err := w.Parse(context.Background(), testdataPath(t, "generic_jsonl", "sample.jsonl"), 0)
	if err != nil {
		t.Fatal(err)
	}

	// sample.jsonl has 4 lines: user, assistant, system, user (no timestamp).
	if len(result.Memories) != 4 {
		t.Fatalf("memories = %d, want 4", len(result.Memories))
	}

	// Check roles.
	roles := []string{"user", "assistant", "system", "user"}
	for i, want := range roles {
		if result.Memories[i].Role != want {
			t.Errorf("memory[%d].Role = %q, want %q", i, result.Memories[i].Role, want)
		}
	}

	// Last line has no timestamp — should be 0.
	if result.Memories[3].Timestamp != 0 {
		t.Errorf("last memory timestamp = %d, want 0 (missing)", result.Memories[3].Timestamp)
	}

	// First line has a timestamp.
	if result.Memories[0].Timestamp <= 0 {
		t.Error("first memory should have positive timestamp")
	}

	if result.Memories[0].SourceMeta["ingest_watcher"] != "generic_jsonl" {
		t.Errorf("ingest_watcher = %q", result.Memories[0].SourceMeta["ingest_watcher"])
	}
}

func TestGenericJSONLParse_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.jsonl")
	os.WriteFile(path, []byte{}, 0600)

	w := newTestGenericJSONLWatcher(t)
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Memories) != 0 {
		t.Errorf("memories = %d, want 0", len(result.Memories))
	}
}

func TestGenericJSONLParse_MalformedLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.jsonl")
	content := `{"role":"user","content":"Good"}
not json
{"role":"assistant","content":"Also good"}
`
	os.WriteFile(path, []byte(content), 0600)

	w := newTestGenericJSONLWatcher(t)
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Memories) != 2 {
		t.Errorf("memories = %d, want 2 (skip bad line)", len(result.Memories))
	}
}

func TestGenericJSONLParse_InvalidRole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "badrole.jsonl")
	content := `{"role":"admin","content":"Not a valid role"}
{"role":"user","content":"Valid"}
`
	os.WriteFile(path, []byte(content), 0600)

	w := newTestGenericJSONLWatcher(t)
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Memories) != 1 {
		t.Errorf("memories = %d, want 1 (skip invalid role)", len(result.Memories))
	}
}

func TestGenericJSONLParse_MissingContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nocontent.jsonl")
	content := `{"role":"user","content":""}
{"role":"user","content":"Has content"}
`
	os.WriteFile(path, []byte(content), 0600)

	w := newTestGenericJSONLWatcher(t)
	result, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Memories) != 1 {
		t.Errorf("memories = %d, want 1 (skip empty content)", len(result.Memories))
	}
}

func TestGenericJSONLParse_FileTooLarge(t *testing.T) {
	w := newTestGenericJSONLWatcher(t)
	w.cfg.MaxFileSize = 5
	_, err := w.Parse(context.Background(), testdataPath(t, "generic_jsonl", "sample.jsonl"), 0)
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestGenericJSONLParse_SymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(testdataPath(t, "generic_jsonl", "sample.jsonl"), link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	w := newTestGenericJSONLWatcher(t)
	_, err := w.Parse(context.Background(), link, 0)
	if err != ErrSymlinkRejected {
		t.Errorf("expected ErrSymlinkRejected, got %v", err)
	}
}

func TestGenericJSONLParse_IncrementalOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "growing.jsonl")

	line1 := `{"role":"user","content":"First"}` + "\n"
	os.WriteFile(path, []byte(line1), 0600)

	w := newTestGenericJSONLWatcher(t)
	r1, err := w.Parse(context.Background(), path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r1.Memories) != 1 {
		t.Fatalf("first parse: %d memories, want 1", len(r1.Memories))
	}

	// Append.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString(`{"role":"assistant","content":"Second"}` + "\n")
	f.Close()

	r2, err := w.Parse(context.Background(), path, r1.NewOffset)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2.Memories) != 1 {
		t.Errorf("second parse: %d memories, want 1", len(r2.Memories))
	}
}

func TestGenericJSONLName(t *testing.T) {
	w := newTestGenericJSONLWatcher(t)
	if w.Name() != "generic_jsonl" {
		t.Errorf("Name() = %q", w.Name())
	}
	if w.SourceName() != "ingest.generic_jsonl" {
		t.Errorf("SourceName() = %q", w.SourceName())
	}
}
