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

type pipelineStatusMsg struct {
	data *api.StatusResponse
	err  error
}

type PipelineTab struct {
	status   *api.StatusResponse
	err      error
}

func NewPipelineTab() *PipelineTab {
	return &PipelineTab{}
}

func (t *PipelineTab) Name() string { return "Pipeline" }

func (t *PipelineTab) Init() tea.Cmd { return nil }

func (t *PipelineTab) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.Status()
		return pipelineStatusMsg{data: data, err: err}
	}
}

func (t *PipelineTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case pipelineStatusMsg:
		t.err = msg.err
		t.status = msg.data
	}
	return t, nil
}

func (t *PipelineTab) View(width, height int) string {
	var sections []string
	s := t.status

	sections = append(sections, components.SectionTitle("Retrieval Cascade (live)", width))

	if t.err != nil {
		sections = append(sections, styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", t.err)))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// 6 retrieval stages with LIVE data
	cascadeNames := []struct{ key, label string }{
		{"policy", "policy"},
		{"exact_cache", "exact_cache"},
		{"semantic_cache", "sem_cache"},
		{"structured", "structured"},
		{"semantic", "semantic"},
		{"hybrid_merge", "hybrid"},
	}

	var stages []components.Stage
	for i, cs := range cascadeNames {
		status := "SKIP"
		latency := "—"
		if s != nil {
			if sm, ok := s.CascadeStages[cs.key]; ok {
				status = sm.Status
				if sm.Hits > 0 {
					latency = fmt.Sprintf("%.1fms (%d)", sm.AvgMs, sm.Hits)
				}
			}
		}
		stages = append(stages, components.Stage{Number: i + 1, Name: cs.label, Status: status, Latency: latency})
	}

	flow := components.StageFlow{Stages: stages, Width: width}
	sections = append(sections, flow.View())

	// Live stats
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Live Stats", width))
	sections = append(sections, "")

	if s != nil {
		statLine := lipgloss.NewStyle().Foreground(styles.TextPrimary)
		memMB := float64(s.MemoryResidentBytes) / (1024 * 1024)
		uptimeH := s.UptimeSeconds / 3600
		uptimeM := (s.UptimeSeconds % 3600) / 60

		sections = append(sections, statLine.Render(fmt.Sprintf(
			"  Memories: %d    Sources: %d    Queue: %d    WAL pending: %d",
			s.MemoriesTotal, s.SourcesTotal, s.QueueDepth, s.WAL.PendingEntries)))
		sections = append(sections, statLine.Render(fmt.Sprintf(
			"  Uptime: %dh%02dm    Goroutines: %d    RSS: %.1f MB    PID: %d",
			uptimeH, uptimeM, s.Goroutines, memMB, s.PID)))
		sections = append(sections, statLine.Render(fmt.Sprintf(
			"  Writes: %d total (%d/m)    Reads: %d total (%d/m)    Errors: %d/m",
			s.WritesTotal, s.Writes1m, s.ReadsTotal, s.Reads1m, s.Errors1m)))

		destStatus := "—"
		for _, d := range s.Destinations {
			health := lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("healthy")
			if !d.Healthy {
				health = lipgloss.NewStyle().Foreground(styles.ColorRed).Render("UNHEALTHY")
			}
			destStatus = fmt.Sprintf("%s (%s)", d.Name, health)
		}
		walHealth := lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("healthy")
		if !s.WAL.Healthy {
			walHealth = lipgloss.NewStyle().Foreground(styles.ColorRed).Render("UNHEALTHY")
		}
		sections = append(sections, statLine.Render(fmt.Sprintf(
			"  Destination: %s    WAL: %s    Integrity: %s",
			destStatus, walHealth, s.WAL.IntegrityMode)))
	} else {
		sections = append(sections, styles.MutedStyle.Render("  Waiting for status data..."))
	}

	// Throughput bars
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Throughput", width))
	sections = append(sections, "")

	barWidth := width - 4
	if barWidth < 20 {
		barWidth = 20
	}

	queuePct, cacheRate := 0.0, 0.0
	consistency := 0.0
	if s != nil {
		queuePct = float64(s.QueueDepth) / 100.0
		if queuePct > 1.0 {
			queuePct = 1.0
		}
		consistency = s.ConsistencyScore
		if consistency < 0 {
			consistency = 0
		}
		cacheRate = s.Cache.HitRate
	}

	sections = append(sections, components.InlineBar{Label: "Queue pressure", Value: queuePct, Width: barWidth, Color: styles.ColorAmber}.View())
	sections = append(sections, components.InlineBar{Label: "Consistency", Value: consistency, Width: barWidth, Color: styles.ColorGreen}.View())
	sections = append(sections, components.InlineBar{Label: "Cache hit rate", Value: cacheRate, Width: barWidth, Color: styles.ColorTeal}.View())

	content := strings.Join(sections, "\n")
	return lipgloss.NewStyle().Width(width).Height(height).Render(content)
}

var _ Tab = (*PipelineTab)(nil)
