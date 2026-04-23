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

import "github.com/charmbracelet/lipgloss"

// Theme defines the complete color palette for the TUI.
// Extracted pixel-exact from the BubbleFish logo.
type Theme struct {
	BG         lipgloss.Color
	BGPanel    lipgloss.Color
	BGPanelAlt lipgloss.Color
	Border     lipgloss.Color

	Teal    lipgloss.Color
	TealDim lipgloss.Color
	Cyan    lipgloss.Color

	Green    lipgloss.Color
	GreenDim lipgloss.Color

	Purple    lipgloss.Color
	PurpleDim lipgloss.Color
	Magenta   lipgloss.Color

	Amber lipgloss.Color
	Red   lipgloss.Color

	Muted    lipgloss.Color
	Dim      lipgloss.Color
	White    lipgloss.Color
	WhiteDim lipgloss.Color

	Accent lipgloss.Color
}

// DeepOcean is the default theme — BubbleFish logo colors.
var DeepOcean = Theme{
	BG:         "#0a0e14",
	BGPanel:    "#0f1520",
	BGPanelAlt: "#141c28",
	Border:     "#1a2540",
	Teal:       "#00b4d8",
	TealDim:    "#005f73",
	Cyan:       "#00ffff",
	Green:      "#3dd68c",
	GreenDim:   "#166534",
	Purple:     "#7c3aed",
	PurpleDim:  "#4c1d95",
	Magenta:    "#c026d3",
	Amber:      "#f59e0b",
	Red:        "#ef4444",
	Muted:      "#4a5568",
	Dim:        "#2d3748",
	White:      "#e2e8f0",
	WhiteDim:   "#94a3b8",
	Accent:     "#00b4d8",
}

// Phosphor is a green-on-black CRT aesthetic.
var Phosphor = Theme{
	BG:         "#0a0a0a",
	BGPanel:    "#0f140f",
	BGPanelAlt: "#141e14",
	Border:     "#1a3a1a",
	Teal:       "#33ff33",
	TealDim:    "#1a661a",
	Cyan:       "#66ff66",
	Green:      "#33ff33",
	GreenDim:   "#1a661a",
	Purple:     "#66cc66",
	PurpleDim:  "#336633",
	Magenta:    "#99ff99",
	Amber:      "#ccff33",
	Red:        "#ff3333",
	Muted:      "#335533",
	Dim:        "#1a331a",
	White:      "#ccffcc",
	WhiteDim:   "#669966",
	Accent:     "#33ff33",
}

// Amber is an IBM 3279 amber terminal aesthetic.
var Amber = Theme{
	BG:         "#1a0e00",
	BGPanel:    "#201400",
	BGPanelAlt: "#281c00",
	Border:     "#3d2800",
	Teal:       "#ffb000",
	TealDim:    "#805800",
	Cyan:       "#ffc833",
	Green:      "#ffb000",
	GreenDim:   "#805800",
	Purple:     "#cc8800",
	PurpleDim:  "#664400",
	Magenta:    "#ffcc33",
	Amber:      "#ffb000",
	Red:        "#ff6633",
	Muted:      "#664400",
	Dim:        "#332200",
	White:      "#ffe0a0",
	WhiteDim:   "#b38000",
	Accent:     "#ffb000",
}

// Midnight is deeper blacks with colder blues.
var Midnight = Theme{
	BG:         "#060810",
	BGPanel:    "#0a0e18",
	BGPanelAlt: "#0e1422",
	Border:     "#141e36",
	Teal:       "#4d9de0",
	TealDim:    "#264d70",
	Cyan:       "#66ccff",
	Green:      "#3dd68c",
	GreenDim:   "#166534",
	Purple:     "#8b5cf6",
	PurpleDim:  "#4c1d95",
	Magenta:    "#a78bfa",
	Amber:      "#f59e0b",
	Red:        "#ef4444",
	Muted:      "#334455",
	Dim:        "#1a2a3a",
	White:      "#d0d8e8",
	WhiteDim:   "#7088a0",
	Accent:     "#4d9de0",
}

// ActiveTheme is the currently active theme. Defaults to DeepOcean.
var ActiveTheme = DeepOcean

// ThemeByName returns the theme matching the given name.
func ThemeByName(name string) (Theme, bool) {
	switch name {
	case "deepocean", "deep-ocean", "default":
		return DeepOcean, true
	case "phosphor", "green":
		return Phosphor, true
	case "amber":
		return Amber, true
	case "midnight":
		return Midnight, true
	default:
		return Theme{}, false
	}
}
