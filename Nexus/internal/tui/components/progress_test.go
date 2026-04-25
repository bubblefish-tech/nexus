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

func TestProgressBar_DeterminateContainsLabel(t *testing.T) {
	t.Helper()
	pb := ProgressBar{Total: 10, Current: 5, Label: "Scanning", Width: 60}
	v := pb.View()
	if !strings.Contains(v, "Scanning") {
		t.Fatalf("expected label 'Scanning' in view, got:\n%s", v)
	}
}

func TestProgressBar_Indeterminate(t *testing.T) {
	t.Helper()
	pb := ProgressBar{Spinning: true, Label: "Loading", Frame: 0}
	v := pb.View()
	if v == "" {
		t.Fatal("expected non-empty spinner view")
	}
	if !strings.Contains(v, "Loading") {
		t.Fatalf("expected label 'Loading' in spinner view")
	}
}

func TestProgressBar_SpinnerFrameWraps(t *testing.T) {
	t.Helper()
	pb := ProgressBar{Spinning: true, Label: "x", Frame: 1000}
	_ = pb.View()
}

func TestProgressBar_ZeroTotal(t *testing.T) {
	t.Helper()
	pb := ProgressBar{Total: 0, Current: 0, Label: "Wait", Width: 60}
	v := pb.View()
	if v == "" {
		t.Fatal("expected output even at Total=0")
	}
}

func TestProgressBar_ProgressChangesView(t *testing.T) {
	t.Helper()
	pb0 := ProgressBar{Total: 10, Current: 0, Label: "Work", Width: 60}
	pb5 := ProgressBar{Total: 10, Current: 5, Label: "Work", Width: 60}
	pb10 := ProgressBar{Total: 10, Current: 10, Label: "Work", Width: 60}

	v0 := pb0.View()
	v5 := pb5.View()
	v10 := pb10.View()

	if v0 == v5 {
		t.Fatal("expected view to differ between 0% and 50%")
	}
	if v5 == v10 {
		t.Fatal("expected view to differ between 50% and 100%")
	}
}

func TestProgressBar_SpinnerFrameChangesView(t *testing.T) {
	t.Helper()
	pb0 := ProgressBar{Spinning: true, Label: "x", Frame: 0}
	pb1 := ProgressBar{Spinning: true, Label: "x", Frame: 1}
	v0 := pb0.View()
	v1 := pb1.View()
	if v0 == v1 {
		t.Fatal("expected different spinner frames to produce different views")
	}
}
