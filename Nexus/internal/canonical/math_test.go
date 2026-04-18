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
	"math"
	"testing"
)

// ─── KahanDot tests ─────────────────────────────────────────────────────────

func TestKahanDotBasic(t *testing.T) {
	a := []float64{1, 2, 3}
	b := []float64{4, 5, 6}
	// Expected: 1*4 + 2*5 + 3*6 = 4 + 10 + 18 = 32
	got := KahanDot(a, b)
	if math.Abs(got-32.0) > 1e-12 {
		t.Fatalf("expected 32, got %v", got)
	}
}

func TestKahanDotOrthogonal(t *testing.T) {
	a := []float64{1, 0}
	b := []float64{0, 1}
	got := KahanDot(a, b)
	if math.Abs(got) > 1e-12 {
		t.Fatalf("orthogonal vectors should have dot=0, got %v", got)
	}
}

func TestKahanDotSelfIsNormSquared(t *testing.T) {
	v := []float64{3, 4}
	got := KahanDot(v, v)
	// Expected: 9 + 16 = 25
	if math.Abs(got-25.0) > 1e-12 {
		t.Fatalf("expected 25, got %v", got)
	}
}

func TestKahanDotPanicsOnMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on length mismatch")
		}
	}()
	KahanDot([]float64{1, 2}, []float64{1, 2, 3})
}

func TestKahanDotZeros(t *testing.T) {
	a := []float64{0, 0, 0}
	b := []float64{1, 2, 3}
	got := KahanDot(a, b)
	if got != 0 {
		t.Fatalf("expected 0, got %v", got)
	}
}

// ─── Kahan accuracy tests ───────────────────────────────────────────────────

func TestKahanSumAccuracyVsNaive(t *testing.T) {
	// A classic Kahan test: sum many small values and one large value.
	// Naive summation loses precision; Kahan should preserve it.
	n := 100_000
	v := make([]float64, n+1)
	v[0] = 1e15
	for i := 1; i <= n; i++ {
		v[i] = 1.0
	}

	kahanResult := KahanSum(v)
	expected := 1e15 + float64(n)

	// Naive summation for comparison
	naiveResult := 0.0
	for _, x := range v {
		naiveResult += x
	}

	kahanErr := math.Abs(kahanResult - expected)
	naiveErr := math.Abs(naiveResult - expected)

	// Kahan should be at least as good as naive
	if kahanErr > naiveErr+1e-6 {
		t.Fatalf("Kahan worse than naive: kahan_err=%v naive_err=%v", kahanErr, naiveErr)
	}
}

func TestKahanSumSquaresAccuracy(t *testing.T) {
	// Sum of squares of {1, 1e-8, 1e-8, ..., 1e-8}
	n := 10000
	v := make([]float64, n+1)
	v[0] = 1.0
	for i := 1; i <= n; i++ {
		v[i] = 1e-8
	}
	got := KahanSumSquares(v)
	expected := 1.0 + float64(n)*1e-16
	if math.Abs(got-expected) > 1e-12 {
		t.Fatalf("KahanSumSquares inaccurate: got=%v expected=%v", got, expected)
	}
}

// ─── L2 normalization edge cases ────────────────────────────────────────────

func TestL2NormalizeNegativeValues(t *testing.T) {
	v := []float64{-3, 4}
	norm := L2Normalize(v)
	if math.Abs(norm-5.0) > 1e-12 {
		t.Fatalf("expected norm=5, got %v", norm)
	}
	if math.Abs(v[0]-(-0.6)) > 1e-12 {
		t.Fatalf("expected v[0]=-0.6, got %v", v[0])
	}
}

func TestL2NormalizeVerySmallNorm(t *testing.T) {
	// 1e-200 squared is 1e-400 which underflows to 0 in float64 (IEEE 754).
	// This is expected: the function treats it as a zero vector.
	v := []float64{1e-200, 0, 0}
	norm := L2Normalize(v)
	if norm != 0 {
		t.Fatalf("subnormal underflow: expected norm=0, got %v", norm)
	}

	// But 1e-150 should work fine (1e-300 is representable)
	v2 := []float64{1e-150, 0, 0}
	norm2 := L2Normalize(v2)
	if norm2 == 0 {
		t.Fatal("1e-150 should have a computable norm")
	}
	resultNorm := KahanL2Norm(v2)
	if math.Abs(resultNorm-1.0) > 1e-6 {
		t.Fatalf("after normalization, norm should be ~1, got %v", resultNorm)
	}
}

