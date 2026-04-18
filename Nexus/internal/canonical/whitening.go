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
	"sync"
)

// WhiteningState holds the per-source online variance estimate for
// diagonal whitening. Updated via Welford's algorithm for numerical
// stability.
//
// v0.1.3 uses diagonal-only variance whitening:
//
//	x[i] = (x[i] - mean[i]) / sqrt(var[i])
//
// Full top-k PCA whitening is deferred to v0.2.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.2 Step 3.
type WhiteningState struct {
	dim             int
	sampleCount     int
	mean            []float64 // running mean per dimension
	m2              []float64 // sum of squared deviations per dimension (Welford)
	warmupThreshold int
	mu              sync.RWMutex
}

// NewWhiteningState creates a whitening state for the given dimension.
// Whitening does not engage until sampleCount >= warmupThreshold.
func NewWhiteningState(dim, warmupThreshold int) *WhiteningState {
	return &WhiteningState{
		dim:             dim,
		warmupThreshold: warmupThreshold,
		mean:            make([]float64, dim),
		m2:              make([]float64, dim),
	}
}

// Update incorporates a new sample via Welford's online algorithm.
// Thread-safe.
func (w *WhiteningState) Update(x []float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sampleCount++
	n := float64(w.sampleCount)
	for i := range x {
		if i >= w.dim {
			break
		}
		delta := x[i] - w.mean[i]
		w.mean[i] += delta / n
		delta2 := x[i] - w.mean[i]
		w.m2[i] += delta * delta2
	}
}

// Apply whitens a vector using the current variance estimate.
// Below warmup threshold, copies input to output unchanged.
// Thread-safe for concurrent reads.
func (w *WhiteningState) Apply(x, output []float64) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.sampleCount < w.warmupThreshold {
		copy(output, x)
		return
	}
	n := float64(w.sampleCount)
	for i := range x {
		if i >= w.dim {
			break
		}
		variance := w.m2[i] / n
		if variance < 1e-12 {
			// Near-zero variance: pass through to avoid division by zero.
			output[i] = x[i] - w.mean[i]
		} else {
			output[i] = (x[i] - w.mean[i]) / math.Sqrt(variance)
		}
	}
}

// SampleCount returns the number of samples seen. Thread-safe.
func (w *WhiteningState) SampleCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.sampleCount
}

// Mean returns a copy of the current running mean. Thread-safe.
func (w *WhiteningState) Mean() []float64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]float64, w.dim)
	copy(out, w.mean)
	return out
}

// Variance returns a copy of the current per-dimension variance. Thread-safe.
func (w *WhiteningState) Variance() []float64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]float64, w.dim)
	if w.sampleCount < 2 {
		return out
	}
	n := float64(w.sampleCount)
	for i := range out {
		out[i] = w.m2[i] / n
	}
	return out
}

// MarshalState returns the whitening state as serializable fields.
func (w *WhiteningState) MarshalState() (sampleCount int, mean, m2 []float64) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	meanCopy := make([]float64, w.dim)
	m2Copy := make([]float64, w.dim)
	copy(meanCopy, w.mean)
	copy(m2Copy, w.m2)
	return w.sampleCount, meanCopy, m2Copy
}

// RestoreState sets the whitening state from previously saved values.
func (w *WhiteningState) RestoreState(sampleCount int, mean, m2 []float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sampleCount = sampleCount
	copy(w.mean, mean)
	copy(w.m2, m2)
}
