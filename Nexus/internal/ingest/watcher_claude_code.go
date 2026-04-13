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
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ClaudeCodeWatcher implements the Watcher interface for Claude Code.
//
// Claude Code stores conversations in ~/.claude/projects/<project-hash>/<session>.jsonl.
// Each line is a JSON object. We parse "user" and "assistant" type lines;
// everything else (tool_use, tool_result, summary) is ignored in v0.1.3.
//
// message.content can be either a string or an array of content blocks;
// we handle both.
type ClaudeCodeWatcher struct {
	cfg    Config
	logger *slog.Logger

	mu    sync.Mutex
	state WatcherState
}

// NewClaudeCodeWatcher creates a Claude Code watcher.
func NewClaudeCodeWatcher(cfg Config, logger *slog.Logger) *ClaudeCodeWatcher {
	return &ClaudeCodeWatcher{
		cfg:    cfg,
		logger: logger,
		state:  StateDisabled,
	}
}

func (w *ClaudeCodeWatcher) Name() string       { return "claude_code" }
func (w *ClaudeCodeWatcher) SourceName() string  { return "ingest.claude_code" }

func (w *ClaudeCodeWatcher) DefaultPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{filepath.Join(home, ".claude", "projects")}
}

func (w *ClaudeCodeWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue // symlinks not followed
		}
		if info.IsDir() {
			return true, p, nil
		}
	}
	return false, "", nil
}

func (w *ClaudeCodeWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *ClaudeCodeWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}

// Parse reads a Claude Code JSONL file from fromOffset and returns extracted
// memories. Bad lines are skipped with a warning. The partial line at EOF
// (if any) is not ingested.
func (w *ClaudeCodeWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	// Security: reject symlinks.
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("claude_code: lstat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymlinkRejected
	}

	// Security: reject files exceeding max size.
	if info.Size() > w.cfg.MaxFileSize {
		return nil, ErrFileTooLarge
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("claude_code: open %s: %w", path, err)
	}
	defer f.Close()

	// Seek to fromOffset.
	if fromOffset > 0 {
		if _, err := f.Seek(fromOffset, io.SeekStart); err != nil {
			return nil, fmt.Errorf("claude_code: seek to %d: %w", fromOffset, err)
		}
	}

	// Extract the project hash from the parent directory name.
	projectHash := filepath.Base(filepath.Dir(path))

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), w.cfg.MaxLineLength)

	var memories []Memory
	currentOffset := fromOffset
	lineNum := 0

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line := scanner.Bytes()
		lineLen := int64(len(line)) + 1 // +1 for newline
		lineNum++

		var entry claudeCodeEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			w.logger.Warn("claude_code: skipping malformed line",
				"component", "ingest",
				"path", path,
				"offset", currentOffset,
				"line", lineNum,
				"error", err,
			)
			currentOffset += lineLen
			continue
		}

		// Only parse user and assistant messages.
		if entry.Type != "user" && entry.Type != "assistant" {
			currentOffset += lineLen
			continue
		}

		content := entry.Message.Content.Text()
		if content == "" {
			currentOffset += lineLen
			continue
		}

		ts := parseTimestamp(entry.Timestamp)

		memories = append(memories, Memory{
			Content: content,
			Role:    entry.Message.Role,
			Model:   entry.Message.Model,
			Timestamp: ts,
			SourceMeta: map[string]string{
				"ingest_watcher":      "claude_code",
				"claude_session_id":   entry.SessionID,
				"claude_line_type":    entry.Type,
				"claude_project_hash": projectHash,
			},
			OriginalFile:   path,
			OriginalOffset: currentOffset,
		})

		currentOffset += lineLen
	}

	if err := scanner.Err(); err != nil {
		w.logger.Warn("claude_code: scanner error (possible truncated line at EOF)",
			"component", "ingest",
			"path", path,
			"error", err,
		)
		// Don't fail — return what we have. The truncated line is not counted
		// in currentOffset, so the next parse will re-read it.
	}

	// Compute hash of last 64 bytes for truncation detection.
	var lastHash [32]byte
	if currentOffset > fromOffset {
		hashStart := currentOffset - 64
		if hashStart < fromOffset {
			hashStart = fromOffset
		}
		buf := make([]byte, currentOffset-hashStart)
		if _, err := f.ReadAt(buf, hashStart); err == nil {
			lastHash = sha256.Sum256(buf)
		}
	}

	return &ParseResult{
		Memories:  memories,
		NewOffset: currentOffset,
		LastHash:  lastHash,
	}, nil
}

// claudeCodeEntry represents a single line in a Claude Code JSONL file.
type claudeCodeEntry struct {
	Type      string              `json:"type"`
	Message   claudeCodeMessage   `json:"message"`
	Timestamp string              `json:"timestamp"`
	SessionID string              `json:"sessionId"`
}

// claudeCodeMessage is the message field inside a Claude Code entry.
type claudeCodeMessage struct {
	Role    string              `json:"role"`
	Content claudeCodeContent   `json:"content"`
	Model   string              `json:"model"`
}

// claudeCodeContent handles both string and array content formats.
// Claude Code emits content as either a plain string or an array of
// content blocks like [{"type":"text","text":"..."}].
type claudeCodeContent struct {
	raw json.RawMessage
}

func (c *claudeCodeContent) UnmarshalJSON(data []byte) error {
	c.raw = make(json.RawMessage, len(data))
	copy(c.raw, data)
	return nil
}

// Text extracts the text content, handling both string and array formats.
func (c *claudeCodeContent) Text() string {
	if len(c.raw) == 0 {
		return ""
	}

	// Try string first.
	var s string
	if err := json.Unmarshal(c.raw, &s); err == nil {
		return s
	}

	// Try array of content blocks.
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(c.raw, &blocks); err == nil {
		var result string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				if result != "" {
					result += "\n"
				}
				result += b.Text
			}
		}
		return result
	}

	return ""
}

// parseTimestamp parses an ISO 8601 timestamp string to Unix milliseconds.
// Returns 0 if parsing fails.
func parseTimestamp(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// Try without nanoseconds.
		t, err = time.Parse("2006-01-02T15:04:05Z", s)
		if err != nil {
			return 0
		}
	}
	return t.UnixMilli()
}
