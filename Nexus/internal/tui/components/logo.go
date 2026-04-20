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

// Logo renders the ASCII BubbleFish logo with lipgloss colors.
// The full ASCII art is populated in TUI.2; this stub shows a text banner.
type Logo struct {
	Width int
}

// asciiArt is the BubbleFish logo. Replaced with full art in TUI.2.
var asciiArt = []struct {
	text  string
	color lipgloss.Color
}{
	{`  ██████╗ ██╗   ██╗██████╗ ██████╗ ██╗     ███████╗███████╗██╗███████╗██╗  ██╗`, styles.ColorTeal},
	{`  ██╔══██╗██║   ██║██╔══██╗██╔══██╗██║     ██╔════╝██╔════╝██║██╔════╝██║  ██║`, styles.ColorTeal},
	{`  ██████╔╝██║   ██║██████╔╝██████╔╝██║     █████╗  █████╗  ██║███████╗███████║`, styles.ColorTeal},
	{`  ██╔══██╗██║   ██║██╔══██╗██╔══██╗██║     ██╔══╝  ██╔══╝  ██║╚════██║██╔══██║`, styles.ColorTeal},
	{`  ██████╔╝╚██████╔╝██████╔╝██████╔╝███████╗███████╗██║     ██║███████║██║  ██║`, styles.ColorGreen},
	{`  ╚═════╝  ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝╚══════╝╚═╝     ╚═╝╚══════╝╚═╝  ╚═╝`, styles.ColorGreen},
	{`         N  E  X  U  S  ·  Governed AI Memory Daemon`, styles.TextSecondary},
	{`         BubbleFish Technologies, Inc.  ·  Copyright © 2026`, styles.TextMuted},
}

// View renders the logo. If the terminal is too narrow for the full banner,
// it falls back to a compact text version.
func (l Logo) View() string {
	const fullWidth = 82
	if l.Width >= fullWidth {
		var lines []string
		for _, row := range asciiArt {
			lines = append(lines, lipgloss.NewStyle().Foreground(row.color).Render(row.text))
		}
		return strings.Join(lines, "\n") + "\n"
	}

	// Compact fallback.
	top := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("BubbleFish NEXUS")
	sub := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Governed AI Memory Daemon  ·  © 2026 BubbleFish Technologies, Inc.")
	return lipgloss.JoinVertical(lipgloss.Left, top, sub) + "\n"
}
