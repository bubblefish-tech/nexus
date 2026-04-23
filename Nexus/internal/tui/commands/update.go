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
	"github.com/bubblefish-tech/nexus/internal/version"
	tea "github.com/charmbracelet/bubbletea"
)

// UpdateResultMsg carries the update-check result.
type UpdateResultMsg struct {
	CurrentVersion string
	Message        string
}

// UpdateCommand checks for and installs software updates.
// Full implementation added in WIRE.6.
type UpdateCommand struct{}

var _ Command = UpdateCommand{}

func (UpdateCommand) Name() string        { return "update" }
func (UpdateCommand) Description() string { return "Check for and install updates" }

func (UpdateCommand) Execute(_ *api.Client) tea.Cmd {
	return func() tea.Msg {
		return UpdateResultMsg{
			CurrentVersion: version.Version,
			Message:        "Update check not yet implemented.",
		}
	}
}
