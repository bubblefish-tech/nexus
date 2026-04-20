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

// This file is compiled only during `go test`. It exposes internal symbols
// needed by _test.go files in the daemon_test package.

package daemon

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/bubblefish-tech/nexus/internal/audit"
	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/idempotency"
	"github.com/bubblefish-tech/nexus/internal/queue"
	"github.com/bubblefish-tech/nexus/internal/wal"
)

// fakeDestination is a no-op Destination + Querier used in tests.
// It satisfies both interfaces so the daemon can boot without real storage.
type fakeDestination struct{}

func (f *fakeDestination) Name() string                                  { return "fake" }
func (f *fakeDestination) Write(_ destination.TranslatedPayload) error   { return nil }
func (f *fakeDestination) Ping() error                                   { return nil }
func (f *fakeDestination) Exists(_ string) (bool, error)                 { return false, nil }
func (f *fakeDestination) Close() error                                  { return nil }
func (f *fakeDestination) Read(_ context.Context, _ string) (*destination.Memory, error) {
	return nil, nil
}
func (f *fakeDestination) Search(_ context.Context, _ *destination.Query) ([]*destination.Memory, error) {
	return nil, nil
}
func (f *fakeDestination) Delete(_ context.Context, _ string) error { return nil }
func (f *fakeDestination) VectorSearch(_ context.Context, _ []float32, _ int) ([]*destination.Memory, error) {
	return nil, nil
}
func (f *fakeDestination) Migrate(_ context.Context, _ int) error { return nil }
func (f *fakeDestination) Health(_ context.Context) (*destination.HealthStatus, error) {
	return &destination.HealthStatus{OK: true}, nil
}
func (f *fakeDestination) Query(_ destination.QueryParams) (destination.QueryResult, error) {
	return destination.QueryResult{Records: []destination.TranslatedPayload{}}, nil
}

// NewTestDaemon constructs a *Daemon wired with a real WAL in a temp
// directory, a real idempotency store, a no-op fake destination, and a real
// queue. It does NOT start a listening HTTP server.
//
// Metrics are initialised by New() so handlers can safely call them in tests.
// The temp directory is cleaned up when the test finishes.
func NewTestDaemon(t testing.TB, cfg *config.Config) *Daemon {
	t.Helper()

	logger := slog.Default()

	walDir := t.TempDir()
	w, err := wal.Open(walDir, 50, logger)
	if err != nil {
		t.Fatalf("NewTestDaemon: open WAL: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Logf("NewTestDaemon: WAL close: %v", err)
		}
	})

	fakeDest := &fakeDestination{}

	idem := idempotency.New()

	d := New(cfg, logger) // metrics initialised inside New()

	q := queue.New(
		queue.Config{
			Size:        100,
			OnProcessed: d.metrics.QueueProcessingRate.Inc,
		},
		logger,
		fakeDest,
		w,
	)
	t.Cleanup(func() { q.Drain() })

	d.wal = w
	d.idem = idem
	d.dest = fakeDest
	d.querier = fakeDest
	d.queue = q
	d.walHealthy.Store(1) // Match Start() initialisation.

	return d
}

// NewTestDaemonWithSQLite constructs a *Daemon wired with a real WAL and a real
// SQLite destination in temp directories. Used by Phase 9 integration tests that
// need to verify end-to-end data persistence (load test, crash recovery).
//
// Returns the Daemon and the SQLite destination for direct verification queries.
func NewTestDaemonWithSQLite(t testing.TB, cfg *config.Config) (*Daemon, *destination.SQLiteDestination) {
	t.Helper()

	logger := slog.Default()

	walDir := t.TempDir()
	w, err := wal.Open(walDir, 50, logger)
	if err != nil {
		t.Fatalf("NewTestDaemonWithSQLite: open WAL: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Logf("NewTestDaemonWithSQLite: WAL close: %v", err)
		}
	})

	dbDir := t.TempDir()
	sqliteDest, err := destination.OpenSQLite(filepath.Join(dbDir, "test.db"), logger)
	if err != nil {
		t.Fatalf("NewTestDaemonWithSQLite: open SQLite: %v", err)
	}
	t.Cleanup(func() {
		if err := sqliteDest.Close(); err != nil {
			t.Logf("NewTestDaemonWithSQLite: SQLite close: %v", err)
		}
	})

	idem := idempotency.New()

	d := New(cfg, logger)

	q := queue.New(
		queue.Config{
			Size:        cfg.Daemon.QueueSize,
			OnProcessed: d.metrics.QueueProcessingRate.Inc,
		},
		logger,
		sqliteDest,
		w,
	)
	t.Cleanup(func() { q.Drain() })

	d.wal = w
	d.idem = idem
	d.dest = sqliteDest
	d.querier = sqliteDest
	d.queue = q
	d.walHealthy.Store(1) // Match Start() initialisation.

	return d, sqliteDest
}

// blockingDestination blocks on every Write call until the test ends.
// Used to force queue backpressure in overload tests.
type blockingDestination struct {
	fakeDestination
	block chan struct{}
}

func (b *blockingDestination) Write(p destination.TranslatedPayload) error {
	<-b.block // blocks forever until channel closed
	return nil
}

