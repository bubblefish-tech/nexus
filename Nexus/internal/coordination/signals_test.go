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

package coordination

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"
)

func testQueue(t *testing.T) *SignalQueue {
	t.Helper()
	return NewSignalQueue(1000, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}

func TestBroadcast_ToTargets(t *testing.T) {
	sq := testQueue(t)
	sq.EnsureQueue("agent-a")
	sq.EnsureQueue("agent-b")
	sq.EnsureQueue("agent-c")

	payload := json.RawMessage(`{"msg": "hello"}`)
	seq := sq.Broadcast("agent-a", "greeting", payload, false, []string{"agent-b", "agent-c"})
	if seq <= 0 {
		t.Fatal("expected positive sequence number")
	}

	// agent-b should have 1 signal.
	signals := sq.Pull("agent-b", 10)
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal for agent-b, got %d", len(signals))
	}
	if signals[0].Type != "greeting" {
		t.Fatalf("expected type greeting, got %s", signals[0].Type)
	}
	if signals[0].FromAgent != "agent-a" {
		t.Fatalf("expected from agent-a, got %s", signals[0].FromAgent)
	}

	// agent-c should have 1 signal.
	signals = sq.Pull("agent-c", 10)
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal for agent-c, got %d", len(signals))
	}

	// agent-a (sender) should have 0 signals.
	signals = sq.Pull("agent-a", 10)
	if len(signals) != 0 {
		t.Fatalf("sender should not receive own broadcast, got %d", len(signals))
	}
}

func TestBroadcast_ToAll(t *testing.T) {
	sq := testQueue(t)
	sq.EnsureQueue("agent-a")
	sq.EnsureQueue("agent-b")

	sq.Broadcast("agent-a", "ping", nil, false, nil)

	// Only agent-b should get the broadcast (agent-a is the sender).
	if sq.PendingCount("agent-b") != 1 {
		t.Fatalf("expected 1 pending for agent-b, got %d", sq.PendingCount("agent-b"))
	}
	if sq.PendingCount("agent-a") != 0 {
		t.Fatalf("sender should have 0 pending, got %d", sq.PendingCount("agent-a"))
	}
}

func TestSend_Direct(t *testing.T) {
	sq := testQueue(t)
	sq.EnsureQueue("agent-b")

	seq := sq.Send("agent-a", "agent-b", "task", json.RawMessage(`{"id": 1}`), false)
	if seq <= 0 {
		t.Fatal("expected positive sequence")
	}

	signals := sq.Pull("agent-b", 10)
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].ToAgent != "agent-b" {
		t.Fatalf("expected to_agent agent-b, got %s", signals[0].ToAgent)
	}
}

func TestPull_Idempotency(t *testing.T) {
	sq := testQueue(t)
	sq.EnsureQueue("agent-b")

	sq.Send("agent-a", "agent-b", "task", nil, false)
	sq.Send("agent-a", "agent-b", "task", nil, false)

	// Pull first signal.
	signals := sq.Pull("agent-b", 1)
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	firstSeq := signals[0].Seq

	// Pull second signal.
	signals = sq.Pull("agent-b", 1)
	if len(signals) != 1 {
		t.Fatalf("expected 1 remaining signal, got %d", len(signals))
	}
	if signals[0].Seq == firstSeq {
		t.Fatal("second pull should have different sequence")
	}

	// Pull again — should be empty.
	signals = sq.Pull("agent-b", 10)
	if len(signals) != 0 {
		t.Fatalf("expected 0 after pulling all, got %d", len(signals))
	}
}

func TestPull_EmptyQueue(t *testing.T) {
	sq := testQueue(t)
	signals := sq.Pull("nonexistent", 10)
	if signals != nil {
		t.Fatalf("expected nil for nonexistent agent, got %v", signals)
	}
}

