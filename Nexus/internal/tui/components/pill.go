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

package components

import (
	"strings"

	"github.com/BubbleFish-Nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// PillStatus renders a colored status pill.
func PillStatus(status string) string {
	label := strings.ToUpper(status)

	var fg, bg lipgloss.Color
	switch strings.ToLower(status) {
	case "live", "ok", "enabled", "running", "ready", "pass":
		fg = lipgloss.Color("#0a0e14")
		bg = styles.ColorGreen
	case "idle", "partial":
		fg = lipgloss.Color("#0a0e14")
		bg = styles.ColorBlue
	case "warn", "degraded":
		fg = lipgloss.Color("#0a0e14")
		bg = styles.ColorAmber
	case "dead", "err", "error", "critical", "disabled", "fail":
		fg = lipgloss.Color("#0a0e14")
		bg = styles.ColorRed
	default:
		fg = styles.TextSecondary
		bg = styles.BgRow
	}

	return lipgloss.NewStyle().
		Foreground(fg).
		Background(bg).
		Padding(0, 1).
		Bold(true).
		Render(label)
}
