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

// Package wal implements the Write-Ahead Log engine for BubbleFish Nexus.
//
// Every payload is written to the WAL with CRC32 + fsync BEFORE it enters
// the queue. The database is NEVER written to directly — always through the
// queue. Temp files for WAL operations MUST be in filepath.Dir(wal.path),
// NEVER os.TempDir().
package wal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// StatusPending marks an entry that has not yet been delivered.
	StatusPending = "PENDING"
	// StatusDelivered marks an entry that was successfully written to the destination.
	StatusDelivered = "DELIVERED"
	// StatusPermanentFailure marks an entry that cannot be retried.
	StatusPermanentFailure = "PERMANENT_FAILURE"

	walVersion      = 2
	defaultMaxSizeMB = int64(50)

	// scannerBufSize is 10MB. The default bufio.Scanner buffer (64KB) is too small
	// for large AI payloads. NEVER reduce this value.
	scannerBufSize = 10 << 20
)

// Entry is a single WAL record. All fields except Payload are indexed at the
// WAL layer for routing and status tracking. Payload holds the full
// TranslatedPayload as raw JSON to avoid re-encoding on replay.
type Entry struct {
	Version        int             `json:"version"`
	PayloadID      string          `json:"payload_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Status         string          `json:"status"`
	Timestamp      time.Time       `json:"timestamp"`
	Source         string          `json:"source"`
	Destination    string          `json:"destination"`
	Subject        string          `json:"subject"`
	ActorType      string          `json:"actor_type"`
	ActorID        string          `json:"actor_id"`
	Payload        json.RawMessage `json:"payload"`
}

// WAL is the write-ahead log engine. All state is held in struct fields;
// there are no package-level variables.
type WAL struct {
	dir         string
	maxSize     int64
	mu          sync.Mutex
	current     *os.File
	currentPath string
	currentSize int64        // protected by mu
	logger      *slog.Logger
	crcFailures atomic.Int64 // read without mu via CRCFailures()
}

// Open opens or creates a WAL rooted at dir. maxSizeMB controls segment
// rotation (default 50). Panics if logger is nil.
func Open(dir string, maxSizeMB int64, logger *slog.Logger) (*WAL, error) {
	if logger == nil {
		panic("wal: logger must not be nil")
	}
	if maxSizeMB <= 0 {
		maxSizeMB = defaultMaxSizeMB
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("wal: create directory: %w", err)
	}
	w := &WAL{
		dir:     dir,
		maxSize: maxSizeMB << 20,
		logger:  logger,
	}
	if err := w.openCurrentSegment(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *WAL) openCurrentSegment() error {
	segs, err := w.segments()
	if err != nil {
		return err
	}
	var path string
	if len(segs) > 0 {
		path = segs[len(segs)-1]
	} else {
		path = w.newSegmentPath()
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("wal: open segment %q: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("wal: stat segment: %w", err)
	}
	w.current = f
	w.currentPath = path
	w.currentSize = info.Size()
	return nil
}

func (w *WAL) newSegmentPath() string {
	return filepath.Join(w.dir, fmt.Sprintf("wal-%d.jsonl", time.Now().UnixNano()))
}

// segments returns all WAL segment paths sorted oldest-first (lexicographic,
// which matches chronological order given the UnixNano naming scheme).
func (w *WAL) segments() ([]string, error) {
	segs, err := filepath.Glob(filepath.Join(w.dir, "wal-*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("wal: discover segments: %w", err)
	}
	sort.Strings(segs)
	return segs, nil
}

// Append writes entry to the WAL. The entry status is forced to PENDING and
// version is set to walVersion. CRC32 is computed over the JSON bytes and
// appended after a tab before the newline. fsync is called before returning.
//
// On any failure the caller must return a 500 to the client. The WAL invariant
// is: if Append returns nil, the entry is durable on disk.
func (w *WAL) Append(entry Entry) error {
	entry.Version = walVersion
	entry.Status = StatusPending
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("wal: marshal entry: %w", err)
	}

	checksum := crc32.ChecksumIEEE(data)
	line := fmt.Sprintf("%s\t%08x\n", data, checksum)

	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := fmt.Fprint(w.current, line)
	if err != nil {
		return fmt.Errorf("wal: write: %w", err)
	}
	if err := w.current.Sync(); err != nil {
		return fmt.Errorf("wal: fsync: %w", err)
	}

	w.currentSize += int64(n)
	if w.currentSize >= w.maxSize {
		if rotErr := w.rotate(); rotErr != nil {
			w.logger.Warn("wal: segment rotation failed",
				"component", "wal",
				"error", rotErr,
			)
		}
	}
	return nil
}

func (w *WAL) rotate() error {
	if err := w.current.Close(); err != nil {
		return fmt.Errorf("wal: close segment for rotation: %w", err)
	}
	newPath := w.newSegmentPath()
	f, err := os.OpenFile(newPath, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("wal: open new segment: %w", err)
	}
	w.logger.Info("wal: segment rotated",
		"component", "wal",
		"new_segment", newPath,
	)
	w.current = f
	w.currentPath = newPath
	w.currentSize = 0
	return nil
}

// Replay reads all WAL segments oldest-first, calling fn for each PENDING entry.
//
// Crash safety: if two segments exist (crash during rotation), both are replayed.
// Duplicate idempotency keys across segments are deduplicated — fn is called
// at most once per key.
//
// Corrupt entries (CRC mismatch, malformed JSON, partial lines) are skipped
// with a WARN log. Replay does NOT hold the WAL mutex; callers must not call
// Append concurrently with Replay.
func (w *WAL) Replay(fn func(Entry)) error {
	segs, err := w.segments()
	if err != nil {
		return err
	}
	seen := make(map[string]bool) // idempotency_key dedup across segments
	for _, seg := range segs {
		if err := w.replaySegment(seg, seen, fn); err != nil {
			return err
		}
	}
	return nil
}

func (w *WAL) replaySegment(path string, seen map[string]bool, fn func(Entry)) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("wal: open segment for replay %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	for scanner.Scan() {
		line := scanner.Text()

		// Partial write (crash mid-write): no tab separator. Skip silently.
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		jsonBytes := []byte(parts[0])
		storedCRC := parts[1]
		computed := fmt.Sprintf("%08x", crc32.ChecksumIEEE(jsonBytes))
		if computed != storedCRC {
			w.crcFailures.Add(1)
			w.logger.Warn("wal: CRC mismatch — entry skipped",
				"component", "wal",
				"segment", path,
				"stored_crc", storedCRC,
				"computed_crc", computed,
			)
			continue
		}

		var entry Entry
		if err := json.Unmarshal(jsonBytes, &entry); err != nil {
			w.logger.Warn("wal: malformed JSON — entry skipped",
				"component", "wal",
				"segment", path,
				"error", err,
			)
			continue
		}

		if entry.Status != StatusPending {
			continue
		}

		// Deduplicate across segments (crash-during-rotation produces two segments
		// with overlapping entries sharing the same idempotency key).
		if entry.IdempotencyKey != "" {
			if seen[entry.IdempotencyKey] {
				continue
			}
			seen[entry.IdempotencyKey] = true
		}

		fn(entry)
	}

	return scanner.Err()
}

// CRCFailures returns the total number of CRC32 mismatches encountered during
// Replay calls. This counter is exposed to Prometheus in Phase 0D via
// bubblefish_wal_crc_failures_total.
func (w *WAL) CRCFailures() int64 {
	return w.crcFailures.Load()
}

// Close closes the current WAL segment. Safe to call multiple times.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current != nil {
		err := w.current.Close()
		w.current = nil
		return err
	}
	return nil
}
