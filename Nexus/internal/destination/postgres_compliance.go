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

package destination

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Compile-time proof that PostgresDestination satisfies the Destination interface.
var _ Destination = (*PostgresDestination)(nil)

// Name returns the stable identifier for this destination.
func (d *PostgresDestination) Name() string { return "postgres" }

// Read retrieves a single memory record by its PayloadID. Returns nil, nil when
// the record does not exist.
func (d *PostgresDestination) Read(ctx context.Context, id string) (*Memory, error) {
	const q = `SELECT payload_id, request_id, source, subject, namespace, destination,
		collection, content, model, role, timestamp, idempotency_key,
		schema_version, transform_version, actor_type, actor_id, metadata,
		sensitivity_labels, classification_tier, tier
		FROM memories WHERE payload_id = $1 LIMIT 1`

	row := d.db.QueryRowContext(ctx, q, id)

	var tp TranslatedPayload
	var metadataStr, sensitivityLabelsStr string

	err := row.Scan(
		&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
		&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
		&tp.Timestamp, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
		&tp.ActorType, &tp.ActorID, &metadataStr,
		&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("destination: postgres: read %q: %w", id, err)
	}

	tp.SensitivityLabels = parsePGTextArray(sensitivityLabelsStr)
	if metadataStr != "" && metadataStr != "{}" {
		_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
	}

	return &tp, nil
}

// Search returns memories matching query, converting the result to a slice of
// pointers. Returns an empty (non-nil) slice when no records match.
func (d *PostgresDestination) Search(ctx context.Context, query *Query) ([]*Memory, error) {
	_ = ctx
	result, err := d.Query(*query)
	if err != nil {
		return nil, err
	}
	out := make([]*Memory, len(result.Records))
	for i := range result.Records {
		cp := result.Records[i]
		out[i] = &cp
	}
	return out, nil
}

// Delete removes the record with the given PayloadID. Deletion of a
// non-existent ID is a no-op (idempotent).
func (d *PostgresDestination) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM memories WHERE payload_id = $1`
	if _, err := d.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("destination: postgres: delete %q: %w", id, err)
	}
	return nil
}

// VectorSearch returns up to limit memories ranked by cosine similarity to
// embedding. Delegates to SemanticSearch with an empty QueryParams so no
// namespace/destination filter is applied.
func (d *PostgresDestination) VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*Memory, error) {
	if len(embedding) == 0 {
		return []*Memory{}, nil
	}
	params := QueryParams{Limit: limit}
	scored, err := d.SemanticSearch(ctx, embedding, params)
	if err != nil {
		return nil, err
	}
	out := make([]*Memory, len(scored))
	for i := range scored {
		cp := scored[i].Payload
		out[i] = &cp
	}
	return out, nil
}

// Migrate is a no-op for PostgreSQL: all schema migrations are applied
// idempotently at open time inside applySchema. The version argument is
// reserved for future use when explicit versioned migrations are required.
func (d *PostgresDestination) Migrate(_ context.Context, _ int) error {
	return nil
}

// Health performs a lightweight liveness probe by pinging the database and
// measuring round-trip latency. Does NOT modify any stored data.
func (d *PostgresDestination) Health(ctx context.Context) (*HealthStatus, error) {
	start := time.Now()
	if err := d.db.PingContext(ctx); err != nil {
		return &HealthStatus{
			OK:    false,
			Error: err.Error(),
		}, nil
	}
	return &HealthStatus{
		OK:      true,
		Latency: time.Since(start),
	}, nil
}
