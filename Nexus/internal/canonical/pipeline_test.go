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

package canonical

import (
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/secrets"
)

// newTestManager creates a fully initialized Manager for testing.
func newTestManager(t *testing.T, dim int) *Manager {
	t.Helper()
	cfg := Config{
		Enabled:              true,
		CanonicalDim:         dim,
		WhiteningWarmup:      100,
		QueryCacheTTLSeconds: 2,
	}
	m := NewManager(cfg)
	if m == nil {
		t.Fatal("expected non-nil manager for enabled config")
	}

	tmpDir := t.TempDir()
	sd, err := secrets.Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Init(sd, slog.Default()); err != nil {
		t.Fatal(err)
	}
	return m
}

// ─── Full Pipeline Tests ────────────────────────────────────────────────────

func TestManagerInitSuccess(t *testing.T) {
	m := newTestManager(t, 64)
	if !m.Enabled() {
		t.Fatal("manager should be enabled after init")
	}
	if m.srht == nil {
		t.Fatal("SRHT should be initialized")
	}
	if m.queryCache == nil {
		t.Fatal("queryCache should be initialized")
	}
}

func TestCanonicalizeFullPipeline(t *testing.T) {
	m := newTestManager(t, 64)

	// Create a 128-dim embedding (will be projected to 64)
	embedding := make([]float64, 128)
	for i := range embedding {
		embedding[i] = float64(i+1) * 0.01
	}

	canonical, meta, err := m.Canonicalize(embedding, "test-source")
	if err != nil {
		t.Fatal(err)
	}

	// Verify output dimension
	if len(canonical) != 64 {
		t.Fatalf("expected 64-dim output, got %d", len(canonical))
	}

	// Verify output is unit-normalized
	norm := KahanL2Norm(canonical)
	if math.Abs(norm-1.0) > 1e-10 {
		t.Fatalf("output should be unit-normalized, got norm=%v", norm)
	}

	// Verify metadata
	if meta.Source != "test-source" {
		t.Fatalf("expected source 'test-source', got %q", meta.Source)
	}
	if meta.OriginalDim != 128 {
		t.Fatalf("expected original_dim=128, got %d", meta.OriginalDim)
	}
	if meta.SampleCount != 1 {
		t.Fatalf("expected sample_count=1, got %d", meta.SampleCount)
	}
	if meta.WhiteningActive {
		t.Fatal("whitening should not be active with 1 sample (warmup=100)")
	}
}

func TestCanonicalizeZeroVector(t *testing.T) {
	m := newTestManager(t, 64)
	embedding := make([]float64, 64)
	_, _, err := m.Canonicalize(embedding, "test")
	if err != ErrZeroVector {
		t.Fatalf("expected ErrZeroVector, got %v", err)
	}
}

func TestCanonicalizeDeterminism(t *testing.T) {
	m := newTestManager(t, 64)
	embedding := []float64{1, 2, 3, 4, 5, 6, 7, 8}

	out1, _, err := m.Canonicalize(embedding, "src-a")
	if err != nil {
		t.Fatal(err)
	}

	// Create a second manager with the same seed
	m2 := newTestManager(t, 64)
	// Copy the seed so they match
	m2.seed = m.seed
	m2.srht, _ = NewSRHT(m.seed, m.srht.inputDim, m.cfg.CanonicalDim)
	m2.whitening = make(map[string]*WhiteningState) // fresh whitening

	out2, _, err := m2.Canonicalize(embedding, "src-a")
	if err != nil {
		t.Fatal(err)
	}

	for i := range out1 {
		if out1[i] != out2[i] {
			t.Fatalf("not deterministic at index %d: %v != %v", i, out1[i], out2[i])
		}
	}
}

