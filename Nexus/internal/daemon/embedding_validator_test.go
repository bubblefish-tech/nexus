// Copyright © 2026 Shawn Sammartano. All rights reserved.
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

package daemon_test

import (
	"testing"

	"github.com/BubbleFish-Nexus/internal/daemon"
)

func TestEmbeddingValidator_ShapeCheck(t *testing.T) {
	t.Helper()
	v := daemon.NewEmbeddingValidator(4, 10, 3.0)

	// Correct dimension passes.
	result := v.Validate("id1", "src", "openai", "hello", make([]float32, 4))
	if result.Err != nil {
		t.Errorf("correct dim: got error %v", result.Err)
	}

	// Wrong dimension fails.
	result = v.Validate("id2", "src", "openai", "hello", make([]float32, 8))
	if result.Err == nil {
		t.Error("wrong dim: expected error, got nil")
	}
}

func TestEmbeddingValidator_ContentHash(t *testing.T) {
	t.Helper()
	v := daemon.NewEmbeddingValidator(0, 10, 3.0)

	result := v.Validate("id1", "src", "openai", "hello world", make([]float32, 4))
	if result.Err != nil {
		t.Fatalf("validate: %v", result.Err)
	}
	if result.ContentHash == "" {
		t.Error("ContentHash should be set")
	}

	// Same content → same hash.
	result2 := v.Validate("id2", "src", "openai", "hello world", make([]float32, 4))
	if result.ContentHash != result2.ContentHash {
		t.Errorf("same content: different hashes: %q vs %q", result.ContentHash, result2.ContentHash)
	}

	// Different content → different hash.
	result3 := v.Validate("id3", "src", "openai", "different content", make([]float32, 4))
	if result.ContentHash == result3.ContentHash {
		t.Error("different content: same hashes (collision)")
	}
}

func TestEmbeddingValidator_WarmupNoDriftAlarm(t *testing.T) {
	t.Helper()
	warmup := 5
	v := daemon.NewEmbeddingValidator(0, warmup, 3.0)

	// Fill the warmup window with normal embeddings (norm ≈ 1.0).
	normal := []float32{1.0, 0.0, 0.0, 0.0}
	for i := 0; i < warmup; i++ {
		id := string(rune('a' + i))
		r := v.Validate(id, "src", "openai", "text", normal)
		if r.Quarantined {
			t.Errorf("warmup[%d]: should not quarantine during warmup", i)
		}
	}

	// Send a wildly anomalous embedding (very large norm). During warmup,
	// no alarm should fire.
	anomalous := []float32{1000.0, 1000.0, 1000.0, 1000.0}
	r := v.Validate("anomaly-during-warmup", "src", "openai", "text", anomalous)
	if r.Quarantined {
		t.Error("warmup: anomaly during warmup should not trigger quarantine")
	}
}

func TestEmbeddingValidator_DriftDetectionAfterWarmup(t *testing.T) {
	t.Helper()
	warmup := 20
	v := daemon.NewEmbeddingValidator(0, warmup, 3.0)

	// Fill warmup with unit-norm embeddings.
	normal := []float32{1.0, 0.0, 0.0, 0.0} // norm = 1.0
	for i := 0; i < warmup; i++ {
		id := string(rune('A' + i))
		v.Validate(id, "src", "openai", "text", normal)
	}

	// After warmup, a massively anomalous embedding (norm >> baseline mean)
	// should trigger quarantine.
	anomalous := []float32{1000.0, 1000.0, 1000.0, 1000.0} // norm ≈ 2000
	r := v.Validate("anomaly-after-warmup", "src", "openai", "text", anomalous)
	if !r.Quarantined {
		t.Error("post-warmup anomaly: expected quarantine, got none")
	}
	if r.QuarantineReason == "" {
		t.Error("quarantine reason should be set")
	}
}

func TestEmbeddingValidator_QuarantinedIDsTracked(t *testing.T) {
	t.Helper()
	warmup := 20
	v := daemon.NewEmbeddingValidator(0, warmup, 3.0)

	normal := []float32{1.0, 0.0}
	for i := 0; i < warmup; i++ {
		id := string(rune('a' + i))
		v.Validate(id, "src", "openai", "text", normal)
	}

	anomalous := []float32{1000.0, 1000.0}
	v.Validate("quarantined-id", "src", "openai", "text", anomalous)

	ids := v.QuarantinedIDs()
	found := false
	for _, id := range ids {
		if id == "quarantined-id" {
			found = true
		}
	}
	if !found {
		t.Error("quarantined ID not in QuarantinedIDs()")
	}

	// Non-quarantined ID should not appear.
	_, ok := v.QuarantineRecord("normal-id")
	if ok {
		t.Error("normal id should not have quarantine record")
	}

	rec, ok := v.QuarantineRecord("quarantined-id")
	if !ok {
		t.Error("quarantined-id should have quarantine record")
	}
	if rec == nil {
		t.Error("quarantine record is nil")
	}
}

func TestEmbeddingValidator_PerProviderBaseline(t *testing.T) {
	t.Helper()
	warmup := 20
	v := daemon.NewEmbeddingValidator(0, warmup, 3.0)

	// Provider A baseline established with normal embeddings.
	normalA := []float32{1.0, 0.0, 0.0, 0.0}
	for i := 0; i < warmup; i++ {
		id := string(rune('a' + i))
		v.Validate(id, "src", "openai", "text", normalA)
	}

	// Provider B is NEW — its first embedding should not trigger alarm
	// even if it looks anomalous relative to provider A's baseline.
	// (Provider B starts fresh with its own 1000-embedding warmup.)
	anomalous := []float32{1000.0, 1000.0, 1000.0, 1000.0}
	r := v.Validate("provider-b-first", "src", "anthropic", "text", anomalous)
	if r.Quarantined {
		t.Error("new provider first embedding should not trigger quarantine (fresh baseline)")
	}
}
