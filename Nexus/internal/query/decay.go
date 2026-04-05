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

package query

import (
	"math"

	"github.com/BubbleFish-Nexus/internal/config"
)

// DecayMode constants for temporal decay reranking.
const (
	DecayModeExponential = "exponential"
	DecayModeStep        = "step"
)

// decayWeightCosSim is the weight applied to cosine similarity in the final
// score formula. The remaining (1 - decayWeightCosSim) is applied to recency.
//
// final_score = (cos_sim * 0.7) + (recency_weight * 0.3)
//
// Reference: Tech Spec Section 3.6.
const (
	decayWeightCosSim  = 0.7
	decayWeightRecency = 0.3
)

// defaultHalfLifeByProfile returns the default half-life in days for a given
// retrieval profile when no explicit config is set.
//
// Reference: Tech Spec Section 3.5 (profile table).
func defaultHalfLifeByProfile(profile string) float64 {
	switch profile {
	case "deep":
		return 30
	default: // balanced
		return 7
	}
}

// DecayConfig is the resolved temporal decay configuration for a single query.
// Produced by ResolveDecay; never mutated after construction.
//
// Reference: Tech Spec Section 3.6.
type DecayConfig struct {
	// Enabled is true when temporal decay reranking should be applied.
	Enabled bool
	// HalfLifeDays is the number of days after which a memory's recency weight
	// is halved. Must be > 0 when Enabled is true.
	HalfLifeDays float64
	// Mode is "exponential" (default) or "step".
	Mode string
	// StepThresholdDays is the cut-off for step mode: entries older than this
	// threshold receive a weight of 0.1. Only used when Mode = "step".
	StepThresholdDays float64
}

// ResolveDecay resolves the effective DecayConfig for a query by applying the
// tiered precedence rules defined in Tech Spec Section 3.6:
//
//  1. Global: [retrieval] section in daemon.toml (RetrievalConfig).
//  2. Per-source: [source.policy.decay] override (PolicyDecayConfig).
//
// The most specific non-zero value wins. A zero HalfLifeDays or empty Mode at
// a given tier falls through to the next tier.
//
// When no tier has TimeDecay enabled, Enabled = false and decay is skipped.
//
// Reference: Tech Spec Section 3.6.
func ResolveDecay(global config.RetrievalConfig, srcDecay config.PolicyDecayConfig, profile string) DecayConfig {
	// Start with global config.
	halfLife := global.HalfLifeDays
	mode := global.DecayMode
	stepThreshold := 0.0
	enabled := global.TimeDecay

	// Per-source override: non-zero HalfLifeDays or non-empty Mode wins.
	if srcDecay.HalfLifeDays > 0 {
		halfLife = srcDecay.HalfLifeDays
		enabled = true // per-source decay implicitly enables decay
	}
	if srcDecay.DecayMode != "" {
		mode = srcDecay.DecayMode
	}
	if srcDecay.StepThresholdDays > 0 {
		stepThreshold = srcDecay.StepThresholdDays
	}

	if !enabled {
		return DecayConfig{Enabled: false}
	}

	// Apply defaults when half-life is unset but decay is enabled.
	if halfLife <= 0 {
		halfLife = defaultHalfLifeByProfile(profile)
	}
	if mode == "" {
		mode = DecayModeExponential
	}

	return DecayConfig{
		Enabled:           true,
		HalfLifeDays:      halfLife,
		Mode:              mode,
		StepThresholdDays: stepThreshold,
	}
}

// computeRecencyWeight returns the recency weight for a memory that is
// daysElapsed days old, using the given decay configuration.
//
// Exponential mode (default):
//
//	lambda = ln(2) / half_life_days
//	weight = exp(-lambda * daysElapsed)
//
// Step mode:
//
//	weight = 1.0 if daysElapsed < StepThresholdDays, else 0.1
//
// daysElapsed is clamped to [0, ∞) before computation.
//
// Reference: Tech Spec Section 3.6.
func computeRecencyWeight(daysElapsed float64, cfg DecayConfig) float64 {
	if daysElapsed < 0 {
		daysElapsed = 0
	}
	switch cfg.Mode {
	case DecayModeStep:
		threshold := cfg.StepThresholdDays
		if threshold <= 0 {
			threshold = cfg.HalfLifeDays // sensible default when not set
		}
		if daysElapsed < threshold {
			return 1.0
		}
		return 0.1
	default: // exponential
		if cfg.HalfLifeDays <= 0 {
			return 1.0 // degenerate: no decay
		}
		lambda := math.Log(2) / cfg.HalfLifeDays
		return math.Exp(-lambda * daysElapsed)
	}
}

// FinalScore computes the temporal-decay-adjusted retrieval score.
//
//	final_score = (cos_sim * 0.7) + (recency_weight * 0.3)
//
// cosSim must be in [0, 1]. daysElapsed is the age of the memory in days.
//
// This function is deterministic: the same inputs always produce the same
// output. Reference: Tech Spec Section 3.6 (determinism invariant).
func FinalScore(cosSim, daysElapsed float64, cfg DecayConfig) float64 {
	recency := computeRecencyWeight(daysElapsed, cfg)
	return cosSim*decayWeightCosSim + recency*decayWeightRecency
}

// profileOverSample returns the default over-sample factor for a retrieval
// profile. Over-sampling retrieves more candidates than max_results so that
// temporal decay reranking has a larger pool to work with.
//
// Reference: Tech Spec Section 3.5 (profile table — Over-sample column).
func profileOverSample(profile string) int {
	switch profile {
	case "deep":
		return 500
	default: // balanced
		return 100
	}
}
