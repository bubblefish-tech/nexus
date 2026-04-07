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

package firewall

import (
	"log/slog"
	"testing"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/destination"
)

// testFirewall creates a RetrievalFirewall with standard tier_order for tests.
func testFirewall(t *testing.T) *RetrievalFirewall {
	t.Helper()
	return New(config.DaemonRetrievalFirewallConfig{
		Enabled:     true,
		TierOrder:   []string{"public", "internal", "confidential", "restricted"},
		DefaultTier: "public",
	}, slog.Default())
}

// testSource creates a config.Source with the given firewall policy.
func testSource(t *testing.T, fwCfg config.SourceRetrievalFirewallConfig) *config.Source {
	t.Helper()
	return &config.Source{
		Name:    "test-source",
		CanRead: true,
		Policy: config.SourcePolicyConfig{
			RetrievalFirewall: fwCfg,
		},
	}
}

func makeRecords(labels [][]string, tiers []string, namespaces []string) []destination.TranslatedPayload {
	records := make([]destination.TranslatedPayload, len(labels))
	for i := range records {
		records[i] = destination.TranslatedPayload{
			PayloadID:         "pay-" + time.Now().Format("150405") + "-" + string(rune('a'+i)),
			SensitivityLabels: labels[i],
		}
		if i < len(tiers) {
			records[i].ClassificationTier = tiers[i]
		}
		if i < len(namespaces) {
			records[i].Namespace = namespaces[i]
		}
	}
	return records
}

// ── PreQuery tests ──────────────────────────────────────────────────────────

func TestPreQuery_Disabled(t *testing.T) {
	fw := New(config.DaemonRetrievalFirewallConfig{Enabled: false}, slog.Default())
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		MaxClassificationTier: "internal",
	})
	if denial := fw.PreQuery(src, "anything"); denial != nil {
		t.Fatalf("expected nil denial when disabled, got %v", denial)
	}
}

func TestPreQuery_UnknownMaxTier(t *testing.T) {
	fw := testFirewall(t)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		MaxClassificationTier: "top-secret",
	})
	denial := fw.PreQuery(src, "")
	if denial == nil {
		t.Fatal("expected denial for unknown max tier")
	}
	if denial.Code != "retrieval_firewall_denied" {
		t.Errorf("code = %q, want retrieval_firewall_denied", denial.Code)
	}
	if denial.Reason != "tier_exceeds_maximum" {
		t.Errorf("reason = %q, want tier_exceeds_maximum", denial.Reason)
	}
}

func TestPreQuery_ValidMaxTier(t *testing.T) {
	fw := testFirewall(t)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		MaxClassificationTier: "confidential",
	})
	if denial := fw.PreQuery(src, ""); denial != nil {
		t.Fatalf("expected nil denial for valid tier, got %v", denial)
	}
}

func TestPreQuery_NamespaceBlocked(t *testing.T) {
	fw := testFirewall(t)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		VisibleNamespaces: []string{"shared", "claude"},
	})
	denial := fw.PreQuery(src, "private")
	if denial == nil {
		t.Fatal("expected denial for namespace not in visible_namespaces")
	}
	if denial.Code != "retrieval_firewall_denied" {
		t.Errorf("code = %q, want retrieval_firewall_denied", denial.Code)
	}
}

func TestPreQuery_NamespaceAllowed(t *testing.T) {
	fw := testFirewall(t)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		VisibleNamespaces: []string{"shared", "claude"},
	})
	if denial := fw.PreQuery(src, "shared"); denial != nil {
		t.Fatalf("expected nil denial for visible namespace, got %v", denial)
	}
}

func TestPreQuery_CrossNamespaceRead(t *testing.T) {
	fw := testFirewall(t)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		VisibleNamespaces:  []string{"shared"},
		CrossNamespaceRead: true,
	})
	if denial := fw.PreQuery(src, "private"); denial != nil {
		t.Fatalf("expected nil denial with cross_namespace_read=true, got %v", denial)
	}
}

