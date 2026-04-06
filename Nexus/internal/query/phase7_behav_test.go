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

// Phase R-7 behavioral verification tests: Tiered Temporal Decay.
//
// These tests correspond to the verification gate checks in the
// State Verification Guide Phase R-7:
//
//	[ ] Per-collection decay overrides per-destination which overrides global.
//	[ ] Step mode: 29 days scores normally, 31 days scores 10%.
package query_test

import (
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
	query "github.com/BubbleFish-Nexus/internal/query"
)

// ---------------------------------------------------------------------------
// CHECK 1: Per-collection overrides per-destination which overrides global
// ---------------------------------------------------------------------------

// TestBehav_Phase7_Check1_CollectionOverridesDestOverridesGlobal verifies the
// three-tier decay precedence: global → per-destination → per-collection.
// The most specific configured tier wins.
//
// Reference: Tech Spec Section 3.6, Build Guide R-7 Behavioral Contract 1.
func TestBehav_Phase7_Check1_CollectionOverridesDestOverridesGlobal(t *testing.T) {
	global := config.RetrievalConfig{
		TimeDecay:    true,
		HalfLifeDays: 7,
		DecayMode:    "exponential",
	}
	destDecay := config.DestinationDecayConfig{
		HalfLifeDays: 14,
		Collections: map[string]config.CollectionDecayConfig{
			"chat_history": {
				HalfLifeDays: 3,
			},
		},
	}

	// Global only: HalfLifeDays = 7.
	cfgGlobal := query.ResolveDecay(global, config.DestinationDecayConfig{}, "", config.PolicyDecayConfig{}, "balanced")
	if cfgGlobal.HalfLifeDays != 7 {
		t.Errorf("global-only: HalfLifeDays = %g; want 7", cfgGlobal.HalfLifeDays)
	}

	// Per-destination overrides global: HalfLifeDays = 14.
	cfgDest := query.ResolveDecay(global, destDecay, "", config.PolicyDecayConfig{}, "balanced")
	if cfgDest.HalfLifeDays != 14 {
		t.Errorf("per-dest: HalfLifeDays = %g; want 14", cfgDest.HalfLifeDays)
	}

	// Per-collection overrides per-destination: HalfLifeDays = 3.
	cfgColl := query.ResolveDecay(global, destDecay, "chat_history", config.PolicyDecayConfig{}, "balanced")
	if cfgColl.HalfLifeDays != 3 {
		t.Errorf("per-collection: HalfLifeDays = %g; want 3", cfgColl.HalfLifeDays)
	}

	// Unknown collection falls back to per-destination.
	cfgUnknown := query.ResolveDecay(global, destDecay, "nonexistent", config.PolicyDecayConfig{}, "balanced")
	if cfgUnknown.HalfLifeDays != 14 {
		t.Errorf("unknown collection: HalfLifeDays = %g; want 14 (fallback to dest)", cfgUnknown.HalfLifeDays)
	}

	t.Logf("PASS: per-collection (3) overrides per-destination (14) overrides global (7)")
}

// TestBehav_Phase7_CollectionOverridesMode verifies that per-collection can
// override the decay mode independently of half-life.
func TestBehav_Phase7_CollectionOverridesMode(t *testing.T) {
	global := config.RetrievalConfig{
		TimeDecay:    true,
		HalfLifeDays: 7,
		DecayMode:    "exponential",
	}
	destDecay := config.DestinationDecayConfig{
		DecayMode: "exponential",
		Collections: map[string]config.CollectionDecayConfig{
			"ephemeral": {
				DecayMode:         "step",
				StepThresholdDays: 30,
			},
		},
	}

	cfg := query.ResolveDecay(global, destDecay, "ephemeral", config.PolicyDecayConfig{}, "balanced")
	if cfg.Mode != "step" {
		t.Errorf("mode = %q; want step (per-collection override)", cfg.Mode)
	}
	if cfg.StepThresholdDays != 30 {
		t.Errorf("StepThresholdDays = %g; want 30", cfg.StepThresholdDays)
	}
}

// TestBehav_Phase7_DestOverridesGlobal_StepMode verifies that per-destination
// can switch mode from exponential to step.
func TestBehav_Phase7_DestOverridesGlobal_StepMode(t *testing.T) {
	global := config.RetrievalConfig{
		TimeDecay: true,
		DecayMode: "exponential",
	}
	destDecay := config.DestinationDecayConfig{
		DecayMode:         "step",
		StepThresholdDays: 30,
	}

	cfg := query.ResolveDecay(global, destDecay, "", config.PolicyDecayConfig{}, "balanced")
	if cfg.Mode != "step" {
		t.Errorf("mode = %q; want step (per-dest override)", cfg.Mode)
	}
	if cfg.StepThresholdDays != 30 {
		t.Errorf("StepThresholdDays = %g; want 30", cfg.StepThresholdDays)
	}
}

