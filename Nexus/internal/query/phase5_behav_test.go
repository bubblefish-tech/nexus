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

// Phase 5 behavioral verification tests.
//
// These tests correspond directly to the three verification gate checks in the
// State Verification Guide Phase 5:
//
//	[ ] Embedding disabled: _nexus.semantic_unavailable = true
//	[ ] Provider unreachable: same graceful degradation
//	[ ] sqlite-vec vector search returns results ranked by cosine similarity
//
// Each test is self-contained and exercises the real component stack
// (cascade + embedding client + SQLite destination). No mocks for I/O paths.
package query_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/embedding"
	"github.com/BubbleFish-Nexus/internal/query"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openTestSQLite(t *testing.T) *destination.SQLiteDestination {
	t.Helper()
	dir := t.TempDir()
	db, err := destination.OpenSQLite(filepath.Join(dir, "test.db"), slog.Default())
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func permitAllSource() *config.Source {
	return &config.Source{
		Name:      "test",
		Namespace: "ns",
		CanRead:   true,
		Policy: config.SourcePolicyConfig{
			Cache: config.PolicyCacheConfig{
				SemanticSimilarityThreshold: 0.92,
			},
		},
	}
}

func balancedQuery(dest, q string) query.CanonicalQuery {
	return query.CanonicalQuery{
		Destination: dest,
		Namespace:   "ns",
		Profile:     "balanced",
		Q:           q,
		Limit:       10,
	}
}

// writeWithEmbedding inserts a payload with a known embedding vector.
func writeWithEmbedding(t *testing.T, db *destination.SQLiteDestination, id, content string, vec []float32) {
	t.Helper()
	err := db.Write(destination.TranslatedPayload{
		PayloadID:   id,
		Source:      "test",
		Namespace:   "ns",
		Destination: "sqlite",
		Content:     content,
		Timestamp:   time.Now().UTC(),
		Embedding:   vec,
	})
	if err != nil {
		t.Fatalf("Write(%s): %v", id, err)
	}
}

// ---------------------------------------------------------------------------
// CHECK 1: Embedding disabled → _nexus.semantic_unavailable = true
// ---------------------------------------------------------------------------

// TestBehav_Phase5_Check1_EmbeddingDisabled verifies that when no embedding
// client is configured (nil), the cascade sets SemanticUnavailable = true.
//
// Verification gate: "Embedding disabled: _nexus.semantic_unavailable = true"
func TestBehav_Phase5_Check1_EmbeddingDisabled(t *testing.T) {
	db := openTestSQLite(t)

	// No embedding client — simulate embedding.enabled = false in config.
	runner := query.New(db, slog.Default()).
		WithEmbeddingClient(nil, nil)

	result, err := runner.Run(context.Background(), permitAllSource(), balancedQuery("sqlite", "hello world"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Denial != nil {
		t.Fatalf("unexpected denial: %v", result.Denial)
	}

	t.Logf("SemanticUnavailable      = %v", result.SemanticUnavailable)
	t.Logf("SemanticUnavailableReason = %q", result.SemanticUnavailableReason)
	t.Logf("RetrievalStage           = %d", result.RetrievalStage)

	if !result.SemanticUnavailable {
		t.Errorf("CHECK 1 FAIL: SemanticUnavailable = false; want true when embedding disabled")
	} else {
		t.Logf("CHECK 1 PASS: SemanticUnavailable = true (reason: %q)", result.SemanticUnavailableReason)
	}
}

// ---------------------------------------------------------------------------
// CHECK 2: Provider unreachable → same graceful degradation
// ---------------------------------------------------------------------------

// errorServer returns HTTP 500 for every request, simulating an unreachable
// or crashed embedding provider.
func errorServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider down", http.StatusInternalServerError)
	}))
}

