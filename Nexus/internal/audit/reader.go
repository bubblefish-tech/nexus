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
	"encoding/json"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
// and applies filters.
//
// Reference: Tech Spec Addendum Section A2.5.
type AuditReader struct {
	logDir   string // Directory containing interaction log files
	baseName string // Base name of the current log file (e.g., "interactions.jsonl")

	// Integrity mode: "crc32" (default) or "mac".
	integrityMode string
	macKey        []byte

	logger *slog.Logger
}

// ReaderOption configures an AuditReader.
type ReaderOption func(*AuditReader)

// WithReaderIntegrity sets the integrity mode and HMAC key for validation.
func WithReaderIntegrity(mode string, key []byte) ReaderOption {
	return func(r *AuditReader) {
		r.integrityMode = mode
		r.macKey = key
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
		logger:        slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Query reads all interaction log files, applies filters, and returns matching
// records with pagination. Files are read oldest-first (rotated files sorted by
// filename, then the current file).
//
// CRC32 is validated on each entry; entries with CRC mismatch are skipped with
// a WARN log. When integrity=mac, HMAC is also validated.
//
// Reference: Tech Spec Addendum Section A2.5.
func (r *AuditReader) Query(filter AuditFilter) (QueryResult, error) {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	files, err := r.discoverFiles()
	if err != nil {
		return QueryResult{}, err
	}

	var allMatching []InteractionRecord
	for _, f := range files {
		records, err := r.readFile(f)
		if err != nil {
			r.logger.Warn("audit: error reading log file",
				"component", "audit",
				"file", f,
				"error", err,
			)
			continue
		}
		for _, rec := range records {
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
	files, err := r.discoverFiles()
	if err != nil {
		return 0, err
	}

	count := 0
	for _, f := range files {
		records, err := r.readFile(f)
		if err != nil {
			r.logger.Warn("audit: error reading log file",
				"component", "audit",
				"file", f,
				"error", err,
			)
			continue
		}
		for _, rec := range records {
			if r.matchesFilter(rec, filter) {
				count++
			}
		}
	}
	return count, nil
}

// discoverFiles finds all interaction log files: rotated files (sorted by
// filename, oldest first) followed by the current log file.
//
// Reference: Tech Spec Addendum Section A2.3.
func (r *AuditReader) discoverFiles() ([]string, error) {
	// Discover rotated files: interactions-YYYYMMDD-HHMMSS.jsonl
	rotatedPattern := filepath.Join(r.logDir, "interactions-*.jsonl")
	rotated, err := filepath.Glob(rotatedPattern)
	if err != nil {
		return nil, fmt.Errorf("audit: glob rotated files: %w", err)
	}
	sort.Strings(rotated) // Lexicographic = chronological for YYYYMMDD-HHMMSS

	// Current file goes last (newest).
	currentPath := filepath.Join(r.logDir, r.baseName)
	var files []string
	files = append(files, rotated...)

	// Only add current file if it exists and is not already in rotated list.
	if _, err := os.Stat(currentPath); err == nil {
		alreadyIncluded := false
		for _, f := range rotated {
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

// readFile parses a single JSONL log file, validating CRC32 (and HMAC when
// integrity=mac) on each entry. Entries with integrity failures are skipped.
//
// Uses a 10MB scanner buffer, same as WAL.
func (r *AuditReader) readFile(path string) ([]InteractionRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("audit: open %s: %w", path, err)
	}
	defer f.Close()

	const maxScanBuf = 10 * 1024 * 1024 // 10 MB
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScanBuf)

	var records []InteractionRecord
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			r.logger.Warn("audit: malformed line (missing CRC32)",
				"component", "audit",
				"file", path,
				"line_number", lineNum,
			)
			continue
		}

		jsonBytes := []byte(parts[0])
		storedCRC := parts[1]

		// Validate CRC32.
		computed := fmt.Sprintf("%08x", crc32.ChecksumIEEE(jsonBytes))
		if computed != storedCRC {
			r.logger.Warn("audit: CRC32 mismatch — entry skipped",
				"component", "audit",
				"file", path,
				"line_number", lineNum,
				"expected", storedCRC,
				"computed", computed,
			)
			continue
		}

		// Validate HMAC when integrity=mac.
		if r.integrityMode == "mac" && len(parts) >= 3 {
			storedHMAC := parts[2]
			if !validateHMAC(jsonBytes, r.macKey, storedHMAC) {
				r.logger.Warn("audit: HMAC mismatch — entry skipped (possible tampering)",
					"component", "audit",
					"file", path,
					"line_number", lineNum,
				)
				continue
			}
		}

		var record InteractionRecord
		if err := json.Unmarshal(jsonBytes, &record); err != nil {
			r.logger.Warn("audit: JSON unmarshal failed — entry skipped",
				"component", "audit",
				"file", path,
				"line_number", lineNum,
				"error", err,
			)
			continue
		}

		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		return records, fmt.Errorf("audit: scan %s: %w", path, err)
	}

	return records, nil
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
