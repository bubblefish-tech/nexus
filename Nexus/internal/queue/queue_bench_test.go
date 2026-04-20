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

package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/wal"
)

// bench helpers — defined here because queue_test.go uses package queue_test
// and its helpers are not visible from package queue.

type benchDest struct{}

func (d *benchDest) Write(_ destination.TranslatedPayload) error { return nil }
func (d *benchDest) Ping() error                                 { return nil }
func (d *benchDest) Exists(_ string) (bool, error)               { return false, nil }
func (d *benchDest) Close() error                                { return nil }
func (d *benchDest) Name() string { return "bench" }
func (d *benchDest) Read(_ context.Context, _ string) (*destination.Memory, error) {
	return nil, nil
}
func (d *benchDest) Search(_ context.Context, _ *destination.Query) ([]*destination.Memory, error) {
	return nil, nil
}
func (d *benchDest) Delete(_ context.Context, _ string) error { return nil }
func (d *benchDest) VectorSearch(_ context.Context, _ []float32, _ int) ([]*destination.Memory, error) {
	return nil, nil
}
func (d *benchDest) Migrate(_ context.Context, _ int) error { return nil }
func (d *benchDest) Health(_ context.Context) (*destination.HealthStatus, error) {
	return &destination.HealthStatus{OK: true}, nil
}

type benchUpdater struct{}

func (u *benchUpdater) MarkDelivered(_ string) error        { return nil }
func (u *benchUpdater) MarkDeliveredBatch(_ []string) error { return nil }
func (u *benchUpdater) MarkPermanentFailure(_ string) error { return nil }

func benchLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func benchWALEntry(id string) wal.Entry {
	tp := destination.TranslatedPayload{
		PayloadID: id,
		Content:   "benchmark content for " + id,
		Timestamp: time.Now().UTC(),
	}
	raw, _ := json.Marshal(tp)
	return wal.Entry{
		PayloadID:      id,
		IdempotencyKey: "idem-" + id,
		Status:         wal.StatusPending,
		Timestamp:      time.Now().UTC(),
		Source:         "bench",
		Destination:    "sqlite",
		Subject:        "user:bench",
		Payload:        raw,
	}
}

// BenchmarkQueue_Enqueue_Single measures the cost of enqueueing one item into a
// running queue with a no-op destination.
func BenchmarkQueue_Enqueue_Single(b *testing.B) {
	b.ReportAllocs()
	q := New(Config{Size: b.N + 1000, Workers: 2}, benchLogger(), &benchDest{}, &benchUpdater{})
	b.Cleanup(func() { q.Drain() })

	entry := benchWALEntry("enq-0")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.PayloadID = fmt.Sprintf("enq-%d", i)
		if err := q.Enqueue(entry); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
	}
}

// BenchmarkQueue_Dequeue_Single measures the throughput of the queue's internal
// worker processing path by enqueueing items and draining. Since Dequeue is not
// a public API (workers consume internally), this measures the full
// enqueue-to-delivery cycle per item.
func BenchmarkQueue_Dequeue_Single(b *testing.B) {
	b.ReportAllocs()

	// Use a blocking destination to fill the queue, then swap to a fast one.
	// Actually, just measure enqueue + drain for N items.
	q := New(Config{Size: b.N + 1000, Workers: 4}, benchLogger(), &benchDest{}, &benchUpdater{})

	entry := benchWALEntry("deq-0")
	for i := 0; i < b.N; i++ {
		entry.PayloadID = fmt.Sprintf("deq-%d", i)
		entry.IdempotencyKey = fmt.Sprintf("idem-deq-%d", i)
		if err := q.Enqueue(entry); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
	}

	b.ResetTimer()
	q.Drain()
}

// BenchmarkQueue_DrainToSQLite_100 measures the time to drain 100 items from
// the queue through the destination writer. The "SQLite" in the name refers to
// the typical production destination; here we use a no-op writer to isolate
// queue overhead from destination I/O.
func BenchmarkQueue_DrainToSQLite_100(b *testing.B) {
	b.ReportAllocs()

	entry := benchWALEntry("drain-0")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := New(Config{Size: 200, Workers: 4}, benchLogger(), &benchDest{}, &benchUpdater{})
		for j := 0; j < 100; j++ {
			entry.PayloadID = fmt.Sprintf("drain-%d-%d", i, j)
			entry.IdempotencyKey = fmt.Sprintf("idem-drain-%d-%d", i, j)
			if err := q.Enqueue(entry); err != nil {
				b.Fatalf("enqueue: %v", err)
			}
		}
		q.Drain()
	}
}
