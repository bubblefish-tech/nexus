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

// Package components provides reusable TUI components.
package components

import (
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CheckboxItem is a single item in a CheckboxList.
type CheckboxItem struct {
	Label       string
	Description string
	Checked     bool
	Disabled    bool
}

// CheckboxList is a keyboard-navigable list of toggleable items.
type CheckboxList struct {
	Items  []CheckboxItem
	Cursor int
	Width  int
}

// Update handles keyboard input: j/k or arrows move the cursor, space toggles.
func (c *CheckboxList) Update(msg tea.Msg) tea.Cmd {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "up", "k":
			if c.Cursor > 0 {
				c.Cursor--
			}
		case "down", "j":
			if c.Cursor < len(c.Items)-1 {
				c.Cursor++
			}
		case " ":
			if c.Cursor < len(c.Items) && !c.Items[c.Cursor].Disabled {
				c.Items[c.Cursor].Checked = !c.Items[c.Cursor].Checked
			}
		}
	}
	return nil
}

// Selected returns the indices of checked items.
func (c *CheckboxList) Selected() []int {
	var out []int
	for i, item := range c.Items {
		if item.Checked {
			out = append(out, i)
		}
	}
	return out
}

// View renders the checkbox list.
func (c CheckboxList) View() string {
	var b strings.Builder
	for i, item := range c.Items {
		cursor := "  "
		labelStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		if i == c.Cursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			labelStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
		}
		var check string
		switch {
		case item.Disabled:
			check = lipgloss.NewStyle().Foreground(styles.TextDim).Render("[-]")
		case item.Checked:
			check = lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("[✓]")
		default:
			check = lipgloss.NewStyle().Foreground(styles.TextMuted).Render("[ ]")
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n",
			cursor, check, labelStyle.Render(item.Label)))
		if item.Description != "" {
			b.WriteString(fmt.Sprintf("      %s\n",
				lipgloss.NewStyle().Foreground(styles.TextMuted).Render(item.Description)))
		}
	}
	return lipgloss.NewStyle().Width(c.Width).Render(b.String())
}
