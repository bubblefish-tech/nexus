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
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// AuditFilter defines the query parameters for filtering interaction records.
// All fields are optional; zero-value means no filter on that field.
//
// Reference: Tech Spec Addendum Section A2.5.
type AuditFilter struct {
	Source         string    // Filter by source name
	ActorType      string    // Filter: user, agent, system
	ActorID        string    // Filter by specific actor
	Operation      string    // Filter: write, query, admin
	PolicyDecision string    // Filter: allowed, denied, filtered
	Subject        string    // Filter by subject namespace
	Destination    string    // Filter by destination
	After          time.Time // Records after this timestamp
	Before         time.Time // Records before this timestamp
	Limit          int       // Max records (1–1000), default 100
	Offset         int       // Pagination offset
}

// QueryResult is the response from AuditReader.Query.
//
// Reference: Tech Spec Addendum Section A2.5.
type QueryResult struct {
	Records       []InteractionRecord `json:"records"`
	TotalMatching int                 `json:"total_matching"`
	Limit         int                 `json:"limit"`
	Offset        int                 `json:"offset"`
	HasMore       bool                `json:"has_more"`
}

// AuditReader reads and queries the interaction log files.
// It discovers all rotated log files via glob, parses JSONL, validates CRC32,
// and applies filters. Supports shadow fallback and cross-segment dedup.
//
// Reference: Tech Spec Addendum Section A2.5, Update U1.3–U1.5.
type AuditReader struct {
	logDir   string // Directory containing interaction log files
	baseName string // Base name of the current log file (e.g., "interactions.jsonl")

	// Integrity mode: "crc32" (default) or "mac".
	integrityMode string
	macKey        []byte // SEPARATE from WAL HMAC key

	// Encryption: when enabled, entries are decrypted before parsing.
	encryptionEnabled bool
	encryptionKey     []byte // SEPARATE from WAL encryption key

	// Dual-write: when true, shadow fallback is attempted on CRC failure.
	dualWrite bool

	// Metrics counters (atomic for thread safety).
	shadowRecoveries atomic.Int64
	crcFailures      atomic.Int64

	logger *slog.Logger
}

// ReaderOption configures an AuditReader.
type ReaderOption func(*AuditReader)

// WithReaderIntegrity sets the integrity mode and HMAC key for validation.
// This key is SEPARATE from the WAL HMAC key.
func WithReaderIntegrity(mode string, key []byte) ReaderOption {
	return func(r *AuditReader) {
		r.integrityMode = mode
		r.macKey = key
	}
}

// WithReaderEncryption enables decryption using the given 32-byte AES-256 key.
// This key is SEPARATE from the WAL encryption key.
//
// Reference: Update U1.2.
func WithReaderEncryption(key []byte) ReaderOption {
	return func(r *AuditReader) {
		r.encryptionEnabled = true
		r.encryptionKey = key
	}
}

// WithReaderDualWrite enables shadow fallback on CRC failure.
//
// Reference: Update U1.3.
func WithReaderDualWrite(enabled bool) ReaderOption {
	return func(r *AuditReader) {
		r.dualWrite = enabled
	}
}

// WithReaderLogger sets the structured logger.
func WithReaderLogger(logger *slog.Logger) ReaderOption {
	return func(r *AuditReader) {
		r.logger = logger
	}
}

