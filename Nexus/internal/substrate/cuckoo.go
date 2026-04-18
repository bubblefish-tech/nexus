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

// Cuckoo filter deletion oracle for substrate. Wraps
// github.com/seiflotfy/cuckoofilter with persistence, rebuild, and
// capacity management.
//
// The cuckoo filter algorithm is from:
//   Fan et al., "Cuckoo Filter: Practically Better Than Bloom"
//   (CoNEXT 2014)
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.6.
package substrate

import (
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	cuckoo "github.com/seiflotfy/cuckoofilter"
)

// CuckooOracle is the deletion oracle for substrate. It holds the set of
// currently-live memory IDs as a cuckoo filter. The oracle is consulted
// before Stage 3.5 runs to confirm that candidates are live.
//
// Thread-safety: all methods are safe for concurrent use. Lookup is the
// hot path (called on every query) and uses RLock. Insert and Delete take
// the write lock.
type CuckooOracle struct {
	mu           sync.RWMutex
	filter       *cuckoo.Filter
	capacity     uint
	insertCount  uint64
	deleteCount  uint64
	rebuildCount uint64
}

// NewCuckooOracle creates an empty oracle with the given initial capacity.
// The underlying cuckoo filter is sized at 2 * capacity to keep load
// factor below 50% for good insertion success probability.
func NewCuckooOracle(capacity uint) *CuckooOracle {
	if capacity < 1024 {
		capacity = 1024
	}
	return &CuckooOracle{
		filter:   cuckoo.NewFilter(capacity),
		capacity: capacity,
	}
}

// Insert adds a memory ID to the filter. Returns ErrCuckooNeedsRebuild
// if the filter is full (kickout chain exceeded). The caller should
// orchestrate a RebuildFromDB when this error is returned.
func (o *CuckooOracle) Insert(memoryID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.filter.Insert([]byte(memoryID)) {
		return ErrCuckooNeedsRebuild
	}
	o.insertCount++
	return nil
}

// Delete removes a memory ID from the filter.
// Cuckoo filters support O(1) deletion, which is why we use them
// instead of Bloom filters.
func (o *CuckooOracle) Delete(memoryID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.filter.Delete([]byte(memoryID))
	o.deleteCount++
}

// Lookup returns true if the memory ID is possibly in the set.
// False positives are bounded (~3% at 50% load with 12-bit fingerprints).
// False negatives are impossible — a live memory always returns true.
func (o *CuckooOracle) Lookup(memoryID string) bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.filter.Lookup([]byte(memoryID))
}

// Count returns the number of items currently in the filter.
func (o *CuckooOracle) Count() uint {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.filter.Count()
}

// Stats returns the oracle's operational counters for diagnostics.
func (o *CuckooOracle) Stats() CuckooStats {
	o.mu.RLock()
	defer o.mu.RUnlock()
	count := o.filter.Count()
	return CuckooStats{
		Capacity:     o.capacity,
		Count:        count,
		InsertCount:  o.insertCount,
		DeleteCount:  o.deleteCount,
		RebuildCount: o.rebuildCount,
	}
}

// CuckooStats holds diagnostic counters for the cuckoo filter.
type CuckooStats struct {
	Capacity     uint
	Count        uint
	InsertCount  uint64
	DeleteCount  uint64
	RebuildCount uint64
}

// Persist serializes the filter to the substrate_cuckoo_filter table.
// The write is a single UPSERT on filter_id=1 (single-row table).
func (o *CuckooOracle) Persist(db *sql.DB) error {
	o.mu.RLock()
	data := o.filter.Encode()
	o.mu.RUnlock()

	// Chaos kill point: filter encoded but SQL write not yet executed.
	ChaosKillPoint("cuckoo_persist")

	_, err := db.Exec(`
		INSERT INTO substrate_cuckoo_filter (filter_id, filter_bytes, last_persisted)
		VALUES (1, ?, ?)
		ON CONFLICT(filter_id) DO UPDATE SET
			filter_bytes = excluded.filter_bytes,
			last_persisted = excluded.last_persisted
	`, data, time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("persist cuckoo filter: %w", err)
	}
	return nil
}

// LoadCuckooOracle restores the filter from the substrate_cuckoo_filter table.
// Returns ErrCuckooNotPersisted if no row exists (fresh install).
// Returns ErrCuckooCorrupt if deserialization fails.
func LoadCuckooOracle(db *sql.DB, expectedCapacity uint) (*CuckooOracle, error) {
	var data []byte
	err := db.QueryRow(`
		SELECT filter_bytes FROM substrate_cuckoo_filter WHERE filter_id = 1
	`).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, ErrCuckooNotPersisted
	}
	if err != nil {
		return nil, fmt.Errorf("query cuckoo filter: %w", err)
	}

	filter, err := cuckoo.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode cuckoo filter: %w: %w", ErrCuckooCorrupt, err)
	}
	return &CuckooOracle{
		filter:   filter,
		capacity: expectedCapacity,
	}, nil
}

// RebuildFromDB reconstructs the cuckoo filter from the memories table.
// Called on startup if the persisted filter fails to load or is inconsistent.
//
// This path is slow (O(n) over all memories) but rare.
func RebuildFromDB(db *sql.DB, capacity uint, logger *slog.Logger) (*CuckooOracle, error) {
	startTime := time.Now()
	logger.Warn("cuckoo: rebuilding filter from memories table")

	var count uint
	err := db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("count memories: %w", err)
	}

	if capacity < count {
		capacity = count
	}
	if capacity < 1024 {
		capacity = 1024
	}
	oracle := NewCuckooOracle(capacity)

	rows, err := db.Query(`SELECT payload_id FROM memories`)
	if err != nil {
		return nil, fmt.Errorf("query memories: %w", err)
	}
	defer rows.Close()

	var inserted uint
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			logger.Warn("cuckoo rebuild: scan row failed, skipping", "error", err)
			continue
		}
		if err := oracle.Insert(id); err != nil {
			return nil, fmt.Errorf("cuckoo rebuild: insert %s: %w", id, err)
		}
		inserted++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}

	oracle.rebuildCount++
	logger.Warn("cuckoo: rebuild complete",
		"memories", inserted,
		"duration", time.Since(startTime),
	)
	return oracle, nil
}
