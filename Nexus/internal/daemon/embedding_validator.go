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

package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"
)

// QuarantineRecord holds information about a quarantined memory entry.
// Quarantine is triggered when the embedding validator detects drift anomalies.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.5.
type QuarantineRecord struct {
	MemoryID        string    `json:"memory_id"`
	Source          string    `json:"source"`
	Provider        string    `json:"provider"`
	QuarantinedAt   time.Time `json:"quarantined_at"`
	Reason          string    `json:"reason"`
	ContentHash     string    `json:"content_hash"`
	NormValue       float64   `json:"norm_value"`
	BaselineMean    float64   `json:"baseline_mean"`
	BaselineStdDev  float64   `json:"baseline_stddev"`
	SigmaDeviation  float64   `json:"sigma_deviation"`
}

// providerBaseline tracks the running statistics for embedding L2 norms
// from a single provider. Used for drift detection.
type providerBaseline struct {
	mu       sync.Mutex
	provider string
	count    int
	// Welford's online algorithm: M1 = running mean, M2 = running sum of
	// squared deviations. stddev = sqrt(M2 / count) when count > 1.
	mean float64
	m2   float64
}

func (pb *providerBaseline) update(norm float64) (mean, stddev float64, n int) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.count++
	delta := norm - pb.mean
	pb.mean += delta / float64(pb.count)
	delta2 := norm - pb.mean
	pb.m2 += delta * delta2
	var sd float64
	if pb.count > 1 {
		sd = math.Sqrt(pb.m2 / float64(pb.count-1))
	}
	return pb.mean, sd, pb.count
}

func (pb *providerBaseline) stats() (mean, stddev float64, n int) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	var sd float64
	if pb.count > 1 {
		sd = math.Sqrt(pb.m2 / float64(pb.count-1))
	}
	return pb.mean, sd, pb.count
}

// embeddingValidator validates embedding envelopes written into Nexus.
// It performs:
//   1. Shape check: verify len(embedding) == configured dimensions
//   2. Content-hash integrity: SHA-256 of the content text is stored as metadata
//   3. Provider-identity stamping: records which provider generated the embedding
//   4. Drift detection: 3-sigma threshold on L2 norm per provider
//   5. Fresh baseline: first 1000 embeddings per provider never trigger alarms
//
// Quarantine state is held in memory and also written to the WAL as
// EntryTypeQuarantine for durability across restarts.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.5.
type embeddingValidator struct {
	mu         sync.RWMutex
	baselines  map[string]*providerBaseline // key: provider name
	quarantine map[string]*QuarantineRecord // key: memory ID

	// configuredDimensions is the expected embedding dimension. 0 = skip check.
	configuredDimensions int
	// baselineWarmupCount is the number of embeddings per provider before
	// drift detection activates. Default 1000.
	baselineWarmupCount int
	// sigmaThreshold is the number of standard deviations that triggers
	// quarantine. Default 3.0.
	sigmaThreshold float64
}

// newEmbeddingValidator creates a validator with the given configured dimensions.
// dimensions=0 skips shape checking. warmupCount and sigmaThreshold use defaults
// if zero.
func newEmbeddingValidator(dimensions, warmupCount int, sigmaThreshold float64) *embeddingValidator {
	if warmupCount <= 0 {
		warmupCount = 1000
	}
	if sigmaThreshold <= 0 {
		sigmaThreshold = 3.0
	}
	return &embeddingValidator{
		baselines:            make(map[string]*providerBaseline),
		quarantine:           make(map[string]*QuarantineRecord),
		configuredDimensions: dimensions,
		baselineWarmupCount:  warmupCount,
		sigmaThreshold:       sigmaThreshold,
	}
}

// ValidationResult is returned by Validate.
type ValidationResult struct {
	// ContentHash is the SHA-256 of the content text, hex-encoded.
	ContentHash string
	// Provider is the provider name stamped on the embedding.
	Provider string
	// Quarantined is true if this embedding triggered drift detection.
	Quarantined bool
	// QuarantineReason explains why the embedding was quarantined.
	QuarantineReason string
	// Err is non-nil if validation failed (shape mismatch, etc.).
	Err error
}

// Validate validates an embedding vector against the envelope checks.
// On success it returns a ValidationResult. A quarantined embedding is
// still considered valid (Err == nil) but Quarantined == true.
func (v *embeddingValidator) Validate(memoryID, source, provider, content string, embedding []float32) ValidationResult {
	var result ValidationResult

	// 1. Content-hash integrity.
	h := sha256.Sum256([]byte(content))
	result.ContentHash = hex.EncodeToString(h[:])

	// 2. Provider-identity stamping.
	result.Provider = provider

	// 3. Shape check.
	if v.configuredDimensions > 0 && len(embedding) != v.configuredDimensions {
		result.Err = fmt.Errorf("embedding validator: shape mismatch: got %d dims, want %d", len(embedding), v.configuredDimensions)
		return result
	}

	if len(embedding) == 0 {
		return result // no embedding — nothing to validate further
	}

	// 4. Compute L2 norm.
	var sumSq float64
	for _, x := range embedding {
		sumSq += float64(x) * float64(x)
	}
	norm := math.Sqrt(sumSq)

	// 5. Drift detection with 3-sigma threshold and warmup period.
	baseline := v.getOrCreateBaseline(provider)
	mean, stddev, count := baseline.update(norm)

	if count > v.baselineWarmupCount && stddev > 0 {
		deviation := math.Abs(norm-mean) / stddev
		if deviation > v.sigmaThreshold {
			result.Quarantined = true
			result.QuarantineReason = fmt.Sprintf("%.2f sigma deviation (threshold %.1f)", deviation, v.sigmaThreshold)
			v.addToQuarantine(memoryID, source, provider, result.ContentHash, norm, mean, stddev, deviation)
		}
	}

	return result
}

func (v *embeddingValidator) getOrCreateBaseline(provider string) *providerBaseline {
	v.mu.Lock()
	defer v.mu.Unlock()
	if b, ok := v.baselines[provider]; ok {
		return b
	}
	b := &providerBaseline{provider: provider}
	v.baselines[provider] = b
	return b
}

func (v *embeddingValidator) addToQuarantine(memoryID, source, provider, contentHash string, norm, mean, stddev, sigma float64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.quarantine[memoryID] = &QuarantineRecord{
		MemoryID:       memoryID,
		Source:         source,
		Provider:       provider,
		QuarantinedAt:  time.Now().UTC(),
		Reason:         fmt.Sprintf("embedding drift: %.2f sigma deviation", sigma),
		ContentHash:    contentHash,
		NormValue:      norm,
		BaselineMean:   mean,
		BaselineStdDev: stddev,
		SigmaDeviation: sigma,
	}
}

// QuarantinedIDs returns a snapshot of all quarantined memory IDs.
func (v *embeddingValidator) QuarantinedIDs() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	ids := make([]string, 0, len(v.quarantine))
	for id := range v.quarantine {
		ids = append(ids, id)
	}
	return ids
}

// QuarantineRecord returns the quarantine record for a specific memory ID.
// Returns (nil, false) if the ID is not in quarantine.
func (v *embeddingValidator) QuarantineRecord(memoryID string) (interface{}, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	rec, ok := v.quarantine[memoryID]
	return rec, ok
}
