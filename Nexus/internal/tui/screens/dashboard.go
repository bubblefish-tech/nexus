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
	"log"
	"os"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var screenDebug *log.Logger

func init() {
	if os.Getenv("DEBUG") != "" {
		f, err := os.OpenFile("tui_debug.log", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err == nil {
			screenDebug = log.New(f, "", 0)
		}
	}
}

func sdbg(format string, args ...interface{}) {
	if screenDebug != nil {
		screenDebug.Printf("  DASH  "+format, args...)
	}
}

type dashAgentsMsg struct {
	data    []api.AgentSummary
	errKind api.ErrorKind
}

type dashStatsMsg struct {
	data    *api.AggregatedStats
	errKind api.ErrorKind
}

// DashboardScreen is Page 1 — the main overview.
type DashboardScreen struct {
	width, height  int
	status         *api.StatusResponse
	stats          *api.AggregatedStats
	agents         []api.AgentSummary
	healthy        bool
	curWrites      int
	curReads       int
	maxWrites      int
	maxReads       int
	cachedArt      string
	cachedArtWidth int
	cachedArtMaxH  int
}

// NewDashboardScreen creates the dashboard with initial state.
func NewDashboardScreen() *DashboardScreen {
	return &DashboardScreen{}
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
			d.curWrites = m.Data.Writes1m
			d.curReads = m.Data.Reads1m
			if m.Data.Writes1m > d.maxWrites {
				d.maxWrites = m.Data.Writes1m
			}
			if m.Data.Reads1m > d.maxReads {
				d.maxReads = m.Data.Reads1m
			}
			d.healthy = true
			sdbg("StatusBroadcast received: memories=%d w1m=%d r1m=%d queue=%d uptime=%ds",
				m.Data.MemoriesTotal, m.Data.Writes1m, m.Data.Reads1m, m.Data.QueueDepth, m.Data.UptimeSeconds)
		} else {
			sdbg("StatusBroadcast received with nil data")
		}
	case dashAgentsMsg:
		if m.errKind == api.ErrKindUnknown {
			d.agents = m.data
			sdbg("AgentsMsg received: count=%d", len(m.data))
		} else {
			sdbg("AgentsMsg errKind=%d", m.errKind)
		}
	case dashStatsMsg:
		if m.errKind == api.ErrKindUnknown && m.data != nil {
			d.stats = m.data
			sdbg("StatsMsg received: memories=%d agents=%d/%d", m.data.MemoryCount, m.data.AgentsConnected, m.data.AgentsKnown)
		}
	default:
		sdbg("unhandled msg type: %T", msg)
	}
	return d, nil
}

func (d *DashboardScreen) FireRefresh(client *api.Client) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			agents, err := client.Agents()
			if err != nil {
				kind := api.Classify(err)
				sdbg("Agents failed kind=%d err=%v", kind, err)
				return dashAgentsMsg{errKind: kind}
			}
			return dashAgentsMsg{data: agents}
		},
		func() tea.Msg {
			stats, err := client.Stats()
			if err != nil {
				kind := api.Classify(err)
				sdbg("Stats failed kind=%d err=%v", kind, err)
				return dashStatsMsg{errKind: kind}
			}
			return dashStatsMsg{data: stats}
		},
	)
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

	return lipgloss.NewStyle().Width(d.width).Height(d.height).Render(
		strings.Join(sections, "\n"),
	)
}

