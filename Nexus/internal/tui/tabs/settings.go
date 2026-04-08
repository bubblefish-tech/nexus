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

// settingsConfigMsg carries the result of a config API call for the settings tab.
type settingsConfigMsg struct {
	data *api.ConfigResponse
	err  error
}

// SettingsTab displays read-only daemon configuration.
type SettingsTab struct {
	status  *api.StatusResponse
	config  *api.ConfigResponse
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

// FireRefresh fetches fresh status and config data from the daemon.
func (t *SettingsTab) FireRefresh(client *api.Client) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			data, err := client.Status()
			return settingsStatusMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := client.Config()
			return settingsConfigMsg{data: data, err: err}
		},
	)
}

// Update handles incoming messages.
func (t *SettingsTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case settingsStatusMsg:
		if msg.err != nil {
			t.err = msg.err
		}
		t.status = msg.data
		return t, nil

	case settingsConfigMsg:
		if msg.err != nil {
			t.err = msg.err
		}
		t.config = msg.data
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

	// Values from /api/config (live) or fallback.
	cfg := t.config

	bind := "-"
	port := "-"
	logLevel := "-"
	mode := "-"
	queueSize := "-"
	if cfg != nil {
		bind = cfg.Daemon.Bind
		port = fmt.Sprintf("%d", cfg.Daemon.Port)
		logLevel = cfg.Daemon.LogLevel
		mode = cfg.Daemon.Mode
		queueSize = fmt.Sprintf("%d", cfg.Daemon.QueueSize)
	}

	sections = append(sections, renderSection("Daemon", [][2]string{
		{"Status", components.PillStatus(status)},
		{"Version", version},
		{"Listen Address", bind + ":" + port},
		{"Log Level", logLevel},
		{"Mode", mode},
		{"Queue Size", queueSize},
	}))

	// WAL section.
	walPath := "-"
	walSegSize := "-"
	walIntegrity := "-"
	walEncryption := "-"
	walWatchdog := "-"
	if cfg != nil {
		walPath = cfg.WAL.Path
		walSegSize = fmt.Sprintf("%dMB", cfg.WAL.MaxSegmentSizeMB)
		walIntegrity = cfg.WAL.IntegrityMode
		if walIntegrity == "" {
			walIntegrity = "crc32"
		}
		walEncryption = boolStr(cfg.WAL.EncryptionEnabled)
		walWatchdog = fmt.Sprintf("%ds", cfg.WAL.WatchdogIntervalS)
	}
	sections = append(sections, renderSection("WAL", [][2]string{
		{"WAL Path", walPath},
		{"Max Segment Size", walSegSize},
		{"Integrity Mode", walIntegrity},
		{"Encryption", walEncryption},
		{"Watchdog Interval", walWatchdog},
	}))

	// MCP section.
	mcpEnabled := "-"
	mcpPort := "-"
	mcpSource := "-"
	if cfg != nil {
		mcpEnabled = boolStr(cfg.MCP.Enabled)
		mcpPort = fmt.Sprintf("%d", cfg.MCP.Port)
		mcpSource = cfg.MCP.SourceName
	}
	sections = append(sections, renderSection("MCP", [][2]string{
		{"MCP Enabled", mcpEnabled},
		{"MCP Port", mcpPort},
		{"MCP Source", mcpSource},
	}))

	// Embedding section.
	embEnabled := "-"
	embModel := "-"
	embProvider := "-"
	if cfg != nil {
		embEnabled = boolStr(cfg.Embedding.Enabled)
		embModel = cfg.Embedding.Model
		embProvider = cfg.Embedding.Provider
	}
	sections = append(sections, renderSection("Embedding", [][2]string{
		{"Enabled", embEnabled},
		{"Provider", embProvider},
		{"Model", embModel},
	}))

	// Retrieval section.
	retDecay := "-"
	retProfile := "-"
	retHalfLife := "-"
	if cfg != nil {
		retDecay = boolStr(cfg.Retrieval.TimeDecay)
		retProfile = cfg.Retrieval.DefaultProfile
		retHalfLife = fmt.Sprintf("%.1f days", cfg.Retrieval.HalfLifeDays)
	}
	sections = append(sections, renderSection("Retrieval", [][2]string{
		{"Temporal Decay", retDecay},
		{"Default Profile", retProfile},
		{"Half-Life", retHalfLife},
	}))

	// TLS section.
	tlsEnabled := "-"
	tlsMin := "-"
	if cfg != nil {
		tlsEnabled = boolStr(cfg.TLS.Enabled)
		tlsMin = cfg.TLS.MinVersion
		if tlsMin == "" {
			tlsMin = "default"
		}
	}
	sections = append(sections, renderSection("TLS", [][2]string{
		{"TLS Enabled", tlsEnabled},
		{"Min Version", tlsMin},
	}))

	// Sources & Destinations.
	srcList := "-"
	dstList := "-"
	if cfg != nil {
		if len(cfg.Sources) > 0 {
			srcList = strings.Join(cfg.Sources, ", ")
		}
		if len(cfg.Destinations) > 0 {
			dstList = strings.Join(cfg.Destinations, ", ")
		}
	}
	sections = append(sections, renderSection("Data Paths", [][2]string{
		{"Sources", srcList},
		{"Destinations", dstList},
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

// boolStr returns "enabled" or "disabled" for a bool value.
func boolStr(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// Compile-time interface check.
var _ Tab = (*SettingsTab)(nil)