// ---------------------------------------------------------------------------
// CHECK 2: Step mode — 29 days scores normally, 31 days scores 10%
// ---------------------------------------------------------------------------

// TestBehav_Phase7_Check2_StepMode_29vs31Days verifies the step-mode behavioral
// contract: a 29-day-old record scores normally (recency = 1.0), while a
// 31-day-old record receives a recency weight of 0.1 (drastically reduced).
//
// Uses HybridMerge with two records of identical cosine similarity to isolate
// the effect of step-mode decay at the 30-day threshold boundary.
//
// Reference: Tech Spec Section 3.6, Build Guide R-7 Verification Gate.
func TestBehav_Phase7_Check2_StepMode_29vs31Days(t *testing.T) {
	now := time.Now().UTC()

	cosSim := float32(0.9)

	below := destination.TranslatedPayload{
		PayloadID: "below-threshold",
		Content:   "29 days old — below step threshold",
		Timestamp: now.Add(-29 * 24 * time.Hour),
	}
	above := destination.TranslatedPayload{
		PayloadID: "above-threshold",
		Content:   "31 days old — above step threshold",
		Timestamp: now.Add(-31 * 24 * time.Hour),
	}

	decayCfg := query.DecayConfig{
		Enabled:           true,
		HalfLifeDays:      7,
		Mode:              "step",
		StepThresholdDays: 30,
	}

	stage3 := []destination.TranslatedPayload{above, below}
	stage4 := []destination.ScoredRecord{
		{Payload: above, Score: cosSim},
		{Payload: below, Score: cosSim},
	}

	merged := query.HybridMerge(stage3, stage4, 10, true, decayCfg, now)

	if len(merged) != 2 {
		t.Fatalf("expected 2 results, got %d", len(merged))
	}

	// The 29-day record must rank higher than the 31-day record.
	if merged[0].PayloadID != "below-threshold" {
		t.Errorf("top result = %q; want below-threshold (29d, normal score)", merged[0].PayloadID)
	}

	// Verify the score difference: below-threshold gets recency=1.0, above gets 0.1.
	// final_score(below) = 0.9*0.7 + 1.0*0.3 = 0.93
	// final_score(above) = 0.9*0.7 + 0.1*0.3 = 0.66
	belowScore := query.FinalScore(float64(cosSim), 29, decayCfg)
	aboveScore := query.FinalScore(float64(cosSim), 31, decayCfg)

	t.Logf("below-threshold (29d): final_score = %.4f", belowScore)
	t.Logf("above-threshold (31d): final_score = %.4f", aboveScore)

	if belowScore <= aboveScore {
		t.Errorf("29d score (%.4f) should be > 31d score (%.4f)", belowScore, aboveScore)
	}

	// The 31-day score's recency component should be 10% of normal (0.1 vs 1.0).
	recencyBelow := query.FinalScore(float64(cosSim), 29, decayCfg) - float64(cosSim)*0.7
	recencyAbove := query.FinalScore(float64(cosSim), 31, decayCfg) - float64(cosSim)*0.7
	ratio := recencyAbove / recencyBelow
	if ratio > 0.11 {
		t.Errorf("recency ratio = %.4f; want ~0.1 (step mode 10%% penalty)", ratio)
	}

	t.Logf("PASS: step mode — 29d scores normally (%.4f), 31d scores reduced (%.4f, recency 10%%)",
		belowScore, aboveScore)
}

// ---------------------------------------------------------------------------
// Determinism: same config + data = same ranking (Section 3.6 invariant)
// ---------------------------------------------------------------------------

// TestBehav_Phase7_Determinism_TieredConfig verifies that tiered decay
// resolution is deterministic: same config + data = same ranking.
//
// Reference: Tech Spec Section 3.6, Build Guide R-7 Behavioral Contract 5.
func TestBehav_Phase7_Determinism_TieredConfig(t *testing.T) {
	global := config.RetrievalConfig{
		TimeDecay:    true,
		HalfLifeDays: 7,
	}
	destDecay := config.DestinationDecayConfig{
		HalfLifeDays: 14,
		Collections: map[string]config.CollectionDecayConfig{
			"notes": {HalfLifeDays: 5},
		},
	}

	cfg1 := query.ResolveDecay(global, destDecay, "notes", config.PolicyDecayConfig{}, "balanced")
	cfg2 := query.ResolveDecay(global, destDecay, "notes", config.PolicyDecayConfig{}, "balanced")

	if cfg1 != cfg2 {
		t.Errorf("non-deterministic: cfg1 = %+v, cfg2 = %+v", cfg1, cfg2)
	}

	// Verify the resolved value is the per-collection override.
	if cfg1.HalfLifeDays != 5 {
		t.Errorf("HalfLifeDays = %g; want 5 (per-collection)", cfg1.HalfLifeDays)
	}
}

