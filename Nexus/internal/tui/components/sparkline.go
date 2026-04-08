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

	"github.com/charmbracelet/lipgloss"
)

// SparkBlocks are the 8 block characters used for sparkline bars.
var SparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline renders a mini bar chart from a slice of values.
type Sparkline struct {
	Values []float64
	Width  int
	Color  lipgloss.Color
}

// View renders the sparkline.
func (s Sparkline) View() string {
	if len(s.Values) == 0 {
		return ""
	}

	maxVal := 0.0
	for _, v := range s.Values {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal == 0 {
		maxVal = 1
	}

	// Use the last `Width` values if we have more than we can display.
	values := s.Values
	w := s.Width
	if w <= 0 {
		w = 20
	}
	if len(values) > w {
		values = values[len(values)-w:]
	}

	var b strings.Builder
	for _, v := range values {
		idx := int((v / maxVal) * float64(len(SparkBlocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(SparkBlocks) {
			idx = len(SparkBlocks) - 1
		}
		b.WriteRune(SparkBlocks[idx])
	}

	return lipgloss.NewStyle().Foreground(s.Color).Render(b.String())
}
