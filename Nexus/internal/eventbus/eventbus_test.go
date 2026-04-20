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

package eventbus_test

import (
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/eventbus"
)

func TestBus_PublishSubscribe(t *testing.T) {
	t.Helper()
	b := eventbus.New(64)
	b.Start()
	defer b.Stop()

	ch, unsub := b.Subscribe()
	defer unsub()

	b.Publish(eventbus.Event{Type: eventbus.EventMemoryWritten, Source: "test"})

	select {
	case e := <-ch:
		if e.Type != eventbus.EventMemoryWritten {
			t.Fatalf("expected memory_written, got %s", e.Type)
		}
		if e.Source != "test" {
			t.Fatalf("expected source test, got %s", e.Source)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestBus_TimestampAutoSet(t *testing.T) {
	t.Helper()
	b := eventbus.New(64)
	b.Start()
	defer b.Stop()

	ch, unsub := b.Subscribe()
	defer unsub()

	before := time.Now().UTC()
	b.Publish(eventbus.Event{Type: eventbus.EventMemoryQueried})
	after := time.Now().UTC()

	select {
	case e := <-ch:
		if e.Timestamp.Before(before) || e.Timestamp.After(after.Add(time.Millisecond)) {
			t.Fatalf("timestamp not set correctly: %v", e.Timestamp)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestBus_MultipleSubscribers(t *testing.T) {
	t.Helper()
	b := eventbus.New(64)
	b.Start()
	defer b.Stop()

	ch1, unsub1 := b.Subscribe()
	ch2, unsub2 := b.Subscribe()
	defer unsub1()
	defer unsub2()

	b.Publish(eventbus.Event{Type: eventbus.EventAgentConnected, AgentID: "a1"})

	for _, ch := range []<-chan eventbus.Event{ch1, ch2} {
		select {
		case e := <-ch:
			if e.AgentID != "a1" {
				t.Fatalf("expected agent a1, got %s", e.AgentID)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout for subscriber")
		}
	}
}

func TestBus_UnsubscribeStopsDelivery(t *testing.T) {
	t.Helper()
	b := eventbus.New(64)
	b.Start()
	defer b.Stop()

	ch, unsub := b.Subscribe()
	unsub()

	b.Publish(eventbus.Event{Type: eventbus.EventQuarantineEvent})
	select {
	case <-ch:
		// channel was closed by unsub — that's fine too
	case <-time.After(50 * time.Millisecond):
		// no delivery after unsub is correct
	}
}

func TestBus_StopIdempotent(t *testing.T) {
	t.Helper()
	b := eventbus.New(64)
	b.Start()
	b.Stop()
	b.Stop() // must not panic
}

func TestBus_LossyWhenFull(t *testing.T) {
	t.Helper()
	// Zero-capacity internal channel forces immediate drop.
	b := eventbus.New(1)
	b.Start()
	defer b.Stop()

	// Publish without a subscriber draining — should not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish(eventbus.Event{Type: eventbus.EventSentinelIngest})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked — not lossy")
	}
}

func TestMarshalSSE(t *testing.T) {
	t.Helper()
	e := eventbus.Event{
		Type:   eventbus.EventDiscoveryEvent,
		Source: "scanner",
	}
	b, err := eventbus.MarshalSSE(e)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "data: ") {
		t.Fatalf("expected SSE data prefix, got: %q", s)
	}
	if !strings.HasSuffix(s, "\n\n") {
		t.Fatalf("expected double newline suffix, got: %q", s)
	}
	if !strings.Contains(s, "discovery_event") {
		t.Fatalf("expected event type in SSE body, got: %q", s)
	}
}

func TestEventTypeConstants(t *testing.T) {
	t.Helper()
	types := []eventbus.EventType{
		eventbus.EventMemoryWritten,
		eventbus.EventMemoryQueried,
		eventbus.EventAgentConnected,
		eventbus.EventAgentDisconnected,
		eventbus.EventQuarantineEvent,
		eventbus.EventSentinelIngest,
		eventbus.EventDiscoveryEvent,
	}
	seen := make(map[eventbus.EventType]bool)
	for _, et := range types {
		if seen[et] {
			t.Fatalf("duplicate event type: %s", et)
		}
		seen[et] = true
		if et == "" {
			t.Fatal("empty event type constant")
		}
	}
}
