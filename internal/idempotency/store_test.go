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

package idempotency

import (
	"fmt"
	"sync"
	"testing"
)

func TestStoreRegisterAndSeen(t *testing.T) {
	s := New()
	s.Register("key-1", "payload-1")

	id, ok := s.Seen("key-1")
	if !ok {
		t.Fatal("Seen: expected true for registered key")
	}
	if id != "payload-1" {
		t.Errorf("Seen: want payload-1, got %s", id)
	}
}

func TestStoreSeenUnknownKey(t *testing.T) {
	s := New()

	id, ok := s.Seen("nonexistent")
	if ok {
		t.Error("Seen: expected false for unknown key")
	}
	if id != "" {
		t.Errorf("Seen: expected empty string, got %q", id)
	}
}

func TestStoreRegisterIdempotent(t *testing.T) {
	// Registering the same key twice (as in WAL replay with duplicate segments)
	// must not panic and must keep the last-written value.
	s := New()
	s.Register("key-1", "payload-1")
	s.Register("key-1", "payload-1") // same mapping, idempotent

	id, ok := s.Seen("key-1")
	if !ok || id != "payload-1" {
		t.Errorf("Seen after idempotent Register: got (%s, %v)", id, ok)
	}
}

func TestStorePayloadIDExists(t *testing.T) {
	s := New()
	s.Register("key-1", "payload-abc")

	if !s.PayloadID("payload-abc") {
		t.Error("PayloadID: expected true for registered payload")
	}
	if s.PayloadID("payload-unknown") {
		t.Error("PayloadID: expected false for unknown payload")
	}
}

func TestStoreMultipleKeys(t *testing.T) {
	s := New()
	const n = 100
	for i := 0; i < n; i++ {
		s.Register(fmt.Sprintf("key-%d", i), fmt.Sprintf("payload-%d", i))
	}
	for i := 0; i < n; i++ {
		id, ok := s.Seen(fmt.Sprintf("key-%d", i))
		if !ok {
			t.Errorf("key-%d: expected found", i)
		}
		if want := fmt.Sprintf("payload-%d", i); id != want {
			t.Errorf("key-%d: want %s, got %s", i, want, id)
		}
		if !s.PayloadID(fmt.Sprintf("payload-%d", i)) {
			t.Errorf("payload-%d: expected PayloadID true", i)
		}
	}
}

// TestStoreRebuildFromWAL simulates the startup sequence: the store is empty,
// then PENDING WAL entries are replayed into it via Register, and subsequent
// Seen calls return the correct payload_id (duplicate suppression).
func TestStoreRebuildFromWAL(t *testing.T) {
	// Simulate WAL entries from replay.
	type walEntry struct {
		IdempotencyKey string
		PayloadID      string
	}
	pendingEntries := []walEntry{
		{"client-req-1", "wal-payload-1"},
		{"client-req-2", "wal-payload-2"},
		{"client-req-3", "wal-payload-3"},
	}

	s := New()
	// Rebuild from WAL replay.
	for _, e := range pendingEntries {
		s.Register(e.IdempotencyKey, e.PayloadID)
	}

	// A new request with a key already in the store must be suppressed.
	id, ok := s.Seen("client-req-2")
	if !ok {
		t.Fatal("Seen: expected duplicate to be detected after WAL rebuild")
	}
	if id != "wal-payload-2" {
		t.Errorf("Seen: want wal-payload-2, got %s", id)
	}

	// A new request with an unseen key must not be suppressed.
	_, ok = s.Seen("brand-new-request")
	if ok {
		t.Error("Seen: expected false for new key not in WAL")
	}
}

// TestStoreConcurrent runs concurrent Register and Seen calls and verifies
// there are no data races (requires go test -race).
func TestStoreConcurrent(t *testing.T) {
	s := New()
	const goroutines = 50
	const keysPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < keysPerGoroutine; i++ {
				key := fmt.Sprintf("g%d-k%d", g, i)
				id := fmt.Sprintf("g%d-p%d", g, i)
				s.Register(key, id)
				s.Seen(key)
				s.PayloadID(id)
			}
		}()
	}
	wg.Wait()
}
