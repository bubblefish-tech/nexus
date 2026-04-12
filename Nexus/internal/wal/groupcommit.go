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

package wal

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// GroupCommitConfig controls group commit behaviour. When Enabled is true,
// Append sends entries to a single consumer goroutine that batches writes
// and performs one fsync per batch. Per-request durability is preserved:
// the HTTP handler blocks until the fsync completes.
type GroupCommitConfig struct {
	Enabled  bool
	MaxBatch int           // max entries per batch before flush (default 256)
	MaxDelay time.Duration // max time to wait for a full batch (default 500µs)
}

// pendingEntry is a single WAL entry waiting to be group-committed. The
// submitter blocks on done until the batch is flushed to disk.
type pendingEntry struct {
	line string // fully formatted WAL line (JSON\tCRC[TAB HMAC]\n)
	done chan error
}

// groupCommitter is the single-goroutine consumer that batches WAL writes.
// It buffers entries until either MaxBatch entries arrive or MaxDelay elapses
// since the first entry in the batch, whichever comes first. On flush it
// writes all buffered entries sequentially, calls fsync once, then signals
// every waiter.
type groupCommitter struct {
	submitCh chan *pendingEntry
	stopCh   chan struct{}
	stopped  chan struct{}
	once     sync.Once
	logger   *slog.Logger

	maxBatch int
	maxDelay time.Duration
}

func newGroupCommitter(cfg GroupCommitConfig, logger *slog.Logger) *groupCommitter {
	maxBatch := cfg.MaxBatch
	if maxBatch <= 0 {
		maxBatch = 256
	}
	maxDelay := cfg.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 500 * time.Microsecond
	}
	return &groupCommitter{
		submitCh: make(chan *pendingEntry, maxBatch),
		stopCh:   make(chan struct{}),
		stopped:  make(chan struct{}),
		logger:   logger,
		maxBatch: maxBatch,
		maxDelay: maxDelay,
	}
}

// submit sends an entry to the group commit goroutine and blocks until the
// batch containing this entry is flushed and fsynced. Returns nil on success
// or the fsync/write error on failure.
func (gc *groupCommitter) submit(line string) error {
	pe := &pendingEntry{
		line: line,
		done: make(chan error, 1),
	}
	select {
	case gc.submitCh <- pe:
	case <-gc.stopCh:
		return fmt.Errorf("wal: group committer stopped")
	}
	return <-pe.done
}

// run is the consumer loop. It must be called in a dedicated goroutine.
// The WAL passes its write function so the groupCommitter does not need
// direct access to the file handle or mutex.
func (gc *groupCommitter) run(writeBatch func(batch []*pendingEntry)) {
	defer close(gc.stopped)

	batch := make([]*pendingEntry, 0, gc.maxBatch)
	var timer *time.Timer
	var timerC <-chan time.Time

	flush := func() {
		if len(batch) == 0 {
			return
		}
		writeBatch(batch)
		batch = batch[:0]
		if timer != nil {
			timer.Stop()
			timerC = nil
		}
	}

	for {
		select {
		case pe := <-gc.submitCh:
			batch = append(batch, pe)
			if len(batch) == 1 {
				// First entry in a new batch — start the deadline timer.
				if timer == nil {
					timer = time.NewTimer(gc.maxDelay)
				} else {
					timer.Reset(gc.maxDelay)
				}
				timerC = timer.C
			}
			if len(batch) >= gc.maxBatch {
				flush()
			}

		case <-timerC:
			flush()

		case <-gc.stopCh:
			// Drain any remaining entries in the channel.
			for {
				select {
				case pe := <-gc.submitCh:
					batch = append(batch, pe)
				default:
					flush()
					return
				}
			}
		}
	}
}

// stop signals the consumer goroutine to flush remaining entries and exit.
// Blocks until the goroutine has exited. Safe to call multiple times.
func (gc *groupCommitter) stop() {
	gc.once.Do(func() {
		close(gc.stopCh)
	})
	<-gc.stopped
}
