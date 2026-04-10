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

// StatCard renders a metric card with label, value, subtitle, and accent color.
type StatCard struct {
	Label    string
	Value    string
	Subtitle string
	Color    lipgloss.Color
	Width    int
}

// View renders the stat card.
func (s StatCard) View() string {
	w := s.Width
	if w < 10 {
		w = 20
	}
	inner := w - 4 // padding

	label := lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Width(inner).
		Render(strings.ToUpper(s.Label))

	value := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.TextPrimary).
		Width(inner).
		Render(s.Value)

	subtitle := lipgloss.NewStyle().
		Foreground(styles.TextSecondary).
		Width(inner).
		Render(s.Subtitle)

	accent := lipgloss.NewStyle().
		Foreground(s.Color).
		Width(inner).
		Render(strings.Repeat("━", inner))

	content := lipgloss.JoinVertical(lipgloss.Left, label, value, subtitle, accent)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderBase).
		Padding(0, 1).
		Width(w).
		Render(content)
}
