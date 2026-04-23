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
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var tunnelProviders = []struct {
	key  string
	name string
	hint string
}{
	{"cloudflare", "Cloudflare Tunnel", "cloudflared tunnel run"},
	{"ngrok", "ngrok", "ngrok http 7474"},
	{"tailscale", "Tailscale Funnel", "tailscale funnel 7474"},
	{"bore", "Bore", "bore local 7474 --to bore.pub"},
	{"custom", "Custom", "provide your own endpoint URL"},
}

// TunnelPage handles optional tunnel configuration.
type TunnelPage struct {
	providerCursor int
	endpointInput  textinput.Model
	inputActive    bool
}

var _ Page = (*TunnelPage)(nil)

// NewTunnelPage returns a TunnelPage.
func NewTunnelPage() *TunnelPage {
	ti := textinput.New()
	ti.Placeholder = "https://your-tunnel-endpoint"
	ti.CharLimit = 256
	return &TunnelPage{endpointInput: ti}
}

func (p *TunnelPage) Name() string { return "Tunnel Configuration" }

func (p *TunnelPage) Init(state *WizardState) tea.Cmd {
	p.endpointInput.SetValue(state.TunnelEndpoint)
	p.inputActive = false
	return nil
}

func (p *TunnelPage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	if p.inputActive {
		if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "esc" || k.String() == "shift+tab") {
			p.inputActive = false
			p.endpointInput.Blur()
			return p, nil
		}
		var cmd tea.Cmd
		p.endpointInput, cmd = p.endpointInput.Update(msg)
		state.TunnelEndpoint = p.endpointInput.Value()
		return p, cmd
	}

	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "y", "Y":
			state.TunnelEnabled = true
		case "n", "N":
			state.TunnelEnabled = false
			state.TunnelProvider = ""
			state.TunnelEndpoint = ""
		case "up", "k":
			if state.TunnelEnabled && p.providerCursor > 0 {
				p.providerCursor--
				state.TunnelProvider = tunnelProviders[p.providerCursor].key
			}
		case "down", "j":
			if state.TunnelEnabled && p.providerCursor < len(tunnelProviders)-1 {
				p.providerCursor++
				state.TunnelProvider = tunnelProviders[p.providerCursor].key
			}
		case " ", "enter":
			if state.TunnelEnabled {
				state.TunnelProvider = tunnelProviders[p.providerCursor].key
				p.inputActive = true
				p.endpointInput.Focus()
				return p, textinput.Blink
			}
		}
	}
	return p, nil
}

func (p *TunnelPage) CanAdvance(state *WizardState) bool {
	if !state.TunnelEnabled {
		return true
	}
	return state.TunnelProvider != "" && strings.TrimSpace(state.TunnelEndpoint) != ""
}

func (p *TunnelPage) View(width, height int) string {
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Tunnel Configuration")
	b.WriteString(title + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Tunnels expose the MCP server (:7474) to remote AI clients.") + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextSecondary).
		Render("Connect to a tunnel? ") +
		lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("[Y]") +
		lipgloss.NewStyle().Foreground(styles.TextMuted).Render(" Yes  ") +
		lipgloss.NewStyle().Foreground(styles.ColorAmber).Render("[N]") +
		lipgloss.NewStyle().Foreground(styles.TextMuted).Render(" No") + "\n\n")

	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Provider Options:\n"))
	for i, prov := range tunnelProviders {
		cursor := "  "
		rowStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
		if i == p.providerCursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			rowStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
		}
		b.WriteString(fmt.Sprintf("%s%s  %s\n",
			cursor,
			rowStyle.Render(prov.name),
			lipgloss.NewStyle().Foreground(styles.TextMuted).Render(prov.hint),
		))
	}
	b.WriteString("\n")
	if p.inputActive {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorBlue).
			Render("Endpoint URL: ") + p.endpointInput.View() + "\n")
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

// ViewWithState renders with the actual tunnel state.
func (p *TunnelPage) ViewWithState(width, height int, state *WizardState) string {
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Tunnel Configuration")
	b.WriteString(title + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Tunnels expose the MCP server (:7474) to remote AI clients.") + "\n\n")

	enabled := "No"
	if state.TunnelEnabled {
		enabled = lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("Yes")
	}
	b.WriteString(fmt.Sprintf("Connect to a tunnel: %s\n\n", enabled))

	if !state.TunnelEnabled {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("Press Y to enable  ·  Ctrl+N to skip"))
		return lipgloss.NewStyle().Width(width).Render(b.String())
	}

	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).Render("Select Provider:\n"))
	for i, prov := range tunnelProviders {
		cursor := "  "
		rowStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
		if i == p.providerCursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			rowStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
		}
		b.WriteString(fmt.Sprintf("%s%s  %s\n",
			cursor,
			rowStyle.Render(prov.name),
			lipgloss.NewStyle().Foreground(styles.TextMuted).Render(prov.hint),
		))
	}
	b.WriteString("\n")
	if p.inputActive || state.TunnelEndpoint != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorBlue).Render("Endpoint URL: "))
		if p.inputActive {
			b.WriteString(p.endpointInput.View())
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(state.TunnelEndpoint))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if state.TunnelProvider == "" || strings.TrimSpace(state.TunnelEndpoint) == "" {
		b.WriteString(lipgloss.NewStyle().Foreground(styles.ColorAmber).
			Render("Select a provider and enter an endpoint URL to continue."))
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}
