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

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// DemoStep defines one step in the scripted demo sequence.
type DemoStep struct {
	Label       string
	Narration   string
	Navigate    AppState
	MinDuration time.Duration
}

var demoScript = []DemoStep{
	{Label: "Dashboard overview", Narration: "Welcome to Nexus — the governed AI cryptographic substrate.", Navigate: StateDashboard, MinDuration: 5 * time.Second},
	{Label: "Browse memories", Narration: "The Memory Browser shows all stored memories with provenance.", Navigate: StateMemoryBrowser, MinDuration: 4 * time.Second},
	{Label: "Retrieval cascade", Narration: "Watch 6 retrieval stages execute — policy, cache, SQL, BM25, vector, merge.", Navigate: StateRetrievalTheater, MinDuration: 8 * time.Second},
	{Label: "Audit chain", Narration: "Every write creates a hash-chained, Ed25519-signed audit entry.", Navigate: StateAuditWalker, MinDuration: 6 * time.Second},
	{Label: "Agent orchestration", Narration: "A2A agents register, exchange tasks, and respect policy gates.", Navigate: StateAgentCanvas, MinDuration: 6 * time.Second},
	{Label: "Crypto vault", Narration: "Master key, audit signing, forward-secure ratchet. AES-256-GCM + Ed25519.", Navigate: StateCryptoVault, MinDuration: 8 * time.Second},
	{Label: "Governance", Narration: "Grants, approvals, and tasks — the control plane for multi-agent policy.", Navigate: StateGovernance, MinDuration: 5 * time.Second},
	{Label: "Immune system", Narration: "Tier-0 signatures intercept prompt injection, credential exfil, role manipulation.", Navigate: StateImmuneTheater, MinDuration: 6 * time.Second},
	{Label: "Complete", Narration: "Every write was captured, cryptographically attested, and streamed.", Navigate: StateDashboard, MinDuration: 3 * time.Second},
}

// DemoModel manages the scripted demo sequence state.
type DemoModel struct {
	Active    bool
	StepIndex int
	StepStart time.Time
	Width     int
}

// DemoAdvanceMsg signals the demo should advance to the next step.
type DemoAdvanceMsg struct{}

// DemoCompleteMsg signals the demo has finished all steps.
type DemoCompleteMsg struct{}

// StartDemo initializes the demo model.
func (d *DemoModel) StartDemo() {
	d.Active = true
	d.StepIndex = 0
	d.StepStart = time.Now()
}

// CurrentStep returns the current demo step.
func (d *DemoModel) CurrentStep() *DemoStep {
	if d.StepIndex >= len(demoScript) {
		return nil
	}
	return &demoScript[d.StepIndex]
}

// Advance moves to the next step or completes.
func (d *DemoModel) Advance() bool {
	d.StepIndex++
	d.StepStart = time.Now()
	return d.StepIndex < len(demoScript)
}

// ShouldAdvance returns true if the current step's minimum duration has elapsed.
func (d *DemoModel) ShouldAdvance() bool {
	step := d.CurrentStep()
	if step == nil {
		return true
	}
	return time.Since(d.StepStart) >= step.MinDuration
}

// View renders the narration panel at the bottom of the screen.
func (d *DemoModel) View() string {
	if !d.Active {
		return ""
	}
	step := d.CurrentStep()
	if step == nil {
		return ""
	}

	w := d.Width
	if w < 60 {
		w = 60
	}

	elapsed := time.Since(d.StepStart)
	progress := float64(elapsed) / float64(step.MinDuration)
	if progress > 1 {
		progress = 1
	}

	barW := w - 40
	if barW < 10 {
		barW = 10
	}
	filled := int(progress * float64(barW))
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", barW-filled)

	header := lipgloss.NewStyle().Foreground(styles.ColorTeal).Bold(true).
		Render(fmt.Sprintf(" DEMO MODE · Step %d of %d", d.StepIndex+1, len(demoScript)))

	narration := lipgloss.NewStyle().Foreground(styles.TextWhiteDim).
		Render(" " + step.Narration)

	progressLine := lipgloss.NewStyle().Foreground(styles.ColorTeal).Render(" "+bar) +
		lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render(fmt.Sprintf("  [%ds]     [Esc] abort", int(step.MinDuration.Seconds())))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorTeal).
		Width(w - 2)

	return box.Render(strings.Join([]string{header, narration, progressLine}, "\n"))
}
