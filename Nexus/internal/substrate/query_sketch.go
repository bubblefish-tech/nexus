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

package substrate

import (
	"errors"
	"math"

	"github.com/BubbleFish-Nexus/internal/canonical"
)

// QuerySketch is the 4-bit asymmetric representation used at query time.
// Each coordinate is quantized to a 4-bit signed integer in [-7, +7].
// Two coefficients are packed per byte (high nibble, low nibble).
//
// For canonical_d=1024: 16B header + 512B coefficients = 528 bytes.
//
// Reference: RaBitQ paper (SIGMOD 2024), Section 4 (asymmetric quantization).
type QuerySketch struct {
	Version      uint32
	CanonicalDim uint32
	StateID      uint32
	MaxAbs       float64 // quantization scale factor
	Coefficients []byte  // 4-bit signed, packed 2 per byte (high nibble first)
}

// ComputeQuerySketch quantizes the query projection to 4 bits per coordinate.
//
// The process:
//  1. Apply SRHT with ratchet state as seed
//  2. Find max absolute value for quantization scale
//  3. Quantize each coordinate to [-7, +7] (4-bit signed, skipping -8)
//  4. Pack two coefficients per byte (high nibble first)
//
// Reference: RaBitQ paper (SIGMOD 2024), Section 4.
func ComputeQuerySketch(canonicalVec []float64, stateBytes [32]byte, stateID uint32) (*QuerySketch, error) {
	d := len(canonicalVec)
	if d <= 0 || d&(d-1) != 0 {
		return nil, errors.New("bbq: canonical dimension must be a positive power of 2")
	}

	srht, err := canonical.NewSRHT(stateBytes, d, d)
	if err != nil {
		return nil, err
	}
	q := make([]float64, d)
	if err := srht.Apply(canonicalVec, q); err != nil {
		return nil, err
	}

	// Find max absolute value for quantization scale
	maxAbs := 0.0
	for _, v := range q {
		if a := math.Abs(v); a > maxAbs {
			maxAbs = a
		}
	}

	// Quantize each coordinate to [-7, +7]
	coefs := make([]byte, (d+1)/2)
	for i := 0; i < d; i++ {
		var level int
		if maxAbs > 0 {
			level = int(math.Round(q[i] / maxAbs * 7))
		}
		if level < -7 {
			level = -7
		}
		if level > 7 {
			level = 7
		}
		// Pack as 4-bit signed (two's complement nibble)
		nibble := byte(level) & 0x0F
		if i%2 == 0 {
			coefs[i/2] = nibble << 4
		} else {
			coefs[i/2] |= nibble
		}
	}

	return &QuerySketch{
		Version:      sketchVersionV1,
		CanonicalDim: uint32(d),
		StateID:      stateID,
		MaxAbs:       maxAbs,
		Coefficients: coefs,
	}, nil
}

// unpackQueryCoefficient extracts the i-th 4-bit signed coefficient.
func unpackQueryCoefficient(coefs []byte, i int) int8 {
	var raw byte
	if i%2 == 0 {
		raw = (coefs[i/2] >> 4) & 0x0F
	} else {
		raw = coefs[i/2] & 0x0F
	}
	// Sign-extend 4-bit to int8
	if raw&0x08 != 0 {
		return int8(raw | 0xF0)
	}
	return int8(raw)
}
