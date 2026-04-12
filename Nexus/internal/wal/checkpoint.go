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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"strings"
	"time"
)

// EntryType discriminates WAL entry kinds. Empty string means data entry
// (backward compat with v0.1.2 entries that have no entry_type field).
const (
	EntryTypeData       = ""           // default — regular data entries
	EntryTypeCheckpoint = "checkpoint" // checkpoint marker
)

// CheckpointData is the payload stored inside a checkpoint WAL entry.
type CheckpointData struct {
	Segment      string `json:"segment"`       // segment filename at checkpoint time
	OffsetBytes  int64  `json:"offset_bytes"`  // byte offset in segment after checkpoint
	Timestamp    string `json:"timestamp"`     // ISO 8601
	AppliedCount int64  `json:"applied_count"` // total entries applied at checkpoint time
	StateHash    string `json:"state_hash"`    // SHA-256 digest for validation
}

// Checkpoint represents a discovered checkpoint during replay.
type Checkpoint struct {
	SegmentPath string         // full path to the segment containing this checkpoint
	LineNumber  int            // line number within the segment (1-based)
	Data        CheckpointData // parsed checkpoint payload
}

// WriteCheckpoint writes a checkpoint entry to the WAL. The checkpoint is
// written through the same path as data entries (group commit or direct)
// to ensure it shares the same fsync durability. The appliedCount and
// stateHash are provided by the caller (typically computed from destination
// state).
func (w *WAL) WriteCheckpoint(appliedCount int64, stateHash string) error {
	cpData := CheckpointData{
		Segment:      w.CurrentSegment(),
		OffsetBytes:  w.currentSizeSnapshot(),
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		AppliedCount: appliedCount,
		StateHash:    stateHash,
	}
	payload, err := json.Marshal(cpData)
	if err != nil {
		return fmt.Errorf("wal: marshal checkpoint: %w", err)
	}

	entry := Entry{
		Version:   walVersion,
		Status:    StatusDelivered, // checkpoints are never "pending" for delivery
		Timestamp: time.Now().UTC(),
		PayloadID: fmt.Sprintf("checkpoint-%d", time.Now().UnixNano()),
		EntryType: EntryTypeCheckpoint,
		Payload:   payload,
	}

	data, merr := json.Marshal(entry)
	if merr != nil {
		return fmt.Errorf("wal: marshal checkpoint entry: %w", merr)
	}

	checksum := crc32.ChecksumIEEE(data)
	var line string
	if w.integrityMode == IntegrityModeMAC {
		mac := computeHMAC(data, w.macKey)
		line = fmt.Sprintf("%s\t%08x\t%s\n", data, checksum, mac)
	} else {
		line = fmt.Sprintf("%s\t%08x\n", data, checksum)
	}

	if w.gc != nil {
		return w.gc.submit(line)
	}
	return w.appendDirect(line)
}

// currentSizeSnapshot returns the current segment size. Thread-safe.
func (w *WAL) currentSizeSnapshot() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentSize
}

// FindLatestCheckpoint scans all WAL segments and returns the most recent
// valid checkpoint. Returns nil if no valid checkpoints exist.
func (w *WAL) FindLatestCheckpoint() (*Checkpoint, error) {
	segs, err := w.segments()
	if err != nil {
		return nil, err
	}

	var best *Checkpoint
	for _, seg := range segs {
		cp, err := w.scanCheckpoints(seg)
		if err != nil {
			return nil, err
		}
		// Take the last valid checkpoint in the last segment that has one.
		if cp != nil {
			best = cp
		}
	}
	return best, nil
}

// scanCheckpoints scans a segment for checkpoint entries and returns the
// last valid one found.
func (w *WAL) scanCheckpoints(path string) (*Checkpoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("wal: open segment for checkpoint scan %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	var best *Checkpoint
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}

		jsonBytes := []byte(parts[0])
		storedCRC := parts[1]

		computed := fmt.Sprintf("%08x", crc32.ChecksumIEEE(jsonBytes))
		if computed != storedCRC {
			continue
		}

		var entry Entry
		if err := json.Unmarshal(jsonBytes, &entry); err != nil {
			continue
		}

		if entry.EntryType != EntryTypeCheckpoint {
			continue
		}

		var cpData CheckpointData
		if err := json.Unmarshal(entry.Payload, &cpData); err != nil {
			w.logger.Warn("wal: malformed checkpoint payload — skipped",
				"component", "wal",
				"segment", path,
				"line_number", lineNum,
			)
			continue
		}

		best = &Checkpoint{
			SegmentPath: path,
			LineNumber:  lineNum,
			Data:        cpData,
		}
	}
	return best, scanner.Err()
}

// ReplayFromCheckpoint replays WAL entries starting after the given
// checkpoint. If cp is nil, falls back to full replay (same as Replay).
// Only PENDING data entries are passed to fn; checkpoint entries are skipped.
func (w *WAL) ReplayFromCheckpoint(cp *Checkpoint, fn func(Entry)) error {
	if cp == nil {
		return w.Replay(fn)
	}

	segs, err := w.segments()
	if err != nil {
		return err
	}

	seen := make(map[string]bool)
	pastCheckpoint := false

	for _, seg := range segs {
		if !pastCheckpoint && seg != cp.SegmentPath {
			// Skip segments entirely before the checkpoint segment.
			continue
		}

		startLine := 0
		if seg == cp.SegmentPath && !pastCheckpoint {
			startLine = cp.LineNumber // skip lines up to and including the checkpoint
			pastCheckpoint = true
		}

		if err := w.replaySegmentFrom(seg, startLine, seen, fn); err != nil {
			return err
		}
	}
	return nil
}

// replaySegmentFrom replays a segment starting after skipLines lines.
// Lines 1..skipLines are skipped; lines skipLines+1.. are processed.
func (w *WAL) replaySegmentFrom(path string, skipLines int, seen map[string]bool, fn func(Entry)) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("wal: open segment for replay %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= skipLines {
			continue
		}

		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
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
				"line_number", lineNum,
			)
			continue
		}

		if w.integrityMode == IntegrityModeMAC && len(parts) == 3 {
			storedHMAC := parts[2]
			if !validateHMAC(jsonBytes, w.macKey, storedHMAC) {
				w.integrityFailures.Add(1)
				w.logger.Warn("wal: HMAC mismatch — entry skipped",
					"component", "wal",
					"segment", path,
					"line_number", lineNum,
				)
				continue
			}
		}

		var entry Entry
		if err := json.Unmarshal(jsonBytes, &entry); err != nil {
			w.logger.Warn("wal: malformed JSON — entry skipped",
				"component", "wal",
				"segment", path,
				"line_number", lineNum,
			)
			continue
		}

		// Skip checkpoint entries — they are not data.
		if entry.EntryType == EntryTypeCheckpoint {
			continue
		}

		if entry.Status != StatusPending {
			continue
		}

		if entry.IdempotencyKey != "" {
			if seen[entry.IdempotencyKey] {
				continue
			}
			seen[entry.IdempotencyKey] = true
		}

		w.pendingCount.Add(1)
		fn(entry)
	}
	return scanner.Err()
}

// ComputeStateHash computes a SHA-256 hash from the applied count and a
// caller-provided content digest. This is used to validate that the
// destination state matches the checkpoint at replay time.
func ComputeStateHash(appliedCount int64, contentDigest string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%s", appliedCount, contentDigest)
	return fmt.Sprintf("%x", h.Sum(nil))
}
