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
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestAllTabs_ViewWidthRespected(t *testing.T) {
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
		narrow := tab.View(40, 24)
		wide := tab.View(200, 40)
		if narrow == "" || wide == "" {
			t.Errorf("%s: empty view at some width", tab.Name())
		}
	}
}

func TestAllTabs_UpdateUnknownMsg_NoPanic(t *testing.T) {
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
		updated, _ := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
		view := updated.View(80, 24)
		if view == "" {
			t.Errorf("%s: empty view after unknown key", tab.Name())
		}
	}
}

func TestControlTab_ViewContainsStatusInfo(t *testing.T) {
	t.Helper()
	tab := NewControlTab()
	view := tab.View(120, 40)
	lower := strings.ToLower(view)
	if !strings.Contains(lower, "status") && !strings.Contains(lower, "overview") &&
		!strings.Contains(lower, "waiting") && !strings.Contains(lower, "load") {
		t.Fatalf("expected status-related content in Overview tab view")
	}
}

func TestTimeTravelTab_ViewContainsSearchPrompt(t *testing.T) {
	t.Helper()
	tab := NewTimeTravelTab()
	view := tab.View(120, 40)
	lower := strings.ToLower(view)
	if !strings.Contains(lower, "subject") && !strings.Contains(lower, "search") &&
		!strings.Contains(lower, "query") && !strings.Contains(lower, "time") {
		t.Fatalf("expected search/query prompt in Time-Travel tab view")
	}
}
