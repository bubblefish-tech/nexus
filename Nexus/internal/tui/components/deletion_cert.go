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

// DeletionCertProps configures the deletion certificate viewer.
type DeletionCertProps struct {
	CertID           string
	MemoryID         string
	ContentHash      string
	DeletedAt        string
	Actor            string
	Reason           string
	MerkleRoot       string
	SignerPubKey     string
	Signature        string
	Width            int
}

// RenderDeletionCert renders the issued deletion certificate modal.
func RenderDeletionCert(p DeletionCertProps) string {
	hashStyle := lipgloss.NewStyle().Foreground(styles.ColorTealDim)
	labelStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
	valueStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary)

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render(" DELETION CERTIFICATE ISSUED"))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("ID:"), valueStyle.Render(p.CertID)))
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Deleted memory:"), valueStyle.Render(p.MemoryID)))
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Content hash:"), hashStyle.Render(p.ContentHash)))
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Deleted at:"), valueStyle.Render(p.DeletedAt)))
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Actor:"), valueStyle.Render(p.Actor)))
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Reason:"), valueStyle.Render(p.Reason)))
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Merkle root:"), hashStyle.Render(p.MerkleRoot)))
	lines = append(lines, fmt.Sprintf("  %s  %s", labelStyle.Render("Signer pub key:"), hashStyle.Render(p.SignerPubKey)))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s", labelStyle.Render("Signature:")))

	sigChunks := splitIntoChunks(p.Signature, 32)
	for _, chunk := range sigChunks {
		lines = append(lines, "    "+hashStyle.Render(chunk))
	}
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("  [v] verify  ·  [o] export json  ·  [c] copy  ·  [Enter] close"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorTeal).
		Width(p.Width - 4)

	return box.Render(strings.Join(lines, "\n"))
}

func splitIntoChunks(s string, size int) []string {
	if size <= 0 {
		return []string{s}
	}
	var chunks []string
	for len(s) > size {
		chunks = append(chunks, s[:size])
		s = s[size:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}
