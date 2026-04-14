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
	"sync"
)

// GenericJSONLWatcher implements the Watcher interface for generic JSONL files.
//
// Accepts any JSONL file where each line matches:
//
//	{"role": "user|assistant|system", "content": "...", "timestamp": "..."}
//
// timestamp is optional (daemon fills in current time if absent).
// content and role are required.
//
// This is the release valve that lets power users run Ingest against any
// tool whose format we don't parse natively, by converting to this schema.
type GenericJSONLWatcher struct {
	cfg    Config
	logger *slog.Logger

	mu    sync.Mutex
	state WatcherState
}

// NewGenericJSONLWatcher creates a Generic JSONL watcher.
func NewGenericJSONLWatcher(cfg Config, logger *slog.Logger) *GenericJSONLWatcher {
	return &GenericJSONLWatcher{
		cfg:    cfg,
		logger: logger,
		state:  StateDisabled,
	}
}

func (w *GenericJSONLWatcher) Name() string      { return "generic_jsonl" }
func (w *GenericJSONLWatcher) SourceName() string { return "ingest.generic_jsonl" }

// DefaultPaths returns the user-configured generic_jsonl_paths. Unlike other
// watchers, there are no hardcoded defaults — the user must specify paths.
func (w *GenericJSONLWatcher) DefaultPaths() []string {
	return w.cfg.GenericJSONLPaths
}

func (w *GenericJSONLWatcher) Detect(ctx context.Context) (bool, string, error) {
	for _, p := range w.DefaultPaths() {
		info, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		// Accept both files and directories.
		return true, p, nil
	}
	return false, "", nil
}

func (w *GenericJSONLWatcher) State() WatcherState {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

func (w *GenericJSONLWatcher) SetState(s WatcherState) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = s
}

// genericJSONLLine is the expected line format.
type genericJSONLLine struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// Parse reads a generic JSONL file from fromOffset and returns extracted
// memories. Lines with missing role or content are skipped.
func (w *GenericJSONLWatcher) Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("generic_jsonl: lstat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymlinkRejected
	}
	if info.Size() > w.cfg.MaxFileSize {
		return nil, ErrFileTooLarge
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("generic_jsonl: open %s: %w", path, err)
	}
	defer f.Close()

	if fromOffset > 0 {
		if _, err := f.Seek(fromOffset, io.SeekStart); err != nil {
			return nil, fmt.Errorf("generic_jsonl: seek to %d: %w", fromOffset, err)
		}
	}

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
		lineLen := int64(len(line)) + 1
		lineNum++

		var entry genericJSONLLine
		if err := json.Unmarshal(line, &entry); err != nil {
			w.logger.Warn("generic_jsonl: skipping malformed line",
				"component", "ingest",
				"path", path,
				"offset", currentOffset,
				"line", lineNum,
				"error", err,
			)
			currentOffset += lineLen
			continue
		}

		if entry.Content == "" || entry.Role == "" {
			currentOffset += lineLen
			continue
		}

		validRole := entry.Role == "user" || entry.Role == "assistant" || entry.Role == "system"
		if !validRole {
			w.logger.Warn("generic_jsonl: skipping line with invalid role",
				"component", "ingest",
				"path", path,
				"role", entry.Role,
			)
			currentOffset += lineLen
			continue
		}

		memories = append(memories, Memory{
			Content:   entry.Content,
			Role:      entry.Role,
			Timestamp: parseTimestamp(entry.Timestamp),
			SourceMeta: map[string]string{
				"ingest_watcher": "generic_jsonl",
			},
			OriginalFile:   path,
			OriginalOffset: currentOffset,
		})

		currentOffset += lineLen
	}

	if err := scanner.Err(); err != nil {
		w.logger.Warn("generic_jsonl: scanner error",
			"component", "ingest",
			"path", path,
			"error", err,
		)
	}

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
