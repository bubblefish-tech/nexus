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

// ChatGPTDesktopWatcher parses ChatGPT Desktop conversation JSON files.
// ChatGPT Desktop stores conversations in JSON format with a messages array.
type ChatGPTDesktopWatcher struct {
	mu    sync.Mutex
	state WatcherState
}

func NewChatGPTDesktopWatcher() *ChatGPTDesktopWatcher {
	return &ChatGPTDesktopWatcher{state: StateDisabled}
}

func (w *ChatGPTDesktopWatcher) Name() string      { return "chatgpt_desktop" }
func (w *ChatGPTDesktopWatcher) SourceName() string { return "ingest.chatgpt_desktop" }

func (w *ChatGPTDesktopWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "com.openai.chat")}
	case "windows":
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			return nil
		}
		return []string{filepath.Join(appdata, "ChatGPT")}
	default:
		return []string{filepath.Join(home, ".config", "ChatGPT")}
	}
}

func (w *ChatGPTDesktopWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

type chatGPTConversation struct {
	Title   string `json:"title"`
	Mapping map[string]struct {
		Message *struct {
			Author struct {
				Role string `json:"role"`
			} `json:"author"`
			Content struct {
				Parts []interface{} `json:"parts"`
			} `json:"content"`
			CreateTime float64 `json:"create_time"`
		} `json:"message"`
	} `json:"mapping"`
	Messages []struct {
		Role       string  `json:"role"`
		Content    string  `json:"content"`
		CreateTime float64 `json:"create_time"`
	} `json:"messages"`
}

func (w *ChatGPTDesktopWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("chatgpt_desktop: read %s: %w", path, err)
	}

	contentHash := sha256.Sum256(data)

	var conv chatGPTConversation
	if err := json.Unmarshal(data, &conv); err != nil {
		return &ParseResult{NewOffset: int64(len(data)), LastHash: contentHash}, nil
	}

	var memories []Memory

	if conv.Mapping != nil {
		for _, node := range conv.Mapping {
			if node.Message == nil {
				continue
			}
			var content string
			for _, part := range node.Message.Content.Parts {
				if s, ok := part.(string); ok && s != "" {
					content = s
					break
				}
			}
			if content == "" {
				continue
			}
			var ts int64
			if node.Message.CreateTime > 0 {
				ts = int64(node.Message.CreateTime * 1000)
			}
			memories = append(memories, Memory{
				Content:      content,
				Role:         normalizeRole(node.Message.Author.Role),
				Timestamp:    ts,
				OriginalFile: path,
				SourceMeta:   map[string]string{"title": conv.Title},
			})
		}
	}

	for _, msg := range conv.Messages {
		if msg.Content == "" {
			continue
		}
		var ts int64
		if msg.CreateTime > 0 {
			ts = int64(msg.CreateTime * 1000)
		}
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

func (w *ChatGPTDesktopWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *ChatGPTDesktopWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}
