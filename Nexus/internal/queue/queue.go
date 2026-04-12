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

// Package queue implements the bounded in-memory message queue for BubbleFish
// Nexus. The queue sits between the WAL (durable store) and the destination
// adapter (SQLite, Postgres, etc.).
//
// Invariants:
//   - Enqueue is non-blocking. A full channel returns ErrLoadShed immediately.
//     The WAL entry is still durable; the caller returns HTTP 429.
//   - Drain / DrainWithContext wrap close(done) in sync.Once. Calling either
//     method multiple times never panics and never closes the channel twice.
//   - Worker goroutines log at WARN on transient destination errors and retry
//     with exponential backoff. After maxDeliveryAttempts, the entry is marked
//     PERMANENT_FAILURE in the WAL and dropped.
//   - A nil logger at construction time panics immediately with a clear message.
package queue

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/wal"
)

const (
	// defaultSize is the number of WAL entries the channel can buffer before
	// Enqueue returns ErrLoadShed. Overridden by Config.Size.
	defaultSize = 10_000

	// defaultWorkers is the number of goroutines consuming entries from the
	// channel. Overridden by Config.Workers.
	defaultWorkers = 1

	// maxDeliveryAttempts is the total number of Write attempts before an
	// entry is classified as PERMANENT and removed from the retry loop.
	maxDeliveryAttempts = 5

	// initialBackoff is the first sleep duration between retry attempts.
	// Subsequent attempts double the backoff (1s, 2s, 4s, 8s, 16s).
	initialBackoff = time.Second

	// batchFlushInterval is how often the worker flushes accumulated
	// delivered payload IDs to the WAL via MarkDeliveredBatch.
	batchFlushInterval = 100 * time.Millisecond

	// batchFlushSize is the max number of delivered IDs to accumulate
	// before flushing, even if the timer hasn't fired yet.
	batchFlushSize = 50
)

// ErrLoadShed is returned by Enqueue when the channel is full. The WAL entry
// remains durable; the HTTP handler translates this to a 429 queue_full with
// Retry-After.
var ErrLoadShed = errors.New("queue: channel full, load shed")

// Config controls queue sizing and worker count.
type Config struct {
	// Size is the channel buffer length. Defaults to defaultSize (10 000).
	Size int

	// Workers is the number of goroutines draining the channel. Defaults to
	// defaultWorkers (1).
	Workers int

	// OnProcessed is an optional callback invoked after each entry is
	// successfully written to the destination. Used to increment the
	// bubblefish_queue_processing_rate metric. Must be safe to call
	// concurrently. If nil, no callback is made.
	OnProcessed func()

	// OnDelivered is an optional callback invoked after each entry is
	// successfully written to the destination. Receives the destination name
	// so the caller can invalidate caches for the affected destination.
	// Must be safe to call concurrently. If nil, no callback is made.
	OnDelivered func(destination string)

	// BeatFn is an optional heartbeat callback called each iteration of
	// the worker loop. Used by the supervisor to detect stalled workers.
	BeatFn func()
}

// Queue is a bounded, concurrency-safe message queue. All state is held in
// struct fields; there are no package-level variables.
type Queue struct {
	ch          chan wal.Entry
	done        chan struct{}
	once        sync.Once
	wg          sync.WaitGroup
	logger      *slog.Logger
	dest        destination.DestinationWriter
	updater     wal.WALUpdater
	onProcessed func()             // optional; called after each successful write
	onDelivered func(dest string)  // optional; called with destination name after successful write
	beatFn      func()             // optional; supervisor heartbeat
}

// New creates a Queue with the given configuration and starts the worker
// goroutines. Both logger and dest must be non-nil. Panics if logger is nil.
//
// Callers MUST call Drain or DrainWithContext before process exit to allow
// in-flight entries to be written to the destination.
func New(cfg Config, logger *slog.Logger, dest destination.DestinationWriter, updater wal.WALUpdater) *Queue {
	if logger == nil {
		panic("queue: logger must not be nil")
	}
	if dest == nil {
		panic("queue: destination must not be nil")
	}
	if updater == nil {
		panic("queue: WAL updater must not be nil")
	}

	size := cfg.Size
	if size <= 0 {
		size = defaultSize
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}

	q := &Queue{
		ch:          make(chan wal.Entry, size),
		done:        make(chan struct{}),
		logger:      logger,
		dest:        dest,
		updater:     updater,
		onProcessed: cfg.OnProcessed,
		onDelivered: cfg.OnDelivered,
		beatFn:      cfg.BeatFn,
	}

	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

// Len returns the current number of entries buffered in the queue channel.
// The value is approximate — it may change between the call and any subsequent
// use. Safe to call concurrently.
func (q *Queue) Len() int {
	return len(q.ch)
}

// Enqueue adds entry to the queue channel. Returns ErrLoadShed immediately if
// the channel is full — it never blocks. This is the non-blocking select
// pattern required by the spec (Tech Spec Section 5):
//
//	select { case q.ch <- entry: return nil; default: return ErrLoadShed }
func (q *Queue) Enqueue(entry wal.Entry) error {
	select {
	case q.ch <- entry:
		return nil
	default:
		return ErrLoadShed
	}
}

// Drain signals all workers to stop and waits for them to finish. It is safe
// to call multiple times; only the first call closes the done channel. Workers
// finish in-flight retries and drain remaining buffered entries before exiting.
func (q *Queue) Drain() {
	q.once.Do(func() {
		close(q.done)
	})
	q.wg.Wait()
}

// DrainWithContext signals all workers to stop and waits up to ctx's deadline
// for them to finish. Returns true if all workers finished within the deadline,
// false if ctx expired first.
//
// If ctx expires, the workers are still running and will exit once their
// current item finishes (because done was already closed). Callers may call
// Drain() afterward for a blocking wait if needed.
//
// Goroutine lifecycle note: the internal goroutine spawned here calls
// q.wg.Wait(), which will return once all workers exit. Because done is always
// closed before q.wg.Wait() is called, the goroutine is guaranteed to
// complete, so no goroutine leak occurs.
func (q *Queue) DrainWithContext(ctx context.Context) bool {
	q.once.Do(func() {
		close(q.done)
	})

	finished := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		return true
	case <-ctx.Done():
		return false
	}
}

