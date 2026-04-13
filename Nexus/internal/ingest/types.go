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

package ingest

// Memory is a single memory extracted by a parser from an AI client's data.
// It carries the verbatim content and metadata needed by the write pipeline.
type Memory struct {
	Content        string
	Role           string // "user", "assistant", or "system"
	Model          string // if known from the source data
	Timestamp      int64  // Unix milliseconds; 0 means daemon fills in current time
	SourceMeta     map[string]string
	OriginalFile   string
	OriginalOffset int64
}

// ParseResult is the output of a single parser invocation on a file.
type ParseResult struct {
	Memories  []Memory
	NewOffset int64    // byte offset to resume from on next parse
	LastHash  [32]byte // SHA-256 of last 64 bytes before NewOffset, for truncation detection
}

// WatcherStatus is a snapshot of a watcher's state for status reporting.
type WatcherStatus struct {
	Name         string       `json:"name"`
	SourceName   string       `json:"source_name"`
	State        WatcherState `json:"state"`
	Path         string       `json:"path,omitempty"`
	IngestCount  int64        `json:"ingest_count"`
	LastIngestAt int64        `json:"last_ingest_at,omitempty"` // Unix seconds
}

// FileState is the persisted state for a single watched file.
type FileState struct {
	Watcher  string
	Path     string
	Offset   int64
	Hash     [32]byte
	LastSeen int64 // Unix seconds
}
