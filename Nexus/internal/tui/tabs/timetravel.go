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
	"time"

	"github.com/BubbleFish-Nexus/internal/tui/api"
	"github.com/BubbleFish-Nexus/internal/tui/components"
	"github.com/BubbleFish-Nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// timeTravelMsg carries the result of a time-travel API call.
type timeTravelMsg struct {
	data *api.TimeTravelResponse
	err  error
}

// TimeTravelTab provides manual time-travel queries against the memory store.
type TimeTravelTab struct {
	asOfInput    textinput.Model
	subjectInput textinput.Model
	focusIndex   int // 0 = asOf, 1 = subject
	client       *api.Client
	result       *api.TimeTravelResponse
	err          error
	queried      bool
}

// NewTimeTravelTab returns an initialised TimeTravelTab.
func NewTimeTravelTab() *TimeTravelTab {
	asOf := textinput.New()
	asOf.Placeholder = "2026-01-15T00:00:00Z"
	asOf.CharLimit = 64
	asOf.Width = 40
	asOf.Prompt = "as_of: "
	asOf.Focus()

	subject := textinput.New()
	subject.Placeholder = "subject filter (optional)"
	subject.CharLimit = 128
	subject.Width = 40
	subject.Prompt = "subject: "

	return &TimeTravelTab{
		asOfInput:    asOf,
		subjectInput: subject,
		focusIndex:   0,
	}
}

// Name returns the tab display name.
func (t *TimeTravelTab) Name() string { return "Time-Travel" }

// Init returns the first command (text input blink).
func (t *TimeTravelTab) Init() tea.Cmd {
	return textinput.Blink
}

// FireRefresh is a no-op for time-travel: queries are manual only.
func (t *TimeTravelTab) FireRefresh(_ *api.Client) tea.Cmd {
	return nil
}

// Update handles incoming messages.
func (t *TimeTravelTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case timeTravelMsg:
		t.err = msg.err
		t.result = msg.data
		t.queried = true
		return t, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab":
			// Toggle focus between the two inputs.
			if t.focusIndex == 0 {
				t.focusIndex = 1
				t.asOfInput.Blur()
				t.subjectInput.Focus()
			} else {
				t.focusIndex = 0
				t.subjectInput.Blur()
				t.asOfInput.Focus()
			}
			return t, textinput.Blink

		case "enter":
			if t.client == nil {
				return t, nil
			}
			client := t.client
			asOf := t.asOfInput.Value()
			subject := t.subjectInput.Value()
			return t, func() tea.Msg {
				data, err := client.TimeTravel(api.TimeTravelOpts{
					AsOf:    asOf,
					Subject: subject,
					Limit:   50,
				})
				return timeTravelMsg{data: data, err: err}
			}
		}
	}

	// Forward key messages to the focused input.
	var cmd tea.Cmd
	if t.focusIndex == 0 {
		t.asOfInput, cmd = t.asOfInput.Update(msg)
	} else {
		t.subjectInput, cmd = t.subjectInput.Update(msg)
	}
	return t, cmd
}

// SetClient stores the API client for manual queries.
func (t *TimeTravelTab) SetClient(client *api.Client) {
	t.client = client
}

// View renders the time-travel query interface and results.
func (t *TimeTravelTab) View(width, height int) string {
	var sections []string

	sections = append(sections, components.SectionTitle("Time-Travel Query", width))
	sections = append(sections, "")

	// Input fields.
	sections = append(sections, t.asOfInput.View())
	sections = append(sections, t.subjectInput.View())
	sections = append(sections, "")
	sections = append(sections, styles.MutedStyle.Render("Tab to switch fields, Enter to query"))
	sections = append(sections, "")

	// Error state.
	if t.err != nil {
		sections = append(sections, styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", t.err)))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Not yet queried.
	if !t.queried {
		sections = append(sections, styles.MutedStyle.Render("Enter an RFC3339 timestamp and press Enter to query."))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Empty results.
	if t.result == nil || len(t.result.Records) == 0 {
		sections = append(sections, styles.MutedStyle.Render("No records found for the given parameters."))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Query params banner.
	banner := fmt.Sprintf("as_of=%s", t.result.AsOf)
	if t.subjectInput.Value() != "" {
		banner += fmt.Sprintf("  subject=%s", t.subjectInput.Value())
	}
	if t.result.HasMore {
		banner += "  (more results available)"
	}
	sections = append(sections, lipgloss.NewStyle().
		Foreground(styles.ColorTeal).
		Background(styles.BgRow).
		Padding(0, 1).
		Width(width-2).
		Render(banner))
	sections = append(sections, "")

	// Results table header.
	header := fmt.Sprintf("  %-10s %-16s %-62s %-12s %-8s %s",
		"PAYLOAD", "SUBJECT", "CONTENT", "SOURCE", "ACTOR", "TIMESTAMP")
	sections = append(sections, styles.MutedStyle.Render(header))
	sections = append(sections, lipgloss.NewStyle().Foreground(styles.BorderBase).Render(
		strings.Repeat("─", width-2)))

	// Result rows.
	maxRows := height - 16
	if maxRows < 5 {
		maxRows = 5
	}
	for i, r := range t.result.Records {
		if i >= maxRows {
			sections = append(sections, styles.MutedStyle.Render(
				fmt.Sprintf("  ... and %d more records", len(t.result.Records)-maxRows)))
			break
		}

		pid := r.PayloadID
		if len(pid) > 8 {
			pid = pid[:8]
		}

		content := r.Content
		if len(content) > 60 {
			content = content[:57] + "..."
		}

		badge := components.ProvBadge(r.ActorType)

		row := fmt.Sprintf("  %-10s %-16s %-62s %-12s %s %s",
			lipgloss.NewStyle().Foreground(styles.TextMuted).Render(pid),
			lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(r.Subject),
			lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(content),
			lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(r.Source),
			badge,
			lipgloss.NewStyle().Foreground(styles.TextMuted).Render(r.Timestamp.In(time.Local).Format("2006-01-02 15:04:05")),
		)
		sections = append(sections, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// Compile-time interface check.
var _ Tab = (*TimeTravelTab)(nil)
