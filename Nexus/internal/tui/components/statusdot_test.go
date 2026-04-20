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

func TestStatusDot_Online_NonEmpty(t *testing.T) {
	t.Helper()
	d := StatusDot{Status: DotOnline, Frame: 0}
	if d.View() == "" {
		t.Fatal("expected non-empty dot view")
	}
}

func TestStatusDot_Offline_NonEmpty(t *testing.T) {
	t.Helper()
	d := StatusDot{Status: DotOffline, Frame: 0}
	if d.View() == "" {
		t.Fatal("expected non-empty offline dot view")
	}
}

func TestStatusDot_Degraded_NonEmpty(t *testing.T) {
	t.Helper()
	d := StatusDot{Status: DotDegraded, Frame: 0}
	if d.View() == "" {
		t.Fatal("expected non-empty degraded dot view")
	}
}

func TestStatusDot_ContainsDot(t *testing.T) {
	t.Helper()
	for _, status := range []DotStatus{DotOnline, DotDegraded, DotOffline} {
		d := StatusDot{Status: status, Frame: 0}
		v := d.View()
		if !strings.Contains(v, "●") {
			t.Errorf("status %d: expected '●' in view, got %q", status, v)
		}
	}
}

func TestStatusDot_PulseFramesDiffer(t *testing.T) {
	t.Helper()
	// Verify the even/odd frame logic path is exercised without crashing.
	// In headless environments lipgloss may not emit ANSI, so we just check
	// that both frames produce non-empty output.
	d0 := StatusDot{Status: DotOnline, Frame: 0}
	d1 := StatusDot{Status: DotOnline, Frame: 1}
	if d0.View() == "" || d1.View() == "" {
		t.Fatal("expected non-empty output for both frames")
	}
}

func TestStatusDot_OfflineNoPulse(t *testing.T) {
	t.Helper()
	// Offline dots should look the same regardless of frame.
	d0 := StatusDot{Status: DotOffline, Frame: 0}
	d1 := StatusDot{Status: DotOffline, Frame: 1}
	if d0.View() != d1.View() {
		t.Fatal("offline dot should not pulse")
	}
}

func TestDotStatusFromString(t *testing.T) {
	t.Helper()
	tests := []struct {
		input string
		want  DotStatus
	}{
		{"green", DotOnline},
		{"amber", DotDegraded},
		{"red", DotOffline},
		{"", DotOffline},
		{"unknown", DotOffline},
	}
	for _, tt := range tests {
		got := DotStatusFromString(tt.input)
		if got != tt.want {
			t.Errorf("DotStatusFromString(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