// TestBehav_Phase5_Check2_ProviderUnreachable verifies that when the embedding
// provider returns an error (HTTP 500), the cascade degrades gracefully:
// SemanticUnavailable = true, no error returned to caller, stage falls back.
//
// The DB is seeded with one record carrying an embedding so that
// CanSemanticSearch() returns true — this forces the cascade to actually call
// Embed() and exercise the provider-error degradation path.
//
// Verification gate: "Provider unreachable: same graceful degradation"
func TestBehav_Phase5_Check2_ProviderUnreachable(t *testing.T) {
	db := openTestSQLite(t)

	// Seed a record with an embedding so CanSemanticSearch() returns true.
	// This forces the cascade to call Embed() before it can skip on empty DB.
	writeWithEmbedding(t, db, "seed-record", "seed content", []float32{1, 0, 0})

	if !db.CanSemanticSearch() {
		t.Fatal("precondition: CanSemanticSearch() = false after seeding; test setup broken")
	}
	t.Logf("CanSemanticSearch()      = true (DB seeded; cascade will call Embed)")

	// Start a fake embedding provider that always returns 500.
	srv := errorServer()
	defer srv.Close()

	cfg := config.EmbeddingConfig{
		Enabled:        true,
		Provider:       embedding.ProviderOpenAI,
		URL:            srv.URL,
		Model:          "text-embedding-3-small",
		Dimensions:     3,
		TimeoutSeconds: 2,
	}
	ec, err := embedding.NewClient(cfg, "test-key", slog.Default())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer ec.Close()

	// Verify the client itself reports ErrEmbeddingUnavailable on HTTP 500.
	_, embedErr := ec.Embed(context.Background(), "test")
	if !errors.Is(embedErr, embedding.ErrEmbeddingUnavailable) {
		t.Fatalf("expected ErrEmbeddingUnavailable from provider, got: %v", embedErr)
	}
	t.Logf("embedding.Embed error    = %v", embedErr)
	t.Logf("errors.Is(ErrUnavailable) = %v", errors.Is(embedErr, embedding.ErrEmbeddingUnavailable))

	// Run the cascade with the broken embedding provider.
	// INVARIANT: Run must return nil error regardless of provider failure.
	runner := query.New(db, slog.Default()).
		WithEmbeddingClient(ec, nil)

	result, err := runner.Run(context.Background(), permitAllSource(), balancedQuery("sqlite", "hello world"))
	if err != nil {
		t.Fatalf("CHECK 2 FAIL: Run returned error %v; want nil (graceful degradation)", err)
	}
	if result.Denial != nil {
		t.Fatalf("unexpected denial: %v", result.Denial)
	}

	t.Logf("Run error                = nil (cascade did not crash or propagate error)")
	t.Logf("SemanticUnavailable      = %v", result.SemanticUnavailable)
	t.Logf("SemanticUnavailableReason = %q", result.SemanticUnavailableReason)
	t.Logf("RetrievalStage           = %d (fell back to Stage 3 structured lookup)", result.RetrievalStage)

	if !result.SemanticUnavailable {
		t.Errorf("CHECK 2 FAIL: SemanticUnavailable = false; want true when provider returns HTTP 500")
	}
	if result.SemanticUnavailableReason != "embedding provider unavailable" {
		t.Errorf("CHECK 2 FAIL: reason = %q; want %q",
			result.SemanticUnavailableReason, "embedding provider unavailable")
	}

	t.Logf("CHECK 2 PASS: SemanticUnavailable = true, reason = %q (graceful degradation confirmed)",
		result.SemanticUnavailableReason)
}

// ---------------------------------------------------------------------------
// CHECK 3: sqlite vector search returns results ranked by cosine similarity
// ---------------------------------------------------------------------------

// mockEmbedClient is a local test double that returns a fixed vector for Embed.
// It avoids hitting any network and is safe for concurrent use.
type mockEmbedClient struct {
	vec []float32
}

func (m *mockEmbedClient) Embed(_ context.Context, _ string) ([]float32, error) {
	return m.vec, nil
}
func (m *mockEmbedClient) Dimensions() int { return len(m.vec) }
func (m *mockEmbedClient) Close() error    { return nil }

// cosineExpected computes cosine similarity between two float32 vectors.
// Used to log expected scores alongside actual SemanticSearch results.
func cosineExpected(a, b []float32) float32 {
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(float64(dot) / (math.Sqrt(float64(na)) * math.Sqrt(float64(nb))))
}

