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

// Phase 6 behavioral verification tests.
//
// These tests correspond directly to the four verification gate checks in the
// State Verification Guide Phase 6:
//
//	[ ] Full cascade: all 6 stages execute for balanced-profile query
//	[ ] Temporal decay: newer memory ranks higher than older contradiction
//	[ ] Determinism: same query twice = identical ranking
//	[ ] Dedup: same payload_id from Stages 3+4 appears once in results
package query_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/cache"
	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/query"
)

// ---------------------------------------------------------------------------
// Helpers shared by Phase 6 tests
// ---------------------------------------------------------------------------

// openTestSQLiteP6 opens a fresh SQLite DB in a test-unique temp dir.
func openTestSQLiteP6(t *testing.T) *destination.SQLiteDestination {
	t.Helper()
	dir := t.TempDir()
	db, err := destination.OpenSQLite(filepath.Join(dir, "test.db"), slog.Default())
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// permitAllSourceP6 returns a source that allows all operations and has the
// default semantic similarity threshold.
func permitAllSourceP6() *config.Source {
	return &config.Source{
		Name:      "test",
		Namespace: "ns",
		CanRead:   true,
		Policy: config.SourcePolicyConfig{
			Cache: config.PolicyCacheConfig{
				ReadFromCache:               true,
				WriteToCache:                true,
				SemanticSimilarityThreshold: 0.92,
			},
		},
	}
}

// sourceWithDecay returns a source with per-source decay configured.
func sourceWithDecay(halfLifeDays float64) *config.Source {
	src := permitAllSourceP6()
	src.Policy.Decay = config.PolicyDecayConfig{
		HalfLifeDays: halfLifeDays,
		DecayMode:    "exponential",
	}
	return src
}

// writeP6 inserts a payload with a given embedding and timestamp.
func writeP6(t *testing.T, db *destination.SQLiteDestination, id, content string, vec []float32, ts time.Time) {
	t.Helper()
	err := db.Write(destination.TranslatedPayload{
		PayloadID:   id,
		Source:      "test",
		Namespace:   "ns",
		Destination: "sqlite",
		Content:     content,
		Timestamp:   ts,
		Embedding:   vec,
	})
	if err != nil {
		t.Fatalf("Write(%s): %v", id, err)
	}
}

// fixedVecClient returns a mock embedding client that always returns the given vector.
type fixedVecClient struct{ vec []float32 }

func (f *fixedVecClient) Embed(_ context.Context, _ string) ([]float32, error) {
	return f.vec, nil
}
func (f *fixedVecClient) Dimensions() int { return len(f.vec) }
func (f *fixedVecClient) Close() error    { return nil }

// ---------------------------------------------------------------------------
// CHECK 1: Full cascade — all 6 stages execute for balanced-profile query
// ---------------------------------------------------------------------------

// TestBehav_Phase6_Check1_FullCascade_Stage5_Runs verifies that when both
// Stage 3 (structured lookup) and Stage 4 (semantic retrieval) produce results,
// Stage 5 (hybrid merge + temporal decay) executes and the cascade reports
// RetrievalStage = 5.
//
// Verification gate: "Full cascade: all 6 stages execute for balanced-profile query"
func TestBehav_Phase6_Check1_FullCascade_Stage5_Runs(t *testing.T) {
	db := openTestSQLiteP6(t)

	now := time.Now().UTC()
	vec := []float32{1, 0, 0}

	// Seed records that Stage 3 (text search) will also match.
	writeP6(t, db, "mem-A", "apple memory", vec, now.Add(-1*time.Hour))
	writeP6(t, db, "mem-B", "banana memory", []float32{0.9, 0.1, 0}, now.Add(-24*time.Hour))
	writeP6(t, db, "mem-C", "cherry memory", []float32{0.8, 0.2, 0}, now.Add(-72*time.Hour))

	if !db.CanSemanticSearch() {
		t.Fatal("precondition: CanSemanticSearch() = false; test setup broken")
	}

	// Use a "balanced" profile which enables Stages 0,1,2,3,4,5.
	mockClient := &fixedVecClient{vec: vec}
	sc := cache.NewSemanticCache(100, nil)
	src := permitAllSourceP6()

	globalRetrieval := config.RetrievalConfig{
		TimeDecay:    true,
		HalfLifeDays: 7,
		DecayMode:    "exponential",
	}

	runner := query.New(db, slog.Default()).
		WithEmbeddingClient(mockClient, nil).
		WithSemanticCache(sc).
		WithRetrievalConfig(globalRetrieval)

	cq := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Profile:     "balanced",
		Q:           "memory", // matches all records via structured search
		Limit:       10,
	}

	result, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	t.Logf("RetrievalStage           = %d", result.RetrievalStage)
	t.Logf("SemanticUnavailable      = %v", result.SemanticUnavailable)
	t.Logf("Records returned         = %d", len(result.Records))

	if result.RetrievalStage != 5 {
		t.Errorf("CHECK 1 FAIL: RetrievalStage = %d; want 5 (hybrid merge ran)", result.RetrievalStage)
	} else {
		t.Logf("CHECK 1 PASS: Stage 5 (hybrid merge) executed; RetrievalStage = 5")
	}

	if result.SemanticUnavailable {
		t.Errorf("CHECK 1 FAIL: SemanticUnavailable = true; semantic should be available")
	}
}

