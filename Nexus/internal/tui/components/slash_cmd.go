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

package components

import (
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SlashCommand describes a single slash command available in the running view.
type SlashCommand struct {
	Name        string
	Description string
	Aliases     []string
}

// SlashCommandSelectedMsg is sent when the user selects a command from the dropdown.
type SlashCommandSelectedMsg struct {
	Name string
}

// SlashCommandModel is the dropdown that appears when the user types `/` in the
// running view. Real integration with running mode happens in TUI.4.
type SlashCommandModel struct {
	commands []SlashCommand
	filtered []SlashCommand
	input    string
	cursor   int
	active   bool
	width    int
}

// NewSlashCommandModel creates a SlashCommandModel with the given command list.
func NewSlashCommandModel(commands []SlashCommand) SlashCommandModel {
	return SlashCommandModel{
		commands: commands,
		filtered: commands,
	}
}

// Activate enables the dropdown and resets input.
func (s *SlashCommandModel) Activate(width int) {
	s.active = true
	s.input = ""
	s.cursor = 0
	s.width = width
	s.filtered = s.commands
}

// Active returns true when the dropdown is visible.
func (s SlashCommandModel) Active() bool { return s.active }

// Update handles keyboard events for the dropdown.
func (s SlashCommandModel) Update(msg tea.Msg) (SlashCommandModel, tea.Cmd) {
	if !s.active {
		return s, nil
	}
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			s.active = false
			return s, nil
		case "up", "k":
			if s.cursor > 0 {
				s.cursor--
			}
		case "down", "j":
			if s.cursor < len(s.filtered)-1 {
				s.cursor++
			}
		case "enter":
			if s.cursor < len(s.filtered) {
				name := s.filtered[s.cursor].Name
				s.active = false
				return s, func() tea.Msg { return SlashCommandSelectedMsg{Name: name} }
			}
		case "backspace":
			if len(s.input) > 0 {
				s.input = s.input[:len(s.input)-1]
				s.filter()
			}
		default:
			if len(k.Runes) == 1 {
				s.input += string(k.Runes)
				s.filter()
			}
		}
	}
	return s, nil
}

// filter rebuilds the filtered command list based on the current input.
func (s *SlashCommandModel) filter() {
	s.cursor = 0
	if s.input == "" {
		s.filtered = s.commands
		return
	}
	query := strings.ToLower(s.input)
	var out []SlashCommand
	for _, cmd := range s.commands {
		if strings.HasPrefix(strings.ToLower(cmd.Name), query) {
			out = append(out, cmd)
			continue
		}
		for _, alias := range cmd.Aliases {
			if strings.HasPrefix(strings.ToLower(alias), query) {
				out = append(out, cmd)
				break
			}
		}
	}
	s.filtered = out
}

// View renders the slash-command dropdown overlay.
func (s SlashCommandModel) View() string {
	if !s.active {
		return ""
	}

	var b strings.Builder
	prompt := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).Render("/") +
		lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(s.input) +
		lipgloss.NewStyle().Foreground(styles.TextMuted).Render("█")
	b.WriteString(prompt + "\n")

	if len(s.filtered) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).Render("  no matching commands") + "\n")
	}

	maxShow := 10
	for i, cmd := range s.filtered {
		if i >= maxShow {
			b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
				Render(fmt.Sprintf("  … %d more", len(s.filtered)-maxShow)) + "\n")
			break
		}
		cursor := "  "
		nameStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		descStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
		if i == s.cursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			nameStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true)
		}
		b.WriteString(fmt.Sprintf("%s%-20s  %s\n",
			cursor,
			nameStyle.Render("/"+cmd.Name),
			descStyle.Render(cmd.Description),
		))
	}
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("↑/↓ navigate  ·  Enter select  ·  Esc cancel"))

	boxWidth := s.width
	if boxWidth < 60 {
		boxWidth = 60
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderFocus).
		Background(styles.BgPanel).
		Padding(0, 1).
		Width(boxWidth - 4).
		Render(b.String())
}
