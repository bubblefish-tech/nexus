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

type controlStatusMsg struct {
	data *api.StatusResponse
	err  error
}

type controlHealthMsg struct {
	ok  bool
	err error
}

type ControlTab struct {
	status       *api.StatusResponse
	healthy      bool
	healthErr    error
	statusErr    error
	writeHistory []int
	readHistory  []int
}

func NewControlTab() *ControlTab {
	return &ControlTab{
		writeHistory: make([]int, 30),
		readHistory:  make([]int, 30),
	}
}

func (t *ControlTab) Name() string { return "Overview" }

func (t *ControlTab) Init() tea.Cmd { return nil }

func (t *ControlTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch m := msg.(type) {
	case controlStatusMsg:
		t.statusErr = m.err
		if m.err == nil && m.data != nil {
			t.status = m.data
			t.writeHistory = append(t.writeHistory[1:], m.data.Writes1m)
			t.readHistory = append(t.readHistory[1:], m.data.Reads1m)
		}
	case controlHealthMsg:
		t.healthErr = m.err
		t.healthy = m.ok
	}
	return t, nil
}

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

func (t *ControlTab) View(width, height int) string {
	var sections []string
	s := t.status

	// ── Row 1: 5 stat cards ──
	cardWidth := (width - 8) / 5
	if cardWidth < 14 {
		cardWidth = 14
	}

	daemonVal := "DOWN"
	daemonClr := styles.ColorRed
	if t.healthy {
		daemonVal = "● LIVE"
		daemonClr = styles.ColorGreen
	}

	memVal, wpsVal, rpsVal, queueVal, uptimeVal := "—", "—", "—", "—", "—"
	verSub := "—"
	if s != nil {
		memVal = fmt.Sprintf("%d", s.MemoriesTotal)
		wpsVal = fmt.Sprintf("%d/m", s.Writes1m)
		rpsVal = fmt.Sprintf("%d/m", s.Reads1m)
		queueVal = fmt.Sprintf("%d", s.QueueDepth)
		h := s.UptimeSeconds / 3600
		m := (s.UptimeSeconds % 3600) / 60
		uptimeVal = fmt.Sprintf("%dh%02dm", h, m)
		verSub = s.Version
	}

	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		components.StatCard{Label: "Status", Value: daemonVal, Subtitle: verSub, Color: daemonClr, Width: cardWidth}.View(),
		" ",
		components.StatCard{Label: "Memories", Value: memVal, Subtitle: "total stored", Color: styles.ColorTeal, Width: cardWidth}.View(),
		" ",
		components.StatCard{Label: "Writes", Value: wpsVal, Subtitle: "per minute", Color: styles.ColorGreen, Width: cardWidth}.View(),
		" ",
		components.StatCard{Label: "Reads", Value: rpsVal, Subtitle: "per minute", Color: styles.ColorBlue, Width: cardWidth}.View(),
		" ",
		components.StatCard{Label: "Queue", Value: queueVal, Subtitle: uptimeVal, Color: styles.ColorPurple, Width: cardWidth}.View(),
	)
	sections = append(sections, cards)

	// ── Row 2: Write throughput sparkline + System stats ──
	sections = append(sections, "")
	half := (width - 2) / 2
	if half < 30 {
		half = 30
	}

	writeSparkTitle := styles.TealStyle.Render("WRITE THROUGHPUT (30 samples)")
	writeSpark := renderMiniSparkline(t.writeHistory, half-4, styles.ColorGreen)
	readSparkTitle := styles.TealStyle.Render("READ THROUGHPUT (30 samples)")
	readSpark := renderMiniSparkline(t.readHistory, half-4, styles.ColorBlue)

	leftPanel := lipgloss.JoinVertical(lipgloss.Left, writeSparkTitle, writeSpark, "", readSparkTitle, readSpark)

	// Right panel: system info
	var sysLines []string
	sysLines = append(sysLines, styles.TealStyle.Render("SYSTEM"))
	if s != nil {
		sysLines = append(sysLines, fmt.Sprintf("  PID: %d  Goroutines: %d  RSS: %.1f MB",
			s.PID, s.Goroutines, float64(s.MemoryResidentBytes)/(1024*1024)))
		sysLines = append(sysLines, fmt.Sprintf("  Immune scans: %d  Quarantined: %d",
			s.ImmuneScans, s.QuarantineTotal))
		sysLines = append(sysLines, fmt.Sprintf("  Audit: %s  Errors/m: %d",
			boolLabel(s.AuditEnabled), s.Errors1m))
		sysLines = append(sysLines, "")
		sysLines = append(sysLines, styles.TealStyle.Render("SOURCES"))
		for _, sh := range s.SourceHealth {
			dot := lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("●")
			sysLines = append(sysLines, fmt.Sprintf("  %s %s  %s", dot, sh.Name, sh.Status))
		}
		sysLines = append(sysLines, "")
		sysLines = append(sysLines, styles.TealStyle.Render("DESTINATIONS"))
		for _, d := range s.Destinations {
			dot := lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("●")
			if !d.Healthy {
				dot = lipgloss.NewStyle().Foreground(styles.ColorRed).Render("●")
			}
			health := "healthy"
			if !d.Healthy {
				health = "UNHEALTHY"
			}
			sysLines = append(sysLines, fmt.Sprintf("  %s %s  %s", dot, d.Name, health))
		}
		sysLines = append(sysLines, fmt.Sprintf("  WAL: %s  %s  pending: %d",
			boolLabel(s.WAL.Healthy), s.WAL.IntegrityMode, s.WAL.PendingEntries))
	} else {
		sysLines = append(sysLines, styles.MutedStyle.Render("  Waiting for data..."))
	}
	rightPanel := lipgloss.JoinVertical(lipgloss.Left, sysLines...)

	row2 := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(half).Render(leftPanel),
		lipgloss.NewStyle().Width(half).Render(rightPanel),
	)
	sections = append(sections, row2)

	// ── Row 3: Write path stages with LIVE latency ──
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Write Path (live)", width))

	writeStageNames := []struct{ key, label string }{
		{"auth", "auth"},
		{"policy", "policy"},
		{"idempotency", "idemp"},
		{"rate_limit", "rate_lim"},
		{"immune_scan", "immune"},
		{"embedding", "embed"},
		{"wal_append", "WAL"},
		{"queue_send", "queue"},
		{"dest_write", "dest"},
	}

	var stages []components.Stage
	for i, ws := range writeStageNames {
		status := "SKIP"
		latency := "—"
		if s != nil {
			if sm, ok := s.WriteStages[ws.key]; ok {
				status = sm.Status
				if sm.Hits > 0 {
					latency = fmt.Sprintf("%.1fms", sm.AvgMs)
				}
			}
		}
		stages = append(stages, components.Stage{Number: i + 1, Name: ws.label, Status: status, Latency: latency})
	}
	flow := components.StageFlow{Stages: stages, Width: width}
	sections = append(sections, flow.View())

	// ── Row 4: Cache + throughput bars ──
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Cache & Throughput", width))
	sections = append(sections, "")

	barWidth := width - 4
	if barWidth < 20 {
		barWidth = 20
	}

	cacheRate, exactRate, semRate := 0.0, 0.0, 0.0
	queuePct := 0.0
	if s != nil {
		cacheRate = s.Cache.HitRate
		exactRate = s.Cache.ExactRate
		semRate = s.Cache.SemanticRate
		queuePct = float64(s.QueueDepth) / 100.0
		if queuePct > 1.0 {
			queuePct = 1.0
		}
	}

	sections = append(sections, components.InlineBar{Label: "Cache total", Value: cacheRate, Width: barWidth, Color: styles.ColorTeal}.View())
	sections = append(sections, components.InlineBar{Label: "  Exact", Value: exactRate, Width: barWidth, Color: styles.ColorGreen}.View())
	sections = append(sections, components.InlineBar{Label: "  Semantic", Value: semRate, Width: barWidth, Color: styles.ColorBlue}.View())
	sections = append(sections, components.InlineBar{Label: "Queue pressure", Value: queuePct, Width: barWidth, Color: styles.ColorAmber}.View())

	// ── Error banner ──
	if t.statusErr != nil {
		sections = append(sections, "")
		sections = append(sections, styles.ErrorStyle.Render("status error: "+t.statusErr.Error()))
	}

	content := strings.Join(sections, "\n")
	return lipgloss.NewStyle().Width(width).Height(height).Render(content)
}

func boolLabel(b bool) string {
	if b {
		return lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("enabled")
	}
	return lipgloss.NewStyle().Foreground(styles.ColorAmber).Render("disabled")
}

func renderMiniSparkline(data []int, width int, color lipgloss.Color) string {
	if width < 5 {
		width = 5
	}
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	maxVal := 1
	for _, v := range data {
		if v > maxVal {
			maxVal = v
		}
	}
	var sb strings.Builder
	start := 0
	if len(data) > width {
		start = len(data) - width
	}
	for i := start; i < len(data); i++ {
		idx := data[i] * (len(blocks) - 1) / maxVal
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		sb.WriteRune(blocks[idx])
	}
	return lipgloss.NewStyle().Foreground(color).Render(sb.String())
}

var _ Tab = (*ControlTab)(nil)
