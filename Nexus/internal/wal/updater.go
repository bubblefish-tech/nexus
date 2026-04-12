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
	"log/slog"
	"os"
	"path/filepath"

	"github.com/BubbleFish-Nexus/internal/fsutil"
)

// WALUpdater is implemented by WAL and consumed by the queue worker to mark
// entries after a destination write attempt. Callers MUST log errors from
// MarkDelivered/MarkDeliveredBatch at WARN level (not ERROR): a failure is
// non-fatal because destination idempotency prevents duplicate writes on
// replay. Callers MUST log errors from MarkPermanentFailure at ERROR level.
type WALUpdater interface {
	MarkDelivered(payloadID string) error
	MarkDeliveredBatch(payloadIDs []string) error
	MarkPermanentFailure(payloadID string) error
}

// MarkDelivered atomically rewrites the WAL entry for payloadID with
// status=DELIVERED. This rewrites the entire segment file containing
// the entry, which is O(segment_size).
//
// HOT-PATH CALLERS MUST USE MarkDeliveredBatch instead, which amortizes
// the rewrite to one operation per segment per batch. The singular
// variant exists for low-frequency callers (recovery tools) and tests.
func (w *WAL) MarkDelivered(payloadID string) error {
	return w.markStatus(payloadID, StatusDelivered)
}

// MarkDeliveredBatch atomically rewrites WAL entries for all payloadIDs with
// status=DELIVERED in a single segment rewrite pass. This is O(N) for N
// entries instead of O(N²) when calling MarkDelivered N times individually.
//
// The WAL mutex is held for the duration so concurrent Append calls are
// serialised. This is intentional: correctness over throughput.
// MarkDeliveredBatch is called off the hot write path.
func (w *WAL) MarkDeliveredBatch(payloadIDs []string) error {
	if len(payloadIDs) == 0 {
		return nil
	}
	return w.markStatusBatch(payloadIDs, StatusDelivered)
}

// MarkPermanentFailure atomically rewrites the WAL entry for payloadID with
// status=PERMANENT_FAILURE. Called by the queue worker when all retries are
// exhausted. The entry will never be re-enqueued on replay.
func (w *WAL) MarkPermanentFailure(payloadID string) error {
	return w.markStatus(payloadID, StatusPermanentFailure)
}

// markStatus is the shared implementation for single-entry MarkPermanentFailure.
// It holds the WAL mutex for the full operation.
func (w *WAL) markStatus(payloadID, status string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	segs, err := w.segments()
	if err != nil {
		return fmt.Errorf("wal: mark %s: list segments: %w", status, err)
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

		found, markErr := w.markStatusInSegment(seg, payloadID, status)

		if isActive {
			// Always reopen the active segment so Append continues to work.
			f, reopenErr := os.OpenFile(seg, os.O_APPEND|os.O_RDWR, 0600)
			if reopenErr != nil {
				return fmt.Errorf("wal: reopen active segment: %w", reopenErr)
			}
			info, statErr := f.Stat()
			if statErr != nil {
				// Already returning an error; close is best-effort here.
				if closeErr := f.Close(); closeErr != nil {
					slog.Error("wal: close segment after stat failure", "err", closeErr)
				}
				return fmt.Errorf("wal: stat active segment after reopen: %w", statErr)
			}
			w.current = f
			w.currentSize = info.Size()
		}

		if markErr != nil {
			return markErr
		}
		if found {
			w.pendingCount.Add(-1)
			return nil
		}
	}

	return fmt.Errorf("wal: payload_id %q not found in any segment", payloadID)
}

// markStatusBatch marks multiple entries as the given status in a single
// segment rewrite pass per segment. O(N) for N entries vs O(N²) for
// individual calls. Holds the WAL mutex for the full operation.
func (w *WAL) markStatusBatch(payloadIDs []string, status string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	segs, err := w.segments()
	if err != nil {
		return fmt.Errorf("wal: batch mark %s: list segments: %w", status, err)
	}

	// Build lookup set for O(1) matching.
	remaining := make(map[string]struct{}, len(payloadIDs))
	for _, id := range payloadIDs {
		remaining[id] = struct{}{}
	}

	for _, seg := range segs {
		if len(remaining) == 0 {
			break
		}

		isActive := seg == w.currentPath

		if isActive && w.current != nil {
			if err := w.current.Close(); err != nil {
				return fmt.Errorf("wal: close active segment before batch rewrite: %w", err)
			}
			w.current = nil
		}

		found, markErr := w.markStatusBatchInSegment(seg, remaining, status)

		if isActive {
			f, reopenErr := os.OpenFile(seg, os.O_APPEND|os.O_RDWR, 0600)
			if reopenErr != nil {
				return fmt.Errorf("wal: reopen active segment: %w", reopenErr)
			}
			info, statErr := f.Stat()
			if statErr != nil {
				// Already returning an error; close is best-effort here.
				if closeErr := f.Close(); closeErr != nil {
					slog.Error("wal: close segment after stat failure", "err", closeErr)
				}
				return fmt.Errorf("wal: stat active segment after reopen: %w", statErr)
			}
			w.current = f
			w.currentSize = info.Size()
		}

		if markErr != nil {
			return markErr
		}
		w.pendingCount.Add(int64(-found))
	}

	return nil
}