func TestCanonicalizeMultipleSources(t *testing.T) {
	m := newTestManager(t, 64)
	embedding := make([]float64, 64)
	for i := range embedding {
		embedding[i] = float64(i+1) * 0.1
	}

	_, metaA, err := m.Canonicalize(embedding, "source-a")
	if err != nil {
		t.Fatal(err)
	}
	_, metaB, err := m.Canonicalize(embedding, "source-b")
	if err != nil {
		t.Fatal(err)
	}

	if metaA.Source != "source-a" || metaB.Source != "source-b" {
		t.Fatal("source names not recorded correctly")
	}

	// Verify separate whitening states
	m.whiteningMu.RLock()
	defer m.whiteningMu.RUnlock()
	if len(m.whitening) != 2 {
		t.Fatalf("expected 2 whitening states, got %d", len(m.whitening))
	}
}

func TestCanonicalizeSmallEmbedding(t *testing.T) {
	// Embedding smaller than canonical_dim should work (zero-padded)
	m := newTestManager(t, 64)
	embedding := []float64{1, 2, 3}

	canonical, meta, err := m.Canonicalize(embedding, "small")
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 64 {
		t.Fatalf("expected 64-dim, got %d", len(canonical))
	}
	if meta.OriginalDim != 3 {
		t.Fatalf("expected original_dim=3, got %d", meta.OriginalDim)
	}
	norm := KahanL2Norm(canonical)
	if math.Abs(norm-1.0) > 1e-10 {
		t.Fatalf("should be unit-normalized, got norm=%v", norm)
	}
}

func TestCanonicalizeLargeEmbedding(t *testing.T) {
	// Embedding larger than canonical_dim: zero-padded to next power of 2
	m := newTestManager(t, 64)
	embedding := make([]float64, 3072) // OpenAI 3072-dim
	for i := range embedding {
		embedding[i] = float64(i%100) * 0.001
	}

	canonical, meta, err := m.Canonicalize(embedding, "openai")
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 64 {
		t.Fatalf("expected 64-dim, got %d", len(canonical))
	}
	if meta.OriginalDim != 3072 {
		t.Fatalf("expected original_dim=3072, got %d", meta.OriginalDim)
	}
}

