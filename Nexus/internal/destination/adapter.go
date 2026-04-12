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

// Package destination defines the DestinationWriter interface and canonical
// write envelope (TranslatedPayload) consumed by all memory backends.
//
// The WAL entry Payload field (json.RawMessage) is deserialized to
// TranslatedPayload by the queue worker before calling Write. Backends MUST
// treat payloads as idempotent: re-delivery of the same PayloadID must not
// produce a duplicate record.
package destination

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TranslatedPayload is the canonical write envelope produced by the field
// mapping and transform stages of the write path. It is stored in the WAL
// Payload field as raw JSON and deserialized by the queue worker before
// handing off to a DestinationWriter.
//
// Reference: Tech Spec Section 7.1.
type TranslatedPayload struct {
	PayloadID        string            `json:"payload_id"`
	RequestID        string            `json:"request_id"`
	Source           string            `json:"source"`
	Subject          string            `json:"subject"`
	Namespace        string            `json:"namespace"`
	Destination      string            `json:"destination"`
	Collection       string            `json:"collection"`
	Content          string            `json:"content"`
	Model            string            `json:"model"`
	Role             string            `json:"role"`
	Timestamp        time.Time         `json:"timestamp"`
	IdempotencyKey   string            `json:"idempotency_key"`
	SchemaVersion    int               `json:"schema_version"`
	TransformVersion string            `json:"transform_version"`
	ActorType        string            `json:"actor_type"`  // user, agent, or system
	ActorID          string            `json:"actor_id"`    // identity of the actor
	Embedding        []float32         `json:"embedding,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`

	// Addendum A3: Retrieval Firewall — sensitivity classification fields.
	// SensitivityLabels are free-form tags (e.g. "pii", "financial").
	// ClassificationTier is a single tier from the configured tier_order
	// (default: "public"). Both default to empty/public when absent.
	// Reference: Tech Spec Addendum Section A3.2.
	SensitivityLabels  []string `json:"sensitivity_labels,omitempty"`
	ClassificationTier string   `json:"classification_tier,omitempty"`

	// Tier is the numeric access tier of this entry (0-3, default 1).
	// Sources may only read entries where entry.Tier <= source.Tier.
	// Enforcement is in the SQL WHERE clause, not post-filter.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	Tier int `json:"tier,omitempty"`

	// LSHBucket is the 16-bit SimHash bucket ID computed from the embedding
	// and the tier-scoped hyperplane vectors. Used as a prefilter for cluster
	// assignment: only entries in the same (tier, lsh_bucket) are candidates.
	// Zero when no embedding is available.
	// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.2.
	LSHBucket int `json:"lsh_bucket,omitempty"`

	// ClusterID groups semantically similar memories. Entries with the same
	// ClusterID share a conceptual topic. Empty when not yet clustered.
	// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.2.
	ClusterID string `json:"cluster_id,omitempty"`

	// ClusterRole indicates this entry's role within its cluster:
	// "primary" (representative), "member", or "superseded".
	// Empty when not yet clustered.
	// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.2.
	ClusterRole string `json:"cluster_role,omitempty"`
}

// DestinationWriter is the interface satisfied by every memory backend
// (SQLite, PostgreSQL, Supabase, etc.). All implementations MUST be safe for
// concurrent use by multiple goroutines.
//
// Reference: Tech Spec Section 3.2.
type DestinationWriter interface {
	// Write persists p to the destination. Implementations MUST be idempotent:
	// writing the same PayloadID twice must succeed without producing a
	// duplicate record.
	Write(p TranslatedPayload) error

	// Ping verifies the destination is reachable and healthy. Used by the
	// doctor command and /ready health endpoint.
	Ping() error

	// Exists reports whether a record with the given payloadID has been
	// written to the destination. Used by consistency assertions (Phase R-10).
	Exists(payloadID string) (bool, error)

	// Close releases all resources held by the destination. Safe to call once.
	Close() error
}

// QueryParams are the input parameters for a basic structured query.
// The full 6-stage retrieval cascade (Phase 3+) builds on top of this.
//
// Reference: Tech Spec Section 3.3, Section 3.8.
type QueryParams struct {
	// Destination is the target destination name.
	Destination string
	// Namespace filters results to a specific namespace.
	Namespace string
	// Subject filters results to a specific subject. Empty means all subjects.
	Subject string
	// Q is a text substring filter applied to the content field. Empty means no filter.
	Q string
	// Limit is the maximum number of records to return. Callers must cap at 200.
	Limit int
	// Cursor is the opaque pagination cursor from a previous QueryResult.NextCursor.
	Cursor string
	// Profile is the retrieval profile (fast, balanced, deep). Used by later phases.
	Profile string
	// ActorType filters results by provenance (user, agent, system). Empty means
	// no filter.
	//
	// Reference: Tech Spec Section 7.1.
	ActorType string

	// TierFilter, when true, adds `AND tier <= SourceTier` to the WHERE clause.
	// Enforcement happens in the SQL layer — not as a post-filter — so the
	// database engine never touches rows the caller is not authorised to see.
	// This eliminates timing side-channels from row-count variations.
	// Set to false for admin queries that must see all tiers.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	TierFilter bool
	// SourceTier is the requesting source's tier level (0-3). Only meaningful
	// when TierFilter = true.
	SourceTier int
}

// QueryResult holds one page of query results and pagination state.
//
// Reference: Tech Spec Section 3.8.
type QueryResult struct {
	Records    []TranslatedPayload
	NextCursor string
	HasMore    bool
}

// Querier is the read interface satisfied by memory backends. It is separate
// from DestinationWriter to allow read-only facade implementations and to
// keep the write-path interface minimal.
//
// Reference: Tech Spec Section 3.3, Section 12.
type Querier interface {
	// Query returns a page of memories matching params.
	Query(params QueryParams) (QueryResult, error)
}

