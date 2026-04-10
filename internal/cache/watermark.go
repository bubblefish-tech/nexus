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

package cache

import "sync"

// WatermarkStore maintains a monotonically increasing counter per destination.
// Cache entries store the watermark at insertion time; an entry is stale when
// its stored watermark is less than the current destination watermark.
//
// Watermarks are advanced by the write path each time a payload is delivered to
// a destination. This invalidates all exact-cache entries for that destination
// without requiring an explicit cache scan.
//
// Reference: Tech Spec Section 3.4 — Stage 1 (watermark freshness check).
type WatermarkStore struct {
	mu    sync.Mutex
	marks map[string]uint64
}

// NewWatermarkStore creates an empty WatermarkStore. All destinations start at
// watermark 0; the first Advance call moves them to 1.
func NewWatermarkStore() *WatermarkStore {
	return &WatermarkStore{
		marks: make(map[string]uint64),
	}
}

// Current returns the current watermark for dest. Returns 0 for unknown
// destinations (no writes have been delivered yet).
func (w *WatermarkStore) Current(dest string) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.marks[dest]
}

// Advance increments the watermark for dest and returns the new value. After
// Advance returns, any cache entry that recorded the previous watermark is
// considered stale and will be rejected by ExactCache.Get.
func (w *WatermarkStore) Advance(dest string) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.marks[dest]++
	return w.marks[dest]
}
