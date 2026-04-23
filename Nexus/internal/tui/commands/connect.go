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

package commands

import (
	"github.com/bubblefish-tech/nexus/internal/tui/api"
	tea "github.com/charmbracelet/bubbletea"
)

// ConnectResultMsg carries the list of connected agents for /connect.
type ConnectResultMsg struct {
	Agents []api.AgentSummary
	Err    error
}

// ConnectCommand lists connected agents and their status.
type ConnectCommand struct{}

var _ Command = ConnectCommand{}

func (ConnectCommand) Name() string        { return "connect" }
func (ConnectCommand) Description() string { return "List connected AI tools and agents" }

func (ConnectCommand) Execute(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		agents, err := client.Agents()
		return ConnectResultMsg{Agents: agents, Err: err}
	}
}

// DisconnectCommand is the /disconnect slash command.
type DisconnectCommand struct{}

var _ Command = DisconnectCommand{}

func (DisconnectCommand) Name() string        { return "disconnect" }
func (DisconnectCommand) Description() string { return "Disconnect from an AI tool" }

func (DisconnectCommand) Execute(_ *api.Client) tea.Cmd {
	return func() tea.Msg {
		return ConnectResultMsg{Err: nil}
	}
}
