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

// RetrievalTheaterScreen is Page 3 — watch queries traverse the cascade.
type RetrievalTheaterScreen struct {
	width, height int
	status        *api.StatusResponse
	err           error
}

// NewRetrievalTheaterScreen creates the retrieval theater.
func NewRetrievalTheaterScreen() *RetrievalTheaterScreen {
	return &RetrievalTheaterScreen{}
}

func (r *RetrievalTheaterScreen) Name() string            { return "Retrieval" }
func (r *RetrievalTheaterScreen) Init() tea.Cmd            { return nil }
func (r *RetrievalTheaterScreen) SetSize(w, h int)         { r.width = w; r.height = h }
func (r *RetrievalTheaterScreen) ShortHelp() []key.Binding { return nil }

func (r *RetrievalTheaterScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	if m, ok := msg.(api.StatusBroadcastMsg); ok && m.Data != nil {
		r.status = m.Data
	}
	return r, nil
}

func (r *RetrievalTheaterScreen) FireRefresh(_ *api.Client) tea.Cmd {
	return nil
}

func (r *RetrievalTheaterScreen) View() string {
	if r.width < 40 || r.height < 10 {
		return ""
	}

	var lines []string

	lines = append(lines, sectionHeader("RETRIEVAL CASCADE", r.width))
	lines = append(lines, "")

	// Stage flow visualization using existing StageFlow component.
	stageNames := []struct{ key, label string }{
		{"policy_gate", "Policy Gate"},
		{"exact_cache", "Exact Cache"},
		{"semantic_cache", "Semantic Cache"},
		{"structured_sql", "Structured SQL"},
		{"bm25_fts5", "BM25 FTS5"},
		{"vector_cosine", "Vector (cosine)"},
		{"hybrid_merge", "Hybrid Merge"},
	}

	var stages []components.Stage
	for i, sn := range stageNames {
		status := "SKIP"
		latency := "—"
		if r.status != nil {
			if sm, ok := r.status.CascadeStages[sn.key]; ok {
				status = sm.Status
				if sm.Hits > 0 {
					latency = fmt.Sprintf("%.1fms", sm.AvgMs)
				}
			}
		}
		stages = append(stages, components.Stage{
			Number: i, Name: sn.label, Status: status, Latency: latency,
		})
	}

	flow := components.StageFlow{Stages: stages, Width: r.width}
	lines = append(lines, flow.View())
	lines = append(lines, "")

	// Cascade details.
	lines = append(lines, sectionHeader("CASCADE DETAILS", r.width))
	lines = append(lines, "")

	if r.status != nil {
		for _, sn := range stageNames {
			if sm, ok := r.status.CascadeStages[sn.key]; ok {
				nameStyle := lipgloss.NewStyle().Foreground(styles.TextPrimary).Width(18)
				var statusStyle lipgloss.Style
				switch sm.Status {
				case "HIT", "OK":
					statusStyle = lipgloss.NewStyle().Foreground(styles.ColorGreen)
				case "MISS":
					statusStyle = lipgloss.NewStyle().Foreground(styles.TextMuted)
				default:
					statusStyle = lipgloss.NewStyle().Foreground(styles.ColorAmber)
				}

				hits := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).
					Render(fmt.Sprintf("%d hits", sm.Hits))
				avg := lipgloss.NewStyle().Foreground(styles.ColorTeal).
					Render(fmt.Sprintf("%.1fms avg", sm.AvgMs))

				lines = append(lines, fmt.Sprintf("  %s %s  %s  %s",
					nameStyle.Render(sn.label),
					statusStyle.Render(sm.Status),
					avg, hits))
			}
		}

		// Cache rates.
		lines = append(lines, "")
		lines = append(lines, sectionHeader("CACHE PERFORMANCE", r.width))
		lines = append(lines, "")
		barW := r.width - 6
		if barW < 20 {
			barW = 20
		}
		lines = append(lines, components.InlineBar{Label: "Total hit rate", Value: r.status.Cache.HitRate, Width: barW, Color: styles.ColorTeal}.View())
		lines = append(lines, components.InlineBar{Label: "  Exact", Value: r.status.Cache.ExactRate, Width: barW, Color: styles.ColorGreen}.View())
		lines = append(lines, components.InlineBar{Label: "  Semantic", Value: r.status.Cache.SemanticRate, Width: barW, Color: styles.ColorBlue}.View())
	} else {
		lines = append(lines, styles.MutedStyle.Render("  Waiting for cascade data..."))
	}

	if r.err != nil {
		lines = append(lines, "")
		lines = append(lines, styles.ErrorStyle.Render("  error: "+r.err.Error()))
	}

	return lipgloss.NewStyle().Width(r.width).Height(r.height).
		Render(strings.Join(lines, "\n"))
}

var _ Screen = (*RetrievalTheaterScreen)(nil)
