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

// ProofTreeProps configures the full-screen walkable proof tree overlay.
type ProofTreeProps struct {
	TotalEntries int
	CursorN      int
	DailyRoots   []string
	Width        int
	Height       int
}

// RenderProofTree draws the full-screen proof tree overlay.
func RenderProofTree(p ProofTreeProps) string {
	if p.Width < 40 || p.Height < 15 {
		return lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("Terminal too small for proof tree overlay")
	}

	hashStyle := lipgloss.NewStyle().Foreground(styles.ColorTealDim)
	headerStyle := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true)
	cursorStyle := lipgloss.NewStyle().Foreground(styles.ColorCyan).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)

	var lines []string

	lines = append(lines, headerStyle.Render(
		fmt.Sprintf(" MERKLE ROOT TIMELINE  —  %d total entries  ·  cursor: #%d", p.TotalEntries, p.CursorN)))
	lines = append(lines, "")

	for i, root := range p.DailyRoots {
		marker := "  "
		if i == len(p.DailyRoots)-1 {
			marker = cursorStyle.Render("→ ")
		}
		label := fmt.Sprintf("day %d", i+1)
		lines = append(lines, fmt.Sprintf(" %s%s  %s",
			marker, mutedStyle.Render(label), hashStyle.Render(truncHash(root, 24))))
	}
	if len(p.DailyRoots) == 0 {
		lines = append(lines, mutedStyle.Render("  No Merkle roots yet"))
	}

	lines = append(lines, "")
	lines = append(lines, headerStyle.Render(" CURRENT TREE"))
	lines = append(lines, "")

	lines = append(lines, fmt.Sprintf("                  ┌─ %s ─┐",
		hashStyle.Render("daily root")))
	lines = append(lines, mutedStyle.Render("                  │  (today)  │"))
	lines = append(lines, mutedStyle.Render("                  └─────┬─────┘"))
	lines = append(lines, mutedStyle.Render("                ┌───────┴───────┐"))
	lines = append(lines, fmt.Sprintf("            ┌───┴───┐      ┌───┴───┐"))
	lines = append(lines, fmt.Sprintf("            │%s│      │%s│",
		hashStyle.Render("inner-L"), hashStyle.Render("inner-R")))
	lines = append(lines, fmt.Sprintf("            └───┬───┘      └───┬───┘"))
	lines = append(lines, fmt.Sprintf("           leaf#%d %s",
		p.CursorN, cursorStyle.Render("← YOU ARE HERE")))
	lines = append(lines, "")

	lines = append(lines, mutedStyle.Render(
		"  [h/l] navigate  ·  [+/-] zoom  ·  [Esc/w] exit"))

	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(styles.ColorTeal).
		Width(p.Width - 2).
		Height(p.Height - 2)

	return box.Render(strings.Join(lines, "\n"))
}
