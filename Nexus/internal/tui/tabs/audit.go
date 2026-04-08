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

// auditLogMsg carries the result of an audit log API call.
type auditLogMsg struct {
	data *api.AuditResponse
	err  error
}

// AuditTab displays audit log records in a scrollable log table.
type AuditTab struct {
	table      components.LogTable
	records    []api.AuditRecord
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
	case auditLogMsg:
		t.err = m.err
		if m.data != nil {
			t.records = m.data.Records
			t.count = len(m.data.Records)
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

// FireRefresh dispatches the audit log API call.
func (t *AuditTab) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.AuditLog(200)
		return auditLogMsg{data: data, err: err}
	}
}

// rebuildRows converts audit records to LogRows.
func (t *AuditTab) rebuildRows() {
	rows := make([]components.LogRow, 0, len(t.records))
	for _, rec := range t.records {
		level := "info"
		switch {
		case rec.PolicyDecision == "denied":
			level = "err"
		case rec.PolicyDecision == "filtered":
			level = "warn"
		case rec.HTTPStatusCode >= 400 && rec.HTTPStatusCode < 500:
			level = "warn"
		case rec.HTTPStatusCode >= 500:
			level = "err"
		case rec.PolicyDecision == "allowed":
			level = "ok"
		}

		code := fmt.Sprintf("%d", rec.HTTPStatusCode)
		if rec.PolicyDecision != "" && rec.PolicyDecision != "allowed" {
			code += " " + rec.PolicyDecision
		}

		rows = append(rows, components.LogRow{
			Time:    rec.Timestamp.Format("15:04:05.000"),
			Source:  rec.Source,
			Message: rec.OperationType + " " + rec.Endpoint,
			Code:    code,
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
