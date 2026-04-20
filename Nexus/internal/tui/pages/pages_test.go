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

package pages

import (
	"testing"

	"github.com/bubblefish-tech/nexus/internal/discover"
	tea "github.com/charmbracelet/bubbletea"
)

// ---- WelcomePage ----

func TestWelcomePage_Name(t *testing.T) {
	t.Helper()
	p := NewWelcomePage()
	if p.Name() != "Welcome" {
		t.Fatalf("expected 'Welcome', got %q", p.Name())
	}
}

func TestWelcomePage_DefaultCannotAdvance(t *testing.T) {
	t.Helper()
	p := NewWelcomePage()
	state := &WizardState{}
	if p.CanAdvance(state) {
		t.Fatal("should not advance with empty mode")
	}
}

func TestWelcomePage_SelectModeCanAdvance(t *testing.T) {
	t.Helper()
	p := NewWelcomePage()
	state := &WizardState{}
	_, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}, state)
	if !p.CanAdvance(state) {
		t.Fatal("should advance after mode selected")
	}
}

func TestWelcomePage_ViewNonEmpty(t *testing.T) {
	t.Helper()
	p := NewWelcomePage()
	v := p.View(80, 24)
	if v == "" {
		t.Fatal("expected non-empty view")
	}
}

func TestWelcomePage_NavigateDown(t *testing.T) {
	t.Helper()
	p := NewWelcomePage()
	state := &WizardState{}
	_, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, state)
	if p.cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", p.cursor)
	}
}

// ---- ScanPage ----

func TestScanPage_Name(t *testing.T) {
	t.Helper()
	p := NewScanPage()
	if p.Name() != "Environment Scan" {
		t.Fatalf("expected 'Environment Scan', got %q", p.Name())
	}
}

func TestScanPage_CannotAdvanceBeforeScan(t *testing.T) {
	t.Helper()
	p := NewScanPage()
	state := &WizardState{}
	if p.CanAdvance(state) {
		t.Fatal("should not advance before scan completes")
	}
}

func TestScanPage_CanAdvanceAfterScan(t *testing.T) {
	t.Helper()
	p := NewScanPage()
	state := &WizardState{ScanComplete: true}
	if !p.CanAdvance(state) {
		t.Fatal("should advance after scan completes")
	}
}

func TestScanPage_ViewWithState(t *testing.T) {
	t.Helper()
	p := NewScanPage()
	state := &WizardState{ScanComplete: true}
	v := p.ViewWithState(80, 24, state)
	if v == "" {
		t.Fatal("expected non-empty view")
	}
}

// ---- FeaturesPage ----

func TestFeaturesPage_Name(t *testing.T) {
	t.Helper()
	p := NewFeaturesPage()
	if p.Name() != "Feature Selection" {
		t.Fatalf("expected 'Feature Selection', got %q", p.Name())
	}
}

func TestFeaturesPage_AlwaysCanAdvance(t *testing.T) {
	t.Helper()
	p := NewFeaturesPage()
	state := &WizardState{}
	if !p.CanAdvance(state) {
		t.Fatal("features page should always allow advance")
	}
}

func TestFeaturesPage_InitSetsDefaults(t *testing.T) {
	t.Helper()
	p := NewFeaturesPage()
	state := &WizardState{Mode: "simple"}
	_ = p.Init(state)
	if state.Features == nil {
		t.Fatal("expected Features map to be initialized")
	}
}

// ---- ToolsPage ----

func TestToolsPage_Name(t *testing.T) {
	t.Helper()
	p := NewToolsPage()
	if p.Name() != "Tool Selection" {
		t.Fatalf("expected 'Tool Selection', got %q", p.Name())
	}
}

func TestToolsPage_AlwaysCanAdvance(t *testing.T) {
	t.Helper()
	p := NewToolsPage()
	state := &WizardState{}
	if !p.CanAdvance(state) {
		t.Fatal("tools page should always allow advance")
	}
}

func TestToolsPage_ViewWithState_EmptyTools(t *testing.T) {
	t.Helper()
	p := NewToolsPage()
	state := &WizardState{}
	v := p.ViewWithState(80, 24, state)
	if v == "" {
		t.Fatal("expected non-empty view")
	}
}

func TestToolsPage_ViewWithState_WithTools(t *testing.T) {
	t.Helper()
	p := NewToolsPage()
	state := &WizardState{
		DiscoveredTools: []discover.DiscoveredTool{
			{Name: "Claude Code", Orchestratable: true},
		},
	}
	_ = p.Init(state)
	v := p.ViewWithState(80, 24, state)
	if v == "" {
		t.Fatal("expected non-empty view with tools")
	}
}

// ---- DatabasePage ----

