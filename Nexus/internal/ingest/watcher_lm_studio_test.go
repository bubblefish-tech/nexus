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

func TestLMStudioWatcher_Name(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcher()
	if w.Name() != "lm_studio" {
		t.Errorf("Name() = %q, want lm_studio", w.Name())
	}
	if w.SourceName() != "ingest.lm_studio" {
		t.Errorf("SourceName() = %q, want ingest.lm_studio", w.SourceName())
	}
}

func TestLMStudioWatcher_DefaultPaths_NonEmpty(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcher()
	paths := w.DefaultPaths()
	if len(paths) == 0 {
		t.Fatal("DefaultPaths() returned empty slice; expected at least one candidate")
	}
	for _, p := range paths {
		if p == "" {
			t.Error("DefaultPaths() returned an empty string")
		}
	}
}

func TestLMStudioWatcher_StateTransitions(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcher()
	if w.State() != StateDisabled {
		t.Errorf("initial state = %v, want StateDisabled", w.State())
	}
	w.SetState(StateActive)
	if w.State() != StateActive {
		t.Errorf("after SetState(Active) = %v, want StateActive", w.State())
	}
}

func TestLMStudioWatcher_Parse_SampleChat(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	result, err := w.Parse(context.Background(), filepath.Join("testdata", "lm_studio", "sample_chat.json"), 0)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(result.Memories) != 4 {
		t.Fatalf("expected 4 memories, got %d", len(result.Memories))
	}
	if result.Memories[0].Role != "user" {
		t.Errorf("first memory role = %q, want user", result.Memories[0].Role)
	}
	if result.Memories[0].Content != "What is the capital of France?" {
		t.Errorf("first memory content = %q", result.Memories[0].Content)
	}
	if result.Memories[1].Role != "assistant" {
		t.Errorf("second memory role = %q, want assistant", result.Memories[1].Role)
	}
}

func TestLMStudioWatcher_Parse_MetaFields(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	result, err := w.Parse(context.Background(), filepath.Join("testdata", "lm_studio", "sample_chat.json"), 0)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(result.Memories) == 0 {
		t.Fatal("no memories returned")
	}
	m := result.Memories[0]
	if m.SourceMeta["ingest_watcher"] != "lm_studio" {
		t.Errorf("ingest_watcher meta = %q, want lm_studio", m.SourceMeta["ingest_watcher"])
	}
	if m.SourceMeta["lms_chat_id"] != "lms-session-001" {
		t.Errorf("lms_chat_id = %q, want lms-session-001", m.SourceMeta["lms_chat_id"])
	}
	if m.Model == "" {
		t.Error("Model should be populated from the conversation-level model field")
	}
}

func TestLMStudioWatcher_Parse_TimestampParsed(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	result, err := w.Parse(context.Background(), filepath.Join("testdata", "lm_studio", "sample_chat.json"), 0)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if result.Memories[0].Timestamp == 0 {
		t.Error("Timestamp should be non-zero when createdAt is present")
	}
}

func TestLMStudioWatcher_Parse_AltTimestampField(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	result, err := w.Parse(context.Background(), filepath.Join("testdata", "lm_studio", "alt_timestamp_field.json"), 0)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(result.Memories) != 2 {
		t.Fatalf("expected 2 memories from alt format, got %d", len(result.Memories))
	}
	if result.Memories[0].Timestamp == 0 {
		t.Error("Timestamp should be non-zero from 'timestamp' field")
	}
}

func TestLMStudioWatcher_Parse_EmptyMessages(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	result, err := w.Parse(context.Background(), filepath.Join("testdata", "lm_studio", "empty_chat.json"), 0)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if len(result.Memories) != 0 {
		t.Errorf("expected 0 memories from empty chat, got %d", len(result.Memories))
	}
}

func TestLMStudioWatcher_Parse_Malformed(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	_, err := w.Parse(context.Background(), filepath.Join("testdata", "lm_studio", "malformed.json"), 0)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestLMStudioWatcher_Parse_FileNotExist(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	_, err := w.Parse(context.Background(), filepath.Join("testdata", "lm_studio", "nonexistent.json"), 0)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestLMStudioWatcher_Parse_SymlinkRejected(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(`{"messages":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks not supported on this platform")
	}
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	_, err := w.Parse(context.Background(), link, 0)
	if err != ErrSymlinkRejected {
		t.Errorf("expected ErrSymlinkRejected, got %v", err)
	}
}

func TestLMStudioWatcher_Parse_FileTooLarge(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	bigFile := filepath.Join(dir, "big.json")
	// Write a minimal valid JSON file, then pretend it's huge by using a tiny MaxFileSize.
	if err := os.WriteFile(bigFile, []byte(`{"messages":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MaxFileSize = 5 // 5 bytes — the file is larger
	w := NewLMStudioWatcherWithConfig(cfg, nil)
	_, err := w.Parse(context.Background(), bigFile, 0)
	if err != ErrFileTooLarge {
		t.Errorf("expected ErrFileTooLarge, got %v", err)
	}
}

func TestLMStudioWatcher_Parse_HashPopulated(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	result, err := w.Parse(context.Background(), filepath.Join("testdata", "lm_studio", "sample_chat.json"), 0)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if result.LastHash == [32]byte{} {
		t.Error("LastHash should be non-zero after a successful parse")
	}
	if result.NewOffset == 0 {
		t.Error("NewOffset should be non-zero after a successful parse")
	}
}

func TestLMStudioWatcher_Parse_ContextCancelled(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	w := NewLMStudioWatcherWithConfig(DefaultConfig(), nil)
	_, err := w.Parse(ctx, filepath.Join("testdata", "lm_studio", "sample_chat.json"), 0)
	if err == nil {
		t.Fatal("expected error when context is cancelled, got nil")
	}
}

func TestLMStudioWatcher_Detect_NoDir(t *testing.T) {
	t.Helper()
	w := NewLMStudioWatcher()
	// Detect should not error even when no LM Studio directory exists.
	detected, _, err := w.Detect(context.Background())
	if err != nil {
		t.Fatalf("Detect() returned unexpected error: %v", err)
	}
	// detected may be true on a machine with LM Studio — either is acceptable.
	_ = detected
}
