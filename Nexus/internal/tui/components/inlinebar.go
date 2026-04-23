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

// InlineBar renders a single-row progress bar with label and value.
type InlineBar struct {
	Label   string
	Value   float64 // 0.0 to 1.0
	Width   int
	Color   lipgloss.Color
}

// View renders the inline bar.
func (b InlineBar) View() string {
	labelStr := lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(b.Label)
	pctStr := fmt.Sprintf("%.0f%%", b.Value*100)
	valueStr := lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true).Render(pctStr)

	barWidth := b.Width - lipgloss.Width(labelStr) - lipgloss.Width(valueStr) - 3
	if barWidth < 5 {
		barWidth = 5
	}

	filled := int(b.Value * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}
	empty := barWidth - filled

	bar := lipgloss.NewStyle().Foreground(b.Color).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(styles.TextMuted).Render(strings.Repeat("░", empty))

	return labelStr + " " + bar + " " + valueStr
}
