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

package destination_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// newTestPostgres opens a PostgresDestination using TEST_POSTGRES_DSN.
// The test is skipped when the env var is not set.
func newTestPostgres(t *testing.T) (*destination.PostgresDestination, func()) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set; skipping postgres integration test")
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	d, err := destination.OpenPostgres(dsn, 3, logger)
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	return d, func() { _ = d.Close() }
}

// TestPostgresDestination_InterfaceCompliance is a compile-time guard that
// *PostgresDestination implements Destination.
func TestPostgresDestination_InterfaceCompliance(t *testing.T) {
	t.Helper()
	var _ destination.Destination = (*destination.PostgresDestination)(nil)
}

// TestPostgresDestination_Name verifies the stable backend identifier.
func TestPostgresDestination_Name(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()
	if got := d.Name(); got != "postgres" {
		t.Errorf("Name() = %q, want %q", got, "postgres")
	}
}

// TestPostgresDestination_Read_Found verifies that a written record is retrievable.
func TestPostgresDestination_Read_Found(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()

	p := basePayload("pg-read-found-01")
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	defer func() { _ = d.Delete(context.Background(), p.PayloadID) }()

	got, err := d.Read(context.Background(), p.PayloadID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got == nil {
		t.Fatal("Read returned nil, want record")
	}
	if got.PayloadID != p.PayloadID {
		t.Errorf("PayloadID = %q, want %q", got.PayloadID, p.PayloadID)
	}
	if got.Content != p.Content {
		t.Errorf("Content = %q, want %q", got.Content, p.Content)
	}
}

// TestPostgresDestination_Read_NotFound verifies nil, nil for a missing ID.
func TestPostgresDestination_Read_NotFound(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()

	got, err := d.Read(context.Background(), "pg-no-such-id")
	if err != nil {
		t.Fatalf("Read unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("Read = %v, want nil for missing ID", got)
	}
}

// TestPostgresDestination_Search returns matching records.
func TestPostgresDestination_Search(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()

	p1 := basePayload("pg-search-01")
	p1.Namespace = "pg-ns-a"
	p2 := basePayload("pg-search-02")
	p2.Namespace = "pg-ns-b"
	for _, p := range []destination.TranslatedPayload{p1, p2} {
		if err := d.Write(p); err != nil {
			t.Fatalf("Write: %v", err)
		}
		id := p.PayloadID
		defer func() { _ = d.Delete(context.Background(), id) }()
	}

	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "pg-ns-a"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search len = %d, want 1", len(results))
	}
	if results[0].PayloadID != p1.PayloadID {
		t.Errorf("Search PayloadID = %q, want %q", results[0].PayloadID, p1.PayloadID)
	}
}

// TestPostgresDestination_Search_Empty verifies a non-nil empty slice for no matches.
func TestPostgresDestination_Search_Empty(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()

	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "pg-no-such-ns"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results == nil {
		t.Error("Search returned nil slice, want empty non-nil slice")
	}
	if len(results) != 0 {
		t.Errorf("Search len = %d, want 0", len(results))
	}
}

// TestPostgresDestination_Delete_Exists removes a record and verifies it is gone.
func TestPostgresDestination_Delete_Exists(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()

	p := basePayload("pg-delete-exists-01")
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := d.Delete(context.Background(), p.PayloadID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err := d.Read(context.Background(), p.PayloadID)
	if err != nil {
		t.Fatalf("Read after Delete: %v", err)
	}
	if got != nil {
		t.Error("Read after Delete returned record, want nil")
	}
}

// TestPostgresDestination_Delete_NotExists is a no-op (idempotent).
func TestPostgresDestination_Delete_NotExists(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()

	if err := d.Delete(context.Background(), "pg-no-such-id"); err != nil {
		t.Errorf("Delete of missing ID should be no-op, got: %v", err)
	}
}

// TestPostgresDestination_VectorSearch_EmptyEmbedding returns empty for zero-length query.
// This test does not require a running database.
func TestPostgresDestination_VectorSearch_EmptyEmbedding(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()

	results, err := d.VectorSearch(context.Background(), nil, 5)
	if err != nil {
		t.Fatalf("VectorSearch(nil): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("VectorSearch(nil) len = %d, want 0", len(results))
	}
}

// TestPostgresDestination_Migrate is a no-op that must return nil.
func TestPostgresDestination_Migrate(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()

	if err := d.Migrate(context.Background(), 1); err != nil {
		t.Errorf("Migrate: %v", err)
	}
}

// TestPostgresDestination_Health_OK verifies Health returns OK=true on an open DB.
func TestPostgresDestination_Health_OK(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	defer cleanup()

	h, err := d.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h == nil {
		t.Fatal("Health returned nil HealthStatus")
	}
	if !h.OK {
		t.Errorf("Health.OK = false, want true; error: %s", h.Error)
	}
	if h.Latency < 0 {
		t.Errorf("Health.Latency = %v, want >= 0", h.Latency)
	}
}

// TestPostgresDestination_Health_ClosedDB verifies Health returns OK=false after Close.
func TestPostgresDestination_Health_ClosedDB(t *testing.T) {
	t.Helper()
	d, cleanup := newTestPostgres(t)
	cleanup() // close the DB before calling Health

	h, err := d.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h == nil {
		t.Fatal("Health returned nil HealthStatus")
	}
	if h.OK {
		t.Error("Health.OK = true on closed DB, want false")
	}
	if h.Error == "" {
		t.Error("Health.Error is empty on closed DB, want non-empty")
	}
}
