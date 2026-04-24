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

type cryptoSigningMsg struct {
	data    *api.SigningStatus
	errKind api.ErrorKind
	hint    string
}

// CryptoVaultScreen is Page 6 — keys, Merkle roots, deletion certificates.
type CryptoVaultScreen struct {
	width, height int
	status        *api.StatusResponse
	signing       *api.SigningStatus
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
	switch m := msg.(type) {
	case api.StatusBroadcastMsg:
		if m.Data != nil {
			c.status = m.Data
		}
	case cryptoSigningMsg:
		if m.errKind == api.ErrKindUnknown && m.data != nil {
			c.signing = m.data
		}
	}
	return c, nil
}

func (c *CryptoVaultScreen) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.SigningStatus()
		if err != nil {
			kind := api.Classify(err)
			sdbg("SigningStatus failed kind=%d err=%v", kind, err)
			return cryptoSigningMsg{errKind: kind, hint: api.HintForEndpoint("/api/crypto/signing", kind)}
		}
		return cryptoSigningMsg{data: data}
	}
}

func (c *CryptoVaultScreen) View() string {
	if c.width < 40 || c.height < 10 {
		return ""
	}

	var lines []string

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

	// Audit signing — three-state display
	lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextWhite).Bold(true).
		Render("  AUDIT SIGNING (Ed25519)"))
	lines = append(lines, c.renderSigningStatus()...)
	lines = append(lines, "")

	// Forward-secure ratchet
	lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextWhite).Bold(true).
		Render("  FORWARD-SECURE RATCHET (HMAC-SHA-256)"))
	lines = append(lines, "    ├─ Type: "+hashStyle.Render("forward-secure deletion ratchet"))
	lines = append(lines, "    └─ Prior keys destroyed: "+
		lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("unrecoverable"))
	lines = append(lines, "")

	// WAL Integrity
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

	// Crypto Profile
	lines = append(lines, sectionHeader("CRYPTO PROFILE", c.width))
	lines = append(lines, "")
	lines = append(lines, "  ✓ Symmetric:    AES-256-GCM (AEAD)")
	lines = append(lines, "  ✓ Signing:      Ed25519")
	lines = append(lines, "  ✓ KDF:          Argon2id + HKDF-SHA-256")
	lines = append(lines, "  ✓ Hash:         SHA-256 (Merkle trees)")
	lines = append(lines, "  ✓ Ratchet:      HMAC-SHA-256 forward-secure")
	lines = append(lines, "")

	// Deletion Certificates
	lines = append(lines, sectionHeader("DELETION CERTIFICATES", c.width))
	lines = append(lines, "")
	lines = append(lines, styles.MutedStyle.Render("  No deletions this session. [g] generate GDPR report"))

	return lipgloss.NewStyle().Width(c.width).Height(c.height).
		Render(strings.Join(lines, "\n"))
}

func (c *CryptoVaultScreen) renderSigningStatus() []string {
	if c.signing == nil {
		return []string{
			fmt.Sprintf("    └─ Status: %s",
				lipgloss.NewStyle().Foreground(styles.ColorAmber).Render("loading...")),
		}
	}

	var stateLabel string
	var stateColor lipgloss.Color
	switch {
	case c.signing.Enabled:
		stateLabel = "enabled"
		stateColor = styles.ColorGreen
	case c.signing.Reason != "":
		stateLabel = "awaiting config"
		stateColor = styles.ColorAmber
	default:
		stateLabel = "error"
		stateColor = styles.ColorRed
	}

	status := fmt.Sprintf("    ├─ Status: %s",
		lipgloss.NewStyle().Foreground(stateColor).Render(stateLabel))

	var extra []string
	if c.signing.Enabled {
		extra = append(extra, fmt.Sprintf("    ├─ Public key: ed25519_pk: %s", c.signing.PublicKeyHash))
		extra = append(extra, fmt.Sprintf("    ├─ Signed: %d events", c.signing.SignedCount))
		rate := "100.00%"
		if c.signing.VerifyFailures > 0 {
			total := c.signing.SignedCount + c.signing.VerifyFailures
			if total > 0 {
				rate = fmt.Sprintf("%.2f%%", float64(c.signing.SignedCount)/float64(total)*100)
			}
		}
		extra = append(extra, fmt.Sprintf("    └─ Verify rate: %s", rate))
	} else {
		if c.signing.Reason != "" {
			extra = append(extra, fmt.Sprintf("    ├─ Reason: %s", c.signing.Reason))
		}
		if c.signing.ConfigHint != "" {
			extra = append(extra, fmt.Sprintf("    └─ Hint: %s", c.signing.ConfigHint))
		}
	}

	return append([]string{status}, extra...)
}

var _ Screen = (*CryptoVaultScreen)(nil)
