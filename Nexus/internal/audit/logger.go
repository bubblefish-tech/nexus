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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bubblefish-tech/nexus/internal/fsutil"
)

// AuditLogger is an append-only, CRC32-protected, optionally HMAC'd and encrypted
// audit trail that records every HTTP interaction with Nexus. It supports dual-file
// write (primary + shadow) for flight-recorder-grade durability.
//
// Thread-safe: all methods are safe for concurrent use.
//
// Reference: Tech Spec Addendum Section A2.3, Update U1.1–U1.5.
type AuditLogger struct {
	mu sync.Mutex

	file        *os.File
	shadowFile  *os.File
	filePath    string
	shadowPath  string
	currentSize int64
	maxSize     int64 // bytes; rotation threshold

	// Integrity mode: "crc32" (default) or "mac".
	integrityMode string
	macKey        []byte // SEPARATE from WAL HMAC key

	// Encryption: optional AES-256-GCM with SEPARATE key from WAL.
	encryptionEnabled bool
	encryptionKey     []byte

	// Dual-write: when true, every record written to both primary and shadow.
	dualWrite bool

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
// This key is SEPARATE from the WAL HMAC key.
//
// Reference: Update U1.1.
func WithIntegrityMode(mode string, key []byte) LoggerOption {
	return func(l *AuditLogger) {
		l.integrityMode = mode
		l.macKey = key
	}
}

// WithEncryption enables AES-256-GCM encryption with the given 32-byte key.
// This key is SEPARATE from the WAL encryption key.
//
// Reference: Update U1.2.
func WithEncryption(key []byte) LoggerOption {
	return func(l *AuditLogger) {
		l.encryptionEnabled = true
		l.encryptionKey = key
	}
}

// WithDualWrite enables or disables dual-file write (primary + shadow).
// Default: true.
//
// Reference: Update U1.3.
func WithDualWrite(enabled bool) LoggerOption {
	return func(l *AuditLogger) {
		l.dualWrite = enabled
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
// Reference: Tech Spec Addendum Section A2.3, Update U1.1–U1.3.
func NewAuditLogger(logFile string, opts ...LoggerOption) (*AuditLogger, error) {
	l := &AuditLogger{
		filePath:      logFile,
		maxSize:       100 * 1024 * 1024, // 100 MB default
		integrityMode: "crc32",
		dualWrite:     true,
		logger:        slog.Default(),
	}
	for _, opt := range opts {
		opt(l)
	}

	if l.integrityMode == "mac" && len(l.macKey) == 0 {
		return nil, fmt.Errorf("audit: integrity mode %q requires a non-empty mac key", l.integrityMode)
	}

	if l.encryptionEnabled && len(l.encryptionKey) != 32 {
		return nil, fmt.Errorf("audit: encryption requires a 32-byte key, got %d bytes", len(l.encryptionKey))
	}

	// Compute shadow path from primary path.
	dir := filepath.Dir(logFile)
	base := filepath.Base(logFile)
	l.shadowPath = filepath.Join(dir, strings.Replace(base, "interactions", "interactions-shadow", 1))

	if err := l.openFile(); err != nil {
		return nil, err
	}

	if l.dualWrite {
		if err := l.openShadowFile(); err != nil {
			return nil, err
		}
	}

	return l, nil
}

// openFile opens (or reopens) the primary log file. Called at init and when the
// file is deleted while the daemon is running.
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
		if closeErr := f.Close(); closeErr != nil {
			l.logger.Warn("audit: close log file after stat failure", "error", closeErr)
		}
		return fmt.Errorf("audit: stat log file: %w", err)
	}

	l.file = f
	l.currentSize = info.Size()
	return nil
}

// openShadowFile opens (or reopens) the shadow log file.
func (l *AuditLogger) openShadowFile() error {
	dir := filepath.Dir(l.shadowPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("audit: create shadow log directory: %w", err)
	}

	f, err := os.OpenFile(l.shadowPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return fmt.Errorf("audit: open shadow log file: %w", err)
	}

	l.shadowFile = f
	return nil
}

// Log appends an interaction record to the audit log.
//
// When dual_write is enabled, the record is written to both primary and shadow
// files. Both are fsync'd. If one write fails but the other succeeds, a WARN
// is logged and the request still succeeds.
//
// Log failure MUST NOT cause request failure — the caller logs WARN and
// increments nexus_audit_log_errors_total.
//
// Reference: Tech Spec Addendum Sections A2.3, A2.4, Update U1.1–U1.3.
func (l *AuditLogger) Log(record InteractionRecord) error {
	// Ensure record_id is set.
	if record.RecordID == "" {
		record.RecordID = NewRecordID()
	}

	line, err := l.formatLine(record)
	if err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	primaryErr := l.writeToFile(&l.file, l.filePath, line, "primary")
	var shadowErr error
	if l.dualWrite {
		shadowErr = l.writeToFile(&l.shadowFile, l.shadowPath, line, "shadow")
	}

	// If both fail, return the primary error.
	if primaryErr != nil && shadowErr != nil {
		return primaryErr
	}

	// If one fails but other succeeds, log WARN but return nil (request succeeds).
	if primaryErr != nil {
		l.logger.Warn("audit: primary write failed, shadow succeeded",
			"component", "audit",
			"file", "primary",
			"error", primaryErr,
		)
	}
	if shadowErr != nil {
		l.logger.Warn("audit: shadow write failed, primary succeeded",
			"component", "audit",
			"file", "shadow",
			"error", shadowErr,
		)
	}

	// Track size using primary file's written bytes.
	if primaryErr == nil {
		l.currentSize += int64(len(line))
		if l.currentSize >= l.maxSize {
			if rotErr := l.rotate(); rotErr != nil {
				l.logger.Warn("audit: log rotation failed",
					"component", "audit",
					"error", rotErr,
				)
			}
		}
	}

	return nil
}

// formatLine serializes a record to the on-disk line format.
//
// Order of operations (when both HMAC and encryption are enabled):
// 1. Compute HMAC over plaintext JSON
// 2. Encrypt (plaintext JSON + HMAC) with AES-256-GCM
// 3. Compute CRC32 over encrypted form
// 4. Write: encrypted_base64<TAB>CRC32_HEX<NEWLINE>
//
// Reference: Update U1.2.
func (l *AuditLogger) formatLine(record InteractionRecord) (string, error) {
	// CRC32 computed with the field set to empty string.
	record.CRC32 = ""
	jsonBytes, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("audit: marshal record: %w", err)
	}

	if l.encryptionEnabled {
		return l.formatEncryptedLine(jsonBytes)
	}
	return l.formatPlaintextLine(jsonBytes), nil
}

