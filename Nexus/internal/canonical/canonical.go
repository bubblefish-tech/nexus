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

// Package canonical implements the pre-substrate canonicalization pipeline
// for BF-Sketch. Every embedding is transformed through dimension
// normalization (SRHT), L2 normalization, and per-source whitening before
// sketch computation.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 3.2.
package canonical

// Metadata holds information about a canonicalized vector.
type Metadata struct {
	// Source is the originating source name.
	Source string

	// OriginalDim is the dimension of the input embedding before projection.
	OriginalDim int

	// SampleCount is the number of samples seen for this source's whitening.
	SampleCount int

	// WhiteningActive indicates whether whitening was applied (sample count
	// exceeded the warmup threshold).
	WhiteningActive bool
}

// Manager orchestrates the five-step canonicalization pipeline.
// A nil Manager is safe to use; all methods return ErrDisabled.
//
// The full implementation is provided in BS.2. For BS.1, Manager is a
// stub that only tracks configuration state.
type Manager struct {
	cfg Config
}

// NewManager creates a Manager from the given config. Returns nil if
// canonical is disabled (nil is safe to use — all methods return ErrDisabled).
func NewManager(cfg Config) *Manager {
	if !cfg.Enabled {
		return nil
	}
	return &Manager{cfg: cfg}
}

// Enabled reports whether the canonicalization pipeline is active.
func (m *Manager) Enabled() bool {
	if m == nil {
		return false
	}
	return m.cfg.Enabled
}

// Canonicalize runs the five-step pipeline on a raw embedding.
// Stub implementation for BS.1; full implementation in BS.2.
func (m *Manager) Canonicalize(rawEmbedding []float64, source string) ([]float64, Metadata, error) {
	if m == nil {
		return nil, Metadata{}, ErrDisabled
	}
	return nil, Metadata{}, ErrDisabled
}

// CanonicalizeQuery is the query-path variant with caching.
// Stub implementation for BS.1; full implementation in BS.2.
func (m *Manager) CanonicalizeQuery(rawEmbedding []float64, source string) ([]float64, Metadata, error) {
	if m == nil {
		return nil, Metadata{}, ErrDisabled
	}
	return nil, Metadata{}, ErrDisabled
}

// Shutdown persists whitening state and releases resources.
// Safe to call on nil.
func (m *Manager) Shutdown() error {
	if m == nil {
		return nil
	}
	return nil
}
