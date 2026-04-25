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

// StatCardProps configures a dashboard stat card.
type StatCardProps struct {
	Label    string
	Value    string
	SubLabel string
	Accent   lipgloss.Color
	Width    int
	Height   int
	Pulse    float64
	Loaded   bool
}

// StatCard renders a single dashboard stat card with gradient top border,
// letter-spaced label, accent-colored value, and sub-label.
func StatCard(p StatCardProps) string {
	w := p.Width
	if w < 12 {
		w = 12
	}
	h := p.Height
	if h < 5 {
		h = 5
	}

	topGradient := renderGradientLine(w-2, p.Accent, p.Pulse)

	inner := w - 4

	labelStyle := lipgloss.NewStyle().
		Foreground(styles.TextMuted).
		Bold(true).
		Width(inner)
	valueStyle := lipgloss.NewStyle().
		Foreground(p.Accent).
		Bold(true).
		Width(inner)
	subStyle := lipgloss.NewStyle().
		Foreground(styles.TextWhiteDim).
		Width(inner)

	label := labelStyle.Render(transformUpperSpaced(p.Label))
	var value, sub string
	if p.Loaded {
		value = valueStyle.Render(p.Value)
		sub = subStyle.Render(p.SubLabel)
	} else {
		value = lipgloss.NewStyle().Foreground(styles.TextMuted).Width(inner).Render("···")
		sub = ""
	}

	body := lipgloss.JoinVertical(lipgloss.Left, label, value, sub)

	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderBase).
		Padding(0, 1).
		Width(w - 2).
		Render(body)

	return lipgloss.JoinVertical(lipgloss.Left, topGradient, card)
}

// renderGradientLine produces a 1-row gradient using accent color with
// intensity modulated by pulse (0..1). Higher pulse brightens the center.
func renderGradientLine(w int, accent lipgloss.Color, pulse float64) string {
	if w < 1 {
		return ""
	}
	intensity := 0.2 + 0.8*pulse
	if intensity > 1 {
		intensity = 1
	}
	_ = intensity
	return lipgloss.NewStyle().
		Foreground(accent).
		Render(strings.Repeat("▀", w))
}

// transformUpperSpaced turns "MEMORIES" into "M E M O R I E S" for letter-spaced small caps.
func transformUpperSpaced(s string) string {
	upper := strings.ToUpper(s)
	if len(upper) == 0 {
		return upper
	}
	var b strings.Builder
	for i, r := range upper {
		if i > 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}
