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
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RetrievalTheaterScreen is Page 3 — live cascade waterfall visualization.
type RetrievalTheaterScreen struct {
	width, height int
	status        *api.StatusResponse
	queryInput    textinput.Model
	editing       bool
	lastQuery     string
}

// NewRetrievalTheaterScreen creates the retrieval theater.
func NewRetrievalTheaterScreen() *RetrievalTheaterScreen {
	ti := textinput.New()
	ti.Placeholder = "type a query and press Enter…"
	ti.CharLimit = 256
	ti.Width = 80
	ti.PromptStyle = lipgloss.NewStyle().Foreground(styles.ColorTeal)
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
	return &RetrievalTheaterScreen{queryInput: ti}
}

func (r *RetrievalTheaterScreen) Name() string { return "Retrieval" }
func (r *RetrievalTheaterScreen) Init() tea.Cmd { return nil }
func (r *RetrievalTheaterScreen) SetSize(w, h int) {
	r.width = w
	r.height = h
	r.queryInput.Width = w - 10
}

func (r *RetrievalTheaterScreen) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "query")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "execute")),
	}
}

func (r *RetrievalTheaterScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case api.StatusBroadcastMsg:
		if m.Data != nil {
			r.status = m.Data
		}
	case tea.KeyMsg:
		if r.editing {
			switch m.String() {
			case "esc":
				r.editing = false
				r.queryInput.Blur()
				return r, nil
			case "enter":
				r.editing = false
				r.lastQuery = r.queryInput.Value()
				r.queryInput.Blur()
				return r, nil
			default:
				var cmd tea.Cmd
				r.queryInput, cmd = r.queryInput.Update(msg)
				return r, cmd
			}
		}
		if m.String() == "/" {
			r.editing = true
			r.queryInput.Focus()
			return r, textinput.Blink
		}
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

	var sections []string

	// Query input
	sections = append(sections, sectionHeader("QUERY", r.width))
	if r.editing {
		sections = append(sections, "  "+r.queryInput.View())
	} else {
		query := r.lastQuery
		if query == "" {
			query = "(press / to enter a query)"
		}
		sections = append(sections, fmt.Sprintf("  [/] %s",
			lipgloss.NewStyle().Foreground(styles.TextWhiteDim).Render(query)))
	}
	sections = append(sections, "")

	// Waterfall cascade visualization
	stages := r.buildWaterfallStages()
	queryLabel := r.lastQuery
	if queryLabel == "" {
		queryLabel = "—"
	}
	waterfall := components.RenderWaterfall(components.WaterfallProps{
		Stages: stages,
		Width:  r.width,
		Query:  queryLabel,
	})
	sections = append(sections, waterfall)
	sections = append(sections, "")

	// Cascade details + cache performance
	sections = append(sections, r.viewCascadeDetails())
	sections = append(sections, "")

	// SQL Preview panel — shows the structured SQL from Stage 3.
	sections = append(sections, components.RenderSQLPreview(components.SQLPreviewProps{
		SQL:    "",
		Params: nil,
		Width:  r.width,
	}))
	sections = append(sections, "")

	sections = append(sections, r.viewCachePerformance())

	return lipgloss.NewStyle().Width(r.width).Height(r.height).
		Render(strings.Join(sections, "\n"))
}

func (r *RetrievalTheaterScreen) buildWaterfallStages() []components.WaterfallStage {
	stageNames := []struct{ key, id, label string }{
		{"policy_gate", "0", "Policy Gate"},
		{"exact_cache", "1", "Exact Cache"},
		{"semantic_cache", "2", "Semantic Cache"},
		{"structured_sql", "3", "Structured SQL"},
		{"bm25_fts5", "3.75", "BM25 FTS5"},
		{"vector_cosine", "4", "Vector (cosine)"},
		{"hybrid_merge", "5", "Hybrid Merge (RRF)"},
	}

	stages := make([]components.WaterfallStage, len(stageNames))
	for i, sn := range stageNames {
		ws := components.WaterfallStage{
			ID:    sn.id,
			Name:  sn.label,
			State: components.WaterfallIdle,
		}
		if r.status != nil {
			if sm, ok := r.status.CascadeStages[sn.key]; ok {
				ws.DurationMs = sm.AvgMs
				ws.Hits = int(sm.Hits)
				if sm.Hits > 0 {
					ws.State = components.WaterfallDone
					ws.Progress = 1.0
					if sm.AvgMs > 50 {
						ws.State = components.WaterfallSlow
					}
				} else if sm.Status == "SKIP" || sm.Status == "MISS" {
					ws.State = components.WaterfallSkipped
					ws.Progress = 1.0
				} else if sm.Status == "HIT" || sm.Status == "OK" {
					ws.State = components.WaterfallDone
					ws.Progress = 1.0
				}
			}
		}
		stages[i] = ws
	}
	return stages
}

func (r *RetrievalTheaterScreen) viewCascadeDetails() string {
	var lines []string
	lines = append(lines, sectionHeader("CASCADE DETAILS", r.width))
	lines = append(lines, "")

	if r.status == nil {
		lines = append(lines, styles.MutedStyle.Render("  Waiting for cascade data..."))
		return strings.Join(lines, "\n")
	}

	stageNames := []struct{ key, label string }{
		{"policy_gate", "Policy Gate"},
		{"exact_cache", "Exact Cache"},
		{"semantic_cache", "Semantic Cache"},
		{"structured_sql", "Structured SQL"},
		{"bm25_fts5", "BM25 FTS5"},
		{"vector_cosine", "Vector (cosine)"},
		{"hybrid_merge", "Hybrid Merge"},
	}

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

	return strings.Join(lines, "\n")
}

func (r *RetrievalTheaterScreen) viewCachePerformance() string {
	var lines []string
	lines = append(lines, sectionHeader("CACHE PERFORMANCE", r.width))
	lines = append(lines, "")

	if r.status == nil {
		lines = append(lines, styles.MutedStyle.Render("  Waiting for data..."))
		return strings.Join(lines, "\n")
	}

	barW := r.width - 6
	if barW < 20 {
		barW = 20
	}
	lines = append(lines, components.InlineBar{Label: "Total hit rate", Value: r.status.Cache.HitRate, Width: barW, Color: styles.ColorTeal}.View())
	lines = append(lines, components.InlineBar{Label: "  Exact", Value: r.status.Cache.ExactRate, Width: barW, Color: styles.ColorGreen}.View())
	lines = append(lines, components.InlineBar{Label: "  Semantic", Value: r.status.Cache.SemanticRate, Width: barW, Color: styles.ColorBlue}.View())

	return strings.Join(lines, "\n")
}

var _ Screen = (*RetrievalTheaterScreen)(nil)
