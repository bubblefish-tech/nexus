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

// Package tabs implements Bubble Tea sub-model tabs for the BubbleFish Nexus TUI.
package tabs

import (
	"github.com/BubbleFish-Nexus/internal/tui/api"
	tea "github.com/charmbracelet/bubbletea"
)

// Tab is the interface each tab sub-model must implement.
// This mirrors the tui.Tab interface to avoid an import cycle.
type Tab interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (Tab, tea.Cmd)
	View(width, height int) string
	FireRefresh(client *api.Client) tea.Cmd
	Name() string
}
