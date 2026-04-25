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
	_ "embed"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

//go:embed assets/bubblefish_fish_20x8.ansi
var miniFishArt string

// MiniLogo renders a compact 20-col × 8-row fish emblem for page headers.
type MiniLogo struct{}

// View returns the 20×8 fish emblem with teal body and green bubbles.
func (m MiniLogo) View() string {
	bodyStyle := lipgloss.NewStyle().Foreground(styles.ColorTeal)
	bubbleStyle := lipgloss.NewStyle().Foreground(styles.ColorGreen)

	var out []string
	for _, line := range strings.Split(strings.TrimRight(miniFishArt, "\n"), "\n") {
		var rendered strings.Builder
		for _, ch := range line {
			switch ch {
			case '°', '·':
				rendered.WriteString(bubbleStyle.Render(string(ch)))
			default:
				rendered.WriteString(bodyStyle.Render(string(ch)))
			}
		}
		out = append(out, rendered.String())
	}
	return strings.Join(out, "\n")
}

// Inline returns a compact single-line fish glyph for the header bar.
func (m MiniLogo) Inline() string {
	return lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("><°>")
}
