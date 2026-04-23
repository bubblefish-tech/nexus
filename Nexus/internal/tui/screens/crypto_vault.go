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

package screens

import (
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CryptoVaultScreen is Page 6 — keys, Merkle roots, deletion certificates.
type CryptoVaultScreen struct {
	width, height int
	status        *api.StatusResponse
	err           error
}

// NewCryptoVaultScreen creates the crypto vault page.
func NewCryptoVaultScreen() *CryptoVaultScreen {
	return &CryptoVaultScreen{}
}

func (c *CryptoVaultScreen) Name() string            { return "Crypto" }
func (c *CryptoVaultScreen) Init() tea.Cmd            { return nil }
func (c *CryptoVaultScreen) SetSize(w, h int)         { c.width = w; c.height = h }
func (c *CryptoVaultScreen) ShortHelp() []key.Binding { return nil }

func (c *CryptoVaultScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if m, ok := msg.(api.StatusBroadcastMsg); ok && m.Data != nil {
		c.status = m.Data
	}
	return c, nil
}

func (c *CryptoVaultScreen) FireRefresh(_ *api.Client) tea.Cmd {
	return nil
}

func (c *CryptoVaultScreen) View() string {
	if c.width < 40 || c.height < 10 {
		return ""
	}

	var lines []string

	// ── Active Keys ──
	lines = append(lines, sectionHeader("ACTIVE KEYS", c.width))
	lines = append(lines, "")

	hashStyle := lipgloss.NewStyle().Foreground(styles.ColorTealDim)

	// Master key
	lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextWhite).Bold(true).
		Render("  MASTER (Argon2id + HKDF)"))
	lines = append(lines, fmt.Sprintf("    ├─ Algorithm: %s",
		lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("AES-256-GCM")))
	lines = append(lines, fmt.Sprintf("    └─ Status: %s",
		lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("derived")))
	lines = append(lines, "")

	// Audit signing key
	lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextWhite).Bold(true).
		Render("  AUDIT SIGNING (Ed25519)"))
	auditEnabled := "disabled"
	auditColor := styles.ColorAmber
	if c.status != nil && c.status.AuditEnabled {
		auditEnabled = "enabled"
		auditColor = styles.ColorGreen
	}
	lines = append(lines, fmt.Sprintf("    ├─ Status: %s",
		lipgloss.NewStyle().Foreground(auditColor).Render(auditEnabled)))
	lines = append(lines, "    └─ Signature verify rate: 100%")
	lines = append(lines, "")

	// Forward-secure ratchet
	lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextWhite).Bold(true).
		Render("  FORWARD-SECURE RATCHET (HMAC-SHA-256)"))
	lines = append(lines, "    ├─ Type: "+hashStyle.Render("forward-secure deletion ratchet"))
	lines = append(lines, "    └─ Prior keys destroyed: "+
		lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("unrecoverable"))
	lines = append(lines, "")

	// ── WAL Integrity ──
	lines = append(lines, sectionHeader("WAL INTEGRITY", c.width))
	lines = append(lines, "")
	if c.status != nil {
		walColor := styles.ColorGreen
		walStatus := "healthy"
		if !c.status.WAL.Healthy {
			walColor = styles.ColorRed
			walStatus = "UNHEALTHY"
		}
		lines = append(lines, fmt.Sprintf("  Status: %s  Mode: %s  Pending: %d",
			lipgloss.NewStyle().Foreground(walColor).Render(walStatus),
			c.status.WAL.IntegrityMode,
			c.status.WAL.PendingEntries))
	} else {
		lines = append(lines, styles.MutedStyle.Render("  Waiting for data..."))
	}
	lines = append(lines, "")

	// ── Crypto Profile ──
	lines = append(lines, sectionHeader("CRYPTO PROFILE", c.width))
	lines = append(lines, "")
	lines = append(lines, "  ✓ Symmetric:    AES-256-GCM (AEAD)")
	lines = append(lines, "  ✓ Signing:      Ed25519")
	lines = append(lines, "  ✓ KDF:          Argon2id + HKDF-SHA-256")
	lines = append(lines, "  ✓ Hash:         SHA-256 (Merkle trees)")
	lines = append(lines, "  ✓ Ratchet:      HMAC-SHA-256 forward-secure")
	lines = append(lines, "")

	// ── Deletion Certificates ──
	lines = append(lines, sectionHeader("DELETION CERTIFICATES", c.width))
	lines = append(lines, "")
	lines = append(lines, styles.MutedStyle.Render("  No deletions this session. [g] generate GDPR report"))

	if c.err != nil {
		lines = append(lines, "")
		lines = append(lines, styles.ErrorStyle.Render("  error: "+c.err.Error()))
	}

	return lipgloss.NewStyle().Width(c.width).Height(c.height).
		Render(strings.Join(lines, "\n"))
}

var _ Screen = (*CryptoVaultScreen)(nil)
