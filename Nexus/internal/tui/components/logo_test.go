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
	"testing"
)

func TestLogo_FullWidth_NonEmpty(t *testing.T) {
	t.Helper()
	out := Logo{Width: 120}.View()
	if out == "" {
		t.Fatal("expected non-empty logo output")
	}
}

func TestLogo_FullWidth_ContainsBubbleFish(t *testing.T) {
	t.Helper()
	out := Logo{Width: 120}.View()
	if !strings.Contains(out, "BubbleFish") {
		t.Fatalf("expected logo to contain 'BubbleFish'")
	}
}

func TestLogo_FullWidth_MinimumHeight(t *testing.T) {
	t.Helper()
	out := Logo{Width: 120}.View()
	lines := strings.Split(out, "\n")
	nonEmpty := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			nonEmpty++
		}
	}
	if nonEmpty < 5 {
		t.Fatalf("expected at least 5 non-empty lines, got %d", nonEmpty)
	}
}

func TestLogo_Compact_NonEmpty(t *testing.T) {
	t.Helper()
	out := Logo{Width: 60}.View()
	if out == "" {
		t.Fatal("expected non-empty compact logo output")
	}
}

func TestLogo_Compact_ContainsBubbleFish(t *testing.T) {
	t.Helper()
	out := Logo{Width: 40}.View()
	if !strings.Contains(out, "BubbleFish") {
		t.Fatal("compact logo should contain 'BubbleFish'")
	}
}

func TestLogo_CompactFallback_NarrowTerminal(t *testing.T) {
	t.Helper()
	out := Logo{Width: 0}.View()
	if out == "" {
		t.Fatal("expected output even at Width=0")
	}
}

func TestLogo_WidthVariation_OutputDiffers(t *testing.T) {
	t.Helper()
	narrow := Logo{Width: 40}.View()
	wide := Logo{Width: 120}.View()
	if narrow == wide {
		t.Fatal("expected different output at different widths")
	}
}
