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
	"sync"
	"time"
)

// Debouncer coalesces rapid filesystem events for the same path. No parse
// runs until dur has elapsed since the last event on that path. Subsequent
// events within the debounce window reset the timer.
type Debouncer struct {
	dur    time.Duration
	mu     sync.Mutex
	timers map[string]*time.Timer
	ready  chan string
}

// NewDebouncer creates a Debouncer with the given quiescent duration.
func NewDebouncer(dur time.Duration) *Debouncer {
	return &Debouncer{
		dur:    dur,
		timers: make(map[string]*time.Timer),
		ready:  make(chan string, 64),
	}
}

// Touch starts or resets the debounce timer for path. After dur elapses
// without another Touch, path is sent on the Ready channel.
func (d *Debouncer) Touch(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[path]; ok {
		t.Stop()
	}
	d.timers[path] = time.AfterFunc(d.dur, func() {
		d.mu.Lock()
		delete(d.timers, path)
		d.mu.Unlock()
		select {
		case d.ready <- path:
		default:
			// Channel full — the next Touch will re-trigger.
		}
	})
}

// Ready returns the channel that receives paths whose debounce window
// has expired.
func (d *Debouncer) Ready() <-chan string { return d.ready }

// Stop cancels all pending timers. After Stop, no more paths will be
// sent on Ready.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for path, t := range d.timers {
		t.Stop()
		delete(d.timers, path)
	}
}