// NewAuditReader creates an AuditReader for the given log file path.
// The reader discovers rotated files in the same directory via glob.
func NewAuditReader(logFile string, opts ...ReaderOption) *AuditReader {
	r := &AuditReader{
		logDir:        filepath.Dir(logFile),
		baseName:      filepath.Base(logFile),
		integrityMode: "crc32",
		dualWrite:     true,
		logger:        slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// ShadowRecoveries returns the count of records recovered from shadow after
// primary corruption. Reference: Update U1.7.
func (r *AuditReader) ShadowRecoveries() int64 {
	return r.shadowRecoveries.Load()
}

// CRCFailures returns the count of records where both primary and shadow had
// CRC32 mismatches. Reference: Update U1.7.
func (r *AuditReader) CRCFailures() int64 {
	return r.crcFailures.Load()
}

// Query reads all interaction log files, applies filters, and returns matching
// records with pagination. Files are read oldest-first (rotated files sorted by
// filename, then the current file).
//
// Rotation marker records are skipped. Cross-segment dedup by record_id.
//
// Reference: Tech Spec Addendum Section A2.5, Update U1.3–U1.5.
func (r *AuditReader) Query(filter AuditFilter) (QueryResult, error) {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	files, err := r.discoverPrimaryFiles()
	if err != nil {
		return QueryResult{}, err
	}

	seen := make(map[string]struct{}) // record_id dedup
	var allMatching []InteractionRecord
	for _, f := range files {
		records, err := r.readFileWithShadow(f)
		if err != nil {
			r.logger.Warn("audit: error reading log file",
				"component", "audit",
				"file", f,
				"error", err,
			)
			continue
		}
		for _, rec := range records {
			// Skip rotation markers (internal bookkeeping).
			if rec.OperationType == "rotation_marker" {
				continue
			}
			// Dedup by record_id across segments (crash recovery).
			if _, dup := seen[rec.RecordID]; dup {
				continue
			}
			seen[rec.RecordID] = struct{}{}

			if r.matchesFilter(rec, filter) {
				allMatching = append(allMatching, rec)
			}
		}
	}

	total := len(allMatching)
	result := QueryResult{
		TotalMatching: total,
		Limit:         filter.Limit,
		Offset:        filter.Offset,
	}

	// Apply offset and limit.
	if filter.Offset >= total {
		result.Records = []InteractionRecord{}
		return result, nil
	}

	end := filter.Offset + filter.Limit
	if end > total {
		end = total
	}
	result.Records = allMatching[filter.Offset:end]
	result.HasMore = end < total

	return result, nil
}

// Count returns the total number of records matching the filter (no pagination).
func (r *AuditReader) Count(filter AuditFilter) (int, error) {
	files, err := r.discoverPrimaryFiles()
	if err != nil {
		return 0, err
	}

	seen := make(map[string]struct{})
	count := 0
	for _, f := range files {
		records, err := r.readFileWithShadow(f)
		if err != nil {
			r.logger.Warn("audit: error reading log file",
				"component", "audit",
				"file", f,
				"error", err,
			)
			continue
		}
		for _, rec := range records {
			if rec.OperationType == "rotation_marker" {
				continue
			}
			if _, dup := seen[rec.RecordID]; dup {
				continue
			}
			seen[rec.RecordID] = struct{}{}
			if r.matchesFilter(rec, filter) {
				count++
			}
		}
	}
	return count, nil
}

// discoverPrimaryFiles finds all primary interaction log files: rotated files
// (sorted by filename, oldest first) followed by the current log file.
// Shadow files are excluded — they're used for fallback, not direct reading.
//
// Reference: Tech Spec Addendum Section A2.3, Update U1.4.
func (r *AuditReader) discoverPrimaryFiles() ([]string, error) {
	// Discover rotated files: interactions-YYYYMMDD-HHMMSS.jsonl
	// Exclude shadow files: interactions-shadow-*.jsonl
	rotatedPattern := filepath.Join(r.logDir, "interactions-*.jsonl")
	rotated, err := filepath.Glob(rotatedPattern)
	if err != nil {
		return nil, fmt.Errorf("audit: glob rotated files: %w", err)
	}

	// Filter out shadow files.
	var primaryRotated []string
	for _, f := range rotated {
		base := filepath.Base(f)
		if strings.HasPrefix(base, "interactions-shadow") {
			continue
		}
		primaryRotated = append(primaryRotated, f)
	}
	sort.Strings(primaryRotated) // Lexicographic = chronological for YYYYMMDD-HHMMSS

	// Current file goes last (newest).
	currentPath := filepath.Join(r.logDir, r.baseName)
	var files []string
	files = append(files, primaryRotated...)

	// Only add current file if it exists and is not already in rotated list.
	if _, err := os.Stat(currentPath); err == nil {
		alreadyIncluded := false
		for _, f := range primaryRotated {
			if f == currentPath {
				alreadyIncluded = true
				break
			}
		}
		if !alreadyIncluded {
			files = append(files, currentPath)
		}
	}

	return files, nil
}

// shadowPathFor returns the shadow file path corresponding to a primary file.
// For "interactions.jsonl" → "interactions-shadow.jsonl"
// For "interactions-YYYYMMDD-HHMMSS.jsonl" → "interactions-shadow-YYYYMMDD-HHMMSS.jsonl"
func (r *AuditReader) shadowPathFor(primaryPath string) string {
	dir := filepath.Dir(primaryPath)
	base := filepath.Base(primaryPath)
	return filepath.Join(dir, strings.Replace(base, "interactions", "interactions-shadow", 1))
}

// readFileWithShadow reads a primary file, falling back to the shadow file for
// any entry with a CRC32 mismatch. If both primary and shadow are invalid for
// the same entry, it's skipped.
//
// Reference: Update U1.3.
func (r *AuditReader) readFileWithShadow(primaryPath string) ([]InteractionRecord, error) {
	primaryLines, err := r.readRawLines(primaryPath)
	if err != nil {
		return nil, err
	}

	var shadowLines []string
	if r.dualWrite {
		shadowPath := r.shadowPathFor(primaryPath)
		if sl, err := r.readRawLines(shadowPath); err == nil {
			shadowLines = sl
		}
	}

	var records []InteractionRecord
	for i, line := range primaryLines {
		rec, err := r.parseLine(line)
		if err == nil {
			records = append(records, rec)
			continue
		}

		// Primary CRC failed — try shadow fallback.
		if r.dualWrite && i < len(shadowLines) {
			rec, err := r.parseLine(shadowLines[i])
			if err == nil {
				r.shadowRecoveries.Add(1)
				r.logger.Warn("audit: recovered entry from shadow after primary corruption",
					"component", "audit",
					"file", primaryPath,
					"line_number", i+1,
				)
				records = append(records, rec)
				continue
			}
		}

		// Both invalid — skip.
		r.crcFailures.Add(1)
		r.logger.Warn("audit: both primary and shadow corrupt — entry skipped",
			"component", "audit",
			"file", primaryPath,
			"line_number", i+1,
		)
	}

	return records, nil
}

// readRawLines reads all non-empty lines from a file.
func (r *AuditReader) readRawLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	defer f.Close()

	const maxScanBuf = 10 * 1024 * 1024 // 10 MB
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScanBuf)

	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return lines, fmt.Errorf("audit: scan %s: %w", path, err)
	}
	return lines, nil
}

