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

// Package safego provides panic-recovery wrappers for subsystem goroutines.
// A panic in a wrapped goroutine is logged and the subsystem is marked
// degraded rather than crashing the entire daemon process.
package safego

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
)

// StatusTracker tracks which subsystems are alive vs degraded.
type StatusTracker struct {
	mu       sync.RWMutex
	degraded map[string]string
}

// NewStatusTracker creates a tracker with no degraded subsystems.
func NewStatusTracker() *StatusTracker {
	return &StatusTracker{degraded: make(map[string]string)}
}

// MarkDegraded records that a subsystem panicked.
func (s *StatusTracker) MarkDegraded(name, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.degraded[name] = reason
}

// Degraded returns a snapshot of degraded subsystems (name → panic reason).
func (s *StatusTracker) Degraded() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.degraded))
	for k, v := range s.degraded {
		out[k] = v
	}
	return out
}

// IsHealthy returns true when no subsystems are degraded.
func (s *StatusTracker) IsHealthy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.degraded) == 0
}

// Go launches fn in a goroutine with panic recovery. If fn panics, the
// panic is logged, the subsystem is marked degraded in tracker (if non-nil),
// and the daemon continues running. The main HTTP server goroutine should
// NOT be wrapped — if that panics, the daemon should crash.
func Go(name string, logger *slog.Logger, tracker *StatusTracker, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				reason := fmt.Sprintf("%v", r)
				if logger != nil {
					logger.Error("PANIC RECOVERED in subsystem",
						"subsystem", name,
						"panic", reason,
						"stack", string(stack),
					)
				}
				if tracker != nil {
					tracker.MarkDegraded(name, reason)
				}
			}
		}()
		fn()
	}()
}
