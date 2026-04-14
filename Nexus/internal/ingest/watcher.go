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

import "context"

// WatcherState represents the lifecycle state of an Ingest watcher.
type WatcherState int

const (
	StateDisabled       WatcherState = iota // global or per-watcher kill switch
	StateNotDetected                        // AI client data directory not found
	StateDetectedPaused                     // detected but paused by user
	StateActive                             // running and ingesting
	StateError                              // unrecoverable error; logged, does not propagate
)

// String returns a human-readable label for the watcher state.
func (s WatcherState) String() string {
	switch s {
	case StateDisabled:
		return "disabled"
	case StateNotDetected:
		return "not_detected"
	case StateDetectedPaused:
		return "detected_paused"
	case StateActive:
		return "active"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// Watcher is the contract every Ingest parser implements. Each watcher
// knows how to detect, parse, and produce memories from one AI client's
// data directory.
type Watcher interface {
	// Name returns the stable identifier (e.g. "claude_code").
	Name() string

	// SourceName returns the synthetic source name for writes
	// (e.g. "ingest.claude_code").
	SourceName() string

	// DefaultPaths returns OS-specific candidate data directories.
	DefaultPaths() []string

	// Detect checks whether the AI client's data directory exists.
	// Returns (detected, resolved_path, error).
	Detect(ctx context.Context) (bool, string, error)

	// Parse reads new content from path starting at fromOffset and returns
	// extracted memories. Implementations must be safe for concurrent calls
	// on different paths but not on the same path.
	Parse(ctx context.Context, path string, fromOffset int64) (*ParseResult, error)

	// State returns the current lifecycle state.
	State() WatcherState

	// SetState transitions the watcher to a new state.
	SetState(WatcherState)
}

// IngestWriter is the interface that the Manager uses to write memories
// into the Nexus pipeline. The daemon provides an adapter that calls the
// internal write path directly (no HTTP handler invoked).
type IngestWriter interface {
	Write(ctx context.Context, source string, memory Memory) error
}
