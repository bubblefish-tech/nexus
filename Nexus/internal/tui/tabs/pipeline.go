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

	"github.com/BubbleFish-Nexus/internal/tui/api"
	"github.com/BubbleFish-Nexus/internal/tui/components"
	"github.com/BubbleFish-Nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pipelineStatusMsg carries the result of a status API call for the pipeline tab.
type pipelineStatusMsg struct {
	data *api.StatusResponse
	err  error
}

// PipelineTab shows the 6-stage retrieval cascade and throughput gauges.
type PipelineTab struct {
	status   *api.StatusResponse
	err      error
	blackBox bool
}

// NewPipelineTab returns an initialised PipelineTab.
func NewPipelineTab() *PipelineTab {
	return &PipelineTab{}
}

// Name returns the tab display name.
func (t *PipelineTab) Name() string { return "Pipeline" }

// Init returns the first command (none needed).
func (t *PipelineTab) Init() tea.Cmd { return nil }

// FireRefresh fetches fresh status data from the daemon.
func (t *PipelineTab) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.Status()
		return pipelineStatusMsg{data: data, err: err}
	}
}

// Update handles incoming messages.
func (t *PipelineTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case pipelineStatusMsg:
		t.err = msg.err
		t.status = msg.data
		return t, nil

	case tea.KeyMsg:
		if msg.String() == "b" {
			t.blackBox = !t.blackBox
		}
		return t, nil
	}
	return t, nil
}

// View renders the pipeline visualisation.
func (t *PipelineTab) View(width, height int) string {
	var sections []string

	// Title.
	sections = append(sections, components.SectionTitle("Retrieval Pipeline", width))

	// Error state.
	if t.err != nil {
		sections = append(sections, styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", t.err)))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Determine stage statuses from live data.
	queueOK := "OK"
	if t.status != nil && t.status.QueueDepth > 100 {
		queueOK = "MISS"
	}

	if t.blackBox {
		// Black-box mode: single summary panel.
		sections = append(sections, "")
		sections = append(sections, styles.MutedStyle.Render("[black-box mode]  6 stages collapsed"))
		sections = append(sections, "")

		status := "idle"
		version := "-"
		if t.status != nil {
			status = t.status.Status
			version = t.status.Version
		}

		card := components.StatCard{
			Label:    "Pipeline Summary",
			Value:    components.PillStatus(status),
			Subtitle: "version " + version,
			Color:    styles.ColorTeal,
			Width:    width / 2,
		}
		sections = append(sections, card.View())
		sections = append(sections, "")
		sections = append(sections, styles.MutedStyle.Render("Press 'b' to expand stages"))
	} else {
		// Full 6-stage cascade. Status derived from live daemon state.
		stageStatus := "SKIP"
		if t.status != nil {
			stageStatus = "OK"
		}
		stageList := []components.Stage{
			{Number: 1, Name: "policy", Status: stageStatus, Latency: "—"},
			{Number: 2, Name: "exact_cache", Status: stageStatus, Latency: "—"},
			{Number: 3, Name: "semantic_cache", Status: stageStatus, Latency: "—"},
			{Number: 4, Name: "temporal_decay", Status: stageStatus, Latency: "—"},
			{Number: 5, Name: "embedding", Status: queueOK, Latency: "—"},
			{Number: 6, Name: "projection", Status: stageStatus, Latency: "—"},
		}

		flow := components.StageFlow{
			Stages: stageList,
			Width:  width,
		}
		sections = append(sections, "")
		sections = append(sections, flow.View())
		sections = append(sections, "")
		sections = append(sections, styles.MutedStyle.Render("Press 'b' for black-box mode"))
	}

	// Throughput gauges.
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Throughput", width))
	sections = append(sections, "")

	barWidth := width - 4
	if barWidth < 20 {
		barWidth = 20
	}

	queuePct := 0.0
	consistency := 0.0
	if t.status != nil {
		// Queue depth normalised to 0-1 (100 is full).
		queuePct = float64(t.status.QueueDepth) / 100.0
		if queuePct > 1.0 {
			queuePct = 1.0
		}
		consistency = t.status.ConsistencyScore
	}

	sections = append(sections, components.InlineBar{
		Label: "Queue pressure",
		Value: queuePct,
		Width: barWidth,
		Color: styles.ColorAmber,
	}.View())

	sections = append(sections, components.InlineBar{
		Label: "Consistency",
		Value: consistency,
		Width: barWidth,
		Color: styles.ColorGreen,
	}.View())

	cacheRate := 0.0
	cacheLabel := "Cache hit rate"
	if t.status == nil {
		cacheLabel = "Cache hit rate (no data)"
	}
	sections = append(sections, components.InlineBar{
		Label: cacheLabel,
		Value: cacheRate,
		Width: barWidth,
		Color: styles.ColorTeal,
	}.View())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// Compile-time interface check.
var _ Tab = (*PipelineTab)(nil)
