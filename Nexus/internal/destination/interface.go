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

package destination

import (
	"context"
	"time"
)

// Memory is the canonical memory record exchanged with Destination adapters.
// It is a type alias for TranslatedPayload so that existing write-path code
// and the new Destination interface share a single concrete type.
type Memory = TranslatedPayload

// Query is the input parameter type for Destination.Search calls.
// It is a type alias for QueryParams so that callers that already use
// QueryParams need no changes.
type Query = QueryParams

// HealthStatus reports the liveness and responsiveness of a Destination.
type HealthStatus struct {
	// OK is true when the destination is reachable and accepting reads/writes.
	OK bool `json:"ok"`
	// Latency is the round-trip time of the most recent health probe.
	Latency time.Duration `json:"latency_ns"`
	// Error holds a human-readable description of any failure. Empty when OK.
	Error string `json:"error,omitempty"`
}

// Destination is the unified interface implemented by every memory backend.
// Adapters MUST be safe for concurrent use by multiple goroutines.
//
// Write and Read use pointer receivers so callers can distinguish a nil
// return (record not found) from an error. All methods accept a context so
// callers can enforce deadlines and propagate cancellation across backends.
//
// Reference: DB.1 — Destination Interface Definition.
type Destination interface {
	// Name returns a stable identifier for this destination (e.g. "sqlite",
	// "postgres"). Used in log output and error messages.
	Name() string

	// Write persists memory to the destination. Implementations MUST be
	// idempotent: writing the same PayloadID twice must succeed without
	// producing a duplicate record.
	Write(ctx context.Context, memory *Memory) error

	// Read retrieves a single memory record by its PayloadID. Returns nil, nil
	// when the record does not exist.
	Read(ctx context.Context, id string) (*Memory, error)

	// Search returns memories matching the supplied query parameters. Returns
	// an empty slice (not nil) when no records match.
	Search(ctx context.Context, query *Query) ([]*Memory, error)

	// Delete removes the record with the given PayloadID. Implementations
	// MUST treat deletion of a non-existent ID as a no-op (idempotent).
	Delete(ctx context.Context, id string) error

	// VectorSearch returns up to limit memories whose stored embedding is
	// nearest to the supplied embedding vector, ranked by cosine similarity
	// descending. Adapters that do not support vector search MUST return
	// ErrVectorSearchUnsupported.
	VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*Memory, error)

	// Migrate applies the schema migration identified by version. Callers
	// invoke this once per version bump; implementations MUST be idempotent.
	Migrate(ctx context.Context, version int) error

	// Health performs a lightweight liveness probe and returns the result.
	// It MUST NOT modify stored data.
	Health(ctx context.Context) (*HealthStatus, error)

	// Close releases all resources held by the destination. Safe to call once.
	Close() error
}

// ErrVectorSearchUnsupported is returned by Destination.VectorSearch
// implementations that do not support vector similarity search.
const ErrVectorSearchUnsupported = destinationError("vector search not supported by this destination")

type destinationError string

func (e destinationError) Error() string { return string(e) }
