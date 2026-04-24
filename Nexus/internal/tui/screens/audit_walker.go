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
		sections = append(sections, components.RenderEntryCard(components.EntryCardProps{
			EntryN:    a.selectedIdx + 1,
			Total:     a.count,
			Timestamp: rec.Timestamp.Format("2006-01-02  15:04:05 UTC"),
			RecordID:  rec.RecordID,
			PrevHash:  rec.PrevHash,
			ContentID: rec.RecordID,
			Preview:   rec.OperationType + " " + rec.Endpoint,
			Hash:      rec.Hash,
			Signature: rec.Signature,
			SigValid:  rec.SignatureValid,
			Width:     a.width,
		}))
		sections = append(sections, "")

		sections = append(sections, sectionHeader("MERKLE INCLUSION PROOF", a.width))
		sections = append(sections, styles.MutedStyle.Render(
			"  Merkle proof available when provenance endpoint is wired (T5)"))
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


var _ Screen = (*AuditWalkerScreen)(nil)
