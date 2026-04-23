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

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// RRFMerge performs Reciprocal Rank Fusion on dense (vector) and sparse (BM25)
// result lists. k is the RRF constant (standard: 60). Returns a merged list
// sorted by combined RRF score (highest first).
//
// Formula: rrf_score(d) = Σ 1/(k + rank_i(d))
// Where rank_i is the 1-based rank of document d in list i.
func RRFMerge(denseResults []destination.ScoredRecord, bm25Results []BM25Result, k int) []destination.TranslatedPayload {
	if k <= 0 {
		k = 60
	}

	type fusionEntry struct {
		payload destination.TranslatedPayload
		score   float64
	}

	scores := make(map[string]*fusionEntry)

	for rank, sr := range denseResults {
		id := sr.Payload.PayloadID
		if id == "" {
			continue
		}
		if _, ok := scores[id]; !ok {
			scores[id] = &fusionEntry{payload: sr.Payload}
		}
		scores[id].score += 1.0 / float64(k+rank+1)
	}

	for rank, bm := range bm25Results {
		id := bm.MemoryID
		if id == "" {
			continue
		}
		if _, ok := scores[id]; !ok {
			scores[id] = &fusionEntry{payload: destination.TranslatedPayload{
				PayloadID: bm.MemoryID,
				Content:   bm.Content,
			}}
		}
		scores[id].score += 1.0 / float64(k+rank+1)
	}

	merged := make([]fusionEntry, 0, len(scores))
	for _, e := range scores {
		merged = append(merged, *e)
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].score != merged[j].score {
			return merged[i].score > merged[j].score
		}
		return merged[i].payload.PayloadID < merged[j].payload.PayloadID
	})

	result := make([]destination.TranslatedPayload, len(merged))
	for i, e := range merged {
		result[i] = e.payload
	}
	return result
}
