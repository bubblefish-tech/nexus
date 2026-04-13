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

package ingest

import (
	"testing"
	"time"
)

func TestDebouncerSingleEvent(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)
	defer d.Stop()

	d.Touch("/a")

	select {
	case path := <-d.Ready():
		if path != "/a" {
			t.Errorf("path = %q, want /a", path)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for debounced path")
	}
}

func TestDebouncerRapidEventsCoalesce(t *testing.T) {
	d := NewDebouncer(100 * time.Millisecond)
	defer d.Stop()

	// Fire 5 rapid events — only 1 should come out.
	for i := 0; i < 5; i++ {
		d.Touch("/a")
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case path := <-d.Ready():
		if path != "/a" {
			t.Errorf("path = %q, want /a", path)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for debounced path")
	}

	// No second event should arrive.
	select {
	case path := <-d.Ready():
		t.Errorf("unexpected second event: %q", path)
	case <-time.After(200 * time.Millisecond):
		// Expected — only one event.
	}
}

func TestDebouncerMultiplePaths(t *testing.T) {
	d := NewDebouncer(50 * time.Millisecond)
	defer d.Stop()

	d.Touch("/a")
	d.Touch("/b")

	seen := make(map[string]bool)
	for i := 0; i < 2; i++ {
		select {
		case path := <-d.Ready():
			seen[path] = true
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("timed out after %d paths", i)
		}
	}

	if !seen["/a"] || !seen["/b"] {
		t.Errorf("seen = %v, want both /a and /b", seen)
	}
}

func TestDebouncerStopCancelsPending(t *testing.T) {
	d := NewDebouncer(100 * time.Millisecond)
	d.Touch("/a")
	d.Stop()

	select {
	case path := <-d.Ready():
		t.Errorf("unexpected event after Stop: %q", path)
	case <-time.After(200 * time.Millisecond):
		// Expected — Stop cancelled the timer.
	}
}
