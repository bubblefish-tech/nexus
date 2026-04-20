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

package mongodb_test

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
	mongodbpkg "github.com/bubblefish-tech/nexus/internal/destination/mongodb"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}

// TestInterfaceCompliance is a compile-time proof — if MongoDBDestination does
// not implement destination.Destination this file will not compile.
func TestInterfaceCompliance(t *testing.T) {
	t.Helper()
	var _ destination.Destination = (*mongodbpkg.MongoDBDestination)(nil)
}

// ── Helper encoding/decoding tests (no DB required) ──────────────────────────

func TestEncodeDecodeEmbedding_RoundTrip(t *testing.T) {
	t.Helper()
	in := []float32{0.1, 0.2, 0.3, -0.5, 1.0}
	blob := mongodbpkg.ExportEncodeEmbedding(in)
	out := mongodbpkg.ExportDecodeEmbedding(blob)
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
	if blob := mongodbpkg.ExportEncodeEmbedding(nil); blob != nil {
		t.Errorf("expected nil blob for nil input, got %v", blob)
	}
	if v := mongodbpkg.ExportDecodeEmbedding(nil); v != nil {
		t.Errorf("expected nil slice for nil blob, got %v", v)
	}
}

func TestEncodeDecodeEmbedding_BadLength(t *testing.T) {
	t.Helper()
	bad := []byte{1, 2, 3, 4, 5} // 5 bytes is not a multiple of 4 — must return nil, not panic.
	if v := mongodbpkg.ExportDecodeEmbedding(bad); v != nil {
		t.Errorf("expected nil for truncated blob, got %v", v)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	t.Helper()
	a := []float32{1, 0}
	b := []float32{0, 1}
	if s := mongodbpkg.ExportCosineSimilarity(a, b); s != 0 {
		t.Errorf("orthogonal vectors: got %v want 0", s)
	}
}

func TestCosineSimilarity_Identical(t *testing.T) {
	t.Helper()
	a := []float32{1, 2, 3}
	if s := mongodbpkg.ExportCosineSimilarity(a, a); math.Abs(float64(s-1.0)) > 1e-5 {
		t.Errorf("identical vectors: got %v want 1.0", s)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	t.Helper()
	zero := []float32{0, 0}
	a := []float32{1, 2}
	if s := mongodbpkg.ExportCosineSimilarity(zero, a); s != 0 {
		t.Errorf("zero vector: got %v want 0", s)
	}
}

func TestDocFromPayload_DefaultTier(t *testing.T) {
	t.Helper()
	p := destination.TranslatedPayload{
		PayloadID:          "test-default-tier",
		ClassificationTier: "",
	}
	doc := mongodbpkg.ExportDocFromPayload(p)
	if doc.ClassificationTier != "public" {
		t.Errorf("ClassificationTier = %q; want \"public\"", doc.ClassificationTier)
	}
}

func TestPayloadFromDoc_RoundTrip(t *testing.T) {
	t.Helper()
	p := destination.TranslatedPayload{
		PayloadID:          "rt-payload-1",
		Content:            "hello world",
		Namespace:          "test-ns",
		ClassificationTier: "internal",
		Tier:               2,
		Embedding:          []float32{1.0, 2.0, 3.0},
		Metadata:           map[string]string{"key": "val"},
		SensitivityLabels:  []string{"pii"},
	}
	doc := mongodbpkg.ExportDocFromPayload(p)
	got := mongodbpkg.ExportPayloadFromDoc(doc)

	if got.PayloadID != p.PayloadID {
		t.Errorf("PayloadID: got %q want %q", got.PayloadID, p.PayloadID)
	}
	if got.Content != p.Content {
		t.Errorf("Content: got %q want %q", got.Content, p.Content)
	}
	if got.ClassificationTier != p.ClassificationTier {
		t.Errorf("ClassificationTier: got %q want %q", got.ClassificationTier, p.ClassificationTier)
	}
	if got.Tier != p.Tier {
		t.Errorf("Tier: got %d want %d", got.Tier, p.Tier)
	}
	if len(got.Embedding) != len(p.Embedding) {
		t.Errorf("Embedding length: got %d want %d", len(got.Embedding), len(p.Embedding))
	}
	for i, v := range p.Embedding {
		if math.Abs(float64(got.Embedding[i]-v)) > 1e-6 {
			t.Errorf("Embedding[%d]: got %v want %v", i, got.Embedding[i], v)
		}
	}
	if got.Metadata["key"] != "val" {
		t.Errorf("Metadata[key]: got %q want \"val\"", got.Metadata["key"])
	}
	if len(got.SensitivityLabels) != 1 || got.SensitivityLabels[0] != "pii" {
		t.Errorf("SensitivityLabels: got %v want [pii]", got.SensitivityLabels)
	}
}

// ── Integration tests (require TEST_MONGODB_URI) ─────────────────────────────

func openTestDB(t *testing.T) *mongodbpkg.MongoDBDestination {
	t.Helper()
	uri := os.Getenv("TEST_MONGODB_URI")
	if uri == "" {
		t.Skip("TEST_MONGODB_URI not set; skipping MongoDB integration tests")
	}
	log := testLogger(t)
	d, err := mongodbpkg.Open(uri, log)
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
		Timestamp:          time.Now().UTC().Truncate(time.Millisecond),
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
	if got := d.Name(); got != "mongodb" {
		t.Errorf("Name() = %q; want \"mongodb\"", got)
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
	// Verify no duplicate: read back and confirm single record.
	got, err := d.Read(context.Background(), p.PayloadID)
	if err != nil {
		t.Fatalf("Read after duplicate write: %v", err)
	}
	if got == nil {
		t.Fatal("Read returned nil; want record")
	}
	if got.PayloadID != p.PayloadID {
		t.Errorf("PayloadID = %q; want %q", got.PayloadID, p.PayloadID)
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
	if got.ClassificationTier != p.ClassificationTier {
		t.Errorf("ClassificationTier = %q; want %q", got.ClassificationTier, p.ClassificationTier)
	}
}

func TestRead_NotFound(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	got, err := d.Read(context.Background(), "does-not-exist-xyz-abc")
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
	p := testPayload("search-unique-1")
	p.Namespace = "search-ns-unique-mongo"
	if err := d.Write(p); err != nil {
		t.Fatalf("Write: %v", err)
	}
	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "search-ns-unique-mongo"})
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
	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "no-such-namespace-xyz-mongo"})
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
	p := testPayload("delete-exists-mongo-1")
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
	if err := d.Delete(context.Background(), "ghost-id-xyz-mongo"); err != nil {
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

	p := testPayload("vec-search-mongo-1")
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
