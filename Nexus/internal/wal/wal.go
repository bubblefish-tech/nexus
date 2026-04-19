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
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Option configures a WAL instance. Pass to Open.
type Option func(*WAL)

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

	// maxEntrySize is the maximum expected size of a single WAL entry line
	// (JSON + sentinels + CRC + newline). Used for disk reservation checks.
	// Set conservatively to 1MB; most entries are well under this.
	maxEntrySize = 1 << 20

	// preAllocThresholdPct is the segment fill percentage at which the next
	// segment is pre-allocated. At 80% fill, a new empty segment is created.
	preAllocThresholdPct = 80
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
	// EntryType discriminates entry kinds. Empty string (zero value) means
	// data entry, preserving backward compatibility with v0.1.2 WAL files.
	EntryType string `json:"entry_type,omitempty"`
	// MonotonicSeq is a strictly increasing sequence number assigned on
	// append, independent of wall-clock time. Used for ordering WAL entries
	// correctly even when the system clock jumps (NTP, VM migration).
	// Zero for v0.1.2 entries; Replay assigns synthetic sequences from
	// file offset for backward compatibility.
	MonotonicSeq int64 `json:"monotonic_seq,omitempty"`
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
	crcFailures      atomic.Int64 // read without mu via CRCFailures()
	sentinelFailures atomic.Int64 // read without mu via SentinelFailures()

	// Integrity mode: IntegrityModeCRC32 (default) or IntegrityModeMAC.
	// When mode=mac, HMAC-SHA256 is computed over JSON bytes on every write
	// and validated on every replay. Reference: Tech Spec Section 6.4.1.
	integrityMode string
	// macKey is the 32-byte HMAC-SHA256 key. Loaded once at startup via
	// config.ResolveEnv. NEVER re-read per entry. NEVER logged.
	macKey []byte
	// integrityFailures counts HMAC mismatches detected during replay.
	// Exposed via IntegrityFailures() for Prometheus.
	integrityFailures atomic.Int64
	// onSecurityEvent is called when a security-relevant event occurs
	// (e.g. HMAC mismatch). May be nil.
	onSecurityEvent SecurityEventFunc

	// pendingCount tracks PENDING entries: incremented on Append, decremented
	// on MarkDelivered/MarkPermanentFailure, initialised during Replay.
	// Read without lock via PendingCount(). Reference: Tech Spec Section 4.4.
	pendingCount atomic.Int64

	// gc is the optional group committer. When non-nil, Append routes
	// through the group commit goroutine instead of writing+fsyncing
	// directly. The durability guarantee is preserved: Append blocks
	// until the batch containing the entry has been fsynced.
	gc *groupCommitter

	// seqFn returns the next monotonic sequence value. Set via
	// WithSequence option. When nil, MonotonicSeq is left at 0
	// (backward compatible with v0.1.2 consumers).
	seqFn func() int64

	// compress enables zstd compression on new WAL entries. Compressed
	// entries use the "zstd:" prefix in the payload field. Replay
	// auto-detects and decompresses regardless of this flag.
	// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.10.
	compress bool
}

// WithIntegrity configures WAL integrity mode. When mode is "mac", key
// must be a non-empty HMAC-SHA256 key (typically 32 bytes). The key is
// stored once and never re-read. Reference: Tech Spec Section 6.4.1.
func WithIntegrity(mode string, key []byte) Option {
	return func(w *WAL) {
		w.integrityMode = mode
		w.macKey = key
	}
}

// WithEncryption configures WAL at-rest encryption with the provided key.
// The key should be 32 bytes (AES-256). Encryption support is a future phase;
// this option is accepted but not yet implemented.
// Reference: Tech Spec Section 6.4.2.
func WithEncryption(key []byte) Option {
	return func(w *WAL) {
		// TODO(v0.1.4): implement WAL-at-rest encryption.
		// This option is accepted for config forward-compatibility but does NOT
		// encrypt WAL data in v0.1.3. The daemon logs a WARN at startup.
		_ = key
	}
}

