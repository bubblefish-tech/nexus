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

package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPrefs_MissingFile_ReturnsNil(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	p, err := LoadPrefs(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Fatal("expected nil prefs for missing file")
	}
}

func TestLoadPrefs_ValidTOML(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	content := `
[sidebar]
sections = ["Health", "Daemon"]
hidden = ["Ports"]
`
	if err := os.WriteFile(filepath.Join(dir, "tui_prefs.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadPrefs(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil prefs")
	}
	if len(p.Sidebar.Sections) != 2 || p.Sidebar.Sections[0] != "Health" {
		t.Errorf("unexpected sections: %v", p.Sidebar.Sections)
	}
	if len(p.Sidebar.Hidden) != 1 || p.Sidebar.Hidden[0] != "Ports" {
		t.Errorf("unexpected hidden: %v", p.Sidebar.Hidden)
	}
}

func TestLoadPrefs_InvalidTOML_ReturnsError(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tui_prefs.toml"), []byte("[[[[bad"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadPrefs(dir)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestDefaultPrefs(t *testing.T) {
	t.Helper()
	p := DefaultPrefs()
	if p == nil {
		t.Fatal("expected non-nil default prefs")
	}
	if len(p.Sidebar.Sections) != len(defaultSectionOrder) {
		t.Errorf("expected %d sections, got %d", len(defaultSectionOrder), len(p.Sidebar.Sections))
	}
	for i, s := range defaultSectionOrder {
		if p.Sidebar.Sections[i] != s {
			t.Errorf("section[%d]: want %q, got %q", i, s, p.Sidebar.Sections[i])
		}
	}
}

func TestApplySidebarOrder_ReordersCorrectly(t *testing.T) {
	t.Helper()
	p := &TUIPrefs{
		Sidebar: SidebarPrefs{
			Sections: []string{"Health", "Daemon", "Ports"},
		},
	}
	available := []string{"Daemon", "Sources", "Destinations", "Ports", "Health"}
	result := p.ApplySidebarOrder(available)
	if len(result) != 5 {
		t.Fatalf("expected 5 sections, got %d: %v", len(result), result)
	}
	if result[0] != "Health" || result[1] != "Daemon" || result[2] != "Ports" {
		t.Errorf("wrong order: %v", result)
	}
	// unlisted available sections appended at end
	if result[3] != "Sources" || result[4] != "Destinations" {
		t.Errorf("trailing sections wrong: %v", result)
	}
}

func TestApplySidebarOrder_ExcludesHidden(t *testing.T) {
	t.Helper()
	p := &TUIPrefs{
		Sidebar: SidebarPrefs{
			Hidden: []string{"Ports", "Sources"},
		},
	}
	available := []string{"Daemon", "Sources", "Destinations", "Ports", "Health"}
	result := p.ApplySidebarOrder(available)
	for _, name := range result {
		if name == "Ports" || name == "Sources" {
			t.Errorf("hidden section %q appeared in result", name)
		}
	}
	if len(result) != 3 {
		t.Errorf("expected 3, got %d: %v", len(result), result)
	}
}

func TestApplySidebarOrder_NilPrefs_ReturnsAvailable(t *testing.T) {
	t.Helper()
	var p *TUIPrefs
	available := []string{"Daemon", "Sources"}
	result := p.ApplySidebarOrder(available)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}

func TestApplySidebarOrder_EmptySections_UsesAvailableOrder(t *testing.T) {
	t.Helper()
	p := &TUIPrefs{Sidebar: SidebarPrefs{}}
	available := []string{"Daemon", "Sources", "Destinations"}
	result := p.ApplySidebarOrder(available)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0] != "Daemon" || result[1] != "Sources" || result[2] != "Destinations" {
		t.Errorf("unexpected order: %v", result)
	}
}

func TestApplySidebarOrder_UnknownSectionsSkipped(t *testing.T) {
	t.Helper()
	p := &TUIPrefs{
		Sidebar: SidebarPrefs{
			Sections: []string{"Daemon", "Ghost", "Health"},
		},
	}
	available := []string{"Daemon", "Health"}
	result := p.ApplySidebarOrder(available)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d: %v", len(result), result)
	}
}
