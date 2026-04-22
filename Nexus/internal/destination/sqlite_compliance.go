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
	"fmt"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/migration"
)

// Compile-time proof that SQLiteDestination satisfies the Destination interface.
var _ Destination = (*SQLiteDestination)(nil)

// Name returns the stable identifier for this destination.
func (d *SQLiteDestination) Name() string { return "sqlite" }

// Read retrieves a single memory record by its PayloadID. Returns nil, nil when
// the record does not exist.
func (d *SQLiteDestination) Read(ctx context.Context, id string) (*Memory, error) {
	const q = `SELECT payload_id, request_id, source, subject, namespace, destination,
		collection, content, model, role, timestamp, idempotency_key,
		schema_version, transform_version, actor_type, actor_id, metadata,
		sensitivity_labels, classification_tier, tier, lsh_bucket, cluster_id, cluster_role,
		signature, signing_key_id, signature_alg,
		content_encrypted, metadata_encrypted, encryption_version
		FROM memories WHERE payload_id = ? LIMIT 1`

	row := d.db.QueryRowContext(ctx, q, id)

	var tp TranslatedPayload
	var timestampStr, metadataStr, sensitivityLabelsStr string
	var lshBucket sql.NullInt64
	var contentEnc, metaEnc []byte
	var encVersion int

	err := row.Scan(
		&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
		&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
		&timestampStr, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
		&tp.ActorType, &tp.ActorID, &metadataStr,
		&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
		&lshBucket, &tp.ClusterID, &tp.ClusterRole,
		&tp.Signature, &tp.SigningKeyID, &tp.SignatureAlg,
		&contentEnc, &metaEnc, &encVersion,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("destination: sqlite: read %q: %w", id, err)
	}

	if lshBucket.Valid {
		tp.LSHBucket = int(lshBucket.Int64)
	}
	if sensitivityLabelsStr != "" {
		tp.SensitivityLabels = strings.Split(sensitivityLabelsStr, ",")
	}
	if t, parseErr := parseTimestamp(timestampStr); parseErr == nil {
		tp.Timestamp = t
	}
	d.decryptPayload(&tp, metadataStr, contentEnc, metaEnc, encVersion)

	return &tp, nil
}

// Search returns memories matching query, converting the result to a slice of
// pointers. Returns an empty (non-nil) slice when no records match.
func (d *SQLiteDestination) Search(ctx context.Context, query *Query) ([]*Memory, error) {
	_ = ctx // SQLite queries are synchronous; ctx honoured by Read/Write paths
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
func (d *SQLiteDestination) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM memories WHERE payload_id = ?`
	if _, err := d.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("destination: sqlite: delete %q: %w", id, err)
	}
	return nil
}

// VectorSearch returns up to limit memories ranked by cosine similarity to
// embedding. Delegates to SemanticSearch with an empty QueryParams so no
// namespace/destination filter is applied (callers add filters via params if
// needed — use Search for filtered retrieval).
func (d *SQLiteDestination) VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*Memory, error) {
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

// sqliteMigrations is the versioned migration history for the SQLite memories schema.
// v1 carries no SQL because applyPragmasAndSchema() already created the schema at
// open time; recording v1 in nexus_migrations marks the baseline for future versions.
var sqliteMigrations = []migration.Migration{
	{Version: 1, Description: "initial memories schema"},
	{Version: 2, Description: "FTS5 virtual table for BM25 sparse retrieval", SQL: migration.FTS5BM25SQL},
	{Version: 3, Description: "temporal bins column with composite index", SQL: migration.TemporalBinsSQL},
}

// Migrate creates the nexus_migrations tracking table (if needed) and applies
// any pending versioned schema migrations. The version argument is unused
// (all pending migrations are always applied).
func (d *SQLiteDestination) Migrate(ctx context.Context, _ int) error {
	mgr := migration.New(d.db)
	return mgr.Apply(ctx, sqliteMigrations)
}

// Health performs a lightweight liveness probe by pinging the database and
// measuring round-trip latency. Does NOT modify any stored data.
func (d *SQLiteDestination) Health(ctx context.Context) (*HealthStatus, error) {
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
