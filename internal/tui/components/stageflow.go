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

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// Stage represents a single stage in a flow.
type Stage struct {
	Number  int
	Name    string
	Status  string // "HIT", "SKIP", "MISS", "OK", "ERR"
	Latency string
}

// StageFlow renders a horizontal chain of stage boxes connected by › glyphs.
type StageFlow struct {
	Stages []Stage
	Width  int
}

// View renders the stage flow.
func (sf StageFlow) View() string {
	if len(sf.Stages) == 0 {
		return ""
	}

	boxWidth := 14
	if sf.Width > 0 && len(sf.Stages) > 0 {
		boxWidth = (sf.Width - len(sf.Stages)) / len(sf.Stages)
		if boxWidth < 12 {
			boxWidth = 12
		}
		if boxWidth > 20 {
			boxWidth = 20
		}
	}

	boxes := make([]string, 0, len(sf.Stages)*2-1)
	for i, s := range sf.Stages {
		borderColor := styles.BorderBase
		statusColor := styles.TextMuted
		switch s.Status {
		case "HIT", "OK":
			borderColor = styles.ColorTeal
			statusColor = styles.ColorTeal
		case "MISS", "ERR":
			borderColor = styles.ColorAmber
			statusColor = styles.ColorAmber
		case "SKIP":
			borderColor = styles.TextDim
			statusColor = styles.TextDim
		}

		header := lipgloss.NewStyle().Foreground(styles.TextMuted).Render(fmt.Sprintf("Stage %d", s.Number))
		name := lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true).Render(s.Name)
		status := lipgloss.NewStyle().Foreground(statusColor).Render(s.Status)
		latency := lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(s.Latency)

		content := lipgloss.JoinVertical(lipgloss.Left, header, name, status, latency)

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Width(boxWidth).
			Padding(0, 1).
			Render(content)

		boxes = append(boxes, box)
		if i < len(sf.Stages)-1 {
			arrow := lipgloss.NewStyle().
				Foreground(styles.TextDim).
				Render(" › ")
			boxes = append(boxes, lipgloss.PlaceVertical(lipgloss.Height(box), lipgloss.Center, arrow))
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, boxes...)
}
