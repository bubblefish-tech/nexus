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

// PerplexityCometWatcher parses Perplexity Comet local conversation files.
// Perplexity stores query/response pairs as JSON in platform-specific paths.
type PerplexityCometWatcher struct {
	mu    sync.Mutex
	state WatcherState
}

func NewPerplexityCometWatcher() *PerplexityCometWatcher {
	return &PerplexityCometWatcher{state: StateDisabled}
}

func (w *PerplexityCometWatcher) Name() string      { return "perplexity_comet" }
func (w *PerplexityCometWatcher) SourceName() string { return "ingest.perplexity_comet" }

func (w *PerplexityCometWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return []string{filepath.Join(home, "Library", "Application Support", "Perplexity")}
	case "windows":
		appdata := os.Getenv("LOCALAPPDATA")
		if appdata == "" {
			return nil
		}
		return []string{filepath.Join(appdata, "Perplexity")}
	default:
		return []string{filepath.Join(home, ".config", "perplexity")}
	}
}

func (w *PerplexityCometWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

type perplexityThread struct {
	Title    string `json:"title"`
	Messages []struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		Query     string `json:"query"`
		Answer    string `json:"answer"`
		Timestamp string `json:"timestamp"`
	} `json:"messages"`
	Entries []struct {
		Query     string `json:"query"`
		Answer    string `json:"answer"`
		Timestamp string `json:"timestamp"`
	} `json:"entries"`
}

func (w *PerplexityCometWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("perplexity_comet: read %s: %w", path, err)
	}

	contentHash := sha256.Sum256(data)

	var thread perplexityThread
	if err := json.Unmarshal(data, &thread); err != nil {
		return &ParseResult{NewOffset: int64(len(data)), LastHash: contentHash}, nil
	}

	var memories []Memory

	for _, msg := range thread.Messages {
		content := msg.Content
		if content == "" {
			content = msg.Query
		}
		if content == "" {
			content = msg.Answer
		}
		if content == "" {
			continue
		}
		role := normalizeRole(msg.Role)
		if role == "" && msg.Query != "" {
			role = "user"
		}
		if role == "" && msg.Answer != "" {
			role = "assistant"
		}
		memories = append(memories, Memory{
			Content:      content,
			Role:         role,
			Timestamp:    parseTimestampMulti(msg.Timestamp),
			OriginalFile: path,
			SourceMeta:   map[string]string{"title": thread.Title},
		})
	}

	for _, entry := range thread.Entries {
		if entry.Query != "" {
			memories = append(memories, Memory{
				Content:      entry.Query,
				Role:         "user",
				Timestamp:    parseTimestampMulti(entry.Timestamp),
				OriginalFile: path,
				SourceMeta:   map[string]string{"title": thread.Title},
			})
		}
		if entry.Answer != "" {
			memories = append(memories, Memory{
				Content:      entry.Answer,
				Role:         "assistant",
				Timestamp:    parseTimestampMulti(entry.Timestamp),
				OriginalFile: path,
				SourceMeta:   map[string]string{"title": thread.Title},
			})
		}
	}

	return &ParseResult{
		Memories:  memories,
		NewOffset: int64(len(data)),
		LastHash:  contentHash,
	}, nil
}

func (w *PerplexityCometWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *PerplexityCometWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}