// NewTestDaemonBlocking constructs a *Daemon with a destination that blocks on
// every Write, causing the queue to fill immediately. Used by queue overload tests.
func NewTestDaemonBlocking(t testing.TB, cfg *config.Config) *Daemon {
	t.Helper()

	logger := slog.Default()

	walDir := t.TempDir()
	w, err := wal.Open(walDir, 50, logger)
	if err != nil {
		t.Fatalf("NewTestDaemonBlocking: open WAL: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Logf("NewTestDaemonBlocking: WAL close: %v", err)
		}
	})

	blockCh := make(chan struct{})
	bd := &blockingDestination{block: blockCh}

	idem := idempotency.New()

	d := New(cfg, logger)

	q := queue.New(
		queue.Config{
			Size:        cfg.Daemon.QueueSize,
			Workers:     1,
			OnProcessed: d.metrics.QueueProcessingRate.Inc,
		},
		logger,
		bd,
		w,
	)
	// LIFO cleanup: Drain must run after blockCh is closed so workers can unblock.
	// Register Drain first (runs last), then close(blockCh) (runs first).
	t.Cleanup(func() { q.Drain() })
	t.Cleanup(func() { close(blockCh) })

	d.wal = w
	d.idem = idem
	d.dest = bd
	d.querier = &fakeDestination{}
	d.queue = q
	d.walHealthy.Store(1) // Match Start() initialisation.

	return d
}

// RequireDataTokenHandler exposes the requireDataToken middleware for testing.
func (d *Daemon) RequireDataTokenHandler(next http.Handler) http.Handler {
	return d.requireDataToken(next)
}

// RequireAdminTokenHandler exposes the requireAdminToken middleware for testing.
func (d *Daemon) RequireAdminTokenHandler(next http.Handler) http.Handler {
	return d.requireAdminToken(next)
}

// WriteHandler returns an http.Handler that routes POST /inbound/{source}
// through the requireDataToken middleware and write handler, using a chi router
// so that chi.URLParam works correctly.
func (d *Daemon) WriteHandler() http.Handler {
	r := chi.NewRouter()
	r.Use(d.requireDataToken)
	r.Post("/inbound/{source}", d.handleWrite)
	return r
}

// QueryHandler returns an http.Handler that routes GET /query/{destination}
// through the requireDataToken middleware and query handler.
func (d *Daemon) QueryHandler() http.Handler {
	r := chi.NewRouter()
	r.Use(d.requireDataToken)
	r.Get("/query/{destination}", d.handleQuery)
	return r
}

// AdminListHandler returns an http.Handler that routes GET /admin/memories
// through the requireAdminToken middleware and admin list handler.
func (d *Daemon) AdminListHandler() http.Handler {
	r := chi.NewRouter()
	r.Use(d.requireAdminToken)
	r.Get("/admin/memories", d.handleAdminList)
	return r
}

// BuildRouter exposes buildRouter for tests that need the full server router
// (e.g. health/ready probes that bypass auth).
func (d *Daemon) BuildRouter() http.Handler {
	return d.buildRouter()
}

// RunWatchdogCheck exposes runWatchdogCheck for testing the WAL health watchdog.
func (d *Daemon) RunWatchdogCheck(walDir string) {
	d.runWatchdogCheck(walDir)
}

// WALHealthy returns the current WAL health state (1=healthy, 0=unhealthy).
func (d *Daemon) WALHealthy() int32 {
	return d.walHealthy.Load()
}

// SetWALHealthy sets the WAL health state for testing.
func (d *Daemon) SetWALHealthy(v int32) {
	d.walHealthy.Store(v)
}

// RunConsistencyCheck exposes runConsistencyCheck for testing the consistency
// assertions background checker.
func (d *Daemon) RunConsistencyCheck(sampleSize int) {
	d.runConsistencyCheck(sampleSize)
}

// ConsistencyScore returns the latest consistency score stored atomically.
// Returns -1.0 if not yet computed.
func (d *Daemon) ConsistencyScore() float64 {
	return math.Float64frombits(d.consistencyScore.Load())
}

// WALDeliveredCount returns the number of DELIVERED entries in the WAL.
// Used by tests to wait for the queue worker's batch flush to complete.
func (d *Daemon) WALDeliveredCount(sampleSize int) (int, error) {
	entries, err := d.wal.SampleDelivered(sampleSize)
	return len(entries), err
}

// SetAuditReader sets the audit reader for testing.
func (d *Daemon) SetAuditReader(r *audit.AuditReader) {
	d.auditReader = r
}

// SetAuditRateLimiter initialises the audit rate limiter for testing.
func (d *Daemon) SetAuditRateLimiter() {
	d.auditRateLimiter = newRateLimiter()
}

// NewTestRateLimiter creates a rateLimiter for testing.
func NewTestRateLimiter() *rateLimiter {
	return newRateLimiter()
}

// EffectiveRPM exposes effectiveRPM for testing.
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.4.
func EffectiveRPM(cfg *config.Config, src *config.Source) int {
	return effectiveRPM(cfg, src)
}

// EffectiveBPS exposes effectiveBPS for testing.
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.4.
func EffectiveBPS(cfg *config.Config, src *config.Source) int64 {
	return effectiveBPS(cfg, src)
}

// NewEmbeddingValidator exposes newEmbeddingValidator for testing.
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.5.
func NewEmbeddingValidator(dimensions, warmupCount int, sigmaThreshold float64) *embeddingValidator {
	return newEmbeddingValidator(dimensions, warmupCount, sigmaThreshold)
}

// RateLimiterWindowCount returns the number of keys in the rate limiter's windows map.
func RateLimiterWindowCount(rl *rateLimiter) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return len(rl.windows)
}

// RateLimiterAllow wraps Allow for testing.
func RateLimiterAllow(rl *rateLimiter, key string, rpm int) (bool, int) {
	return rl.Allow(key, rpm)
}
