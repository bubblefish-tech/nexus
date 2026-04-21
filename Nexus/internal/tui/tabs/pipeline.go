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

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
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

	// Live stats.
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Live Stats", width))
	sections = append(sections, "")

	if t.status != nil {
		statLine := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		memMB := float64(t.status.MemoryResidentBytes) / (1024 * 1024)
		uptimeH := t.status.UptimeSeconds / 3600
		uptimeM := (t.status.UptimeSeconds % 3600) / 60

		sections = append(sections, statLine.Render(fmt.Sprintf(
			"  Memories: %d    Sources: %d    Queue: %d    WAL pending: %d",
			t.status.MemoriesTotal, t.status.SourcesTotal,
			t.status.QueueDepth, t.status.WAL.PendingEntries)))
		sections = append(sections, statLine.Render(fmt.Sprintf(
			"  Uptime: %dh%02dm    Goroutines: %d    RSS: %.1f MB    PID: %d",
			uptimeH, uptimeM, t.status.Goroutines, memMB, t.status.PID)))

		destStatus := "—"
		for _, d := range t.status.Destinations {
			health := "healthy"
			if !d.Healthy {
				health = "UNHEALTHY"
			}
			destStatus = fmt.Sprintf("%s (%s)", d.Name, health)
		}
		sections = append(sections, statLine.Render(fmt.Sprintf(
			"  Destination: %s    WAL: %s    Integrity: %s",
			destStatus,
			walHealthLabel(t.status.WAL.Healthy),
			t.status.WAL.IntegrityMode)))
	} else {
		sections = append(sections, styles.MutedStyle.Render("  Waiting for status data..."))
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
	cacheRate := 0.0
	if t.status != nil {
		queuePct = float64(t.status.QueueDepth) / 100.0
		if queuePct > 1.0 {
			queuePct = 1.0
		}
		consistency = t.status.ConsistencyScore
		if consistency < 0 {
			consistency = 0
		}
		cacheRate = t.status.Cache.HitRate
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

	sections = append(sections, components.InlineBar{
		Label: "Cache hit rate",
		Value: cacheRate,
		Width: barWidth,
		Color: styles.ColorTeal,
	}.View())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func walHealthLabel(healthy bool) string {
	if healthy {
		return "healthy"
	}
	return "UNHEALTHY"
}

// Compile-time interface check.
var _ Tab = (*PipelineTab)(nil)