func TestCanonicalizeWhiteningEngages(t *testing.T) {
	cfg := Config{
		Enabled:              true,
		CanonicalDim:         64,
		WhiteningWarmup:      10, // low warmup for test speed
		QueryCacheTTLSeconds: 60,
	}
	m := NewManager(cfg)
	tmpDir := t.TempDir()
	sd, err := secrets.Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Init(sd, slog.Default()); err != nil {
		t.Fatal(err)
	}

	// Feed 15 samples (above warmup=10)
	for i := 0; i < 15; i++ {
		emb := make([]float64, 64)
		for j := range emb {
			emb[j] = float64(i*64+j) * 0.001
		}
		m.Canonicalize(emb, "test")
	}

	// Next canonicalization should have whitening active
	emb := make([]float64, 64)
	for j := range emb {
		emb[j] = float64(j) * 0.01
	}
	_, meta, err := m.Canonicalize(emb, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !meta.WhiteningActive {
		t.Fatal("whitening should be active after 16 samples (warmup=10)")
	}
}

func TestCanonicalizeConcurrency(t *testing.T) {
	m := newTestManager(t, 64)
	var wg sync.WaitGroup
	errs := make(chan error, 100)

	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				emb := make([]float64, 64)
				for j := range emb {
					emb[j] = float64(g*100+i*10+j) * 0.01
				}
				_, _, err := m.Canonicalize(emb, "concurrent-src")
				if err != nil {
					errs <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent canonicalize error: %v", err)
	}
}

// ─── Query Cache Tests ──────────────────────────────────────────────────────

func TestCanonicalizeQueryCacheHit(t *testing.T) {
	m := newTestManager(t, 64)
	embedding := make([]float64, 64)
	for i := range embedding {
		embedding[i] = float64(i+1) * 0.1
	}

	out1, _, err := m.CanonicalizeQuery(embedding, "src")
	if err != nil {
		t.Fatal(err)
	}
	out2, _, err := m.CanonicalizeQuery(embedding, "src")
	if err != nil {
		t.Fatal(err)
	}

	// Cached result should be identical (same slice data)
	for i := range out1 {
		if out1[i] != out2[i] {
			t.Fatalf("cache miss at %d: %v != %v", i, out1[i], out2[i])
		}
	}
}

func TestCanonicalizeQueryDifferentSources(t *testing.T) {
	m := newTestManager(t, 64)
	embedding := make([]float64, 64)
	for i := range embedding {
		embedding[i] = float64(i+1) * 0.1
	}

	out1, _, _ := m.CanonicalizeQuery(embedding, "src-a")
	out2, _, _ := m.CanonicalizeQuery(embedding, "src-b")

	// Different sources should produce different cache keys
	// (different source string in hash). The first call computes; the
	// second for a different source also computes.
	key1 := queryCacheKey(embedding, "src-a")
	key2 := queryCacheKey(embedding, "src-b")
	if key1 == key2 {
		t.Fatal("different sources should produce different cache keys")
	}
	_ = out1
	_ = out2
}

func TestQueryCacheExpiration(t *testing.T) {
	cfg := Config{
		Enabled:              true,
		CanonicalDim:         64,
		WhiteningWarmup:      1000,
		QueryCacheTTLSeconds: 1, // 1 second TTL
	}
	m := NewManager(cfg)
	tmpDir := t.TempDir()
	sd, _ := secrets.Open(tmpDir)
	m.Init(sd, slog.Default())

	embedding := make([]float64, 64)
	for i := range embedding {
		embedding[i] = float64(i) * 0.1
	}

	key := queryCacheKey(embedding, "test")

	// Put entry in cache
	m.queryCache.put(key, make([]float64, 64), Metadata{Source: "test"})

	// Should be there immediately
	if _, ok := m.queryCache.get(key); !ok {
		t.Fatal("entry should be in cache immediately")
	}

	// Wait for expiration
	time.Sleep(1500 * time.Millisecond)

	// Should be gone
	if _, ok := m.queryCache.get(key); ok {
		t.Fatal("expired entry should be evicted")
	}
}

func TestQueryCacheKeyDeterminism(t *testing.T) {
	emb := []float64{1.0, 2.0, 3.0}
	k1 := queryCacheKey(emb, "src")
	k2 := queryCacheKey(emb, "src")
	if k1 != k2 {
		t.Fatal("same inputs should produce same key")
	}
}

func TestQueryCacheKeyUniqueness(t *testing.T) {
	emb1 := []float64{1.0, 2.0, 3.0}
	emb2 := []float64{1.0, 2.0, 3.0001}
	k1 := queryCacheKey(emb1, "src")
	k2 := queryCacheKey(emb2, "src")
	if k1 == k2 {
		t.Fatal("slightly different embeddings should produce different keys")
	}
}

// ─── Seed Tests ─────────────────────────────────────────────────────────────

func TestLoadOrCreateSeedCreatesThenLoads(t *testing.T) {
	tmpDir := t.TempDir()
	sd, err := secrets.Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// First call: creates seed
	seed1, err := LoadOrCreateSeed(sd)
	if err != nil {
		t.Fatal(err)
	}
	// Seed should not be all zeros
	allZero := true
	for _, b := range seed1 {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("created seed should not be all zeros")
	}

	// Second call: loads existing seed
	seed2, err := LoadOrCreateSeed(sd)
	if err != nil {
		t.Fatal(err)
	}
	if seed1 != seed2 {
		t.Fatal("loaded seed should match created seed")
	}
}

func TestLoadOrCreateSeedCorruptSize(t *testing.T) {
	tmpDir := t.TempDir()
	sd, err := secrets.Open(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Write a seed file with wrong size
	if err := sd.WriteSecret(seedFileName, []byte("too-short")); err != nil {
		t.Fatal(err)
	}

	// Should create a new seed (replacing the corrupt one)
	seed, err := LoadOrCreateSeed(sd)
	if err != nil {
		t.Fatal(err)
	}
	if seed == [32]byte{} {
		t.Fatal("should have created new seed for corrupt file")
	}

	// The saved seed should now be 32 bytes
	data, err := sd.ReadSecret(seedFileName)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 32 {
		t.Fatalf("seed file should be 32 bytes, got %d", len(data))
	}
}
