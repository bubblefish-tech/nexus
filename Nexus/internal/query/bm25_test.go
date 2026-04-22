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
	"testing"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

func TestRRFMerge_BothLists(t *testing.T) {
	t.Helper()
	dense := []destination.ScoredRecord{
		{Payload: destination.TranslatedPayload{PayloadID: "a", Content: "alpha"}, Score: 0.9},
		{Payload: destination.TranslatedPayload{PayloadID: "b", Content: "beta"}, Score: 0.8},
	}
	bm25 := []BM25Result{
		{MemoryID: "b", Content: "beta", Rank: -1.0},
		{MemoryID: "c", Content: "gamma", Rank: -0.5},
	}
	merged := RRFMerge(dense, bm25, 60)
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(merged))
	}
	if merged[0].PayloadID != "b" {
		t.Errorf("expected 'b' ranked first (in both lists), got %q", merged[0].PayloadID)
	}
}

func TestRRFMerge_EmptyBM25(t *testing.T) {
	t.Helper()
	dense := []destination.ScoredRecord{
		{Payload: destination.TranslatedPayload{PayloadID: "a"}, Score: 0.9},
	}
	merged := RRFMerge(dense, nil, 60)
	if len(merged) != 1 {
		t.Fatalf("expected 1 result, got %d", len(merged))
	}
}

func TestRRFMerge_EmptyDense(t *testing.T) {
	t.Helper()
	bm25 := []BM25Result{
		{MemoryID: "x", Content: "xray"},
	}
	merged := RRFMerge(nil, bm25, 60)
	if len(merged) != 1 {
		t.Fatalf("expected 1 result, got %d", len(merged))
	}
	if merged[0].PayloadID != "x" {
		t.Errorf("expected payload_id 'x', got %q", merged[0].PayloadID)
	}
}

func TestRRFMerge_DefaultK(t *testing.T) {
	t.Helper()
	dense := []destination.ScoredRecord{
		{Payload: destination.TranslatedPayload{PayloadID: "a"}, Score: 0.5},
	}
	merged := RRFMerge(dense, nil, 0)
	if len(merged) != 1 {
		t.Fatalf("expected 1 result with k=0 (defaults to 60), got %d", len(merged))
	}
}

func TestExtractTemporalHint_Yesterday(t *testing.T) {
	t.Helper()
	bin := ExtractTemporalHint("what did I say yesterday about TPS-42?")
	if bin != 2 {
		t.Errorf("expected bin 2 (yesterday), got %d", bin)
	}
}

func TestExtractTemporalHint_NoHint(t *testing.T) {
	t.Helper()
	bin := ExtractTemporalHint("tell me about my project")
	if bin != -1 {
		t.Errorf("expected -1 (no hint), got %d", bin)
	}
}

func TestExtractTemporalHint_Today(t *testing.T) {
	t.Helper()
	bin := ExtractTemporalHint("what did we discuss today?")
	if bin != 1 {
		t.Errorf("expected bin 1 (today), got %d", bin)
	}
}

func TestExtractTemporalHint_LastWeek(t *testing.T) {
	t.Helper()
	bin := ExtractTemporalHint("the meeting from last week")
	if bin != 4 {
		t.Errorf("expected bin 4 (last week), got %d", bin)
	}
}

func TestExtractTemporalHint_CaseInsensitive(t *testing.T) {
	t.Helper()
	bin := ExtractTemporalHint("What happened YESTERDAY?")
	if bin != 2 {
		t.Errorf("expected bin 2 (yesterday), got %d", bin)
	}
}
