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
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// HeatGrid renders a row of colored cells representing intensity levels.
type HeatGrid struct {
	Values []int
	Width  int
}

// View renders the heat grid.
func (h HeatGrid) View() string {
	if len(h.Values) == 0 {
		return ""
	}

	var cells []string
	for _, v := range h.Values {
		color := styles.BorderBase
		switch {
		case v == 0:
			color = styles.BorderBase
		case v <= 10:
			color = styles.HeatLow
		case v <= 50:
			color = styles.HeatMedLow
		case v <= 100:
			color = styles.HeatMedHi
		default:
			color = styles.HeatHigh
		}
		cell := lipgloss.NewStyle().Foreground(color).Render("■■")
		cells = append(cells, cell)
	}

	grid := strings.Join(cells, " ")

	legend := lipgloss.NewStyle().Foreground(styles.TextMuted).Render(
		"  0 ") +
		lipgloss.NewStyle().Foreground(styles.BorderBase).Render("■") + " " +
		lipgloss.NewStyle().Foreground(styles.HeatLow).Render("■") + " " +
		lipgloss.NewStyle().Foreground(styles.HeatMedLow).Render("■") + " " +
		lipgloss.NewStyle().Foreground(styles.HeatMedHi).Render("■") + " " +
		lipgloss.NewStyle().Foreground(styles.HeatHigh).Render("■") +
		lipgloss.NewStyle().Foreground(styles.TextMuted).Render(" 100+")

	return lipgloss.JoinVertical(lipgloss.Left, grid, legend)
}
