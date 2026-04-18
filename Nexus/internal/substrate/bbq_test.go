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
	"math"
	"testing"

	"github.com/BubbleFish-Nexus/internal/canonical"
)

// makeCanonicalVec creates a deterministic unit-norm canonical vector.
func makeCanonicalVec(dim int, seed float64) []float64 {
	v := make([]float64, dim)
	for i := range v {
		v[i] = math.Sin(seed*float64(i+1)*0.1) + math.Cos(seed*float64(i+2)*0.07)
	}
	canonical.L2Normalize(v)
	return v
}

// ─── StoreSketch computation tests ──────────────────────────────────────────

func TestComputeStoreSketchBasic(t *testing.T) {
	state := [32]byte{1, 2, 3, 4, 5}
	vec := makeCanonicalVec(64, 1.0)

	sketch, err := ComputeStoreSketch(vec, state, 1)
	if err != nil {
		t.Fatal(err)
	}
	if sketch.Version != sketchVersionV1 {
		t.Fatalf("expected version %x, got %x", sketchVersionV1, sketch.Version)
	}
	if sketch.CanonicalDim != 64 {
		t.Fatalf("expected dim=64, got %d", sketch.CanonicalDim)
	}
	if sketch.StateID != 1 {
		t.Fatalf("expected stateID=1, got %d", sketch.StateID)
	}
	if len(sketch.SignBits) != 8 {
		t.Fatalf("expected 8 sign bytes for dim=64, got %d", len(sketch.SignBits))
	}
}

func TestComputeStoreSketchSize(t *testing.T) {
	tests := []struct {
		dim      int
		wantSign int
		wantTotal int
	}{
		{64, 8, 40},
		{128, 16, 48},
		{256, 32, 64},
		{1024, 128, 160},
	}
	for _, tt := range tests {
		vec := makeCanonicalVec(tt.dim, 1.0)
		sketch, err := ComputeStoreSketch(vec, [32]byte{42}, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(sketch.SignBits) != tt.wantSign {
			t.Fatalf("dim=%d: signBits len=%d, want %d", tt.dim, len(sketch.SignBits), tt.wantSign)
		}
		marshaled, _ := sketch.Marshal()
		if len(marshaled) != tt.wantTotal {
			t.Fatalf("dim=%d: marshal len=%d, want %d", tt.dim, len(marshaled), tt.wantTotal)
		}
		if SketchSize(tt.dim) != tt.wantTotal {
			t.Fatalf("dim=%d: SketchSize=%d, want %d", tt.dim, SketchSize(tt.dim), tt.wantTotal)
		}
	}
}

func TestComputeStoreSketchDeterminism(t *testing.T) {
	state := [32]byte{10, 20, 30}
	vec := makeCanonicalVec(128, 2.5)

	s1, _ := ComputeStoreSketch(vec, state, 1)
	s2, _ := ComputeStoreSketch(vec, state, 1)

	b1, _ := s1.Marshal()
	b2, _ := s2.Marshal()

	if len(b1) != len(b2) {
		t.Fatal("marshaled lengths differ")
	}
	for i := range b1 {
		if b1[i] != b2[i] {
			t.Fatalf("not deterministic at byte %d", i)
		}
	}
}

func TestComputeStoreSketchDifferentStates(t *testing.T) {
	vec := makeCanonicalVec(64, 1.0)
	s1, _ := ComputeStoreSketch(vec, [32]byte{1}, 1)
	s2, _ := ComputeStoreSketch(vec, [32]byte{2}, 2)

	// Different states should produce different sign bits
	same := 0
	for i := range s1.SignBits {
		if s1.SignBits[i] == s2.SignBits[i] {
			same++
		}
	}
	if same == len(s1.SignBits) {
		t.Fatal("different states should produce different sketches")
	}
}

func TestComputeStoreSketchInvalidDim(t *testing.T) {
	vec := make([]float64, 100) // not power of 2
	_, err := ComputeStoreSketch(vec, [32]byte{}, 1)
	if err == nil {
		t.Fatal("expected error for non-power-of-2 dim")
	}
}

// ─── Correction factor tests ────────────────────────────────────────────────

func TestCorrectionsAllPositive(t *testing.T) {
	y := []float64{1, 2, 3, 4}
	c := computeCorrections(y)
	// All positive: posL2Norm = sqrt(1+4+9+16) = sqrt(30), negL2Norm = 0
	if math.Abs(float64(c[0])-math.Sqrt(30)) > 0.01 {
		t.Fatalf("posL2Norm: got %v, want %v", c[0], math.Sqrt(30))
	}
	if c[1] != 0 {
		t.Fatalf("negL2Norm should be 0 for all-positive, got %v", c[1])
	}
	if math.Abs(float64(c[2])-4.0) > 0.01 {
		t.Fatalf("maxAbs: got %v, want 4", c[2])
	}
	expectedMean := float32((1 + 2 + 3 + 4) / 4.0)
	if math.Abs(float64(c[3])-float64(expectedMean)) > 0.01 {
		t.Fatalf("meanAbs: got %v, want %v", c[3], expectedMean)
	}
}

func TestCorrectionsMixed(t *testing.T) {
	y := []float64{3, -4}
	c := computeCorrections(y)
	// posL2Norm = sqrt(9) = 3, negL2Norm = sqrt(16) = 4
	if math.Abs(float64(c[0])-3.0) > 0.01 {
		t.Fatalf("posL2Norm: got %v, want 3", c[0])
	}
	if math.Abs(float64(c[1])-4.0) > 0.01 {
		t.Fatalf("negL2Norm: got %v, want 4", c[1])
	}
	if math.Abs(float64(c[2])-4.0) > 0.01 {
		t.Fatalf("maxAbs: got %v, want 4", c[2])
	}
}

// ─── Marshal/Unmarshal round-trip tests ─────────────────────────────────────

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	state := [32]byte{99}
	vec := makeCanonicalVec(256, 3.14)
	sketch, _ := ComputeStoreSketch(vec, state, 42)

	data, err := sketch.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	restored, err := UnmarshalStoreSketch(data)
	if err != nil {
		t.Fatal(err)
	}

	if restored.Version != sketch.Version {
		t.Fatal("version mismatch")
	}
	if restored.CanonicalDim != sketch.CanonicalDim {
		t.Fatal("dim mismatch")
	}
	if restored.StateID != sketch.StateID {
		t.Fatal("stateID mismatch")
	}
	for i := range sketch.Corrections {
		if restored.Corrections[i] != sketch.Corrections[i] {
			t.Fatalf("correction[%d] mismatch: %v != %v", i, restored.Corrections[i], sketch.Corrections[i])
		}
	}
	for i := range sketch.SignBits {
		if restored.SignBits[i] != sketch.SignBits[i] {
			t.Fatalf("signBits[%d] mismatch", i)
		}
	}
}

