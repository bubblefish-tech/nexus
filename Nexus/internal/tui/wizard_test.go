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
	"testing"

	"github.com/bubblefish-tech/nexus/internal/tui/pages"
	tea "github.com/charmbracelet/bubbletea"
)

// stubPage is a minimal Page for testing navigation logic.
type stubPage struct {
	name       string
	canAdvance bool
}

func (s stubPage) Init(_ *pages.WizardState) tea.Cmd        { return nil }
func (s stubPage) Update(_ tea.Msg, _ *pages.WizardState) (pages.Page, tea.Cmd) {
	return s, nil
}
func (s stubPage) View(_, _ int) string                   { return s.name }
func (s stubPage) Name() string                           { return s.name }
func (s stubPage) CanAdvance(_ *pages.WizardState) bool   { return s.canAdvance }

func makeWizard(canAdvance bool, n int) WizardModel {
	state := &pages.WizardState{}
	pgs := make([]pages.Page, n)
	for i := range pgs {
		pgs[i] = stubPage{name: "page", canAdvance: canAdvance}
	}
	return NewWizardModel(state, pgs)
}

func TestWizard_AdvanceWhenCanAdvance(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 3)
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if updated.current != 1 {
		t.Fatalf("expected page 1, got %d", updated.current)
	}
}

func TestWizard_NoAdvanceWhenCannotAdvance(t *testing.T) {
	t.Helper()
	w := makeWizard(false, 3)
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if updated.current != 0 {
		t.Fatalf("expected page 0, got %d", updated.current)
	}
}

func TestWizard_BackNavigation(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 3)
	// Advance twice.
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if w.current != 2 {
		t.Fatalf("expected page 2, got %d", w.current)
	}
	// Go back once.
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if w.current != 1 {
		t.Fatalf("expected page 1 after back, got %d", w.current)
	}
}

func TestWizard_NoBackAtFirst(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 3)
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if updated.current != 0 {
		t.Fatalf("expected to stay at page 0, got %d", updated.current)
	}
}

func TestWizard_NoAdvancePastLast(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 2)
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	// Now at last page.
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if w.current != 1 {
		t.Fatalf("expected to stay at last page, got %d", w.current)
	}
}

func TestWizard_ViewNonEmpty(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 2)
	w.width = 100
	w.height = 30
	v := w.View()
	if v == "" {
		t.Fatal("expected non-empty view")
	}
}

func TestWizard_EmptyPages(t *testing.T) {
	t.Helper()
	state := &pages.WizardState{}
	w := NewWizardModel(state, nil)
	w.width = 100
	w.height = 30
	// Should not panic.
	_ = w.View()
	_, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
}
