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

package importer

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

type mockWriter struct {
	memories []Memory
}

func (w *mockWriter) Write(source string, memory Memory) error {
	w.memories = append(w.memories, memory)
	return nil
}

// createTestZIP creates a ZIP with the given files.
func createTestZIP(t *testing.T, dir string, name string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for fname, content := range files {
		w, err := zw.Create(fname)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	zw.Close()
	f.Close()
	return path
}

func TestImportClaudeZIP(t *testing.T) {
	dir := t.TempDir()
	convJSON := `[{"uuid":"c1","name":"Test Conv","chat_messages":[
		{"text":"Hello Claude","sender":"human","created_at":"2026-04-10T10:00:00.000Z"},
		{"text":"Hello!","sender":"assistant","created_at":"2026-04-10T10:00:01.000Z"}
	]}]`
	path := createTestZIP(t, dir, "claude.zip", map[string]string{
		"conversations.json": convJSON,
		"users.json":         `[{"uuid":"u1"}]`,
	})

	w := &mockWriter{}
	result, err := Run(Options{Path: path, Format: FormatAuto, Writer: w})
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != FormatClaudeZIP {
		t.Errorf("format = %q, want claude-zip", result.Format)
	}
	if result.Written != 2 {
		t.Errorf("written = %d, want 2", result.Written)
	}
	if len(w.memories) != 2 {
		t.Fatalf("memories = %d, want 2", len(w.memories))
	}
	if w.memories[0].Content != "Hello Claude" {
		t.Errorf("first content = %q", w.memories[0].Content)
	}
}

func TestImportChatGPTZIP(t *testing.T) {
	dir := t.TempDir()
	convJSON := `[{"id":"c1","title":"Test","mapping":{"n1":{"message":{"author":{"role":"user"},"content":{"parts":["Hi GPT"]},"create_time":1712750400.0}},"n2":{"message":{"author":{"role":"assistant"},"content":{"parts":["Hi!"]},"create_time":1712750401.0}}}}]`
	path := createTestZIP(t, dir, "chatgpt.zip", map[string]string{
		"conversations.json": convJSON,
	})

	w := &mockWriter{}
	result, err := Run(Options{Path: path, Format: FormatAuto, Writer: w})
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != FormatChatGPTZIP {
		t.Errorf("format = %q, want chatgpt-zip", result.Format)
	}
	if result.Written != 2 {
		t.Errorf("written = %d, want 2", result.Written)
	}
}

func TestImportClaudeCodeDir(t *testing.T) {
	dir := t.TempDir()
	content := `{"type":"user","message":{"role":"user","content":"Test msg"},"timestamp":"2026-04-10T10:00:00.000Z","sessionId":"s1"}
{"type":"assistant","message":{"role":"assistant","content":"Reply","model":"m"},"timestamp":"2026-04-10T10:00:01.000Z","sessionId":"s1"}
`
	os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(content), 0600)

	w := &mockWriter{}
	result, err := Run(Options{Path: dir, Format: FormatClaudeCodeDir, Writer: w})
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 2 {
		t.Errorf("written = %d, want 2", result.Written)
	}
}

func TestImportCursorDir(t *testing.T) {
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chat-history")
	os.MkdirAll(chatDir, 0700)

	chatJSON := `{"id":"c1","title":"Test","messages":[
		{"role":"user","content":"Hi Cursor","timestamp":"2026-04-10T10:00:00.000Z"},
		{"role":"assistant","content":"Hello!","timestamp":"2026-04-10T10:00:01.000Z"}
	]}`
	os.WriteFile(filepath.Join(chatDir, "chat1.json"), []byte(chatJSON), 0600)

	w := &mockWriter{}
	result, err := Run(Options{Path: dir, Format: FormatAuto, Writer: w})
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != FormatCursorDir {
		t.Errorf("format = %q, want cursor-dir", result.Format)
	}
	if result.Written != 2 {
		t.Errorf("written = %d, want 2", result.Written)
	}
}

func TestImportGenericJSONL(t *testing.T) {
	dir := t.TempDir()
	content := `{"role":"user","content":"Line 1","timestamp":"2026-04-10T10:00:00.000Z"}
{"role":"assistant","content":"Line 2","timestamp":"2026-04-10T10:00:01.000Z"}
`
	path := filepath.Join(dir, "test.jsonl")
	os.WriteFile(path, []byte(content), 0600)

	w := &mockWriter{}
	result, err := Run(Options{Path: path, Format: FormatAuto, Writer: w})
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != FormatJSONL {
		t.Errorf("format = %q, want jsonl", result.Format)
	}
	if result.Written != 2 {
		t.Errorf("written = %d, want 2", result.Written)
	}
}

func TestImportDryRun(t *testing.T) {
	dir := t.TempDir()
	content := `{"role":"user","content":"Line 1"}
{"role":"assistant","content":"Line 2"}
`
	path := filepath.Join(dir, "test.jsonl")
	os.WriteFile(path, []byte(content), 0600)

	result, err := Run(Options{Path: path, Format: FormatJSONL, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 2 {
		t.Errorf("dry-run written = %d, want 2 (counted but not sent)", result.Written)
	}
}

func TestImportCustomSourceName(t *testing.T) {
	dir := t.TempDir()
	content := `{"role":"user","content":"Line 1"}
`
	path := filepath.Join(dir, "test.jsonl")
	os.WriteFile(path, []byte(content), 0600)

	w := &mockWriter{}
	_, err := Run(Options{Path: path, Format: FormatJSONL, SourceName: "custom_source", Writer: w})
	if err != nil {
		t.Fatal(err)
	}
	// The writer is called but source name is handled by the CLI layer.
	// Here we just verify no error.
}

func TestImportNonexistentFile(t *testing.T) {
	_, err := Run(Options{Path: "/nonexistent/path", Format: FormatAuto})
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestImportCorruptZIP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.zip")
	os.WriteFile(path, []byte("not a zip file"), 0600)

	_, err := Run(Options{Path: path, Format: FormatAuto})
	if err == nil {
		t.Error("expected error for corrupt zip")
	}
}

func TestImportIdempotent(t *testing.T) {
	dir := t.TempDir()
	content := `{"role":"user","content":"Same content"}
`
	path := filepath.Join(dir, "test.jsonl")
	os.WriteFile(path, []byte(content), 0600)

	w := &mockWriter{}
	r1, _ := Run(Options{Path: path, Format: FormatJSONL, Writer: w})
	r2, _ := Run(Options{Path: path, Format: FormatJSONL, Writer: w})

	// Both runs write — actual dedup happens in the Nexus write pipeline.
	// The importer just feeds memories; it doesn't deduplicate itself.
	if r1.Written != 1 || r2.Written != 1 {
		t.Errorf("r1.Written=%d, r2.Written=%d, want 1,1", r1.Written, r2.Written)
	}
}
