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

import "github.com/charmbracelet/bubbles/key"

// GlobalKeyMap defines keybindings that are always active.
type GlobalKeyMap struct {
	Quit      key.Binding
	HardQuit  key.Binding
	Help      key.Binding
	Palette   key.Binding
	Slash     key.Binding
	Escape    key.Binding
	Refresh   key.Binding
	Pause     key.Binding
	NextPage  key.Binding
	PrevPage  key.Binding
	Tab1      key.Binding
	Tab2      key.Binding
	Tab3      key.Binding
	Tab4      key.Binding
	Tab5      key.Binding
	Tab6      key.Binding
	Tab7      key.Binding
	Tab8      key.Binding
}

// DefaultGlobalKeyMap returns the global keybindings.
func DefaultGlobalKeyMap() GlobalKeyMap {
	return GlobalKeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "quit"),
		),
		HardQuit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "force quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Palette: key.NewBinding(
			key.WithKeys("ctrl+k"),
			key.WithHelp("ctrl+k", "command palette"),
		),
		Slash: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "commands"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Pause: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "pause/resume"),
		),
		NextPage: key.NewBinding(
			key.WithKeys("ctrl+n", "right"),
			key.WithHelp("→/ctrl+n", "next page"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys("ctrl+p", "left"),
			key.WithHelp("←/ctrl+p", "prev page"),
		),
		Tab1: key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "Dashboard")),
		Tab2: key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "Memory")),
		Tab3: key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "Retrieval")),
		Tab4: key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "Audit")),
		Tab5: key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "Agents")),
		Tab6: key.NewBinding(key.WithKeys("6"), key.WithHelp("6", "Crypto")),
		Tab7: key.NewBinding(key.WithKeys("7"), key.WithHelp("7", "Governance")),
		Tab8: key.NewBinding(key.WithKeys("8"), key.WithHelp("8", "Immune")),
	}
}

// tabBindings returns an ordered slice of the nine tab keybindings.
func (k GlobalKeyMap) tabBindings() []key.Binding {
	return []key.Binding{
		k.Tab1, k.Tab2, k.Tab3, k.Tab4, k.Tab5,
		k.Tab6, k.Tab7, k.Tab8,
	}
}

// tabStateForIndex maps a 0-based tab index to an AppState.
func tabStateForIndex(idx int) AppState {
	states := []AppState{
		StateDashboard,
		StateMemoryBrowser,
		StateRetrievalTheater,
		StateAuditWalker,
		StateAgentCanvas,
		StateCryptoVault,
		StateGovernance,
		StateImmuneTheater,
	}
	if idx >= 0 && idx < len(states) {
		return states[idx]
	}
	return StateDashboard
}

// tabIndexForState maps an AppState to a 0-based tab index.
func tabIndexForState(s AppState) int {
	switch s {
	case StateDashboard:
		return 0
	case StateMemoryBrowser:
		return 1
	case StateRetrievalTheater:
		return 2
	case StateAuditWalker:
		return 3
	case StateAgentCanvas:
		return 4
	case StateCryptoVault:
		return 5
	case StateGovernance:
		return 6
	case StateImmuneTheater:
		return 7
	default:
		return 0
	}
}