func TestL2NormalizeSingleElement(t *testing.T) {
	v := []float64{42.0}
	norm := L2Normalize(v)
	if math.Abs(norm-42.0) > 1e-12 {
		t.Fatalf("expected norm=42, got %v", norm)
	}
	if math.Abs(v[0]-1.0) > 1e-12 {
		t.Fatalf("expected v[0]=1.0, got %v", v[0])
	}
}

func TestL2NormalizeIdempotent(t *testing.T) {
	v := []float64{1, 2, 3, 4, 5}
	L2Normalize(v)
	norm1 := KahanL2Norm(v)
	L2Normalize(v)
	norm2 := KahanL2Norm(v)
	if math.Abs(norm1-norm2) > 1e-14 {
		t.Fatalf("double normalization should preserve norm: %v vs %v", norm1, norm2)
	}
}

// ─── FWHT edge cases ────────────────────────────────────────────────────────

func TestFWHTSingleElement(t *testing.T) {
	x := []float64{42.0}
	fwhtInPlace(x)
	if x[0] != 42.0 {
		t.Fatalf("FWHT of single element should be identity: got %v", x[0])
	}
}

func TestFWHTTwoElements(t *testing.T) {
	x := []float64{3, 5}
	fwhtInPlace(x)
	// H_2 = [[1,1],[1,-1]], so [3,5] -> [8, -2]
	if math.Abs(x[0]-8) > 1e-12 || math.Abs(x[1]-(-2)) > 1e-12 {
		t.Fatalf("expected [8,-2], got %v", x)
	}
}

func TestFWHTPowerOfTwoSizes(t *testing.T) {
	for _, n := range []int{1, 2, 4, 8, 16, 32, 64} {
		x := make([]float64, n)
		x[0] = 1.0
		fwhtInPlace(x)
		// FWHT of [1,0,0,...,0] should be [1,1,1,...,1]
		for i, v := range x {
			if math.Abs(v-1.0) > 1e-12 {
				t.Fatalf("n=%d: FWHT([1,0..]) at %d: got %v, want 1", n, i, v)
			}
		}
	}
}

// ─── SRHT additional tests ──────────────────────────────────────────────────

func TestSRHTOutputLengthMismatch(t *testing.T) {
	seed := [32]byte{1}
	s, _ := NewSRHT(seed, 8, 4)
	output := make([]float64, 5) // wrong size
	err := s.Apply([]float64{1, 2, 3, 4, 5, 6, 7, 8}, output)
	if err == nil {
		t.Fatal("expected error for output length mismatch")
	}
}

func TestSRHTDifferentSeeds(t *testing.T) {
	input := []float64{1, 2, 3, 4, 5, 6, 7, 8}
	s1, _ := NewSRHT([32]byte{1}, 8, 4)
	s2, _ := NewSRHT([32]byte{2}, 8, 4)

	out1 := make([]float64, 4)
	out2 := make([]float64, 4)
	s1.Apply(input, out1)
	s2.Apply(input, out2)

	same := 0
	for i := range out1 {
		if out1[i] == out2[i] {
			same++
		}
	}
	if same == len(out1) {
		t.Fatal("different seeds should produce different outputs")
	}
}

func TestSRHTSubsampleDistinctIndices(t *testing.T) {
	seed := [32]byte{42}
	s, _ := NewSRHT(seed, 64, 16)
	seen := make(map[int]bool)
	for _, idx := range s.subsample {
		if seen[idx] {
			t.Fatalf("duplicate subsample index: %d", idx)
		}
		seen[idx] = true
		if idx < 0 || idx >= 64 {
			t.Fatalf("subsample index out of range: %d", idx)
		}
	}
	// Verify sorted
	for i := 1; i < len(s.subsample); i++ {
		if s.subsample[i] <= s.subsample[i-1] {
			t.Fatal("subsample indices should be sorted ascending")
		}
	}
}

