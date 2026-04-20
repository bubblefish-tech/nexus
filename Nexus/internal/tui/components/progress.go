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

// spinFrames are the frames for the indeterminate spinner.
var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ProgressBar renders a determinate or indeterminate progress bar.
type ProgressBar struct {
	// Determinate mode: Total > 0.
	Total   int
	Current int
	Label   string
	Width   int
	// Indeterminate mode: Spinning = true.
	Spinning bool
	Frame    int // current spinner frame (caller increments via tick)
}

// View renders the progress bar.
func (p ProgressBar) View() string {
	if p.Spinning {
		frame := spinFrames[p.Frame%len(spinFrames)]
		spinner := lipgloss.NewStyle().Foreground(styles.ColorTeal).Render(frame)
		label := lipgloss.NewStyle().Foreground(styles.TextSecondary).Render("  " + p.Label)
		return spinner + label
	}

	// Determinate bar.
	barWidth := p.Width - len(p.Label) - 12
	if barWidth < 4 {
		barWidth = 4
	}

	var pct float64
	if p.Total > 0 {
		pct = float64(p.Current) / float64(p.Total)
	}
	filled := int(pct * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	bar := lipgloss.NewStyle().Foreground(styles.ColorTeal).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(styles.TextDim).Render(strings.Repeat("░", barWidth-filled))

	label := lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(p.Label)
	pctStr := lipgloss.NewStyle().Foreground(styles.TextMuted).Render(fmt.Sprintf(" %3.0f%%", pct*100))

	return fmt.Sprintf("[%s] %s%s", bar, label, pctStr)
}
