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
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// ClaudeDesktopWatcher parses Claude Desktop conversation JSON files.
// Claude Desktop stores conversations at platform-specific paths as JSON
// files with a messages array containing role, content, and timestamp fields.
type ClaudeDesktopWatcher struct {
	mu    sync.Mutex
	state WatcherState
}

func NewClaudeDesktopWatcher() *ClaudeDesktopWatcher {
	return &ClaudeDesktopWatcher{state: StateDisabled}
}

func (w *ClaudeDesktopWatcher) Name() string      { return "claude_desktop" }
func (w *ClaudeDesktopWatcher) SourceName() string { return "ingest.claude_desktop" }

func (w *ClaudeDesktopWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "Claude")}
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return nil
		}
		return []string{filepath.Join(appdata, "Claude")}
	default:
		return []string{filepath.Join(home, ".config", "Claude")}
	}
}

func (w *ClaudeDesktopWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

type claudeDesktopConversation struct {
	Messages []struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
		CreatedAt string `json:"created_at"`
	} `json:"messages"`
	Title string `json:"title"`
}

func (w *ClaudeDesktopWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("claude_desktop: read %s: %w", path, err)
	}

	contentHash := sha256.Sum256(data)

	var conv claudeDesktopConversation
	if err := json.Unmarshal(data, &conv); err != nil {
		return &ParseResult{NewOffset: int64(len(data)), LastHash: contentHash}, nil
	}

	var memories []Memory
	for _, msg := range conv.Messages {
		if msg.Content == "" {
			continue
		}
		ts := parseTimestampMulti(msg.Timestamp, msg.CreatedAt)
		memories = append(memories, Memory{
			Content:      msg.Content,
			Role:         normalizeRole(msg.Role),
			Timestamp:    ts,
			OriginalFile: path,
			SourceMeta:   map[string]string{"title": conv.Title},
		})
	}

	return &ParseResult{
		Memories:  memories,
		NewOffset: int64(len(data)),
		LastHash:  contentHash,
	}, nil
}

func (w *ClaudeDesktopWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *ClaudeDesktopWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}
