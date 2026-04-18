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

import "math"

// L2Normalize divides a vector by its L2 norm using Kahan summation for
// cross-platform deterministic norm computation. In-place operation.
// Returns the computed norm. If the norm is zero the vector is unchanged.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.2 Step 2.
func L2Normalize(v []float64) float64 {
	norm := KahanL2Norm(v)
	if norm == 0 {
		return 0
	}
	inv := 1.0 / norm
	for i := range v {
		v[i] = v[i] * inv
	}
	return norm
}

// KahanL2Norm computes sqrt(sum(v_i^2)) with Kahan compensated summation.
// This produces identical results across all IEEE 754 platforms because
// the compensation removes the platform-dependent rounding of naive summation.
func KahanL2Norm(v []float64) float64 {
	sum := KahanSumSquares(v)
	return math.Sqrt(sum)
}

// KahanSumSquares computes sum(v_i^2) with Kahan compensated summation.
func KahanSumSquares(v []float64) float64 {
	var sum, c float64
	for _, x := range v {
		y := x*x - c
		t := sum + y
		c = (t - sum) - y
		sum = t
	}
	return sum
}

// KahanSum computes sum(v_i) with Kahan compensated summation.
func KahanSum(v []float64) float64 {
	var sum, c float64
	for _, x := range v {
		y := x - c
		t := sum + y
		c = (t - sum) - y
		sum = t
	}
	return sum
}

// KahanDot computes the dot product sum(a_i * b_i) with Kahan compensated
// summation. Panics if the lengths differ.
func KahanDot(a, b []float64) float64 {
	if len(a) != len(b) {
		panic("canonical: KahanDot length mismatch")
	}
	var sum, c float64
	for i := range a {
		y := a[i]*b[i] - c
		t := sum + y
		c = (t - sum) - y
		sum = t
	}
	return sum
}
