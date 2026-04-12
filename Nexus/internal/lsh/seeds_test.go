// Copyright © 2026 Shawn Sammartano. All rights reserved.
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

package lsh_test

import (
	"testing"

	"github.com/BubbleFish-Nexus/internal/lsh"
	"github.com/BubbleFish-Nexus/internal/secrets"
)

func newSeeds(t *testing.T) *lsh.TierSeeds {
	t.Helper()
	dir, err := secrets.Open(t.TempDir())
	if err != nil {
		t.Fatalf("secrets.Open: %v", err)
	}
	ts, err := lsh.LoadOrGenerate(dir)
	if err != nil {
		t.Fatalf("LoadOrGenerate: %v", err)
	}
	return ts
}

// TestTierSeeds_Deterministic verifies the same seed always yields the same
// hyperplane vectors and therefore the same bucket for the same input.
func TestTierSeeds_Deterministic(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	dir, _ := secrets.Open(base)
	ts1, _ := lsh.LoadOrGenerate(dir)
	ts2, _ := lsh.LoadOrGenerate(dir) // reload from same seed files

	dim := 8
	vec := makeVec(dim, 1.0)

	for tier := 0; tier < lsh.NumTiers; tier++ {
		h1, err := ts1.HyperplaneVectors(tier, dim)
		if err != nil {
			t.Fatalf("tier %d: %v", tier, err)
		}
		h2, err := ts2.HyperplaneVectors(tier, dim)
		if err != nil {
			t.Fatalf("tier %d reload: %v", tier, err)
		}
		b1, _ := lsh.BucketID(vec, h1)
		b2, _ := lsh.BucketID(vec, h2)
		if b1 != b2 {
			t.Errorf("tier %d: bucket changed after reload (%d != %d)", tier, b1, b2)
		}
	}
}

// TestTierSeeds_CrossTierIsolation verifies the same vector maps to different
// bucket IDs in different tiers (with overwhelming probability for random seeds).
func TestTierSeeds_CrossTierIsolation(t *testing.T) {
	t.Helper()
	ts := newSeeds(t)
	dim := 32
	vec := makeVec(dim, 0.5)

	buckets := make(map[uint16]int)
	for tier := 0; tier < lsh.NumTiers; tier++ {
		hvecs, err := ts.HyperplaneVectors(tier, dim)
		if err != nil {
			t.Fatalf("tier %d: %v", tier, err)
		}
		b, err := lsh.BucketID(vec, hvecs)
		if err != nil {
			t.Fatalf("BucketID tier %d: %v", tier, err)
		}
		if prev, ok := buckets[b]; ok {
			t.Errorf("tier %d and tier %d produced the same bucket %d (should be distinct)", tier, prev, b)
		}
		buckets[b] = tier
	}
}

// TestBucketID_Errors verifies error conditions are reported.
func TestBucketID_Errors(t *testing.T) {
	t.Helper()
	ts := newSeeds(t)

	hvecs, _ := ts.HyperplaneVectors(0, 4)

	// Empty vector.
	if _, err := lsh.BucketID(nil, hvecs); err == nil {
		t.Error("expected error for nil vector")
	}

	// Dimension mismatch.
	if _, err := lsh.BucketID(makeVec(8, 1.0), hvecs); err == nil {
		t.Error("expected error for dimension mismatch")
	}
}

// TestHyperplaneVectors_InvalidTier verifies out-of-range tier returns error.
func TestHyperplaneVectors_InvalidTier(t *testing.T) {
	t.Helper()
	ts := newSeeds(t)
	for _, tier := range []int{-1, lsh.NumTiers, 100} {
		if _, err := ts.HyperplaneVectors(tier, 8); err == nil {
			t.Errorf("tier %d: expected error, got nil", tier)
		}
	}
}

// TestHyperplaneVectors_InvalidDim verifies dim <= 0 returns error.
func TestHyperplaneVectors_InvalidDim(t *testing.T) {
	t.Helper()
	ts := newSeeds(t)
	for _, dim := range []int{0, -1} {
		if _, err := ts.HyperplaneVectors(0, dim); err == nil {
			t.Errorf("dim %d: expected error, got nil", dim)
		}
	}
}

// makeVec creates a float32 slice of length dim filled with value v.
func makeVec(dim int, v float32) []float32 {
	s := make([]float32, dim)
	for i := range s {
		s[i] = v
	}
	return s
}
