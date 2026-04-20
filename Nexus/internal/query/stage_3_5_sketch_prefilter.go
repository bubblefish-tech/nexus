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

package query

import (
	"log/slog"
	"sort"

	"github.com/bubblefish-tech/nexus/internal/canonical"
	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/substrate"
)

// SketchPrefilterConfig holds the dependencies for Stage 3.5.
// All fields are optional — when nil/disabled, Stage 3.5 is a no-op.
type SketchPrefilterConfig struct {
	Substrate    *substrate.Substrate
	Canonical    *canonical.Manager
	Logger       *slog.Logger
}

// stage35SketchPrefilter runs the BBQ sketch prefilter on a candidate set.
// Rule 18: runtime errors do NOT disable substrate globally. They fall
// through to Stage 4 with the original candidate set.
//
// Stage 3.5 activates when ALL of these conditions are true:
//   - Substrate is enabled
//   - Query has a numeric embedding
//   - Candidate set exceeds the prefilter threshold (default 200)
//   - At least 50% of candidates are in the cuckoo filter
//
// When any condition fails, the original candidates are returned unchanged.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.7.
func stage35SketchPrefilter(
	cfg *SketchPrefilterConfig,
	queryEmbedding []float32,
	records []destination.TranslatedPayload,
	threshold int,
	topK int,
) []destination.TranslatedPayload {
	// Short-circuit: substrate disabled or nil
	if cfg == nil || cfg.Substrate == nil || !cfg.Substrate.Enabled() {
		return records
	}
	// Short-circuit: no embedding in query
	if len(queryEmbedding) == 0 {
		return records
	}
	// Short-circuit: candidate set below threshold
	if len(records) <= threshold {
		return records
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Canonicalize the query embedding (with cache)
	if cfg.Canonical == nil || !cfg.Canonical.Enabled() {
		return records
	}
	// Convert float32 → float64 for canonicalization pipeline
	embF64 := make([]float64, len(queryEmbedding))
	for i, v := range queryEmbedding {
		embF64[i] = float64(v)
	}
	canonicalQ, _, err := cfg.Canonical.CanonicalizeQuery(embF64, "query")
	if err != nil {
		logger.Warn("stage 3.5: canonicalize query failed, falling through to stage 4",
			"component", "cascade", "error", err)
		return records
	}

	// Get current ratchet state.
	state := cfg.Substrate.CurrentRatchetState()
	if state == nil {
		logger.Warn("stage 3.5: no ratchet state, falling through to stage 4",
			"component", "cascade")
		return records
	}

	// Compute query sketch at current ratchet state.
	querySketch, err := substrate.ComputeQuerySketch(canonicalQ, state.StateBytes, state.StateID)
	if err != nil {
		logger.Warn("stage 3.5: compute query sketch failed, falling through to stage 4",
			"component", "cascade", "error", err)
		return records
	}

	// Check cuckoo filter coverage: Stage 3.5 only helps when ≥50% of
	// candidates have stored sketches. Below that fraction, ranking is too noisy.
	cuckooHits := 0
	for _, r := range records {
		if cfg.Substrate.CuckooLookup(r.PayloadID) {
			cuckooHits++
		}
	}
	if cuckooHits*2 < len(records) {
		return records
	}

	// Score each candidate via EstimateInnerProduct.
	// Candidates without sketches receive score 0 (neutral) — ranked below
	// positively-scored entries but kept in the result set.
	scored := make([]scoredCandidate, len(records))
	for i, r := range records {
		sc := scoredCandidate{record: r, score: 0}
		if cfg.Substrate.CuckooLookup(r.PayloadID) {
			storeSketch, loadErr := cfg.Substrate.LoadStoreSketch(r.PayloadID)
			if loadErr != nil {
				logger.Warn("stage 3.5: load sketch failed",
					"component", "cascade", "memory_id", r.PayloadID, "error", loadErr)
			} else if estimate, estErr := substrate.EstimateInnerProduct(storeSketch, querySketch); estErr == nil {
				sc.score = estimate
			}
		}
		scored[i] = sc
	}

	return rankAndTruncate(scored, topK)
}

// scoredCandidate pairs a record with its sketch inner-product estimate.
type scoredCandidate struct {
	record destination.TranslatedPayload
	score  float64
}

// rankAndTruncate sorts candidates by score descending and keeps top-K.
func rankAndTruncate(scored []scoredCandidate, topK int) []destination.TranslatedPayload {
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if len(scored) > topK {
		scored = scored[:topK]
	}
	result := make([]destination.TranslatedPayload, len(scored))
	for i, sc := range scored {
		result[i] = sc.record
	}
	return result
}
