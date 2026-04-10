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

// Package idempotency provides an in-memory deduplication store for
// BubbleFish Nexus write requests.
//
// The store is always empty on process start and rebuilt exclusively from WAL
// replay. It is never persisted to disk. This invariant ensures that duplicate
// detection is consistent with the durable WAL state after a crash.
package idempotency

import "sync"

// Store is a thread-safe in-memory deduplication map. All state is in struct
// fields; there are no package-level variables.
type Store struct {
	mu   sync.RWMutex
	keys map[string]string // idempotency_key → payload_id
	ids  map[string]bool   // payload_id set for Exists checks
}

// New returns an empty Store ready for use.
func New() *Store {
	return &Store{
		keys: make(map[string]string),
		ids:  make(map[string]bool),
	}
}

// Register records an idempotency_key → payload_id mapping. If the key is
// already registered, the existing mapping is overwritten (idempotent for WAL
// replay where the same PENDING entry may appear in multiple segments).
func (s *Store) Register(key, payloadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys[key] = payloadID
	s.ids[payloadID] = true
}

// Seen returns the payload_id previously registered for key, and true.
// Returns ("", false) if the key has not been seen.
func (s *Store) Seen(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.keys[key]
	return id, ok
}

// PayloadID returns true if id has been registered as a payload_id.
// Used by consistency assertions (Phase R-10).
func (s *Store) PayloadID(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ids[id]
}
