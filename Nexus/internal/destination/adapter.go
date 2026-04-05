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

// Package destination defines the DestinationWriter interface and canonical
// write envelope (TranslatedPayload) consumed by all memory backends.
//
// The WAL entry Payload field (json.RawMessage) is deserialized to
// TranslatedPayload by the queue worker before calling Write. Backends MUST
// treat payloads as idempotent: re-delivery of the same PayloadID must not
// produce a duplicate record.
package destination

import "time"

// TranslatedPayload is the canonical write envelope produced by the field
// mapping and transform stages of the write path. It is stored in the WAL
// Payload field as raw JSON and deserialized by the queue worker before
// handing off to a DestinationWriter.
//
// Reference: Tech Spec Section 7.1.
type TranslatedPayload struct {
	PayloadID        string            `json:"payload_id"`
	RequestID        string            `json:"request_id"`
	Source           string            `json:"source"`
	Subject          string            `json:"subject"`
	Namespace        string            `json:"namespace"`
	Destination      string            `json:"destination"`
	Collection       string            `json:"collection"`
	Content          string            `json:"content"`
	Model            string            `json:"model"`
	Role             string            `json:"role"`
	Timestamp        time.Time         `json:"timestamp"`
	IdempotencyKey   string            `json:"idempotency_key"`
	SchemaVersion    int               `json:"schema_version"`
	TransformVersion string            `json:"transform_version"`
	ActorType        string            `json:"actor_type"`  // user, agent, or system
	ActorID          string            `json:"actor_id"`    // identity of the actor
	Embedding        []float32         `json:"embedding,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// DestinationWriter is the interface satisfied by every memory backend
// (SQLite, PostgreSQL, Supabase, etc.). All implementations MUST be safe for
// concurrent use by multiple goroutines.
//
// Reference: Tech Spec Section 3.2.
type DestinationWriter interface {
	// Write persists p to the destination. Implementations MUST be idempotent:
	// writing the same PayloadID twice must succeed without producing a
	// duplicate record.
	Write(p TranslatedPayload) error

	// Ping verifies the destination is reachable and healthy. Used by the
	// doctor command and /ready health endpoint.
	Ping() error

	// Exists reports whether a record with the given payloadID has been
	// written to the destination. Used by consistency assertions (Phase R-10).
	Exists(payloadID string) (bool, error)

	// Close releases all resources held by the destination. Safe to call once.
	Close() error
}
