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

package events_test

import (
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/events"
)

func TestLiteBus_EmitAndStream(t *testing.T) {
	t.Helper()
	b := events.NewLiteBus(8)
	defer b.Close()

	b.Emit("test_event", map[string]any{"key": "value"})

	select {
	case e := <-b.Stream():
		if e.Type != "test_event" {
			t.Fatalf("got type %q, want %q", e.Type, "test_event")
		}
		if e.Data["key"] != "value" {
			t.Fatalf("got data %v, want key=value", e.Data)
		}
		if e.Timestamp.IsZero() {
			t.Fatal("timestamp should not be zero")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestLiteBus_NonBlockingDrop(t *testing.T) {
	t.Helper()
	b := events.NewLiteBus(2)
	defer b.Close()

	// Fill buffer.
	b.Emit("e1", nil)
	b.Emit("e2", nil)
	// This must not block.
	done := make(chan struct{})
	go func() {
		b.Emit("e3_dropped", nil) // should drop silently
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Emit blocked on full buffer")
	}
}

func TestLiteBus_CloseClosesStream(t *testing.T) {
	t.Helper()
	b := events.NewLiteBus(4)
	b.Close()

	// Stream channel should be closed.
	select {
	case _, ok := <-b.Stream():
		if ok {
			t.Fatal("expected channel to be closed")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout: channel not closed after Close()")
	}
}

func TestLiteBus_EmitAfterClose_NoOp(t *testing.T) {
	t.Helper()
	b := events.NewLiteBus(4)
	b.Close()
	// Must not panic.
	b.Emit("after_close", map[string]any{"x": 1})
}

func TestLiteBus_CloseIdempotent(t *testing.T) {
	t.Helper()
	b := events.NewLiteBus(4)
	b.Close()
	b.Close() // must not panic
}

func TestLiteBus_DefaultBufferSize(t *testing.T) {
	t.Helper()
	b := events.NewLiteBus(0) // 0 → clamped to 256
	defer b.Close()
	b.Emit("e", nil) // must not block
}

func TestLiteEvent_TimestampUTC(t *testing.T) {
	t.Helper()
	b := events.NewLiteBus(4)
	defer b.Close()

	before := time.Now().UTC()
	b.Emit("ts_test", nil)
	after := time.Now().UTC()

	e := <-b.Stream()
	if e.Timestamp.Before(before) || e.Timestamp.After(after) {
		t.Fatalf("timestamp %v outside [%v, %v]", e.Timestamp, before, after)
	}
}