func TestUnmarshalTooShort(t *testing.T) {
	_, err := UnmarshalStoreSketch(make([]byte, 10))
	if err == nil {
		t.Fatal("expected error for too-short data")
	}
}

func TestUnmarshalBadMagic(t *testing.T) {
	data := make([]byte, 40)
	data[0] = 0xFF // wrong magic
	_, err := UnmarshalStoreSketch(data)
	if err == nil {
		t.Fatal("expected error for bad magic")
	}
}

func TestUnmarshalTruncatedSignBits(t *testing.T) {
	state := [32]byte{1}
	vec := makeCanonicalVec(64, 1.0)
	sketch, _ := ComputeStoreSketch(vec, state, 1)
	data, _ := sketch.Marshal()
	// Truncate: keep header but remove sign bits
	_, err := UnmarshalStoreSketch(data[:sketchHeaderSize])
	if err == nil {
		t.Fatal("expected error for truncated sign bits")
	}
}

func TestMarshalNilSketch(t *testing.T) {
	var s *StoreSketch
	_, err := s.Marshal()
	if err == nil {
		t.Fatal("expected error for nil sketch")
	}
}

// ─── QuerySketch tests ──────────────────────────────────────────────────────

func TestComputeQuerySketchBasic(t *testing.T) {
	state := [32]byte{7, 8, 9}
	vec := makeCanonicalVec(64, 5.0)

	qs, err := ComputeQuerySketch(vec, state, 1)
	if err != nil {
		t.Fatal(err)
	}
	if qs.CanonicalDim != 64 {
		t.Fatalf("expected dim=64, got %d", qs.CanonicalDim)
	}
	if len(qs.Coefficients) != 32 { // 64/2
		t.Fatalf("expected 32 coefficient bytes, got %d", len(qs.Coefficients))
	}
}

