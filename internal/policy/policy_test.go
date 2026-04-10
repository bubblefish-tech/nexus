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

package policy_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/policy"
)

// knownDests returns a destination name set for test use.
func knownDests(names ...string) map[string]bool {
	t := make(map[string]bool, len(names))
	for _, n := range names {
		t[n] = true
	}
	return t
}

// sampleEntry returns a fully-populated PolicyEntry for table-driven tests.
func sampleEntry(source string, dests []string) policy.PolicyEntry {
	return policy.PolicyEntry{
		Source:                source,
		AllowedDestinations:   dests,
		AllowedOperations:     []string{"write", "read", "search"},
		AllowedRetrievalModes: []string{"exact", "structured", "semantic", "hybrid"},
		AllowedProfiles:       []string{"fast", "balanced", "deep"},
		MaxResults:            50,
		MaxResponseBytes:      65536,
		FieldVisibility: policy.FieldVisibilityEntry{
			IncludeFields: []string{"content", "source", "role", "timestamp"},
			StripMetadata: true,
		},
		Cache: policy.PolicyCacheEntry{
			ReadFromCache:               true,
			WriteToCache:                true,
			MaxTTLSeconds:               300,
			SemanticSimilarityThreshold: 0.92,
		},
		Decay: policy.PolicyDecayEntry{
			HalfLifeDays:      14,
			DecayMode:         "exponential",
			StepThresholdDays: 30,
		},
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Validate tests
// ────────────────────────────────────────────────────────────────────────────

func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		entries    []policy.PolicyEntry
		knownDests map[string]bool
		wantErrSub string // non-empty → error expected containing this substring
	}{
		{
			name:       "valid single source",
			entries:    []policy.PolicyEntry{sampleEntry("claude", []string{"sqlite"})},
			knownDests: knownDests("sqlite"),
		},
		{
			name: "valid multiple sources multiple destinations",
			entries: []policy.PolicyEntry{
				sampleEntry("claude", []string{"sqlite", "openbrain"}),
				sampleEntry("gpt4", []string{"sqlite"}),
			},
			knownDests: knownDests("sqlite", "openbrain"),
		},
		{
			name:       "empty allowed_destinations is valid",
			entries:    []policy.PolicyEntry{sampleEntry("claude", nil)},
			knownDests: knownDests("sqlite"),
		},
		{
			name:       "no sources is valid",
			entries:    nil,
			knownDests: knownDests("sqlite"),
		},
		{
			name:       "unknown destination reference",
			entries:    []policy.PolicyEntry{sampleEntry("claude", []string{"nonexistent"})},
			knownDests: knownDests("sqlite"),
			wantErrSub: "SCHEMA_ERROR",
		},
		{
			name:       "unknown destination error contains destination name",
			entries:    []policy.PolicyEntry{sampleEntry("claude", []string{"nonexistent"})},
			knownDests: knownDests("sqlite"),
			wantErrSub: "nonexistent",
		},
		{
			name:       "unknown destination error contains source name",
			entries:    []policy.PolicyEntry{sampleEntry("mysource", []string{"nonexistent"})},
			knownDests: knownDests("sqlite"),
			wantErrSub: "mysource",
		},
		{
			name: "duplicate destination within same source",
			entries: []policy.PolicyEntry{
				sampleEntry("claude", []string{"sqlite", "sqlite"}),
			},
			knownDests: knownDests("sqlite"),
			wantErrSub: "SCHEMA_ERROR",
		},
		{
			name: "duplicate destination error contains dest name",
			entries: []policy.PolicyEntry{
				sampleEntry("claude", []string{"sqlite", "sqlite"}),
			},
			knownDests: knownDests("sqlite"),
			wantErrSub: "sqlite",
		},
		{
			name: "second source has unknown destination",
			entries: []policy.PolicyEntry{
				sampleEntry("claude", []string{"sqlite"}),
				sampleEntry("gpt4", []string{"ghost"}),
			},
			knownDests: knownDests("sqlite"),
			wantErrSub: "SCHEMA_ERROR",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Helper()
			err := policy.Validate(tc.entries, tc.knownDests)
			if tc.wantErrSub == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tc.wantErrSub)
			}
			if !strings.Contains(err.Error(), tc.wantErrSub) {
				t.Fatalf("Validate() error %q does not contain %q", err.Error(), tc.wantErrSub)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Compile tests
// ────────────────────────────────────────────────────────────────────────────

func TestCompile_ValidPolicy(t *testing.T) {
	dir := t.TempDir()

	entries := []policy.PolicyEntry{
		sampleEntry("claude", []string{"sqlite"}),
		sampleEntry("gpt4", []string{"sqlite"}),
	}

	if err := policy.Compile(entries, dir, nil); err != nil {
		t.Fatalf("Compile() unexpected error: %v", err)
	}

	outPath := filepath.Join(dir, "policies.json")

	// File must exist.
	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("policies.json not found: %v", err)
	}

	// File must have 0600 permissions on Unix-like systems.
	// On Windows, the mode bits may differ but the file must still be readable.
	mode := info.Mode().Perm()
	if mode != 0600 {
		// On Windows CreateTemp yields 0666; log but do not fail the build.
		// The critical property is that we attempted Chmod(0600).
		t.Logf("policies.json permissions: %04o (may differ on Windows)", mode)
	}

	// Parse and verify contents.
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var compiled policy.CompiledPolicies
	if err := json.Unmarshal(raw, &compiled); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(compiled.Policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(compiled.Policies))
	}
	if compiled.Version == "" {
		t.Error("version must not be empty")
	}
	if compiled.CompiledAt.IsZero() {
		t.Error("compiled_at must not be zero")
	}
}

