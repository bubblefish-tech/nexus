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
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/pages"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WizardModel manages the multi-page setup wizard.
// It renders: logo (top) + progress indicator + page content + nav bar (bottom).
type WizardModel struct {
	pgs     []pages.Page
	current int
	state   *pages.WizardState
	width   int
	height  int
}

// NewWizardModel creates a wizard with the given pages and shared state.
func NewWizardModel(state *pages.WizardState, pgs []pages.Page) WizardModel {
	return WizardModel{
		pgs:   pgs,
		state: state,
	}
}

// Init starts the first page.
func (w WizardModel) Init() tea.Cmd {
	if len(w.pgs) == 0 {
		return nil
	}
	return w.pgs[0].Init(w.state)
}

// Update handles messages: WindowSizeMsg, KeyMsg (navigation), and all others
// are forwarded to the current page.
func (w WizardModel) Update(msg tea.Msg) (WizardModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w.width = msg.Width
		w.height = msg.Height
		return w, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+n", "right":
			if w.current < len(w.pgs)-1 && len(w.pgs) > 0 && w.pgs[w.current].CanAdvance(w.state) {
				w.current++
				return w, w.pgs[w.current].Init(w.state)
			}
			return w, nil
		case "ctrl+b", "left":
			if w.current > 0 {
				w.current--
				return w, nil
			}
			return w, nil
		}
	}

	// Route to the current page.
	if len(w.pgs) == 0 {
		return w, nil
	}
	updated, cmd := w.pgs[w.current].Update(msg, w.state)
	w.pgs[w.current] = updated
	return w, cmd
}

// View renders the wizard layout.
func (w WizardModel) View() string {
	if w.width < 60 || w.height < 20 {
		return lipgloss.NewStyle().Foreground(styles.ColorAmber).Bold(true).
			Render(fmt.Sprintf("Terminal too small (min 60×20, current %d×%d).", w.width, w.height))
	}

	// Logo area.
	logo := components.Logo{Width: w.width}.View()

	// Progress line: "Step N of M — Page Name".
	total := len(w.pgs)
	cur := w.current + 1
	pageName := ""
	if w.current < len(w.pgs) {
		pageName = w.pgs[w.current].Name()
	}
	progressText := fmt.Sprintf("Step %d of %d  —  %s", cur, total, pageName)
	progressLine := lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Width(w.width).
		Render(progressText)

	// Navigation hint.
	navHint := w.buildNavHint()

	// Compute heights.
	logoH := lipgloss.Height(logo)
	progressH := 1
	navH := 1
	separatorH := 2 // two separator lines
	contentH := w.height - logoH - progressH - navH - separatorH
	if contentH < 5 {
		contentH = 5
	}

	// Page content — try ViewWithState for pages that implement it.
	var pageContent string
	if w.current < len(w.pgs) {
		pageContent = w.viewPage(w.current, w.width, contentH)
	}

	sep := lipgloss.NewStyle().Foreground(styles.BorderBase).
		Render(strings.Repeat("─", w.width))

	content := lipgloss.NewStyle().Width(w.width).Height(contentH).
		Render(pageContent)

	inner := lipgloss.JoinVertical(lipgloss.Center,
		logo,
		progressLine,
		sep,
		content,
		sep,
		navHint,
	)

	return lipgloss.Place(w.width, w.height, lipgloss.Center, lipgloss.Center, inner)
}

// viewPage dispatches to ViewWithState if available, otherwise View.
func (w WizardModel) viewPage(idx, width, height int) string {
	pg := w.pgs[idx]
	type stateViewer interface {
		ViewWithState(int, int, *pages.WizardState) string
	}
	if sv, ok := pg.(stateViewer); ok {
		return sv.ViewWithState(width, height, w.state)
	}
	return pg.View(width, height)
}

func (w WizardModel) buildNavHint() string {
	var parts []string
	if w.current > 0 {
		parts = append(parts, styles.MutedStyle.Render("← Ctrl+B  Back"))
	}
	if w.current < len(w.pgs)-1 {
		canAdv := len(w.pgs) > 0 && w.pgs[w.current].CanAdvance(w.state)
		label := "Ctrl+N / →  Next"
		if canAdv {
			parts = append(parts, styles.TealStyle.Render(label))
		} else {
			parts = append(parts, styles.MutedStyle.Render(label))
		}
	} else {
		parts = append(parts, styles.TealStyle.Render("Enter  Confirm"))
	}
	return "  " + strings.Join(parts, "   ")
}
