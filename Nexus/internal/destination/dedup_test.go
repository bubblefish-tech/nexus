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

package destination

import (
	"testing"
	"time"
)

func TestDedupCache_DuplicateWithinWindow(t *testing.T) {
	t.Helper()
	c := NewDedupCache(24 * time.Hour)
	c.Record("hello world", "id-1")

	got := c.Check("hello world")
	if got != "id-1" {
		t.Errorf("Check = %q, want %q", got, "id-1")
	}
}

func TestDedupCache_DifferentContentNotDuplicate(t *testing.T) {
	t.Helper()
	c := NewDedupCache(24 * time.Hour)
	c.Record("hello", "id-1")

	got := c.Check("world")
	if got != "" {
		t.Errorf("Check = %q, want empty for different content", got)
	}
}

func TestDedupCache_ExpiredNotDuplicate(t *testing.T) {
	t.Helper()
	c := NewDedupCache(1 * time.Millisecond)
	c.Record("hello", "id-1")

	time.Sleep(5 * time.Millisecond)
	got := c.Check("hello")
	if got != "" {
		t.Errorf("Check = %q, want empty for expired entry", got)
	}
}
