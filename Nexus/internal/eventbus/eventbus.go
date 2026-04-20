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

// Package eventbus provides a lossy pub/sub activity event bus for the
// WebUI activity feed (GET /api/events/stream).
//
// INVARIANT: Publish never blocks the caller. Slow consumers are skipped.
package eventbus

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// EventType identifies the category of activity event.
type EventType string

const (
	EventMemoryWritten     EventType = "memory_written"
	EventMemoryQueried     EventType = "memory_queried"
	EventAgentConnected    EventType = "agent_connected"
	EventAgentDisconnected EventType = "agent_disconnected"
	EventQuarantineEvent   EventType = "quarantine_event"
	EventIngest            EventType = "ingest"
	EventDiscoveryEvent    EventType = "discovery_event"
)

// Event is one activity event published to the bus and streamed to SSE clients.
type Event struct {
	Type      EventType         `json:"type"`
	Timestamp time.Time         `json:"ts"`
	Source    string            `json:"source,omitempty"`
	AgentID   string            `json:"agent_id,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
}

// MarshalSSE serialises e as an SSE "data:" frame terminated by two newlines.
func MarshalSSE(e Event) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, "data: %s\n\n", b), nil
}

// Bus is a lossy buffered pub/sub event bus. Publish is always non-blocking;
// slow consumers miss events rather than stalling publishers.
type Bus struct {
	ch      chan Event
	mu      sync.RWMutex
	clients map[uint64]chan Event
	nextID  uint64
	stop    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
}

// New creates a Bus with an internal channel of capacity cap.
// cap should be at least 64; 256 is a reasonable default.
func New(cap int) *Bus {
	if cap < 1 {
		cap = 256
	}
	return &Bus{
		ch:      make(chan Event, cap),
		clients: make(map[uint64]chan Event),
		stop:    make(chan struct{}),
	}
}

// Start launches the fan-out dispatcher goroutine. Must be called before Publish.
func (b *Bus) Start() {
	b.wg.Add(1)
	go b.dispatch()
}

// Stop shuts down the dispatcher gracefully. Safe to call multiple times.
func (b *Bus) Stop() {
	b.once.Do(func() { close(b.stop) })
	b.wg.Wait()
}

// Publish sends e to all current subscribers. Non-blocking: if the internal
// channel is full the event is silently dropped.
func (b *Bus) Publish(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	select {
	case b.ch <- e:
	default:
	}
}

// Subscribe returns a read channel and an unsubscribe function. Each
// subscriber gets its own buffered channel (capacity 64). Calling the returned
// function removes the subscriber; the channel is then drained and closed.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	const perClientCap = 64
	ch := make(chan Event, perClientCap)

	b.mu.Lock()
	id := b.nextID
	b.nextID++
	b.clients[id] = ch
	b.mu.Unlock()

	unsub := func() {
		b.mu.Lock()
		delete(b.clients, id)
		b.mu.Unlock()
		// drain so the dispatcher never blocks on a closed channel
		for len(ch) > 0 {
			<-ch
		}
		close(ch)
	}
	return ch, unsub
}

func (b *Bus) dispatch() {
	defer b.wg.Done()
	for {
		select {
		case <-b.stop:
			return
		case e := <-b.ch:
			b.mu.RLock()
			for _, ch := range b.clients {
				select {
				case ch <- e:
				default: // slow consumer — drop
				}
			}
			b.mu.RUnlock()
		}
	}
}