func TestSignalQueue_Overflow(t *testing.T) {
	sq := NewSignalQueue(3, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	sq.EnsureQueue("agent-b")

	// Send 5 signals to a queue with depth 3.
	for i := 0; i < 5; i++ {
		sq.Send("agent-a", "agent-b", "overflow", nil, false)
	}

	// Should only have the last 3 (oldest dropped).
	if sq.PendingCount("agent-b") != 3 {
		t.Fatalf("expected 3 after overflow, got %d", sq.PendingCount("agent-b"))
	}

	signals := sq.Pull("agent-b", 10)
	// The sequences should be 3, 4, 5 (first two dropped).
	if signals[0].Seq != 3 {
		t.Fatalf("expected seq 3 (oldest surviving), got %d", signals[0].Seq)
	}
}

func TestReplayPersistent_Idempotent(t *testing.T) {
	sq := testQueue(t)
	sq.EnsureQueue("agent-b")

	sig := Signal{
		Seq:        42,
		FromAgent:  "agent-a",
		ToAgent:    "agent-b",
		Type:       "task",
		Persistent: true,
	}

	// Replay once.
	sq.ReplayPersistent(sig)
	if sq.PendingCount("agent-b") != 1 {
		t.Fatalf("expected 1 after first replay, got %d", sq.PendingCount("agent-b"))
	}

	// Replay again — should NOT produce a duplicate.
	sq.ReplayPersistent(sig)
	if sq.PendingCount("agent-b") != 1 {
		t.Fatalf("expected 1 after duplicate replay, got %d", sq.PendingCount("agent-b"))
	}
}

func TestReplayPersistent_SkipsEphemeral(t *testing.T) {
	sq := testQueue(t)
	sq.EnsureQueue("agent-b")

	sig := Signal{
		Seq:        1,
		FromAgent:  "agent-a",
		ToAgent:    "agent-b",
		Type:       "task",
		Persistent: false, // ephemeral
	}

	sq.ReplayPersistent(sig)
	if sq.PendingCount("agent-b") != 0 {
		t.Fatal("ephemeral signals should not be replayed")
	}
}

func TestReplayPersistent_UpdatesNextSeq(t *testing.T) {
	sq := testQueue(t)
	sq.EnsureQueue("agent-b")

	sig := Signal{
		Seq:        100,
		FromAgent:  "agent-a",
		ToAgent:    "agent-b",
		Type:       "task",
		Persistent: true,
	}
	sq.ReplayPersistent(sig)

	// New signals should get sequences > 100.
	newSeq := sq.Send("agent-a", "agent-b", "new", nil, false)
	if newSeq <= 100 {
		t.Fatalf("new sequence should be > 100, got %d", newSeq)
	}
}

func TestReplayPersistent_AfterPull(t *testing.T) {
	sq := testQueue(t)
	sq.EnsureQueue("agent-b")

	sig := Signal{
		Seq:        1,
		FromAgent:  "agent-a",
		ToAgent:    "agent-b",
		Type:       "task",
		Persistent: true,
	}

	// Replay, pull (which marks as seen), then replay again.
	sq.ReplayPersistent(sig)
	sq.Pull("agent-b", 10)

	// After pull, replaying the same sequence should be skipped
	// because Pull marks sequences as seen.
	sq.ReplayPersistent(sig)
	if sq.PendingCount("agent-b") != 0 {
		t.Fatal("replaying after pull should not re-enqueue")
	}
}

func TestMarshalUnmarshalSignal(t *testing.T) {
	orig := Signal{
		Seq:        42,
		FromAgent:  "agent-a",
		ToAgent:    "agent-b",
		Type:       "task_complete",
		Payload:    json.RawMessage(`{"result": "ok"}`),
		Persistent: true,
	}

	data, err := MarshalSignalForWAL(orig)
	if err != nil {
		t.Fatal(err)
	}

	restored, err := UnmarshalSignalFromWAL(data)
	if err != nil {
		t.Fatal(err)
	}

	if restored.Seq != orig.Seq {
		t.Fatalf("seq mismatch: %d vs %d", restored.Seq, orig.Seq)
	}
	if restored.Type != orig.Type {
		t.Fatalf("type mismatch: %s vs %s", restored.Type, orig.Type)
	}
	if restored.FromAgent != orig.FromAgent {
		t.Fatalf("from mismatch")
	}
	if !restored.Persistent {
		t.Fatal("persistent flag lost")
	}
}

func TestConcurrentAccess(t *testing.T) {
	sq := testQueue(t)

	for i := 0; i < 10; i++ {
		sq.EnsureQueue("agent-" + string(rune('a'+i)))
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			from := "agent-" + string(rune('a'+(i%10)))
			to := "agent-" + string(rune('a'+((i+1)%10)))
			sq.Send(from, to, "ping", nil, false)
			sq.Pull(to, 5)
			sq.PendingCount(to)
			sq.Broadcast(from, "broadcast", nil, false, nil)
		}(i)
	}
	wg.Wait()
	// No panics or races = pass.
}

func TestEnsureQueue_CreatesQueue(t *testing.T) {
	sq := testQueue(t)

	sq.EnsureQueue("new-agent")

	// Sending to the new agent should work.
	sq.Send("other", "new-agent", "hello", nil, false)
	if sq.PendingCount("new-agent") != 1 {
		t.Fatal("expected 1 pending after send to newly created queue")
	}
}

func TestPendingCount_NoQueue(t *testing.T) {
	sq := testQueue(t)
	if sq.PendingCount("nonexistent") != 0 {
		t.Fatal("expected 0 for nonexistent agent")
	}
}
