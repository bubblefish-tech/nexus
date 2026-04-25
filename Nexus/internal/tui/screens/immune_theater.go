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

type immuneSecurityMsg struct {
	events  *api.SecurityEventsResponse
	summary *api.SecuritySummaryResponse
	errKind api.ErrorKind
	hint    string
}

type immuneQuarantineMsg struct {
	data    *api.QuarantineResponse
	errKind api.ErrorKind
	hint    string
}

// ImmuneTheaterScreen is Page 8 — quarantine + threat signatures.
type ImmuneTheaterScreen struct {
	width, height   int
	securityEvents  []api.SecurityEvent
	securitySummary *api.SecuritySummaryResponse
	quarantine      *api.QuarantineResponse
	errKind         api.ErrorKind
	errHint         string
	loading         bool
}

// NewImmuneTheaterScreen creates the immune theater.
func NewImmuneTheaterScreen() *ImmuneTheaterScreen {
	return &ImmuneTheaterScreen{loading: true}
}

func (im *ImmuneTheaterScreen) Name() string             { return "Immune" }
func (im *ImmuneTheaterScreen) Init() tea.Cmd            { return nil }
func (im *ImmuneTheaterScreen) SetSize(w, h int)         { im.width = w; im.height = h }
func (im *ImmuneTheaterScreen) ShortHelp() []key.Binding { return nil }

func (im *ImmuneTheaterScreen) Update(msg tea.Msg) (Screen, tea.Cmd) {
	switch m := msg.(type) {
	case immuneSecurityMsg:
		im.loading = false
		if m.errKind != api.ErrKindUnknown {
			im.errKind = m.errKind
			im.errHint = m.hint
		} else {
			im.errKind = api.ErrKindUnknown
			im.errHint = ""
			if m.events != nil {
				im.securityEvents = m.events.Events
			}
			if m.summary != nil {
				im.securitySummary = m.summary
			}
		}
	case immuneQuarantineMsg:
		im.loading = false
		if m.errKind != api.ErrKindUnknown {
			im.errKind = m.errKind
			im.errHint = m.hint
		} else {
			im.errKind = api.ErrKindUnknown
			im.errHint = ""
			if m.data != nil {
				im.quarantine = m.data
			}
		}
	}
	return im, nil
}

func (im *ImmuneTheaterScreen) FireRefresh(client *api.Client) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			events, err := client.SecurityEvents(50)
			if err != nil {
				kind := api.Classify(err)
				sdbg("SecurityEvents failed kind=%d err=%v", kind, err)
				return immuneSecurityMsg{errKind: kind, hint: api.HintForEndpoint("/api/security/events", kind)}
			}
			summary, err := client.SecuritySummary()
			if err != nil {
				kind := api.Classify(err)
				sdbg("SecuritySummary failed kind=%d err=%v", kind, err)
				return immuneSecurityMsg{events: events, errKind: kind, hint: api.HintForEndpoint("/api/security/summary", kind)}
			}
			return immuneSecurityMsg{events: events, summary: summary}
		},
		func() tea.Msg {
			data, err := client.QuarantineList(50)
			if err != nil {
				kind := api.Classify(err)
				sdbg("Quarantine failed kind=%d err=%v", kind, err)
				return immuneQuarantineMsg{errKind: kind, hint: api.HintForEndpoint("/api/quarantine", kind)}
			}
			return immuneQuarantineMsg{data: data}
		},
	)
}

func (im *ImmuneTheaterScreen) View() string {
	if im.width < 40 || im.height < 10 {
		return ""
	}

	if im.loading {
		frame := int(time.Now().UnixMilli()/150) % 8
		return components.Render(loadingOpts(im.width, im.height, frame))
	}
	if im.errKind != api.ErrKindUnknown {
		return components.Render(emptyStateOpts(im.errKind, im.errHint, im.width, im.height))
	}

	leftW := im.width / 2
	rightW := im.width - leftW - 1

	left := im.viewSignatures(leftW)
	right := im.viewQuarantine(rightW)

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftW).Render(left),
		lipgloss.NewStyle().Width(1).Foreground(styles.BorderBase).Render(
			strings.Repeat("│\n", im.height-3)),
		lipgloss.NewStyle().Width(rightW).Render(right),
	)

	var footer string
	if im.quarantine != nil {
		footer = lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render(fmt.Sprintf("  Quarantine: %d total · %d pending",
				im.quarantine.Total, im.quarantine.Pending))
	}
	if im.securitySummary != nil {
		footer += lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render(fmt.Sprintf("  ·  Auth failures: %d · Policy denials: %d · Rate limits: %d",
				im.securitySummary.AuthFailures,
				im.securitySummary.PolicyDenials,
				im.securitySummary.RateLimitHits))
	}

	result := lipgloss.JoinVertical(lipgloss.Left, body, footer)

	return lipgloss.NewStyle().Width(im.width).Height(im.height).Render(result)
}

