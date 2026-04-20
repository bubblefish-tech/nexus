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

// LMStudioWatcher implements the Watcher interface for LM Studio.
//
// LM Studio stores conversations as individual JSON files under one of:
//   - ~/.lmstudio/conversations/           (Windows / Linux, older builds)
//   - ~/.cache/lm-studio/conversations/    (macOS / Linux, newer builds)
//
// Each file is a complete JSON object rewritten on every save (like Cursor),
// so offset tracking is replaced by whole-file hash comparison. Content hash
// idempotency in the Manager handles deduplication across repeated parses.
//
// Field layout handled:
//
//	{"id":..., "title":..., "model":..., "createdAt":...,
//	 "messages":[{"role":..., "content":..., "createdAt"|"timestamp":...}]}
type LMStudioWatcher struct {
	cfg    Config
	logger *slog.Logger

	mu    sync.Mutex
	state WatcherState
}

// NewLMStudioWatcher creates an LM Studio watcher.
func NewLMStudioWatcher() *LMStudioWatcher {
	return &LMStudioWatcher{state: StateDisabled}
}

// NewLMStudioWatcherWithConfig creates an LM Studio watcher with a config and logger.
func NewLMStudioWatcherWithConfig(cfg Config, logger *slog.Logger) *LMStudioWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &LMStudioWatcher{cfg: cfg, logger: logger, state: StateDisabled}
}

func (w *LMStudioWatcher) Name() string       { return "lm_studio" }
func (w *LMStudioWatcher) SourceName() string  { return "ingest.lm_studio" }

// DefaultPaths returns the OS-specific candidate conversation directories.
func (w *LMStudioWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	switch runtime.GOOS {
	case "windows":
		appdata := os.Getenv("APPDATA")
		paths := []string{filepath.Join(home, ".lmstudio", "conversations")}
		if appdata != "" {
			paths = append(paths, filepath.Join(appdata, "LM Studio", "conversations"))
		}
		return paths
	case "darwin":
		return []string{
			filepath.Join(home, ".cache", "lm-studio", "conversations"),
			filepath.Join(home, ".lmstudio", "conversations"),
		}
	default: // linux
		return []string{
			filepath.Join(home, ".cache", "lm-studio", "conversations"),
			filepath.Join(home, ".lmstudio", "conversations"),
		}
	}
}

// Detect checks whether any known LM Studio conversation directory exists.
func (w *LMStudioWatcher) Detect(ctx context.Context) (bool, string, error) {
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

func (w *LMStudioWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *LMStudioWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}

// lmStudioFile is the JSON structure of an LM Studio conversation file.
type lmStudioFile struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	Model    string           `json:"model"`
	Messages []lmStudioMessage `json:"messages"`
}

// lmStudioMessage handles both "createdAt" and "timestamp" time fields.
type lmStudioMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`  // newer builds
	Timestamp string `json:"timestamp"`  // older builds
}

// ts returns whichever timestamp field is populated.
func (m *lmStudioMessage) ts() string {
	if m.CreatedAt != "" {
		return m.CreatedAt
	}
	return m.Timestamp
}

// Parse reads an LM Studio conversation JSON file and returns all messages as
// memories. Since LM Studio rewrites the whole file on each save, fromOffset
// is used only for Watcher interface compatibility — the file is always read
// from the beginning. The content hash idempotency layer handles deduplication.
func (w *LMStudioWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	// Security: reject symlinks.
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("lm_studio: lstat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymlinkRejected
	}

	if w.cfg.MaxFileSize > 0 && info.Size() > w.cfg.MaxFileSize {
		return nil, ErrFileTooLarge
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lm_studio: read %s: %w", path, err)
	}

	var cf lmStudioFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("lm_studio: parse %s: %w", path, err)
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
			Model:     cf.Model,
			Timestamp: parseTimestamp(msg.ts()),
			SourceMeta: map[string]string{
				"ingest_watcher":  "lm_studio",
				"lms_chat_id":     cf.ID,
				"lms_chat_title":  cf.Title,
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
