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
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// DedupCache tracks recently written content hashes to avoid duplicate writes.
// Thread-safe via a mutex.
type DedupCache struct {
	mu      sync.Mutex
	entries map[string]dedupEntry
	window  time.Duration
}

type dedupEntry struct {
	payloadID string
	writtenAt time.Time
}

// NewDedupCache creates a dedup cache with the given window duration.
func NewDedupCache(window time.Duration) *DedupCache {
	return &DedupCache{
		entries: make(map[string]dedupEntry),
		window:  window,
	}
}

// Check returns the existing payload ID if identical content was written
// within the dedup window. Returns "" if no duplicate found.
func (c *DedupCache) Check(content string) string {
	hash := contentHash(content)
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[hash]; ok {
		if time.Since(e.writtenAt) < c.window {
			return e.payloadID
		}
		delete(c.entries, hash)
	}
	return ""
}

// Record stores a content hash → payload ID mapping.
func (c *DedupCache) Record(content, payloadID string) {
	hash := contentHash(content)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[hash] = dedupEntry{payloadID: payloadID, writtenAt: time.Now()}
}

func contentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}
