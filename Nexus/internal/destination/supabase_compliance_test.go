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
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// sbRow is the JSON shape that the Supabase REST API returns for a memories row.
// Mirrors the unexported supabaseQueryRow in supabase.go.
type sbRow struct {
	PayloadID        string            `json:"payload_id"`
	RequestID        string            `json:"request_id"`
	Source           string            `json:"source"`
	Subject          string            `json:"subject"`
	Namespace        string            `json:"namespace"`
	Destination      string            `json:"destination"`
	Collection       string            `json:"collection"`
	Content          string            `json:"content"`
	Model            string            `json:"model"`
	Role             string            `json:"role"`
	Timestamp        string            `json:"timestamp"`
	IdempotencyKey   string            `json:"idempotency_key"`
	SchemaVersion    int               `json:"schema_version"`
	TransformVersion string            `json:"transform_version"`
	ActorType        string            `json:"actor_type"`
	ActorID          string            `json:"actor_id"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// supabaseMock is a stateful in-memory HTTP handler that mimics the Supabase
// REST API endpoints used by SupabaseDestination.
type supabaseMock struct {
	mu         sync.Mutex
	rows       map[string]sbRow // keyed by payload_id
	headStatus int              // status returned by HEAD /rest/v1/memories
}

func newSupabaseMock() *supabaseMock {
	return &supabaseMock{
		rows:       make(map[string]sbRow),
		headStatus: http.StatusOK,
	}
}

func payloadToSBRow(p destination.TranslatedPayload) sbRow {
	return sbRow{
		PayloadID:        p.PayloadID,
		RequestID:        p.RequestID,
		Source:           p.Source,
		Subject:          p.Subject,
		Namespace:        p.Namespace,
		Destination:      p.Destination,
		Collection:       p.Collection,
		Content:          p.Content,
		Model:            p.Model,
		Role:             p.Role,
		Timestamp:        p.Timestamp.UTC().Format(time.RFC3339Nano),
		IdempotencyKey:   p.IdempotencyKey,
		SchemaVersion:    p.SchemaVersion,
		TransformVersion: p.TransformVersion,
		ActorType:        p.ActorType,
		ActorID:          p.ActorID,
		Metadata:         p.Metadata,
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (m *supabaseMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case r.Method == http.MethodHead && path == "/rest/v1/memories":
		w.WriteHeader(m.headStatus)

	case r.Method == http.MethodPost && path == "/rest/v1/memories":
		var row sbRow
		if err := json.NewDecoder(r.Body).Decode(&row); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.rows[row.PayloadID] = row
		m.mu.Unlock()
		w.WriteHeader(http.StatusCreated)

	case r.Method == http.MethodGet && path == "/rest/v1/memories":
		q := r.URL.Query()
		pidFilter := q.Get("payload_id") // "eq.{id}" or ""
		nsFilter := q.Get("namespace")   // "eq.{ns}" or ""

		m.mu.Lock()
		result := make([]sbRow, 0, len(m.rows))
		for _, row := range m.rows {
			if pidFilter != "" {
				wantID := strings.TrimPrefix(pidFilter, "eq.")
				if row.PayloadID != wantID {
					continue
				}
			}
			if nsFilter != "" {
				wantNS := strings.TrimPrefix(nsFilter, "eq.")
				if row.Namespace != wantNS {
					continue
				}
			}
			result = append(result, row)
		}
		m.mu.Unlock()
		writeJSON(w, http.StatusOK, result)

	case r.Method == http.MethodDelete && path == "/rest/v1/memories":
		q := r.URL.Query()
		pidFilter := q.Get("payload_id") // "eq.{id}"
		if pidFilter != "" {
			wantID := strings.TrimPrefix(pidFilter, "eq.")
			m.mu.Lock()
			delete(m.rows, wantID)
			m.mu.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodPost && path == "/rest/v1/rpc/match_memories":
		// Return empty scored results (no real vector math in mock).
		writeJSON(w, http.StatusOK, []struct{}{})

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// newTestSupabase starts an httptest.Server backed by supabaseMock and returns
// a SupabaseDestination pointed at it. The mock is returned so individual tests
// can configure headStatus or inspect state.
func newTestSupabase(t *testing.T) (*destination.SupabaseDestination, *supabaseMock, func()) {
	t.Helper()
	mock := newSupabaseMock()
	srv := httptest.NewServer(mock)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	d, err := destination.OpenSupabase(srv.URL, "test-key", logger)
	if err != nil {
		srv.Close()
		t.Fatalf("OpenSupabase: %v", err)
	}
	return d, mock, func() {
		_ = d.Close()
		srv.Close()
	}
}

// TestSupabaseDestination_InterfaceCompliance is a compile-time guard that
// *SupabaseDestination implements Destination.
func TestSupabaseDestination_InterfaceCompliance(t *testing.T) {
	t.Helper()
	var _ destination.Destination = (*destination.SupabaseDestination)(nil)
}

func TestSupabaseDestination_Name(t *testing.T) {
	t.Helper()
	d, _, cleanup := newTestSupabase(t)
	defer cleanup()
	if got := d.Name(); got != "supabase" {
		t.Errorf("Name() = %q, want %q", got, "supabase")
	}
}

func TestSupabaseDestination_Read_Found(t *testing.T) {
	t.Helper()
	d, mock, cleanup := newTestSupabase(t)
	defer cleanup()

	p := basePayload("sb-read-found-01")
	mock.mu.Lock()
	mock.rows[p.PayloadID] = payloadToSBRow(p)
	mock.mu.Unlock()

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

func TestSupabaseDestination_Read_NotFound(t *testing.T) {
	t.Helper()
	d, _, cleanup := newTestSupabase(t)
	defer cleanup()

	got, err := d.Read(context.Background(), "sb-no-such-id")
	if err != nil {
		t.Fatalf("Read unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("Read = %v, want nil for missing ID", got)
	}
}

func TestSupabaseDestination_Read_HTTPError(t *testing.T) {
	t.Helper()
	// Use a server that always returns 500 for GETs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	d, err := destination.OpenSupabase(srv.URL, "test-key", logger)
	if err != nil {
		t.Fatalf("OpenSupabase: %v", err)
	}
	defer func() { _ = d.Close() }()

	_, err = d.Read(context.Background(), "any-id")
	if err == nil {
		t.Error("Read: expected error for HTTP 500, got nil")
	}
}

func TestSupabaseDestination_Search(t *testing.T) {
	t.Helper()
	d, mock, cleanup := newTestSupabase(t)
	defer cleanup()

	p1 := basePayload("sb-search-01")
	p1.Namespace = "sb-ns-a"
	p2 := basePayload("sb-search-02")
	p2.Namespace = "sb-ns-b"
	mock.mu.Lock()
	mock.rows[p1.PayloadID] = payloadToSBRow(p1)
	mock.rows[p2.PayloadID] = payloadToSBRow(p2)
	mock.mu.Unlock()

	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "sb-ns-a"})
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

func TestSupabaseDestination_Search_Empty(t *testing.T) {
	t.Helper()
	d, _, cleanup := newTestSupabase(t)
	defer cleanup()

	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "sb-no-such-ns"})
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

func TestSupabaseDestination_Delete_Exists(t *testing.T) {
	t.Helper()
	d, mock, cleanup := newTestSupabase(t)
	defer cleanup()

	p := basePayload("sb-delete-exists-01")
	mock.mu.Lock()
	mock.rows[p.PayloadID] = payloadToSBRow(p)
	mock.mu.Unlock()

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

func TestSupabaseDestination_Delete_NotExists(t *testing.T) {
	t.Helper()
	d, _, cleanup := newTestSupabase(t)
	defer cleanup()

	if err := d.Delete(context.Background(), "sb-no-such-id"); err != nil {
		t.Errorf("Delete of missing ID should be no-op, got: %v", err)
	}
}

func TestSupabaseDestination_VectorSearch_EmptyEmbedding(t *testing.T) {
	t.Helper()
	d, _, cleanup := newTestSupabase(t)
	defer cleanup()

	results, err := d.VectorSearch(context.Background(), nil, 5)
	if err != nil {
		t.Fatalf("VectorSearch(nil): %v", err)
	}
	if len(results) != 0 {
		t.Errorf("VectorSearch(nil) len = %d, want 0", len(results))
	}
}

func TestSupabaseDestination_VectorSearch(t *testing.T) {
	t.Helper()
	d, _, cleanup := newTestSupabase(t)
	defer cleanup()

	// Mock returns empty scored results; verify no error and empty non-nil slice.
	results, err := d.VectorSearch(context.Background(), []float32{0.1, 0.2, 0.3}, 5)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if results == nil {
		t.Error("VectorSearch returned nil, want non-nil slice")
	}
}

func TestSupabaseDestination_Migrate(t *testing.T) {
	t.Helper()
	d, _, cleanup := newTestSupabase(t)
	defer cleanup()

	if err := d.Migrate(context.Background(), 1); err != nil {
		t.Errorf("Migrate: %v", err)
	}
}

func TestSupabaseDestination_Health_OK(t *testing.T) {
	t.Helper()
	d, _, cleanup := newTestSupabase(t)
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

func TestSupabaseDestination_Health_ServerError(t *testing.T) {
	t.Helper()
	d, mock, cleanup := newTestSupabase(t)
	defer cleanup()

	mock.headStatus = http.StatusInternalServerError

	h, err := d.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h == nil {
		t.Fatal("Health returned nil HealthStatus")
	}
	if h.OK {
		t.Error("Health.OK = true for HTTP 500, want false")
	}
	if h.Error == "" {
		t.Error("Health.Error is empty for HTTP 500, want non-empty")
	}
}
