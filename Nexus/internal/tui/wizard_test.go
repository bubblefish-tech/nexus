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
	"strings"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/tui/pages"
	tea "github.com/charmbracelet/bubbletea"
)

type stubPage struct {
	name       string
	canAdvance bool
}

func (s stubPage) Init(_ *pages.WizardState) tea.Cmd                         { return nil }
func (s stubPage) Update(_ tea.Msg, _ *pages.WizardState) (pages.Page, tea.Cmd) { return s, nil }
func (s stubPage) View(_, _ int) string                                       { return s.name }
func (s stubPage) Name() string                                               { return s.name }
func (s stubPage) CanAdvance(_ *pages.WizardState) bool                       { return s.canAdvance }

func makeWizard(canAdvance bool, n int) WizardModel {
	state := &pages.WizardState{}
	pgs := make([]pages.Page, n)
	for i := range pgs {
		pgs[i] = stubPage{name: "page", canAdvance: canAdvance}
	}
	w := NewWizardModel(state, pgs)
	w.width = 100
	w.height = 30
	return w
}

func makeNamedWizard(names []string) WizardModel {
	state := &pages.WizardState{}
	pgs := make([]pages.Page, len(names))
	for i, name := range names {
		pgs[i] = stubPage{name: name, canAdvance: true}
	}
	w := NewWizardModel(state, pgs)
	w.width = 100
	w.height = 30
	return w
}

func TestWizard_AdvanceWhenCanAdvance(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 3)
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if updated.current != 1 {
		t.Fatalf("expected page 1, got %d", updated.current)
	}
	view := updated.View()
	if !strings.Contains(view, "Step 2 of 3") {
		t.Fatalf("expected 'Step 2 of 3' in view after advance, got:\n%s", view)
	}
}

func TestWizard_NoAdvanceWhenCannotAdvance(t *testing.T) {
	t.Helper()
	w := makeWizard(false, 3)
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if updated.current != 0 {
		t.Fatalf("expected page 0, got %d", updated.current)
	}
	view := updated.View()
	if !strings.Contains(view, "Step 1 of 3") {
		t.Fatalf("expected 'Step 1 of 3' in view when advance blocked, got:\n%s", view)
	}
}

func TestWizard_BackNavigation(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 3)
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if w.current != 2 {
		t.Fatalf("expected page 2, got %d", w.current)
	}
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if w.current != 1 {
		t.Fatalf("expected page 1 after back, got %d", w.current)
	}
	view := w.View()
	if !strings.Contains(view, "Step 2 of 3") {
		t.Fatalf("expected 'Step 2 of 3' after going back, got:\n%s", view)
	}
}

func TestWizard_NoBackAtFirst(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 3)
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	if updated.current != 0 {
		t.Fatalf("expected to stay at page 0, got %d", updated.current)
	}
	view := updated.View()
	if !strings.Contains(view, "Step 1") {
		t.Fatalf("expected Step 1 in view at first page")
	}
}

func TestWizard_NoAdvancePastLast(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 2)
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if w.current != 1 {
		t.Fatalf("expected to stay at last page, got %d", w.current)
	}
	view := w.View()
	if !strings.Contains(view, "Step 2 of 2") {
		t.Fatalf("expected 'Step 2 of 2' at last page, got:\n%s", view)
	}
}

func TestWizard_ViewShowsPageName(t *testing.T) {
	t.Helper()
	w := makeNamedWizard([]string{"Welcome", "Config", "Done"})
	view := w.View()
	if !strings.Contains(view, "Welcome") {
		t.Fatalf("expected 'Welcome' in view, got:\n%s", view)
	}

	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	view = w.View()
	if !strings.Contains(view, "Config") {
		t.Fatalf("expected 'Config' in view after advance, got:\n%s", view)
	}
}

func TestWizard_ViewShowsNavHints(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 3)
	view := w.View()
	if !strings.Contains(view, "Next") {
		t.Fatalf("expected 'Next' nav hint in view")
	}

	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	view = w.View()
	if !strings.Contains(view, "Back") {
		t.Fatalf("expected 'Back' nav hint after first page")
	}
}

func TestWizard_ViewNonEmpty(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 2)
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
	_ = w.View()
	_, _ = w.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
}

func TestWizard_RightArrowAdvances(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 3)
	updated, _ := w.Update(tea.KeyMsg{Type: tea.KeyRight})
	if updated.current != 1 {
		t.Fatalf("expected page 1 after right arrow, got %d", updated.current)
	}
	view := updated.View()
	if !strings.Contains(view, "Step 2") {
		t.Fatalf("expected Step 2 in view after right arrow")
	}
}

func TestWizard_LeftArrowGoesBack(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 3)
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyRight})
	w, _ = w.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if w.current != 0 {
		t.Fatalf("expected page 0 after left arrow, got %d", w.current)
	}
}

func TestWizard_TooSmallTerminal(t *testing.T) {
	t.Helper()
	w := makeWizard(true, 2)
	w.width = 30
	w.height = 10
	view := w.View()
	if !strings.Contains(view, "too small") {
		t.Fatalf("expected 'too small' message for undersized terminal")
	}
}