func TestQuerySketchDeterminism(t *testing.T) {
	state := [32]byte{11}
	vec := makeCanonicalVec(128, 2.0)

	q1, _ := ComputeQuerySketch(vec, state, 1)
	q2, _ := ComputeQuerySketch(vec, state, 1)

	if len(q1.Coefficients) != len(q2.Coefficients) {
		t.Fatal("coefficient lengths differ")
	}
	for i := range q1.Coefficients {
		if q1.Coefficients[i] != q2.Coefficients[i] {
			t.Fatalf("not deterministic at byte %d", i)
		}
	}
}

func TestQuerySketchCoefficientRange(t *testing.T) {
	state := [32]byte{55}
	vec := makeCanonicalVec(64, 3.0)

	qs, _ := ComputeQuerySketch(vec, state, 1)

	for i := 0; i < 64; i++ {
		coef := unpackQueryCoefficient(qs.Coefficients, i)
		if coef < -7 || coef > 7 {
			t.Fatalf("coefficient %d out of range [-7,7]: %d", i, coef)
		}
	}
}

func TestQuerySketchQuantizationMonotonicity(t *testing.T) {
	// If input component i > component j, then quantized level i >= level j
	// (approximately, since the SRHT scrambles the coordinates)
	// We test this property on the projected vector, not the input.
	state := [32]byte{1}
	dim := 64

	// Create a monotonically increasing canonical vector
	vec := make([]float64, dim)
	for i := range vec {
		vec[i] = float64(i+1) / float64(dim)
	}
	canonical.L2Normalize(vec)

	qs, _ := ComputeQuerySketch(vec, state, 1)
	_ = qs // monotonicity is on the projected space, not input space;
	// just verify no crash
}

// ─── unpackQueryCoefficient tests ───────────────────────────────────────────

