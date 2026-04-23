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
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// TestSQLiteDestination_Name verifies the stable backend identifier.
func TestSQLiteDestination_Name(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()
	if got := d.Name(); got != "sqlite" {
		t.Errorf("Name() = %q, want %q", got, "sqlite")
	}
}

// TestSQLiteDestination_Read_Found verifies that a written record is retrievable by ID.
func TestSQLiteDestination_Read_Found(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	p := basePayload("read-found-01")
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

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

// TestSQLiteDestination_Read_NotFound verifies nil, nil for a missing ID.
func TestSQLiteDestination_Read_NotFound(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	got, err := d.Read(context.Background(), "no-such-id")
	if err != nil {
		t.Fatalf("Read unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("Read = %v, want nil for missing ID", got)
	}
}

// TestSQLiteDestination_Search returns matching records.
func TestSQLiteDestination_Search(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	p1 := basePayload("search-01")
	p1.Namespace = "ns-a"
	p2 := basePayload("search-02")
	p2.Namespace = "ns-b"
	for _, p := range []destination.TranslatedPayload{p1, p2} {
		if err := d.Write(p); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "ns-a"})
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

// TestSQLiteDestination_Search_Empty verifies a non-nil empty slice for no matches.
func TestSQLiteDestination_Search_Empty(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "no-such-ns"})
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

// TestSQLiteDestination_Delete_Exists removes a record and verifies it is gone.
func TestSQLiteDestination_Delete_Exists(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	p := basePayload("delete-exists-01")
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

// TestSQLiteDestination_Delete_NotExists is a no-op (idempotent).
func TestSQLiteDestination_Delete_NotExists(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	if err := d.Delete(context.Background(), "no-such-id"); err != nil {
		t.Errorf("Delete of missing ID should be no-op, got: %v", err)
	}
}

// TestSQLiteDestination_VectorSearch returns results when embeddings are stored.
func TestSQLiteDestination_VectorSearch(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	p := basePayload("vsearch-01")
	p.Embedding = []float32{1, 0, 0}
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	results, err := d.VectorSearch(context.Background(), []float32{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("VectorSearch returned no results, want at least 1")
	}
	if results[0].PayloadID != p.PayloadID {
		t.Errorf("VectorSearch[0].PayloadID = %q, want %q", results[0].PayloadID, p.PayloadID)
	}
}

// TestSQLiteDestination_VectorSearch_EmptyEmbedding returns empty for zero-length query.
func TestSQLiteDestination_VectorSearch_EmptyEmbedding(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	results, err := d.VectorSearch(context.Background(), nil, 5)
	if err != nil {
		t.Fatalf("VectorSearch(nil): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("VectorSearch(nil) len = %d, want 0", len(results))
	}
}

// TestSQLiteDestination_Migrate is a no-op that must return nil.
func TestSQLiteDestination_Migrate(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	if err := d.Migrate(context.Background(), 1); err != nil {
		t.Errorf("Migrate: %v", err)
	}
}

// TestSQLiteDestination_Health_OK verifies Health returns OK=true on an open DB.
func TestSQLiteDestination_Health_OK(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
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

// TestSQLiteDestination_Health_ClosedDB verifies Health returns OK=false after Close.
func TestSQLiteDestination_Health_ClosedDB(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
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

// TestSQLiteDestination_InterfaceCompliance is a compile-time guard that
// *SQLiteDestination implements Destination.
func TestSQLiteDestination_InterfaceCompliance(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()
	var _ destination.Destination = d
}

// TestSQLiteDestination_Read_TimestampRoundtrip verifies timestamp is preserved.
func TestSQLiteDestination_Read_TimestampRoundtrip(t *testing.T) {
	t.Helper()
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	ts := time.Date(2026, 6, 15, 10, 30, 45, 0, time.UTC)
	p := basePayload("ts-roundtrip-01")
	p.Timestamp = ts
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := d.Read(context.Background(), p.PayloadID)
	if err != nil || got == nil {
		t.Fatalf("Read: err=%v got=%v", err, got)
	}
	if !got.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, ts)
	}
}