// ---------------------------------------------------------------------------
// CHECK 2: Temporal decay — newer memory ranks higher than older contradiction
// ---------------------------------------------------------------------------

// TestBehav_Phase6_Check2_TemporalDecay_NewerRanksHigher inserts two records
// with identical cosine similarity to the query (both point in the same direction)
// but different ages. The newer record must rank higher after temporal decay.
//
// Verification gate: "Temporal decay: newer memory ranks higher than older contradiction"
func TestBehav_Phase6_Check2_TemporalDecay_NewerRanksHigher(t *testing.T) {
	now := time.Now().UTC()

	// Both records have the same vector (cos_sim = 1.0 for both).
	vecSame := []float32{1, 0, 0}

	// Older record: 60 days old — heavily decayed with half_life = 7 days.
	older := destination.TranslatedPayload{
		PayloadID: "old-record",
		Content:   "The user prefers tea.",
		Timestamp: now.Add(-60 * 24 * time.Hour),
		Embedding: vecSame,
	}
	// Newer record: 1 day old — minimal decay.
	newer := destination.TranslatedPayload{
		PayloadID: "new-record",
		Content:   "The user now prefers coffee.",
		Timestamp: now.Add(-24 * time.Hour),
		Embedding: vecSame,
	}

	decayCfg := query.DecayConfig{
		Enabled:      true,
		HalfLifeDays: 7,
		Mode:         "exponential",
	}

	// Call HybridMerge directly to test decay reranking.
	stage3 := []destination.TranslatedPayload{older, newer}
	stage4 := []destination.ScoredRecord{
		{Payload: older, Score: 1.0},
		{Payload: newer, Score: 1.0},
	}

	merged := query.HybridMerge(stage3, stage4, 10, true, decayCfg, now)

	t.Logf("Merged results (%d):", len(merged))
	for i, r := range merged {
		age := now.Sub(r.Timestamp).Hours() / 24
		t.Logf("  [%d] payload_id=%-15s  age=%.1f days  content=%q",
			i+1, r.PayloadID, age, r.Content)
	}

	if len(merged) != 2 {
		t.Fatalf("expected 2 results, got %d", len(merged))
	}

	if merged[0].PayloadID != "new-record" {
		t.Errorf("CHECK 2 FAIL: top result = %q; want new-record (newer memory must rank higher)",
			merged[0].PayloadID)
	} else {
		t.Logf("CHECK 2 PASS: newer memory (new-record) ranks higher than older memory (old-record)")
	}
}

// ---------------------------------------------------------------------------
// CHECK 3: Determinism — same query twice = identical ranking
// ---------------------------------------------------------------------------

// TestBehav_Phase6_Check3_Determinism runs the same HybridMerge call twice
// and verifies the output ranking is identical both times.
//
// Verification gate: "Determinism: same query twice = identical ranking"
func TestBehav_Phase6_Check3_Determinism(t *testing.T) {
	now := time.Now().UTC()

	decayCfg := query.DecayConfig{
		Enabled:      true,
		HalfLifeDays: 7,
		Mode:         "exponential",
	}

	stage3 := []destination.TranslatedPayload{
		{PayloadID: "c", Timestamp: now.Add(-3 * 24 * time.Hour), Content: "gamma"},
		{PayloadID: "a", Timestamp: now.Add(-1 * 24 * time.Hour), Content: "alpha"},
		{PayloadID: "b", Timestamp: now.Add(-2 * 24 * time.Hour), Content: "beta"},
	}
	stage4 := []destination.ScoredRecord{
		{Payload: stage3[0], Score: 0.95},
		{Payload: stage3[1], Score: 0.90},
		{Payload: stage3[2], Score: 0.80},
	}

	run1 := query.HybridMerge(stage3, stage4, 10, true, decayCfg, now)
	run2 := query.HybridMerge(stage3, stage4, 10, true, decayCfg, now)

	if len(run1) != len(run2) {
		t.Fatalf("run1 len=%d, run2 len=%d; must be equal", len(run1), len(run2))
	}

	for i := range run1 {
		if run1[i].PayloadID != run2[i].PayloadID {
			t.Errorf("CHECK 3 FAIL: position %d: run1 = %q, run2 = %q (non-deterministic ranking)",
				i, run1[i].PayloadID, run2[i].PayloadID)
		}
	}

	t.Logf("CHECK 3 PASS: two runs produced identical %d-record ranking:", len(run1))
	for i, r := range run1 {
		t.Logf("  [%d] %s", i+1, r.PayloadID)
	}
}

