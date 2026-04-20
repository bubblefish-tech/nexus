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

package tui

import (
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// View renders the entire TUI.
func (m Model) View() string {
	// Minimum size guard.
	if m.width < 80 || m.height < 20 {
		msg := fmt.Sprintf("Terminal too small (minimum 80×20). Current: %d×%d.", m.width, m.height)
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			lipgloss.NewStyle().Foreground(styles.ColorAmber).Bold(true).Render(msg))
	}

	// Daemon not running state.
	if !m.daemonUp {
		return m.viewDaemonDown()
	}

	// Help overlay.
	if m.showHelp {
		return m.viewHelp()
	}

	// Normal layout: tabbar + (sidebar + main) + statusbar.
	tabbar := m.viewTabbar()
	sbStatus := "—"
	sbVersion := "—"
	sbQueue := 0
	if !m.daemonUp {
		sbStatus = "down"
	}
	if m.statusCache != nil {
		sbStatus = m.statusCache.Status
		sbVersion = m.statusCache.Version
		sbQueue = m.statusCache.QueueDepth
	}
	statusbar := components.Statusbar{
		Data: components.StatusbarData{
			Status:  sbStatus,
			Version: sbVersion,
			Queue:   sbQueue,
			Paused:  m.paused,
		},
		Width: m.width,
	}.View()

	tabbarH := lipgloss.Height(tabbar)
	statusbarH := 1
	mainH := m.height - tabbarH - statusbarH

	sidebarW := 0
	var sidebar string
	if m.sidebarOpen {
		sidebarW = components.SidebarWidth
		sidebar = components.Sidebar{
			Sections: m.buildSidebarSections(),
			Height:   mainH,
		}.View()
	}

	mainW := m.width - sidebarW
	if mainW < 40 {
		mainW = 40
	}

	var tabContent string
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		tabContent = m.tabs[m.activeTab].View(mainW, mainH)
	}

	mainPane := lipgloss.NewStyle().
		Width(mainW).
		Height(mainH).
		Render(tabContent)

	var body string
	if m.sidebarOpen {
		body = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, mainPane)
	} else {
		body = mainPane
	}

	return lipgloss.JoinVertical(lipgloss.Left, tabbar, body, statusbar)
}

func (m Model) viewTabbar() string {
	var tabs []string
	for i, t := range m.tabs {
		label := fmt.Sprintf(" %d %s ", i+1, t.Name())
		if i == m.activeTab {
			tabs = append(tabs, styles.ActiveTab.Render(label))
		} else {
			tabs = append(tabs, styles.InactiveTab.Render(label))
		}
	}
	bar := lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)
	line := lipgloss.NewStyle().Foreground(styles.BorderBase).Render(strings.Repeat("─", m.width))
	return lipgloss.JoinVertical(lipgloss.Left, bar, line)
}

func (m Model) viewDaemonDown() string {
	title := lipgloss.NewStyle().Foreground(styles.ColorRed).Bold(true).
		Render("  DAEMON NOT RUNNING")
	body := lipgloss.NewStyle().Foreground(styles.TextSecondary).
		Render(fmt.Sprintf("\n  Start with: bubblefish start\n\n  Retry count: %d\n  Press 'r' to retry, 'q' to quit.", m.retryCount))
	content := lipgloss.JoinVertical(lipgloss.Left, title, body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) viewHelp() string {
	helpText := `
  KEYBINDINGS

  1-7          Switch to tab N
  tab/S-tab    Next / previous tab
  q / ctrl+c   Quit
  r            Force refresh active tab
  ctrl+r       Toggle auto-refresh (pause)
  H            Toggle sidebar
  ?            Toggle this help

  SCROLLABLE PANES
  j/k  ↑/↓    Scroll up/down
  ctrl+d/u     Half-page down/up
  g / G        Jump to top / bottom

  AUDIT TAB
  /            Open text filter
  a            Toggle auto-scroll
  esc          Clear filter

  PIPELINE TAB
  b            Toggle black box mode

  CONFLICTS TAB
  p / n        Previous / next conflict

  TIME-TRAVEL TAB
  Enter        Execute query
  esc          Cancel input

  SETTINGS TAB
  e            Print edit instruction

  Press ? or esc to close.
`
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorTeal).
		Foreground(styles.TextPrimary).
		Padding(1, 2).
		Render(helpText)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) buildSidebarSections() []components.SidebarSection {
	dotStatus := components.DotOnline
	statusVal := "LIVE"
	if !m.daemonUp {
		dotStatus = components.DotOffline
		statusVal = "DOWN"
	}
	statusDot := components.StatusDot{Status: dotStatus, Frame: m.dotFrame}.View()

	ver := "—"
	queue := "0"
	consistency := "—"
	if m.statusCache != nil {
		ver = m.statusCache.Version
		queue = fmt.Sprintf("%d", m.statusCache.QueueDepth)
		if m.statusCache.ConsistencyScore >= 0 {
			consistency = fmt.Sprintf("%.2f", m.statusCache.ConsistencyScore)
		}
	}

	all := map[string]components.SidebarSection{
		"Daemon": {
			Title: "Daemon",
			Items: []components.SidebarItem{
				{Name: "Status", Value: statusVal, Dot: statusDot},
				{Name: "Version", Value: ver},
				{Name: "Mode", Value: "simple"},
			},
		},
		"Sources": {
			Title: "Sources",
			Items: []components.SidebarItem{
				{Name: "default", Value: "active", Dot: "green"},
			},
		},
		"Destinations": {
			Title: "Destinations",
			Items: []components.SidebarItem{
				{Name: "sqlite", Value: "ok", Dot: "green"},
				{Name: "wal", Value: queue + " pend"},
			},
		},
		"Ports": {
			Title: "Ports",
			Items: []components.SidebarItem{
				{Name: "API", Value: ":8080"},
				{Name: "MCP", Value: ":7474"},
				{Name: "Dashboard", Value: ":8081"},
			},
		},
		"Health": {
			Title: "Health",
			Items: []components.SidebarItem{
				{Name: "Consistency", Value: consistency},
				{Name: "Queue", Value: queue},
			},
		},
	}

	available := []string{"Daemon", "Sources", "Destinations", "Ports", "Health"}
	ordered := m.prefs.ApplySidebarOrder(available)

	sections := make([]components.SidebarSection, 0, len(ordered))
	for _, name := range ordered {
		if sec, ok := all[name]; ok {
			sections = append(sections, sec)
		}
	}
	return sections
}
