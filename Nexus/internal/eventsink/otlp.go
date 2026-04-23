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
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// deliverOTLP sends an event to an OpenTelemetry Collector via OTLP/HTTP
// JSON encoding. Posts to {url}/v1/logs.
//
// URL format: "otlp://host:port" or "http://host:port" (port 4318 is standard).
//
// No OTel SDK dependency — hand-crafted JSON payload matching the OTLP
// LogsService ExportLogsServiceRequest schema.
//
// Reference: v0.1.3 Build Plan Section 6.4.
func (s *Sink) deliverOTLP(sink *SinkConfig, e Event) {
	baseURL := sink.URL
	if strings.HasPrefix(baseURL, "otlp://") {
		baseURL = "http://" + strings.TrimPrefix(baseURL, "otlp://")
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/v1/logs"

	timeout := time.Duration(sink.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	// Build OTLP LogRecord.
	logRecord := map[string]interface{}{
		"timeUnixNano":         e.Timestamp.UnixNano(),
		"severityNumber":       9, // INFO
		"severityText":         "INFO",
		"body":                 map[string]interface{}{"stringValue": e.EventType},
		"attributes":           otlpAttributes(e),
	}

	payload := map[string]interface{}{
		"resourceLogs": []interface{}{
			map[string]interface{}{
				"resource": map[string]interface{}{
					"attributes": []interface{}{
						otlpKV("service.name", "nexus-nexus"),
					},
				},
				"scopeLogs": []interface{}{
					map[string]interface{}{
						"scope": map[string]interface{}{
							"name":    "nexus.eventsink",
							"version": "0.1.0",
						},
						"logRecords": []interface{}{logRecord},
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		s.metrics.IncFailed()
		return
	}

	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		s.metrics.IncFailed()
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply custom headers (e.g. API keys for managed collectors).
	for k, v := range sink.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		s.logger.Debug("eventsink: otlp delivery failed",
			"component", "events",
			"sink", sink.Name,
			"endpoint", endpoint,
			"error", err,
		)
		s.metrics.IncFailed()
		return
	}
	_ = resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		s.metrics.IncDelivered()
	} else {
		s.logger.Debug("eventsink: otlp non-2xx response",
			"component", "events",
			"sink", sink.Name,
			"status", resp.StatusCode,
		)
		s.metrics.IncFailed()
	}
}

// otlpAttributes converts an Event to OTLP KeyValue attributes.
func otlpAttributes(e Event) []interface{} {
	return []interface{}{
		otlpKV("event_type", e.EventType),
		otlpKV("payload_id", e.PayloadID),
		otlpKV("source", e.Source),
		otlpKV("subject", e.Subject),
		otlpKV("destination", e.Destination),
		otlpKV("actor_type", e.ActorType),
		otlpKV("actor_id", e.ActorID),
	}
}

// otlpKV creates an OTLP KeyValue pair with a string value.
func otlpKV(key, value string) map[string]interface{} {
	return map[string]interface{}{
		"key":   key,
		"value": map[string]interface{}{"stringValue": value},
	}
}