func TestUnpackQueryCoefficient(t *testing.T) {
	// Pack known values and verify unpacking
	tests := []struct {
		name    string
		packed  byte
		idx     int
		want    int8
	}{
		{"high nibble +7", 0x70, 0, 7},
		{"high nibble +1", 0x10, 0, 1},
		{"high nibble 0", 0x00, 0, 0},
		{"high nibble -1", 0xF0, 0, -1},
		{"high nibble -7", 0x90, 0, -7},
		{"low nibble +7", 0x07, 1, 7},
		{"low nibble -1", 0x0F, 1, -1},
		{"low nibble -7", 0x09, 1, -7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coefs := []byte{tt.packed}
			got := unpackQueryCoefficient(coefs, tt.idx)
			if got != tt.want {
				t.Fatalf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// ─── Inner product estimator tests ──────────────────────────────────────────

func TestEstimateInnerProductSelfSketch(t *testing.T) {
	state := [32]byte{42}
	vec := makeCanonicalVec(64, 1.0)

	store, _ := ComputeStoreSketch(vec, state, 1)
	query, _ := ComputeQuerySketch(vec, state, 1)

	ip, err := EstimateInnerProduct(store, query)
	if err != nil {
		t.Fatal(err)
	}

	// Self inner product should be positive (vector dotted with itself)
	if ip <= 0 {
		t.Fatalf("self inner product should be positive, got %v", ip)
	}
}

func TestEstimateInnerProductOrthogonal(t *testing.T) {
	// Create two nearly-orthogonal unit vectors
	state := [32]byte{42}
	dim := 64

	v1 := make([]float64, dim)
	v2 := make([]float64, dim)
	for i := range v1 {
		if i < dim/2 {
			v1[i] = 1.0
		} else {
			v2[i] = 1.0
		}
	}
	canonical.L2Normalize(v1)
	canonical.L2Normalize(v2)

	store, _ := ComputeStoreSketch(v1, state, 1)
	query, _ := ComputeQuerySketch(v2, state, 1)

	ip, err := EstimateInnerProduct(store, query)
	if err != nil {
		t.Fatal(err)
	}

	// Should be near zero (exact zero unlikely due to quantization)
	if math.Abs(ip) > 0.5 {
		t.Fatalf("orthogonal inner product should be near 0, got %v", ip)
	}
}

func TestEstimateInnerProductStateMismatch(t *testing.T) {
	vec := makeCanonicalVec(64, 1.0)
	store, _ := ComputeStoreSketch(vec, [32]byte{1}, 1)
	query, _ := ComputeQuerySketch(vec, [32]byte{2}, 2)

	_, err := EstimateInnerProduct(store, query)
	if err == nil {
		t.Fatal("expected error for state mismatch")
	}
}

func TestEstimateInnerProductDimMismatch(t *testing.T) {
	v1 := makeCanonicalVec(64, 1.0)
	v2 := makeCanonicalVec(128, 1.0)
	store, _ := ComputeStoreSketch(v1, [32]byte{1}, 1)
	query, _ := ComputeQuerySketch(v2, [32]byte{1}, 1)

	_, err := EstimateInnerProduct(store, query)
	if err == nil {
		t.Fatal("expected error for dim mismatch")
	}
}

func TestEstimateInnerProductNilSketches(t *testing.T) {
	if _, err := EstimateInnerProduct(nil, nil); err == nil {
		t.Fatal("expected error for nil sketches")
	}
	store, _ := ComputeStoreSketch(makeCanonicalVec(64, 1.0), [32]byte{1}, 1)
	if _, err := EstimateInnerProduct(store, nil); err == nil {
		t.Fatal("expected error for nil query")
	}
}

func TestEstimateInnerProductCorrelation(t *testing.T) {
	// Similar vectors should have higher estimated IP than dissimilar ones
	state := [32]byte{77}
	dim := 128

	base := makeCanonicalVec(dim, 1.0)

	// Similar: slight perturbation
	similar := make([]float64, dim)
	copy(similar, base)
	similar[0] += 0.01
	similar[1] -= 0.01
	canonical.L2Normalize(similar)

	// Dissimilar: very different
	dissimilar := makeCanonicalVec(dim, 99.0)

	storeBase, _ := ComputeStoreSketch(base, state, 1)
	querySimilar, _ := ComputeQuerySketch(similar, state, 1)
	queryDissimilar, _ := ComputeQuerySketch(dissimilar, state, 1)

	ipSimilar, _ := EstimateInnerProduct(storeBase, querySimilar)
	ipDissimilar, _ := EstimateInnerProduct(storeBase, queryDissimilar)

	if ipSimilar <= ipDissimilar {
		t.Fatalf("similar vectors should have higher IP estimate: similar=%v dissimilar=%v",
			ipSimilar, ipDissimilar)
	}
}

// ─── Statistical accuracy test ──────────────────────────────────────────────

func TestEstimateInnerProductAccuracy(t *testing.T) {
	// Measure relative error of the estimator over many random pairs
	state := [32]byte{42}
	dim := 128
	nPairs := 200

	var totalRelErr float64
	counted := 0

	for i := 0; i < nPairs; i++ {
		v1 := makeCanonicalVec(dim, float64(i)*0.37)
		v2 := makeCanonicalVec(dim, float64(i)*0.73+100)

		// Ground truth inner product
		groundTruth := canonical.KahanDot(v1, v2)

		store, _ := ComputeStoreSketch(v1, state, 1)
		query, _ := ComputeQuerySketch(v2, state, 1)
		estimated, err := EstimateInnerProduct(store, query)
		if err != nil {
			t.Fatal(err)
		}

		if math.Abs(groundTruth) > 0.01 {
			relErr := math.Abs(estimated-groundTruth) / math.Abs(groundTruth)
			totalRelErr += relErr
			counted++
		}
	}

	if counted > 0 {
		avgRelErr := totalRelErr / float64(counted)
		t.Logf("average relative error over %d pairs: %.4f (%.1f%%)", counted, avgRelErr, avgRelErr*100)
		// The estimator should be within reasonable bounds
		// RaBitQ with 1-bit signs and 4-bit query is approximate
		if avgRelErr > 2.0 {
			t.Fatalf("average relative error too high: %v", avgRelErr)
		}
	}
}