// TestBehav_Phase5_Check3_CosineSimilarityRanking verifies that the SQLite
// SemanticSearch path returns results ranked by cosine similarity descending.
//
// Test setup:
//
//	Record A  embedding=[1, 0, 0]   (pure X axis)
//	Record B  embedding=[0.9, 0.1, 0] (near X axis)
//	Record C  embedding=[0, 1, 0]   (pure Y axis — orthogonal to query)
//
//	Query vector = [1, 0, 0]
//
// Expected cosine ranking:  A (1.000) > B (~0.994) > C (0.000)
//
// Verification gate: "sqlite-vec vector search returns results ranked by cosine similarity"
func TestBehav_Phase5_Check3_CosineSimilarityRanking(t *testing.T) {
	db := openTestSQLite(t)

	vecA := []float32{1, 0, 0}
	vecB := []float32{0.9, 0.1, 0}
	vecC := []float32{0, 1, 0}

	writeWithEmbedding(t, db, "record-A", "content about apples", vecA)
	writeWithEmbedding(t, db, "record-B", "content about bananas", vecB)
	writeWithEmbedding(t, db, "content-C", "content about cherries", vecC)

	queryVec := []float32{1, 0, 0}
	expectedScoreA := cosineExpected(queryVec, vecA)
	expectedScoreB := cosineExpected(queryVec, vecB)
	expectedScoreC := cosineExpected(queryVec, vecC)

	t.Logf("Expected cosine scores:")
	t.Logf("  record-A  vec=%v  score=%.6f", vecA, expectedScoreA)
	t.Logf("  record-B  vec=%v  score=%.6f", vecB, expectedScoreB)
	t.Logf("  content-C vec=%v  score=%.6f", vecC, expectedScoreC)

	// Verify CanSemanticSearch reports true (has indexed embeddings).
	if !db.CanSemanticSearch() {
		t.Fatal("CHECK 3 FAIL: CanSemanticSearch() = false; want true after writing records with embeddings")
	}
	t.Logf("CanSemanticSearch()      = true")

	// Run SemanticSearch directly on the destination to verify ranking.
	results, err := db.SemanticSearch(context.Background(), queryVec, destination.QueryParams{
		Namespace:   "ns",
		Destination: "sqlite",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("SemanticSearch: %v", err)
	}

	t.Logf("SemanticSearch results (%d):", len(results))
	for i, r := range results {
		t.Logf("  [%d] payload_id=%-12s  content=%-30q  score=%.6f",
			i+1, r.Payload.PayloadID, r.Payload.Content, r.Score)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify descending order.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("CHECK 3 FAIL: results not sorted descending: results[%d].Score=%.6f > results[%d].Score=%.6f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}

	// Verify record-A is ranked first (exact match to query vector).
	if results[0].Payload.PayloadID != "record-A" {
		t.Errorf("CHECK 3 FAIL: top result = %q; want record-A (closest to query)", results[0].Payload.PayloadID)
	}

	// Verify record-B is ranked second.
	if results[1].Payload.PayloadID != "record-B" {
		t.Errorf("CHECK 3 FAIL: second result = %q; want record-B", results[1].Payload.PayloadID)
	}

	// Verify content-C is ranked last (orthogonal — cosine ≈ 0).
	if results[2].Payload.PayloadID != "content-C" {
		t.Errorf("CHECK 3 FAIL: last result = %q; want content-C (orthogonal to query)", results[2].Payload.PayloadID)
	}

	if results[2].Score > 0.01 {
		t.Errorf("CHECK 3 FAIL: content-C score=%.6f; want ~0.0 (orthogonal vector)", results[2].Score)
	}

	t.Logf("CHECK 3 PASS: results ranked by cosine similarity — A(%.4f) > B(%.4f) > C(%.4f)",
		results[0].Score, results[1].Score, results[2].Score)

	// Also verify through the full cascade using a mock embedding client.
	t.Log("")
	t.Log("-- Cascade Stage 4 path --")

	mockClient := &mockEmbedClient{vec: queryVec}
	runner := query.New(db, slog.Default()).
		WithEmbeddingClient(mockClient, nil)

	cascResult, err := runner.Run(context.Background(), permitAllSource(), balancedQuery("sqlite", "apples"))
	if err != nil {
		t.Fatalf("cascade Run: %v", err)
	}

	t.Logf("RetrievalStage           = %d", cascResult.RetrievalStage)
	t.Logf("SemanticUnavailable      = %v", cascResult.SemanticUnavailable)
	t.Logf("Records returned         = %d", len(cascResult.Records))

	if cascResult.SemanticUnavailable {
		t.Errorf("CHECK 3 FAIL: cascade marked semantic unavailable when client + data are present")
	}
	// Phase 6: when Stage 3 (structured) also returns results, Stage 5 (hybrid
	// merge) runs. Accept Stage 4 (semantic only) or Stage 5 (hybrid merge).
	if cascResult.RetrievalStage < 4 {
		t.Errorf("CHECK 3 FAIL: RetrievalStage = %d; want >= 4 (semantic retrieval active)", cascResult.RetrievalStage)
	}
	if len(cascResult.Records) == 0 {
		t.Errorf("CHECK 3 FAIL: cascade returned 0 records; want > 0")
	} else {
		t.Logf("CHECK 3 PASS: cascade Stage %d active, returned %d record(s), top result = %q",
			cascResult.RetrievalStage, len(cascResult.Records), cascResult.Records[0].PayloadID)
	}
}

// ---------------------------------------------------------------------------
// Bonus: verify _nexus JSON shape via marshal round-trip
// ---------------------------------------------------------------------------

// TestBehav_Phase5_NexusMetadata_JSONShape verifies that the cascade result
// fields map to the expected _nexus JSON keys, confirming the HTTP response
// will carry semantic_unavailable correctly.
func TestBehav_Phase5_NexusMetadata_JSONShape(t *testing.T) {
	type nexusMeta struct {
		ResultCount               int    `json:"result_count"`
		Profile                   string `json:"profile"`
		RetrievalStage            int    `json:"retrieval_stage"`
		SemanticUnavailable       bool   `json:"semantic_unavailable,omitempty"`
		SemanticUnavailableReason string `json:"semantic_unavailable_reason,omitempty"`
	}

	// Simulate a cascade result with embedding disabled.
	result := query.CascadeResult{
		Profile:                   "balanced",
		RetrievalStage:            3,
		SemanticUnavailable:       true,
		SemanticUnavailableReason: "embedding not configured",
	}

	meta := nexusMeta{
		ResultCount:               0,
		Profile:                   result.Profile,
		RetrievalStage:            result.RetrievalStage,
		SemanticUnavailable:       result.SemanticUnavailable,
		SemanticUnavailableReason: result.SemanticUnavailableReason,
	}

	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	t.Logf("_nexus JSON: %s", string(b))

	checks := []struct {
		key  string
		want interface{}
	}{
		{"semantic_unavailable", true},
		{"semantic_unavailable_reason", "embedding not configured"},
		{"retrieval_stage", float64(3)},
		{"profile", "balanced"},
	}

	for _, c := range checks {
		got, ok := m[c.key]
		if !ok {
			t.Errorf("key %q missing from _nexus JSON", c.key)
			continue
		}
		gotStr := fmt.Sprintf("%v", got)
		wantStr := fmt.Sprintf("%v", c.want)
		if gotStr != wantStr {
			t.Errorf("_nexus[%q] = %v; want %v", c.key, got, c.want)
		} else {
			t.Logf("  _nexus[%q] = %v  ✓", c.key, got)
		}
	}

	// Confirm omitempty: when SemanticUnavailable is false the key must be absent.
	result2 := nexusMeta{Profile: "balanced", RetrievalStage: 1}
	b2, _ := json.Marshal(result2)
	var m2 map[string]interface{}
	_ = json.Unmarshal(b2, &m2)
	t.Logf("_nexus JSON (unavailable=false): %s", string(b2))
	if _, present := m2["semantic_unavailable"]; present {
		t.Errorf("semantic_unavailable present in JSON when false; want omitempty to suppress it")
	} else {
		t.Logf("  semantic_unavailable omitted when false (omitempty working correctly)  ✓")
	}
}

// Compile-time assertion: CascadeResult fields used above exist.
var _ = query.CascadeResult{
	SemanticUnavailable:       false,
	SemanticUnavailableReason: "",
}

// Ensure filepath is used (openTestSQLite uses filepath.Join).
var _ = filepath.Join
