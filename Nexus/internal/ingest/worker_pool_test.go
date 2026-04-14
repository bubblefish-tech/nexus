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
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolExecutesTasks(t *testing.T) {
	p := NewWorkerPool(2)
	var count atomic.Int64

	for i := 0; i < 10; i++ {
		p.Submit(func() {
			count.Add(1)
		})
	}

	p.Shutdown()

	if got := count.Load(); got != 10 {
		t.Errorf("executed = %d, want 10", got)
	}
}

func TestWorkerPoolConcurrencyCapped(t *testing.T) {
	p := NewWorkerPool(2)
	var concurrent atomic.Int64
	var maxConcurrent atomic.Int64

	for i := 0; i < 20; i++ {
		p.Submit(func() {
			cur := concurrent.Add(1)
			// Track max.
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			concurrent.Add(-1)
		})
	}

	p.Shutdown()

	if max := maxConcurrent.Load(); max > 2 {
		t.Errorf("max concurrent = %d, want <= 2", max)
	}
}

func TestWorkerPoolShutdownDrains(t *testing.T) {
	p := NewWorkerPool(1)
	done := make(chan struct{})

	p.Submit(func() {
		time.Sleep(50 * time.Millisecond)
		close(done)
	})

	p.Shutdown() // Should block until the task finishes.

	select {
	case <-done:
		// Good — task completed before Shutdown returned.
	default:
		t.Error("Shutdown returned before task completed")
	}
}
