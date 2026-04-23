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
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

// deliverSyslog sends an event as an RFC 5424 syslog message over UDP or TCP.
// URL format: "syslog://host:port" (UDP) or "syslog+tcp://host:port" (TCP).
//
// Reference: v0.1.3 Build Plan Section 6.4.
func (s *Sink) deliverSyslog(sink *SinkConfig, e Event) {
	msg := formatSyslogMessage(sink, e)

	network := "udp"
	addr := sink.URL
	if strings.HasPrefix(addr, "syslog+tcp://") {
		network = "tcp"
		addr = strings.TrimPrefix(addr, "syslog+tcp://")
	} else if strings.HasPrefix(addr, "syslog://") {
		addr = strings.TrimPrefix(addr, "syslog://")
	}

	timeout := time.Duration(sink.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	conn, err := net.DialTimeout(network, addr, timeout)
	if err != nil {
		s.logger.Debug("eventsink: syslog connection failed",
			"component", "events",
			"sink", sink.Name,
			"network", network,
			"addr", addr,
			"error", err,
		)
		s.metrics.IncFailed()
		return
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte(msg)); err != nil {
		s.logger.Debug("eventsink: syslog write failed",
			"component", "events",
			"sink", sink.Name,
			"error", err,
		)
		s.metrics.IncFailed()
		return
	}

	s.metrics.IncDelivered()
}

// formatSyslogMessage creates an RFC 5424 formatted syslog message.
func formatSyslogMessage(sink *SinkConfig, e Event) string {
	facility := sink.Facility
	if facility == "" {
		facility = "local0"
	}
	tag := sink.Tag
	if tag == "" {
		tag = "nexus"
	}

	// RFC 5424: <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID SD MSG
	pri := syslogPriority(facility, "info")
	ts := e.Timestamp.UTC().Format(time.RFC3339Nano)

	// Structured data with event details.
	payload, _ := json.Marshal(map[string]string{
		"event_type": e.EventType,
		"payload_id": e.PayloadID,
		"source":     e.Source,
		"subject":    e.Subject,
	})

	return fmt.Sprintf("<%d>1 %s - %s - - - %s\n", pri, ts, tag, payload)
}

// syslogPriority computes RFC 5424 PRI value from facility and severity.
func syslogPriority(facility, severity string) int {
	facilityCode := 16 // local0 default
	switch facility {
	case "local0":
		facilityCode = 16
	case "local1":
		facilityCode = 17
	case "local2":
		facilityCode = 18
	case "local3":
		facilityCode = 19
	case "local4":
		facilityCode = 20
	case "local5":
		facilityCode = 21
	case "local6":
		facilityCode = 22
	case "local7":
		facilityCode = 23
	case "user":
		facilityCode = 1
	}

	severityCode := 6 // info
	switch severity {
	case "emerg":
		severityCode = 0
	case "alert":
		severityCode = 1
	case "crit":
		severityCode = 2
	case "err":
		severityCode = 3
	case "warning":
		severityCode = 4
	case "notice":
		severityCode = 5
	case "info":
		severityCode = 6
	case "debug":
		severityCode = 7
	}

	return facilityCode*8 + severityCode
}
