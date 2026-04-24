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

// WaterfallStageState represents the execution state of a cascade stage.
type WaterfallStageState int

const (
	WaterfallIdle     WaterfallStageState = iota
	WaterfallRunning
	WaterfallDone
	WaterfallSkipped
	WaterfallSlow
	WaterfallError
)

// WaterfallStage is one row in the cascade waterfall visualization.
type WaterfallStage struct {
	ID         string
	Name       string
	State      WaterfallStageState
	DurationMs float64
	Hits       int
	Progress   float64
	Extra      string
}

// WaterfallProps configures the waterfall renderer.
type WaterfallProps struct {
	Stages []WaterfallStage
	Width  int
	Query  string
}

// RenderWaterfall returns the full waterfall block showing cascade stage progress.
func RenderWaterfall(p WaterfallProps) string {
	header := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render(fmt.Sprintf(" RETRIEVAL CASCADE  —  %s", p.Query))
	var lines []string
	lines = append(lines, header)
	lines = append(lines, "")
	for _, s := range p.Stages {
		lines = append(lines, renderWaterfallStage(s, p.Width))
	}
	return strings.Join(lines, "\n")
}

func renderWaterfallStage(s WaterfallStage, totalWidth int) string {
	barWidth := totalWidth - 50
	if barWidth < 20 {
		barWidth = 20
	}

	filled := int(float64(barWidth) * s.Progress)
	if s.State == WaterfallDone || s.State == WaterfallSlow {
		filled = barWidth
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	var color lipgloss.Color
	switch s.State {
	case WaterfallRunning:
		color = styles.ColorTeal
	case WaterfallDone:
		color = styles.ColorGreen
	case WaterfallSlow:
		color = styles.ColorAmber
	case WaterfallError:
		color = styles.ColorRed
	default:
		color = styles.TextMuted
	}

	barStyled := lipgloss.NewStyle().Foreground(color).Render(bar)

	label := fmt.Sprintf(" Stage %-5s %-20s", s.ID, s.Name)
	labelStyled := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(label)

	var metric string
	switch s.State {
	case WaterfallRunning:
		metric = fmt.Sprintf("%.1fms running…", s.DurationMs)
	case WaterfallDone:
		metric = fmt.Sprintf("%.1fms  %d hits ✓", s.DurationMs, s.Hits)
	case WaterfallSlow:
		metric = fmt.Sprintf("%.1fms  slow ⚠", s.DurationMs)
	case WaterfallSkipped:
		metric = "skipped"
	case WaterfallError:
		metric = "error"
	default:
		metric = "—"
	}
	metricStyled := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).Render(metric)

	return fmt.Sprintf("%s [%s] %s", labelStyled, barStyled, metricStyled)
}
