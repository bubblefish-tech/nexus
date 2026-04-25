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

package dialect

import "testing"

func TestPlaceholder_SQLite(t *testing.T) {
	t.Helper()
	b := Builder{Dialect: SQLite}
	for _, n := range []int{1, 2, 5, 100} {
		got := b.Placeholder(n)
		if got != "?" {
			t.Errorf("SQLite placeholder(%d) = %q, want ?", n, got)
		}
	}
}

func TestPlaceholder_Postgres(t *testing.T) {
	t.Helper()
	b := Builder{Dialect: Postgres}
	tests := []struct {
		n    int
		want string
	}{
		{1, "$1"},
		{2, "$2"},
		{10, "$10"},
		{100, "$100"},
	}
	for _, tt := range tests {
		got := b.Placeholder(tt.n)
		if got != tt.want {
			t.Errorf("Postgres placeholder(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestBoolLiteral(t *testing.T) {
	t.Helper()
	tests := []struct {
		dialect Dialect
		val     bool
		want    string
	}{
		{SQLite, true, "1"},
		{SQLite, false, "0"},
		{Postgres, true, "TRUE"},
		{Postgres, false, "FALSE"},
	}
	for _, tt := range tests {
		b := Builder{Dialect: tt.dialect}
		got := b.BoolLiteral(tt.val)
		if got != tt.want {
			t.Errorf("BoolLiteral(dialect=%d, %v) = %q, want %q", tt.dialect, tt.val, got, tt.want)
		}
	}
}

func TestTimestampFunc(t *testing.T) {
	t.Helper()
	sqlite := Builder{Dialect: SQLite}
	if got := sqlite.TimestampFunc(); got != "datetime('now')" {
		t.Errorf("SQLite TimestampFunc = %q", got)
	}

	pg := Builder{Dialect: Postgres}
	if got := pg.TimestampFunc(); got != "NOW() AT TIME ZONE 'UTC'" {
		t.Errorf("Postgres TimestampFunc = %q", got)
	}
}
