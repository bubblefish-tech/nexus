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

package canonical

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sort"
)

// SRHT implements a Subsampled Randomized Hadamard Transform for
// deterministic dimensionality reduction. The transform consists of three
// components: a diagonal sign flip, a fast Walsh-Hadamard transform, and
// random subsampling.
//
// All randomness is derived deterministically from a 32-byte seed via
// SHA-256 counter-mode expansion. Given the same seed, the transform
// produces bit-identical output on all platforms.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.2 Step 1.
type SRHT struct {
	seed      [32]byte
	inputDim  int    // must be power of 2
	outputDim int    // <= inputDim
	signFlips []int8 // +1 or -1, length inputDim, derived from seed
	subsample []int  // sorted indices into inputDim, length outputDim
}

// NewSRHT creates a new SRHT transform. inputDim must be a power of 2.
// outputDim must not exceed inputDim. The sign flips and subsampling pattern
// are derived deterministically from the seed.
func NewSRHT(seed [32]byte, inputDim, outputDim int) (*SRHT, error) {
	if inputDim <= 0 || inputDim&(inputDim-1) != 0 {
		return nil, errors.New("srht: input dimension must be a positive power of 2")
	}
	if outputDim <= 0 || outputDim > inputDim {
		return nil, errors.New("srht: output dimension must be in [1, inputDim]")
	}
	s := &SRHT{
		seed:      seed,
		inputDim:  inputDim,
		outputDim: outputDim,
		signFlips: make([]int8, inputDim),
		subsample: make([]int, outputDim),
	}
	s.deriveSignFlips()
	s.deriveSubsample()
	return s, nil
}

// deriveSignFlips generates inputDim sign bits from SHA-256 counter-mode
// expansion of the seed. Deterministic across platforms.
func (s *SRHT) deriveSignFlips() {
	var buf []byte
	counterBuf := make([]byte, 4)
	counter := uint32(0)
	for len(buf)*8 < s.inputDim {
		h := sha256.New()
		h.Write(s.seed[:])
		binary.LittleEndian.PutUint32(counterBuf, counter)
		h.Write(counterBuf)
		buf = append(buf, h.Sum(nil)...)
		counter++
	}
	for i := 0; i < s.inputDim; i++ {
		byteIdx := i / 8
		bitIdx := uint(i % 8)
		if buf[byteIdx]&(1<<bitIdx) != 0 {
			s.signFlips[i] = +1
		} else {
			s.signFlips[i] = -1
		}
	}
}

// deriveSubsample picks outputDim distinct indices via Fisher-Yates from a
// seeded PRNG. Deterministic across platforms.
func (s *SRHT) deriveSubsample() {
	// If outputDim == inputDim, select all indices (identity subsample).
	if s.outputDim == s.inputDim {
		for i := range s.subsample {
			s.subsample[i] = i
		}
		return
	}

	rng := newSeededPRNG(s.seed, []byte("srht-subsample"))
	indices := make([]int, s.inputDim)
	for i := range indices {
		indices[i] = i
	}
	// Fisher-Yates shuffle
	for i := len(indices) - 1; i > 0; i-- {
		j := int(rng.Uint64() % uint64(i+1))
		indices[i], indices[j] = indices[j], indices[i]
	}
	copy(s.subsample, indices[:s.outputDim])
	// Sort for deterministic ordering in output
	sort.Ints(s.subsample)
}

// Apply runs the SRHT on an input vector. Input is zero-padded to inputDim
// if shorter. Output must have length outputDim.
func (s *SRHT) Apply(input []float64, output []float64) error {
	if len(output) != s.outputDim {
		return errors.New("srht: output length mismatch")
	}
	// Work in a buffer of size inputDim
	buf := make([]float64, s.inputDim)
	copy(buf, input) // zero-padded if input is shorter

	// Step 1: Sign flip (diagonal D)
	for i := range buf {
		if s.signFlips[i] == -1 {
			buf[i] = -buf[i]
		}
	}

	// Step 2: Fast Walsh-Hadamard transform (in-place, O(n log n))
	fwhtInPlace(buf)

	// Step 3: Normalize by 1/sqrt(inputDim) to make the transform unitary
	norm := 1.0 / math.Sqrt(float64(s.inputDim))
	for i := range buf {
		buf[i] = buf[i] * norm
	}

	// Step 4: Subsample
	for i, idx := range s.subsample {
		output[i] = buf[idx]
	}
	return nil
}

// fwhtInPlace is the standard in-place fast Walsh-Hadamard transform.
// It is deterministic and uses only addition and subtraction on float64,
// which is IEEE 754 compliant on all Go-supported platforms.
//
// Time complexity: O(n log n) where n = len(x).
// Space complexity: O(1) additional (in-place).
func fwhtInPlace(x []float64) {
	n := len(x)
	h := 1
	for h < n {
		for i := 0; i < n; i += h * 2 {
			for j := i; j < i+h; j++ {
				a := x[j]
				b := x[j+h]
				x[j] = a + b
				x[j+h] = a - b
			}
		}
		h *= 2
	}
}

// seededPRNG is a deterministic PRNG derived from a seed and a domain
// separator via SHA-256 counter mode. It produces uint64 values.
type seededPRNG struct {
	seed    [32]byte
	domain  []byte
	counter uint64
	buf     []byte
	pos     int
}

// newSeededPRNG creates a deterministic PRNG from a seed and domain separator.
func newSeededPRNG(seed [32]byte, domain []byte) *seededPRNG {
	return &seededPRNG{seed: seed, domain: domain}
}

// Uint64 returns the next deterministic pseudo-random uint64.
func (p *seededPRNG) Uint64() uint64 {
	for p.pos+8 > len(p.buf) {
		// Expand: H(seed || domain || counter)
		h := sha256.New()
		h.Write(p.seed[:])
		h.Write(p.domain)
		var ctr [8]byte
		binary.LittleEndian.PutUint64(ctr[:], p.counter)
		h.Write(ctr[:])
		p.buf = h.Sum(p.buf[:0])
		p.pos = 0
		p.counter++
	}
	v := binary.LittleEndian.Uint64(p.buf[p.pos : p.pos+8])
	p.pos += 8
	return v
}

// NextPowerOfTwo returns the smallest power of 2 >= n.
func NextPowerOfTwo(n int) int {
	if n <= 0 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	n |= n >> 32
	return n + 1
}