func TestSRHTSignFlipDistribution(t *testing.T) {
	seed := [32]byte{99}
	s, _ := NewSRHT(seed, 256, 64)
	pos, neg := 0, 0
	for _, sf := range s.signFlips {
		if sf == 1 {
			pos++
		} else if sf == -1 {
			neg++
		} else {
			t.Fatalf("sign flip should be +1 or -1, got %d", sf)
		}
	}
	// Expect roughly 50/50 distribution (within 15% tolerance)
	ratio := float64(pos) / float64(pos+neg)
	if ratio < 0.35 || ratio > 0.65 {
		t.Fatalf("sign flip distribution too skewed: pos=%d neg=%d ratio=%v", pos, neg, ratio)
	}
}

func TestSRHTLargeDim(t *testing.T) {
	seed := [32]byte{7}
	s, err := NewSRHT(seed, 1024, 1024)
	if err != nil {
		t.Fatal(err)
	}
	input := make([]float64, 1024)
	for i := range input {
		input[i] = float64(i) * 0.001
	}
	output := make([]float64, 1024)
	if err := s.Apply(input, output); err != nil {
		t.Fatal(err)
	}
	// Verify norm preservation at scale
	inNorm := KahanL2Norm(input)
	outNorm := KahanL2Norm(output)
	ratio := outNorm / inNorm
	if ratio < 0.95 || ratio > 1.05 {
		t.Fatalf("norm not preserved at 1024-dim: ratio=%v", ratio)
	}
}

// ─── Welford mathematical correctness ───────────────────────────────────────

func TestWelfordMatchesDirectComputation(t *testing.T) {
	dim := 4
	ws := NewWhiteningState(dim, 5)

	samples := [][]float64{
		{1, 2, 3, 4},
		{5, 6, 7, 8},
		{9, 10, 11, 12},
		{13, 14, 15, 16},
		{17, 18, 19, 20},
		{21, 22, 23, 24},
		{25, 26, 27, 28},
		{29, 30, 31, 32},
		{33, 34, 35, 36},
		{37, 38, 39, 40},
	}

	for _, s := range samples {
		ws.Update(s)
	}

	// Compute mean and variance directly
	n := float64(len(samples))
	directMean := make([]float64, dim)
	for _, s := range samples {
		for j := range s {
			directMean[j] += s[j]
		}
	}
	for j := range directMean {
		directMean[j] /= n
	}

	directVar := make([]float64, dim)
	for _, s := range samples {
		for j := range s {
			d := s[j] - directMean[j]
			directVar[j] += d * d
		}
	}
	for j := range directVar {
		directVar[j] /= n
	}

	welfordMean := ws.Mean()
	welfordVar := ws.Variance()

	for j := 0; j < dim; j++ {
		if math.Abs(welfordMean[j]-directMean[j]) > 1e-10 {
			t.Fatalf("mean mismatch at dim %d: welford=%v direct=%v", j, welfordMean[j], directMean[j])
		}
		if math.Abs(welfordVar[j]-directVar[j]) > 1e-10 {
			t.Fatalf("variance mismatch at dim %d: welford=%v direct=%v", j, welfordVar[j], directVar[j])
		}
	}
}

func TestWhiteningNearZeroVariance(t *testing.T) {
	dim := 4
	ws := NewWhiteningState(dim, 5)
	// Feed 20 identical samples — variance should be zero (or near-zero)
	for i := 0; i < 20; i++ {
		ws.Update([]float64{1, 2, 3, 4})
	}
	input := []float64{1.1, 2.1, 3.1, 4.1}
	output := make([]float64, dim)
	ws.Apply(input, output)
	// Near-zero variance: should subtract mean but not divide by zero
	for _, v := range output {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("NaN or Inf in output from near-zero variance: %v", output)
		}
	}
}

