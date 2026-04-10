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

// SectionTitle renders an uppercase gray label with bottom border.
func SectionTitle(title string, width int) string {
	label := lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Bold(true).
		Render(strings.ToUpper(title))

	line := lipgloss.NewStyle().
		Foreground(styles.BorderBase).
		Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(lipgloss.Left, label, line)
}
