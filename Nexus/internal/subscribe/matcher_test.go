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

package subscribe

import (
	"context"
	"math"
	"sync/atomic"
	"testing"

	_ "modernc.org/sqlite"
)

func fakeEmbedder(vectors map[string][]float32) EmbedFunc {
	return func(_ context.Context, text string) ([]float32, error) {
		if vec, ok := vectors[text]; ok {
			return vec, nil
		}
		return []float32{0.1, 0.1, 0.1}, nil
	}
}

func TestMatcher_HighSimilarityMatches(t *testing.T) {
	db := testDB(t)
	store, _ := NewStore(db)

	store.Add("agent-1", "competitive intelligence")

	vec := []float32{0.9, 0.1, 0.0}
	embedder := fakeEmbedder(map[string][]float32{
		"competitive intelligence":                     vec,
		"our competitor just launched a new product":    vec,
	})

	matcher := NewMatcher(store, embedder)
	matches, err := matcher.Match(context.Background(), "our competitor just launched a new product")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d", len(matches))
	}
}

func TestMatcher_LowSimilarityNoMatch(t *testing.T) {
	db := testDB(t)
	store, _ := NewStore(db)

	store.Add("agent-1", "competitive intelligence")

	embedder := fakeEmbedder(map[string][]float32{
		"competitive intelligence": {1.0, 0.0, 0.0},
		"recipe for chocolate cake": {0.0, 0.0, 1.0},
	})

	matcher := NewMatcher(store, embedder)
	matches, err := matcher.Match(context.Background(), "recipe for chocolate cake")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches, got %d", len(matches))
	}
}

func TestMatcher_CachePreventsRecompute(t *testing.T) {
	db := testDB(t)
	store, _ := NewStore(db)

	store.Add("agent-1", "bug reports")

	var callCount atomic.Int64
	embedder := func(_ context.Context, text string) ([]float32, error) {
		callCount.Add(1)
		return []float32{0.5, 0.5, 0.5}, nil
	}

	matcher := NewMatcher(store, embedder)
	matcher.Match(context.Background(), "first write")
	matcher.Match(context.Background(), "second write")
	matcher.Match(context.Background(), "third write")

	// 3 content embeds + 1 filter embed (cached after first call) = 4
	got := callCount.Load()
	if got != 4 {
		t.Errorf("expected 4 embed calls (3 content + 1 cached filter), got %d", got)
	}
}

func TestMatcher_CacheInvalidation(t *testing.T) {
	db := testDB(t)
	store, _ := NewStore(db)

	sub, _ := store.Add("agent-1", "old filter")

	var callCount atomic.Int64
	embedder := func(_ context.Context, text string) ([]float32, error) {
		callCount.Add(1)
		return []float32{0.5, 0.5, 0.5}, nil
	}

	matcher := NewMatcher(store, embedder)
	matcher.Match(context.Background(), "content 1")

	before := callCount.Load()
	matcher.InvalidateCache(sub.ID)
	matcher.Match(context.Background(), "content 2")
	after := callCount.Load()

	// After invalidation, filter should be re-embedded (1 content + 1 filter = 2 more)
	if after-before != 2 {
		t.Errorf("expected 2 new calls after invalidation, got %d", after-before)
	}
}

func TestMatcher_NilEmbedder(t *testing.T) {
	db := testDB(t)
	store, _ := NewStore(db)

	matcher := NewMatcher(store, nil)
	matches, err := matcher.Match(context.Background(), "anything")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Errorf("expected 0 matches with nil embedder, got %d", len(matches))
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{"opposite", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{"empty", nil, nil, 0.0},
		{"mismatched", []float32{1, 0}, []float32{1, 0, 0}, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSimilarity(tt.a, tt.b)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("cosineSimilarity = %f, want %f", got, tt.want)
			}
		})
	}
}
