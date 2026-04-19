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

package edition_test

import (
	"strings"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/edition"
)

func TestCommunityDefault(t *testing.T) {
	t.Helper()
	if edition.Current.Name != "community" {
		t.Fatalf("expected community, got %s", edition.Current.Name)
	}
	if len(edition.Current.Features) != 0 {
		t.Fatalf("community edition should have no features, got %v", edition.Current.Features)
	}
}

func TestHas(t *testing.T) {
	t.Helper()
	e := &edition.Edition{
		Name:     "enterprise",
		Features: []string{edition.FeatureRBAC, edition.FeatureLDAP},
	}
	tests := []struct {
		feature string
		want    bool
	}{
		{edition.FeatureRBAC, true},
		{edition.FeatureLDAP, true},
		{edition.FeatureSAML, false},
		{"unknown", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := e.Has(tc.feature); got != tc.want {
			t.Errorf("Has(%q) = %v, want %v", tc.feature, got, tc.want)
		}
	}
}

func TestHasCommunityHasNothing(t *testing.T) {
	t.Helper()
	features := []string{
		edition.FeatureRBAC, edition.FeatureLDAP, edition.FeatureSAML,
		edition.FeatureOIDC, edition.FeatureSplunkAudit, edition.FeatureHA,
		edition.FeatureMultiTenant, edition.FeatureMFA, edition.FeatureKeyLifecycle,
		edition.FeatureIncidentMgmt, edition.FeatureSessionMgmt, edition.FeatureCNSA2,
		edition.FeatureCAC, edition.FeatureLMSSigning, edition.FeatureClassMarking,
		edition.FeatureBoundaryEnforce, edition.FeatureMediaSanitize,
		edition.FeatureComplianceReport, edition.FeatureAirGapInstall, edition.FeatureFIPS,
	}
	for _, f := range features {
		if edition.Current.Has(f) {
			t.Errorf("community edition unexpectedly has feature %q", f)
		}
	}
}

func TestString(t *testing.T) {
	t.Helper()
	e := &edition.Edition{Name: "enterprise"}
	s := e.String()
	if !strings.HasPrefix(s, "nexus/") {
		t.Errorf("String() = %q, want prefix nexus/", s)
	}
	if !strings.Contains(s, "enterprise") {
		t.Errorf("String() = %q, want to contain edition name", s)
	}
}

func TestAllFeatureConstantsUnique(t *testing.T) {
	t.Helper()
	all := []string{
		edition.FeatureRBAC, edition.FeatureLDAP, edition.FeatureSAML,
		edition.FeatureOIDC, edition.FeatureSplunkAudit, edition.FeatureHA,
		edition.FeatureMultiTenant, edition.FeatureMFA, edition.FeatureKeyLifecycle,
		edition.FeatureIncidentMgmt, edition.FeatureSessionMgmt, edition.FeatureCNSA2,
		edition.FeatureCAC, edition.FeatureLMSSigning, edition.FeatureClassMarking,
		edition.FeatureBoundaryEnforce, edition.FeatureMediaSanitize,
		edition.FeatureComplianceReport, edition.FeatureAirGapInstall, edition.FeatureFIPS,
	}
	seen := make(map[string]bool)
	for _, f := range all {
		if seen[f] {
			t.Errorf("duplicate feature constant value %q", f)
		}
		seen[f] = true
	}
}
