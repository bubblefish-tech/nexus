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

package mysql_test

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
	mysqlpkg "github.com/bubblefish-tech/nexus/internal/destination/mysql"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}

// TestInterfaceCompliance is a compile-time proof — if MySQLDestination does
// not implement destination.Destination this file will not compile.
func TestInterfaceCompliance(t *testing.T) {
	t.Helper()
	var _ destination.Destination = (*mysqlpkg.MySQLDestination)(nil)
}

// ── Helper encoding/decoding tests (no DB required) ──────────────────────────

func TestEncodeDecodeEmbedding_RoundTrip(t *testing.T) {
	t.Helper()
	in := []float32{0.1, 0.2, 0.3, -0.5, 1.0}
	blob := mysqlpkg.ExportEncodeEmbedding(in)
	out := mysqlpkg.ExportDecodeEmbedding(blob)
	if len(out) != len(in) {
		t.Fatalf("length mismatch: got %d want %d", len(out), len(in))
	}
	for i, v := range in {
		if math.Abs(float64(out[i]-v)) > 1e-6 {
			t.Errorf("index %d: got %v want %v", i, out[i], v)
		}
	}
}

func TestEncodeDecodeEmbedding_Empty(t *testing.T) {
	t.Helper()
	if blob := mysqlpkg.ExportEncodeEmbedding(nil); blob != nil {
		t.Errorf("expected nil blob for nil input, got %v", blob)
	}
	if v := mysqlpkg.ExportDecodeEmbedding(nil); v != nil {
		t.Errorf("expected nil slice for nil blob, got %v", v)
	}
}

