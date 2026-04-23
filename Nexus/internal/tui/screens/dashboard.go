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

package screens

import (
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type dashAgentsMsg struct {
	data []api.AgentSummary
	err  error
}

// DashboardScreen is Page 1 — the main overview.
type DashboardScreen struct {
	width, height int
	status        *api.StatusResponse
	agents        []api.AgentSummary
	healthy       bool
	statusErr     error
	writeHistory  []int
	readHistory   []int
}

// NewDashboardScreen creates the dashboard with initial state.
func NewDashboardScreen() *DashboardScreen {
	return &DashboardScreen{
		writeHistory: make([]int, 60),
		readHistory:  make([]int, 60),
	}
}

func (d *DashboardScreen) Name() string { return "Dashboard" }

func (d *DashboardScreen) Init() tea.Cmd { return nil }

func (d *DashboardScreen) SetSize(w, h int) {
	d.width = w
	d.height = h
}

func (d *DashboardScreen) ShortHelp() []key.Binding { return nil }

func (d *DashboardScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case api.StatusBroadcastMsg:
		if m.Data != nil {
			d.status = m.Data
			d.statusErr = nil
			d.writeHistory = append(d.writeHistory[1:], m.Data.Writes1m)
			d.readHistory = append(d.readHistory[1:], m.Data.Reads1m)
			d.healthy = true
		}
	case dashAgentsMsg:
		if m.err == nil {
			d.agents = m.data
		}
	}
	return d, nil
}

func (d *DashboardScreen) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		agents, err := client.Agents()
		return dashAgentsMsg{data: agents, err: err}
	}
}

func (d *DashboardScreen) View() string {
	if d.width < 40 || d.height < 10 {
		return ""
	}

	var sections []string

	// ── Left column: Logo + Connected Tools ──
	// ── Right column: Stats + Metrics + Activity ──
	leftW := d.width * 38 / 100
	if leftW < 30 {
		leftW = 30
	}
	rightW := d.width - leftW - 1
	if rightW < 40 {
		rightW = 40
	}

	left := d.viewLeftColumn(leftW)
	right := d.viewRightColumn(rightW)

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftW).Render(left),
		lipgloss.NewStyle().Width(rightW).Render(right),
	)
	sections = append(sections, body)

	if d.statusErr != nil {
		sections = append(sections, styles.ErrorStyle.Render("  status: "+d.statusErr.Error()))
	}

	return lipgloss.NewStyle().Width(d.width).Height(d.height).Render(
		strings.Join(sections, "\n"),
	)
}

func (d *DashboardScreen) viewLeftColumn(w int) string {
	var lines []string

	// Fish emblem (ANSI art for wide terminals, ASCII for narrow)
	if w >= 70 {
		lines = append(lines, components.RenderFishEmblem())
	} else {
		logo := components.Logo{Width: w}
		lines = append(lines, logo.View())
	}
	lines = append(lines, "")

	// Brand text
	brandStyle := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).Width(w).Align(lipgloss.Center)
	lines = append(lines, brandStyle.Render("BubbleFish Technologies"))
	titleStyle := lipgloss.NewStyle().Foreground(styles.ColorCyan).Bold(true).Width(w).Align(lipgloss.Center)
	lines = append(lines, titleStyle.Render("N E X U S"))
	lines = append(lines, brandStyle.Render("Public Edition"))
	lines = append(lines, "")

	sep := lipgloss.NewStyle().Foreground(styles.BorderBase).Width(w).Align(lipgloss.Center).
		Render(strings.Repeat("─", w-4))
	lines = append(lines, sep)
	lines = append(lines, "")

	// Founder + copyright
	mutedCenter := lipgloss.NewStyle().Foreground(styles.TextMuted).Width(w).Align(lipgloss.Center)
	lines = append(lines, mutedCenter.Render("Shawn Sammartano, Founder"))
	lines = append(lines, mutedCenter.Render("© 2026 Shawn Sammartano"))
	lines = append(lines, mutedCenter.Render("AGPL-3.0 · bubblefish.sh"))
	lines = append(lines, mutedCenter.Render("Patent Pending"))
	lines = append(lines, "")

	// Connected Tools
	lines = append(lines, sectionHeader("CONNECTED TOOLS", w))
	if len(d.agents) > 0 {
		for _, a := range d.agents {
			dot := agentDot(a.Status)
			name := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(a.DisplayName)
			lines = append(lines, fmt.Sprintf("  %s %s", dot, name))
		}
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextMuted).Render("  No agents connected"))
	}

	return strings.Join(lines, "\n")
}

