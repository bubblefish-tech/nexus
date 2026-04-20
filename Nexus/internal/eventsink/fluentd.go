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
	"net"
	"strings"
	"time"
)

// deliverFluentd sends an event to a Fluent Bit/Fluentd collector over TCP
// using JSON-mode forward protocol (one JSON line per event).
//
// URL format: "fluentd://host:port" (default port 24224).
//
// The JSON line format is compatible with Fluent Bit's `in_tcp` plugin in
// JSON mode: {"tag":"...", "time":unix, "record":{...}}
//
// Reference: v0.1.3 Build Plan Section 6.4.
func (s *Sink) deliverFluentd(sink *SinkConfig, e Event) {
	addr := sink.URL
	if strings.HasPrefix(addr, "fluentd://") {
		addr = strings.TrimPrefix(addr, "fluentd://")
	}

	tag := sink.Tag
	if tag == "" {
		tag = "nexus.events"
	}

	timeout := time.Duration(sink.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	record := map[string]interface{}{
		"event_type":  e.EventType,
		"payload_id":  e.PayloadID,
		"source":      e.Source,
		"subject":     e.Subject,
		"destination": e.Destination,
		"actor_type":  e.ActorType,
		"actor_id":    e.ActorID,
	}
	if sink.Content == "full" && e.Content != nil {
		record["content"] = json.RawMessage(e.Content)
	}

	msg := map[string]interface{}{
		"tag":    tag,
		"time":   e.Timestamp.Unix(),
		"record": record,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		s.metrics.IncFailed()
		return
	}
	data = append(data, '\n')

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		s.logger.Debug("eventsink: fluentd connection failed",
			"component", "events",
			"sink", sink.Name,
			"addr", addr,
			"error", err,
		)
		s.metrics.IncFailed()
		return
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(data); err != nil {
		s.logger.Debug("eventsink: fluentd write failed",
			"component", "events",
			"sink", sink.Name,
			"error", err,
		)
		s.metrics.IncFailed()
		return
	}

	s.metrics.IncDelivered()
}