// ---------------------------------------------------------------------------
// resolveDecay returns (mode, halfLife, threshold) — contract check
// ---------------------------------------------------------------------------

// TestBehav_Phase7_ResolveDecay_ReturnsAllFields verifies that resolveDecay
// returns mode, halfLife, and threshold from the winning tier.
//
// Reference: Build Guide R-7 Behavioral Contract 4.
func TestBehav_Phase7_ResolveDecay_ReturnsAllFields(t *testing.T) {
	global := config.RetrievalConfig{
		TimeDecay: true,
	}
	destDecay := config.DestinationDecayConfig{
		Collections: map[string]config.CollectionDecayConfig{
			"chat": {
				HalfLifeDays:      2,
				DecayMode:         "step",
				StepThresholdDays: 24,
			},
		},
	}

	cfg := query.ResolveDecay(global, destDecay, "chat", config.PolicyDecayConfig{}, "balanced")

	if !cfg.Enabled {
		t.Fatal("expected decay enabled")
	}
	if cfg.Mode != "step" {
		t.Errorf("Mode = %q; want step", cfg.Mode)
	}
	if cfg.HalfLifeDays != 2 {
		t.Errorf("HalfLifeDays = %g; want 2", cfg.HalfLifeDays)
	}
	if cfg.StepThresholdDays != 24 {
		t.Errorf("StepThresholdDays = %g; want 24", cfg.StepThresholdDays)
	}
}

// TestBehav_Phase7_SourcePolicyOverridesCollection verifies that per-source
// policy decay takes highest precedence when set, even over per-collection.
func TestBehav_Phase7_SourcePolicyOverridesCollection(t *testing.T) {
	global := config.RetrievalConfig{
		TimeDecay:    true,
		HalfLifeDays: 7,
	}
	destDecay := config.DestinationDecayConfig{
		HalfLifeDays: 14,
		Collections: map[string]config.CollectionDecayConfig{
			"notes": {HalfLifeDays: 3},
		},
	}
	srcDecay := config.PolicyDecayConfig{
		HalfLifeDays: 1, // source policy override — most specific
	}

	cfg := query.ResolveDecay(global, destDecay, "notes", srcDecay, "balanced")
	if cfg.HalfLifeDays != 1 {
		t.Errorf("HalfLifeDays = %g; want 1 (source policy overrides collection)", cfg.HalfLifeDays)
	}
}

// TestBehav_Phase7_DestDecayImplicitlyEnables verifies that setting
// per-destination decay enables decay even when global TimeDecay = false.
func TestBehav_Phase7_DestDecayImplicitlyEnables(t *testing.T) {
	global := config.RetrievalConfig{TimeDecay: false}
	destDecay := config.DestinationDecayConfig{
		HalfLifeDays: 10,
	}

	cfg := query.ResolveDecay(global, destDecay, "", config.PolicyDecayConfig{}, "balanced")
	if !cfg.Enabled {
		t.Error("expected decay enabled by per-destination setting")
	}
	if cfg.HalfLifeDays != 10 {
		t.Errorf("HalfLifeDays = %g; want 10", cfg.HalfLifeDays)
	}
}

// TestBehav_Phase7_CollectionDecayImplicitlyEnables verifies that setting
// per-collection decay enables decay even when global and dest are unset.
func TestBehav_Phase7_CollectionDecayImplicitlyEnables(t *testing.T) {
	global := config.RetrievalConfig{TimeDecay: false}
	destDecay := config.DestinationDecayConfig{
		Collections: map[string]config.CollectionDecayConfig{
			"important": {HalfLifeDays: 60},
		},
	}

	cfg := query.ResolveDecay(global, destDecay, "important", config.PolicyDecayConfig{}, "balanced")
	if !cfg.Enabled {
		t.Error("expected decay enabled by per-collection setting")
	}
	if cfg.HalfLifeDays != 60 {
		t.Errorf("HalfLifeDays = %g; want 60", cfg.HalfLifeDays)
	}
}
