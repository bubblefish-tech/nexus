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

// Package vizpipe implements the live pipeline visualization event channel for
// the BubbleFish Nexus dashboard. Events are sent via a lossy buffered channel
// and broadcast to SSE clients.
//
// INVARIANT: The visualization channel NEVER blocks hot paths. The channel is
// lossy by design — if the channel is full, events are dropped and a metric is
// incremented.
//
// Reference: Tech Spec Section 13.2, Phase R-21.
package vizpipe

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// Event represents a pipeline visualization event.
// Reference: Tech Spec Section 13.2.
type Event struct {
	RequestID   string    `json:"request_id"`
	Stage       string    `json:"stage"`
	DurationMs  float64   `json:"duration_ms"`
	HitMiss     string    `json:"hit_miss"` // "hit", "miss", or ""
	ResultCount int       `json:"result_count"`
	Timestamp   time.Time `json:"timestamp"`
	Source      string    `json:"source,omitempty"`
	Destination string    `json:"destination,omitempty"`
	Profile     string    `json:"profile,omitempty"`
}

// Pipe is the lossy visualization event pipe. Emit from the query path
// (non-blocking). SSE clients subscribe to receive events.
type Pipe struct {
	ch      chan Event
	metrics DropMetric
	logger  *slog.Logger

	mu      sync.RWMutex
	clients map[uint64]chan Event
	nextID  uint64

	stop chan struct{}
	once sync.Once
	wg   sync.WaitGroup
}

// DropMetric is the interface for the dropped events counter.
type DropMetric interface {
	Inc()
}

// New creates a Pipe with a lossy buffered channel of the given capacity.
func New(capacity int, metric DropMetric, logger *slog.Logger) *Pipe {
	if capacity <= 0 {
		capacity = 1000
	}
	return &Pipe{
		ch:      make(chan Event, capacity),
		metrics: metric,
		logger:  logger,
		clients: make(map[uint64]chan Event),
		stop:    make(chan struct{}),
	}
}

// Start launches the dispatcher goroutine that fans out events to SSE clients.
func (p *Pipe) Start() {
	p.wg.Add(1)
	go p.dispatch()
}

// Stop shuts down the dispatcher. Safe to call multiple times (sync.Once).
func (p *Pipe) Stop() {
	p.once.Do(func() {
		close(p.stop)
		p.wg.Wait()
	})
}

// Emit sends an event to the visualization channel. It NEVER blocks — if the
// channel is full, the event is dropped and the metric is incremented.
//
// INVARIANT: Called on the query hot path. Must be non-blocking.
func (p *Pipe) Emit(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	select {
	case p.ch <- e:
	default:
		p.metrics.Inc()
	}
}

// Subscribe registers a new SSE client and returns a channel to receive events
// and an unsubscribe function. The channel has a small buffer; slow clients
// will miss events (lossy).
func (p *Pipe) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	p.mu.Lock()
	id := p.nextID
	p.nextID++
	p.clients[id] = ch
	p.mu.Unlock()

	unsub := func() {
		p.mu.Lock()
		delete(p.clients, id)
		p.mu.Unlock()
	}
	return ch, unsub
}

// dispatch reads events from the main channel and fans them out to all
// subscribed SSE clients. Slow clients silently miss events.
func (p *Pipe) dispatch() {
	defer p.wg.Done()
	for {
		select {
		case e := <-p.ch:
			p.broadcast(e)
		case <-p.stop:
			// Drain remaining.
			for {
				select {
				case e := <-p.ch:
					p.broadcast(e)
				default:
					return
				}
			}
		}
	}
}

// broadcast sends an event to all subscribed clients. Non-blocking per client.
func (p *Pipe) broadcast(e Event) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, ch := range p.clients {
		select {
		case ch <- e:
		default:
			// Slow client — drop silently.
		}
	}
}

// MarshalSSE encodes an Event as an SSE data line.
func MarshalSSE(e Event) ([]byte, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	// SSE format: "data: {json}\n\n"
	buf := make([]byte, 0, 6+len(data)+2)
	buf = append(buf, "data: "...)
	buf = append(buf, data...)
	buf = append(buf, '\n', '\n')
	return buf, nil
}