// ---------------------------------------------------------------------------
// CHECK 4: Dedup — same payload_id from Stages 3+4 appears once in results
// ---------------------------------------------------------------------------

// TestBehav_Phase6_Check4_Dedup verifies that when the same payload_id appears
// in both Stage 3 (structured) and Stage 4 (semantic) results, it is present
// exactly once in the merged output.
//
// Verification gate: "Dedup: same payload_id from Stages 3+4 appears once in results"
func TestBehav_Phase6_Check4_Dedup(t *testing.T) {
	now := time.Now().UTC()

	payload := destination.TranslatedPayload{
		PayloadID: "shared-id",
		Content:   "shared memory",
		Timestamp: now.Add(-1 * 24 * time.Hour),
	}
	onlyStage3 := destination.TranslatedPayload{
		PayloadID: "stage3-only",
		Content:   "structured only",
		Timestamp: now.Add(-2 * 24 * time.Hour),
	}
	onlyStage4 := destination.TranslatedPayload{
		PayloadID: "stage4-only",
		Content:   "semantic only",
		Timestamp: now.Add(-3 * 24 * time.Hour),
	}

	stage3 := []destination.TranslatedPayload{payload, onlyStage3}
	stage4 := []destination.ScoredRecord{
		{Payload: payload, Score: 0.99},
		{Payload: onlyStage4, Score: 0.85},
	}

	merged := query.HybridMerge(stage3, stage4, 10, false, query.DecayConfig{}, now)

	t.Logf("Merged results (%d):", len(merged))
	for i, r := range merged {
		t.Logf("  [%d] payload_id=%s", i+1, r.PayloadID)
	}

	if len(merged) != 3 {
		t.Errorf("CHECK 4 FAIL: got %d records; want 3 (shared-id must appear once)", len(merged))
	}

	seen := make(map[string]int)
	for _, r := range merged {
		seen[r.PayloadID]++
	}

	if seen["shared-id"] != 1 {
		t.Errorf("CHECK 4 FAIL: shared-id appears %d times; want exactly 1", seen["shared-id"])
	} else {
		t.Logf("CHECK 4 PASS: shared-id appears exactly once in merged output")
	}

	if seen["stage3-only"] != 1 {
		t.Errorf("CHECK 4 FAIL: stage3-only appears %d times; want 1", seen["stage3-only"])
	}
	if seen["stage4-only"] != 1 {
		t.Errorf("CHECK 4 FAIL: stage4-only appears %d times; want 1", seen["stage4-only"])
	}
}

// ---------------------------------------------------------------------------
// Bonus: Semantic cache short-circuit — Stage 2 hit skips Stages 3–5
// ---------------------------------------------------------------------------

