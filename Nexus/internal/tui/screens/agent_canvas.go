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
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type agentListMsg struct {
	agents  []api.AgentSummary
	errKind api.ErrorKind
	hint    string
}

// AgentCanvasScreen is Page 5 — A2A orchestration flow.
type AgentCanvasScreen struct {
	width, height int
	agents        []api.AgentSummary
	errKind       api.ErrorKind
	errHint       string
	loading       bool
}

// NewAgentCanvasScreen creates the agent canvas.
func NewAgentCanvasScreen() *AgentCanvasScreen {
	return &AgentCanvasScreen{loading: true}
}

func (a *AgentCanvasScreen) Name() string            { return "Agents" }
func (a *AgentCanvasScreen) Init() tea.Cmd            { return nil }
func (a *AgentCanvasScreen) SetSize(w, h int)         { a.width = w; a.height = h }
func (a *AgentCanvasScreen) ShortHelp() []key.Binding { return nil }

func (a *AgentCanvasScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if m, ok := msg.(agentListMsg); ok {
		a.loading = false
		a.errKind = m.errKind
		a.errHint = m.hint
		if m.errKind == api.ErrKindUnknown {
			a.agents = m.agents
		}
	}
	return a, nil
}

func (a *AgentCanvasScreen) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		agents, err := client.Agents()
		if err != nil {
			kind := api.Classify(err)
			sdbg("Agents failed kind=%d err=%v", kind, err)
			return agentListMsg{errKind: kind, hint: api.HintForEndpoint("/api/control/agents", kind)}
		}
		return agentListMsg{agents: agents}
	}
}

func (a *AgentCanvasScreen) View() string {
	if a.width < 40 || a.height < 10 {
		return ""
	}

	if a.loading {
		frame := int(time.Now().UnixMilli()/150) % 8
		return components.Render(loadingOpts(a.width, a.height, frame))
	}
	if a.errKind != api.ErrKindUnknown {
		return components.Render(emptyStateOpts(a.errKind, a.errHint, a.width, a.height))
	}

	var lines []string
	lines = append(lines, sectionHeader("AGENT ORCHESTRATION", a.width))
	lines = append(lines, "")

	canvasH := a.height * 70 / 100
	if canvasH < 8 {
		canvasH = 8
	}

	if len(a.agents) == 0 {
		centered := lipgloss.Place(a.width, canvasH, lipgloss.Center, lipgloss.Center,
			styles.MutedStyle.Render("No agents registered. Connect via MCP or A2A."))
		lines = append(lines, centered)
	} else {
		lines = append(lines, a.viewNodeGraph(canvasH))
	}

	lines = append(lines, "")
	lines = append(lines, sectionHeader("ACTIVE FLOWS", a.width))
	lines = append(lines, "")

	if len(a.agents) == 0 {
		lines = append(lines, styles.MutedStyle.Render("  No active flows"))
	} else {
		for _, ag := range a.agents {
			dot := agentDot(ag.Status)
			name := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(ag.DisplayName)
			status := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).Render(ag.Status)
			tier := lipgloss.NewStyle().Foreground(styles.ColorTealDim).
				Render(fmt.Sprintf("tier-%d", ag.TrustTier))
			lines = append(lines, fmt.Sprintf("  %s %s  %s  %s", dot, name, status, tier))
		}
	}

	return lipgloss.NewStyle().Width(a.width).Height(a.height).
		Render(strings.Join(lines, "\n"))
}

func (a *AgentCanvasScreen) viewNodeGraph(height int) string {
	var lines []string

	nodeStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderStrong).
		Padding(0, 1).
		Width(14)

	nexusNode := nodeStyle.Copy().BorderForeground(styles.ColorTeal).
		Render(lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
			Render("Nexus\n(core)"))

	var agentNodes []string
	for _, ag := range a.agents {
		color := styles.TextPrimary
		if ag.Status != "active" && ag.Status != "online" {
			color = styles.TextMuted
		}
		node := nodeStyle.Render(
			lipgloss.NewStyle().Foreground(color).Render(ag.DisplayName))
		agentNodes = append(agentNodes, node)
	}

	lines = append(lines, lipgloss.PlaceHorizontal(a.width, lipgloss.Center, nexusNode))

	arrow := lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("│")
	lines = append(lines, lipgloss.PlaceHorizontal(a.width, lipgloss.Center, arrow))

	if len(agentNodes) > 0 {
		row := lipgloss.JoinHorizontal(lipgloss.Top, agentNodes...)
		lines = append(lines, lipgloss.PlaceHorizontal(a.width, lipgloss.Center, row))
	}

	for len(lines) < height {
		lines = append(lines, "")
	}

	return strings.Join(lines[:height], "\n")
}

var _ Screen = (*AgentCanvasScreen)(nil)