func TestEncodeDecodeEmbedding_BadLength(t *testing.T) {
	t.Helper()
	// 5 bytes is not a multiple of 4 — must return nil, not panic.
	bad := []byte{1, 2, 3, 4, 5}
	if v := mysqlpkg.ExportDecodeEmbedding(bad); v != nil {
		t.Errorf("expected nil for truncated blob, got %v", v)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	t.Helper()
	a := []float32{1, 0}
	b := []float32{0, 1}
	if s := mysqlpkg.ExportCosineSimilarity(a, b); s != 0 {
		t.Errorf("orthogonal vectors: got %v want 0", s)
	}
}

func TestCosineSimilarity_Identical(t *testing.T) {
	t.Helper()
	a := []float32{1, 2, 3}
	if s := mysqlpkg.ExportCosineSimilarity(a, a); math.Abs(float64(s-1.0)) > 1e-5 {
		t.Errorf("identical vectors: got %v want 1.0", s)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	t.Helper()
	zero := []float32{0, 0}
	a := []float32{1, 2}
	if s := mysqlpkg.ExportCosineSimilarity(zero, a); s != 0 {
		t.Errorf("zero vector: got %v want 0", s)
	}
}

func TestMarshalMetadata_Nil(t *testing.T) {
	t.Helper()
	s, err := mysqlpkg.ExportMarshalMetadata(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "{}" {
		t.Errorf("got %q want \"{}\"", s)
	}
}

func TestMarshalMetadata_Map(t *testing.T) {
	t.Helper()
	m := map[string]string{"key": "val"}
	s, err := mysqlpkg.ExportMarshalMetadata(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == "" || s == "{}" {
		t.Errorf("expected non-empty JSON, got %q", s)
	}
}

func TestParseSensitivityLabels_Empty(t *testing.T) {
	t.Helper()
	if l := mysqlpkg.ExportParseSensitivityLabels(""); len(l) != 0 {
		t.Fatalf("expected 0, got %d", len(l))
	}
}

func TestParseSensitivityLabels_Multiple(t *testing.T) {
	t.Helper()
	l := mysqlpkg.ExportParseSensitivityLabels("pii,financial")
	if len(l) != 2 { t.Fatalf("expected 2, got %d", len(l)) }
}

func TestOpen_InvalidDSN(t *testing.T) {
	t.Helper()
	_, err := mysqlpkg.Open("invalid:dsn", testLogger(t))
	if err == nil { t.Fatal("expected error") }
}

// ── Integration tests (require TEST_MYSQL_DSN) ────────────────────────────────

func openTestDB(t *testing.T) *mysqlpkg.MySQLDestination {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set; skipping MySQL integration tests")
	}
	log := testLogger(t)
	d, err := mysqlpkg.Open(dsn, log)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func testPayload(id string) destination.TranslatedPayload {
	return destination.TranslatedPayload{
		PayloadID:          id,
		RequestID:          "req-" + id,
		Source:             "test-source",
		Subject:            "test-subject",
		Namespace:          "test-ns",
		Destination:        "test-dest",
		Collection:         "test-col",
		Content:            "hello " + id,
		Model:              "gpt-4",
		Role:               "user",
		Timestamp:          time.Now().UTC().Truncate(time.Microsecond),
		IdempotencyKey:     "ikey-" + id,
		SchemaVersion:      1,
		TransformVersion:   "v1",
		ActorType:          "user",
		ActorID:            "actor-1",
		Metadata:           map[string]string{"foo": "bar"},
		SensitivityLabels:  []string{"pii"},
		ClassificationTier: "internal",
		Tier:               1,
	}
}

func TestName(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	if got := d.Name(); got != "mysql" {
		t.Errorf("Name() = %q; want \"mysql\"", got)
	}
}

func TestWrite_Idempotent(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	p := testPayload("write-idem-1")
	if err := d.Write(p); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := d.Write(p); err != nil {
		t.Fatalf("second Write (idempotent): %v", err)
	}
}

func TestRead_Found(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	p := testPayload("read-found-1")
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := d.Read(context.Background(), p.PayloadID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got == nil {
		t.Fatal("Read returned nil; want record")
	}
	if got.PayloadID != p.PayloadID {
		t.Errorf("PayloadID = %q; want %q", got.PayloadID, p.PayloadID)
	}
	if got.Content != p.Content {
		t.Errorf("Content = %q; want %q", got.Content, p.Content)
	}
}

func TestRead_NotFound(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	got, err := d.Read(context.Background(), "does-not-exist-xyz")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != nil {
		t.Errorf("Read returned %v; want nil", got)
	}
}

func TestSearch(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	p := testPayload("search-1")
	p.Namespace = "search-ns-unique"
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "search-ns-unique"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("Search returned no results; want at least 1")
	}
}

func TestSearch_Empty(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "no-such-namespace-xyz"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if results == nil {
		t.Error("Search returned nil; want empty non-nil slice")
	}
}

func TestDelete_Exists(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	p := testPayload("delete-exists-1")
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
		t.Error("record still present after Delete")
	}
}

func TestDelete_NotExists(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	if err := d.Delete(context.Background(), "ghost-id-xyz"); err != nil {
		t.Errorf("Delete of non-existent ID: %v", err)
	}
}

func TestVectorSearch_EmptyEmbedding(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	results, err := d.VectorSearch(context.Background(), nil, 10)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if results == nil {
		t.Error("VectorSearch returned nil; want empty non-nil slice")
	}
}

func TestVectorSearch_AppLevelCosine(t *testing.T) {
	t.Helper()
	d := openTestDB(t)

	// Write a payload with a known embedding.
	p := testPayload("vec-search-1")
	p.Embedding = []float32{1.0, 0.0, 0.0}
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Query with the same vector — should find our record as top result.
	results, err := d.VectorSearch(context.Background(), []float32{1.0, 0.0, 0.0}, 5)
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	found := false
	for _, r := range results {
		if r.PayloadID == p.PayloadID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("VectorSearch did not return the payload with matching embedding")
	}
}

func TestMigrate_NoOp(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	if err := d.Migrate(context.Background(), 1); err != nil {
		t.Errorf("Migrate: %v", err)
	}
}

func TestHealth_OK(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	status, err := d.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !status.OK {
		t.Errorf("Health.OK = false; error: %s", status.Error)
	}
	if status.Latency == 0 {
		t.Error("Health.Latency = 0; expected non-zero round-trip time")
	}
}
