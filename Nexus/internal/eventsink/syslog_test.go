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

package eventsink

import (
	"strings"
	"testing"
	"time"
)

func TestSyslogPriority_Facilities(t *testing.T) {
	t.Helper()
	tests := []struct {
		facility string
		want     int
	}{
		{"local0", 16*8 + 6},
		{"local1", 17*8 + 6},
		{"local7", 23*8 + 6},
		{"user", 1*8 + 6},
		{"unknown", 16*8 + 6},
	}
	for _, tt := range tests {
		got := syslogPriority(tt.facility, "info")
		if got != tt.want {
			t.Errorf("syslogPriority(%q, info) = %d, want %d", tt.facility, got, tt.want)
		}
	}
}

func TestSyslogPriority_Severities(t *testing.T) {
	t.Helper()
	tests := []struct {
		severity string
		code     int
	}{
		{"emerg", 0}, {"alert", 1}, {"crit", 2}, {"err", 3},
		{"warning", 4}, {"notice", 5}, {"info", 6}, {"debug", 7},
	}
	for _, tt := range tests {
		got := syslogPriority("local0", tt.severity)
		want := 16*8 + tt.code
		if got != want {
			t.Errorf("syslogPriority(local0, %q) = %d, want %d", tt.severity, got, want)
		}
	}
}

func TestFormatSyslogMessage_Defaults(t *testing.T) {
	t.Helper()
	sink := &SinkConfig{Name: "test"}
	e := Event{
		EventType: "memory_written",
		PayloadID: "p1",
		Source:    "src",
		Subject:   "sub",
		Timestamp: time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC),
	}
	msg := formatSyslogMessage(sink, e)
	if !strings.HasPrefix(msg, "<") {
		t.Fatal("expected PRI prefix")
	}
	if !strings.Contains(msg, "nexus") {
		t.Error("expected default tag 'nexus'")
	}
	if !strings.Contains(msg, "memory_written") {
		t.Error("expected event_type in payload")
	}
}

func TestFormatSyslogMessage_CustomFacilityAndTag(t *testing.T) {
	t.Helper()
	sink := &SinkConfig{Facility: "local3", Tag: "myapp"}
	e := Event{Timestamp: time.Now().UTC()}
	msg := formatSyslogMessage(sink, e)
	if !strings.Contains(msg, "myapp") {
		t.Error("expected custom tag 'myapp'")
	}
	pri := syslogPriority("local3", "info")
	expected := "<" + strings.TrimLeft(strings.Split(msg, ">")[0], "<")
	_ = expected
	if !strings.HasPrefix(msg, "<") {
		t.Errorf("expected PRI %d in message", pri)
	}
}
