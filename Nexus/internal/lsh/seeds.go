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

// Package lsh implements Locality-Sensitive Hashing for BubbleFish Nexus.
//
// Seeds are per-tier: the same content in different tiers produces different
// bucket IDs, making cross-tier bucket collisions impossible by construction.
// Seeds are persisted in ~/.bubblefish/Nexus/secrets/ so they survive restarts.
//
// Phase 2.2 establishes the seed infrastructure. Phase 3.1 adds the full
// 16-hyperplane SimHash computation on top of these seeds.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.2, Phase 3 Subtask 3.1.
package lsh

import (
	"encoding/binary"
	"fmt"
	"math/rand"

	"github.com/BubbleFish-Nexus/internal/secrets"
)

const (
	// NumTiers is the number of access tiers (0-3).
	NumTiers = 4
	// NumHyperplanes is the number of random hyperplanes used for SimHash.
	// Each hyperplane contributes 1 bit to the bucket ID.
	NumHyperplanes = 16
)

// TierSeeds holds per-tier random seeds used to generate hyperplane vectors.
// A fresh TierSeeds instance must be created with LoadOrGenerate.
type TierSeeds struct {
	seeds [NumTiers][]byte
}

// LoadOrGenerate loads (or generates) per-tier LSH seeds from the secrets
// directory. Each tier's seed is 32 bytes, persisted at
// secrets/lsh-tier-N.seed (0600).
//
// After construction, seeds[tier] is stable across restarts, ensuring that
// the same content in the same tier always maps to the same bucket.
func LoadOrGenerate(dir *secrets.Dir) (*TierSeeds, error) {
	raw, err := dir.LoadOrGenerateAllLSHSeeds()
	if err != nil {
		return nil, fmt.Errorf("lsh: load seeds: %w", err)
	}
	return &TierSeeds{seeds: raw}, nil
}

// HyperplaneVectors returns NumHyperplanes unit vectors for the given tier,
// seeded deterministically from the tier's 32-byte seed. The same seed always
// produces the same vectors. Different tiers produce different vectors.
//
// dim is the embedding dimension (e.g. 1536 for text-embedding-3-small).
// Returns an error if tier is out of range or dim <= 0.
func (ts *TierSeeds) HyperplaneVectors(tier, dim int) ([][NumHyperplanes]float64, error) {
	if tier < 0 || tier >= NumTiers {
		return nil, fmt.Errorf("lsh: invalid tier %d", tier)
	}
	if dim <= 0 {
		return nil, fmt.Errorf("lsh: dim must be positive, got %d", dim)
	}

	seed := ts.seeds[tier]
	// Derive a deterministic int64 seed from the 32-byte material.
	seedInt := int64(binary.LittleEndian.Uint64(seed[:8]))
	//nolint:gosec // G404: not a security use; deterministic PRNG for LSH hyperplanes.
	rng := rand.New(rand.NewSource(seedInt))

	// Generate NumHyperplanes random vectors of dimension dim.
	// Each vector is a row: hvecs[i][h] is component i of hyperplane h.
	hvecs := make([][NumHyperplanes]float64, dim)
	for i := range hvecs {
		for h := range hvecs[i] {
			hvecs[i][h] = rng.NormFloat64()
		}
	}
	return hvecs, nil
}

// BucketID computes the 16-bit LSH bucket ID for the given embedding vector
// using the tier's hyperplane vectors. The result is a uint16 where bit h is
// set if the dot product with hyperplane h is positive.
//
// This is the SimHash construction: same content in tier T always maps to the
// same bucket; same content in a different tier maps to a different bucket
// (with overwhelming probability) because the hyperplanes differ.
//
// vec must have the same dimension as hvecs was generated for.
func BucketID(vec []float32, hvecs [][NumHyperplanes]float64) (uint16, error) {
	if len(vec) == 0 {
		return 0, fmt.Errorf("lsh: empty vector")
	}
	if len(vec) != len(hvecs) {
		return 0, fmt.Errorf("lsh: vec dim %d != hvecs dim %d", len(vec), len(hvecs))
	}

	var dots [NumHyperplanes]float64
	for i, v := range vec {
		for h := 0; h < NumHyperplanes; h++ {
			dots[h] += float64(v) * hvecs[i][h]
		}
	}

	var bucket uint16
	for h := 0; h < NumHyperplanes; h++ {
		if dots[h] >= 0 {
			bucket |= 1 << uint(h)
		}
	}
	return bucket, nil
}
