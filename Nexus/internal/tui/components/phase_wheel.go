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
	"math/rand"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/tui/styles"
	"github.com/charmbracelet/lipgloss"
)

// KuramotoSim simulates N coupled oscillators via the Kuramoto ODE.
type KuramotoSim struct {
	N        int
	Phases   []float64
	Omegas   []float64
	Coupling float64
	DT       float64
}

// NewKuramotoSim creates a new Kuramoto simulator with N oscillators.
func NewKuramotoSim(n int, couplingK float64) *KuramotoSim {
	rng := rand.New(rand.NewSource(42))
	ks := &KuramotoSim{
		N: n, Coupling: couplingK, DT: 0.05,
		Phases: make([]float64, n),
		Omegas: make([]float64, n),
	}
	for i := 0; i < n; i++ {
		ks.Phases[i] = rng.Float64() * 2 * math.Pi
		ks.Omegas[i] = 1.0 + rng.NormFloat64()*0.2
	}
	return ks
}

// Step advances phases by dt via the Kuramoto ODE.
func (ks *KuramotoSim) Step() {
	newPhases := make([]float64, ks.N)
	for i := 0; i < ks.N; i++ {
		sum := 0.0
		for j := 0; j < ks.N; j++ {
			sum += math.Sin(ks.Phases[j] - ks.Phases[i])
		}
		newPhases[i] = math.Mod(ks.Phases[i]+(ks.Omegas[i]+ks.Coupling/float64(ks.N)*sum)*ks.DT, 2*math.Pi)
		if newPhases[i] < 0 {
			newPhases[i] += 2 * math.Pi
		}
	}
	ks.Phases = newPhases
}

// OrderParameter returns R (synchronization magnitude) and Psi (mean angle).
func (ks *KuramotoSim) OrderParameter() (R, Psi float64) {
	var sumCos, sumSin float64
	for _, p := range ks.Phases {
		sumCos += math.Cos(p)
		sumSin += math.Sin(p)
	}
	R = math.Sqrt(sumCos*sumCos+sumSin*sumSin) / float64(ks.N)
	Psi = math.Atan2(sumSin, sumCos)
	return
}

// PhaseWheelProps configures the Kuramoto phase wheel renderer.
type PhaseWheelProps struct {
	Phases    []float64
	R         float64
	Psi       float64
	Width     int
	Height    int
	Synthetic bool
}

// RenderPhaseWheel draws an ASCII phase wheel with oscillator dots.
func RenderPhaseWheel(p PhaseWheelProps) string {
	if p.Width < 20 || p.Height < 8 {
		return lipgloss.NewStyle().Foreground(styles.TextMuted).
			Render(fmt.Sprintf("R = %.2f (synthetic)", p.R))
	}

	cx := p.Width / 2
	cy := p.Height / 2
	rx := float64(p.Width) / 2.5
	ry := float64(p.Height) / 2.5

	grid := make([][]rune, p.Height)
	for y := range grid {
		grid[y] = make([]rune, p.Width)
		for x := range grid[y] {
			grid[y][x] = ' '
		}
	}

	// Draw circle outline
	for a := 0.0; a < 2*math.Pi; a += 0.05 {
		x := cx + int(math.Round(rx*math.Cos(a)))
		y := cy + int(math.Round(ry*math.Sin(a)))
		if x >= 0 && x < p.Width && y >= 0 && y < p.Height {
			grid[y][x] = '·'
		}
	}

	// Draw phase dots
	for _, phase := range p.Phases {
		x := cx + int(math.Round(rx*math.Cos(phase)))
		y := cy + int(math.Round(ry*math.Sin(phase)))
		if x >= 0 && x < p.Width && y >= 0 && y < p.Height {
			grid[y][x] = '●'
		}
	}

	// Draw mean vector arrow
	arrowLen := p.R * rx * 0.6
	for t := 0.0; t < arrowLen; t += 0.5 {
		x := cx + int(math.Round(t*math.Cos(p.Psi)))
		y := cy + int(math.Round(t*math.Sin(p.Psi)*0.5))
		if x >= 0 && x < p.Width && y >= 0 && y < p.Height {
			grid[y][x] = '→'
		}
	}

	var lines []string
	for _, row := range grid {
		lines = append(lines, string(row))
	}

	label := fmt.Sprintf("  R = %.2f  Ψ = %.2f rad  N = %d", p.R, p.Psi, len(p.Phases))
	if p.Synthetic {
		label += "  [SYNTHETIC]"
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(styles.TextMuted).Render(label))

	return strings.Join(lines, "\n")
}