// formatPlaintextLine produces: JSON<TAB>CRC32<NEWLINE> or JSON<TAB>CRC32<TAB>HMAC<NEWLINE>.
func (l *AuditLogger) formatPlaintextLine(jsonBytes []byte) string {
	checksum := crc32.ChecksumIEEE(jsonBytes)

	if l.integrityMode == "mac" {
		mac := computeHMAC(jsonBytes, l.macKey)
		return fmt.Sprintf("%s\t%08x\t%s\n", jsonBytes, checksum, mac)
	}
	return fmt.Sprintf("%s\t%08x\n", jsonBytes, checksum)
}

// formatEncryptedLine encrypts the payload and produces: base64_encrypted<TAB>CRC32_HEX<NEWLINE>.
//
// Encryption order: HMAC over plaintext → encrypt (plaintext + HMAC) → CRC over ciphertext.
func (l *AuditLogger) formatEncryptedLine(jsonBytes []byte) (string, error) {
	// Step 1: If HMAC mode, compute HMAC over plaintext and append.
	payload := jsonBytes
	if l.integrityMode == "mac" {
		mac := computeHMAC(jsonBytes, l.macKey)
		// Append tab + HMAC to plaintext so it's encrypted together.
		payload = append(payload, '\t')
		payload = append(payload, mac...)
	}

	// Step 2: Encrypt.
	encrypted, err := encryptRecord(payload, l.encryptionKey)
	if err != nil {
		return "", err
	}

	// Step 3: CRC32 over encrypted bytes.
	encB64 := base64.StdEncoding.EncodeToString(encrypted)
	checksum := crc32.ChecksumIEEE([]byte(encB64))

	return fmt.Sprintf("%s\t%08x\n", encB64, checksum), nil
}