// worker is the goroutine that reads WAL entries from the channel, writes them
// to the destination, and batches MarkDelivered calls for efficiency. Each
// worker exits when the done channel is closed AND the channel buffer is empty.
func (q *Queue) worker() {
	defer q.wg.Done()

	var pending []string
	ticker := time.NewTicker(batchFlushInterval)
	defer ticker.Stop()

	flush := func() {
		if len(pending) == 0 {
			return
		}
		batch := pending
		pending = nil
		if err := q.updater.MarkDeliveredBatch(batch); err != nil {
			q.logger.Warn("queue: MarkDeliveredBatch failed (data safe in destination)",
				"component", "queue",
				"batch_size", len(batch),
				"error", err,
			)
		}
	}

	for {
		if q.beatFn != nil {
			q.beatFn()
		}

		select {
		case entry, ok := <-q.ch:
			if !ok {
				flush()
				return
			}
			if id := q.processEntry(entry); id != "" {
				pending = append(pending, id)
				if len(pending) >= batchFlushSize {
					flush()
				}
			}

		case <-ticker.C:
			flush()

		case <-q.done:
			// Drain any entries already buffered in the channel before exiting.
			for {
				select {
				case entry, ok := <-q.ch:
					if !ok {
						flush()
						return
					}
					if id := q.processEntry(entry); id != "" {
						pending = append(pending, id)
						if len(pending) >= batchFlushSize {
							flush()
						}
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// processEntry deserializes the WAL entry's Payload, attempts to write it to
// the destination with exponential backoff. Returns the payload ID on success
// (for batch marking) or empty string on failure.
//
// Failure classification:
//   - JSON unmarshal error → PERMANENT (not retryable; data is malformed)
//   - destination.Write error → TRANSIENT for up to maxDeliveryAttempts,
//     then PERMANENT
func (q *Queue) processEntry(entry wal.Entry) string {
	var tp destination.TranslatedPayload
	if err := json.Unmarshal(entry.Payload, &tp); err != nil {
		q.logger.Error("queue: unmarshal payload — marking PERMANENT_FAILURE",
			"component", "queue",
			"payload_id", entry.PayloadID,
			"error", err,
		)
		q.markPermanent(entry.PayloadID)
		return ""
	}

	backoff := initialBackoff
	for attempt := 1; attempt <= maxDeliveryAttempts; attempt++ {
		writeErr := q.dest.Write(tp)
		if writeErr == nil {
			// Notify metrics observer (e.g. bubblefish_queue_processing_rate).
			if q.onProcessed != nil {
				q.onProcessed()
			}
			// Notify cache invalidator with the destination name.
			if q.onDelivered != nil {
				q.onDelivered(entry.Destination)
			}
			return entry.PayloadID
		}

		if attempt < maxDeliveryAttempts {
			q.logger.Warn("queue: destination write failed — will retry",
				"component", "queue",
				"payload_id", entry.PayloadID,
				"attempt", attempt,
				"max_attempts", maxDeliveryAttempts,
				"backoff", backoff,
				"error", writeErr,
			)
			// Context-aware backoff sleep: exits immediately if the queue is
			// being drained, preventing the worker from blocking shutdown.
			select {
			case <-time.After(backoff):
			case <-q.done:
				// Queue is draining. Stop retrying so the worker can exit.
				q.logger.Warn("queue: drain signaled during retry backoff — abandoning entry",
					"component", "queue",
					"payload_id", entry.PayloadID,
				)
				return ""
			}
			backoff *= 2
		} else {
			q.logger.Error("queue: destination write failed after all attempts — marking PERMANENT_FAILURE",
				"component", "queue",
				"payload_id", entry.PayloadID,
				"attempts", maxDeliveryAttempts,
				"error", writeErr,
			)
			q.markPermanent(entry.PayloadID)
		}
	}
	return ""
}

// markPermanent updates the WAL entry for payloadID to PERMANENT_FAILURE.
// A failure to update the WAL is logged at ERROR level but does not panic —
// the entry will be retried on next replay, and the destination's idempotency
// will prevent a duplicate write.
func (q *Queue) markPermanent(payloadID string) {
	if err := q.updater.MarkPermanentFailure(payloadID); err != nil {
		q.logger.Error("queue: MarkPermanentFailure failed",
			"component", "queue",
			"payload_id", payloadID,
			"error", err,
		)
	}
}
