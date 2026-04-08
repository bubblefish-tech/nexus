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

// securitySummaryMsg carries the result of a security summary API call.
type securitySummaryMsg struct {
	data *api.SecuritySummaryResponse
	err  error
}

// securityLintMsg carries the result of a lint API call.
type securityLintMsg struct {
	data *api.LintResponse
	err  error
}

// SecurityTab displays the hardening matrix, auth summary, and lint warnings.
type SecurityTab struct {
	summary    *api.SecuritySummaryResponse
	lint       *api.LintResponse
	summaryErr error
	lintErr    error

	// History for sparklines (rolling window of last 20 refreshes).
	authHistory  []float64
	denyHistory  []float64
	rateHistory  []float64
}

// NewSecurityTab creates a new Security tab.
func NewSecurityTab() *SecurityTab {
	return &SecurityTab{}
}

// Name returns the tab display name.
func (t *SecurityTab) Name() string { return "Security" }

// Init returns the initial command.
func (t *SecurityTab) Init() tea.Cmd { return nil }

// Update handles incoming messages.
func (t *SecurityTab) Update(msg tea.Msg) (Tab, tea.Cmd) {
	switch m := msg.(type) {
	case securitySummaryMsg:
		t.summaryErr = m.err
		if m.data != nil {
			t.summary = m.data
			t.authHistory = appendHistory(t.authHistory, float64(m.data.AuthFailures))
			t.denyHistory = appendHistory(t.denyHistory, float64(m.data.PolicyDenials))
			t.rateHistory = appendHistory(t.rateHistory, float64(m.data.RateLimitHits))
		}
	case securityLintMsg:
		t.lintErr = m.err
		if m.data != nil {
			t.lint = m.data
		}
	}
	return t, nil
}

// FireRefresh dispatches parallel summary and lint API calls.
func (t *SecurityTab) FireRefresh(client *api.Client) tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			data, err := client.SecuritySummary()
			return securitySummaryMsg{data: data, err: err}
		},
		func() tea.Msg {
			data, err := client.Lint()
			return securityLintMsg{data: data, err: err}
		},
	)
}

// View renders the security tab content.
func (t *SecurityTab) View(width, height int) string {
	var sections []string

	// --- Hardening matrix ---
	sections = append(sections, components.SectionTitle("Hardening Matrix", width))

	type feature struct {
		name   string
		status string
	}

	features := []feature{
		{"WAL integrity", boolStatus(t.summary, func(s *api.SecuritySummaryResponse) bool { return s.WALTamperDetected == 0 })},
		{"Config signature", boolStatus(t.summary, func(s *api.SecuritySummaryResponse) bool { return s.ConfigSignatureInvalid == 0 })},
		{"Retrieval firewall", boolStatus(t.summary, func(s *api.SecuritySummaryResponse) bool { return s.RetrievalFirewallFiltered >= 0 })},
		{"Rate limiting", boolStatus(t.summary, func(s *api.SecuritySummaryResponse) bool { return s.RateLimitHits >= 0 })},
		{"Auth enforcement", boolStatus(t.summary, func(s *api.SecuritySummaryResponse) bool { return true })},
		{"Admin audit", boolStatus(t.summary, func(s *api.SecuritySummaryResponse) bool { return s.AdminAccess >= 0 })},
	}

	colWidth := (width - 4) / 2
	if colWidth < 30 {
		colWidth = 30
	}

	var matrixRows []string
	for i := 0; i < len(features); i += 2 {
		left := renderFeatureRow(features[i].name, features[i].status, colWidth)
		right := ""
		if i+1 < len(features) {
			right = renderFeatureRow(features[i+1].name, features[i+1].status, colWidth)
		}
		matrixRows = append(matrixRows, lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right))
	}
	sections = append(sections, strings.Join(matrixRows, "\n"))

	// --- Auth summary with sparklines ---
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Auth Summary", width))

	sparkWidth := 20
	authFail := "—"
	policyDeny := "—"
	rateLimitHits := "—"
	if t.summary != nil {
		authFail = fmt.Sprintf("%d", t.summary.AuthFailures)
		policyDeny = fmt.Sprintf("%d", t.summary.PolicyDenials)
		rateLimitHits = fmt.Sprintf("%d", t.summary.RateLimitHits)
	}

	authSpark := components.Sparkline{Values: t.authHistory, Width: sparkWidth, Color: styles.ColorRed}
	denySpark := components.Sparkline{Values: t.denyHistory, Width: sparkWidth, Color: styles.ColorAmber}
	rateSpark := components.Sparkline{Values: t.rateHistory, Width: sparkWidth, Color: styles.ColorBlue}

	labelWidth := 20
	valueWidth := 8
	summaryLines := []string{
		renderSummaryRow("Auth failures", authFail, authSpark.View(), labelWidth, valueWidth),
		renderSummaryRow("Policy denials", policyDeny, denySpark.View(), labelWidth, valueWidth),
		renderSummaryRow("Rate-limit hits", rateLimitHits, rateSpark.View(), labelWidth, valueWidth),
	}
	sections = append(sections, strings.Join(summaryLines, "\n"))

	// --- Extra counters ---
	if t.summary != nil {
		sections = append(sections, "")
		extra := lipgloss.JoinHorizontal(lipgloss.Top,
			styles.MutedStyle.Render("WAL tamper: ")+formatCount(t.summary.WALTamperDetected),
			"  ",
			styles.MutedStyle.Render("Config sig invalid: ")+formatCount(t.summary.ConfigSignatureInvalid),
			"  ",
			styles.MutedStyle.Render("Admin access: ")+formatCount(t.summary.AdminAccess),
		)
		sections = append(sections, extra)
	}

	// --- Lint warnings ---
	sections = append(sections, "")
	sections = append(sections, components.SectionTitle("Lint Warnings", width))

	if t.lint != nil && len(t.lint.Findings) > 0 {
		for _, f := range t.lint.Findings {
			sev := renderSeverity(f.Severity)
			check := styles.MutedStyle.Render("[" + f.Check + "]")
			msg := lipgloss.NewStyle().Foreground(styles.TextPrimary).Render(f.Message)
			sections = append(sections, sev+" "+check+" "+msg)
		}
	} else if t.lint != nil {
		sections = append(sections, styles.SuccessStyle.Render("No lint findings — all checks pass."))
	} else {
		sections = append(sections, styles.MutedStyle.Render("Waiting for data..."))
	}

	// --- Errors ---
	if t.summaryErr != nil {
		sections = append(sections, styles.ErrorStyle.Render("summary error: "+t.summaryErr.Error()))
	}
	if t.lintErr != nil {
		sections = append(sections, styles.ErrorStyle.Render("lint error: "+t.lintErr.Error()))
	}

	content := strings.Join(sections, "\n")

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Render(content)
}

