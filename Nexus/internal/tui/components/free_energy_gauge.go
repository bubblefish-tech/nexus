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
	"math"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// FreeEnergyGauge displays the Active Inference free energy metric.
// F = -log p(o|m) + D_KL(q||p) — approximated from system metrics.
type FreeEnergyGauge struct {
	Values []float64 // rolling 60-second window
	Width  int
}

// Compute derives a simplified free energy value from system metrics.
// Inputs: cacheHitRate (0-1), policyAccuracy (0-1), quarantinePrecision (0-1).
func (g *FreeEnergyGauge) Compute(cacheHitRate, policyAccuracy, quarantinePrecision float64) float64 {
	predictionError := 1.0 - policyAccuracy
	klDivergence := math.Abs(cacheHitRate - 0.5) // divergence from expected
	fe := predictionError + klDivergence*(1-quarantinePrecision+0.01)
	if fe < 0 {
		fe = 0
	}
	return fe
}

// Push adds a value to the rolling window.
func (g *FreeEnergyGauge) Push(v float64) {
	if len(g.Values) >= 60 {
		g.Values = g.Values[1:]
	}
	g.Values = append(g.Values, v)
}

// View renders the gauge as a single line:
// ◈ FREE ENERGY  0.142 nats  ▁▂▂▁▂▃▂▁  ▼ minimizing
func (g *FreeEnergyGauge) View() string {
	label := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("◈ FREE ENERGY")

	current := 0.0
	if len(g.Values) > 0 {
		current = g.Values[len(g.Values)-1]
	}
	valueStr := lipgloss.NewStyle().Foreground(styles.TextWhite).Bold(true).
		Render(fmt.Sprintf("  %.3f nats", current))

	sparkW := g.Width - 36
	if sparkW < 10 {
		sparkW = 10
	}
	spark := g.renderSparkline(sparkW)

	trend := g.trend()

	return lipgloss.JoinHorizontal(lipgloss.Bottom, label, valueStr, "  ", spark, "  ", trend)
}

func (g *FreeEnergyGauge) renderSparkline(width int) string {
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	maxVal := 0.001
	for _, v := range g.Values {
		if v > maxVal {
			maxVal = v
		}
	}
	var sb strings.Builder
	start := 0
	if len(g.Values) > width {
		start = len(g.Values) - width
	}
	for i := start; i < len(g.Values); i++ {
		idx := int(g.Values[i] / maxVal * float64(len(blocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		sb.WriteRune(blocks[idx])
	}
	// Pad to width
	for sb.Len() < width {
		sb.WriteRune(blocks[0])
	}
	return lipgloss.NewStyle().Foreground(styles.ColorTeal).Render(sb.String())
}

func (g *FreeEnergyGauge) trend() string {
	if len(g.Values) < 5 {
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("▶ stable")
	}
	recent := g.Values[len(g.Values)-5:]
	avg := 0.0
	for _, v := range recent {
		avg += v
	}
	avg /= float64(len(recent))

	older := g.Values[0 : len(g.Values)-5]
	if len(older) == 0 {
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("▶ stable")
	}
	oldAvg := 0.0
	for _, v := range older {
		oldAvg += v
	}
	oldAvg /= float64(len(older))

	diff := avg - oldAvg
	if diff < -0.01 {
		return lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("▼ minimizing")
	}
	if diff > 0.01 {
		return lipgloss.NewStyle().Foreground(styles.ColorAmber).Render("▲ rising")
	}
	return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("▶ stable")
}