func TestDatabasePage_Name(t *testing.T) {
	t.Helper()
	p := NewDatabasePage()
	if p.Name() != "Database Selection" {
		t.Fatalf("expected 'Database Selection', got %q", p.Name())
	}
}

func TestDatabasePage_SQLiteCanAdvance(t *testing.T) {
	t.Helper()
	p := NewDatabasePage()
	state := &WizardState{DatabaseType: "sqlite"}
	if !p.CanAdvance(state) {
		t.Fatal("sqlite should always allow advance (no DSN required)")
	}
}

func TestDatabasePage_PostgresRequiresDSN(t *testing.T) {
	t.Helper()
	p := NewDatabasePage()
	state := &WizardState{DatabaseType: "postgres", DatabaseDSN: ""}
	if p.CanAdvance(state) {
		t.Fatal("postgres without DSN should not allow advance")
	}
	state.DatabaseDSN = "postgres://localhost/db"
	if !p.CanAdvance(state) {
		t.Fatal("postgres with DSN should allow advance")
	}
}

func TestDatabasePage_ViewNonEmpty(t *testing.T) {
	t.Helper()
	p := NewDatabasePage()
	v := p.View(80, 24)
	if v == "" {
		t.Fatal("expected non-empty view")
	}
}

// ---- SecurityPage ----

func TestSecurityPage_Name(t *testing.T) {
	t.Helper()
	p := NewSecurityPage()
	if p.Name() != "Encryption" {
		t.Fatalf("expected 'Encryption', got %q", p.Name())
	}
}

func TestSecurityPage_CanAdvance_EmptyOptional(t *testing.T) {
	t.Helper()
	p := NewSecurityPage()
	state := &WizardState{EncryptionPass: ""}
	if !p.CanAdvance(state) {
		t.Fatal("empty password (optional) should allow advance")
	}
}

func TestSecurityPage_ViewNonEmpty(t *testing.T) {
	t.Helper()
	p := NewSecurityPage()
	v := p.View(80, 24)
	if v == "" {
		t.Fatal("expected non-empty view")
	}
}

// ---- TunnelPage ----

func TestTunnelPage_Name(t *testing.T) {
	t.Helper()
	p := NewTunnelPage()
	if p.Name() != "Tunnel Configuration" {
		t.Fatalf("expected 'Tunnel Configuration', got %q", p.Name())
	}
}

func TestTunnelPage_AlwaysCanAdvance(t *testing.T) {
	t.Helper()
	p := NewTunnelPage()
	state := &WizardState{}
	if !p.CanAdvance(state) {
		t.Fatal("tunnel page should always allow advance")
	}
}

func TestTunnelPage_ViewWithState(t *testing.T) {
	t.Helper()
	p := NewTunnelPage()
	state := &WizardState{}
	v := p.ViewWithState(80, 24, state)
	if v == "" {
		t.Fatal("expected non-empty view")
	}
}

// ---- DirectoryPage ----

func TestDirectoryPage_Name(t *testing.T) {
	t.Helper()
	p := NewDirectoryPage()
	if p.Name() != "Install Directory" {
		t.Fatalf("expected 'Install Directory', got %q", p.Name())
	}
}

func TestDirectoryPage_CanAdvanceAfterInit(t *testing.T) {
	t.Helper()
	p := NewDirectoryPage()
	state := &WizardState{}
	_ = p.Init(state)
	if !p.CanAdvance(state) {
		t.Fatal("directory page should allow advance after init sets InstallDir")
	}
}

func TestDirectoryPage_InitSetsInstallDir(t *testing.T) {
	t.Helper()
	p := NewDirectoryPage()
	state := &WizardState{}
	_ = p.Init(state)
	if state.InstallDir == "" {
		t.Fatal("expected InstallDir to be set after Init")
	}
}

// ---- SummaryPage ----

func TestSummaryPage_Name(t *testing.T) {
	t.Helper()
	p := NewSummaryPage()
	if p.Name() != "Summary" {
		t.Fatalf("expected 'Summary', got %q", p.Name())
	}
}

func TestSummaryPage_CannotAdvance(t *testing.T) {
	t.Helper()
	p := NewSummaryPage()
	state := &WizardState{}
	if p.CanAdvance(state) {
		t.Fatal("summary page should never allow wizard advance (it triggers install instead)")
	}
}

func TestSummaryPage_ViewWithState_NonEmpty(t *testing.T) {
	t.Helper()
	p := NewSummaryPage()
	state := &WizardState{
		Mode:         "simple",
		DatabaseType: "sqlite",
		InstallDir:   "/tmp/test",
	}
	v := p.ViewWithState(80, 24, state)
	if v == "" {
		t.Fatal("expected non-empty summary view")
	}
}
