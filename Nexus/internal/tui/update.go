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
	"github.com/bubblefish-tech/nexus/internal/tui/api"
	tea "github.com/charmbracelet/bubbletea"
)

func fetchStatusCache(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.Status()
		return statusCacheMsg{data: data, err: err}
	}
}

// Update handles all messages for the root model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case healthCheckMsg:
		if msg.err != nil || !msg.ok {
			m.daemonUp = false
			m.lastErr = msg.err
			return m, nil
		}
		m.daemonUp = true
		m.lastErr = nil
		// Lazy-init the first tab on connection.
		if !m.tabInited[m.activeTab] {
			m.tabInited[m.activeTab] = true
			tabCmd := m.tabs[m.activeTab].FireRefresh(m.client)
			statusCmd := fetchStatusCache(m.client)
			return m, tea.Batch(tabCmd, statusCmd)
		}
		return m, nil

	case statusCacheMsg:
		if msg.err == nil && msg.data != nil {
			m.statusCache = msg.data
		}
		return m, nil

	case tickMsg:
		if !m.daemonUp {
			return m, tea.Batch(healthCheckCmd(m.client), tickCmd())
		}
		if m.paused {
			return m, tickCmd()
		}
		tabCmd := m.tabs[m.activeTab].FireRefresh(m.client)
		statusCmd := fetchStatusCache(m.client)
		return m, tea.Batch(tabCmd, statusCmd, tickCmd())

	case dotTickMsg:
		m.dotFrame++
		return m, dotTickCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Route all other messages to the active tab.
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		updated, cmd := m.tabs[m.activeTab].Update(msg)
		m.tabs[m.activeTab] = updated
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global keys — always active.
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	case "ctrl+r":
		m.paused = !m.paused
		return m, nil
	case "H":
		m.sidebarOpen = !m.sidebarOpen
		return m, nil
	case "r":
		if !m.daemonUp {
			m.retryCount++
			return m, healthCheckCmd(m.client)
		}
		if m.daemonUp {
			cmd := m.tabs[m.activeTab].FireRefresh(m.client)
			return m, cmd
		}
		return m, nil
	}

	// Tab switching — only when help is not showing.
	if !m.showHelp {
		switch key {
		case "1":
			return m.switchTab(0)
		case "2":
			return m.switchTab(1)
		case "3":
			return m.switchTab(2)
		case "4":
			return m.switchTab(3)
		case "5":
			return m.switchTab(4)
		case "6":
			return m.switchTab(5)
		case "7":
			return m.switchTab(6)
		case "tab":
			next := (m.activeTab + 1) % len(m.tabs)
			return m.switchTab(next)
		case "shift+tab":
			prev := (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			return m.switchTab(prev)
		}
	}

	// Escape closes help.
	if key == "esc" && m.showHelp {
		m.showHelp = false
		return m, nil
	}

	// Route to active tab.
	if m.activeTab >= 0 && m.activeTab < len(m.tabs) {
		updated, cmd := m.tabs[m.activeTab].Update(msg)
		m.tabs[m.activeTab] = updated
		return m, cmd
	}

	return m, nil
}

func (m Model) switchTab(idx int) (tea.Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.tabs) {
		return m, nil
	}
	m.activeTab = idx
	if !m.tabInited[idx] && m.daemonUp {
		m.tabInited[idx] = true
		cmd := m.tabs[idx].FireRefresh(m.client)
		return m, cmd
	}
	return m, nil
}