// MemoryCounter is an optional interface for destination backends that support
// counting the total number of stored memories. Used by the /api/status admin
// endpoint to populate the memories_total field. Callers must type-assert.
type MemoryCounter interface {
	// MemoryCount returns the total number of memory records in the destination.
	MemoryCount() (int64, error)
}

// ConflictGroup represents a set of contradictory memories sharing the same
// subject and collection (entity_key) but with divergent content.
// Reference: Tech Spec Section 13.2 — Conflict Inspector.
type ConflictGroup struct {
	Subject          string               `json:"subject"`
	EntityKey        string               `json:"entity_key"` // collection field
	ConflictingValues []string            `json:"conflicting_values"`
	Sources          []string             `json:"sources"`
	Timestamps       []time.Time          `json:"timestamps"`
	Count            int                  `json:"count"`
}

// ConflictParams holds filter and pagination options for conflict queries.
type ConflictParams struct {
	Source    string // filter by source
	Subject   string // filter by subject
	ActorType string // filter by actor_type
	Limit     int
	Offset    int
}

// ConflictQuerier is an optional interface for destination backends that
// support conflict detection. Callers must type-assert to check for support.
// Reference: Tech Spec Section 13.2.
type ConflictQuerier interface {
	// QueryConflicts returns groups of contradictory memories.
	QueryConflicts(params ConflictParams) ([]ConflictGroup, error)
}

// TimeTravelParams holds options for time-travel queries.
type TimeTravelParams struct {
	AsOf        time.Time // return memories with timestamp <= this value
	Namespace   string
	Subject     string
	Destination string
	Limit       int
	Offset      int
}

// TimeTravelQuerier is an optional interface for destination backends that
// support time-travel queries. Callers must type-assert to check for support.
// Reference: Tech Spec Section 13.2.
type TimeTravelQuerier interface {
	// QueryTimeTravel returns memories as of the specified timestamp.
	QueryTimeTravel(params TimeTravelParams) (QueryResult, error)
}

// ScoredRecord pairs a TranslatedPayload with its cosine similarity score
// from a semantic search. Used by Stage 4 (Semantic Retrieval).
//
// Reference: Tech Spec Section 3.4 — Stage 4.
type ScoredRecord struct {
	Payload TranslatedPayload
	Score   float32
}

// SemanticSearcher is implemented by destination backends that support vector
// similarity search. It is an optional extension of DestinationWriter — callers
// must type-assert to check for support.
//
// CanSemanticSearch must be checked before calling SemanticSearch. Destinations
// that have no indexed embeddings (e.g. a fresh SQLite DB with no writes) may
// return false to skip Stage 4 gracefully.
//
// Reference: Tech Spec Section 3.4 — Stage 4.
type SemanticSearcher interface {
	// CanSemanticSearch reports whether this destination has vector index support
	// and at least one indexed embedding. Returns false to signal graceful skip.
	CanSemanticSearch() bool

	// SemanticSearch returns up to params.Limit records nearest to vec, ranked
	// by cosine similarity descending. Filters are applied from params
	// (Namespace, Destination). Implementations must be safe for concurrent use.
	SemanticSearch(ctx context.Context, vec []float32, params QueryParams) ([]ScoredRecord, error)
}

// ClusterQueryParams holds the options for querying cluster members.
// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.4.
type ClusterQueryParams struct {
	// ClusterID is the cluster to query.
	ClusterID string
	// TierFilter, when true, adds tier enforcement to the WHERE clause.
	TierFilter bool
	// SourceTier is the requesting source's access tier.
	SourceTier int
}

// ClusterQuerier is an optional interface for destination backends that
// support cluster-aware retrieval. Callers must type-assert to check for
// support.
// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.4.
type ClusterQuerier interface {
	// QueryClusterMembers returns all members of the specified cluster.
	QueryClusterMembers(params ClusterQueryParams) ([]TranslatedPayload, error)

	// QueryBucketCandidates returns entries in the same (tier, lsh_bucket) that
	// have an embedding, for cluster assignment. Limited to candidateLimit rows.
	QueryBucketCandidates(tier, bucket, candidateLimit int) ([]TranslatedPayload, error)

	// UpdateCluster sets the cluster_id and cluster_role for a given payload_id.
	UpdateCluster(payloadID, clusterID, clusterRole string) error
}

// ValidActorType reports whether s is one of the accepted provenance values:
// "user", "agent", or "system". Empty is NOT valid — callers must resolve
// defaults before calling this function.
//
// Reference: Tech Spec Section 7.1 — Provenance Semantics.
func ValidActorType(s string) bool {
	switch s {
	case "user", "agent", "system":
		return true
	default:
		return false
	}
}

const (
	// DefaultQueryLimit is applied when the client sends limit=0 or omits it.
	DefaultQueryLimit = 20
	// MaxQueryLimit is the hard cap regardless of what the client requests.
	MaxQueryLimit = 200
)

// ClampLimit enforces the default and maximum query limits.
// Reference: Tech Spec Phase 0C Behavioral Contract item 16.
func ClampLimit(requested int) int {
	if requested <= 0 {
		return DefaultQueryLimit
	}
	if requested > MaxQueryLimit {
		return MaxQueryLimit
	}
	return requested
}

// EncodeCursor encodes an integer offset as a URL-safe base64 cursor string.
func EncodeCursor(offset int) string {
	return base64.URLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

// DecodeCursor decodes a cursor string back to an integer offset.
// Returns 0 and no error for an empty cursor (first page).
func DecodeCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	b, err := base64.URLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("destination: invalid cursor: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("destination: invalid cursor value: %w", err)
	}
	return n, nil
}