func TestPreQuery_EmptyNamespacePassesNamespaceCheck(t *testing.T) {
	fw := testFirewall(t)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		VisibleNamespaces: []string{"shared"},
	})
	// Empty namespace = no namespace filter on query → skip namespace check.
	if denial := fw.PreQuery(src, ""); denial != nil {
		t.Fatalf("expected nil denial for empty namespace, got %v", denial)
	}
}

// ── PostFilter tests ────────────────────────────────────────────────────────

func TestPostFilter_Disabled(t *testing.T) {
	fw := New(config.DaemonRetrievalFirewallConfig{Enabled: false}, slog.Default())
	records := makeRecords([][]string{{"pii"}}, []string{"restricted"}, nil)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		BlockedLabels:         []string{"pii"},
		MaxClassificationTier: "public",
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 1 {
		t.Fatalf("disabled firewall should pass all records, got %d", len(result.Records))
	}
	if result.Filtered {
		t.Error("Filtered should be false when disabled")
	}
}

func TestPostFilter_EmptyRecords(t *testing.T) {
	fw := testFirewall(t)
	src := testSource(t, config.SourceRetrievalFirewallConfig{})
	result := fw.PostFilter(src, nil)
	if result.Filtered {
		t.Error("should not be filtered on empty input")
	}
	if result.CountRemaining != 0 {
		t.Errorf("CountRemaining = %d, want 0", result.CountRemaining)
	}
}

func TestPostFilter_BlockedLabels(t *testing.T) {
	fw := testFirewall(t)
	records := makeRecords(
		[][]string{{"pii", "financial"}, {"financial"}, {"public-data"}},
		[]string{"public", "public", "public"},
		nil,
	)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		BlockedLabels: []string{"pii"},
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 2 {
		t.Fatalf("expected 2 records (pii blocked), got %d", len(result.Records))
	}
	if !result.Filtered {
		t.Error("Filtered should be true")
	}
	if result.CountRemoved != 1 {
		t.Errorf("CountRemoved = %d, want 1", result.CountRemoved)
	}
}

func TestPostFilter_BlockedLabelsAbsolute_NoAdminBypass(t *testing.T) {
	// INVARIANT: blocked_labels are ABSOLUTE. Even "admin" sources must respect them.
	fw := testFirewall(t)
	records := makeRecords(
		[][]string{{"executive", "trade-secret"}},
		[]string{"public"},
		nil,
	)
	// Simulate an admin-like source — blocked_labels still apply.
	src := &config.Source{
		Name:    "admin-source",
		CanRead: true,
		Policy: config.SourcePolicyConfig{
			RetrievalFirewall: config.SourceRetrievalFirewallConfig{
				BlockedLabels: []string{"executive", "trade-secret"},
			},
		},
	}
	result := fw.PostFilter(src, records)
	if len(result.Records) != 0 {
		t.Fatalf("blocked_labels must be absolute — expected 0 records, got %d", len(result.Records))
	}
}

func TestPostFilter_MaxClassificationTier(t *testing.T) {
	fw := testFirewall(t)
	records := makeRecords(
		[][]string{{}, {}, {}, {}},
		[]string{"public", "internal", "confidential", "restricted"},
		nil,
	)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		MaxClassificationTier: "internal",
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 2 {
		t.Fatalf("expected 2 records (public + internal), got %d", len(result.Records))
	}
	if !result.TierFiltered {
		t.Error("TierFiltered should be true")
	}
}

func TestPostFilter_MaxTier_EmptyTierDefaultsPublic(t *testing.T) {
	fw := testFirewall(t)
	// Record with empty tier should default to "public".
	records := makeRecords([][]string{{}}, []string{""}, nil)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		MaxClassificationTier: "public",
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 1 {
		t.Fatalf("empty tier should default to public and pass, got %d records", len(result.Records))
	}
}

