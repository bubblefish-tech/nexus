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

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// controlStatusMsg carries the result of a status API call.
type controlStatusMsg struct {
	data *api.StatusResponse
	err  error
}

// controlHealthMsg carries the result of a health API call.
type controlHealthMsg struct {
	ok  bool
	err error
}

// ControlTab is the landing tab showing daemon health signals.
type ControlTab struct {
	status    *api.StatusResponse
	healthy   bool
	healthErr error
	statusErr error
	heatData  []int
}

// NewControlTab creates a new Control tab.
func NewControlTab() *ControlTab {
	// Seed 24 hourly cells with zeros.
	heat := make([]int, 24)
	return &ControlTab{
		heatData: heat,
	}
}

// Name returns the tab display name.
func (t *ControlTab) Name() string { return "Control" }

// Init returns the initial command (none — data arrives via FireRefresh).
func (t *ControlTab) Init() tea.Cmd { return nil }

// Update handles incoming messages.
func (t *ControlTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch m := msg.(type) {
	case controlStatusMsg:
		t.statusErr = m.err
		if m.data != nil {
			t.status = m.data
		}
	case controlHealthMsg:
		t.healthErr = m.err
		t.healthy = m.ok
	}
	return t, nil
}

// FireRefresh dispatches parallel status and health API calls.
func (t *ControlTab) FireRefresh(client *api.Client) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			data, err := client.Status()
			return controlStatusMsg{data: data, err: err}
		},
		func() tea.Msg {
			ok, err := client.Health()
			return controlHealthMsg{ok: ok, err: err}
		},
	)
}

// View renders the control tab content.
func (t *ControlTab) View(width, height int) string {
	var sections []string

	// --- Row of 4 stat cards ---
	cardWidth := (width - 6) / 4
	if cardWidth < 16 {
		cardWidth = 16
	}

	daemonValue := "DOWN"
	daemonColor := styles.ColorRed
	if t.healthy {
		daemonValue = "LIVE"
		daemonColor = styles.ColorGreen
	}
	daemonSubtitle := ""
	if t.healthErr != nil {
		daemonSubtitle = t.healthErr.Error()
	}

	queueValue := "—"
	consistencyValue := "—"
	versionValue := "—"
	if t.status != nil {
		queueValue = fmt.Sprintf("%d", t.status.QueueDepth)
		consistencyValue = fmt.Sprintf("%.2f", t.status.ConsistencyScore)
		versionValue = t.status.Version
	}

	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		components.StatCard{
			Label:    "Daemon Status",
			Value:    daemonValue,
			Subtitle: daemonSubtitle,
			Color:    daemonColor,
			Width:    cardWidth,
		}.View(),
		" ",
		components.StatCard{
			Label:    "Queue Depth",
			Value:    queueValue,
			Subtitle: "pending writes",
			Color:    styles.ColorTeal,
			Width:    cardWidth,
		}.View(),
		" ",
		components.StatCard{
			Label:    "Consistency",
			Value:    consistencyValue,
			Subtitle: "score",
			Color:    styles.ColorBlue,
			Width:    cardWidth,
		}.View(),
		" ",
		components.StatCard{
			Label:    "Version",
			Value:    versionValue,
			Subtitle: "bubblefish nexus",
			Color:    styles.ColorPurple,
			Width:    cardWidth,
		}.View(),
	)
	sections = append(sections, cards)

	// --- Write path stepper ---
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Write Path", width))

	writeStages := []components.Stage{
		{Number: 1, Name: "auth", Status: "OK", Latency: ""},
		{Number: 2, Name: "canWrite", Status: "OK", Latency: ""},
		{Number: 3, Name: "idempotency", Status: "OK", Latency: ""},
		{Number: 4, Name: "rate_limit", Status: "OK", Latency: ""},
		{Number: 5, Name: "WAL_append", Status: "OK", Latency: ""},
		{Number: 6, Name: "queue_send", Status: "OK", Latency: ""},
		{Number: 7, Name: "dest_write", Status: "OK", Latency: ""},
		{Number: 8, Name: "DELIVERED", Status: "OK", Latency: ""},
		{Number: 9, Name: "event_sink", Status: "OK", Latency: ""},
	}

	// Mark all stages as SKIP when daemon is not reporting.
	if t.status == nil {
		for i := range writeStages {
			writeStages[i].Status = "SKIP"
		}
	}

	flow := components.StageFlow{
		Stages: writeStages,
		Width:  width,
	}
	sections = append(sections, flow.View())

	// --- Write activity heat grid ---
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Write Activity (24h)", width))

	grid := components.HeatGrid{
		Values: t.heatData,
		Width:  width,
	}
	sections = append(sections, grid.View())

	// --- Error banner ---
	if t.statusErr != nil {
		sections = append(sections, "")
		errLine := styles.ErrorStyle.Render("status error: "+t.statusErr.Error())
		sections = append(sections, errLine)
	}

	content := strings.Join(sections, "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Render(content)
}

// Compile-time interface check.
var _ Tab = (*ControlTab)(nil)
