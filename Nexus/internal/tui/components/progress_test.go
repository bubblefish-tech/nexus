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

func TestProgressBar_DeterminateNonEmpty(t *testing.T) {
	t.Helper()
	pb := ProgressBar{Total: 10, Current: 5, Label: "Scanning", Width: 60}
	v := pb.View()
	if v == "" {
		t.Fatal("expected non-empty determinate view")
	}
}

func TestProgressBar_DeterminateContainsPercent(t *testing.T) {
	t.Helper()
	pb := ProgressBar{Total: 10, Current: 5, Label: "Scanning", Width: 60}
	v := pb.View()
	if !strings.Contains(v, "%") {
		t.Fatal("expected percent sign in determinate view")
	}
}

func TestProgressBar_Indeterminate(t *testing.T) {
	t.Helper()
	pb := ProgressBar{Spinning: true, Label: "Loading", Frame: 0}
	v := pb.View()
	if v == "" {
		t.Fatal("expected non-empty spinner view")
	}
}

func TestProgressBar_SpinnerFrameWraps(t *testing.T) {
	t.Helper()
	// Should not panic on frame > len(spinFrames).
	pb := ProgressBar{Spinning: true, Label: "x", Frame: 1000}
	_ = pb.View()
}

func TestProgressBar_ZeroTotal(t *testing.T) {
	t.Helper()
	// Total=0 → 0% fill, should not panic.
	pb := ProgressBar{Total: 0, Current: 0, Label: "Wait", Width: 60}
	v := pb.View()
	if v == "" {
		t.Fatal("expected output even at Total=0")
	}
}
