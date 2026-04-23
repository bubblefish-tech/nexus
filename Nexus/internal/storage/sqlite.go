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

package storage

import (
	"context"
	"database/sql"

	"github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/bubblefish-tech/nexus/internal/destination"
)

// SQLiteBackend adapts the existing destination.SQLiteDestination to the
// storage.Backend interface. Pure delegation — no behavior changes.
//
// Reference: Bombproof Build Plan Phase BP.0.2.
type SQLiteBackend struct {
	dst *destination.SQLiteDestination
}

// NewSQLiteBackend wraps an existing SQLiteDestination as a Backend.
func NewSQLiteBackend(dst *destination.SQLiteDestination) *SQLiteBackend {
	return &SQLiteBackend{dst: dst}
}

// Unwrap returns the underlying SQLiteDestination for callers that need
// direct access during the migration period.
// TODO(BP.4): Remove Unwrap once all direct callers are migrated.
func (s *SQLiteBackend) Unwrap() *destination.SQLiteDestination {
	return s.dst
}

func (s *SQLiteBackend) Name() string                              { return s.dst.Name() }
func (s *SQLiteBackend) Write(p destination.TranslatedPayload) error { return s.dst.Write(p) }
func (s *SQLiteBackend) Close() error                              { return s.dst.Close() }
func (s *SQLiteBackend) Ping() error                               { return s.dst.Ping() }
func (s *SQLiteBackend) Exists(payloadID string) (bool, error)     { return s.dst.Exists(payloadID) }

func (s *SQLiteBackend) Query(params destination.QueryParams) (destination.QueryResult, error) {
	return s.dst.Query(params)
}

func (s *SQLiteBackend) CanSemanticSearch() bool { return s.dst.CanSemanticSearch() }

func (s *SQLiteBackend) SemanticSearch(ctx context.Context, vec []float32, params destination.QueryParams) ([]destination.ScoredRecord, error) {
	return s.dst.SemanticSearch(ctx, vec, params)
}

func (s *SQLiteBackend) MemoryCount() (int64, error) { return s.dst.MemoryCount() }

func (s *SQLiteBackend) QueryConflicts(params destination.ConflictParams) ([]destination.ConflictGroup, error) {
	return s.dst.QueryConflicts(params)
}

func (s *SQLiteBackend) QueryTimeTravel(params destination.TimeTravelParams) (destination.QueryResult, error) {
	return s.dst.QueryTimeTravel(params)
}

func (s *SQLiteBackend) SetEncryption(mkm *crypto.MasterKeyManager) {
	s.dst.SetEncryption(mkm)
}

// RawDB returns the underlying *sql.DB for callers that still need direct
// access (BM25 searcher, temporal bin refresh, integrity checks).
// TODO(BP.4): Remove once these callers are expressed as Backend methods.
func (s *SQLiteBackend) RawDB() *sql.DB {
	return s.dst.DB()
}

func (s *SQLiteBackend) Capabilities() Capabilities {
	return Capabilities{
		ConcurrentWriters: false,
		NativeVectorIndex: false,
		FTSRanking:        "bm25",
		SemanticSearch:    s.dst.CanSemanticSearch(),
		ConflictDetection: true,
		TimeTravel:        true,
		ClusterQueries:    true,
		Encryption:        true,
	}
}

// Compile-time interface check.
var _ Backend = (*SQLiteBackend)(nil)
