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
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var modeChoices = []struct {
	key   string
	label string
	desc  string
}{
	{"simple", "Simple Setup", "Quick start with sensible defaults — recommended for first-time users"},
	{"balanced", "Balanced Setup", "Full configuration with embedding, audit chain, and control plane"},
	{"safe", "Safe Setup", "Maximum security: WAL MAC, encrypted config, strict rate limits"},
	{"import", "Import Data", "Import existing conversation history from Claude, Cursor, or LM Studio"},
}

// WelcomePage is the first wizard page — mode selection.
type WelcomePage struct {
	cursor int
}

var _ Page = (*WelcomePage)(nil)

// NewWelcomePage returns a WelcomePage with the default cursor on "simple".
func NewWelcomePage() *WelcomePage { return &WelcomePage{} }

func (p *WelcomePage) Name() string { return "Welcome" }

func (p *WelcomePage) Init(state *WizardState) tea.Cmd {
	// Pre-select the current mode if already set.
	for i, c := range modeChoices {
		if c.key == state.Mode {
			p.cursor = i
			return nil
		}
	}
	p.cursor = 0
	return nil
}

func (p *WelcomePage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(modeChoices)-1 {
				p.cursor++
			}
		case " ", "enter":
			state.Mode = modeChoices[p.cursor].key
			return p, func() tea.Msg { return AdvancePageMsg{} }
		}
	}
	return p, nil
}

func (p *WelcomePage) CanAdvance(state *WizardState) bool {
	return state.Mode != ""
}

func (p *WelcomePage) View(width, height int) string {
	var b strings.Builder

	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Choose your setup mode")
	b.WriteString(title + "\n\n")

	for i, c := range modeChoices {
		cursor := "  "
		labelStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		descStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
		if i == p.cursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			labelStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, labelStyle.Render(c.label)))
		b.WriteString(fmt.Sprintf("    %s\n\n", descStyle.Render(c.desc)))
	}

	hint := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("↑/↓ or j/k to navigate  ·  Space or Enter to select and continue")
	b.WriteString(hint)

	return lipgloss.PlaceHorizontal(width, lipgloss.Center, b.String())
}
