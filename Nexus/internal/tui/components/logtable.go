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

package components

import (
	"strings"

	"github.com/BubbleFish-Nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LogRow is a single row in the log table.
type LogRow struct {
	Time    string
	Source  string
	Message string
	Code    string
	Level   string // "info", "ok", "warn", "err", "security"
}

// LogTable is a scrollable viewport for log rows.
type LogTable struct {
	Viewport   viewport.Model
	Rows       []LogRow
	Filter     string
	AutoScroll bool
	Width      int
	Height     int
}

// NewLogTable creates a new log table.
func NewLogTable(w, h int) LogTable {
	vp := viewport.New(w, h)
	return LogTable{
		Viewport:   vp,
		AutoScroll: true,
		Width:      w,
		Height:     h,
	}
}

// SetSize updates the viewport dimensions.
func (l *LogTable) SetSize(w, h int) {
	l.Width = w
	l.Height = h
	l.Viewport.Width = w
	l.Viewport.Height = h
}

// SetRows updates the log rows and re-renders content.
func (l *LogTable) SetRows(rows []LogRow) {
	l.Rows = rows
	l.renderContent()
}

func (l *LogTable) renderContent() {
	var lines []string
	for _, r := range l.Rows {
		if l.Filter != "" && !strings.Contains(strings.ToLower(r.Message), strings.ToLower(l.Filter)) &&
			!strings.Contains(strings.ToLower(r.Source), strings.ToLower(l.Filter)) {
			continue
		}

		borderColor := styles.ColorBlue
		switch r.Level {
		case "ok":
			borderColor = styles.ColorGreen
		case "warn":
			borderColor = styles.ColorAmber
		case "err":
			borderColor = styles.ColorRed
		case "security":
			borderColor = styles.ColorPurple
		}

		border := lipgloss.NewStyle().Foreground(borderColor).Render("┃")
		ts := lipgloss.NewStyle().Foreground(styles.TextMuted).Width(20).Render(r.Time)
		src := lipgloss.NewStyle().Foreground(styles.TextSecondary).Width(12).Render(r.Source)
		msg := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(truncate(r.Message, 80))
		code := lipgloss.NewStyle().Foreground(styles.TextSecondary).Render(r.Code)

		line := border + " " + ts + " " + src + " " + msg + " " + code
		lines = append(lines, line)
	}

	l.Viewport.SetContent(strings.Join(lines, "\n"))
	if l.AutoScroll {
		l.Viewport.GotoBottom()
	}
}

// Update handles viewport messages.
func (l *LogTable) Update(msg tea.Msg) {
	l.Viewport, _ = l.Viewport.Update(msg)
}

// View renders the log table.
func (l LogTable) View() string {
	return l.Viewport.View()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
