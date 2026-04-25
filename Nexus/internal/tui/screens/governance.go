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

type govGrantsMsg struct {
	data *api.GrantsResponse
	err  error
}

type govApprovalsMsg struct {
	data *api.ApprovalsResponse
	err  error
}

type govTasksMsg struct {
	data *api.TasksResponse
	err  error
}

// GovernanceScreen is Page 7 — grants, approvals, policy log.
type GovernanceScreen struct {
	width, height int
	grants        []api.Grant
	approvals     []api.Approval
	tasks         []api.Task
	err           error
}

// NewGovernanceScreen creates the governance page.
func NewGovernanceScreen() *GovernanceScreen {
	return &GovernanceScreen{}
}

func (g *GovernanceScreen) Name() string            { return "Gov" }
func (g *GovernanceScreen) Init() tea.Cmd            { return nil }
func (g *GovernanceScreen) SetSize(w, h int)         { g.width = w; g.height = h }
func (g *GovernanceScreen) ShortHelp() []key.Binding { return nil }

func (g *GovernanceScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case govGrantsMsg:
		if m.err == nil && m.data != nil {
			g.grants = m.data.Grants
		} else {
			g.err = m.err
		}
	case govApprovalsMsg:
		if m.err == nil && m.data != nil {
			g.approvals = m.data.Approvals
		} else {
			g.err = m.err
		}
	case govTasksMsg:
		if m.err == nil && m.data != nil {
			g.tasks = m.data.Tasks
		} else {
			g.err = m.err
		}
	}
	return g, nil
}

func (g *GovernanceScreen) FireRefresh(client *api.Client) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			data, err := client.Grants()
			return govGrantsMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := client.Approvals()
			return govApprovalsMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := client.Tasks()
			return govTasksMsg{data: data, err: err}
		},
	)
}

func (g *GovernanceScreen) View() string {
	if g.width < 40 || g.height < 10 {
		return ""
	}

	var lines []string

	// ── Grants ──
	lines = append(lines, sectionHeader("GRANTS", g.width))
	lines = append(lines, "")

	if len(g.grants) == 0 {
		lines = append(lines, styles.MutedStyle.Render("  No active grants"))
	} else {
		for _, gr := range g.grants {
			agent := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(gr.AgentID)
			cap := lipgloss.NewStyle().Foreground(styles.ColorTeal).Render(gr.Capability)
			scope := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).Render(gr.Scope)
			lines = append(lines, fmt.Sprintf("  %s  →  %s  scope: %s", agent, cap, scope))
		}
	}
	lines = append(lines, "")

	// ── Pending Approvals ──
	lines = append(lines, sectionHeader("PENDING APPROVALS", g.width))
	lines = append(lines, "")

	pending := 0
	for _, a := range g.approvals {
		if a.Decision == "" || a.Decision == "pending" {
			pending++
		}
	}

	if pending == 0 {
		lines = append(lines, styles.MutedStyle.Render("  No pending approvals"))
	} else {
		for _, a := range g.approvals {
			if a.Decision != "" && a.Decision != "pending" {
				continue
			}
			agent := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(a.AgentID)
			cap := lipgloss.NewStyle().Foreground(styles.ColorAmber).Render(a.Capability)
			lines = append(lines, fmt.Sprintf("  ⏳ %s requests %s", agent, cap))
		}
	}
	lines = append(lines, "")

	// ── Active Tasks ──
	lines = append(lines, sectionHeader("ACTIVE TASKS", g.width))
	lines = append(lines, "")

	activeTasks := 0
	for _, t := range g.tasks {
		if t.Status == "running" || t.Status == "pending" {
			activeTasks++
		}
	}

	if activeTasks == 0 {
		lines = append(lines, styles.MutedStyle.Render("  No active tasks"))
	} else {
		for _, t := range g.tasks {
			if t.Status != "running" && t.Status != "pending" {
				continue
			}
			statusColor := styles.ColorAmber
			if t.Status == "running" {
				statusColor = styles.ColorGreen
			}
			agent := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(t.AgentID)
			status := lipgloss.NewStyle().Foreground(statusColor).Render(t.Status)
			cap := lipgloss.NewStyle().Foreground(styles.ColorTeal).Render(t.Capability)
			lines = append(lines, fmt.Sprintf("  %s  %s  %s", status, agent, cap))
		}
	}
	lines = append(lines, "")

	// ── Summary ──
	lines = append(lines, sectionHeader("POLICY SUMMARY", g.width))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  Grants: %d  ·  Pending: %d  ·  Tasks: %d",
		len(g.grants), pending, len(g.tasks)))

	if g.err != nil {
		lines = append(lines, "")
		lines = append(lines, styles.ErrorStyle.Render("  error: "+g.err.Error()))
	}

	return lipgloss.NewStyle().Width(g.width).Height(g.height).
		Render(strings.Join(lines, "\n"))
}

var _ Screen = (*GovernanceScreen)(nil)
