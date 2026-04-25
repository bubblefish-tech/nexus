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
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var crypticRunes = []rune{'□', '■', '▣', '▢', '◈', '◇', '◆', '▪', '▫', '●', '○', '◐', '◑', '◒', '◓', '◔', '◕', '◖', '◗'}

// CrypticSpinnerProps configures the gradient cryptic-character spinner.
type CrypticSpinnerProps struct {
	Size       int
	Label      string
	Frame      int
	ColorA     lipgloss.Color
	ColorB     lipgloss.Color
	LabelColor lipgloss.Color
}

// RenderCrypticSpinner returns a single line: [runes] label
func RenderCrypticSpinner(p CrypticSpinnerProps) string {
	if p.Size < 1 {
		p.Size = 15
	}
	chars := make([]string, p.Size)
	for i := 0; i < p.Size; i++ {
		idx := (p.Frame*7 + i*13) % len(crypticRunes)
		ch := string(crypticRunes[idx])
		t := (math.Sin(float64(p.Frame)*0.1+float64(i)*0.3) + 1) / 2
		blend := t*float64(i)/float64(p.Size) + (1-t)*float64(p.Size-i)/float64(p.Size)
		c := lerpColor(p.ColorA, p.ColorB, blend)
		chars[i] = lipgloss.NewStyle().Foreground(c).Render(ch)
	}
	spinner := strings.Join(chars, "")
	labelStyled := lipgloss.NewStyle().Foreground(p.LabelColor).Render(p.Label)
	return spinner + "  " + labelStyled
}

func lerpColor(a, b lipgloss.Color, t float64) lipgloss.Color {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	ar, ag, ab := parseHex(string(a))
	br, bg, bb := parseHex(string(b))
	rr := int(float64(ar)*(1-t) + float64(br)*t)
	gg := int(float64(ag)*(1-t) + float64(bg)*t)
	bv := int(float64(ab)*(1-t) + float64(bb)*t)
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", rr, gg, bv))
}

func parseHex(s string) (r, g, b int) {
	if len(s) < 7 || s[0] != '#' {
		return 128, 128, 128
	}
	fmt.Sscanf(s[1:], "%02x%02x%02x", &r, &g, &b)
	return
}
