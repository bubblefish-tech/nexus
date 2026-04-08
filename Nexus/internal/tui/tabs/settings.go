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

package tabs

import (
	"fmt"
	"strings"

	"github.com/BubbleFish-Nexus/internal/tui/api"
	"github.com/BubbleFish-Nexus/internal/tui/components"
	"github.com/BubbleFish-Nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// settingsStatusMsg carries the result of a status API call for the settings tab.
type settingsStatusMsg struct {
	data *api.StatusResponse
	err  error
}

// SettingsTab displays read-only daemon configuration.
type SettingsTab struct {
	status  *api.StatusResponse
	err     error
	editMsg string
}

// NewSettingsTab returns an initialised SettingsTab.
func NewSettingsTab() *SettingsTab {
	return &SettingsTab{}
}

// Name returns the tab display name.
func (t *SettingsTab) Name() string { return "Settings" }

// Init returns the first command (none needed).
func (t *SettingsTab) Init() tea.Cmd { return nil }

// FireRefresh fetches fresh status data from the daemon.
func (t *SettingsTab) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.Status()
		return settingsStatusMsg{data: data, err: err}
	}
}

// Update handles incoming messages.
func (t *SettingsTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case settingsStatusMsg:
		t.err = msg.err
		t.status = msg.data
		return t, nil

	case tea.KeyMsg:
		if msg.String() == "e" {
			t.editMsg = "To edit settings, modify ~/.bubblefish/nexus.toml and restart the daemon."
		}
		return t, nil
	}
	return t, nil
}

// View renders the settings display.
func (t *SettingsTab) View(width, height int) string {
	var sections []string

	sections = append(sections, components.SectionTitle("Settings", width))

	if t.err != nil {
		sections = append(sections, styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", t.err)))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	version := "-"
	status := "-"
	if t.status != nil {
		version = t.status.Version
		status = t.status.Status
	}

	colWidth := width - 6
	if colWidth < 40 {
		colWidth = 40
	}

	// Section renderer helper.
	renderSection := func(title string, pairs [][2]string) string {
		var lines []string
		lines = append(lines, "")
		lines = append(lines, styles.SectionHeader.Render(strings.ToUpper(title)))
		lines = append(lines, lipgloss.NewStyle().Foreground(styles.BorderBase).Render(
			strings.Repeat("─", colWidth)))
		for _, kv := range pairs {
			key := lipgloss.NewStyle().
				Foreground(styles.TextSecondary).
				Width(24).
				Render(kv[0])
			val := lipgloss.NewStyle().
				Foreground(styles.TextPrimary).
				Render(kv[1])
			lines = append(lines, "  "+key+val)
		}
		return strings.Join(lines, "\n")
	}

	// Daemon section.
	sections = append(sections, renderSection("Daemon", [][2]string{
		{"Status", components.PillStatus(status)},
		{"Version", version},
		{"Listen Address", "127.0.0.1:3407"},
		{"PID File", "~/.bubblefish/nexus.pid"},
	}))

	// WAL section.
	sections = append(sections, renderSection("WAL", [][2]string{
		{"WAL Path", "~/.bubblefish/wal/"},
		{"Sync Mode", "full"},
		{"Max Segment Size", "64MB"},
		{"Retention", "7d"},
	}))

	// MCP section.
	sections = append(sections, renderSection("MCP", [][2]string{
		{"MCP Enabled", "true"},
		{"MCP Transport", "stdio"},
		{"MCP Auth", "token"},
	}))

	// Retrieval section.
	sections = append(sections, renderSection("Retrieval", [][2]string{
		{"Pipeline Stages", "6"},
		{"Cache TTL", "300s"},
		{"Semantic Threshold", "0.75"},
		{"Temporal Decay", "enabled"},
		{"Embedding Model", "all-MiniLM-L6-v2"},
	}))

	// Edit message.
	sections = append(sections, "")
	if t.editMsg != "" {
		sections = append(sections, styles.WarnStyle.Render(t.editMsg))
		sections = append(sections, "")
	}

	// Footer.
	footer := "Settings are read-only in TUI. Press 'e' for edit instructions."
	sections = append(sections, styles.MutedStyle.Render(footer))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// Compile-time interface check.
var _ Tab = (*SettingsTab)(nil)
