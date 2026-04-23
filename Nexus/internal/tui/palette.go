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

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PaletteCommand is a single entry in the command palette.
type PaletteCommand struct {
	Name        string
	Description string
}

// PaletteSelectedMsg is emitted when a palette command is selected.
type PaletteSelectedMsg struct {
	Name string
}

// PaletteModel is the Ctrl+K command palette overlay with fuzzy search.
type PaletteModel struct {
	active      bool
	input       textinput.Model
	commands    []PaletteCommand
	filtered    []PaletteCommand
	selectedIdx int
	width       int
}

// NewPaletteModel creates the command palette.
func NewPaletteModel(commands []PaletteCommand) PaletteModel {
	ti := textinput.New()
	ti.Placeholder = "search for a command..."
	ti.CharLimit = 128
	ti.Width = 50
	ti.PromptStyle = lipgloss.NewStyle().Foreground(styles.ColorTeal)
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
	return PaletteModel{
		input:    ti,
		commands: commands,
		filtered: commands,
	}
}

// Active returns whether the palette is visible.
func (p *PaletteModel) Active() bool { return p.active }

// Open shows the palette.
func (p *PaletteModel) Open(width int) {
	p.active = true
	p.width = width
	p.input.Focus()
	p.input.SetValue("")
	p.filtered = p.commands
	p.selectedIdx = 0
}

// Close hides the palette.
func (p *PaletteModel) Close() {
	p.active = false
	p.input.Blur()
}

// Update handles palette messages.
func (p *PaletteModel) Update(msg tea.Msg) (PaletteModel, tea.Cmd) {
	if !p.active {
		return *p, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			p.Close()
			return *p, nil
		case "enter":
			if p.selectedIdx < len(p.filtered) {
				name := p.filtered[p.selectedIdx].Name
				p.Close()
				return *p, func() tea.Msg { return PaletteSelectedMsg{Name: name} }
			}
			p.Close()
			return *p, nil
		case "up":
			if p.selectedIdx > 0 {
				p.selectedIdx--
			}
			return *p, nil
		case "down":
			if p.selectedIdx < len(p.filtered)-1 {
				p.selectedIdx++
			}
			return *p, nil
		default:
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			p.filter()
			return *p, cmd
		}
	}

	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return *p, cmd
}

func (p *PaletteModel) filter() {
	query := strings.ToLower(p.input.Value())
	if query == "" {
		p.filtered = p.commands
		p.selectedIdx = 0
		return
	}
	var matched []PaletteCommand
	for _, c := range p.commands {
		name := strings.ToLower(c.Name)
		desc := strings.ToLower(c.Description)
		if strings.Contains(name, query) || strings.Contains(desc, query) {
			matched = append(matched, c)
		}
	}
	p.filtered = matched
	if p.selectedIdx >= len(p.filtered) {
		p.selectedIdx = 0
	}
}

// View renders the centered palette overlay.
func (p *PaletteModel) View() string {
	if !p.active {
		return ""
	}

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  > "+p.input.View())
	lines = append(lines, "")

	for i, cmd := range p.filtered {
		prefix := "  ◈  "
		nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		descStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)

		if i == p.selectedIdx {
			prefix = "  ▸  "
			nameStyle = nameStyle.Foreground(styles.ColorTeal).Bold(true)
		}

		name := nameStyle.Render(cmd.Name)
		desc := descStyle.Render("  " + cmd.Description)
		lines = append(lines, prefix+name+desc)
	}

	if len(p.filtered) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("  No matching commands"))
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("  ↑↓ navigate   ↵ execute   esc dismiss"))

	content := strings.Join(lines, "\n")

	boxW := 60
	if p.width > 0 && p.width < boxW+10 {
		boxW = p.width - 10
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorTeal).
		Background(styles.BgPanel).
		Width(boxW).
		Padding(0, 1).
		Render(content)

	return box
}