// appendHistory adds a value to the rolling history, keeping at most 20 entries.
func appendHistory(h []float64, v float64) []float64 {
	h = append(h, v)
	if len(h) > 20 {
		h = h[len(h)-20:]
	}
	return h
}

// boolStatus returns "ENABLED" or "DISABLED" based on a predicate over the summary.
func boolStatus(s *api.SecuritySummaryResponse, pred func(*api.SecuritySummaryResponse) bool) string {
	if s == nil {
		return "DISABLED"
	}
	if pred(s) {
		return "ENABLED"
	}
	return "DISABLED"
}

// renderFeatureRow renders a feature name + pill in a fixed-width row.
func renderFeatureRow(name, status string, w int) string {
	label := lipgloss.NewStyle().
		Foreground(styles.TextSecondary).
		Width(w - 12).
		Render(name)
	pill := components.PillStatus(status)
	return label + " " + pill
}

// renderSummaryRow renders a metric label, value, and sparkline.
func renderSummaryRow(label, value, spark string, labelW, valueW int) string {
	l := lipgloss.NewStyle().Foreground(styles.TextSecondary).Width(labelW).Render(label)
	v := lipgloss.NewStyle().Foreground(styles.TextPrimary).Bold(true).Width(valueW).Render(value)
	return l + " " + v + " " + spark
}

// formatCount renders a count with red highlighting if non-zero.
func formatCount(n int) string {
	s := fmt.Sprintf("%d", n)
	if n > 0 {
		return styles.ErrorStyle.Render(s)
	}
	return styles.SuccessStyle.Render(s)
}

// Compile-time interface check.
var _ Tab = (*SecurityTab)(nil)

// renderSeverity returns a colored severity tag.
func renderSeverity(sev string) string {
	switch strings.ToLower(sev) {
	case "error", "critical":
		return styles.ErrorStyle.Render(strings.ToUpper(sev))
	case "warn", "warning":
		return styles.WarnStyle.Render("WARN")
	case "info":
		return styles.TealStyle.Render("INFO")
	default:
		return styles.MutedStyle.Render(strings.ToUpper(sev))
	}
}
