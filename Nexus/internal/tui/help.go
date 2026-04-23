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

	"github.com/bubblefish-tech/nexus/internal/tui/screens"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// HelpOverlay renders a structured keybinding reference.
type HelpOverlay struct {
	Width  int
	Height int
	Keys   GlobalKeyMap
	Screen screens.Screen // active screen for page-specific help
}

// View renders the help overlay as a centered box.
func (h HelpOverlay) View() string {
	titleStyle := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true)
	keyStyle := lipgloss.NewStyle().Foreground(styles.ColorTeal)
	descStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)
	sectionStyle := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).Bold(true)

	var lines []string
	lines = append(lines, titleStyle.Render("  KEYBINDINGS"))
	lines = append(lines, "")

	lines = append(lines, sectionStyle.Render("  NAVIGATION"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "1-9", "Switch to page"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "ctrl+n/p", "Next / previous page"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "ctrl+k", "Command palette"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "/", "Slash commands"))
	lines = append(lines, "")

	lines = append(lines, sectionStyle.Render("  GENERAL"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "q / ctrl+c", "Quit"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "r", "Force refresh"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "ctrl+r", "Pause auto-refresh"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "?", "Toggle help"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "esc", "Close overlay"))
	lines = append(lines, "")

	lines = append(lines, sectionStyle.Render("  SCROLLABLE PANES"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "j/k ↑/↓", "Scroll up/down"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "ctrl+d/u", "Half-page down/up"))
	lines = append(lines, renderBinding(keyStyle, descStyle, "g / G", "Jump to top/bottom"))

	// Page-specific help.
	if h.Screen != nil {
		bindings := h.Screen.ShortHelp()
		if len(bindings) > 0 {
			lines = append(lines, "")
			lines = append(lines, sectionStyle.Render("  "+strings.ToUpper(h.Screen.Name())+" PAGE"))
			for _, b := range bindings {
				lines = append(lines, renderBinding(keyStyle, descStyle,
					b.Help().Key, b.Help().Desc))
			}
		}
	}

	lines = append(lines, "")
	lines = append(lines, styles.MutedStyle.Render("  Press ? or esc to close."))

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorTeal).
		Foreground(styles.TextPrimary).
		Padding(1, 2).
		Render(content)

	return lipgloss.Place(h.Width, h.Height, lipgloss.Center, lipgloss.Center, box)
}

func renderBinding(keyStyle, descStyle lipgloss.Style, k, desc string) string {
	return "    " + keyStyle.Width(14).Render(k) + descStyle.Render(desc)
}

// Ensure key.Binding implements Help interface.
var _ key.Binding