// markStatusInSegment scans segPath for an entry matching payloadID, rewrites
// it with the given status and a fresh CRC32 (+ fresh HMAC if integrity=mac),
// and atomically replaces the segment via a temp file + rename on the same
// filesystem. Reference: Tech Spec Section 4.3.
func (w *WAL) markStatusInSegment(segPath, payloadID, status string) (bool, error) {
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

		wl := parseWALLine(line)
		if wl == nil {
			// Partial line — preserve as-is; it will be skipped on replay.
			lines = append(lines, line)
			continue
		}

		var entry Entry
		if err := json.Unmarshal(wl.JSONBytes, &entry); err != nil {
			// Unparseable line — preserve as-is.
			lines = append(lines, line)
			continue
		}

		if entry.PayloadID == payloadID && !found {
			entry.Status = status
			updated, marshalErr := json.Marshal(entry)
			if marshalErr != nil {
				// Abort before any write — close file and surface error.
				if closeErr := f.Close(); closeErr != nil {
					slog.Error("wal: close segment after stat failure", "err", closeErr)
				}
				return false, fmt.Errorf("wal: marshal updated entry: %w", marshalErr)
			}
			// Recompute CRC32 and HMAC over the new JSON bytes.
			// Always write in the new sentinel format.
			// Reference: Tech Spec Section 4.3.
			line = formatWALContent(updated, w.integrityMode, w.macKey)
			found = true
		}

		lines = append(lines, line)
	}

	scanErr := scanner.Err()
	// Close the source file before rename so Windows allows overwrite.
	if err := f.Close(); err != nil {
		slog.Error("wal: close segment file", "path", segPath, "error", err)
	}

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
	defer func() {
		if tmp != nil {
			if closeErr := tmp.Close(); closeErr != nil {
				slog.Error("wal: close temp file in cleanup", "error", closeErr)
			}
		}
		if tmp != nil {
			// Best-effort removal of the temp file on the failure path.
			if removeErr := os.Remove(tmpPath); removeErr != nil {
				slog.Error("wal: remove temp file in cleanup", "path", tmpPath, "error", removeErr)
			}
		}
	}()

	// Best-effort chmod: mandatory on Unix (0600), advisory on Windows.
	// The rename replaces the segment so permissions must match.
	_ = tmp.Chmod(0600)

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
	tmp = nil // signal to deferred cleanup that close already succeeded

	if err := fsutil.RobustRename(tmpPath, segPath); err != nil {
		return false, fmt.Errorf("wal: rename temp to segment: %w", err)
	}

	return true, nil
}

// markStatusBatchInSegment scans segPath and rewrites all entries whose
// payload_id is in the remaining set. Returns the count of entries matched
// and removes matched IDs from the remaining set. One segment rewrite for
// any number of matches — O(N) instead of O(N²).
func (w *WAL) markStatusBatchInSegment(segPath string, remaining map[string]struct{}, status string) (int, error) {
	f, err := os.Open(segPath)
	if err != nil {
		return 0, fmt.Errorf("wal: open segment %q: %w", segPath, err)
	}

	var lines []string
	found := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	for scanner.Scan() {
		line := scanner.Text()

		wl := parseWALLine(line)
		if wl == nil {
			lines = append(lines, line)
			continue
		}

		var entry Entry
		if err := json.Unmarshal(wl.JSONBytes, &entry); err != nil {
			lines = append(lines, line)
			continue
		}

		if _, ok := remaining[entry.PayloadID]; ok {
			entry.Status = status
			updated, marshalErr := json.Marshal(entry)
			if marshalErr != nil {
				if closeErr := f.Close(); closeErr != nil {
					slog.Error("wal: close segment file", "path", segPath, "error", closeErr)
				}
				return 0, fmt.Errorf("wal: marshal updated entry: %w", marshalErr)
			}
			line = formatWALContent(updated, w.integrityMode, w.macKey)
			delete(remaining, entry.PayloadID)
			found++
		}

		lines = append(lines, line)
	}

	scanErr := scanner.Err()
	if err := f.Close(); err != nil {
		slog.Error("wal: close segment file", "path", segPath, "error", err)
	}

	if scanErr != nil {
		return 0, fmt.Errorf("wal: scan segment %q: %w", segPath, scanErr)
	}
	if found == 0 {
		return 0, nil
	}

	tmp, err := os.CreateTemp(filepath.Dir(segPath), "wal-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("wal: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmp != nil {
			if closeErr := tmp.Close(); closeErr != nil {
				slog.Error("wal: close temp file in cleanup", "error", closeErr)
			}
		}
		if tmp != nil {
			// Best-effort removal of the temp file on the failure path.
			if removeErr := os.Remove(tmpPath); removeErr != nil {
				slog.Error("wal: remove temp file in cleanup", "path", tmpPath, "error", removeErr)
			}
		}
	}()

	_ = tmp.Chmod(0600)

	for _, line := range lines {
		if _, err := fmt.Fprintln(tmp, line); err != nil {
			return 0, fmt.Errorf("wal: write temp file: %w", err)
		}
	}

	if err := tmp.Sync(); err != nil {
		return 0, fmt.Errorf("wal: fsync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("wal: close temp file: %w", err)
	}
	tmp = nil // signal to deferred cleanup that close already succeeded

	if err := fsutil.RobustRename(tmpPath, segPath); err != nil {
		return 0, fmt.Errorf("wal: rename temp to segment: %w", err)
	}

	return found, nil
}