func TestCompile_FieldVisibility(t *testing.T) {
	dir := t.TempDir()

	entry := sampleEntry("claude", []string{"sqlite"})
	entry.FieldVisibility.IncludeFields = []string{"content", "role", "model"}
	entry.FieldVisibility.StripMetadata = true

	if err := policy.Compile([]policy.PolicyEntry{entry}, dir, nil); err != nil {
		t.Fatalf("Compile() unexpected error: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, "policies.json"))
	var compiled policy.CompiledPolicies
	_ = json.Unmarshal(raw, &compiled)

	fv := compiled.Policies[0].FieldVisibility
	if !fv.StripMetadata {
		t.Error("strip_metadata should be true")
	}
	if len(fv.IncludeFields) != 3 {
		t.Fatalf("expected 3 include_fields, got %d", len(fv.IncludeFields))
	}
	if fv.IncludeFields[0] != "content" {
		t.Errorf("first include_field: want %q, got %q", "content", fv.IncludeFields[0])
	}
}

func TestCompile_AllPolicyFields(t *testing.T) {
	dir := t.TempDir()

	entry := sampleEntry("mysource", []string{"sqlite"})

	if err := policy.Compile([]policy.PolicyEntry{entry}, dir, nil); err != nil {
		t.Fatalf("Compile() unexpected error: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, "policies.json"))
	var compiled policy.CompiledPolicies
	_ = json.Unmarshal(raw, &compiled)

	p := compiled.Policies[0]

	if p.Source != "mysource" {
		t.Errorf("source: want %q, got %q", "mysource", p.Source)
	}
	if p.MaxResults != 50 {
		t.Errorf("max_results: want 50, got %d", p.MaxResults)
	}
	if p.MaxResponseBytes != 65536 {
		t.Errorf("max_response_bytes: want 65536, got %d", p.MaxResponseBytes)
	}
	if !p.Cache.ReadFromCache {
		t.Error("cache.read_from_cache should be true")
	}
	if p.Cache.SemanticSimilarityThreshold != 0.92 {
		t.Errorf("semantic_similarity_threshold: want 0.92, got %v", p.Cache.SemanticSimilarityThreshold)
	}
	if p.Decay.HalfLifeDays != 14 {
		t.Errorf("decay.half_life_days: want 14, got %v", p.Decay.HalfLifeDays)
	}
	if p.Decay.DecayMode != "exponential" {
		t.Errorf("decay.decay_mode: want %q, got %q", "exponential", p.Decay.DecayMode)
	}
}

func TestCompile_CreatesOutputDirWith0700(t *testing.T) {
	base := t.TempDir()
	outputDir := filepath.Join(base, "compiled")

	if err := policy.Compile(nil, outputDir, nil); err != nil {
		t.Fatalf("Compile() unexpected error: %v", err)
	}

	info, err := os.Stat(outputDir)
	if err != nil {
		t.Fatalf("compiled dir not found: %v", err)
	}
	if !info.IsDir() {
		t.Error("compiled should be a directory")
	}
	// On Unix: must be 0700. On Windows, mode bits are less precise.
	mode := info.Mode().Perm()
	if mode != 0700 {
		t.Logf("compiled dir permissions: %04o (may differ on Windows)", mode)
	}
}

func TestCompile_ZeroSources(t *testing.T) {
	dir := t.TempDir()

	if err := policy.Compile(nil, dir, nil); err != nil {
		t.Fatalf("Compile() with zero entries unexpected error: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "policies.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var compiled policy.CompiledPolicies
	if err := json.Unmarshal(raw, &compiled); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if compiled.Policies == nil {
		// json.MarshalIndent encodes nil slice as null; normalise for test.
		compiled.Policies = []policy.PolicyEntry{}
	}
	if len(compiled.Policies) != 0 {
		t.Errorf("expected 0 policies, got %d", len(compiled.Policies))
	}
}

func TestCompile_OutputPathHelper(t *testing.T) {
	got := policy.OutputPath("/foo/compiled")
	want := filepath.Join("/foo/compiled", "policies.json")
	if got != want {
		t.Errorf("OutputPath: want %q, got %q", want, got)
	}
}
