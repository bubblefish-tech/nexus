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

package pages

import (
	"fmt"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// featureEntry defines an optional Nexus feature shown on the features page.
type featureEntry struct {
	key     string
	label   string
	desc    string
	safeOn  bool // enabled by default in "safe" mode
	simOn   bool // enabled by default in "simple" mode
}

var allFeatures = []featureEntry{
	{"embedding", "Semantic Embedding", "Enable vector search via an embedding provider", false, false},
	{"mcp", "MCP Server", "Expose memories via the Model Context Protocol (port 7474)", true, true},
	{"a2a", "A2A Protocol", "Agent-to-agent communication and registry", false, false},
	{"control", "Control Plane", "Grants, approvals, tasks, and policy engine", false, false},
	{"substrate", "BF-Sketch Substrate", "Forward-secure sketch + cryptographic deletion proofs", false, false},
	{"ingest", "Sentinel Ingest", "Auto-ingest conversations from Claude Code, Cursor, LM Studio", true, false},
	{"audit", "Audit Chain", "Hash-chained, Ed25519-signed audit log", true, true},
	{"dashboard", "Web Dashboard", "Read-only monitoring UI at port 8081", true, true},
}

// FeaturesPage presents a checkbox list of optional features.
type FeaturesPage struct {
	cursor int
}

var _ Page = (*FeaturesPage)(nil)

// NewFeaturesPage returns a FeaturesPage.
func NewFeaturesPage() *FeaturesPage { return &FeaturesPage{} }

func (p *FeaturesPage) Name() string { return "Feature Selection" }

func (p *FeaturesPage) Init(state *WizardState) tea.Cmd {
	if state.Features == nil {
		state.Features = make(map[string]bool)
		// Set mode-based defaults.
		for _, f := range allFeatures {
			switch state.Mode {
			case "simple":
				state.Features[f.key] = f.simOn
			case "safe":
				state.Features[f.key] = f.safeOn
			default: // "balanced"
				state.Features[f.key] = f.safeOn // same as safe for balanced
			}
		}
	}
	return nil
}

func (p *FeaturesPage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	if state.Features == nil {
		state.Features = make(map[string]bool)
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(allFeatures)-1 {
				p.cursor++
			}
		case " ":
			key := allFeatures[p.cursor].key
			state.Features[key] = !state.Features[key]
		}
	}
	return p, nil
}

func (p *FeaturesPage) CanAdvance(_ *WizardState) bool { return true }

func (p *FeaturesPage) View(width, height int) string {
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Select features to enable")
	b.WriteString(title + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Space to toggle  ·  ↑/↓ or j/k to navigate") + "\n\n")

	for i, f := range allFeatures {
		cursor := "  "
		rowStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		if i == p.cursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			rowStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
		}
		check := "[ ]"
		checkStyle := lipgloss.NewStyle().Foreground(styles.TextMuted)
		// We can't read state here (View doesn't have it), show placeholder
		_ = f
		b.WriteString(fmt.Sprintf("%s%s %s\n",
			cursor,
			checkStyle.Render(check),
			rowStyle.Render(f.label),
		))
		b.WriteString(fmt.Sprintf("      %s\n",
			lipgloss.NewStyle().Foreground(styles.TextMuted).Render(f.desc)))
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}

// ViewWithState renders with actual checkbox state.
func (p *FeaturesPage) ViewWithState(width, height int, state *WizardState) string {
	var b strings.Builder
	title := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render("Select features to enable")
	b.WriteString(title + "\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(styles.TextMuted).
		Render("Space to toggle  ·  ↑/↓ or j/k to navigate") + "\n\n")

	feats := state.Features
	if feats == nil {
		feats = make(map[string]bool)
	}

	for i, f := range allFeatures {
		cursor := "  "
		rowStyle := lipgloss.NewStyle().Foreground(styles.TextSecondary)
		if i == p.cursor {
			cursor = lipgloss.NewStyle().Foreground(styles.ColorTeal).Render("▶ ")
			rowStyle = lipgloss.NewStyle().Foreground(styles.TextPrimary)
		}
		var check string
		if feats[f.key] {
			check = lipgloss.NewStyle().Foreground(styles.ColorGreen).Render("[✓]")
		} else {
			check = lipgloss.NewStyle().Foreground(styles.TextMuted).Render("[ ]")
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n",
			cursor, check, rowStyle.Render(f.label)))
		b.WriteString(fmt.Sprintf("      %s\n",
			lipgloss.NewStyle().Foreground(styles.TextMuted).Render(f.desc)))
	}
	return lipgloss.NewStyle().Width(width).Render(b.String())
}
