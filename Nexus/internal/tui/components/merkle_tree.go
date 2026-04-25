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

// MerkleProofProps configures the ASCII Merkle tree renderer.
type MerkleProofProps struct {
	Root       string
	LeafHash   string
	Siblings   []string
	Directions []string
	Valid      bool
	Width      int
}

// RenderMerkleTree draws an ASCII inclusion proof from leaf to root.
func RenderMerkleTree(p MerkleProofProps) string {
	if len(p.Siblings) == 0 {
		return lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("  Merkle proof not available for this entry.")
	}

	hashStyle := lipgloss.NewStyle().Foreground(styles.ColorTealDim)
	highlightStyle := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true)
	mutedStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)

	var lines []string

	rootLabel := "ROOT"
	if p.Valid {
		rootLabel += " ✓"
	}
	lines = append(lines, fmt.Sprintf("                    ┌─ %s: %s ─┐",
		highlightStyle.Render(rootLabel), hashStyle.Render(truncHash(p.Root, 16))))

	for i := len(p.Siblings) - 1; i >= 0; i-- {
		sib := p.Siblings[i]
		lines = append(lines, mutedStyle.Render("                    │"))
		lines = append(lines, fmt.Sprintf("              ┌─────┴─────┐        ┌───────────┐"))

		if i == 0 {
			lines = append(lines, fmt.Sprintf("              │ %s │ ← leaf  │ %s │",
				hashStyle.Render(truncHash(p.LeafHash, 10)),
				hashStyle.Render(truncHash(sib, 10))))
		} else {
			lines = append(lines, fmt.Sprintf("              │ %s │        │ %s │",
				hashStyle.Render(truncHash("...", 10)),
				hashStyle.Render(truncHash(sib, 10))))
		}
		lines = append(lines, fmt.Sprintf("              └───────────┘        └───────────┘"))
	}

	return strings.Join(lines, "\n")
}
