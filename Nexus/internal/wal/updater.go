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

package wal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
)

// WALUpdater is implemented by WAL and consumed by the queue worker to mark
// entries as delivered after a successful destination write. Callers MUST log
// errors from MarkDelivered at WARN level (not ERROR): a failure here is
// non-fatal because destination idempotency prevents duplicate writes on replay.
type WALUpdater interface {
	MarkDelivered(payloadID string) error
}

// MarkDelivered atomically rewrites the WAL entry for payloadID with
// status=DELIVERED. The temp file is written to filepath.Dir(segment),
// guaranteeing os.Rename atomicity (same filesystem, no EXDEV).
//
// The WAL mutex is held for the duration so concurrent Append calls are
// serialised. This is intentional: correctness over throughput. MarkDelivered
// is called off the hot write path (after destination I/O completes).
func (w *WAL) MarkDelivered(payloadID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	segs, err := w.segments()
	if err != nil {
		return fmt.Errorf("wal: mark delivered: list segments: %w", err)
	}

	for _, seg := range segs {
		isActive := seg == w.currentPath

		// Close the active segment file handle before rename. On Windows,
		// renaming over an open file descriptor fails with ACCESS_DENIED.
		if isActive && w.current != nil {
			if err := w.current.Close(); err != nil {
				return fmt.Errorf("wal: close active segment before rewrite: %w", err)
			}
			w.current = nil
		}

		found, markErr := w.markDeliveredInSegment(seg, payloadID)

		if isActive {
			// Always reopen the active segment so Append continues to work.
			f, reopenErr := os.OpenFile(seg, os.O_APPEND|os.O_RDWR, 0600)
			if reopenErr != nil {
				return fmt.Errorf("wal: reopen active segment: %w", reopenErr)
			}
			info, statErr := f.Stat()
			if statErr != nil {
				f.Close()
				return fmt.Errorf("wal: stat active segment after reopen: %w", statErr)
			}
			w.current = f
			w.currentSize = info.Size()
		}

		if markErr != nil {
			return markErr
		}
		if found {
			return nil
		}
	}

	return fmt.Errorf("wal: payload_id %q not found in any segment", payloadID)
}

// markDeliveredInSegment scans segPath for an entry matching payloadID, rewrites
// it with status=DELIVERED and a fresh CRC32, and atomically replaces the segment
// via a temp file + rename on the same filesystem.
func (w *WAL) markDeliveredInSegment(segPath, payloadID string) (bool, error) {
	f, err := os.Open(segPath)
	if err != nil {
		return false, fmt.Errorf("wal: open segment %q: %w", segPath, err)
	}

	var lines []string
	found := false

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			// Partial line — preserve as-is; it will be skipped on replay.
			lines = append(lines, line)
			continue
		}

		jsonBytes := []byte(parts[0])
		var entry Entry
		if err := json.Unmarshal(jsonBytes, &entry); err != nil {
			// Unparseable line — preserve as-is.
			lines = append(lines, line)
			continue
		}

		if entry.PayloadID == payloadID && !found {
			entry.Status = StatusDelivered
			updated, marshalErr := json.Marshal(entry)
			if marshalErr != nil {
				// Abort before any write — close file and surface error.
				f.Close()
				return false, fmt.Errorf("wal: marshal updated entry: %w", marshalErr)
			}
			checksum := crc32.ChecksumIEEE(updated)
			line = fmt.Sprintf("%s\t%08x", updated, checksum)
			found = true
		}

		lines = append(lines, line)
	}

	scanErr := scanner.Err()
	// Close the source file before rename so Windows allows overwrite.
	f.Close()

	if scanErr != nil {
		return false, fmt.Errorf("wal: scan segment %q: %w", segPath, scanErr)
	}
	if !found {
		return false, nil
	}

	// Write updated content to a temp file in the same directory as the segment.
	// This guarantees os.Rename is on the same filesystem (no EXDEV failure).
	tmp, err := os.CreateTemp(filepath.Dir(segPath), "wal-*.tmp")
	if err != nil {
		return false, fmt.Errorf("wal: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	done := false
	defer func() {
		// On success done=true; the file was renamed away so Close/Remove are
		// no-ops. On failure, clean up the temp file.
		tmp.Close()
		if !done {
			os.Remove(tmpPath) // nolint: best-effort cleanup on failure path
		}
	}()

	// Best-effort chmod: mandatory on Unix (0600), advisory on Windows.
	// The rename replaces the segment so permissions must match.
	_ = tmp.Chmod(0600) // nolint: non-fatal on Windows; original segment is 0600

	for _, line := range lines {
		if _, err := fmt.Fprintln(tmp, line); err != nil {
			return false, fmt.Errorf("wal: write temp file: %w", err)
		}
	}

	if err := tmp.Sync(); err != nil {
		return false, fmt.Errorf("wal: fsync temp file: %w", err)
	}
	// Explicit close before Rename: required on Windows.
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("wal: close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, segPath); err != nil {
		return false, fmt.Errorf("wal: rename temp to segment: %w", err)
	}

	done = true
	return true, nil
}
