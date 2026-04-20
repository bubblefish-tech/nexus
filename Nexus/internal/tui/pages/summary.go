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

package pages

import (
	"fmt"
	"sort"
	"strings"

	nexusinstall "github.com/bubblefish-tech/nexus/internal/install"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// installResultMsg carries the outcome of the install operation.
type installResultMsg struct {
	err error
}

// SummaryPage shows a read-only config summary and triggers installation.
type SummaryPage struct {
	installing bool
	installed  bool
	err        string
}

var _ Page = (*SummaryPage)(nil)

// NewSummaryPage returns a SummaryPage.
func NewSummaryPage() *SummaryPage { return &SummaryPage{} }

func (p *SummaryPage) Name() string { return "Summary" }

func (p *SummaryPage) Init(_ *WizardState) tea.Cmd { return nil }

func (p *SummaryPage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case installResultMsg:
		p.installing = false
		if msg.err != nil {
			p.err = msg.err.Error()
			return p, nil
		}
		p.installed = true
		configDir := state.InstallDir
		return p, func() tea.Msg { return WizardCompleteMsg{ConfigDir: configDir} }

	case tea.KeyMsg:
		if p.installed || p.installing {
			return p, nil
		}
		if msg.String() == "enter" {
			p.installing = true
			p.err = ""
			st := *state // snapshot
			return p, func() tea.Msg {
				err := runInstallFromState(&st)
				return installResultMsg{err: err}
			}
		}
	}
	return p, nil
}

func (p *SummaryPage) CanAdvance(_ *WizardState) bool { return false }

func (p *SummaryPage) View(width, height int) string {
	return lipgloss.NewStyle().Foreground(styles.TextMuted).Width(width).
		Render("Loading summary…")
}

// ViewWithState renders the full summary with current wizard state.
func (p *SummaryPage) ViewWithState(width, height int, state *WizardState) string {
	var b strings.Builder

	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Installation Summary")
	b.WriteString(title + "\n\n")

	row := func(label, value string) string {
		return fmt.Sprintf("  %s  %s\n",
			lipgloss.NewStyle().Foreground(styles.TextMuted).Width(22).Render(label+":"),
			lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(value),
		)
	}

	b.WriteString(row("Mode", orDash(state.Mode)))
	b.WriteString(row("Install directory", orDash(state.InstallDir)))
	b.WriteString(row("Database", orDash(state.DatabaseType)))
	if state.DatabaseDSN != "" {
		b.WriteString(row("DSN", maskDSN(state.DatabaseDSN)))
	}
	enc := "disabled"
	if state.EncryptionPass != "" {
		enc = "enabled (password set)"
	}
	b.WriteString(row("Encryption", enc))
	tunnel := "disabled"
	if state.TunnelEnabled && state.TunnelProvider != "" {
		tunnel = state.TunnelProvider
	}
	b.WriteString(row("Tunnel", tunnel))
	b.WriteString(row("Discovered tools", fmt.Sprintf("%d found, %d selected",
		len(state.DiscoveredTools), countSelected(state))))

	// Features
	if len(state.Features) > 0 {
		var enabled []string
		for k, v := range state.Features {
			if v {
				enabled = append(enabled, k)
			}
		}
		sort.Strings(enabled)
		if len(enabled) > 0 {
			b.WriteString(row("Features", strings.Join(enabled, ", ")))
		}
	}

	b.WriteString("\n")

	if p.err != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorRed).Bold(true).
			Render("✗  Install failed: "+p.err) + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("Press Enter to retry or Ctrl+B to go back."))
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}

	if p.installing {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTeal).
			Render("⠋  Installing…"))
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}

	if p.installed {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorGreen).Bold(true).
			Render("✓  Installation complete!") + "\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextSecondary).
			Render("Run `bubblefish start` to start the daemon."))
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}

	b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Press Enter to install  ·  Ctrl+B to go back"))
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func countSelected(state *WizardState) int {
	n := 0
	for _, v := range state.SelectedTools {
		if v {
			n++
		}
	}
	return n
}

// maskDSN hides the password portion of a DSN string.
func maskDSN(dsn string) string {
	if len(dsn) <= 8 {
		return "***"
	}
	return dsn[:8] + "…"
}

// runInstallFromState generates the Nexus config from wizard state.
func runInstallFromState(state *WizardState) error {
	return nexusinstall.Install(nexusinstall.Options{
		ConfigDir:      state.InstallDir,
		Mode:           state.Mode,
		DestType:       state.DatabaseType,
		DSN:            state.DatabaseDSN,
		EncryptionPass: state.EncryptionPass,
		Force:          false,
	})
}
