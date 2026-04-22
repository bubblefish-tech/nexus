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

package tabs

import (
	"testing"
)

func TestControlTab_Name(t *testing.T) {
	t.Helper()
	tab := NewControlTab()
	if tab.Name() != "Overview" {
		t.Fatalf("expected 'Overview', got %q", tab.Name())
	}
}

func TestAuditTab_Name(t *testing.T) {
	t.Helper()
	tab := NewAuditTab()
	if tab.Name() != "Audit" {
		t.Fatalf("expected 'Audit', got %q", tab.Name())
	}
}

func TestSecurityTab_Name(t *testing.T) {
	t.Helper()
	tab := NewSecurityTab()
	if tab.Name() != "Security" {
		t.Fatalf("expected 'Security', got %q", tab.Name())
	}
}

func TestPipelineTab_Name(t *testing.T) {
	t.Helper()
	tab := NewPipelineTab()
	if tab.Name() != "Pipeline" {
		t.Fatalf("expected 'Pipeline', got %q", tab.Name())
	}
}

func TestConflictsTab_Name(t *testing.T) {
	t.Helper()
	tab := NewConflictsTab()
	if tab.Name() != "Conflicts" {
		t.Fatalf("expected 'Conflicts', got %q", tab.Name())
	}
}

func TestTimeTravelTab_Name(t *testing.T) {
	t.Helper()
	tab := NewTimeTravelTab()
	if tab.Name() != "Time-Travel" {
		t.Fatalf("expected 'Time-Travel', got %q", tab.Name())
	}
}

func TestSettingsTab_Name(t *testing.T) {
	t.Helper()
	tab := NewSettingsTab()
	if tab.Name() != "Settings" {
		t.Fatalf("expected 'Settings', got %q", tab.Name())
	}
}

func TestAllTabs_ViewNonEmpty(t *testing.T) {
	t.Helper()
	tabs := []Tab{
		NewControlTab(),
		NewAuditTab(),
		NewSecurityTab(),
		NewPipelineTab(),
		NewConflictsTab(),
		NewTimeTravelTab(),
		NewSettingsTab(),
	}
	for _, tab := range tabs {
		v := tab.View(80, 24)
		if v == "" {
			t.Errorf("%s View returned empty string", tab.Name())
		}
	}
}

func TestAllTabs_InitNoPanic(t *testing.T) {
	t.Helper()
	tabs := []Tab{
		NewControlTab(),
		NewAuditTab(),
		NewSecurityTab(),
		NewPipelineTab(),
		NewConflictsTab(),
		NewTimeTravelTab(),
		NewSettingsTab(),
	}
	for _, tab := range tabs {
		_ = tab.Init()
	}
}
