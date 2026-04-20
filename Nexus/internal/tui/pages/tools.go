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

// ToolsPage lets the user include or exclude discovered AI tools.
type ToolsPage struct {
	cursor int
}

var _ Page = (*ToolsPage)(nil)

// NewToolsPage returns a ToolsPage.
func NewToolsPage() *ToolsPage { return &ToolsPage{} }

func (p *ToolsPage) Name() string { return "Tool Selection" }

func (p *ToolsPage) Init(state *WizardState) tea.Cmd {
	if state.SelectedTools == nil {
		state.SelectedTools = make(map[int]bool)
		// Pre-select orchestratable tools by default.
		for i, t := range state.DiscoveredTools {
			state.SelectedTools[i] = t.Orchestratable
		}
	}
	return nil
}

func (p *ToolsPage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	if state.SelectedTools == nil {
		state.SelectedTools = make(map[int]bool)
	}
	tools := state.DiscoveredTools
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if len(tools) > 0 && p.cursor < len(tools)-1 {
				p.cursor++
			}
		case " ":
			if p.cursor < len(tools) {
				state.SelectedTools[p.cursor] = !state.SelectedTools[p.cursor]
			}
		}
	}
	return p, nil
}

func (p *ToolsPage) CanAdvance(_ *WizardState) bool { return true }

func (p *ToolsPage) View(width, height int) string {
	return lipgloss.NewStyle().Foreground(styles.TextMuted).Width(width).
		Render("Waiting for scan results…")
}

// ViewWithState renders the tool list with checkbox state.
func (p *ToolsPage) ViewWithState(width, height int, state *WizardState) string {
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Select AI tools to connect")
	b.WriteString(title + "\n\n")

	if len(state.DiscoveredTools) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("No tools discovered on this machine.\n\nYou can connect tools manually after installation.\n"))
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("\nPress Ctrl+N to continue."))
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}

	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Space to toggle  ·  ↑/↓ or j/k to navigate") + "\n\n")

	sel := state.SelectedTools
	if sel == nil {
		sel = make(map[int]bool)
	}

	for i, t := range state.DiscoveredTools {
		cursor := "  "
		rowStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		if i == p.cursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			rowStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
		}
		var check string
		if sel[i] {
			check = lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("[✓]")
		} else {
			check = lipgloss.NewStyle().Foreground(styles.TextMuted).Render("[ ]")
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n",
			cursor, check, rowStyle.Render(t.Name)))
		b.WriteString(fmt.Sprintf("      %s  %s\n",
			lipgloss.NewStyle().Foreground(styles.ColorPurple).Render(t.DetectionMethod),
			lipgloss.NewStyle().Foreground(styles.TextMuted).Render(t.ConnectionType),
		))
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}