// WithGroupCommit enables the group commit write path. When enabled, Append
// routes entries through a single consumer goroutine that batches writes and
// performs one fsync per batch. Per-request durability is preserved: Append
// blocks until the batch is fsynced.
func WithGroupCommit(cfg GroupCommitConfig) Option {
	return func(w *WAL) {
		if cfg.Enabled {
			w.gc = newGroupCommitter(cfg, w.logger)
		}
	}
}

// WithSequence configures the monotonic sequence function. When set, every
// Append assigns entry.MonotonicSeq = seqFn(). The seqFn must return
// strictly increasing values. Typically backed by a seq.Counter.
func WithSequence(seqFn func() int64) Option {
	return func(w *WAL) {
		w.seqFn = seqFn
	}
}

// WithSecurityEvent registers a callback invoked on security events such
// as wal_tamper_detected. The callback is invoked synchronously during
// replay; it must not block.
func WithSecurityEvent(fn SecurityEventFunc) Option {
	return func(w *WAL) {
		w.onSecurityEvent = fn
	}
}

// WithCompression enables zstd compression for new WAL entries. Compressed
// entries are 3-5x smaller, resulting in fewer bytes to fsync and smaller
// backups. Replay auto-detects and decompresses regardless of this flag,
// so compressed and uncompressed segments can coexist.
//
// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.10.
func WithCompression() Option {
	return func(w *WAL) {
		w.compress = true
	}
}

