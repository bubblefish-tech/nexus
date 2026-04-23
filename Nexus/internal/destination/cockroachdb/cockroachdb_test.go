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

package cockroachdb_test

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
	crdbpkg "github.com/bubblefish-tech/nexus/internal/destination/cockroachdb"
)

func testLogger(_ *testing.T) *slog.Logger { return slog.Default() }

// TestInterfaceCompliance is a compile-time proof — if CockroachDBDestination
// does not implement destination.Destination this file will not compile.
func TestInterfaceCompliance(t *testing.T) {
	t.Helper()
	var _ destination.Destination = (*crdbpkg.CockroachDBDestination)(nil)
}

// ── Helper function unit tests (no DB required) ───────────────────────────────

func TestEncodeDecodeEmbedding_RoundTrip(t *testing.T) {
	t.Helper()
	in := []float32{0.1, 0.2, 0.3, -0.5, 1.0}
	blob := crdbpkg.ExportEncodeEmbedding(in)
	out := crdbpkg.ExportDecodeEmbedding(blob)
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
	if blob := crdbpkg.ExportEncodeEmbedding(nil); blob != nil {
		t.Errorf("expected nil blob for nil input, got %v", blob)
	}
	if v := crdbpkg.ExportDecodeEmbedding(nil); v != nil {
		t.Errorf("expected nil slice for nil blob, got %v", v)
	}
}

func TestEncodeDecodeEmbedding_BadLength(t *testing.T) {
	t.Helper()
	bad := []byte{1, 2, 3, 4, 5} // not a multiple of 4
	if v := crdbpkg.ExportDecodeEmbedding(bad); v != nil {
		t.Errorf("expected nil for truncated blob, got %v", v)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	t.Helper()
	a := []float32{1, 0}
	b := []float32{0, 1}
	if s := crdbpkg.ExportCosineSimilarity(a, b); s != 0 {
		t.Errorf("orthogonal vectors: got %v want 0", s)
	}
}

func TestCosineSimilarity_Identical(t *testing.T) {
	t.Helper()
	a := []float32{1, 2, 3}
	if s := crdbpkg.ExportCosineSimilarity(a, a); math.Abs(float64(s-1.0)) > 1e-5 {
		t.Errorf("identical vectors: got %v want 1.0", s)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	t.Helper()
	zero := []float32{0, 0}
	a := []float32{1, 2}
	if s := crdbpkg.ExportCosineSimilarity(zero, a); s != 0 {
		t.Errorf("zero vector: got %v want 0", s)
	}
}

func TestMarshalMetadata_Nil(t *testing.T) {
	t.Helper()
	s, err := crdbpkg.ExportMarshalMetadata(nil)
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
	s, err := crdbpkg.ExportMarshalMetadata(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == "" || s == "{}" {
		t.Errorf("expected non-empty JSON, got %q", s)
	}
}

func TestParsePGTextArray_Empty(t *testing.T) {
	t.Helper()
	for _, input := range []string{"", "{}"} {
		if v := crdbpkg.ExportParsePGTextArray(input); v != nil {
			t.Errorf("input %q: got %v want nil", input, v)
		}
	}
}

func TestParsePGTextArray_Values(t *testing.T) {
	t.Helper()
	got := crdbpkg.ExportParsePGTextArray("{pii,financial}")
	if len(got) != 2 || got[0] != "pii" || got[1] != "financial" {
		t.Errorf("got %v want [pii financial]", got)
	}
}

func TestPGTextArray_RoundTrip(t *testing.T) {
	t.Helper()
	labels := []string{"pii", "financial"}
	encoded := crdbpkg.ExportPGTextArray(labels)
	decoded := crdbpkg.ExportParsePGTextArray(encoded)
	if len(decoded) != len(labels) {
		t.Fatalf("round-trip length mismatch: got %d want %d", len(decoded), len(labels))
	}
	for i, v := range labels {
		if decoded[i] != v {
			t.Errorf("index %d: got %q want %q", i, decoded[i], v)
		}
	}
}

func TestPGTextArray_Nil(t *testing.T) {
	t.Helper()
	if got := crdbpkg.ExportPGTextArray(nil); got != "{}" {
		t.Errorf("got %q want \"{}\"", got)
	}
}

func TestOpen_InvalidDSN(t *testing.T) {
	t.Helper()
	_, err := crdbpkg.Open("not-a-valid-dsn", testLogger(t))
	if err == nil { t.Fatal("expected error") }
}

func TestPGTextArray_EmptyInput(t *testing.T) {
	t.Helper()
	r := crdbpkg.ExportParsePGTextArray("")
	if len(r) != 0 { t.Fatalf("expected 0, got %d", len(r)) }
}

func TestMarshalMetadata_NonNil(t *testing.T) {
	t.Helper()
	m := map[string]string{"x": "y"}
	s, err := crdbpkg.ExportMarshalMetadata(m)
	if err != nil { t.Fatal(err) }
	if s == "" || s == "{}" {
		t.Fatal("expected non-empty JSON")
	}
}

// ── Integration tests (require TEST_CRDB_DSN) ─────────────────────────────────

func openTestDB(t *testing.T) *crdbpkg.CockroachDBDestination {
	t.Helper()
	dsn := os.Getenv("TEST_CRDB_DSN")
	if dsn == "" {
		t.Skip("TEST_CRDB_DSN not set; skipping CockroachDB integration tests")
	}
	d, err := crdbpkg.Open(dsn, testLogger(t))
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
	if got := d.Name(); got != "cockroachdb" {
		t.Errorf("Name() = %q; want \"cockroachdb\"", got)
	}
}

func TestWrite_Idempotent(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	p := testPayload("crdb-write-idem-1")
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
	p := testPayload("crdb-read-found-1")
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
	got, err := d.Read(context.Background(), "crdb-does-not-exist-xyz")
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
	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "crdb-no-such-ns-xyz"})
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
	p := testPayload("crdb-delete-exists-1")
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
	if err := d.Delete(context.Background(), "crdb-ghost-id-xyz"); err != nil {
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

	p := testPayload("crdb-vec-search-1")
	p.Namespace = "crdb-vec-ns-unique"
	p.Embedding = []float32{1.0, 0.0, 0.0}
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}

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
