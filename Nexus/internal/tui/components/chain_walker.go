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

// EntryCardProps configures the provenance chain entry card renderer.
type EntryCardProps struct {
	EntryN     int
	Total      int
	Timestamp  string
	RecordID   string
	PrevHash   string
	ContentID  string
	Preview    string
	Hash       string
	Signature  string
	SigValid   bool
	Width      int
}

// RenderEntryCard draws a provenance chain entry showing the
// prev_hash → content → hash → signature flow.
func RenderEntryCard(p EntryCardProps) string {
	w := p.Width
	if w < 40 {
		w = 40
	}
	inner := w - 4

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderStrong).
		Padding(0, 1).
		Width(w - 2)

	hashStyle := lipgloss.NewStyle().Foreground(styles.ColorTealDim)
	arrowStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
	arrow := arrowStyle.Render("                   ↓")

	header := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render(fmt.Sprintf("  Entry #%d", p.EntryN))
	ts := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).
		Render(p.Timestamp)
	titleLine := lipgloss.JoinHorizontal(lipgloss.Bottom, header, "  ", ts)

	prevHash := truncHash(p.PrevHash, inner-14)
	contentPreview := p.Preview
	if len(contentPreview) > inner-30 {
		contentPreview = contentPreview[:inner-33] + "..."
	}
	hash := truncHash(p.Hash, inner-14)
	sig := truncHash(p.Signature, inner-20)

	sigStatus := lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("verified ✓")
	if !p.SigValid {
		sigStatus = lipgloss.NewStyle().Foreground(styles.ColorRed).Render("UNVERIFIED ✗")
	}

	var lines []string
	lines = append(lines, titleLine)
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  prev_hash:  %s", hashStyle.Render(prevHash)))
	lines = append(lines, arrow)
	lines = append(lines, fmt.Sprintf("  content:    %s (%q)", hashStyle.Render(p.ContentID), contentPreview))
	lines = append(lines, arrow)
	lines = append(lines, fmt.Sprintf("  hash:       %s", hashStyle.Render(hash)))
	lines = append(lines, arrow)
	lines = append(lines, fmt.Sprintf("  signature:  ed25519: %s (%s)", hashStyle.Render(sig), sigStatus))

	return box.Render(strings.Join(lines, "\n"))
}

func truncHash(h string, maxLen int) string {
	if maxLen < 8 {
		maxLen = 8
	}
	if len(h) <= maxLen {
		return h
	}
	half := (maxLen - 1) / 2
	return h[:half] + "…" + h[len(h)-half:]
}
