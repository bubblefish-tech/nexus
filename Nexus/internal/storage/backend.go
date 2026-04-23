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

// Package storage defines the Backend interface that all Nexus subsystems
// use for durable storage. Implementations live in sub-packages (sqlite,
// postgres in v0.1.4+).
//
// This is a unification layer over the existing destination.Destination,
// destination.Querier, and optional extension interfaces. It does NOT
// change any behavior — it consolidates the surface area.
//
// Reference: Bombproof Build Plan Phase BP.0.
package storage

import (
	"context"
	"database/sql"

	"github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/bubblefish-tech/nexus/internal/destination"
)

// Backend is the unified storage interface for Nexus.
//
// Every method maps 1:1 to an existing destination interface method.
// The goal is to remove scattered type assertions in daemon.go and
// handlers.go and replace them with a single interface that advertises
// all capabilities upfront.
//
// Implementations: SQLiteBackend (v0.1.3), PostgresBackend (v0.1.4+).
type Backend interface {
	// ── Core (from destination.Destination) ──

	Name() string
	Write(p destination.TranslatedPayload) error
	Close() error
	Ping() error
	Exists(payloadID string) (bool, error)

	// ── Query (from destination.Querier) ──

	Query(params destination.QueryParams) (destination.QueryResult, error)

	// ── Semantic search (from destination.SemanticSearcher) ──

	CanSemanticSearch() bool
	SemanticSearch(ctx context.Context, vec []float32, params destination.QueryParams) ([]destination.ScoredRecord, error)

	// ── Counts (from destination.MemoryCounter) ──

	MemoryCount() (int64, error)

	// ── Conflict detection (from destination.ConflictQuerier) ──

	QueryConflicts(params destination.ConflictParams) ([]destination.ConflictGroup, error)

	// ── Time-travel (from destination.TimeTravelQuerier) ──

	QueryTimeTravel(params destination.TimeTravelParams) (destination.QueryResult, error)

	// ── Encryption (from SQLiteDestination.SetEncryption) ──

	SetEncryption(mkm *crypto.MasterKeyManager)

	// ── Raw DB access (temporary — removed in BP.4 when BM25/temporal
	// bin callers are migrated to Backend methods) ──
	// TODO(BP.4): Remove RawDB() once BM25Searcher and temporal bin
	// refresh are expressed as Backend methods.

	RawDB() *sql.DB

	// ── Capabilities ──

	Capabilities() Capabilities
}

// Capabilities advertises what a Backend implementation supports.
// Callers check this instead of type-asserting.
type Capabilities struct {
	ConcurrentWriters    bool   // false for SQLite, true for PG
	NativeVectorIndex    bool   // sqlite-vec or pgvector
	FTSRanking           string // "bm25" | "ts_rank_cd"
	SemanticSearch       bool   // CanSemanticSearch() available
	ConflictDetection    bool   // QueryConflicts() available
	TimeTravel           bool   // QueryTimeTravel() available
	ClusterQueries       bool   // cluster member/bucket queries
	Encryption           bool   // SetEncryption() supported
}
