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

// WorkerPool is a bounded pool of goroutines that execute parse tasks.
// Submissions that exceed capacity are dropped (the debouncer will retry).
type WorkerPool struct {
	tasks chan func()
	wg    sync.WaitGroup
}

// NewWorkerPool creates a pool with n workers and a task buffer of n*4.
func NewWorkerPool(n int) *WorkerPool {
	if n < 1 {
		n = 1
	}
	p := &WorkerPool{tasks: make(chan func(), n*4)}
	for i := 0; i < n; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

func (p *WorkerPool) worker() {
	defer p.wg.Done()
	for task := range p.tasks {
		task()
	}
}

// Submit enqueues a task. If the pool is full, the task is dropped after
// a short timeout — the debouncer will re-trigger the path on the next event.
func (p *WorkerPool) Submit(task func()) {
	select {
	case p.tasks <- task:
	case <-time.After(100 * time.Millisecond):
		// Drop — debouncer will retry.
	}
}

// Shutdown closes the task channel and waits for all workers to finish.
func (p *WorkerPool) Shutdown() {
	close(p.tasks)
	p.wg.Wait()
}
