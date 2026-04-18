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
	"fmt"
	"testing"

	"github.com/BubbleFish-Nexus/internal/destination"
)

func makeTestRecords(n int) []destination.TranslatedPayload {
	records := make([]destination.TranslatedPayload, n)
	for i := range records {
		records[i] = destination.TranslatedPayload{
			PayloadID: fmt.Sprintf("mem-%04d", i),
			Subject:   fmt.Sprintf("subject-%d", i),
		}
	}
	return records
}

func TestStage35DisabledSubstrate(t *testing.T) {
	records := makeTestRecords(300)
	result := stage35SketchPrefilter(nil, []float32{1, 2, 3}, records, 200, 100)
	if len(result) != 300 {
		t.Fatalf("nil config should return all records, got %d", len(result))
	}
}

func TestStage35NoEmbedding(t *testing.T) {
	cfg := &SketchPrefilterConfig{}
	records := makeTestRecords(300)
	result := stage35SketchPrefilter(cfg, nil, records, 200, 100)
	if len(result) != 300 {
		t.Fatalf("nil embedding should return all records, got %d", len(result))
	}
	result = stage35SketchPrefilter(cfg, []float32{}, records, 200, 100)
	if len(result) != 300 {
		t.Fatalf("empty embedding should return all records, got %d", len(result))
	}
}

func TestStage35BelowThreshold(t *testing.T) {
	cfg := &SketchPrefilterConfig{}
	records := makeTestRecords(150) // below 200 threshold
	result := stage35SketchPrefilter(cfg, []float32{1, 2, 3}, records, 200, 100)
	if len(result) != 150 {
		t.Fatalf("below threshold should return all records, got %d", len(result))
	}
}

func TestStage35ExactThreshold(t *testing.T) {
	cfg := &SketchPrefilterConfig{}
	records := makeTestRecords(200) // exactly at threshold
	result := stage35SketchPrefilter(cfg, []float32{1, 2, 3}, records, 200, 100)
	// At threshold (<=), should NOT activate
	if len(result) != 200 {
		t.Fatalf("at threshold should return all records, got %d", len(result))
	}
}

func TestStage35NilCanonical(t *testing.T) {
	cfg := &SketchPrefilterConfig{
		// Substrate and Canonical are nil
	}
	records := makeTestRecords(300)
	result := stage35SketchPrefilter(cfg, []float32{1, 2, 3}, records, 200, 100)
	if len(result) != 300 {
		t.Fatalf("nil canonical should return all records, got %d", len(result))
	}
}

func TestRankAndTruncate(t *testing.T) {
	scored := []scoredCandidate{
		{record: destination.TranslatedPayload{PayloadID: "low"}, score: 0.1},
		{record: destination.TranslatedPayload{PayloadID: "high"}, score: 0.9},
		{record: destination.TranslatedPayload{PayloadID: "mid"}, score: 0.5},
	}

	result := rankAndTruncate(scored, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].PayloadID != "high" {
		t.Fatalf("first result should be 'high', got %s", result[0].PayloadID)
	}
	if result[1].PayloadID != "mid" {
		t.Fatalf("second result should be 'mid', got %s", result[1].PayloadID)
	}
}

func TestRankAndTruncateTopKLargerThanInput(t *testing.T) {
	scored := []scoredCandidate{
		{record: destination.TranslatedPayload{PayloadID: "a"}, score: 0.5},
	}
	result := rankAndTruncate(scored, 100)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestRankAndTruncateEmpty(t *testing.T) {
	result := rankAndTruncate(nil, 10)
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}
}

func TestWithSketchPrefilterBuilder(t *testing.T) {
	cr := New(nil, nil)
	cfg := &SketchPrefilterConfig{}
	cr.WithSketchPrefilter(cfg)
	if cr.sketchPrefilter != cfg {
		t.Fatal("WithSketchPrefilter should set the field")
	}
}