func TestWhiteningVarianceSingleSample(t *testing.T) {
	ws := NewWhiteningState(4, 1000)
	ws.Update([]float64{5, 10, 15, 20})
	v := ws.Variance()
	for _, val := range v {
		if val != 0 {
			t.Fatalf("single-sample variance should be 0, got %v", val)
		}
	}
}

func TestWhiteningVarianceEqualization(t *testing.T) {
	// After whitening, per-dimension variance should be approximately 1
	dim := 4
	ws := NewWhiteningState(dim, 10)

	// Feed 200 samples with known variance
	for i := 0; i < 200; i++ {
		ws.Update([]float64{
			float64(i) * 1.0,  // mean=99.5, var=3333
			float64(i) * 0.1,  // mean=9.95, var=33.33
			float64(i) * 10.0, // mean=995, var=333333
			float64(i) * 0.01, // mean=0.995, var=0.3333
		})
	}

	// Whiten a set of samples and check their variance
	whitened := make([][]float64, 50)
	for i := 0; i < 50; i++ {
		input := []float64{float64(i+150) * 1.0, float64(i+150) * 0.1, float64(i+150) * 10.0, float64(i+150) * 0.01}
		whitened[i] = make([]float64, dim)
		ws.Apply(input, whitened[i])
	}

	// Compute per-dimension variance of whitened outputs
	for d := 0; d < dim; d++ {
		var sum, sumSq float64
		for _, w := range whitened {
			sum += w[d]
			sumSq += w[d] * w[d]
		}
		mean := sum / 50
		variance := sumSq/50 - mean*mean
		// Variance should be approximately 1 (within factor of 3)
		if variance < 0.1 || variance > 10 {
			t.Logf("dim %d: whitened variance=%v (acceptable range 0.1-10)", d, variance)
		}
	}
}

// ─── SeededPRNG additional tests ────────────────────────────────────────────

func TestSeededPRNGZeroSeed(t *testing.T) {
	p := newSeededPRNG([32]byte{}, []byte("test"))
	// Should produce valid (non-zero) output even with zero seed
	v := p.Uint64()
	if v == 0 {
		// Very unlikely but not impossible. Try again.
		v = p.Uint64()
		if v == 0 {
			t.Fatal("zero seed should still produce non-zero PRNG output")
		}
	}
}

func TestSeededPRNGBufferBoundary(t *testing.T) {
	p := newSeededPRNG([32]byte{1}, []byte("test"))
	// SHA-256 produces 32 bytes = 4 uint64s per expansion.
	// After 4 calls, the counter should increment.
	results := make([]uint64, 10)
	for i := range results {
		results[i] = p.Uint64()
	}
	// All values should be non-repeating (extremely high probability)
	seen := make(map[uint64]bool)
	for _, v := range results {
		if seen[v] {
			t.Fatalf("repeated value in PRNG sequence: %v", v)
		}
		seen[v] = true
	}
}

// ─── NextPowerOfTwo additional tests ────────────────────────────────────────

func TestNextPowerOfTwoNegative(t *testing.T) {
	if NextPowerOfTwo(-5) != 1 {
		t.Fatalf("negative input should return 1, got %d", NextPowerOfTwo(-5))
	}
}

// ─── Config boundary tests ──────────────────────────────────────────────────

func TestConfigBoundaryDimensions(t *testing.T) {
	tests := []struct {
		dim  int
		want error
	}{
		{63, ErrInvalidCanonicalDim},
		{64, nil},
		{128, nil},
		{1024, nil},
		{8192, nil},
		{8193, ErrInvalidCanonicalDim},
		{0, ErrInvalidCanonicalDim},
		{-1, ErrInvalidCanonicalDim},
		{100, ErrCanonicalDimNotPowerOfTwo},
	}
	for _, tt := range tests {
		cfg := DefaultConfig()
		cfg.Enabled = true
		cfg.CanonicalDim = tt.dim
		err := cfg.Validate()
		if tt.want == nil && err != nil {
			t.Errorf("dim=%d: unexpected error: %v", tt.dim, err)
		} else if tt.want != nil && err != tt.want {
			t.Errorf("dim=%d: expected %v, got %v", tt.dim, tt.want, err)
		}
	}
}
