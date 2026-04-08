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

// conflictsMsg carries the result of a conflicts API call.
type conflictsMsg struct {
	data *api.ConflictsResponse
	err  error
}

// ConflictsTab shows conflict groups with navigation.
type ConflictsTab struct {
	conflicts *api.ConflictsResponse
	err       error
	cursor    int
}

// NewConflictsTab returns an initialised ConflictsTab.
func NewConflictsTab() *ConflictsTab {
	return &ConflictsTab{}
}

// Name returns the tab display name.
func (t *ConflictsTab) Name() string { return "Conflicts" }

// Init returns the first command (none needed).
func (t *ConflictsTab) Init() tea.Cmd { return nil }

// FireRefresh fetches fresh conflict data from the daemon.
func (t *ConflictsTab) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.Conflicts(api.ConflictOpts{Limit: 50})
		return conflictsMsg{data: data, err: err}
	}
}

// Update handles incoming messages.
func (t *ConflictsTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case conflictsMsg:
		t.err = msg.err
		t.conflicts = msg.data
		t.cursor = 0
		return t, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "n":
			if t.conflicts != nil && t.cursor < len(t.conflicts.Conflicts)-1 {
				t.cursor++
			}
		case "p":
			if t.cursor > 0 {
				t.cursor--
			}
		}
		return t, nil
	}
	return t, nil
}

// View renders the conflicts display.
func (t *ConflictsTab) View(width, height int) string {
	var sections []string

	sections = append(sections, components.SectionTitle("Memory Conflicts", width))

	if t.err != nil {
		sections = append(sections, styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", t.err)))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	if t.conflicts == nil || len(t.conflicts.Conflicts) == 0 {
		sections = append(sections, "")
		sections = append(sections, styles.MutedStyle.Render("No conflicts detected. Memory is consistent."))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	total := len(t.conflicts.Conflicts)
	sections = append(sections, "")
	counter := fmt.Sprintf("Conflict %d of %d", t.cursor+1, total)
	sections = append(sections, styles.TealStyle.Render(counter))
	sections = append(sections, styles.MutedStyle.Render("Navigate: p/n  previous/next"))
	sections = append(sections, "")

	c := t.conflicts.Conflicts[t.cursor]
	sections = append(sections, renderConflictCard(c, width))

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderConflictCard renders a single conflict group.
func renderConflictCard(c api.ConflictEntry, width int) string {
	var lines []string

	// Subject header.
	subjectLine := styles.PrimaryStyle.Bold(true).Render(c.Subject)
	if c.EntityKey != "" {
		subjectLine += styles.MutedStyle.Render("  key=" + c.EntityKey)
	}
	lines = append(lines, subjectLine)
	lines = append(lines, "")

	// Conflicting values.
	lines = append(lines, styles.StatLabel.Render("CONFLICTING VALUES"))
	for i, v := range c.ConflictingValues {
		display := v
		if len(display) > 80 {
			display = display[:77] + "..."
		}
		lines = append(lines, fmt.Sprintf("  %d. %s", i+1,
			lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(display)))
	}
	lines = append(lines, "")

	// Sources with provenance badges.
	lines = append(lines, styles.StatLabel.Render("SOURCES"))
	for _, src := range c.Sources {
		lines = append(lines, "  "+components.ProvBadge(src)+" "+
			lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(src))
	}
	lines = append(lines, "")

	// Timestamps.
	lines = append(lines, styles.StatLabel.Render("TIMESTAMPS"))
	for _, ts := range c.Timestamps {
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(ts))
	}
	lines = append(lines, "")

	// Decay score colouring based on count.
	decayScore := 1.0
	if c.Count > 0 {
		decayScore = 1.0 / float64(c.Count)
	}
	scoreColor := styles.ColorGreen
	if decayScore < 0.4 {
		scoreColor = styles.ColorRed
	} else if decayScore < 0.7 {
		scoreColor = styles.ColorAmber
	}
	scoreLine := fmt.Sprintf("Decay score: %.2f  (count: %d)", decayScore, c.Count)
	lines = append(lines, lipgloss.NewStyle().Foreground(scoreColor).Render(scoreLine))

	content := strings.Join(lines, "\n")

	cardWidth := width - 4
	if cardWidth < 40 {
		cardWidth = 40
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderBase).
		Padding(1, 2).
		Width(cardWidth).
		Render(content)
}

// Compile-time interface check.
var _ Tab = (*ConflictsTab)(nil)
