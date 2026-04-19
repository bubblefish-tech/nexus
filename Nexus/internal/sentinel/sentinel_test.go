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

package sentinel

import (
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/wal"
)

type mockWAL struct {
	entries []wal.Entry
}

func (m *mockWAL) SampleDelivered(count int) ([]wal.Entry, error) {
	if count > len(m.entries) {
		count = len(m.entries)
	}
	return m.entries[:count], nil
}

type mockDest struct {
	existing map[string]bool
}

func (m *mockDest) Exists(payloadID string) (bool, error) {
	return m.existing[payloadID], nil
}

func TestSentinelDetectsAnomaly(t *testing.T) {
	mw := &mockWAL{entries: []wal.Entry{
		{PayloadID: "p1", Source: "test", Destination: "sqlite"},
		{PayloadID: "p2", Source: "test", Destination: "sqlite"},
		{PayloadID: "p3", Source: "test", Destination: "sqlite"},
	}}
	md := &mockDest{existing: map[string]bool{
		"p1": true,
		"p2": false, // missing!
		"p3": true,
	}}

	s := New(Config{
		WAL:        mw,
		Dest:       md,
		Interval:   50 * time.Millisecond,
		SampleSize: 3,
	})

	s.Start()
	time.Sleep(150 * time.Millisecond)
	s.Stop()

	if s.ChecksTotal() == 0 {
		t.Error("expected checks > 0")
	}
	if s.AnomaliesTotal() == 0 {
		t.Error("expected anomalies > 0 for missing entry p2")
	}
}

func TestSentinelAllPresent(t *testing.T) {
	mw := &mockWAL{entries: []wal.Entry{
		{PayloadID: "p1"},
		{PayloadID: "p2"},
	}}
	md := &mockDest{existing: map[string]bool{
		"p1": true,
		"p2": true,
	}}

	s := New(Config{
		WAL:        mw,
		Dest:       md,
		Interval:   50 * time.Millisecond,
		SampleSize: 2,
	})

	s.Start()
	time.Sleep(150 * time.Millisecond)
	s.Stop()

	if s.ChecksTotal() == 0 {
		t.Error("expected checks > 0")
	}
	if s.AnomaliesTotal() != 0 {
		t.Errorf("expected 0 anomalies, got %d", s.AnomaliesTotal())
	}
}

func TestSentinelStopDrains(t *testing.T) {
	mw := &mockWAL{}
	md := &mockDest{existing: map[string]bool{}}

	s := New(Config{
		WAL:        mw,
		Dest:       md,
		Interval:   1 * time.Hour, // won't tick
		SampleSize: 1,
	})

	s.Start()
	s.Stop() // should return promptly
	s.Stop() // safe to call twice
}
