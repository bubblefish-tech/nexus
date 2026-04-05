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
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/idempotency"
	"github.com/BubbleFish-Nexus/internal/queue"
	"github.com/BubbleFish-Nexus/internal/wal"
)

// fakeDestination is a no-op DestinationWriter + Querier used in tests.
// It satisfies both interfaces so the daemon can boot without real storage.
type fakeDestination struct{}

func (f *fakeDestination) Write(p destination.TranslatedPayload) error { return nil }
func (f *fakeDestination) Ping() error                                  { return nil }
func (f *fakeDestination) Exists(id string) (bool, error)              { return false, nil }
func (f *fakeDestination) Close() error                                 { return nil }
func (f *fakeDestination) Query(params destination.QueryParams) (destination.QueryResult, error) {
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

// BuildRouter exposes buildRouter for tests that need the full server router
// (e.g. health/ready probes that bypass auth).
func (d *Daemon) BuildRouter() http.Handler {
	return d.buildRouter()
}
