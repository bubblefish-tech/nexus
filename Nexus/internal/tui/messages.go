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
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	tea "github.com/charmbracelet/bubbletea"
)

// AppState identifies which screen is active in the root model.
type AppState int

const (
	StateSplash           AppState = iota
	StateDashboard                 // Main overview (default landing)
	StateMemoryBrowser             // Search + inspect memories
	StateRetrievalTheater          // Watch queries traverse cascade
	StateAuditWalker               // Step through hash chain
	StateAgentCanvas               // A2A orchestration flow
	StateCryptoVault               // Keys, Merkle roots, deletion certs
	StateGovernance                // Grants, approvals, policy log
	StateImmuneTheater             // Quarantine + threat signatures
)

// NavigateMsg requests a screen transition.
type NavigateMsg struct{ To AppState }

// StatusRefreshMsg carries a fresh /api/status response.
type StatusRefreshMsg struct {
	Data *api.StatusResponse
	Err  error
}

// HealthCheckResultMsg reports daemon availability.
type HealthCheckResultMsg struct {
	OK  bool
	Err error
}

// DataTickMsg drives periodic API refresh (5s interval).
type DataTickMsg time.Time

// DotTickMsg drives the status dot pulse animation (500ms interval).
type DotTickMsg time.Time

// SplashDoneMsg signals the splash animation has completed.
type SplashDoneMsg struct{}

// cmdResultMsg is a simple text result from a slash command (e.g. /theme).
type cmdResultMsg string

// dataTickCmd returns a command that fires DataTickMsg after 5 seconds.
func dataTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return DataTickMsg(t) })
}

// dotTickCmd returns a command that fires DotTickMsg every 500ms.
func dotTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return DotTickMsg(t) })
}

// healthCheckCmd checks daemon availability via the API client.
func healthCheckCmd(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		ok, err := client.Health()
		return HealthCheckResultMsg{OK: ok, Err: err}
	}
}

// fetchStatusCmd fetches /api/status.
func fetchStatusCmd(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.Status()
		return StatusRefreshMsg{Data: data, Err: err}
	}
}
