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

package temporal

import (
	"testing"
	"time"
)

func TestComputeBin(t *testing.T) {
	t.Helper()
	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		age  time.Duration
		want int
	}{
		{"30 seconds ago", 30 * time.Second, 0},
		{"30 minutes ago", 30 * time.Minute, 0},
		{"2 hours ago", 2 * time.Hour, 1},
		{"36 hours ago", 36 * time.Hour, 2},
		{"3 days ago", 3 * 24 * time.Hour, 3},
		{"10 days ago", 10 * 24 * time.Hour, 4},
		{"20 days ago", 20 * 24 * time.Hour, 5},
		{"45 days ago", 45 * 24 * time.Hour, 6},
		{"80 days ago", 80 * 24 * time.Hour, 7},
		{"200 days ago", 200 * 24 * time.Hour, 8},
		{"500 days ago", 500 * 24 * time.Hour, 9},
		{"800 days ago", 800 * 24 * time.Hour, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := now.Add(-tt.age)
			got := ComputeBin(ts, now)
			if got != tt.want {
				t.Errorf("ComputeBin(%v ago) = %d, want %d", tt.age, got, tt.want)
			}
		})
	}
}

func TestBinLabel(t *testing.T) {
	t.Helper()
	tests := []struct {
		bin  int
		want string
	}{
		{0, "last hour"},
		{1, "today"},
		{2, "yesterday"},
		{5, "this month"},
		{10, "older"},
		{-1, "unknown"},
		{11, "unknown"},
	}
	for _, tt := range tests {
		got := BinLabel(tt.bin)
		if got != tt.want {
			t.Errorf("BinLabel(%d) = %q, want %q", tt.bin, got, tt.want)
		}
	}
}

func TestHumanRelativeTime(t *testing.T) {
	t.Helper()
	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		age  time.Duration
		want string
	}{
		{"just now", 10 * time.Second, "just now"},
		{"minutes", 5 * time.Minute, "5 minutes ago"},
		{"1 minute", 1 * time.Minute, "1 minute ago"},
		{"about an hour", 90 * time.Minute, "about an hour ago"},
		{"hours", 5 * time.Hour, "5 hours ago"},
		{"yesterday", 30 * time.Hour, "yesterday"},
		{"days", 4 * 24 * time.Hour, "4 days ago"},
		{"last week", 10 * 24 * time.Hour, "last week"},
		{"weeks", 21 * 24 * time.Hour, "3 weeks ago"},
		{"last month", 45 * 24 * time.Hour, "last month"},
		{"months", 120 * 24 * time.Hour, "4 months ago"},
		{"last year", 400 * 24 * time.Hour, "last year"},
		{"years", 1000 * 24 * time.Hour, "2 years ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := now.Add(-tt.age)
			got := HumanRelativeTime(ts, now)
			if got != tt.want {
				t.Errorf("HumanRelativeTime(%v ago) = %q, want %q", tt.age, got, tt.want)
			}
		})
	}
}

func TestComputeBin_BoundaryExact(t *testing.T) {
	t.Helper()
	now := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	justUnderHour := now.Add(-59*time.Minute - 59*time.Second)
	if ComputeBin(justUnderHour, now) != 0 {
		t.Error("59m59s should be bin 0 (last hour)")
	}
	justOverHour := now.Add(-time.Hour - time.Second)
	if ComputeBin(justOverHour, now) != 1 {
		t.Error("1h1s should be bin 1 (today)")
	}
}
