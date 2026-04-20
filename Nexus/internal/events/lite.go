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

// Package events provides the Event Bus Lite: a lightweight, lossy, single-consumer
// event bus used for internal event routing throughout Nexus.
//
// Design: non-blocking Emit drops events when the buffer is full; Stream() returns
// the raw channel so a single goroutine (or bridge to an SSE fan-out bus) consumes
// all events. Safe for concurrent Emit calls.
package events

// Emitter is the minimal interface for publishing events.
// A2ADashboard and other non-daemon packages accept an Emitter so they can
// publish events without importing the concrete LiteBus type.

import (
	"sync/atomic"
	"time"
)

// Emitter is the minimal interface for publishing events.
type Emitter interface {
	Emit(eventType string, data map[string]any)
}

// LiteEvent is a structured event emitted on the bus.
type LiteEvent struct {
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

// LiteBus is a buffered, lossy, single-consumer event bus.
// Emit is always non-blocking; events are dropped when the buffer is full.
// Stream returns the underlying channel; exactly one goroutine should read from it.
// Close shuts down the bus; further Emit calls are silently ignored.
type LiteBus struct {
	ch     chan LiteEvent
	closed atomic.Bool
}

// NewLiteBus creates a LiteBus with the given channel buffer size.
// bufferSize < 1 is clamped to 256.
func NewLiteBus(bufferSize int) *LiteBus {
	if bufferSize < 1 {
		bufferSize = 256
	}
	return &LiteBus{ch: make(chan LiteEvent, bufferSize)}
}

// Emit publishes an event of the given type with arbitrary data.
// It is non-blocking: if the channel buffer is full the event is silently dropped.
// Emit after Close is a no-op.
func (b *LiteBus) Emit(eventType string, data map[string]any) {
	if b.closed.Load() {
		return
	}
	select {
	case b.ch <- LiteEvent{
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		Data:      data,
	}:
	default: // buffer full — drop
	}
}

// Stream returns the read-only event channel.
// Exactly one goroutine should consume from this channel.
// The channel is closed when Close is called.
func (b *LiteBus) Stream() <-chan LiteEvent {
	return b.ch
}

// Close stops the bus. The channel returned by Stream is closed so the consumer
// goroutine can exit via a range loop. Safe to call multiple times.
func (b *LiteBus) Close() {
	if b.closed.CompareAndSwap(false, true) {
		close(b.ch)
	}
}
