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
	"github.com/charmbracelet/lipgloss"
)

// SidebarWidth is the fixed width of the sidebar.
const SidebarWidth = 22

// SidebarItem is a key-value pair in the sidebar.
type SidebarItem struct {
	Name  string
	Value string
	Dot   string // color name: "green", "amber", "red", ""
}

// SidebarSection is a group of items with a header.
type SidebarSection struct {
	Title string
	Items []SidebarItem
}

// Sidebar renders a fixed-width left pane with sections.
type Sidebar struct {
	Sections  []SidebarSection
	Collapsed bool
	Height    int
}

// View renders the sidebar.
func (s Sidebar) View() string {
	if s.Collapsed {
		return ""
	}

	inner := SidebarWidth - 4
	var sections []string

	for _, sec := range s.Sections {
		header := lipgloss.NewStyle().
			Foreground(styles.TextMuted).
			Bold(true).
			Render(strings.ToUpper(sec.Title))
		sections = append(sections, header)

		for _, item := range sec.Items {
			dot := ""
			switch item.Dot {
			case "green":
				dot = lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("● ")
			case "amber":
				dot = lipgloss.NewStyle().Foreground(styles.ColorAmber).Render("● ")
			case "red":
				dot = lipgloss.NewStyle().Foreground(styles.ColorRed).Render("● ")
			}

			name := lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(item.Name)
			val := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(item.Value)

			nameWidth := lipgloss.Width(dot + name)
			valWidth := lipgloss.Width(val)
			gap := inner - nameWidth - valWidth
			if gap < 1 {
				gap = 1
			}
			line := dot + name + fmt.Sprintf("%*s", gap, "") + val
			sections = append(sections, line)
		}
		sections = append(sections, "")
	}

	content := strings.Join(sections, "\n")

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderBase).
		Width(SidebarWidth).
		Height(s.Height).
		Padding(1, 1).
		Render(content)
}
