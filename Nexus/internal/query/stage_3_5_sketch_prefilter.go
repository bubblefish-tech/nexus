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

	// Get current ratchet state
	// TODO(shawn): wire ratchet access through Substrate.CurrentRatchetState()
	// For now, Stage 3.5 is structurally complete but returns records unchanged
	// until the full Substrate coordinator (BS.2+) exposes the ratchet and
	// sketch store via public methods.
	_ = canonicalQ

	// Stage 3.5 structural placeholder: when fully wired, this will:
	// 1. Check cuckoo filter for each candidate
	// 2. Compute query sketch at current ratchet state
	// 3. Score each candidate by EstimateInnerProduct
	// 4. Sort by score descending, keep top-K
	// 5. Return reduced candidate set

	// For now, return all candidates (Stage 3.5 is a no-op until the
	// Substrate coordinator exposes sketch loading).
	return records
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
