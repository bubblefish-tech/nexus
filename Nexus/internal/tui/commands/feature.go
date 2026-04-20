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

// FeatureResultMsg carries the current feature state for /enable and /disable.
type FeatureResultMsg struct {
	Config *api.ConfigResponse
	Err    error
}

// EnableCommand is the /enable slash command.
type EnableCommand struct{}

var _ Command = EnableCommand{}

func (EnableCommand) Name() string        { return "enable" }
func (EnableCommand) Description() string { return "Enable a feature (e.g. /enable mcp)" }

func (EnableCommand) Execute(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		cfg, err := client.Config()
		return FeatureResultMsg{Config: cfg, Err: err}
	}
}

// DisableFeatureCommand is the /disable slash command.
type DisableFeatureCommand struct{}

var _ Command = DisableFeatureCommand{}

func (DisableFeatureCommand) Name() string        { return "disable" }
func (DisableFeatureCommand) Description() string { return "Disable a feature (e.g. /disable mcp)" }

func (DisableFeatureCommand) Execute(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		cfg, err := client.Config()
		return FeatureResultMsg{Config: cfg, Err: err}
	}
}