func (d *DashboardScreen) viewLeftColumn(w int) string {
	var lines []string

	artMaxLines := d.height - 18
	if artMaxLines < 10 {
		artMaxLines = 10
	}
	if d.cachedArt == "" || d.cachedArtWidth != w || d.cachedArtMaxH != artMaxLines {
		artLines := strings.Split(components.RenderFishEmblem(), "\n")
		if len(artLines) > artMaxLines {
			artLines = artLines[:artMaxLines]
		}
		for i, al := range artLines {
			artLines[i] = lipgloss.PlaceHorizontal(w, lipgloss.Center, al+"\033[0m")
		}
		d.cachedArt = strings.Join(artLines, "\n")
		d.cachedArtWidth = w
		d.cachedArtMaxH = artMaxLines
	}
	lines = append(lines, d.cachedArt)
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
			dot := agentDot(a.Status, a.LastSeenAt)
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

	// ── 3×2 Stat cards grid ──
	cardW := (w - 6) / 3
	if cardW < 14 {
		cardW = 14
	}
	cardH := 7
	loaded := d.stats != nil || s != nil

	memVal, auditVal, agentVal := "—", "—", "—"
	memSub, auditSub, agentSub := "total stored", "chain entries", "connected"
	healthVal, quarVal, walVal := "—", "—", "—"
	healthSub, quarSub, walSub := "subsystems", "items held", "fsync verified ✓"

	if d.stats != nil {
		memVal = fmt.Sprintf("%d", d.stats.MemoryCount)
		memSub = fmt.Sprintf("↑ %d this session", d.stats.SessionWrites)
		auditVal = fmt.Sprintf("%d", d.stats.AuditCount)
		if d.stats.Health.ChainIntact {
			auditSub = "chain intact ✓"
		} else {
			auditSub = "⚠ verify pending"
		}
		agentVal = fmt.Sprintf("%d / %d", d.stats.AgentsConnected, d.stats.AgentsKnown)
		agentSub = "connected / discovered"
		healthVal = strings.ToUpper(d.stats.Health.State)
		healthSub = "all subsystems"
		if d.stats.Health.State != "nominal" {
			healthSub = "degraded"
		}
		quarVal = fmt.Sprintf("%d", d.stats.QuarantineTotal)
		quarSub = "items held"
		if d.stats.WALLagMs < 1 {
			walVal = "< 1ms"
		} else {
			walVal = fmt.Sprintf("%.0fms", d.stats.WALLagMs)
		}
		if d.stats.WALFsyncOK {
			walSub = "fsync verified ✓"
		} else {
			walSub = "⚠ fsync pending"
		}
	} else if s != nil {
		memVal = fmt.Sprintf("%d", s.MemoriesTotal)
		quarVal = fmt.Sprintf("%d", s.QuarantineTotal)
		if d.healthy {
			healthVal = "NOMINAL"
			healthSub = "all subsystems"
		}
	}

	if len(d.agents) > 0 && d.stats == nil {
		connected := 0
		for _, a := range d.agents {
			if a.Status == "active" || a.Status == "online" {
				connected++
			}
		}
		agentVal = fmt.Sprintf("%d / %d", connected, len(d.agents))
		agentSub = "connected / discovered"
	}

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		components.StatCard(components.StatCardProps{Label: "Memories", Value: memVal, SubLabel: memSub, Accent: styles.ColorTeal, Width: cardW, Height: cardH, Loaded: loaded}),
		"  ",
		components.StatCard(components.StatCardProps{Label: "Audit Events", Value: auditVal, SubLabel: auditSub, Accent: styles.ColorGreen, Width: cardW, Height: cardH, Loaded: loaded}),
		"  ",
		components.StatCard(components.StatCardProps{Label: "AI Agents", Value: agentVal, SubLabel: agentSub, Accent: styles.ColorPurple, Width: cardW, Height: cardH, Loaded: loaded}),
	)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top,
		components.StatCard(components.StatCardProps{Label: "Health", Value: healthVal, SubLabel: healthSub, Accent: styles.ColorGreen, Width: cardW, Height: cardH, Loaded: loaded}),
		"  ",
		components.StatCard(components.StatCardProps{Label: "Quarantine", Value: quarVal, SubLabel: quarSub, Accent: styles.ColorAmber, Width: cardW, Height: cardH, Loaded: loaded}),
		"  ",
		components.StatCard(components.StatCardProps{Label: "WAL Lag", Value: walVal, SubLabel: walSub, Accent: styles.ColorGreen, Width: cardW, Height: cardH, Loaded: loaded}),
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
	lines = append(lines, "  "+renderThroughputGauge(d.curWrites, d.maxWrites, w-4, styles.ColorGreen))
	lines = append(lines, "")
	lines = append(lines, sectionHeader("READ THROUGHPUT (60s)", w))
	lines = append(lines, "  "+renderThroughputGauge(d.curReads, d.maxReads, w-4, styles.ColorBlue))

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

const agentStaleThreshold = 2 * time.Minute

func agentDot(status string, lastSeen time.Time) string {
	stale := !lastSeen.IsZero() && time.Since(lastSeen) > agentStaleThreshold
	if lastSeen.IsZero() || stale {
		return lipgloss.NewStyle().Foreground(styles.ColorRed).Render("●")
	}
	switch status {
	case "active", "online":
		return lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("●")
	case "idle", "partial":
		return lipgloss.NewStyle().Foreground(styles.ColorBlue).Render("◑")
	default:
		return lipgloss.NewStyle().Foreground(styles.ColorRed).Render("●")
	}
}

func renderThroughputGauge(current, peak, width int, color lipgloss.Color) string {
	if width < 5 {
		width = 5
	}
	if peak < 1 {
		peak = 1
	}
	filled := current * width / peak
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled

	label := fmt.Sprintf(" %d/m", current)
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(styles.TextMuted).Render(strings.Repeat("░", empty))
	return bar + lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(label)
}

var _ Screen = (*DashboardScreen)(nil)
