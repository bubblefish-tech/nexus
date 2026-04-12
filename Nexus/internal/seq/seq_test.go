// Copyright © 2026 BubbleFish Technologies, Inc.

package seq

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

// TestCounter_Monotonicity verifies that 100 goroutines each calling Next()
// 10000 times produce exactly 1,000,000 unique, gapless values.
func TestCounter_Monotonicity(t *testing.T) {
	dir := t.TempDir()
	c := New(dir)
	c.Restore(0)

	const goroutines = 100
	const callsPerGoroutine = 10000
	total := goroutines * callsPerGoroutine

	results := make([]int64, total)
	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				results[offset+i] = c.Next()
			}
		}(g * callsPerGoroutine)
	}
	wg.Wait()

	// Sort and verify: values must be exactly {1, 2, ..., total}.
	sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })

	for i, v := range results {
		expected := int64(i + 1)
		if v != expected {
			t.Fatalf("gap at index %d: want %d, got %d", i, expected, v)
		}
	}
}

// TestCounter_PersistRestore verifies the counter persists to disk and
// restores correctly, continuing from the last-used value.
func TestCounter_PersistRestore(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: create counter, use it, persist.
	c1 := New(dir)
	c1.Restore(0)
	for i := 0; i < 100; i++ {
		c1.Next()
	}
	if c1.Current() != 100 {
		t.Fatalf("after 100 calls: want 100, got %d", c1.Current())
	}
	if err := c1.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	// Phase 2: new counter, restore, verify it continues from 100.
	c2 := New(dir)
	c2.Restore(0) // highestWALSeq=0, so persisted value (100) wins
	if c2.Current() != 100 {
		t.Fatalf("after Restore: want 100, got %d", c2.Current())
	}

	// Next value should be 101.
	v := c2.Next()
	if v != 101 {
		t.Fatalf("first Next after restore: want 101, got %d", v)
	}
}

// TestCounter_RestoreFromWALSeq verifies that when the highest WAL sequence
// exceeds the persisted value, the counter uses the WAL sequence.
func TestCounter_RestoreFromWALSeq(t *testing.T) {
	dir := t.TempDir()

	// Persist a low value.
	c1 := New(dir)
	c1.Restore(0)
	for i := 0; i < 50; i++ {
		c1.Next()
	}
	if err := c1.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	// Restore with a higher WAL sequence.
	c2 := New(dir)
	c2.Restore(200) // WAL seq 200 > persisted 50
	if c2.Current() != 200 {
		t.Fatalf("after Restore with WAL seq 200: want 200, got %d", c2.Current())
	}

	v := c2.Next()
	if v != 201 {
		t.Fatalf("first Next: want 201, got %d", v)
	}
}

// TestCounter_MissingStateFile verifies that a missing state file results
// in initialization from the highest WAL sequence.
func TestCounter_MissingStateFile(t *testing.T) {
	dir := t.TempDir()
	// No state file exists.

	c := New(dir)
	c.Restore(42)
	if c.Current() != 42 {
		t.Fatalf("want 42 (from WAL), got %d", c.Current())
	}
}

// TestCounter_CorruptStateFile verifies that a corrupt state file is
// treated as missing (falls back to WAL sequence).
func TestCounter_CorruptStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, stateFile)
	if err := os.WriteFile(path, []byte("not-a-number\n"), 0600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	c := New(dir)
	c.Restore(99)
	if c.Current() != 99 {
		t.Fatalf("want 99 (from WAL, corrupt state ignored), got %d", c.Current())
	}
}

// TestCounter_NeverReuses verifies that across two "restarts" with
// persistence, no value is ever reused.
func TestCounter_NeverReuses(t *testing.T) {
	dir := t.TempDir()
	seen := make(map[int64]bool)

	for restart := 0; restart < 5; restart++ {
		c := New(dir)
		c.Restore(0)
		for i := 0; i < 20; i++ {
			v := c.Next()
			if seen[v] {
				t.Fatalf("restart %d: value %d reused", restart, v)
			}
			seen[v] = true
		}
		if err := c.Persist(); err != nil {
			t.Fatalf("restart %d: Persist: %v", restart, err)
		}
	}

	if len(seen) != 100 {
		t.Fatalf("want 100 unique values, got %d", len(seen))
	}
}
