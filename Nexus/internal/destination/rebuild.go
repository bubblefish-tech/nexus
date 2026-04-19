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

package destination

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/bubblefish-tech/nexus/internal/wal"
)

// RebuildOptions configures a destination rebuild from WAL replay.
type RebuildOptions struct {
	// WALDir is the directory containing WAL segment files.
	WALDir string
	// DestPath is the SQLite database file path.
	DestPath string
	// DestName filters entries by destination name. Empty means all entries.
	DestName string
	// BackupOld renames the old DB file before rebuilding (default true).
	BackupOld bool
	// Logger for structured output.
	Logger *slog.Logger
}

// RebuildResult summarizes the rebuild outcome.
type RebuildResult struct {
	EntriesReplayed int    `json:"entries_replayed"`
	EntriesWritten  int    `json:"entries_written"`
	EntriesSkipped  int    `json:"entries_skipped"`
	BackupPath      string `json:"backup_path,omitempty"`
	Duration        string `json:"duration"`
}

// Rebuild replays WAL entries into a fresh destination database. The old
// database file is backed up (unless BackupOld is false) and a new empty
// database is created from scratch.
//
// Reference: v0.1.3 Build Plan Section 6.6.
func Rebuild(opts RebuildOptions) (RebuildResult, error) {
	if opts.WALDir == "" {
		return RebuildResult{}, fmt.Errorf("destination: WAL directory is required")
	}
	if opts.DestPath == "" {
		return RebuildResult{}, fmt.Errorf("destination: destination path is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	start := time.Now()
	result := RebuildResult{}

	// Backup old database file if it exists.
	if opts.BackupOld {
		if _, err := os.Stat(opts.DestPath); err == nil {
			backupPath := fmt.Sprintf("%s.bak.%d", opts.DestPath, time.Now().Unix())
			if err := os.Rename(opts.DestPath, backupPath); err != nil {
				return result, fmt.Errorf("destination: backup old file: %w", err)
			}
			result.BackupPath = backupPath
			opts.Logger.Info("destination: old database backed up",
				"component", "rebuild",
				"backup_path", backupPath,
			)
		}
	} else {
		// Remove old file if it exists (no backup requested).
		_ = os.Remove(opts.DestPath)
	}

	// Open WAL for replay (read-only — we only call Replay, not Append).
	w, err := wal.Open(opts.WALDir, 50, opts.Logger)
	if err != nil {
		return result, fmt.Errorf("destination: open WAL: %w", err)
	}
	defer func() { _ = w.Close() }()

	// Open fresh SQLite destination.
	dest, err := OpenSQLite(opts.DestPath, opts.Logger)
	if err != nil {
		return result, fmt.Errorf("destination: open fresh database: %w", err)
	}
	defer func() { _ = dest.Close() }()

	// Replay WAL entries into the fresh destination.
	if err := w.Replay(func(entry wal.Entry) {
		result.EntriesReplayed++

		// Filter by destination name if specified.
		if opts.DestName != "" && entry.Destination != opts.DestName {
			result.EntriesSkipped++
			return
		}

		// Unmarshal the full TranslatedPayload from the WAL entry.
		var tp TranslatedPayload
		if err := json.Unmarshal(entry.Payload, &tp); err != nil {
			opts.Logger.Warn("destination: skip entry with unparseable payload",
				"component", "rebuild",
				"payload_id", entry.PayloadID,
				"error", err,
			)
			result.EntriesSkipped++
			return
		}

		if err := dest.Write(tp); err != nil {
			opts.Logger.Warn("destination: write failed during rebuild",
				"component", "rebuild",
				"payload_id", entry.PayloadID,
				"error", err,
			)
			result.EntriesSkipped++
			return
		}

		result.EntriesWritten++
	}); err != nil {
		return result, fmt.Errorf("destination: WAL replay: %w", err)
	}

	result.Duration = time.Since(start).Round(time.Millisecond).String()

	opts.Logger.Info("destination: rebuild complete",
		"component", "rebuild",
		"entries_replayed", result.EntriesReplayed,
		"entries_written", result.EntriesWritten,
		"entries_skipped", result.EntriesSkipped,
		"duration", result.Duration,
	)

	return result, nil
}
