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

package destination_test

import (
	"testing"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// ---------------------------------------------------------------------------
// ValidActorType
// ---------------------------------------------------------------------------

func TestValidActorType(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"user", true},
		{"agent", true},
		{"system", true},
		{"", false},
		{"admin", false},
		{"USER", false},
		{"Agent", false},
		{"robot", false},
	}
	for _, tt := range tests {
		if got := destination.ValidActorType(tt.input); got != tt.want {
			t.Errorf("ValidActorType(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// SQLite: actor_type query filter
// ---------------------------------------------------------------------------

func TestSQLiteDestination_QueryActorTypeFilter(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	// Write payloads with different actor types.
	for _, at := range []string{"user", "agent", "system"} {
		p := basePayload("prov-" + at)
		p.ActorType = at
		p.ActorID = at + "-actor"
		if err := d.Write(p); err != nil {
			t.Fatalf("Write(%s): %v", at, err)
		}
	}

	// Query with actor_type=user: only user returned.
	result, err := d.Query(destination.QueryParams{
		Namespace: "default",
		Limit:     10,
		ActorType: "user",
	})
	if err != nil {
		t.Fatalf("Query(actor_type=user): %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record for actor_type=user, got %d", len(result.Records))
	}
	if result.Records[0].ActorType != "user" {
		t.Fatalf("expected actor_type=user, got %q", result.Records[0].ActorType)
	}

	// Query with actor_type=agent: only agent returned.
	result, err = d.Query(destination.QueryParams{
		Namespace: "default",
		Limit:     10,
		ActorType: "agent",
	})
	if err != nil {
		t.Fatalf("Query(actor_type=agent): %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record for actor_type=agent, got %d", len(result.Records))
	}
	if result.Records[0].ActorType != "agent" {
		t.Fatalf("expected actor_type=agent, got %q", result.Records[0].ActorType)
	}

	// Query without actor_type filter: all 3 returned.
	result, err = d.Query(destination.QueryParams{
		Namespace: "default",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Query(no filter): %v", err)
	}
	if len(result.Records) != 3 {
		t.Fatalf("expected 3 records without filter, got %d", len(result.Records))
	}
}

// ---------------------------------------------------------------------------
// SQLite: schema migration — existing data survives with empty defaults
// ---------------------------------------------------------------------------

func TestSQLiteDestination_SchemaDefaultActorFields(t *testing.T) {
	d, cleanup := newTestSQLite(t)
	defer cleanup()

	// Simulate pre-migration data by inserting directly with empty actor fields.
	db := destination.ExposeDB(d)
	_, err := db.Exec(`INSERT INTO memories (
		payload_id, request_id, source, subject, namespace, destination,
		collection, content, model, role, timestamp, idempotency_key,
		schema_version, transform_version, actor_type, actor_id, metadata
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', '{}')`,
		"legacy-001", "req-legacy", "src", "user:bob", "default", "sqlite",
		"memories", "old data", "gpt-3.5", "user",
		"2025-01-01T00:00:00Z", "idem-legacy", 1, "v1",
	)
	if err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	// Query should return the row with empty actor fields (no crash).
	result, err := d.Query(destination.QueryParams{
		Namespace: "default",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(result.Records))
	}
	if result.Records[0].ActorType != "" {
		t.Fatalf("expected empty actor_type for legacy data, got %q", result.Records[0].ActorType)
	}
	if result.Records[0].ActorID != "" {
		t.Fatalf("expected empty actor_id for legacy data, got %q", result.Records[0].ActorID)
	}
}
