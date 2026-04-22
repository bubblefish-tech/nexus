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

// Package pages contains the individual setup wizard pages.
package pages

import (
	"github.com/bubblefish-tech/nexus/internal/discover"
	tea "github.com/charmbracelet/bubbletea"
)

// WizardState accumulates user choices across all setup wizard pages.
type WizardState struct {
	// Page 1: deployment mode selection.
	Mode string // "simple", "balanced", "safe"

	// Page 2: environment scan results.
	DiscoveredTools []discover.DiscoveredTool
	ScanComplete    bool
	ScanErr         error

	// Page 3: feature selection (feature name → enabled).
	Features map[string]bool

	// Page 4: tool selection (tool name → selected).
	SelectedTools map[string]bool

	// Page 5: database selection.
	DatabaseType string // "sqlite", "postgres", "mysql", "cockroachdb", "mongodb", "firestore", "tidb", "turso"
	DatabaseDSN  string

	// Page 6: encryption password (empty = no encryption).
	EncryptionPass string

	// Page 7: tunnel configuration.
	TunnelEnabled  bool
	TunnelProvider string // "cloudflare", "ngrok", "tailscale", "bore", "custom"
	TunnelEndpoint string

	// Page 8: install directory.
	InstallDir string

	// ConfigDir is the pre-existing config directory used during scanning
	// (before the final install location is known).
	ConfigDir string
}

// WizardCompleteMsg is sent by the summary page when installation completes.
// The App receives this message and exits the wizard.
type WizardCompleteMsg struct {
	ConfigDir string
}

// AdvancePageMsg is sent by pages that auto-advance after selection.
type AdvancePageMsg struct{}

// Page is the interface all setup wizard pages implement.
type Page interface {
	// Init is called once when the page becomes active.
	Init(state *WizardState) tea.Cmd
	// Update handles terminal messages for this page.
	Update(msg tea.Msg, state *WizardState) (Page, tea.Cmd)
	// View renders the page content within the given dimensions.
	View(width, height int) string
	// Name returns the human-readable page title.
	Name() string
	// CanAdvance returns true when the user may proceed to the next page.
	CanAdvance(state *WizardState) bool
}
