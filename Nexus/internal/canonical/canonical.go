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

// Package canonical implements the pre-substrate canonicalization pipeline
// for BF-Sketch. Every embedding is transformed through dimension
// normalization (SRHT), L2 normalization, and per-source whitening before
// sketch computation.
//
// The pipeline has five steps:
//  1. Dimension normalization via SRHT to canonical_d
//  2. L2 normalization (unit vector on the canonical_d-sphere)
//  3. Per-source anisotropy correction (diagonal whitening via Welford)
//  4. Metadata tagging
//  5. Substrate handoff
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.2.
package canonical

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/bubblefish-tech/nexus/internal/secrets"
)

// Metadata holds information about a canonicalized vector.
type Metadata struct {
	// Source is the originating source name.
	Source string

	// OriginalDim is the dimension of the input embedding before projection.
	OriginalDim int

	// SampleCount is the number of samples seen for this source's whitening.
	SampleCount int

	// WhiteningActive indicates whether whitening was applied (sample count
	// exceeded the warmup threshold).
	WhiteningActive bool
}

// Manager orchestrates the five-step canonicalization pipeline.
// A nil Manager is safe to use; all methods return ErrDisabled.
type Manager struct {
	cfg         Config
	seed        [32]byte
	srht        *SRHT
	whitening   map[string]*WhiteningState
	whiteningMu sync.RWMutex
	queryCache  *queryCache
	logger      *slog.Logger
}

// NewManager creates a Manager from the given config. Returns nil if
// canonical is disabled (nil is safe to use — all methods return ErrDisabled).
func NewManager(cfg Config) *Manager {
	if !cfg.Enabled {
		return nil
	}
	return &Manager{
		cfg:       cfg,
		whitening: make(map[string]*WhiteningState),
	}
}

// Init initializes the manager with a secrets directory and logger. Must be
// called before Canonicalize if the manager is non-nil. Returns an error if
// the seed cannot be loaded or the SRHT cannot be created.
func (m *Manager) Init(sd *secrets.Dir, logger *slog.Logger) error {
	if m == nil {
		return nil
	}
	m.logger = logger

	seed, err := LoadOrCreateSeed(sd)
	if err != nil {
		return fmt.Errorf("canonical init: load seed: %w", err)
	}
	m.seed = seed

	// SRHT from canonical_d to canonical_d. Input dimension is the next
	// power of 2 >= canonical_d (which is canonical_d itself, since
	// canonical_d must be a power of 2 per config validation).
	inputDim := NextPowerOfTwo(m.cfg.CanonicalDim)
	srht, err := NewSRHT(seed, inputDim, m.cfg.CanonicalDim)
	if err != nil {
		return fmt.Errorf("canonical init: srht: %w", err)
	}
	m.srht = srht

	ttl := time.Duration(m.cfg.QueryCacheTTLSeconds) * time.Second
	m.queryCache = newQueryCache(ttl)

	return nil
}

// Enabled reports whether the canonicalization pipeline is active.
func (m *Manager) Enabled() bool {
	if m == nil {
		return false
	}
	return m.cfg.Enabled
}

// Dim returns the configured canonical dimension, or 0 if the manager is nil.
func (m *Manager) Dim() int {
	if m == nil {
		return 0
	}
	return m.cfg.CanonicalDim
}

// Canonicalize runs the five-step pipeline on a raw embedding.
//
// Step 1: Dimension normalization via SRHT
// Step 2: L2 normalization
// Step 3: Per-source whitening (diagonal, Welford)
// Step 4: Metadata tagging
// Step 5: Substrate handoff (return)
func (m *Manager) Canonicalize(rawEmbedding []float64, source string) ([]float64, Metadata, error) {
	if m == nil {
		return nil, Metadata{}, ErrDisabled
	}

	// Step 1: Dimension normalization via SRHT
	canonical := make([]float64, m.cfg.CanonicalDim)
	padded := make([]float64, m.srht.inputDim)
	copy(padded, rawEmbedding) // zero-padded if shorter
	if err := m.srht.Apply(padded, canonical); err != nil {
		return nil, Metadata{}, fmt.Errorf("canonicalize srht: %w", err)
	}

	// Step 2: L2 normalization
	norm := L2Normalize(canonical)
	if norm == 0 {
		return nil, Metadata{}, ErrZeroVector
	}

	// Step 3: Per-source whitening
	m.whiteningMu.Lock()
	ws, ok := m.whitening[source]
	if !ok {
		ws = NewWhiteningState(m.cfg.CanonicalDim, m.cfg.WhiteningWarmup)
		m.whitening[source] = ws
	}
	m.whiteningMu.Unlock()

	// Update with the new sample first
	ws.Update(canonical)

	whitened := make([]float64, m.cfg.CanonicalDim)
	ws.Apply(canonical, whitened)

	// Re-normalize after whitening
	L2Normalize(whitened)

	// Step 4: Metadata tagging
	meta := Metadata{
		Source:          source,
		OriginalDim:     len(rawEmbedding),
		SampleCount:     ws.SampleCount(),
		WhiteningActive: ws.SampleCount() >= m.cfg.WhiteningWarmup,
	}

	// Step 5: Handoff
	return whitened, meta, nil
}

// CanonicalizeQuery is the query-path variant with caching.
func (m *Manager) CanonicalizeQuery(rawEmbedding []float64, source string) ([]float64, Metadata, error) {
	if m == nil {
		return nil, Metadata{}, ErrDisabled
	}

	key := queryCacheKey(rawEmbedding, source)
	if cached, ok := m.queryCache.get(key); ok {
		return cached.canonical, cached.meta, nil
	}

	canonical, meta, err := m.Canonicalize(rawEmbedding, source)
	if err != nil {
		return nil, meta, err
	}

	m.queryCache.put(key, canonical, meta)
	return canonical, meta, nil
}

// Shutdown persists whitening state and releases resources.
// Safe to call on nil.
func (m *Manager) Shutdown() error {
	if m == nil {
		return nil
	}
	// Whitening persistence deferred to v0.1.4; no-op for now.
	return nil
}

// queryCacheKey computes a SHA-256 hash from the embedding bytes and source.
func queryCacheKey(embedding []float64, source string) [32]byte {
	h := sha256.New()
	h.Write([]byte(source))
	h.Write([]byte{0}) // separator
	buf := make([]byte, 8)
	for _, v := range embedding {
		binary.LittleEndian.PutUint64(buf, math.Float64bits(v))
		h.Write(buf)
	}
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}

// queryCache is a simple TTL-based cache for canonicalized query vectors.
type queryCache struct {
	mu      sync.Mutex
	entries map[[32]byte]*queryCacheEntry
	ttl     time.Duration
}

type queryCacheEntry struct {
	canonical []float64
	meta      Metadata
	expiresAt time.Time
}

func newQueryCache(ttl time.Duration) *queryCache {
	return &queryCache{
		entries: make(map[[32]byte]*queryCacheEntry),
		ttl:     ttl,
	}
}

func (c *queryCache) get(key [32]byte) (*queryCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		if ok {
			delete(c.entries, key)
		}
		return nil, false
	}
	return e, true
}

func (c *queryCache) put(key [32]byte, canonical []float64, meta Metadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &queryCacheEntry{
		canonical: canonical,
		meta:      meta,
		expiresAt: time.Now().Add(c.ttl),
	}
}
