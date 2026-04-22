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

// Logo renders the BubbleFish ASCII art logo with lipgloss colors.
type Logo struct {
	Width int
}

// fishLines is the ASCII fish with bubbles. Teal body, green bubbles (°/·).
// Each entry: (text, isBubble). Bubbles render in green; body lines in teal.
//
// Design goal: ~40 chars wide, 5 lines, readable in any terminal font.
var fishLines = []struct {
	text   string
	bubble bool
}{
	{`      °   ·        °    ·  °`, true},
	{`  °       ·   ><((((°>      °`, false},
	{`    ·   °      ·   °   ·`, true},
}

// bannerLines is the BUBBLEFISH NEXUS block-letter banner.
// Rendered in teal (top rows) shading to green (bottom rows).
var bannerLines = []struct {
	text  string
	color lipgloss.Color
}{
	{`  ██████╗ ██╗   ██╗██████╗ ██████╗ ██╗     ███████╗███████╗██╗███████╗██╗  ██╗`, styles.ColorTeal},
	{`  ██╔══██╗██║   ██║██╔══██╗██╔══██╗██║     ██╔════╝██╔════╝██║██╔════╝██║  ██║`, styles.ColorTeal},
	{`  ██████╔╝██║   ██║██████╔╝██████╔╝██║     █████╗  █████╗  ██║███████╗███████║`, styles.ColorTeal},
	{`  ██╔══██╗██║   ██║██╔══██╗██╔══██╗██║     ██╔══╝  ██╔══╝  ██║╚════██║██╔══██║`, styles.ColorTeal},
	{`  ██████╔╝╚██████╔╝██████╔╝██████╔╝███████╗███████╗██║     ██║███████║██║  ██║`, styles.ColorGreen},
	{`  ╚═════╝  ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝╚══════╝╚═╝     ╚═╝╚══════╝╚═╝  ╚═╝`, styles.ColorGreen},
}

// View renders the logo. Full art for terminals ≥82 columns; compact fish for
// narrower terminals.
func (l Logo) View() string {
	const fullWidth = 82
	if l.Width >= fullWidth {
		return l.fullView()
	}
	return l.compactView()
}

func (l Logo) fullView() string {
	var lines []string

	// Fish + bubbles row above the banner.
	for _, row := range fishLines {
		color := styles.ColorTeal
		if row.bubble {
			color = styles.ColorGreen
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(color).Render(row.text))
	}

	lines = append(lines, "")

	// Block-letter banner.
	for _, row := range bannerLines {
		lines = append(lines, lipgloss.NewStyle().Foreground(row.color).Render(row.text))
	}

	lines = append(lines, "")

	// The banner block is ~82 chars wide; center subtitle/copyright/designer to match.
	bannerWidth := 82
	if l.Width > bannerWidth {
		bannerWidth = l.Width
	}

	subtitleText := lipgloss.NewStyle().Foreground(styles.TextSecondary).
		Render("N   E   X   U   S   ·   THE  Governed  AI  Control  Plane")
	copyrightText := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("BubbleFish  Technologies,  Inc.   ·   Copyright  ©  2026")
	designerText := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Designed by: Shawn Sammartano")

	lines = append(lines,
		lipgloss.PlaceHorizontal(bannerWidth, lipgloss.Center, subtitleText),
		lipgloss.PlaceHorizontal(bannerWidth, lipgloss.Center, copyrightText),
		"",
		lipgloss.PlaceHorizontal(bannerWidth, lipgloss.Center, designerText),
	)

	return strings.Join(lines, "\n") + "\n"
}

func (l Logo) compactView() string {
	fish := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("><((((°>  BubbleFish NEXUS")
	sub := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("THE Governed AI Control Plane  ·  © 2026 BubbleFish Technologies, Inc.")
	designer := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Designed by: Shawn Sammartano")
	w := l.Width
	if w < 1 {
		w = 80
	}
	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.PlaceHorizontal(w, lipgloss.Center, fish),
		lipgloss.PlaceHorizontal(w, lipgloss.Center, sub),
		lipgloss.PlaceHorizontal(w, lipgloss.Center, designer),
	) + "\n"
}
