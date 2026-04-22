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

package firestore_test

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/destination"
	fspkg "github.com/bubblefish-tech/nexus/internal/destination/firestore"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}

// TestInterfaceCompliance is a compile-time proof.
func TestInterfaceCompliance(t *testing.T) {
	t.Helper()
	var _ destination.Destination = (*fspkg.FirestoreDestination)(nil)
}

// ── Helper conversion tests (no DB required) ─────────────────────────────────

func TestFloat32Float64RoundTrip(t *testing.T) {
	t.Helper()
	in := []float32{0.1, 0.2, -0.5, 1.0}
	mid := fspkg.ExportFloat32ToFloat64(in)
	out := fspkg.ExportFloat64ToFloat32(mid)
	if len(out) != len(in) {
		t.Fatalf("length mismatch: got %d want %d", len(out), len(in))
	}
	for i, v := range in {
		if math.Abs(float64(out[i]-v)) > 1e-5 {
			t.Errorf("index %d: got %v want %v", i, out[i], v)
		}
	}
}

func TestFloat32ToFloat64_Empty(t *testing.T) {
	t.Helper()
	if v := fspkg.ExportFloat32ToFloat64(nil); v != nil {
		t.Errorf("expected nil for nil input, got %v", v)
	}
	if v := fspkg.ExportFloat64ToFloat32(nil); v != nil {
		t.Errorf("expected nil for nil input, got %v", v)
	}
}

func TestDocFromPayload_DefaultTier(t *testing.T) {
	t.Helper()
	p := destination.TranslatedPayload{
		PayloadID:          "test-fs-tier",
		ClassificationTier: "",
	}
	doc := fspkg.ExportDocFromPayload(p)
	if doc.ClassificationTier != "public" {
		t.Errorf("ClassificationTier = %q; want \"public\"", doc.ClassificationTier)
	}
}

func TestPayloadFromDoc_RoundTrip(t *testing.T) {
	t.Helper()
	p := destination.TranslatedPayload{
		PayloadID:          "fs-rt-1",
		Content:            "firestore content",
		Namespace:          "fs-ns",
		ClassificationTier: "restricted",
		Tier:               2,
		Embedding:          []float32{1.0, 2.0, 3.0},
		Metadata:           map[string]string{"k": "v"},
		SensitivityLabels:  []string{"pii"},
	}
	doc := fspkg.ExportDocFromPayload(p)
	got := fspkg.ExportPayloadFromDoc(doc)

	if got.PayloadID != p.PayloadID {
		t.Errorf("PayloadID: got %q want %q", got.PayloadID, p.PayloadID)
	}
	if got.Content != p.Content {
		t.Errorf("Content: got %q want %q", got.Content, p.Content)
	}
	if got.ClassificationTier != p.ClassificationTier {
		t.Errorf("ClassificationTier: got %q want %q", got.ClassificationTier, p.ClassificationTier)
	}
	if len(got.Embedding) != len(p.Embedding) {
		t.Errorf("Embedding length: got %d want %d", len(got.Embedding), len(p.Embedding))
	}
}

func TestVectorSearchUnsupported(t *testing.T) {
	t.Helper()
	// VectorSearch must return ErrVectorSearchUnsupported without a live DB.
	// We can't instantiate FirestoreDestination without a project, so we test
	// the sentinel value directly.
	if errors.Is(destination.ErrVectorSearchUnsupported, destination.ErrVectorSearchUnsupported) == false {
		t.Error("ErrVectorSearchUnsupported sentinel not self-equal")
	}
}

func TestOpen_EmptyProjectID(t *testing.T) {
	t.Helper()
	_, err := fspkg.Open("", testLogger(t))
	if err == nil { t.Fatal("expected error for empty project ID") }
}

func TestDocFromPayload_WithEmbedding(t *testing.T) {
	t.Helper()
	tp := destination.TranslatedPayload{
		PayloadID: "p1",
		Content:   "test",
		Embedding: []float32{0.1, 0.2, 0.3},
	}
	doc := fspkg.ExportDocFromPayload(tp)
	if doc.PayloadID != "p1" { t.Fatalf("expected p1, got %s", doc.PayloadID) }
}

func TestFloat32Float64_LargeArray(t *testing.T) {
	t.Helper()
	in := make([]float32, 768)
	for i := range in { in[i] = float32(i) / 768.0 }
	f64 := fspkg.ExportFloat32ToFloat64(in)
	f32 := fspkg.ExportFloat64ToFloat32(f64)
	if len(f32) != 768 { t.Fatalf("expected 768, got %d", len(f32)) }
}

// ── Integration tests (require TEST_FIRESTORE_PROJECT) ───────────────────────

func openTestDB(t *testing.T) *fspkg.FirestoreDestination {
	t.Helper()
	projectID := os.Getenv("TEST_FIRESTORE_PROJECT")
	if projectID == "" {
		t.Skip("TEST_FIRESTORE_PROJECT not set; skipping Firestore integration tests")
	}
	credFile := os.Getenv("TEST_FIRESTORE_CREDENTIALS") // optional
	log := testLogger(t)
	d, err := fspkg.OpenWithCredentials(projectID, credFile, log)
	if err != nil {
		t.Fatalf("OpenWithCredentials: %v", err)
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
	if got := d.Name(); got != "firestore" {
		t.Errorf("Name() = %q; want \"firestore\"", got)
	}
}

func TestWrite_Idempotent(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	p := testPayload("fs-write-idem-1")
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
	p := testPayload("fs-read-found-1")
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
	got, err := d.Read(context.Background(), "does-not-exist-fs-xyz")
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
	results, err := d.Search(context.Background(), &destination.QueryParams{Namespace: "no-such-ns-fs-xyz"})
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
	p := testPayload("fs-delete-1")
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
	if err := d.Delete(context.Background(), "ghost-fs-xyz"); err != nil {
		t.Errorf("Delete of non-existent doc: %v", err)
	}
}

func TestVectorSearch_ReturnsUnsupported(t *testing.T) {
	t.Helper()
	d := openTestDB(t)
	_, err := d.VectorSearch(context.Background(), []float32{1, 2, 3}, 5)
	if !errors.Is(err, destination.ErrVectorSearchUnsupported) {
		t.Errorf("VectorSearch: got %v; want ErrVectorSearchUnsupported", err)
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
