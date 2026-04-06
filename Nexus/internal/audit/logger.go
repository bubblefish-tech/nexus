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

package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditLogger is an append-only, CRC32-protected, optionally HMAC'd audit trail
// that records every HTTP interaction with Nexus. It shares durability primitives
// with the WAL but is a separate file, separate concern, and separate package.
//
// Thread-safe: all methods are safe for concurrent use.
//
// Reference: Tech Spec Addendum Section A2.3.
type AuditLogger struct {
	mu sync.Mutex

	file        *os.File
	filePath    string
	currentSize int64
	maxSize     int64 // bytes; rotation threshold

	// Integrity mode: "crc32" (default) or "mac".
	integrityMode string
	macKey        []byte

	logger *slog.Logger
}

// LoggerOption configures an AuditLogger. Pass to NewAuditLogger.
type LoggerOption func(*AuditLogger)

// WithMaxFileSize sets the rotation threshold in bytes.
// Default: 100 MB (100 * 1024 * 1024).
func WithMaxFileSize(bytes int64) LoggerOption {
	return func(l *AuditLogger) {
		l.maxSize = bytes
	}
}

// WithIntegrityMode sets the integrity mode and HMAC key.
// mode must be "crc32" or "mac". If "mac", key must be non-empty.
// Uses the same HMAC key as the WAL — single key management story.
func WithIntegrityMode(mode string, key []byte) LoggerOption {
	return func(l *AuditLogger) {
		l.integrityMode = mode
		l.macKey = key
	}
}

// WithLogger sets the structured logger for warnings and errors.
func WithLogger(logger *slog.Logger) LoggerOption {
	return func(l *AuditLogger) {
		l.logger = logger
	}
}

// NewAuditLogger creates an AuditLogger that writes to logFile.
// The directory is created with 0700 if it doesn't exist.
// The file is opened with O_APPEND|O_WRONLY|O_CREATE, permissions 0600.
//
// Reference: Tech Spec Addendum Section A2.3.
func NewAuditLogger(logFile string, opts ...LoggerOption) (*AuditLogger, error) {
	l := &AuditLogger{
		filePath:      logFile,
		maxSize:       100 * 1024 * 1024, // 100 MB default
		integrityMode: "crc32",
		logger:        slog.Default(),
	}
	for _, opt := range opts {
		opt(l)
	}

	if l.integrityMode == "mac" && len(l.macKey) == 0 {
		return nil, fmt.Errorf("audit: integrity mode %q requires a non-empty mac key", l.integrityMode)
	}

	if err := l.openFile(); err != nil {
		return nil, err
	}
	return l, nil
}

// openFile opens (or reopens) the log file. Called at init and when the file
// is deleted while the daemon is running.
func (l *AuditLogger) openFile() error {
	dir := filepath.Dir(l.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("audit: create log directory: %w", err)
	}

	f, err := os.OpenFile(l.filePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("audit: open log file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("audit: stat log file: %w", err)
	}

	l.file = f
	l.currentSize = info.Size()
	return nil
}

// Log appends an interaction record to the audit log.
//
// The record's CRC32 field is computed over the JSON bytes with the crc32 field
// set to empty string, matching WAL semantics. Every append is fsync'd.
//
// Log failure MUST NOT cause request failure — the caller logs WARN and
// increments bubblefish_audit_log_errors_total.
//
// Reference: Tech Spec Addendum Sections A2.3, A2.4.
func (l *AuditLogger) Log(record InteractionRecord) error {
	// Ensure record_id is set.
	if record.RecordID == "" {
		record.RecordID = NewRecordID()
	}

	// CRC32 computed with the field set to empty string.
	record.CRC32 = ""
	jsonBytes, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("audit: marshal record: %w", err)
	}

	checksum := crc32.ChecksumIEEE(jsonBytes)

	var line string
	if l.integrityMode == "mac" {
		mac := computeHMAC(jsonBytes, l.macKey)
		line = fmt.Sprintf("%s\t%08x\t%s\n", jsonBytes, checksum, mac)
	} else {
		line = fmt.Sprintf("%s\t%08x\n", jsonBytes, checksum)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Edge case: log file deleted while daemon running — recreate.
	if l.file == nil {
		if err := l.openFile(); err != nil {
			return err
		}
	}

	n, err := fmt.Fprint(l.file, line)
	if err != nil {
		// Attempt to reopen on next call.
		l.file.Close()
		l.file = nil
		return fmt.Errorf("audit: write: %w", err)
	}

	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("audit: fsync: %w", err)
	}

	l.currentSize += int64(n)
	if l.currentSize >= l.maxSize {
		if rotErr := l.rotate(); rotErr != nil {
			l.logger.Warn("audit: log rotation failed",
				"component", "audit",
				"error", rotErr,
			)
		}
	}
	return nil
}

// rotate renames the current log file and opens a new one.
// Caller must hold l.mu.
//
// Rotated files: interactions-YYYYMMDD-HHMMSS.jsonl
// Reference: Tech Spec Addendum Section A2.3.
func (l *AuditLogger) rotate() error {
	if err := l.file.Close(); err != nil {
		return fmt.Errorf("audit: close for rotation: %w", err)
	}
	l.file = nil

	ts := time.Now().UTC().Format("20060102-150405")
	dir := filepath.Dir(l.filePath)
	rotatedPath := filepath.Join(dir, fmt.Sprintf("interactions-%s.jsonl", ts))

	if err := os.Rename(l.filePath, rotatedPath); err != nil {
		return fmt.Errorf("audit: rename for rotation: %w", err)
	}

	l.logger.Info("audit: log rotated",
		"component", "audit",
		"rotated_to", rotatedPath,
	)

	return l.openFile()
}

// Close flushes and closes the audit log file.
func (l *AuditLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// computeHMAC computes HMAC-SHA256 over data using key and returns the
// hex-encoded result. Same algorithm as WAL — single key management story.
func computeHMAC(data, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// validateHMAC checks that expectedHex matches the HMAC-SHA256 of data
// using key. Returns false if expectedHex is not valid hex or the MAC
// does not match. Uses hmac.Equal for constant-time comparison.
func validateHMAC(data, key []byte, expectedHex string) bool {
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hmac.Equal(mac.Sum(nil), expected)
}