// parseLine parses a single log line and validates its integrity.
// Returns the record, or error if CRC/HMAC/encryption validation fails.
func (r *AuditReader) parseLine(line string) (InteractionRecord, error) {
	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return InteractionRecord{}, fmt.Errorf("malformed line (missing CRC32)")
	}

	dataPart := parts[0]
	storedCRC := parts[1]

	// Validate CRC32 over the data part.
	computed := fmt.Sprintf("%08x", crc32.ChecksumIEEE([]byte(dataPart)))
	if computed != storedCRC {
		return InteractionRecord{}, fmt.Errorf("CRC32 mismatch: computed=%s stored=%s", computed, storedCRC)
	}

	// If encrypted, decode and decrypt.
	var jsonBytes []byte
	var hmacHex string

	if r.encryptionEnabled {
		encBytes, err := base64.StdEncoding.DecodeString(dataPart)
		if err != nil {
			return InteractionRecord{}, fmt.Errorf("base64 decode: %w", err)
		}
		plaintext, err := decryptRecord(encBytes, r.encryptionKey)
		if err != nil {
			return InteractionRecord{}, fmt.Errorf("decrypt: %w", err)
		}
		// Plaintext may contain tab + HMAC if HMAC mode was used.
		if r.integrityMode == "mac" {
			idx := strings.LastIndex(string(plaintext), "\t")
			if idx >= 0 {
				jsonBytes = plaintext[:idx]
				hmacHex = string(plaintext[idx+1:])
			} else {
				jsonBytes = plaintext
			}
		} else {
			jsonBytes = plaintext
		}
	} else {
		jsonBytes = []byte(dataPart)
		// HMAC is the third tab-separated field for plaintext mode.
		if r.integrityMode == "mac" && len(parts) >= 3 {
			hmacHex = parts[2]
		}
	}

	// Validate HMAC when integrity=mac.
	if r.integrityMode == "mac" && hmacHex != "" {
		if !validateHMAC(jsonBytes, r.macKey, hmacHex) {
			return InteractionRecord{}, fmt.Errorf("HMAC mismatch (possible tampering)")
		}
	}

	var record InteractionRecord
	if err := json.Unmarshal(jsonBytes, &record); err != nil {
		return InteractionRecord{}, fmt.Errorf("JSON unmarshal: %w", err)
	}

	return record, nil
}

// matchesFilter returns true if the record matches all non-zero filter criteria.
func (r *AuditReader) matchesFilter(rec InteractionRecord, f AuditFilter) bool {
	if f.Source != "" && rec.Source != f.Source {
		return false
	}
	if f.ActorType != "" && rec.ActorType != f.ActorType {
		return false
	}
	if f.ActorID != "" && rec.ActorID != f.ActorID {
		return false
	}
	if f.Operation != "" && rec.OperationType != f.Operation {
		return false
	}
	if f.PolicyDecision != "" && rec.PolicyDecision != f.PolicyDecision {
		return false
	}
	if f.Subject != "" && rec.Subject != f.Subject {
		return false
	}
	if f.Destination != "" && rec.Destination != f.Destination {
		return false
	}
	if !f.After.IsZero() && !rec.Timestamp.After(f.After) {
		return false
	}
	if !f.Before.IsZero() && !rec.Timestamp.Before(f.Before) {
		return false
	}
	return true
}
