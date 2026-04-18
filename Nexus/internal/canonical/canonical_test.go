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
	"errors"
	"math"
	"sync"
	"testing"
)

// ─── Config tests ───────────────────────────────────────────────────────────

func TestDefaultConfigIsDisabled(t *testing.T) {
	t.Helper()
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Fatal("expected disabled by default")
	}
	if cfg.CanonicalDim != 1024 {
		t.Fatalf("expected canonical_dim=1024, got %d", cfg.CanonicalDim)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
		want   error
	}{
		{"disabled always valid", func(c *Config) { c.Enabled = false; c.CanonicalDim = 3 }, nil},
		{"dim too small", func(c *Config) { c.Enabled = true; c.CanonicalDim = 32 }, ErrInvalidCanonicalDim},
		{"dim too large", func(c *Config) { c.Enabled = true; c.CanonicalDim = 16384 }, ErrInvalidCanonicalDim},
		{"dim not power of 2", func(c *Config) { c.Enabled = true; c.CanonicalDim = 1000 }, ErrCanonicalDimNotPowerOfTwo},
		{"warmup too small", func(c *Config) { c.Enabled = true; c.WhiteningWarmup = 50 }, ErrWhiteningWarmupTooSmall},
		{"valid enabled", func(c *Config) { c.Enabled = true }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

// ─── Manager nil-safety tests ───────────────────────────────────────────────

func TestManagerNilSafe(t *testing.T) {
	t.Helper()
	var m *Manager
	if m.Enabled() {
		t.Fatal("nil manager should report disabled")
	}
	_, _, err := m.Canonicalize([]float64{1, 2, 3}, "test")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
	_, _, err = m.CanonicalizeQuery([]float64{1, 2, 3}, "test")
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
	if err := m.Shutdown(); err != nil {
		t.Fatalf("nil shutdown should not error: %v", err)
	}
}

func TestManagerDisabledReturnsNil(t *testing.T) {
	t.Helper()
	cfg := DefaultConfig()
	m := NewManager(cfg)
	if m != nil {
		t.Fatal("disabled config should return nil manager")
	}
}

// ─── SRHT tests ─────────────────────────────────────────────────────────────

func TestSRHTSmallInput(t *testing.T) {
	seed := [32]byte{1, 2, 3, 4, 5}
	s, err := NewSRHT(seed, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	input := []float64{1, 2, 3, 4, 5, 6, 7, 8}
	output := make([]float64, 4)
	if err := s.Apply(input, output); err != nil {
		t.Fatal(err)
	}
	// Output should be non-zero
	allZero := true
	for _, v := range output {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("SRHT output is all zeros")
	}
}

func TestSRHTDeterminism(t *testing.T) {
	seed := [32]byte{42}
	s, err := NewSRHT(seed, 16, 8)
	if err != nil {
		t.Fatal(err)
	}
	input := []float64{1, 0, -1, 0.5, 2, -3, 0.1, -0.1, 0, 0, 0, 0, 0, 0, 0, 0}
	out1 := make([]float64, 8)
	out2 := make([]float64, 8)
	if err := s.Apply(input, out1); err != nil {
		t.Fatal(err)
	}
	// Create a new SRHT with the same seed
	s2, err := NewSRHT(seed, 16, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.Apply(input, out2); err != nil {
		t.Fatal(err)
	}
	for i := range out1 {
		if out1[i] != out2[i] {
			t.Fatalf("SRHT not deterministic at index %d: %v != %v", i, out1[i], out2[i])
		}
	}
}

func TestSRHTIdentitySubsample(t *testing.T) {
	// When inputDim == outputDim, subsample should be identity
	seed := [32]byte{99}
	s, err := NewSRHT(seed, 8, 8)
	if err != nil {
		t.Fatal(err)
	}
	for i, idx := range s.subsample {
		if idx != i {
			t.Fatalf("identity subsample broken at %d: got %d", i, idx)
		}
	}
}

func TestSRHTZeroPadding(t *testing.T) {
	seed := [32]byte{10}
	s, err := NewSRHT(seed, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	// Short input (only 3 elements, rest zero-padded)
	input := []float64{1, 2, 3}
	output := make([]float64, 4)
	if err := s.Apply(input, output); err != nil {
		t.Fatal(err)
	}
	allZero := true
	for _, v := range output {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("zero-padded input should produce non-zero output")
	}
}

func TestSRHTInvalidDimensions(t *testing.T) {
	seed := [32]byte{}
	if _, err := NewSRHT(seed, 7, 4); err == nil {
		t.Fatal("expected error for non-power-of-2 inputDim")
	}
	if _, err := NewSRHT(seed, 8, 16); err == nil {
		t.Fatal("expected error for outputDim > inputDim")
	}
	if _, err := NewSRHT(seed, 8, 0); err == nil {
		t.Fatal("expected error for outputDim=0")
	}
}

func TestSRHTNormPreservation(t *testing.T) {
	// The unitary SRHT should approximately preserve the L2 norm
	// (exactly when outputDim == inputDim, approximately when subsampled)
	seed := [32]byte{77}
	dim := 64
	s, err := NewSRHT(seed, dim, dim)
	if err != nil {
		t.Fatal(err)
	}
	input := make([]float64, dim)
	for i := range input {
		input[i] = float64(i+1) / float64(dim)
	}
	inputNorm := KahanL2Norm(input)

	output := make([]float64, dim)
	if err := s.Apply(input, output); err != nil {
		t.Fatal(err)
	}
	outputNorm := KahanL2Norm(output)

	ratio := outputNorm / inputNorm
	if ratio < 0.9 || ratio > 1.1 {
		t.Fatalf("norm not preserved: input=%v output=%v ratio=%v", inputNorm, outputNorm, ratio)
	}
}

// ─── L2 Normalization tests ─────────────────────────────────────────────────

func TestL2NormalizeZeroVector(t *testing.T) {
	v := []float64{0, 0, 0, 0}
	norm := L2Normalize(v)
	if norm != 0 {
		t.Fatalf("expected zero norm, got %v", norm)
	}
	for i, x := range v {
		if x != 0 {
			t.Fatalf("zero vector should be unchanged at %d: got %v", i, x)
		}
	}
}

func TestL2Normalize34(t *testing.T) {
	v := []float64{3, 4}
	norm := L2Normalize(v)
	if math.Abs(norm-5) > 1e-12 {
		t.Fatalf("expected norm=5, got %v", norm)
	}
	if math.Abs(v[0]-0.6) > 1e-12 {
		t.Fatalf("expected v[0]=0.6, got %v", v[0])
	}
	if math.Abs(v[1]-0.8) > 1e-12 {
		t.Fatalf("expected v[1]=0.8, got %v", v[1])
	}
}

func TestL2NormalizeUnitVector(t *testing.T) {
	v := []float64{1, 0, 0}
	norm := L2Normalize(v)
	if math.Abs(norm-1) > 1e-12 {
		t.Fatalf("expected norm=1, got %v", norm)
	}
	if math.Abs(v[0]-1) > 1e-12 || v[1] != 0 || v[2] != 0 {
		t.Fatalf("unit vector should be preserved: got %v", v)
	}
}

func TestL2NormalizeLargeVector(t *testing.T) {
	dim := 1024
	v := make([]float64, dim)
	for i := range v {
		v[i] = float64(i + 1)
	}
	L2Normalize(v)
	norm := KahanL2Norm(v)
	if math.Abs(norm-1.0) > 1e-10 {
		t.Fatalf("after normalization, norm should be ~1, got %v", norm)
	}
}

func TestKahanSumDeterminism(t *testing.T) {
	v := make([]float64, 10000)
	for i := range v {
		v[i] = 1e-8
	}
	s1 := KahanSum(v)
	s2 := KahanSum(v)
	if s1 != s2 {
		t.Fatalf("KahanSum not deterministic: %v != %v", s1, s2)
	}
	expected := 1e-4
	if math.Abs(s1-expected) > 1e-10 {
		t.Fatalf("KahanSum incorrect: got %v, want %v", s1, expected)
	}
}

// ─── Whitening tests ────────────────────────────────────────────────────────

func TestWhiteningBelowWarmupIsIdentity(t *testing.T) {
	ws := NewWhiteningState(4, 100)
	input := []float64{1, 2, 3, 4}
	output := make([]float64, 4)
	ws.Apply(input, output)
	for i := range input {
		if output[i] != input[i] {
			t.Fatalf("below warmup, output should equal input at %d: %v != %v", i, output[i], input[i])
		}
	}
}

func TestWhiteningAboveWarmupChangesOutput(t *testing.T) {
	dim := 4
	ws := NewWhiteningState(dim, 10)
	// Feed 100 samples with known distribution
	for i := 0; i < 100; i++ {
		sample := []float64{
			float64(i) * 0.1,
			float64(i) * 0.2,
			float64(i) * 0.3,
			float64(i) * 0.4,
		}
		ws.Update(sample)
	}
	input := []float64{5.0, 10.0, 15.0, 20.0}
	output := make([]float64, dim)
	ws.Apply(input, output)
	// Output should differ from input (mean-subtracted and variance-scaled)
	same := true
	for i := range input {
		if math.Abs(output[i]-input[i]) > 1e-6 {
			same = false
			break
		}
	}
	if same {
		t.Fatal("whitening above warmup should change the output")
	}
}

func TestWhiteningDeterminism(t *testing.T) {
	dim := 4
	ws1 := NewWhiteningState(dim, 5)
	ws2 := NewWhiteningState(dim, 5)
	// Feed same samples
	for i := 0; i < 20; i++ {
		s := []float64{float64(i), float64(i * 2), float64(i * 3), float64(i * 4)}
		ws1.Update(s)
		ws2.Update(s)
	}
	input := []float64{10, 20, 30, 40}
	out1 := make([]float64, dim)
	out2 := make([]float64, dim)
	ws1.Apply(input, out1)
	ws2.Apply(input, out2)
	for i := range out1 {
		if out1[i] != out2[i] {
			t.Fatalf("whitening not deterministic at %d: %v != %v", i, out1[i], out2[i])
		}
	}
}

func TestWhiteningConcurrency(t *testing.T) {
	dim := 4
	ws := NewWhiteningState(dim, 5)
	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s := []float64{float64(g*100 + i), float64(g), float64(i), 0}
				ws.Update(s)
			}
		}(g)
	}
	wg.Wait()
	if ws.SampleCount() != 1000 {
		t.Fatalf("expected 1000 samples, got %d", ws.SampleCount())
	}
}

func TestWhiteningMarshalRestore(t *testing.T) {
	dim := 4
	ws := NewWhiteningState(dim, 5)
	for i := 0; i < 20; i++ {
		ws.Update([]float64{float64(i), float64(i * 2), 0, 1})
	}
	sc, mean, m2 := ws.MarshalState()

	ws2 := NewWhiteningState(dim, 5)
	ws2.RestoreState(sc, mean, m2)

	input := []float64{5, 10, 0, 1}
	out1 := make([]float64, dim)
	out2 := make([]float64, dim)
	ws.Apply(input, out1)
	ws2.Apply(input, out2)
	for i := range out1 {
		if out1[i] != out2[i] {
			t.Fatalf("restored state differs at %d: %v != %v", i, out1[i], out2[i])
		}
	}
}

// ─── FWHT tests ─────────────────────────────────────────────────────────────

func TestFWHTSmall(t *testing.T) {
	// Known Walsh-Hadamard of [1,0,0,0] = [1,1,1,1]
	x := []float64{1, 0, 0, 0}
	fwhtInPlace(x)
	for i, v := range x {
		if v != 1 {
			t.Fatalf("fwht([1,0,0,0]) at %d: got %v, want 1", i, v)
		}
	}
}

func TestFWHTInverse(t *testing.T) {
	// FWHT is its own inverse up to a scale factor of n
	x := []float64{1, 2, 3, 4}
	orig := make([]float64, 4)
	copy(orig, x)

	fwhtInPlace(x)
	fwhtInPlace(x)
	// After two transforms, result should be n * original
	n := float64(len(x))
	for i := range x {
		expected := orig[i] * n
		if math.Abs(x[i]-expected) > 1e-10 {
			t.Fatalf("FWHT inverse at %d: got %v, want %v", i, x[i], expected)
		}
	}
}

// ─── NextPowerOfTwo tests ───────────────────────────────────────────────────

func TestNextPowerOfTwo(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{0, 1}, {1, 1}, {2, 2}, {3, 4}, {4, 4}, {5, 8},
		{1023, 1024}, {1024, 1024}, {1025, 2048},
	}
	for _, tt := range tests {
		got := NextPowerOfTwo(tt.in)
		if got != tt.want {
			t.Fatalf("NextPowerOfTwo(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// ─── SeededPRNG tests ───────────────────────────────────────────────────────

func TestSeededPRNGDeterminism(t *testing.T) {
	seed := [32]byte{1, 2, 3}
	p1 := newSeededPRNG(seed, []byte("test"))
	p2 := newSeededPRNG(seed, []byte("test"))
	for i := 0; i < 100; i++ {
		v1 := p1.Uint64()
		v2 := p2.Uint64()
		if v1 != v2 {
			t.Fatalf("PRNG not deterministic at step %d: %v != %v", i, v1, v2)
		}
	}
}

func TestSeededPRNGDifferentDomains(t *testing.T) {
	seed := [32]byte{1, 2, 3}
	p1 := newSeededPRNG(seed, []byte("domain-a"))
	p2 := newSeededPRNG(seed, []byte("domain-b"))
	same := 0
	for i := 0; i < 100; i++ {
		if p1.Uint64() == p2.Uint64() {
			same++
		}
	}
	if same > 5 {
		t.Fatal("different domains should produce different sequences")
	}
}
