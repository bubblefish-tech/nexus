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
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/api"
	"github.com/bubblefish-tech/nexus/internal/tui/components"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type auditLogMsg struct {
	data    *api.AuditResponse
	errKind api.ErrorKind
	hint    string
}

type auditKeyMap struct {
	Prev   key.Binding
	Next   key.Binding
	Filter key.Binding
	Auto   key.Binding
}

var auditKeys = auditKeyMap{
	Prev:   key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h/←", "prev entry")),
	Next:   key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l/→", "next entry")),
	Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	Auto:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "auto-scroll")),
}

// AuditWalkerScreen is Page 4 — hash chain walker.
type AuditWalkerScreen struct {
	width, height int
	table         components.LogTable
	records       []api.AuditRecord
	errKind       api.ErrorKind
	errHint       string
	loading       bool
	autoScroll    bool
	selectedIdx   int
	count         int
}

// NewAuditWalkerScreen creates the audit walker.
func NewAuditWalkerScreen() *AuditWalkerScreen {
	return &AuditWalkerScreen{
		table:      components.NewLogTable(80, 20),
		autoScroll: true,
		loading:    true,
	}
}

func (a *AuditWalkerScreen) Name() string { return "Audit" }
func (a *AuditWalkerScreen) Init() tea.Cmd { return nil }

func (a *AuditWalkerScreen) SetSize(w, h int) {
	a.width = w
	a.height = h
}

func (a *AuditWalkerScreen) ShortHelp() []key.Binding {
	return []key.Binding{auditKeys.Prev, auditKeys.Next, auditKeys.Filter, auditKeys.Auto}
}

func (a *AuditWalkerScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case auditLogMsg:
		a.loading = false
		a.errKind = m.errKind
		a.errHint = m.hint
		if m.errKind == api.ErrKindUnknown && m.data != nil {
			a.records = m.data.Records
			a.count = len(m.data.Records)
			a.rebuildRows()
		}
		return a, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(m, auditKeys.Prev):
			if a.selectedIdx > 0 {
				a.selectedIdx--
			}
			return a, nil
		case key.Matches(m, auditKeys.Next):
			if a.selectedIdx < len(a.records)-1 {
				a.selectedIdx++
			}
			return a, nil
		case key.Matches(m, auditKeys.Auto):
			a.autoScroll = !a.autoScroll
			a.table.AutoScroll = a.autoScroll
			return a, nil
		default:
			a.table.Update(m)
		}
	}
	return a, nil
}

func (a *AuditWalkerScreen) FireRefresh(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		data, err := client.AuditLog(200)
		if err != nil {
			kind := api.Classify(err)
			sdbg("AuditLog failed kind=%d err=%v", kind, err)
			return auditLogMsg{errKind: kind, hint: api.HintForEndpoint("/api/audit/log", kind)}
		}
		return auditLogMsg{data: data}
	}
}

func (a *AuditWalkerScreen) rebuildRows() {
	rows := make([]components.LogRow, 0, len(a.records))
	for _, rec := range a.records {
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
	a.table.SetRows(rows)
}

func (a *AuditWalkerScreen) View() string {
	if a.width < 40 || a.height < 10 {
		return ""
	}

	if a.loading {
		frame := int(time.Now().UnixMilli()/150) % 8
		return components.Render(loadingOpts(a.width, a.height, frame))
	}
	if a.errKind != api.ErrKindUnknown {
		return components.Render(emptyStateOpts(a.errKind, a.errHint, a.width, a.height))
	}

	var sections []string

	header := lipgloss.JoinHorizontal(lipgloss.Bottom,
		sectionHeader("AUDIT CHAIN", a.width),
		"  ",
		lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render(fmt.Sprintf("— %d entries", a.count)),
	)
	sections = append(sections, header)

	navHint := lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("  ← [h] previous  ·  next [l] →  ·  [a] auto-scroll  ·  [/] filter")
	sections = append(sections, navHint)
	sections = append(sections, "")

	if len(a.records) > 0 && a.selectedIdx < len(a.records) {
		rec := a.records[a.selectedIdx]
		sections = append(sections, a.viewEntryDetail(rec))
		sections = append(sections, "")
	}

	tableH := a.height - len(sections) - 2
	if tableH < 5 {
		tableH = 5
	}
	a.table.SetSize(a.width, tableH)
	sections = append(sections, a.table.View())

	autoTag := styles.MutedStyle.Render("OFF")
	if a.autoScroll {
		autoTag = styles.SuccessStyle.Render("ON")
	}
	countStrip := styles.MutedStyle.Render(
		fmt.Sprintf("%d events · auto-scroll: ", a.count)) + autoTag
	sections = append(sections, countStrip)

	return lipgloss.NewStyle().Width(a.width).Height(a.height).
		Render(strings.Join(sections, "\n"))
}

func (a *AuditWalkerScreen) viewEntryDetail(rec api.AuditRecord) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.BorderStrong).
		Padding(0, 1).
		Width(a.width - 4)

	header := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render(fmt.Sprintf("  Entry #%d", a.selectedIdx+1))
	ts := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).
		Render(rec.Timestamp.Format("2006-01-02  15:04:05 UTC"))

	titleLine := lipgloss.JoinHorizontal(lipgloss.Bottom, header, "  ", ts)

	hashStyle := lipgloss.NewStyle().Foreground(styles.ColorTealDim)
	idLine := fmt.Sprintf("  record_id:  %s", hashStyle.Render(rec.RecordID))
	reqLine := fmt.Sprintf("  request_id: %s", hashStyle.Render(rec.RequestID))
	opLine := fmt.Sprintf("  operation:  %s %s → %d",
		rec.HTTPMethod, rec.Endpoint, rec.HTTPStatusCode)
	srcLine := fmt.Sprintf("  source:     %s  ·  actor: %s (%s)",
		rec.Source, rec.ActorType, rec.ActorID)

	policyColor := styles.ColorGreen
	if rec.PolicyDecision == "denied" {
		policyColor = styles.ColorRed
	} else if rec.PolicyDecision == "filtered" {
		policyColor = styles.ColorAmber
	}
	policyLine := fmt.Sprintf("  policy:     %s",
		lipgloss.NewStyle().Foreground(policyColor).Render(rec.PolicyDecision))
	if rec.PolicyReason != "" {
		policyLine += "  (" + rec.PolicyReason + ")"
	}

	latencyLine := fmt.Sprintf("  latency:    %.1fms", rec.LatencyMs)

	content := strings.Join([]string{titleLine, "", idLine, reqLine, opLine, srcLine, policyLine, latencyLine}, "\n")
	return box.Render(content)
}

var _ Screen = (*AuditWalkerScreen)(nil)
