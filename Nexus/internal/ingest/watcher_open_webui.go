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
	"sync"
)

// OpenWebUIWatcher parses Open WebUI exported conversation JSON files.
// Open WebUI stores conversations in ~/.open-webui/ and supports JSON exports.
type OpenWebUIWatcher struct {
	mu    sync.Mutex
	state WatcherState
}

func NewOpenWebUIWatcher() *OpenWebUIWatcher {
	return &OpenWebUIWatcher{state: StateDisabled}
}

func (w *OpenWebUIWatcher) Name() string      { return "open_webui" }
func (w *OpenWebUIWatcher) SourceName() string { return "ingest.open_webui" }

func (w *OpenWebUIWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{filepath.Join(home, ".open-webui")}
}

func (w *OpenWebUIWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

type openWebUIChat struct {
	Title    string `json:"title"`
	Messages []struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		Timestamp string `json:"timestamp"`
		CreatedAt string `json:"created_at"`
	} `json:"messages"`
	Chat struct {
		Messages []struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Timestamp string `json:"timestamp"`
		} `json:"messages"`
	} `json:"chat"`
}

func (w *OpenWebUIWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open_webui: read %s: %w", path, err)
	}

	contentHash := sha256.Sum256(data)

	var chat openWebUIChat
	if err := json.Unmarshal(data, &chat); err != nil {
		return &ParseResult{NewOffset: int64(len(data)), LastHash: contentHash}, nil
	}

	var memories []Memory

	msgs := chat.Messages
	if len(msgs) == 0 && len(chat.Chat.Messages) > 0 {
		for _, m := range chat.Chat.Messages {
			msgs = append(msgs, struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				Timestamp string `json:"timestamp"`
				CreatedAt string `json:"created_at"`
			}{Role: m.Role, Content: m.Content, Timestamp: m.Timestamp})
		}
	}

	for _, msg := range msgs {
		if msg.Content == "" {
			continue
		}
		memories = append(memories, Memory{
			Content:      msg.Content,
			Role:         normalizeRole(msg.Role),
			Timestamp:    parseTimestampMulti(msg.Timestamp, msg.CreatedAt),
			OriginalFile: path,
			SourceMeta:   map[string]string{"title": chat.Title},
		})
	}

	return &ParseResult{
		Memories:  memories,
		NewOffset: int64(len(data)),
		LastHash:  contentHash,
	}, nil
}

func (w *OpenWebUIWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *OpenWebUIWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}