// Open opens or creates a WAL rooted at dir. maxSizeMB controls segment
// rotation (default 50). Panics if logger is nil. Options configure
// integrity mode and security event callbacks.
func Open(dir string, maxSizeMB int64, logger *slog.Logger, opts ...Option) (*WAL, error) {
	if logger == nil {
		panic("wal: logger must not be nil")
	}
	if maxSizeMB <= 0 {
		maxSizeMB = defaultMaxSizeMB
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("wal: create directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("wal: chmod directory: %w", err)
	}
	w := &WAL{
		dir:           dir,
		maxSize:       maxSizeMB << 20,
		logger:        logger,
		integrityMode: IntegrityModeCRC32,
	}
	for _, opt := range opts {
		opt(w)
	}
	// Fail-fast: integrity=mac requires a non-empty MAC key.
	// Reference: Tech Spec Section 4.1 — daemon MUST refuse to start.
	if w.integrityMode == IntegrityModeMAC && len(w.macKey) == 0 {
		return nil, fmt.Errorf("wal: integrity mode %q requires a non-empty mac key", IntegrityModeMAC)
	}
	if err := w.openCurrentSegment(); err != nil {
		return nil, err
	}
	if w.gc != nil {
		go w.gc.run(w.writeBatch)
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
		if closeErr := f.Close(); closeErr != nil {
			slog.Error("wal: close segment after stat failure", "err", closeErr)
		}
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
// appended after a tab before the newline.
//
// When group commit is enabled, the entry is sent to a consumer goroutine
// that batches writes and calls fsync once per batch. When group commit is
// disabled (legacy mode), fsync is called per entry.
//
// In both modes the durability guarantee is the same: if Append returns nil,
// the entry is on disk. On any failure the caller must return a 500 to the
// client.
func (w *WAL) Append(entry Entry) error {
	entry.Version = walVersion
	entry.Status = StatusPending
	// TODO(monotonic): Timestamp is wall-clock for display/forensics only.
	// Ordering uses MonotonicSeq (below). Phases 2-4 MUST compare
	// MonotonicSeq, never Timestamp, for sequencing decisions.
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if w.seqFn != nil {
		entry.MonotonicSeq = w.seqFn()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("wal: marshal entry: %w", err)
	}

	// Optionally compress the JSON payload before CRC computation.
	// Compression happens before CRC32, so the CRC covers compressed bytes.
	// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.10.
	var payloadBytes []byte
	if w.compress {
		payloadBytes = []byte(compressPayload(data))
	} else {
		payloadBytes = data
	}

	line := formatWALContent(payloadBytes, w.integrityMode, w.macKey) + "\n"

	if w.gc != nil {
		// Group commit path: submit to the consumer goroutine and block
		// until the batch is fsynced. pendingCount is incremented inside
		// writeBatch after the successful fsync.
		return w.gc.submit(line)
	}

	return w.appendDirect(line)
}

// appendDirect writes a single line to the current segment with fsync.
// Used when group commit is disabled (legacy mode).
func (w *WAL) appendDirect(line string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Disk-full reservation check for legacy (non-group-commit) path.
	if free, dErr := diskFreeBytes(w.dir); dErr == nil && free < uint64(maxEntrySize) {
		return fmt.Errorf("wal: disk full — need %d bytes, only %d available", maxEntrySize, free)
	}

	n, err := fmt.Fprint(w.current, line)
	if err != nil {
		return fmt.Errorf("wal: write: %w", err)
	}
	// fsync per entry is REQUIRED for the legacy durability guarantee.
	// When group commit is enabled, fsync is batched in writeBatch instead.
	if err := w.current.Sync(); err != nil {
		return fmt.Errorf("wal: fsync: %w", err)
	}

	w.currentSize += int64(n)
	w.pendingCount.Add(1)
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

// writeBatch is called by the group committer goroutine. It writes all
// entries in the batch sequentially, calls fsync once, then signals every
// waiter. On failure, all waiters receive the error.
func (w *WAL) writeBatch(batch []*pendingEntry) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Disk-full reservation check: verify enough space for (batch_size × maxEntrySize).
	// Reject the entire batch if insufficient — data is better refused than
	// partially written and corrupted. Reference: v0.1.3 Build Plan Subtask 1.7.
	requiredBytes := uint64(len(batch)) * uint64(maxEntrySize)
	if free, err := diskFreeBytes(w.dir); err == nil && free < requiredBytes {
		diskErr := fmt.Errorf("wal: disk full — need %d bytes for batch, only %d available", requiredBytes, free)
		w.logger.Error("wal: disk-full reservation check failed",
			"component", "wal",
			"required_bytes", requiredBytes,
			"free_bytes", free,
			"batch_size", len(batch),
		)
		for _, pe := range batch {
			pe.done <- diskErr
		}
		return
	}

	var totalBytes int64
	var writeErr error
	for _, pe := range batch {
		n, err := fmt.Fprint(w.current, pe.line)
		if err != nil {
			writeErr = fmt.Errorf("wal: write: %w", err)
			break
		}
		totalBytes += int64(n)
	}

	if writeErr == nil {
		if err := w.current.Sync(); err != nil {
			writeErr = fmt.Errorf("wal: fsync: %w", err)
		}
	}

	// Signal all waiters. On success, increment pendingCount for each
	// entry and check segment rotation.
	for _, pe := range batch {
		if writeErr != nil {
			pe.done <- writeErr
		} else {
			w.pendingCount.Add(1)
			pe.done <- nil
		}
	}

	if writeErr == nil {
		w.currentSize += totalBytes
		if w.currentSize >= w.maxSize {
			if rotErr := w.rotate(); rotErr != nil {
				w.logger.Warn("wal: segment rotation failed",
					"component", "wal",
					"error", rotErr,
				)
			}
		} else if w.currentSize*100/w.maxSize >= preAllocThresholdPct {
			// Pre-allocate next segment at 80% fill so rotation is instant.
			// Reference: v0.1.3 Build Plan Subtask 1.7.
			w.preAllocateNextSegment()
		}
	}
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

// preAllocateNextSegment creates the next segment file so that rotation just
// does close+open rather than create. Called when current segment reaches 80%
// fill. Idempotent — no-op if the next segment already exists or on any error.
// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.7.
func (w *WAL) preAllocateNextSegment() {
	nextPath := w.newSegmentPath()
	f, err := os.OpenFile(nextPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		// Already exists or disk error — ignore silently.
		return
	}
	_ = f.Close()
	w.logger.Debug("wal: pre-allocated next segment",
		"component", "wal",
		"path", nextPath,
	)
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
	defer func() {
		if err := f.Close(); err != nil {
			slog.Error("wal: close segment after replay", "path", path, "err", err)
		}
	}()

	// 10MB buffer accommodates the maximum WAL entry size (matches max
	// payload size from config). Allocated once per segment at startup
	// replay; not hot-path.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		wl := parseWALLine(line)
		if wl == nil {
			continue // partial write — fewer than 2 tab fields
		}

		// Sentinel validation: fail closed on sentinel errors.
		// If the start sentinel is present but the end sentinel is missing
		// or corrupt, the entry may be a torn write. Reject unconditionally.
		if wl.HasSentinels && wl.SentinelErr != nil {
			w.sentinelFailures.Add(1)
			w.logger.Error("wal: sentinel corruption detected — entry rejected",
				"component", "wal",
				"segment", path,
				"line_number", lineNum,
				"error", wl.SentinelErr,
			)
			continue
		}

		jsonBytes := wl.JSONBytes
		storedCRC := wl.StoredCRC

		// CRC32 validated first (cheap). Reference: Tech Spec Section 4.1.
		computed := fmt.Sprintf("%08x", crc32.ChecksumIEEE(jsonBytes))
		if computed != storedCRC {
			w.crcFailures.Add(1)
			w.logger.Warn("wal: CRC mismatch — entry skipped",
				"component", "wal",
				"segment", path,
				"line_number", lineNum,
				"stored_crc", storedCRC,
				"computed_crc", computed,
			)
			continue
		}

		// HMAC validated second (more expensive). Reference: Tech Spec Section 4.1.
		// Only checked when integrity=mac AND the line has an HMAC field.
		// Pre-upgrade entries without HMAC are treated as valid when mode=mac
		// because no tamper check is possible for entries written before upgrade.
		if w.integrityMode == IntegrityModeMAC && wl.StoredHMAC != "" {
			if !validateHMAC(jsonBytes, w.macKey, wl.StoredHMAC) {
				w.integrityFailures.Add(1)
				w.logger.Warn("wal: HMAC mismatch — entry skipped (possible tampering)",
					"component", "wal",
					"segment", path,
					"line_number", lineNum,
				)
				if w.onSecurityEvent != nil {
					w.onSecurityEvent("wal_tamper_detected",
						slog.String("segment_file", path),
						slog.Int("line_number", lineNum),
					)
				}
				continue
			}
		}

		// Decompress if the payload is zstd-compressed.
		entryBytes := jsonBytes
		if decompressed, wasCompressed, dErr := decompressPayload(jsonBytes); dErr != nil {
			w.logger.Warn("wal: zstd decompression failed — entry skipped",
				"component", "wal",
				"segment", path,
				"line_number", lineNum,
				"error", dErr,
			)
			continue
		} else if wasCompressed {
			entryBytes = decompressed
		}

		var entry Entry
		if err := json.Unmarshal(entryBytes, &entry); err != nil {
			w.logger.Warn("wal: malformed JSON — entry skipped",
				"component", "wal",
				"segment", path,
				"line_number", lineNum,
				"error", err,
			)
			continue
		}

		// Skip non-data entries (checkpoints, audit, etc.).
		if entry.EntryType != EntryTypeData {
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

		w.pendingCount.Add(1)
		fn(entry)
	}

	return scanner.Err()
}

// PendingCount returns the current count of PENDING WAL entries. Incremented
// on Append and Replay, decremented on MarkDelivered/MarkPermanentFailure.
// Safe to call concurrently. Reference: Tech Spec Section 4.4.
// CurrentSegment returns the filename (not full path) of the current WAL
// segment. Used by the /api/status admin endpoint.
func (w *WAL) CurrentSegment() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return filepath.Base(w.currentPath)
}

func (w *WAL) PendingCount() int64 {
	return w.pendingCount.Load()
}

// CRCFailures returns the total number of CRC32 mismatches encountered during
// Replay calls. This counter is exposed to Prometheus in Phase 0D via
// bubblefish_wal_crc_failures_total.
func (w *WAL) CRCFailures() int64 {
	return w.crcFailures.Load()
}

// IntegrityFailures returns the total number of HMAC mismatches encountered
// during Replay calls. Only non-zero when integrity=mac. Exposed to
// Prometheus via bubblefish_wal_integrity_failures_total.
func (w *WAL) IntegrityFailures() int64 {
	return w.integrityFailures.Load()
}

// HighestSeq scans all WAL segments and returns the highest MonotonicSeq
// value found. Returns 0 if no entries have a MonotonicSeq. Used during
// startup to initialize the sequence counter from WAL state when the
// persisted seq.state file is missing or stale.
func (w *WAL) HighestSeq() (int64, error) {
	segs, err := w.segments()
	if err != nil {
		return 0, err
	}
	var highest int64
	for _, seg := range segs {
		h, err := w.scanHighestSeq(seg)
		if err != nil {
			return 0, err
		}
		if h > highest {
			highest = h
		}
	}
	return highest, nil
}

func (w *WAL) scanHighestSeq(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("wal: open segment for seq scan %q: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	var highest int64
	for scanner.Scan() {
		wl := parseWALLine(scanner.Text())
		if wl == nil {
			continue
		}
		if wl.HasSentinels && wl.SentinelErr != nil {
			continue
		}
		entryBytes := wl.JSONBytes
		if dec, ok, _ := decompressPayload(wl.JSONBytes); ok {
			entryBytes = dec
		}
		var entry Entry
		if err := json.Unmarshal(entryBytes, &entry); err != nil {
			continue
		}
		if entry.MonotonicSeq > highest {
			highest = entry.MonotonicSeq
		}
	}
	return highest, scanner.Err()
}

// SentinelFailures returns the total number of sentinel corruption events
// detected during Replay calls. A sentinel failure indicates the start
// sentinel was present but the end sentinel was missing or corrupt,
// suggesting a torn sector write. Exposed to Prometheus via
// bubblefish_wal_sentinel_failures_total.
func (w *WAL) SentinelFailures() int64 {
	return w.sentinelFailures.Load()
}

// SampleDelivered scans all WAL segments and returns up to count randomly
// sampled entries with status DELIVERED. This is a read-only operation used
// by the consistency checker to verify that delivered payloads exist in the
// destination. Corrupt or malformed entries are silently skipped.
//
// If fewer than count DELIVERED entries exist, all of them are returned.
// Reference: Tech Spec Section 11.5.
func (w *WAL) SampleDelivered(count int) ([]Entry, error) {
	segs, err := w.segments()
	if err != nil {
		return nil, err
	}

	var delivered []Entry
	for _, seg := range segs {
		if err := w.scanDelivered(seg, &delivered); err != nil {
			return nil, err
		}
	}

	if len(delivered) <= count {
		return delivered, nil
	}

	// Fisher-Yates shuffle, take first count.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	rng.Shuffle(len(delivered), func(i, j int) {
		delivered[i], delivered[j] = delivered[j], delivered[i]
	})
	return delivered[:count], nil
}

// scanDelivered reads a single segment and appends DELIVERED entries to out.
func (w *WAL) scanDelivered(path string, out *[]Entry) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("wal: open segment for scan %q: %w", path, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Error("wal: close segment after scan", "path", path, "err", err)
		}
	}()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	for scanner.Scan() {
		line := scanner.Text()

		wl := parseWALLine(line)
		if wl == nil {
			continue
		}

		// Skip sentinel-corrupt entries silently (same as CRC failures here).
		if wl.HasSentinels && wl.SentinelErr != nil {
			continue
		}

		computed := fmt.Sprintf("%08x", crc32.ChecksumIEEE(wl.JSONBytes))
		if computed != wl.StoredCRC {
			continue // skip corrupt entries silently
		}

		entryBytes := wl.JSONBytes
		if dec, ok, _ := decompressPayload(wl.JSONBytes); ok {
			entryBytes = dec
		}
		var entry Entry
		if err := json.Unmarshal(entryBytes, &entry); err != nil {
			continue
		}

		if entry.Status == StatusDelivered {
			*out = append(*out, entry)
		}
	}
	return scanner.Err()
}

// Close flushes any pending group commit entries and closes the current
// WAL segment. Safe to call multiple times.
func (w *WAL) Close() error {
	// Stop the group committer first so it flushes its buffer and exits.
	// This must happen before we close the file handle.
	if w.gc != nil {
		w.gc.stop()
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.current != nil {
		err := w.current.Close()
		w.current = nil
		return err
	}
	return nil
}
