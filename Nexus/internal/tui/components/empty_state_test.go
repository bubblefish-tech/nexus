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
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func baseOpts(kind EmptyStateKind, w, h int) EmptyStateOptions {
	return EmptyStateOptions{
		Kind:   kind,
		Width:  w,
		Height: h,
		BorderStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#333")),
		MutedColor: lipgloss.Color("#555"),
		WhiteDim:   lipgloss.Color("#aaa"),
		Amber:      lipgloss.Color("#f90"),
		Teal:       lipgloss.Color("#0bd"),
		Hint:       "Test hint",
		Frame:      3,
	}
}

var httpDigitsRe = regexp.MustCompile(`\bHTTP [0-9]{3}\b`)

func TestRender_AllKinds(t *testing.T) {
	t.Helper()
	kinds := []struct {
		kind  EmptyStateKind
		name  string
		title string
	}{
		{EmptyStateLoading, "loading", "Loading"},
		{EmptyStateNoData, "nodata", "No data yet."},
		{EmptyStateDisconnected, "disconnected", "Daemon unreachable"},
		{EmptyStateFeatureGated, "gated", "Feature gated"},
	}
	widths := []int{60, 120, 200}
	heights := []int{10, 20}

	for _, k := range kinds {
		for _, w := range widths {
			for _, h := range heights {
				t.Run(k.name, func(t *testing.T) {
					result := Render(baseOpts(k.kind, w, h))
					if result == "" {
						t.Fatal("Render returned empty string")
					}
					if httpDigitsRe.MatchString(result) {
						t.Errorf("Render contains HTTP status digits: %s", result)
					}
					if !strings.Contains(result, k.title) {
						t.Errorf("Render missing expected title %q", k.title)
					}
				})
			}
		}
	}
}

func TestRender_TooSmall(t *testing.T) {
	t.Helper()
	result := Render(baseOpts(EmptyStateNoData, 20, 4))
	if !strings.Contains(result, "…") {
		t.Error("expected ellipsis for too-small panel")
	}
}

func TestRender_DefaultHints(t *testing.T) {
	t.Helper()
	opts := baseOpts(EmptyStateNoData, 80, 15)
	opts.Hint = ""
	result := Render(opts)
	if !strings.Contains(result, "Data will appear") {
		t.Error("expected default NoData hint")
	}

	opts2 := baseOpts(EmptyStateFeatureGated, 80, 15)
	opts2.Hint = ""
	result2 := Render(opts2)
	if !strings.Contains(result2, "Daemon configuration") {
		t.Error("expected default FeatureGated hint")
	}
}

func TestRender_DisconnectedRetry(t *testing.T) {
	t.Helper()
	opts := baseOpts(EmptyStateDisconnected, 80, 15)
	opts.RetryInSecs = 4
	result := Render(opts)
	if !strings.Contains(result, "4s") {
		t.Errorf("expected retry countdown 4s in output")
	}
}
