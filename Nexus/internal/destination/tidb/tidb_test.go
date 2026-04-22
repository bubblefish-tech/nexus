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

package tidb_test

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
	tidbpkg "github.com/bubblefish-tech/nexus/internal/destination/tidb"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}

// TestInterfaceCompliance is a compile-time proof.
func TestInterfaceCompliance(t *testing.T) {
	t.Helper()
	var _ destination.Destination = (*tidbpkg.TiDBDestination)(nil)
}

// ── Helper encoding/decoding tests (no DB required) ──────────────────────────

func TestEncodeDecodeEmbedding_RoundTrip(t *testing.T) {
	t.Helper()
	in := []float32{0.1, 0.2, 0.3, -0.5, 1.0}
	blob := tidbpkg.ExportEncodeEmbedding(in)
	out := tidbpkg.ExportDecodeEmbedding(blob)
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
	if b := tidbpkg.ExportEncodeEmbedding(nil); b != nil {
		t.Errorf("expected nil for nil input, got %v", b)
	}
	if v := tidbpkg.ExportDecodeEmbedding(nil); v != nil {
		t.Errorf("expected nil for nil blob, got %v", v)
	}
}

func TestEncodeDecodeEmbedding_BadLength(t *testing.T) {
	t.Helper()
	bad := []byte{1, 2, 3, 4, 5}
	if v := tidbpkg.ExportDecodeEmbedding(bad); v != nil {
		t.Errorf("expected nil for truncated blob, got %v", v)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	t.Helper()
	a, b := []float32{1, 0}, []float32{0, 1}
	if s := tidbpkg.ExportCosineSimilarity(a, b); s != 0 {
		t.Errorf("orthogonal: got %v want 0", s)
	}
}

func TestCosineSimilarity_Identical(t *testing.T) {
	t.Helper()
	a := []float32{1, 2, 3}
	if s := tidbpkg.ExportCosineSimilarity(a, a); math.Abs(float64(s-1.0)) > 1e-5 {
		t.Errorf("identical: got %v want 1.0", s)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	t.Helper()
	zero, a := []float32{0, 0}, []float32{1, 2}
	if s := tidbpkg.ExportCosineSimilarity(zero, a); s != 0 {
		t.Errorf("zero vector: got %v want 0", s)
	}
}

func TestMarshalEmbeddingTV_RoundTrip(t *testing.T) {
	t.Helper()
	v := []float32{1.0, 2.5, -0.3}
	s := tidbpkg.ExportMarshalEmbeddingTV(v)
	if s == "" {
		t.Fatal("marshalEmbeddingTV returned empty string for non-nil input")
	}
	if s[0] != '[' {
		t.Errorf("expected JSON array, got %q", s)
	}
}

func TestMarshalEmbeddingTV_Empty(t *testing.T) {
	t.Helper()
	if s := tidbpkg.ExportMarshalEmbeddingTV(nil); s != "" {
		t.Errorf("expected empty string for nil input, got %q", s)
	}
}

func TestMarshalMetadata_Nil(t *testing.T) {
	t.Helper()
	s, err := tidbpkg.ExportMarshalMetadata(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s != "{}" {
		t.Errorf("got %q want \"{}\"", s)
	}
}

func TestParseSensitivityLabels_Empty(t *testing.T) {
	t.Helper()
	labels := tidbpkg.ExportParseSensitivityLabels("")
	if len(labels) != 0 {
		t.Fatalf("expected 0 labels for empty string, got %d", len(labels))
	}
}

func TestParseSensitivityLabels_Single(t *testing.T) {
	t.Helper()
	labels := tidbpkg.ExportParseSensitivityLabels("pii")
	if len(labels) != 1 || labels[0] != "pii" {
		t.Fatalf("expected [pii], got %v", labels)
	}
}

func TestParseSensitivityLabels_Multiple(t *testing.T) {
	t.Helper()
	labels := tidbpkg.ExportParseSensitivityLabels("pii,financial,health")
	if len(labels) != 3 {
		t.Fatalf("expected 3 labels, got %d", len(labels))
	}
}

func TestOpen_InvalidDSN(t *testing.T) {
	t.Helper()
	_, err := tidbpkg.Open("invalid:dsn:that:wont:connect", testLogger(t))
	if err == nil {
		t.Fatal("expected error for invalid DSN")
	}
}

func TestMarshalMetadata_WithValues(t *testing.T) {
	t.Helper()
	m := map[string]string{"key": "value", "foo": "bar"}
	data, err := tidbpkg.ExportMarshalMetadata(m)
	if err != nil { t.Fatal(err) }
	if data == "" || data == "{}" {
		t.Fatal("expected non-empty metadata JSON")
	}
}

// ── Integration tests (require TEST_TIDB_DSN) ────────────────────────────────

func openTestDB(t *testing.T) *tidbpkg.TiDBDestination {
	t.Helper()
	dsn := os.Getenv("TEST_TIDB_DSN")
	if dsn == "" {
		t.Skip("TEST_TIDB_DSN not set; skipping TiDB integration tests")
	}
	log := testLogger(t)
	d, err := tidbpkg.Open(dsn, log)
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
	if got := d.Name(); got != "tidb" {
		t.Errorf("Name() = %q; want \"tidb\"", got)
	}
}

func TestWrite_Idempotent(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	p := testPayload("tidb-write-idem-1")
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
	p := testPayload("tidb-read-found-1")
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
	if got.Content != p.Content {
		t.Errorf("Content = %q; want %q", got.Content, p.Content)
	}
}

func TestRead_NotFound(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	got, err := d.Read(context.Background(), "does-not-exist-tidb-xyz")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != nil {
		t.Errorf("Read returned %v; want nil", got)
	}
}

func TestSearch_Empty(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "no-such-ns-tidb-xyz"})
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
	p := testPayload("tidb-delete-1")
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
	if err := d.Delete(context.Background(), "ghost-tidb-xyz"); err != nil {
		t.Errorf("Delete of non-existent: %v", err)
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
}