func TestPostFilter_MaxTier_UnknownMemoryTierBlocked(t *testing.T) {
	fw := testFirewall(t)
	records := makeRecords([][]string{{}}, []string{"top-secret"}, nil)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		MaxClassificationTier: "restricted",
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 0 {
		t.Fatalf("unknown memory tier should be blocked, got %d records", len(result.Records))
	}
}

func TestPostFilter_RequiredLabels(t *testing.T) {
	fw := testFirewall(t)
	records := makeRecords(
		[][]string{{"financial", "approved"}, {"financial"}, {"approved"}},
		[]string{"public", "public", "public"},
		nil,
	)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		RequiredLabels: []string{"financial", "approved"},
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record with both required labels, got %d", len(result.Records))
	}
}

func TestPostFilter_VisibleNamespaces(t *testing.T) {
	fw := testFirewall(t)
	records := makeRecords(
		[][]string{{}, {}, {}},
		[]string{"public", "public", "public"},
		[]string{"shared", "private", "claude"},
	)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		VisibleNamespaces: []string{"shared"},
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record in 'shared' namespace, got %d", len(result.Records))
	}
	if result.Records[0].Namespace != "shared" {
		t.Errorf("expected namespace 'shared', got %q", result.Records[0].Namespace)
	}
}

func TestPostFilter_CrossNamespaceRead(t *testing.T) {
	fw := testFirewall(t)
	records := makeRecords(
		[][]string{{}, {}, {}},
		[]string{"public", "public", "public"},
		[]string{"shared", "private", "claude"},
	)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		VisibleNamespaces:  []string{"shared"},
		CrossNamespaceRead: true,
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 3 {
		t.Fatalf("cross_namespace_read=true should pass all namespaces, got %d", len(result.Records))
	}
}

func TestPostFilter_AllFiltered(t *testing.T) {
	fw := testFirewall(t)
	records := makeRecords(
		[][]string{{"pii"}, {"pii"}},
		[]string{"public", "public"},
		nil,
	)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		BlockedLabels: []string{"pii"},
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 0 {
		t.Fatalf("expected 0 records, got %d", len(result.Records))
	}
	if !result.Filtered {
		t.Error("Filtered should be true when all removed")
	}
	if result.CountRemoved != 2 {
		t.Errorf("CountRemoved = %d, want 2", result.CountRemoved)
	}
	if result.CountRemaining != 0 {
		t.Errorf("CountRemaining = %d, want 0", result.CountRemaining)
	}
}

func TestPostFilter_CombinedRules(t *testing.T) {
	// Test all rules applied simultaneously.
	fw := testFirewall(t)
	records := makeRecords(
		[][]string{
			{"pii"},                   // blocked by blocked_labels
			{},                        // blocked by tier (confidential > internal)
			{"financial", "approved"}, // passes all checks
			{"approved"},              // blocked by required_labels (missing "financial")
			{"financial", "approved"}, // blocked by namespace
		},
		[]string{"public", "confidential", "internal", "public", "public"},
		[]string{"shared", "shared", "shared", "shared", "private"},
	)
	src := testSource(t, config.SourceRetrievalFirewallConfig{
		BlockedLabels:         []string{"pii"},
		MaxClassificationTier: "internal",
		RequiredLabels:        []string{"financial", "approved"},
		VisibleNamespaces:     []string{"shared"},
	})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record passing all rules, got %d", len(result.Records))
	}
}

func TestPostFilter_NoFirewallConfig_AllPass(t *testing.T) {
	// When source has no retrieval_firewall config → all memories visible.
	fw := testFirewall(t)
	records := makeRecords(
		[][]string{{"pii"}, {"executive"}, {}},
		[]string{"restricted", "confidential", "public"},
		nil,
	)
	src := testSource(t, config.SourceRetrievalFirewallConfig{})
	result := fw.PostFilter(src, records)
	if len(result.Records) != 3 {
		t.Fatalf("no firewall config should pass all, got %d", len(result.Records))
	}
	if result.Filtered {
		t.Error("Filtered should be false")
	}
}

