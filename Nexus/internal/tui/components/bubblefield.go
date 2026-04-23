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
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"
)

var bubbleChars = []rune{'·', '∘', '○', '◯'}

// Bubble is a single rising bubble in the background field.
type Bubble struct {
	x, y     float64
	char     rune
	spring   harmonica.Spring
	velocity float64
	phase    float64 // sinusoidal x-drift
}

// BubbleField is a physics-driven rising-bubble background layer.
// Bubbles spawn at the bottom and float upward via harmonica springs.
type BubbleField struct {
	Bubbles []Bubble
	Width   int
	Height  int
	rng     *rand.Rand
}

// NewBubbleField creates a field with n bubbles.
func NewBubbleField(width, height, count int) *BubbleField {
	bf := &BubbleField{
		Width:  width,
		Height: height,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	bf.Bubbles = make([]Bubble, count)
	for i := range bf.Bubbles {
		bf.Bubbles[i] = bf.spawnBubble(true)
	}
	return bf
}

func (bf *BubbleField) spawnBubble(randomY bool) Bubble {
	y := float64(bf.Height + 2)
	if randomY {
		y = bf.rng.Float64() * float64(bf.Height)
	}
	return Bubble{
		x:      bf.rng.Float64() * float64(bf.Width),
		y:      y,
		char:   bubbleChars[bf.rng.Intn(len(bubbleChars))],
		spring: harmonica.NewSpring(harmonica.FPS(30), 0.4, 0.3),
		phase:  bf.rng.Float64() * 2 * math.Pi,
	}
}

// SetSize updates the field dimensions and redistributes all bubbles
// evenly across the new area.
func (bf *BubbleField) SetSize(w, h int) {
	bf.Width = w
	bf.Height = h
	for i := range bf.Bubbles {
		bf.Bubbles[i] = bf.spawnBubble(true)
	}
}

// Tick advances the simulation by dt.
func (bf *BubbleField) Tick(dt time.Duration) {
	dtSec := dt.Seconds()
	for i := range bf.Bubbles {
		b := &bf.Bubbles[i]
		b.y, b.velocity = b.spring.Update(b.y, b.velocity, -2)
		b.phase += dtSec * 0.8
		if b.y < -1 {
			bf.Bubbles[i] = bf.spawnBubble(false)
		}
	}
}

// Render draws the bubble field as a string grid.
// Returns a multi-line string where bubble positions are marked.
func (bf *BubbleField) Render() string {
	if bf.Width < 1 || bf.Height < 1 {
		return ""
	}

	grid := make([][]rune, bf.Height)
	for y := range grid {
		grid[y] = make([]rune, bf.Width)
		for x := range grid[y] {
			grid[y][x] = ' '
		}
	}

	for _, b := range bf.Bubbles {
		drift := math.Sin(b.phase) * 3
		bx := int(b.x + drift)
		by := int(b.y)
		if bx >= 0 && bx < bf.Width && by >= 0 && by < bf.Height {
			grid[by][bx] = b.char
		}
	}

	style := lipgloss.NewStyle().Foreground(styles.ColorTealDim)
	var lines []string
	for _, row := range grid {
		lines = append(lines, style.Render(string(row)))
	}
	return strings.Join(lines, "\n")
}
