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

	"github.com/bubblefish-tech/nexus/internal/discover"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type toolEntry struct {
	Name           string
	ConnectionType string
	Detected       bool
	DetectionMethod string
}

// ToolsPage lets the user include or exclude AI tools from the full manifest.
type ToolsPage struct {
	cursor int
	tools  []toolEntry
	built  bool
}

var _ Page = (*ToolsPage)(nil)

// NewToolsPage returns a ToolsPage.
func NewToolsPage() *ToolsPage { return &ToolsPage{} }

func (p *ToolsPage) Name() string { return "Tool Selection" }

func (p *ToolsPage) buildToolList(state *WizardState) {
	if p.built {
		return
	}
	p.built = true

	detectedSet := make(map[string]bool)
	for _, dt := range state.DiscoveredTools {
		detectedSet[dt.Name] = true
	}

	seen := make(map[string]bool)
	var detected, available []toolEntry

	for _, dt := range state.DiscoveredTools {
		if seen[dt.Name] {
			continue
		}
		seen[dt.Name] = true
		detected = append(detected, toolEntry{
			Name:            dt.Name,
			ConnectionType:  dt.ConnectionType,
			Detected:        true,
			DetectionMethod: dt.DetectionMethod,
		})
	}

	for _, td := range discover.KnownTools {
		if seen[td.Name] {
			continue
		}
		seen[td.Name] = true
		available = append(available, toolEntry{
			Name:           td.Name,
			ConnectionType: td.ConnectionType,
			Detected:       false,
		})
	}

	p.tools = append(detected, available...)
}

func (p *ToolsPage) Init(state *WizardState) tea.Cmd {
	p.buildToolList(state)
	if state.SelectedTools == nil {
		state.SelectedTools = make(map[string]bool)
		for _, t := range p.tools {
			if t.Detected {
				state.SelectedTools[t.Name] = true
			}
		}
	}
	return nil
}

func (p *ToolsPage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	p.buildToolList(state)
	if state.SelectedTools == nil {
		state.SelectedTools = make(map[string]bool)
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if len(p.tools) > 0 && p.cursor < len(p.tools)-1 {
				p.cursor++
			}
		case " ", "enter":
			if p.cursor < len(p.tools) {
				name := p.tools[p.cursor].Name
				state.SelectedTools[name] = !state.SelectedTools[name]
			}
		}
	}
	return p, nil
}

func (p *ToolsPage) CanAdvance(_ *WizardState) bool { return true }

func (p *ToolsPage) View(width, height int) string {
	return lipgloss.NewStyle().Foreground(styles.TextMuted).Width(width).
		Render("Waiting for scan results...")
}

// ViewWithState renders the tool list with checkbox state.
func (p *ToolsPage) ViewWithState(width, height int, state *WizardState) string {
	p.buildToolList(state)

	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Select AI Tools to Connect")
	b.WriteString(title + "\n\n")

	if len(p.tools) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("No tools available.\n\nPress Ctrl+N to continue."))
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}

	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Space to toggle  ·  ↑/↓ or j/k to navigate") + "\n\n")

	sel := state.SelectedTools
	if sel == nil {
		sel = make(map[string]bool)
	}

	inDetected := true
	wroteAvailHeader := false
	for i, t := range p.tools {
		if inDetected && !t.Detected {
			inDetected = false
			if i > 0 {
				b.WriteString("\n")
			}
			if !wroteAvailHeader {
				b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).Italic(true).
					Render("  Available (not detected on this machine):") + "\n\n")
				wroteAvailHeader = true
			}
		}

		cursor := "  "
		rowStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		if i == p.cursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			rowStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
		}
		var check string
		if sel[t.Name] {
			check = lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("[✓]")
		} else {
			check = lipgloss.NewStyle().Foreground(styles.TextMuted).Render("[ ]")
		}

		suffix := ""
		if t.Detected {
			suffix = lipgloss.NewStyle().Foreground(styles.ColorGreen).Render(" (detected)")
		}
		b.WriteString(fmt.Sprintf("%s%s %s%s\n",
			cursor, check, rowStyle.Render(t.Name), suffix))
		if t.ConnectionType != "" {
			b.WriteString(fmt.Sprintf("      %s\n",
				lipgloss.NewStyle().Foreground(styles.TextMuted).Render(t.ConnectionType)))
		}
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}
