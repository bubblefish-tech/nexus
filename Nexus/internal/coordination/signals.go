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

// Package coordination implements agent-to-agent signal queues for the
// BubbleFish Nexus Agent Gateway. Signals are the coordination primitives
// agents use to communicate without going through memory writes.
//
// Ephemeral signals live only in memory and are lost on restart.
// Persistent signals are written to the WAL and survive restart. On startup,
// the signal queue is reconstructed from WAL replay of persistent entries.
// Reconstruction is idempotent: replaying the same WAL entry twice does not
// produce duplicate signals.
package coordination

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Signal represents a coordination message between agents.
type Signal struct {
	Seq        int64           `json:"seq"`
	FromAgent  string          `json:"from_agent"`
	ToAgent    string          `json:"to_agent"` // empty = broadcast
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	Persistent bool            `json:"persistent"`
	Timestamp  time.Time       `json:"timestamp"`
}

// SignalQueue manages per-agent signal queues with bounded depth and
// monotonic sequence numbers. It supports both ephemeral (in-memory only)
// and persistent (WAL-backed) signals.
//
// CRASH SAFETY: Persistent signals are written to the WAL before being
// enqueued. On restart, ReplayPersistent rebuilds the queue from WAL entries.
// The monotonic sequence number ensures idempotent replay: a signal with a
// sequence already present in the queue is silently skipped.
type SignalQueue struct {
	mu       sync.RWMutex
	queues   map[string]*agentQueue // agent_id → queue
	maxDepth int
	logger   *slog.Logger
	nextSeq  atomic.Int64
}

// agentQueue is the signal queue for a single agent.
type agentQueue struct {
	signals []Signal
	seenSeq map[int64]bool // tracks delivered sequences for idempotency
}

// NewSignalQueue creates a signal queue with the given max depth per agent.
func NewSignalQueue(maxDepth int, logger *slog.Logger) *SignalQueue {
	if maxDepth <= 0 {
		maxDepth = 1000
	}
	return &SignalQueue{
		queues:   make(map[string]*agentQueue),
		maxDepth: maxDepth,
		logger:   logger,
	}
}

// Broadcast enqueues a signal to all target agents. If targets is empty,
// the signal goes to all agents with existing queues.
//
// Returns the assigned sequence number.
func (sq *SignalQueue) Broadcast(fromAgent string, signalType string, payload json.RawMessage, persistent bool, targets []string) int64 {
	seq := sq.nextSeq.Add(1)
	now := time.Now().UTC()

	sig := Signal{
		Seq:        seq,
		FromAgent:  fromAgent,
		Type:       signalType,
		Payload:    payload,
		Persistent: persistent,
		Timestamp:  now,
	}

	sq.mu.Lock()
	defer sq.mu.Unlock()

	if len(targets) == 0 {
		// Broadcast to all known agents.
		for agentID := range sq.queues {
			if agentID == fromAgent {
				continue // don't send to self
			}
			s := sig
			s.ToAgent = agentID
			sq.enqueueUnsafe(agentID, s)
		}
	} else {
		for _, target := range targets {
			if target == fromAgent {
				continue
			}
			s := sig
			s.ToAgent = target
			sq.enqueueUnsafe(target, s)
		}
	}

	return seq
}

// Send enqueues a signal to a specific agent. Returns the assigned sequence.
func (sq *SignalQueue) Send(fromAgent, toAgent string, signalType string, payload json.RawMessage, persistent bool) int64 {
	seq := sq.nextSeq.Add(1)
	now := time.Now().UTC()

	sig := Signal{
		Seq:        seq,
		FromAgent:  fromAgent,
		ToAgent:    toAgent,
		Type:       signalType,
		Payload:    payload,
		Persistent: persistent,
		Timestamp:  now,
	}

	sq.mu.Lock()
	defer sq.mu.Unlock()
	sq.enqueueUnsafe(toAgent, sig)

	return seq
}

