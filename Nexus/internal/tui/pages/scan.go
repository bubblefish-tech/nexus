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
	"context"
	"fmt"
	"log/slog"

	"github.com/bubblefish-tech/nexus/internal/discover"
	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// scanCompleteMsg carries the result of a full environment scan.
type scanCompleteMsg struct {
	tools []discover.DiscoveredTool
	err   error
}

// spinnerFrames are the frames for the indeterminate scan animation.
var spinnerFrames = []string{"⠋", "⠙", "⠸", "⠴", "⠦", "⠇"}

// scanTickMsg advances the spinner.
type scanTickMsg struct{}

// ScanPage runs the five-tier environment scanner and shows discovered tools.
type ScanPage struct {
	frame   int
	started bool
}

var _ Page = (*ScanPage)(nil)

// NewScanPage returns a ScanPage.
func NewScanPage() *ScanPage { return &ScanPage{} }

func (p *ScanPage) Name() string { return "Environment Scan" }

func (p *ScanPage) Init(state *WizardState) tea.Cmd {
	if state.ScanComplete {
		return nil
	}
	p.started = true
	configDir := state.ConfigDir
	return func() tea.Msg {
		logger := slog.Default()
		scanner := discover.NewScanner(configDir, logger)
		tools, err := scanner.FullScan(context.Background())
		return scanCompleteMsg{tools: tools, err: err}
	}
}

func (p *ScanPage) Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd) {
	switch msg := msg.(type) {
	case scanCompleteMsg:
		state.DiscoveredTools = msg.tools
		state.ScanComplete = true
		state.ScanErr = msg.err
		return p, nil
	case scanTickMsg:
		p.frame = (p.frame + 1) % len(spinnerFrames)
		if !state.ScanComplete {
			return p, func() tea.Msg { return scanTickMsg{} }
		}
		return p, nil
	}
	return p, nil
}

func (p *ScanPage) CanAdvance(state *WizardState) bool { return state.ScanComplete }

func (p *ScanPage) View(width, height int) string {
	// This view is rendered by the parent wizard — state is not available here.
	// The wizard passes the state in Update; View relies on cached fields only.
	// We show the spinner or the result count based on whether the scan completed.
	// Since View doesn't receive state, we use p.started as a proxy.
	if !p.started {
		return lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render("Preparing scan…")
	}
	frame := spinnerFrames[p.frame%len(spinnerFrames)]
	return lipgloss.NewStyle().Foreground(styles.ColorTeal).
		Render(fmt.Sprintf("%s  Scanning environment…", frame))
}

// ViewWithState renders using the latest WizardState — called from the wizard view.
func (p *ScanPage) ViewWithState(width, height int, state *WizardState) string {
	if state.ScanErr != nil {
		return lipgloss.NewStyle().Foreground(styles.ColorRed).
			Render(fmt.Sprintf("Scan error: %v\n\nPress Ctrl+N to continue anyway.", state.ScanErr))
	}
	if state.ScanComplete {
		found := len(state.DiscoveredTools)
		msg := fmt.Sprintf("✓  Scan complete — %d tool(s) found.", found)
		style := lipgloss.NewStyle().Foreground(styles.ColorGreen).Bold(true)
		result := style.Render(msg) + "\n\n"
		for _, t := range state.DiscoveredTools {
			result += lipgloss.NewStyle().Foreground(styles.TextSecondary).
				Render(fmt.Sprintf("  • %s  (%s)", t.Name, t.DetectionMethod)) + "\n"
		}
		return lipgloss.NewStyle().Width(width).Render(result)
	}
	frame := spinnerFrames[p.frame%len(spinnerFrames)]
	return lipgloss.NewStyle().Foreground(styles.ColorTeal).
		Render(fmt.Sprintf("%s  Scanning environment for AI tools…", frame))
}