// writeToFile writes line to the given file handle, reopening if nil.
// Performs fsync after write.
func (l *AuditLogger) writeToFile(fp **os.File, path, line, label string) error {
	if *fp == nil {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("audit: create %s directory: %w", label, err)
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			return fmt.Errorf("audit: open %s file: %w", label, err)
		}
		*fp = f
	}

	_, err := fmt.Fprint(*fp, line)
	if err != nil {
		if closeErr := (*fp).Close(); closeErr != nil {
			l.logger.Warn("audit: close file after write failure", "label", label, "error", closeErr)
		}
		*fp = nil
		return fmt.Errorf("audit: write %s: %w", label, err)
	}

	if err := (*fp).Sync(); err != nil {
		return fmt.Errorf("audit: fsync %s: %w", label, err)
	}

	return nil
}

// rotate writes a rotation marker, renames files, and opens new ones.
// Caller must hold l.mu.
//
// Rotation sequence (crash-safe):
// 1. Write rotation_marker record to current file(s)
// 2. fsync current file(s)
// 3. Rename current files to timestamped names
// 4. Create new files
//
// Reference: Update U1.4, U1.5.
func (l *AuditLogger) rotate() error {
	// Step 1: Write rotation marker before renaming.
	marker := InteractionRecord{
		RecordID:       NewRecordID(),
		Timestamp:      time.Now().UTC(),
		OperationType:  "rotation_marker",
		PolicyDecision: "allowed",
		LatencyMs:      0,
	}
	markerLine, err := l.formatLine(marker)
	if err != nil {
		return fmt.Errorf("audit: format rotation marker: %w", err)
	}

	// Write marker to primary.
	if l.file != nil {
		if _, err := fmt.Fprint(l.file, markerLine); err != nil {
			return fmt.Errorf("audit: write rotation marker to primary: %w", err)
		}
		if err := l.file.Sync(); err != nil {
			return fmt.Errorf("audit: fsync rotation marker primary: %w", err)
		}
	}

	// Write marker to shadow.
	if l.dualWrite && l.shadowFile != nil {
		if _, err := fmt.Fprint(l.shadowFile, markerLine); err != nil {
			l.logger.Warn("audit: write rotation marker to shadow failed",
				"component", "audit",
				"error", err,
			)
		} else {
			_ = l.shadowFile.Sync()
		}
	}

	// Step 2: Close files.
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			return fmt.Errorf("audit: close primary for rotation: %w", err)
		}
		l.file = nil
	}

	if l.dualWrite && l.shadowFile != nil {
		if err := l.shadowFile.Close(); err != nil {
			l.logger.Warn("audit: close shadow for rotation failed",
				"component", "audit",
				"error", err,
			)
		}
		l.shadowFile = nil
	}

	// Step 3: Rename.
	ts := time.Now().UTC().Format("20060102-150405")
	dir := filepath.Dir(l.filePath)
	rotatedPrimary := filepath.Join(dir, fmt.Sprintf("interactions-%s.jsonl", ts))
	rotatedShadow := filepath.Join(dir, fmt.Sprintf("interactions-shadow-%s.jsonl", ts))

	if err := fsutil.RobustRename(l.filePath, rotatedPrimary); err != nil {
		return fmt.Errorf("audit: rename primary for rotation: %w", err)
	}

	if l.dualWrite {
		if err := fsutil.RobustRename(l.shadowPath, rotatedShadow); err != nil {
			l.logger.Warn("audit: rename shadow for rotation failed",
				"component", "audit",
				"error", err,
			)
		}
	}

	l.logger.Info("audit: log rotated",
		"component", "audit",
		"rotated_to", rotatedPrimary,
	)

	// Step 4: Open new files.
	if err := l.openFile(); err != nil {
		return err
	}
	if l.dualWrite {
		if err := l.openShadowFile(); err != nil {
			return err
		}
	}

	return nil
}

// Close flushes and closes all audit log files.
func (l *AuditLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var firstErr error
	if l.file != nil {
		firstErr = l.file.Close()
		l.file = nil
	}
	if l.shadowFile != nil {
		if err := l.shadowFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		l.shadowFile = nil
	}
	return firstErr
}

// computeHMAC computes HMAC-SHA256 over data using key and returns the
// hex-encoded result. This key is SEPARATE from the WAL HMAC key.
//
// Reference: Update U1.1.
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