func (im *ImmuneTheaterScreen) viewSignatures(w int) string {
	var lines []string
	lines = append(lines, sectionHeader("TIER-0 SIGNATURES", w))
	lines = append(lines, "")

	signatures := []struct {
		name string
		desc string
	}{
		{"jailbreak-ignore-prior", "Ignore all previous instructions"},
		{"prompt-injection-system", "System prompt manipulation"},
		{"credential-exfil-attempt", "Credential extraction attempts"},
		{"role-manipulation", "Role/privilege escalation"},
		{"secrets-phishing", "Secret/API key extraction"},
		{"encoding-bypass", "Base64/hex/rot13 obfuscation"},
		{"multi-turn-grooming", "Multi-turn context manipulation"},
		{"tool-abuse-escalation", "Tool use privilege escalation"},
		{"data-poisoning", "Training data corruption"},
		{"context-window-overflow", "Context window flooding"},
		{"indirect-injection", "Third-party content injection"},
		{"output-weaponization", "Response weaponization"},
	}

	matched := make(map[string]int)
	for _, ev := range im.securityEvents {
		if ev.EventType == "quarantine" || ev.EventType == "immune_scan" {
			if rule, ok := ev.Details["rule"].(string); ok {
				matched[rule]++
			}
		}
	}

	for _, sig := range signatures {
		count := matched[sig.name]
		arrow := lipgloss.NewStyle().Foreground(styles.TextMuted).Render("▸")
		name := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(sig.name)
		var countStr string
		if count > 0 {
			countStr = lipgloss.NewStyle().Foreground(styles.ColorAmber).
				Render(fmt.Sprintf("  matched: %d", count))
		} else {
			countStr = lipgloss.NewStyle().Foreground(styles.TextMuted).
				Render("  matched: 0")
		}
		lines = append(lines, fmt.Sprintf("  %s %s%s", arrow, name, countStr))
	}

	return strings.Join(lines, "\n")
}

func (im *ImmuneTheaterScreen) viewQuarantine(w int) string {
	var lines []string
	lines = append(lines, sectionHeader("QUARANTINE QUEUE", w))
	lines = append(lines, "")

	items := im.quarantineRecords()
	if len(items) == 0 {
		hint := "No items in quarantine. Malicious writes will appear here."
		if im.quarantine != nil && im.quarantine.Pending > 0 {
			hint = fmt.Sprintf("%d items pending — admin access required to view", im.quarantine.Pending)
		}
		lines = append(lines, styles.MutedStyle.Render("  "+hint))
		return strings.Join(lines, "\n")
	}

	for _, item := range items {
		ts := item.CreatedAt.Format("15:04:05")
		src := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).Render("src=" + item.Source)
		rule := lipgloss.NewStyle().Foreground(styles.ColorAmber).Render("rule=" + item.Rule)
		lines = append(lines, fmt.Sprintf("  %s  %s  %s", ts, src, rule))

		content := item.Content
		if len(content) > w-6 {
			content = content[:w-9] + "..."
		}
		lines = append(lines, fmt.Sprintf("    %s",
			lipgloss.NewStyle().Foreground(styles.TextMuted).Render(content)))

		statusColor := styles.TextMuted
		if item.Status == "pending" {
			statusColor = styles.ColorAmber
		} else if item.Status == "approved" {
			statusColor = styles.ColorGreen
		} else if item.Status == "rejected" {
			statusColor = styles.ColorRed
		}
		lines = append(lines, fmt.Sprintf("    ↳ %s",
			lipgloss.NewStyle().Foreground(statusColor).Render(item.Status)))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

func (im *ImmuneTheaterScreen) quarantineRecords() []api.QuarantineRecord {
	if im.quarantine == nil {
		return nil
	}
	return im.quarantine.Records
}

var _ Screen = (*ImmuneTheaterScreen)(nil)
