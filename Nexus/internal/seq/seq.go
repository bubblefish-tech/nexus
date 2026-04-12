// Copyright © 2026 BubbleFish Technologies, Inc.

// Package seq provides a global monotonic sequence counter for ordering
// WAL entries and audit log entries independently of wall-clock time.
//
// The counter is an atomic int64 incremented on every WAL append and
// every audit entry write. It protects against NTP corrections, VM
// migrations, and clock skew from breaking ordering invariants.
//
// Persistence: the counter is saved to $BUBBLEFISH_HOME/seq.state on
// shutdown and restored on start as max(persisted, highest_seq_in_wal) + 1.
// If the state file is missing or corrupt, the counter initializes from
// the highest WAL sequence + 1.
package seq

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// Counter is a monotonic sequence counter. It is safe for concurrent use.
// All state is held in struct fields; there are no package-level variables.
type Counter struct {
	val      atomic.Int64
	stateDir string // directory for seq.state persistence
}

const stateFile = "seq.state"

// New creates a Counter that persists to the given directory. The directory
// must already exist. Call Restore() before first use to initialize from
// persisted state.
func New(stateDir string) *Counter {
	return &Counter{stateDir: stateDir}
}

// Next returns the next monotonic sequence value. Values are guaranteed
// to be unique and strictly increasing across concurrent callers.
// Never returns 0 — the first value is 1.
func (c *Counter) Next() int64 {
	return c.val.Add(1)
}

// Current returns the current counter value without incrementing.
func (c *Counter) Current() int64 {
	return c.val.Load()
}

// Restore initializes the counter from persisted state and/or the highest
// known WAL sequence. The counter resumes from max(persisted, highestWALSeq) + 1.
// If the state file is missing or corrupt, persisted is treated as 0.
//
// This must be called before any calls to Next(). It is not safe to call
// concurrently with Next().
func (c *Counter) Restore(highestWALSeq int64) {
	persisted := c.loadState()
	start := persisted
	if highestWALSeq > start {
		start = highestWALSeq
	}
	c.val.Store(start)
}

// Persist writes the current counter value to disk. Call during graceful
// shutdown. The file format is a single line containing the decimal value.
func (c *Counter) Persist() error {
	path := filepath.Join(c.stateDir, stateFile)
	content := fmt.Sprintf("%d\n", c.val.Load())
	return os.WriteFile(path, []byte(content), 0600)
}

// loadState reads the persisted counter value from disk. Returns 0 if the
// file is missing, empty, or corrupt.
func (c *Counter) loadState() int64 {
	path := filepath.Join(c.stateDir, stateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}
