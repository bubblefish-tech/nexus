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
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// auditEventsMsg carries the result of a security events API call.
type auditEventsMsg struct {
	data *api.SecurityEventsResponse
	err  error
}

// AuditTab displays security events in a scrollable log table.
type AuditTab struct {
	table      components.LogTable
	events     []api.SecurityEvent
	err        error
	autoScroll bool
	filtering  bool
	filterText textinput.Model
	count      int
}

// NewAuditTab creates a new Audit tab.
func NewAuditTab() *AuditTab {
	ti := textinput.New()
	ti.Placeholder = "type to filter..."
	ti.CharLimit = 128
	ti.Width = 40
	ti.PromptStyle = lipgloss.NewStyle().Foreground(styles.ColorTeal)
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)

	return &AuditTab{
		table:      components.NewLogTable(80, 20),
		autoScroll: true,
		filterText: ti,
	}
}

// Name returns the tab display name.
func (t *AuditTab) Name() string { return "Audit" }

// Init returns the initial command.
func (t *AuditTab) Init() tea.Cmd { return nil }

// Update handles incoming messages and key events.
func (t *AuditTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch m := msg.(type) {
	case auditEventsMsg:
		t.err = m.err
		if m.data != nil {
			t.events = m.data.Events
			t.count = len(m.data.Events)
			t.rebuildRows()
		}
		return t, nil

	case tea.KeyMsg:
		if t.filtering {
			switch m.String() {
			case "esc", "enter":
				t.filtering = false
				t.filterText.Blur()
				t.table.Filter = t.filterText.Value()
				t.rebuildRows()
				return t, nil
			default:
				var cmd tea.Cmd
				t.filterText, cmd = t.filterText.Update(msg)
				t.table.Filter = t.filterText.Value()
				t.rebuildRows()
				return t, cmd
			}
		}

		switch m.String() {
		case "a":
			t.autoScroll = !t.autoScroll
			t.table.AutoScroll = t.autoScroll
			return t, nil
		case "/":
			t.filtering = true
			t.filterText.Focus()
			return t, textinput.Blink
		}

		// Forward navigation keys to the viewport.
		t.table.Update(msg)
		return t, nil
	}

	// Forward other messages (e.g., blink) to the filter input.
	if t.filtering {
		var cmd tea.Cmd
		t.filterText, cmd = t.filterText.Update(msg)
		return t, cmd
	}

	return t, nil
}

// FireRefresh dispatches the security events API call.
func (t *AuditTab) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.SecurityEvents(200)
		return auditEventsMsg{data: data, err: err}
	}
}

// rebuildRows converts security events to LogRows.
func (t *AuditTab) rebuildRows() {
	rows := make([]components.LogRow, 0, len(t.events))
	for _, ev := range t.events {
		level := "info"
		switch {
		case strings.Contains(ev.EventType, "tamper"):
			level = "security"
		case strings.Contains(ev.EventType, "fail") || strings.Contains(ev.EventType, "denied"):
			level = "err"
		case strings.Contains(ev.EventType, "rate_limit"):
			level = "warn"
		case strings.Contains(ev.EventType, "success") || strings.Contains(ev.EventType, "ok"):
			level = "ok"
		}

		detail := ""
		for k, v := range ev.Details {
			if detail != "" {
				detail += ", "
			}
			detail += k + "=" + v
		}

		rows = append(rows, components.LogRow{
			Time:    ev.Timestamp.Format("15:04:05.000"),
			Source:  ev.IP,
			Message: ev.EventType + " " + ev.Endpoint,
			Code:    detail,
			Level:   level,
		})
	}
	t.table.SetRows(rows)
}

// View renders the audit tab content.
func (t *AuditTab) View(width, height int) string {
	var sections []string

	// --- Filter bar ---
	filterLabel := styles.MutedStyle.Render("FILTER")
	filterButtons := lipgloss.JoinHorizontal(lipgloss.Center,
		styles.MutedStyle.Render("[/] search"),
		"  ",
		styles.MutedStyle.Render("[a] auto-scroll"),
	)

	autoTag := styles.MutedStyle.Render("OFF")
	if t.autoScroll {
		autoTag = styles.SuccessStyle.Render("ON")
	}
	scrollStatus := styles.MutedStyle.Render("auto-scroll: ") + autoTag

	filterBar := lipgloss.JoinHorizontal(lipgloss.Center,
		filterLabel, "  ", filterButtons, "  ", scrollStatus,
	)
	sections = append(sections, filterBar)

	// --- Text input (shown when filtering) ---
	if t.filtering {
		sections = append(sections, t.filterText.View())
	} else if t.filterText.Value() != "" {
		activeFilter := styles.TealStyle.Render("filter: "+t.filterText.Value()) +
			"  " + styles.MutedStyle.Render("[/] edit  [esc] clear")
		sections = append(sections, activeFilter)
	}

	// --- Log table ---
	tableHeight := height - len(sections) - 3 // room for count strip + padding
	if tableHeight < 5 {
		tableHeight = 5
	}
	t.table.SetSize(width, tableHeight)
	sections = append(sections, t.table.View())

	// --- Count strip ---
	countStrip := styles.MutedStyle.Render(
		fmt.Sprintf("%d events", t.count) + " \u00b7 auto-refresh 5s",
	)
	sections = append(sections, countStrip)

	// --- Error line ---
	if t.err != nil {
		errLine := styles.ErrorStyle.Render("events error: " + t.err.Error())
		sections = append(sections, errLine)
	}

	content := strings.Join(sections, "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Render(content)
}

// Compile-time interface check.
var _ Tab = (*AuditTab)(nil)