// TestBehav_Phase6_SemanticCacheShortCircuit verifies that after a first query
// populates the semantic cache, a semantically identical second query hits
// Stage 2 and does not proceed to Stages 3–5.
//
// Reference: Tech Spec Section 3.7 — Semantic Short-Circuit.
func TestBehav_Phase6_SemanticCacheShortCircuit(t *testing.T) {
	db := openTestSQLiteP6(t)

	now := time.Now().UTC()
	vec := []float32{1, 0, 0}
	writeP6(t, db, "r1", "apple memory", vec, now.Add(-1*time.Hour))
	writeP6(t, db, "r2", "banana memory", []float32{0.9, 0.1, 0}, now.Add(-24*time.Hour))

	if !db.CanSemanticSearch() {
		t.Fatal("precondition: CanSemanticSearch() = false")
	}

	sc := cache.NewSemanticCache(100, nil)
	src := permitAllSourceP6()
	mockClient := &fixedVecClient{vec: vec}

	globalRetrieval := config.RetrievalConfig{
		TimeDecay:    true,
		HalfLifeDays: 7,
		DecayMode:    "exponential",
	}

	runner := query.New(db, slog.Default()).
		WithEmbeddingClient(mockClient, nil).
		WithSemanticCache(sc).
		WithRetrievalConfig(globalRetrieval)

	cq := query.CanonicalQuery{
		Destination: "sqlite",
		Namespace:   "ns",
		Profile:     "balanced",
		Q:           "memory",
		Limit:       10,
	}

	// First query — populates the semantic cache.
	r1, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	t.Logf("First query: RetrievalStage = %d, records = %d", r1.RetrievalStage, len(r1.Records))
	if r1.RetrievalStage < 3 {
		t.Fatalf("first query should use Stage 3+ not Stage %d", r1.RetrievalStage)
	}

	// Second query — should hit Stage 2 (semantic cache).
	r2, err := runner.Run(context.Background(), src, cq)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	t.Logf("Second query: RetrievalStage = %d, records = %d", r2.RetrievalStage, len(r2.Records))

	if r2.RetrievalStage != 2 {
		t.Errorf("BONUS FAIL: second query RetrievalStage = %d; want 2 (semantic cache hit)",
			r2.RetrievalStage)
	} else {
		t.Logf("BONUS PASS: semantic cache short-circuited on second query (Stage 2 hit)")
	}
}

// ---------------------------------------------------------------------------
// Bonus: Decay config resolution — tiered precedence
// ---------------------------------------------------------------------------

// TestBehav_Phase6_DecayResolution_PerSourceOverridesGlobal verifies that a
// per-source HalfLifeDays setting overrides the global RetrievalConfig value.
func TestBehav_Phase6_DecayResolution_PerSourceOverridesGlobal(t *testing.T) {
	global := config.RetrievalConfig{
		TimeDecay:    true,
		HalfLifeDays: 7,
		DecayMode:    "exponential",
	}
	srcDecay := config.PolicyDecayConfig{
		HalfLifeDays: 30, // per-source override
	}

	cfg := query.ResolveDecay(global, srcDecay, "balanced")

	if !cfg.Enabled {
		t.Error("expected decay enabled, got disabled")
	}
	if cfg.HalfLifeDays != 30 {
		t.Errorf("HalfLifeDays = %g; want 30 (per-source override)", cfg.HalfLifeDays)
	}
	t.Logf("PASS: per-source HalfLifeDays=30 overrides global HalfLifeDays=7")
}

// TestBehav_Phase6_DecayResolution_GlobalOnly uses global config with no per-source override.
func TestBehav_Phase6_DecayResolution_GlobalOnly(t *testing.T) {
	global := config.RetrievalConfig{
		TimeDecay:    true,
		HalfLifeDays: 14,
		DecayMode:    "exponential",
	}

	cfg := query.ResolveDecay(global, config.PolicyDecayConfig{}, "balanced")

	if !cfg.Enabled {
		t.Error("expected decay enabled")
	}
	if cfg.HalfLifeDays != 14 {
		t.Errorf("HalfLifeDays = %g; want 14", cfg.HalfLifeDays)
	}
}

// TestBehav_Phase6_DecayResolution_Disabled verifies that decay is disabled
// when TimeDecay = false and no per-source override is set.
func TestBehav_Phase6_DecayResolution_Disabled(t *testing.T) {
	global := config.RetrievalConfig{TimeDecay: false}
	cfg := query.ResolveDecay(global, config.PolicyDecayConfig{}, "balanced")
	if cfg.Enabled {
		t.Error("expected decay disabled when TimeDecay = false and no per-source override")
	}
}

// TestBehav_Phase6_DecayResolution_ProfileDefault verifies that when HalfLifeDays
// is 0 (unset) but TimeDecay is true, the profile-specific default is used.
func TestBehav_Phase6_DecayResolution_ProfileDefault(t *testing.T) {
	global := config.RetrievalConfig{TimeDecay: true, HalfLifeDays: 0}

	balanced := query.ResolveDecay(global, config.PolicyDecayConfig{}, "balanced")
	if balanced.HalfLifeDays != 7 {
		t.Errorf("balanced profile default HalfLifeDays = %g; want 7", balanced.HalfLifeDays)
	}

	deep := query.ResolveDecay(global, config.PolicyDecayConfig{}, "deep")
	if deep.HalfLifeDays != 30 {
		t.Errorf("deep profile default HalfLifeDays = %g; want 30", deep.HalfLifeDays)
	}
}
