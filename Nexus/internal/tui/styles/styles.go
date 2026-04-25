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

// Package styles defines all lipgloss colors and styles for the TUI.
// No lipgloss.Color() calls should appear anywhere else.
package styles

import "github.com/charmbracelet/lipgloss"

// Background layers.
var (
	BgBase  = lipgloss.Color("#0a0e14")
	BgPanel = lipgloss.Color("#0d1219")
	BgRow   = lipgloss.Color("#111820")
	BgHover = lipgloss.Color("#151d27")
)

// Borders.
var (
	BorderBase  = lipgloss.Color("#1e2d3d")
	BorderHover = lipgloss.Color("#2d4055")
	BorderFocus = lipgloss.Color("#00b4d8")
)

// Text.
var (
	TextPrimary   = lipgloss.Color("#cdd9e5")
	TextSecondary = lipgloss.Color("#8b9db0")
	TextMuted     = lipgloss.Color("#4a6072")
	TextDim       = lipgloss.Color("#2d4055")
)

// Semantic colors.
var (
	ColorGreen  = lipgloss.Color("#3dd68c")
	ColorTeal   = lipgloss.Color("#00b4d8")
	ColorBlue   = lipgloss.Color("#4d9de0")
	ColorAmber  = lipgloss.Color("#f0a500")
	ColorRed    = lipgloss.Color("#e05555")
	ColorPurple = lipgloss.Color("#a78bfa")
	ColorGray   = lipgloss.Color("#4a6072")
)

// Extended DeepOcean palette (spec v2).
var (
	ColorCyan      = lipgloss.Color("#00ffff")
	ColorTealDim   = lipgloss.Color("#005f73")
	ColorGreenDim  = lipgloss.Color("#166534")
	ColorPurpleViv = lipgloss.Color("#7c3aed")
	ColorPurpleDim = lipgloss.Color("#4c1d95")
	ColorMagenta   = lipgloss.Color("#c026d3")
	BgPanelAlt     = lipgloss.Color("#141c28")
	BorderStrong   = lipgloss.Color("#1a2540")
	TextWhite      = lipgloss.Color("#e2e8f0")
	TextWhiteDim   = lipgloss.Color("#94a3b8")
)

// Heat grid gradient.
var (
	HeatLow    = lipgloss.Color("#1a4a2e")
	HeatMedLow = lipgloss.Color("#2d7a4a")
	HeatMedHi  = lipgloss.Color("#3dd68c")
	HeatHigh   = lipgloss.Color("#5fffb1")
)

// Contrast foreground for colored pill/badge backgrounds.
var TextContrast = lipgloss.Color("#ffffff")

// Computed styles.
var (
	ActiveTab = lipgloss.NewStyle().
			Foreground(ColorTeal).
			Bold(true)

	InactiveTab = lipgloss.NewStyle().
			Foreground(TextMuted)

	PanelBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(BorderBase).
			Background(BgPanel)

	PanelBorderFocus = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(BorderFocus).
				Background(BgPanel)

	StatValue = lipgloss.NewStyle().
			Bold(true).
			Foreground(TextPrimary)

	StatLabel = lipgloss.NewStyle().
			Foreground(TextSecondary)

	StatSublabel = lipgloss.NewStyle().
			Foreground(TextMuted)

	SectionHeader = lipgloss.NewStyle().
			Foreground(TextMuted).
			Bold(true)

	StatusBarStyle = lipgloss.NewStyle().
			Foreground(TextSecondary).
			Background(BgRow)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorRed).
			Bold(true)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorGreen)

	WarnStyle = lipgloss.NewStyle().
			Foreground(ColorAmber)

	MutedStyle = lipgloss.NewStyle().
			Foreground(TextMuted)

	DimStyle = lipgloss.NewStyle().
			Foreground(TextDim)

	PrimaryStyle = lipgloss.NewStyle().
			Foreground(TextPrimary)

	TealStyle = lipgloss.NewStyle().
			Foreground(ColorTeal)

	PurpleStyle = lipgloss.NewStyle().
			Foreground(ColorPurple)
)
