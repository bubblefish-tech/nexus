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

// This file implements the BBQ (Binary Balanced Quantization) sketch
// computation as a clean-room Go port of the RaBitQ algorithm described in:
//
//   "RaBitQ: Quantizing High-Dimensional Vectors with a Theoretical Error
//   Bound for Approximate Nearest Neighbor Search" (SIGMOD 2024)
//
// The implementation is derived solely from the paper's algorithm description.
// No code was copied from any existing codebase.
package substrate

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/bubblefish-tech/nexus/internal/canonical"
)

// Sketch format constants.
const (
	sketchMagic      = 0x42_46_53_4B // "BFSK"
	sketchVersionV1  = 0x0001_0000
	sketchHeaderSize = 32 // magic(4) + version(4) + dim(4) + stateID(4) + corrections(16)
)

// StoreSketch is the 1-bit binary quantization sketch stored per memory.
// For canonical_d=1024: 16B header + 16B corrections + 128B sign bits = 160 bytes.
type StoreSketch struct {
	Version      uint32
	CanonicalDim uint32
	StateID      uint32
	Corrections  [4]float32 // [posL2Norm, negL2Norm, maxAbs, meanAbs]
	SignBits     []byte     // canonical_dim / 8 bytes, LSB-first packing
}

// ComputeStoreSketch produces a 1-bit sketch from a canonical vector using
// the given ratchet state as the SRHT seed. Deterministic given identical
// (canonical, stateBytes, stateID).
//
// The sketch function:
//  1. Apply SRHT with stateBytes as seed (forward-secure component)
//  2. Pack sign bits (1 bit per coordinate, LSB-first)
//  3. Compute four correction factors from the projected vector
//
// Reference: RaBitQ paper (SIGMOD 2024), Section 4.
func ComputeStoreSketch(canonicalVec []float64, stateBytes [32]byte, stateID uint32) (*StoreSketch, error) {
	d := len(canonicalVec)
	if d <= 0 || d&(d-1) != 0 {
		return nil, errors.New("bbq: canonical dimension must be a positive power of 2")
	}

	// Apply SRHT with ratchet state as seed (same dim → same dim)
	srht, err := canonical.NewSRHT(stateBytes, d, d)
	if err != nil {
		return nil, err
	}
	y := make([]float64, d)
	if err := srht.Apply(canonicalVec, y); err != nil {
		return nil, err
	}

	// Compute correction factors
	corrections := computeCorrections(y)

	// Pack sign bits (LSB-first)
	signBits := make([]byte, d/8)
	for i := 0; i < d; i++ {
		if y[i] > 0 {
			signBits[i/8] |= 1 << uint(i%8)
		}
		// y[i] == 0 maps to sign = 0 (negative side), which is conservative
	}

	return &StoreSketch{
		Version:      sketchVersionV1,
		CanonicalDim: uint32(d),
		StateID:      stateID,
		Corrections:  corrections,
		SignBits:      signBits,
	}, nil
}

// computeCorrections computes the four RaBitQ correction factors from the
// projected vector y:
//
//	[0] posL2Norm:  sqrt(sum(y_i^2) for y_i > 0)
//	[1] negL2Norm:  sqrt(sum(y_i^2) for y_i < 0)
//	[2] maxAbs:     max(|y_i|)
//	[3] meanAbs:    mean(|y_i|)
//
// All computations use float64 with Kahan summation for cross-platform
// determinism. Results are stored as float32.
//
// Reference: RaBitQ paper (SIGMOD 2024), Section 4.
func computeCorrections(y []float64) [4]float32 {
	var posSum, negSum, maxAbs float64
	var posC, negC float64       // Kahan compensation
	var absSum, absC float64     // Kahan for mean absolute

	for _, v := range y {
		absV := math.Abs(v)
		if absV > maxAbs {
			maxAbs = absV
		}

		// Kahan accumulate |v| for mean
		ay := absV - absC
		at := absSum + ay
		absC = (at - absSum) - ay
		absSum = at

		if v > 0 {
			py := v*v - posC
			pt := posSum + py
			posC = (pt - posSum) - py
			posSum = pt
		} else if v < 0 {
			ny := v*v - negC
			nt := negSum + ny
			negC = (nt - negSum) - ny
			negSum = nt
		}
	}

	meanAbs := absSum / float64(len(y))

	return [4]float32{
		float32(math.Sqrt(posSum)),
		float32(math.Sqrt(negSum)),
		float32(maxAbs),
		float32(meanAbs),
	}
}

// Marshal encodes a StoreSketch to the on-disk byte format.
//
//	Byte offset  Size  Field
//	0            4     Magic: 0x42 0x46 0x53 0x4B ("BFSK")
//	4            4     Version: 0x00010000
//	8            4     Canonical dim (LittleEndian uint32)
//	12           4     Ratchet state_id (LittleEndian uint32)
//	16           16    Correction factors (4 × float32, LittleEndian)
//	32           N/8   Sign bits, packed LSB-first
func (s *StoreSketch) Marshal() ([]byte, error) {
	if s == nil {
		return nil, errors.New("bbq: nil sketch")
	}
	signBytes := int(s.CanonicalDim) / 8
	totalSize := sketchHeaderSize + signBytes
	buf := make([]byte, totalSize)

	// Magic
	binary.LittleEndian.PutUint32(buf[0:4], sketchMagic)
	// Version
	binary.LittleEndian.PutUint32(buf[4:8], s.Version)
	// Canonical dim
	binary.LittleEndian.PutUint32(buf[8:12], s.CanonicalDim)
	// State ID
	binary.LittleEndian.PutUint32(buf[12:16], s.StateID)
	// Corrections (4 × float32)
	for i, c := range s.Corrections {
		binary.LittleEndian.PutUint32(buf[16+i*4:20+i*4], math.Float32bits(c))
	}
	// Sign bits
	copy(buf[sketchHeaderSize:], s.SignBits)

	return buf, nil
}

// UnmarshalStoreSketch parses the on-disk byte format into a StoreSketch.
func UnmarshalStoreSketch(data []byte) (*StoreSketch, error) {
	if len(data) < sketchHeaderSize {
		return nil, errors.New("bbq: sketch too short")
	}
	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != sketchMagic {
		return nil, errors.New("bbq: invalid sketch magic")
	}

	s := &StoreSketch{
		Version:      binary.LittleEndian.Uint32(data[4:8]),
		CanonicalDim: binary.LittleEndian.Uint32(data[8:12]),
		StateID:      binary.LittleEndian.Uint32(data[12:16]),
	}
	for i := 0; i < 4; i++ {
		s.Corrections[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[16+i*4 : 20+i*4]))
	}

	signBitBytes := int(s.CanonicalDim) / 8
	if len(data) < sketchHeaderSize+signBitBytes {
		return nil, errors.New("bbq: sketch truncated")
	}
	s.SignBits = make([]byte, signBitBytes)
	copy(s.SignBits, data[sketchHeaderSize:sketchHeaderSize+signBitBytes])

	return s, nil
}

// SketchSize returns the total byte size of a sketch for the given canonical dimension.
func SketchSize(canonicalDim int) int {
	return sketchHeaderSize + canonicalDim/8
}