// ── Benchmark ───────────────────────────────────────────────────────────────

func BenchmarkPostFilter_1000Records(b *testing.B) {
	fw := New(config.DaemonRetrievalFirewallConfig{
		Enabled:     true,
		TierOrder:   []string{"public", "internal", "confidential", "restricted"},
		DefaultTier: "public",
	}, slog.Default())

	records := make([]destination.TranslatedPayload, 1000)
	tiers := []string{"public", "internal", "confidential", "restricted"}
	for i := range records {
		records[i] = destination.TranslatedPayload{
			PayloadID:          "pay-bench-" + string(rune(i)),
			ClassificationTier: tiers[i%4],
			Namespace:          "shared",
		}
		if i%3 == 0 {
			records[i].SensitivityLabels = []string{"financial"}
		}
		if i%7 == 0 {
			records[i].SensitivityLabels = append(records[i].SensitivityLabels, "pii")
		}
	}

	src := &config.Source{
		Name:    "bench-source",
		CanRead: true,
		Policy: config.SourcePolicyConfig{
			RetrievalFirewall: config.SourceRetrievalFirewallConfig{
				BlockedLabels:         []string{"executive"},
				MaxClassificationTier: "confidential",
				VisibleNamespaces:     []string{"shared"},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fw.PostFilter(src, records)
	}
}

// TestPostFilter_1000Records_Correctness verifies PostFilter output
// correctness on a 1000-record input. No timing assertions — use
// BenchmarkPostFilter_1000Records for performance measurement.
func TestPostFilter_1000Records_Correctness(t *testing.T) {
	fw := New(config.DaemonRetrievalFirewallConfig{
		Enabled:     true,
		TierOrder:   []string{"public", "internal", "confidential", "restricted"},
		DefaultTier: "public",
	}, slog.Default())

	records := make([]destination.TranslatedPayload, 1000)
	tiers := []string{"public", "internal", "confidential", "restricted"}
	for i := range records {
		records[i] = destination.TranslatedPayload{
			PayloadID:          "pay-perf",
			ClassificationTier: tiers[i%4],
			Namespace:          "shared",
		}
		if i%5 == 0 {
			records[i].SensitivityLabels = []string{"pii"}
		}
	}

	src := &config.Source{
		Name:    "perf-source",
		CanRead: true,
		Policy: config.SourcePolicyConfig{
			RetrievalFirewall: config.SourceRetrievalFirewallConfig{
				BlockedLabels:         []string{"pii"},
				MaxClassificationTier: "confidential",
				VisibleNamespaces:     []string{"shared"},
			},
		},
	}

	result := fw.PostFilter(src, records)

	// "restricted" tier records are removed (250 of 1000).
	// "pii" blocked-label records are removed (200 of 1000, every 5th).
	// Some overlap: every 20th record is both restricted AND pii-labeled.
	// Expected kept: records that are NOT restricted AND NOT pii-labeled.
	expectedRemoved := 0
	for i := 0; i < 1000; i++ {
		isRestricted := tiers[i%4] == "restricted"
		hasPII := i%5 == 0
		if isRestricted || hasPII {
			expectedRemoved++
		}
	}
	expectedKept := 1000 - expectedRemoved

	if result.CountRemaining != expectedKept {
		t.Errorf("CountRemaining = %d, want %d", result.CountRemaining, expectedKept)
	}
	if result.CountRemoved != expectedRemoved {
		t.Errorf("CountRemoved = %d, want %d", result.CountRemoved, expectedRemoved)
	}
	if !result.Filtered {
		t.Error("Filtered should be true")
	}
	if !result.TierFiltered {
		t.Error("TierFiltered should be true (restricted records removed)")
	}
	if len(result.FilteredLabels) == 0 {
		t.Error("FilteredLabels should contain 'pii'")
	}

	// Verify no remaining record has pii label or restricted tier.
	for _, rec := range result.Records {
		if rec.ClassificationTier == "restricted" {
			t.Error("restricted-tier record should not appear in results")
			break
		}
		for _, label := range rec.SensitivityLabels {
			if label == "pii" {
				t.Error("pii-labeled record should not appear in results")
				break
			}
		}
	}
}

// ── Edge cases ──────────────────────────────────────────────────────────────

func TestPreQueryDenial_Error(t *testing.T) {
	d := &PreQueryDenial{Code: "test_code", Reason: "test reason"}
	want := "test_code: test reason"
	if got := d.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestTierIndex(t *testing.T) {
	fw := testFirewall(t)
	tests := []struct {
		tier string
		want int
	}{
		{"public", 0},
		{"internal", 1},
		{"confidential", 2},
		{"restricted", 3},
		{"unknown", -1},
	}
	for _, tt := range tests {
		if got := fw.TierIndex(tt.tier); got != tt.want {
			t.Errorf("TierIndex(%q) = %d, want %d", tt.tier, got, tt.want)
		}
	}
}

// TestFirewall_PrecomputedSets verifies that PostFilter uses precomputed
// sets from SourceRetrievalFirewallConfig when available, and produces
// identical results to the non-cached path.
func TestFirewall_PrecomputedSets(t *testing.T) {
	fw := testFirewall(t)

	records := []destination.TranslatedPayload{
		{PayloadID: "1", Namespace: "shared", ClassificationTier: "public", SensitivityLabels: []string{"pii"}},
		{PayloadID: "2", Namespace: "shared", ClassificationTier: "internal"},
		{PayloadID: "3", Namespace: "shared", ClassificationTier: "confidential", SensitivityLabels: []string{"financial"}},
		{PayloadID: "4", Namespace: "other", ClassificationTier: "public"},
	}

	// Source WITHOUT precomputed sets (tests that construct sources directly).
	srcNoCached := &config.Source{
		Name: "no-cache", CanRead: true,
		Policy: config.SourcePolicyConfig{
			RetrievalFirewall: config.SourceRetrievalFirewallConfig{
				BlockedLabels:         []string{"pii"},
				MaxClassificationTier: "internal",
				VisibleNamespaces:     []string{"shared"},
			},
		},
	}
	resultNoCached := fw.PostFilter(srcNoCached, records)

	// Source WITH precomputed sets (as config/loader.go produces).
	srcCached := &config.Source{
		Name: "cached", CanRead: true,
		Policy: config.SourcePolicyConfig{
			RetrievalFirewall: config.SourceRetrievalFirewallConfig{
				BlockedLabels:         []string{"pii"},
				MaxClassificationTier: "internal",
				VisibleNamespaces:     []string{"shared"},
				BlockedLabelsSet:      map[string]struct{}{"pii": {}},
				RequiredLabelsSet:     map[string]struct{}{},
				VisibleNamespacesSet:  map[string]struct{}{"shared": {}},
			},
		},
	}
	resultCached := fw.PostFilter(srcCached, records)

	if resultNoCached.CountRemaining != resultCached.CountRemaining {
		t.Errorf("CountRemaining mismatch: no-cache=%d cached=%d",
			resultNoCached.CountRemaining, resultCached.CountRemaining)
	}
	if resultNoCached.CountRemoved != resultCached.CountRemoved {
		t.Errorf("CountRemoved mismatch: no-cache=%d cached=%d",
			resultNoCached.CountRemoved, resultCached.CountRemoved)
	}
	if len(resultCached.Records) != 1 {
		t.Fatalf("expected 1 record (internal+shared, no pii), got %d", len(resultCached.Records))
	}
	if resultCached.Records[0].PayloadID != "2" {
		t.Errorf("expected PayloadID=2, got %s", resultCached.Records[0].PayloadID)
	}
}
