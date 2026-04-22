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

package styles

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestColorConstants_NonEmpty(t *testing.T) {
	t.Helper()
	colors := map[string]lipgloss.Color{
		"ColorGreen":  ColorGreen,
		"ColorTeal":   ColorTeal,
		"ColorBlue":   ColorBlue,
		"ColorAmber":  ColorAmber,
		"ColorRed":    ColorRed,
		"ColorPurple": ColorPurple,
		"TextPrimary": TextPrimary,
		"TextMuted":   TextMuted,
		"BgBase":      BgBase,
		"BorderBase":  BorderBase,
	}
	for name, c := range colors {
		if string(c) == "" {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestPrecomputedStyles_Render(t *testing.T) {
	t.Helper()
	styles := map[string]lipgloss.Style{
		"ActiveTab":      ActiveTab,
		"InactiveTab":    InactiveTab,
		"StatValue":      StatValue,
		"ErrorStyle":     ErrorStyle,
		"SuccessStyle":   SuccessStyle,
		"WarnStyle":      WarnStyle,
		"MutedStyle":     MutedStyle,
		"TealStyle":      TealStyle,
		"SectionHeader":  SectionHeader,
	}
	for name, s := range styles {
		out := s.Render("test")
		if out == "" {
			t.Errorf("%s.Render produced empty output", name)
		}
	}
}