// Pull retrieves and removes up to maxN pending signals for the given agent.
// Signals are returned in FIFO order. The caller must confirm delivery by
// not calling Pull again with overlapping sequences (monotonic guarantee).
func (sq *SignalQueue) Pull(agentID string, maxN int) []Signal {
	if maxN <= 0 {
		maxN = 100
	}

	sq.mu.Lock()
	defer sq.mu.Unlock()

	q, ok := sq.queues[agentID]
	if !ok || len(q.signals) == 0 {
		return nil
	}

	n := maxN
	if n > len(q.signals) {
		n = len(q.signals)
	}

	pulled := make([]Signal, n)
	copy(pulled, q.signals[:n])

	// Mark as seen for idempotency.
	for _, s := range pulled {
		q.seenSeq[s.Seq] = true
	}

	// Remove from queue.
	q.signals = q.signals[n:]

	return pulled
}

// PendingCount returns the number of pending signals for an agent.
func (sq *SignalQueue) PendingCount(agentID string) int {
	sq.mu.RLock()
	defer sq.mu.RUnlock()

	q, ok := sq.queues[agentID]
	if !ok {
		return 0
	}
	return len(q.signals)
}

// EnsureQueue creates an empty queue for the agent if one doesn't exist.
// Used to register agents for broadcast targeting.
func (sq *SignalQueue) EnsureQueue(agentID string) {
	sq.mu.Lock()
	defer sq.mu.Unlock()
	if _, ok := sq.queues[agentID]; !ok {
		sq.queues[agentID] = &agentQueue{
			seenSeq: make(map[int64]bool),
		}
	}
}

// ReplayPersistent re-enqueues a persistent signal from WAL replay.
// Idempotent: if the signal's sequence was already seen, it is silently skipped.
//
// CRASH SAFETY: This function is called during daemon startup for every
// persistent signal WAL entry. It must not produce duplicates even if the
// same entry appears multiple times in the WAL (segment overlap during crash).
func (sq *SignalQueue) ReplayPersistent(sig Signal) {
	if !sig.Persistent {
		return // ephemeral signals are not replayed
	}

	sq.mu.Lock()
	defer sq.mu.Unlock()

	agentID := sig.ToAgent
	q, ok := sq.queues[agentID]
	if !ok {
		q = &agentQueue{
			seenSeq: make(map[int64]bool),
		}
		sq.queues[agentID] = q
	}

	// Idempotency: skip if already seen (pulled) or already in queue.
	if q.seenSeq[sig.Seq] {
		return
	}
	for _, existing := range q.signals {
		if existing.Seq == sig.Seq {
			return
		}
	}

	sq.enqueueUnsafe(agentID, sig)

	// Update nextSeq to be at least as high as the replayed sequence.
	for {
		current := sq.nextSeq.Load()
		if sig.Seq <= current {
			break
		}
		if sq.nextSeq.CompareAndSwap(current, sig.Seq) {
			break
		}
	}
}

// enqueueUnsafe adds a signal to an agent's queue. Caller must hold mu.Lock().
// Enforces maxDepth by dropping the oldest signal on overflow (FIFO with overflow).
func (sq *SignalQueue) enqueueUnsafe(agentID string, sig Signal) {
	q, ok := sq.queues[agentID]
	if !ok {
		q = &agentQueue{
			seenSeq: make(map[int64]bool),
		}
		sq.queues[agentID] = q
	}

	if len(q.signals) >= sq.maxDepth {
		// Drop oldest (FIFO overflow).
		sq.logger.Warn("coordination: signal queue overflow, dropping oldest",
			"agent_id", agentID,
			"depth", len(q.signals),
		)
		q.signals = q.signals[1:]
	}

	q.signals = append(q.signals, sig)
}

// MarshalSignalForWAL creates a JSON payload suitable for a WAL entry
// from a Signal. The result is used as the Entry.Payload field.
func MarshalSignalForWAL(sig Signal) (json.RawMessage, error) {
	data, err := json.Marshal(sig)
	if err != nil {
		return nil, fmt.Errorf("coordination: marshal signal: %w", err)
	}
	return data, nil
}

// UnmarshalSignalFromWAL parses a Signal from a WAL entry payload.
func UnmarshalSignalFromWAL(payload json.RawMessage) (Signal, error) {
	var sig Signal
	if err := json.Unmarshal(payload, &sig); err != nil {
		return Signal{}, fmt.Errorf("coordination: unmarshal signal: %w", err)
	}
	return sig, nil
}
