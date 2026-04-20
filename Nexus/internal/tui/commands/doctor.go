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

// DoctorResultMsg carries the health-check result for the /doctor command.
type DoctorResultMsg struct {
	Healthy bool
	Err     error
}

// DoctorCommand runs a health check and surfaces any issues.
type DoctorCommand struct{}

var _ Command = DoctorCommand{}

func (DoctorCommand) Name() string        { return "doctor" }
func (DoctorCommand) Description() string { return "Run health check and surface issues" }

func (DoctorCommand) Execute(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		ok, err := client.Health()
		return DoctorResultMsg{Healthy: ok && err == nil, Err: err}
	}
}
