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
	"sort"
	"time"

	"github.com/BubbleFish-Nexus/internal/destination"
)

// neutralCosSim is the cosine similarity assigned to Stage 3-only records
// (structured lookup results that did not appear in Stage 4 semantic results).
// A neutral value of 0.5 ensures they are competitive with low-scoring semantic
// results while not dominating fresh, high-similarity semantic results.
const neutralCosSim = 0.5

// hybridCandidate is an internal record used during Stage 5 hybrid merge.
// It pairs a payload with its cosine similarity score (from Stage 4, or neutral
// for Stage 3-only records).
type hybridCandidate struct {
	payload destination.TranslatedPayload
	cosSim  float64
}

// HybridMerge implements Stage 5 of the 6-stage retrieval cascade:
// deduplication, temporal decay reranking, and trimming to maxResults.
//
// It accepts Stage 3 results (structured lookup, no cosine score) and Stage 4
// results (semantic retrieval, with cosine score from SemanticSearch). Records
// are deduplicated by payload_id; Stage 4's cosine score takes precedence over
// the neutral score for any record appearing in both sets.
//
// When decayEnabled is true, temporal decay is applied using decayCfg, and
// records are ranked by final_score = (cos_sim * 0.7) + (recency_weight * 0.3).
// When decayEnabled is false, records are ranked by cosine similarity alone.
//
// Ranking is deterministic: ties are broken by payload_id (lexicographic
// ascending) so the same inputs always produce the same ordering.
//
// maxResults caps the output. Pass 0 to return all merged records.
//
// Reference: Tech Spec Section 3.4 — Stage 5, Section 3.6.
func HybridMerge(
	stage3 []destination.TranslatedPayload,
	stage4 []destination.ScoredRecord,
	maxResults int,
	decayEnabled bool,
	decayCfg DecayConfig,
	now time.Time,
) []destination.TranslatedPayload {
	// Build a map from payload_id → candidate.
	// Stage 4 results are inserted first so their cosine scores win on dedup.
	merged := make(map[string]*hybridCandidate, len(stage4)+len(stage3))

	for i := range stage4 {
		sr := &stage4[i]
		if sr.Payload.PayloadID == "" {
			continue
		}
		if _, exists := merged[sr.Payload.PayloadID]; !exists {
			merged[sr.Payload.PayloadID] = &hybridCandidate{
				payload: sr.Payload,
				cosSim:  float64(sr.Score),
			}
		}
	}

	for i := range stage3 {
		tp := &stage3[i]
		if tp.PayloadID == "" {
			continue
		}
		if _, exists := merged[tp.PayloadID]; !exists {
			merged[tp.PayloadID] = &hybridCandidate{
				payload: *tp,
				cosSim:  neutralCosSim,
			}
		}
	}

	// Build flat slice of scored candidates.
	type scoredCandidate struct {
		payload    destination.TranslatedPayload
		finalScore float64
	}
	candidates := make([]scoredCandidate, 0, len(merged))

	for _, c := range merged {
		var fs float64
		if decayEnabled {
			daysElapsed := now.Sub(c.payload.Timestamp).Hours() / 24
			if daysElapsed < 0 {
				daysElapsed = 0
			}
			fs = FinalScore(c.cosSim, daysElapsed, decayCfg)
		} else {
			fs = c.cosSim
		}
		candidates = append(candidates, scoredCandidate{
			payload:    c.payload,
			finalScore: fs,
		})
	}

	// Sort descending by finalScore. Tiebreak by PayloadID ascending to ensure
	// deterministic output for identical scores.
	//
	// INVARIANT: Same config + data = same ranking (Tech Spec Section 3.6).
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].finalScore != candidates[j].finalScore {
			return candidates[i].finalScore > candidates[j].finalScore
		}
		return candidates[i].payload.PayloadID < candidates[j].payload.PayloadID
	})

	// Trim to maxResults.
	if maxResults > 0 && len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}

	// Extract payloads.
	out := make([]destination.TranslatedPayload, len(candidates))
	for i := range candidates {
		out[i] = candidates[i].payload
	}
	return out
}
