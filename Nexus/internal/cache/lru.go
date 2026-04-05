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

// Package cache implements the Stage 1 exact cache for the BubbleFish Nexus
// 6-stage retrieval cascade. It provides a zero-dependency generic LRU bounded
// by total byte capacity, scope-isolated cache entries, monotonic watermark
// invalidation, and Prometheus hit/miss counters.
//
// Reference: Tech Spec Section 3.4 — Stage 1.
package cache

import (
	"container/list"
	"sync"
)

// LRU is a generic, thread-safe least-recently-used cache bounded by total
// byte capacity. Keys must be comparable. Values can be any type.
//
// All exported methods are safe for concurrent use by multiple goroutines.
// There are no package-level variables — all state lives in struct fields.
//
// Implementation: doubly-linked list (container/list) + map[K]*list.Element.
// Add is O(1) amortised. Get is O(1). Eviction is O(1) per evicted entry.
//
// Reference: Tech Spec Section 3.4 (zero-dep LRU, Go generics).
// CRITICAL: Do NOT replace with hashicorp/golang-lru (MPL 2.0 licence).
type LRU[K comparable, V any] struct {
	mu        sync.Mutex
	maxBytes  int64
	usedBytes int64
	items     map[K]*list.Element
	evict     *list.List
}

// lruEntry is the value stored in each list.Element.
type lruEntry[K comparable, V any] struct {
	key   K
	value V
	bytes int64
}

// NewLRU creates an LRU bounded to maxBytes of total capacity. maxBytes must
// be positive; callers that pass ≤0 will get an always-evicting cache.
func NewLRU[K comparable, V any](maxBytes int64) *LRU[K, V] {
	return &LRU[K, V]{
		maxBytes: maxBytes,
		items:    make(map[K]*list.Element),
		evict:    list.New(),
	}
}

// Add inserts or updates key with value. bytes is the caller's estimate of the
// entry's memory footprint; it must be ≥1. If the cache is full after the
// insert the least-recently-used entries are evicted until capacity is
// satisfied. Updating an existing key moves it to the front (most recent).
func (l *LRU[K, V]) Add(key K, value V, bytes int64) {
	if bytes < 1 {
		bytes = 1
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.items[key]; ok {
		// Update existing entry in-place — no new list element needed.
		e := elem.Value.(*lruEntry[K, V])
		l.usedBytes += bytes - e.bytes
		e.value = value
		e.bytes = bytes
		l.evict.MoveToFront(elem)
	} else {
		e := &lruEntry[K, V]{key: key, value: value, bytes: bytes}
		elem := l.evict.PushFront(e)
		l.items[key] = elem
		l.usedBytes += bytes
	}

	// Evict least-recently-used entries until within capacity. The loop stops
	// when only one entry remains so we never evict the item we just inserted
	// (even if that single entry exceeds maxBytes).
	for l.usedBytes > l.maxBytes && l.evict.Len() > 1 {
		l.removeBack()
	}
}

// Get retrieves the value for key and promotes it to most-recently-used.
// Returns the zero value of V and false when the key is absent.
func (l *LRU[K, V]) Get(key K) (V, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if elem, ok := l.items[key]; ok {
		l.evict.MoveToFront(elem)
		return elem.Value.(*lruEntry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Remove deletes key from the cache. No-op if key is absent.
func (l *LRU[K, V]) Remove(key K) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.removeKey(key)
}

// Len returns the number of entries currently in the cache.
func (l *LRU[K, V]) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.items)
}

// BytesUsed returns the current total byte usage across all entries.
func (l *LRU[K, V]) BytesUsed() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.usedBytes
}

// removeBack removes the least-recently-used (back) entry. Caller must hold mu.
func (l *LRU[K, V]) removeBack() {
	back := l.evict.Back()
	if back == nil {
		return
	}
	e := back.Value.(*lruEntry[K, V])
	l.evict.Remove(back)
	delete(l.items, e.key)
	l.usedBytes -= e.bytes
}

// removeKey removes a specific key. Caller must hold mu.
func (l *LRU[K, V]) removeKey(key K) {
	if elem, ok := l.items[key]; ok {
		e := elem.Value.(*lruEntry[K, V])
		l.evict.Remove(elem)
		delete(l.items, key)
		l.usedBytes -= e.bytes
	}
}
