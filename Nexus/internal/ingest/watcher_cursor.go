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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// CursorWatcher implements the Watcher interface for the Cursor editor.
//
// Cursor stores chat history in ~/.cursor/chat-history/*.json (as of April 2026).
// Each file is a JSON object with an "id", "title", and "messages" array.
// Unlike Claude Code's append-only JSONL, Cursor rewrites the whole file
// on each save, so offset tracking is replaced by whole-file hash comparison.
// The content hash idempotency layer deduplicates on repeated full parses.
type CursorWatcher struct {
	cfg    Config
	logger *slog.Logger

	mu    sync.Mutex
	state WatcherState
}

// NewCursorWatcher creates a Cursor watcher.
func NewCursorWatcher(cfg Config, logger *slog.Logger) *CursorWatcher {
	return &CursorWatcher{
		cfg:    cfg,
		logger: logger,
		state:  StateDisabled,
	}
}

func (w *CursorWatcher) Name() string      { return "cursor" }
func (w *CursorWatcher) SourceName() string { return "ingest.cursor" }

func (w *CursorWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	switch runtime.GOOS {
	case "windows":
		return []string{filepath.Join(home, ".cursor", "chat-history")}
	case "darwin":
		return []string{filepath.Join(home, ".cursor", "chat-history")}
	default: // linux
		return []string{filepath.Join(home, ".cursor", "chat-history")}
	}
}

func (w *CursorWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

func (w *CursorWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *CursorWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}

// cursorFile is the JSON structure of a Cursor chat history file.
type cursorFile struct {
	ID       string          `json:"id"`
	Title    string          `json:"title"`
	Messages []cursorMessage `json:"messages"`
}

type cursorMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// Parse reads a Cursor chat JSON file and returns all messages as memories.
// Since Cursor rewrites the whole file, fromOffset is used only for
// compatibility with the Watcher interface — the file is always read from
// the beginning. The content hash idempotency layer handles deduplication.
func (w *CursorWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	// Security: reject symlinks.
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("cursor: lstat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymlinkRejected
	}
	if info.Size() > w.cfg.MaxFileSize {
		return nil, ErrFileTooLarge
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cursor: read %s: %w", path, err)
	}

	var cf cursorFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("cursor: parse %s: %w", path, err)
	}

	memories := make([]Memory, 0, len(cf.Messages))
	for i, msg := range cf.Messages {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if msg.Content == "" {
			continue
		}
		if msg.Role != "user" && msg.Role != "assistant" && msg.Role != "system" {
			continue
		}

		memories = append(memories, Memory{
			Content:   msg.Content,
			Role:      msg.Role,
			Timestamp: parseTimestamp(msg.Timestamp),
			SourceMeta: map[string]string{
				"ingest_watcher": "cursor",
				"cursor_chat_id": cf.ID,
				"cursor_title":   cf.Title,
			},
			OriginalFile:   path,
			OriginalOffset: int64(i),
		})
	}

	fileHash := sha256.Sum256(data)

	return &ParseResult{
		Memories:  memories,
		NewOffset: info.Size(),
		LastHash:  fileHash,
	}, nil
}