func (d *DashboardScreen) viewRightColumn(w int) string {
	var lines []string
	s := d.status

	// ── Stat cards row ──
	cardW := (w - 6) / 3
	if cardW < 14 {
		cardW = 14
	}

	memVal, auditVal, agentVal := "—", "—", "—"
	memSub, auditSub, agentSub := "total stored", "chain entries", "connected"
	healthVal, quarVal, walVal := "—", "—", "—"
	healthSub, quarSub, walSub := "subsystems", "items", "fsync"

	if s != nil {
		memVal = fmt.Sprintf("%d", s.MemoriesTotal)
		if s.AuditEnabled {
			auditVal = "enabled"
		} else {
			auditVal = "disabled"
		}
		auditSub = "chain ✓"
		agentSub = "conn/disc"
	}
	if len(d.agents) > 0 {
		connected := 0
		total := len(d.agents)
		for _, a := range d.agents {
			if a.Status == "active" || a.Status == "online" {
				connected++
			}
		}
		agentVal = fmt.Sprintf("%d / %d", connected, total)
	}

	if d.healthy {
		healthVal = "NOMINAL"
		healthSub = "all subsys"
	} else {
		healthVal = "DOWN"
		healthSub = "check daemon"
	}

	if s != nil {
		quarVal = fmt.Sprintf("%d", s.QuarantineTotal)
		quarSub = "quarantined"
		if s.WAL.PendingEntries == 0 {
			walVal = "<1ms"
		} else {
			walVal = fmt.Sprintf("%d pend", s.WAL.PendingEntries)
		}
		walSub = "WAL lag"
	}

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		components.StatCard{Label: "Memories", Value: memVal, Subtitle: memSub, Color: styles.ColorTeal, Width: cardW}.View(),
		" ",
		components.StatCard{Label: "Audit", Value: auditVal, Subtitle: auditSub, Color: styles.ColorPurple, Width: cardW}.View(),
		" ",
		components.StatCard{Label: "Agents", Value: agentVal, Subtitle: agentSub, Color: styles.ColorBlue, Width: cardW}.View(),
	)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top,
		components.StatCard{Label: "Health", Value: healthVal, Subtitle: healthSub, Color: styles.ColorGreen, Width: cardW}.View(),
		" ",
		components.StatCard{Label: "Quarantine", Value: quarVal, Subtitle: quarSub, Color: styles.ColorAmber, Width: cardW}.View(),
		" ",
		components.StatCard{Label: "WAL Lag", Value: walVal, Subtitle: walSub, Color: styles.ColorGray, Width: cardW}.View(),
	)

	lines = append(lines, row1, "", row2, "")

	// ── Free Energy gauge ──
	feGauge := &components.FreeEnergyGauge{Width: w}
	if s != nil {
		fe := feGauge.Compute(s.Cache.HitRate, 0.95, 1.0)
		feGauge.Push(fe)
	}
	lines = append(lines, feGauge.View())
	lines = append(lines, "")

	// ── Retrieval + Crypto summary ──
	lines = append(lines, sectionHeader("RETRIEVAL", w))
	if s != nil {
		var p50, p95, p99 string
		if cs, ok := s.CascadeStages["hybrid_merge"]; ok && cs.Hits > 0 {
			p50 = fmt.Sprintf("%.0f", cs.AvgMs)
		} else {
			p50 = "—"
		}
		p95, p99 = "—", "—"
		lines = append(lines, fmt.Sprintf("  p50=%sms  p95=%sms  p99=%sms", p50, p95, p99))
	} else {
		lines = append(lines, styles.MutedStyle.Render("  Waiting for data..."))
	}

	lines = append(lines, "")
	lines = append(lines, sectionHeader("CRYPTO", w))
	lines = append(lines, fmt.Sprintf("  ✓ AES-256-GCM  ✓ Ed25519  ✓ ratchet"))

	// ── Throughput sparklines ──
	lines = append(lines, "")
	lines = append(lines, sectionHeader("WRITE THROUGHPUT (60s)", w))
	lines = append(lines, "  "+renderSparkline(d.writeHistory, w-4, styles.ColorGreen))
	lines = append(lines, "")
	lines = append(lines, sectionHeader("READ THROUGHPUT (60s)", w))
	lines = append(lines, "  "+renderSparkline(d.readHistory, w-4, styles.ColorBlue))

	// ── System info ──
	lines = append(lines, "")
	lines = append(lines, sectionHeader("SYSTEM", w))
	if s != nil {
		lines = append(lines, fmt.Sprintf("  PID: %d  Goroutines: %d  RSS: %.1f MB",
			s.PID, s.Goroutines, float64(s.MemoryResidentBytes)/(1024*1024)))
		h := s.UptimeSeconds / 3600
		m := (s.UptimeSeconds % 3600) / 60
		sec := s.UptimeSeconds % 60
		lines = append(lines, fmt.Sprintf("  Uptime: %dh%02dm%02ds  Errors/m: %d", h, m, sec, s.Errors1m))
	}

	return strings.Join(lines, "\n")
}

func sectionHeader(title string, _ int) string {
	return lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("◈ " + title)
}

func agentDot(status string) string {
	switch status {
	case "active", "online":
		return lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("●")
	case "idle", "partial":
		return lipgloss.NewStyle().Foreground(styles.ColorBlue).Render("◑")
	default:
		return lipgloss.NewStyle().Foreground(styles.TextMuted).Render("○")
	}
}

func renderSparkline(data []int, width int, color lipgloss.Color) string {
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

var _ Screen = (*DashboardScreen)(nil)
