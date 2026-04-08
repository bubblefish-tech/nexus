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

// Package tui implements the Bubble Tea terminal UI for BubbleFish Nexus.
package tui

import (
	"time"

	"github.com/BubbleFish-Nexus/internal/tui/api"
	"github.com/BubbleFish-Nexus/internal/tui/tabs"
	tea "github.com/charmbracelet/bubbletea"
)

// statusCacheMsg carries a fresh /api/status response for the root model.
type statusCacheMsg struct {
	data *api.StatusResponse
	err  error
}

// Model is the root Elm model for the TUI.
type Model struct {
	activeTab    int
	tabs         []tabs.Tab
	client       *api.Client
	width        int
	height       int
	lastErr      error
	paused       bool
	showHelp     bool
	sidebarOpen  bool
	daemonUp     bool
	retryCount   int
	tabInited    []bool // tracks which tabs have been lazily initialized
	statusCache  *api.StatusResponse
}

// tickMsg drives periodic API refresh.
type tickMsg time.Time

// healthCheckMsg is the result of a daemon health check.
type healthCheckMsg struct {
	ok  bool
	err error
}

func tickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func healthCheckCmd(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		ok, err := client.Health()
		return healthCheckMsg{ok: ok, err: err}
	}
}

// NewModel creates the root model with all tabs.
func NewModel(client *api.Client, tabList []tabs.Tab) Model {
	inited := make([]bool, len(tabList))
	return Model{
		activeTab:   0,
		tabs:        tabList,
		client:      client,
		sidebarOpen: true,
		daemonUp:    true,
		tabInited:   inited,
	}
}

// Init starts the first tick and health check.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), healthCheckCmd(m.client))
}
