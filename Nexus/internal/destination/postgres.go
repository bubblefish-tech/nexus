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
	"log/slog"
	"strings"

	// jackc/pgx/v5 stdlib adapter provides the "pgx" driver for database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
)

const pgxDriverName = "pgx"

// createPostgresMemoriesTableFmt is the DDL for the memories table. The %d
// placeholder is replaced with the configured embedding dimensions at runtime.
//
// The embedding column is a vector type from the pgvector extension. Callers
// must ensure pgvector is installed in the target database; see
// https://github.com/pgvector/pgvector.
//
// All data queries use parameterized placeholders — no string concatenation.
// The %d here is safe because it is an integer from the validated config, not
// user input.
const createPostgresMemoriesTableFmt = `
CREATE TABLE IF NOT EXISTS memories (
    payload_id        TEXT        PRIMARY KEY,
    request_id        TEXT        NOT NULL DEFAULT '',
    source            TEXT        NOT NULL DEFAULT '',
    subject           TEXT        NOT NULL DEFAULT '',
    namespace         TEXT        NOT NULL DEFAULT '',
    destination       TEXT        NOT NULL DEFAULT '',
    collection        TEXT        NOT NULL DEFAULT '',
    content           TEXT        NOT NULL DEFAULT '',
    model             TEXT        NOT NULL DEFAULT '',
    role              TEXT        NOT NULL DEFAULT '',
    timestamp         TIMESTAMPTZ NOT NULL DEFAULT now(),
    idempotency_key   TEXT        NOT NULL DEFAULT '',
    schema_version    INTEGER     NOT NULL DEFAULT 0,
    transform_version TEXT        NOT NULL DEFAULT '',
    actor_type        TEXT        NOT NULL DEFAULT '',
    actor_id          TEXT        NOT NULL DEFAULT '',
    metadata          JSONB       NOT NULL DEFAULT '{}',
    embedding         vector(%d),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
)`

const createPostgresIdxIdempotency = `
CREATE INDEX IF NOT EXISTS idx_memories_idempotency_key ON memories (idempotency_key)`

const createPostgresIdxEmbedding = `
CREATE INDEX IF NOT EXISTS idx_memories_embedding ON memories
    USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)`

// addPostgresSensitivityLabelsColumn adds the TEXT[] column for sensitivity labels.
// Reference: Tech Spec Addendum Section A4.3.
const addPostgresSensitivityLabelsColumn = `ALTER TABLE memories ADD COLUMN IF NOT EXISTS sensitivity_labels TEXT[] DEFAULT '{}'`

// addPostgresClassificationTierColumn adds the tier column.
// Reference: Tech Spec Addendum Section A4.3.
const addPostgresClassificationTierColumn = `ALTER TABLE memories ADD COLUMN IF NOT EXISTS classification_tier TEXT DEFAULT 'public'`

// createPostgresIdxClassification adds a B-tree index on classification_tier.
const createPostgresIdxClassification = `CREATE INDEX IF NOT EXISTS idx_memories_classification ON memories(classification_tier)`

// createPostgresIdxSensitivity adds a GIN index on the sensitivity_labels array.
const createPostgresIdxSensitivity = `CREATE INDEX IF NOT EXISTS idx_memories_sensitivity ON memories USING GIN(sensitivity_labels)`

// PostgresDestination writes TranslatedPayload records to a PostgreSQL database
// with pgvector support for Stage 4 semantic retrieval.
//
// The embedding column stores a pgvector vector. When a payload carries no
// embedding, the column is NULL and CanSemanticSearch returns false for that
// record (but the destination as a whole may still support semantic search if
// other records have embeddings).
//
// All SQL uses parameterized statements. Write is idempotent via
// INSERT ... ON CONFLICT DO NOTHING.
//
// Reference: Tech Spec Section 3.4 — Stage 4, Phase 5 Behavioral Contract 3.
type PostgresDestination struct {
	db         *sql.DB
	dimensions int
	logger     *slog.Logger
}

// OpenPostgres opens a connection to the PostgreSQL database identified by dsn,
// enables the pgvector extension, and creates the memories schema.
//
// dsn is a libpq-style connection string or a pgx DSN
// (e.g. "postgres://user:pass@host:5432/db").
// dimensions is the embedding vector size (e.g. 1536 for text-embedding-3-small).
func OpenPostgres(dsn string, dimensions int, logger *slog.Logger) (*PostgresDestination, error) {
	if logger == nil {
		panic("destination: postgres logger must not be nil")
	}
	if dimensions <= 0 {
		dimensions = 1536
	}

	db, err := sql.Open(pgxDriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("destination: postgres: open: %w", err)
	}

	d := &PostgresDestination{db: db, dimensions: dimensions, logger: logger}
	if err := d.applySchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	logger.Info("destination: postgres opened",
		"component", "destination",
		"dimensions", dimensions,
	)
	return d, nil
}

// applySchema enables pgvector and creates the memories table + indexes.
func (d *PostgresDestination) applySchema() error {
	// Enable pgvector extension. Requires the extension to be installed.
	if _, err := d.db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return fmt.Errorf("destination: postgres: create extension vector: %w", err)
	}

	//nolint:gosec // dimensions is a validated int from config, not user input.
	ddl := fmt.Sprintf(createPostgresMemoriesTableFmt, d.dimensions)
	if _, err := d.db.Exec(ddl); err != nil {
		return fmt.Errorf("destination: postgres: create memories table: %w", err)
	}
	if _, err := d.db.Exec(createPostgresIdxIdempotency); err != nil {
		return fmt.Errorf("destination: postgres: create idempotency index: %w", err)
	}

	// IVFFlat index requires at least some rows to train; ignore errors on
	// an empty table (index can be created later by the operator).
	if _, err := d.db.Exec(createPostgresIdxEmbedding); err != nil {
		d.logger.Warn("destination: postgres: create embedding index (may require data first)",
			"component", "destination",
			"error", err,
		)
	}

	// Idempotent migration: add sensitivity_labels and classification_tier
	// columns for the Retrieval Firewall. Existing rows get safe defaults.
	// Reference: Tech Spec Addendum Section A4.3.
	if _, err := d.db.Exec(addPostgresSensitivityLabelsColumn); err != nil {
		return fmt.Errorf("destination: postgres: add sensitivity_labels column: %w", err)
	}
	if _, err := d.db.Exec(addPostgresClassificationTierColumn); err != nil {
		return fmt.Errorf("destination: postgres: add classification_tier column: %w", err)
	}
	if _, err := d.db.Exec(createPostgresIdxClassification); err != nil {
		return fmt.Errorf("destination: postgres: create classification index: %w", err)
	}
	if _, err := d.db.Exec(createPostgresIdxSensitivity); err != nil {
		return fmt.Errorf("destination: postgres: create sensitivity GIN index: %w", err)
	}

	return nil
}

// Write persists p to the memories table. Idempotent via ON CONFLICT DO NOTHING.
// If p.Embedding is non-empty it is stored as a pgvector value; otherwise NULL.
// All values are bound via parameterized placeholders.
func (d *PostgresDestination) Write(p TranslatedPayload) error {
	metadataJSON, err := marshalMetadata(p.Metadata)
	if err != nil {
		return fmt.Errorf("destination: postgres: marshal metadata: %w", err)
	}

	var embeddingVal interface{}
	if len(p.Embedding) > 0 {
		embeddingVal = float32SliceToPGVector(p.Embedding)
	}

	// Encode sensitivity_labels as PostgreSQL TEXT[] literal.
	// Reference: Tech Spec Addendum Section A3.2.
	sensitivityLabelsArr := pgTextArray(p.SensitivityLabels)
	classificationTier := p.ClassificationTier
	if classificationTier == "" {
		classificationTier = "public"
	}

	const query = `
INSERT INTO memories (
    payload_id, request_id, source, subject, namespace, destination,
    collection, content, model, role, timestamp, idempotency_key,
    schema_version, transform_version, actor_type, actor_id, metadata, embedding,
    sensitivity_labels, classification_tier
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
ON CONFLICT (payload_id) DO NOTHING`

	_, err = d.db.Exec(query,
		p.PayloadID,
		p.RequestID,
		p.Source,
		p.Subject,
		p.Namespace,
		p.Destination,
		p.Collection,
		p.Content,
		p.Model,
		p.Role,
		p.Timestamp.UTC(),
		p.IdempotencyKey,
		p.SchemaVersion,
		p.TransformVersion,
		p.ActorType,
		p.ActorID,
		metadataJSON,
		embeddingVal,
		sensitivityLabelsArr,
		classificationTier,
	)
	if err != nil {
		return fmt.Errorf("destination: postgres: write payload_id %q: %w", p.PayloadID, err)
	}

	d.logger.Debug("destination: postgres: write",
		"component", "destination",
		"payload_id", p.PayloadID,
	)
	return nil
}

// Ping verifies the database connection is alive.
func (d *PostgresDestination) Ping() error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("destination: postgres: ping: %w", err)
	}
	return nil
}

// Exists reports whether a record with payloadID exists in the memories table.
func (d *PostgresDestination) Exists(payloadID string) (bool, error) {
	const query = `SELECT 1 FROM memories WHERE payload_id = $1 LIMIT 1`
	var dummy int
	err := d.db.QueryRow(query, payloadID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("destination: postgres: exists %q: %w", payloadID, err)
	}
	return true, nil
}

// Query returns a page of memories using parameterized structured queries.
func (d *PostgresDestination) Query(params QueryParams) (QueryResult, error) {
	limit := ClampLimit(params.Limit)
	offset, err := DecodeCursor(params.Cursor)
	if err != nil {
		return QueryResult{}, fmt.Errorf("destination: postgres: query: %w", err)
	}

	args := make([]interface{}, 0, 6)
	conditions := make([]string, 0, 4)
	idx := 1

	if params.Namespace != "" {
		conditions = append(conditions, fmt.Sprintf("namespace = $%d", idx))
		args = append(args, params.Namespace)
		idx++
	}
	if params.Destination != "" {
		conditions = append(conditions, fmt.Sprintf("destination = $%d", idx))
		args = append(args, params.Destination)
		idx++
	}
	if params.Subject != "" {
		conditions = append(conditions, fmt.Sprintf("subject = $%d", idx))
		args = append(args, params.Subject)
		idx++
	}
	if params.Q != "" {
		conditions = append(conditions, fmt.Sprintf("content ILIKE $%d", idx))
		args = append(args, "%"+params.Q+"%")
		idx++
	}
	if params.ActorType != "" {
		conditions = append(conditions, fmt.Sprintf("actor_type = $%d", idx))
		args = append(args, params.ActorType)
		idx++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	fetchLimit := limit + 1
	args = append(args, fetchLimit, offset)

	//nolint:gosec // whereClause built from fixed condition strings, no user input.
	q := fmt.Sprintf(
		"SELECT payload_id, request_id, source, subject, namespace, destination, collection, content, model, role, timestamp, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier FROM memories %s ORDER BY timestamp DESC LIMIT $%d OFFSET $%d",
		whereClause, idx, idx+1,
	)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return QueryResult{}, fmt.Errorf("destination: postgres: query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("close rows", "err", err)
		}
	}()

	records := make([]TranslatedPayload, 0, limit)
	for rows.Next() {
		var tp TranslatedPayload
		var metadataStr string
		var sensitivityLabelsStr string

		if err := rows.Scan(
			&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
			&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
			&tp.Timestamp, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
			&tp.ActorType, &tp.ActorID, &metadataStr,
			&sensitivityLabelsStr, &tp.ClassificationTier,
		); err != nil {
			return QueryResult{}, fmt.Errorf("destination: postgres: query: scan: %w", err)
		}
		tp.SensitivityLabels = parsePGTextArray(sensitivityLabelsStr)
		if metadataStr != "" && metadataStr != "{}" {
			_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
		}
		records = append(records, tp)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("destination: postgres: query: rows: %w", err)
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	var nextCursor string
	if hasMore {
		nextCursor = EncodeCursor(offset + limit)
	}
	return QueryResult{Records: records, NextCursor: nextCursor, HasMore: hasMore}, nil
}

// CanSemanticSearch reports whether any row in the memories table has a
// non-NULL embedding, indicating the pgvector index is usable.
func (d *PostgresDestination) CanSemanticSearch() bool {
	const q = `SELECT 1 FROM memories WHERE embedding IS NOT NULL LIMIT 1`
	var dummy int
	err := d.db.QueryRow(q).Scan(&dummy)
	return err == nil
}

// SemanticSearch performs a pgvector cosine similarity search.
// It issues a parameterized ORDER BY embedding <=> $1 query using the ivfflat
// index created during schema setup.
//
// Reference: Tech Spec Section 3.4 — Stage 4.
func (d *PostgresDestination) SemanticSearch(ctx context.Context, vec []float32, params QueryParams) ([]ScoredRecord, error) {
	limit := ClampLimit(params.Limit)

	args := make([]interface{}, 0, 5)
	conditions := make([]string, 0, 3)
	conditions = append(conditions, "embedding IS NOT NULL")

	pgVec := float32SliceToPGVector(vec)
	args = append(args, pgVec)
	idx := 2 // $1 is the query vector

	if params.Namespace != "" {
		conditions = append(conditions, fmt.Sprintf("namespace = $%d", idx))
		args = append(args, params.Namespace)
		idx++
	}
	if params.Destination != "" {
		conditions = append(conditions, fmt.Sprintf("destination = $%d", idx))
		args = append(args, params.Destination)
		idx++
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, limit)

	//nolint:gosec // whereClause built from fixed condition strings.
	q := fmt.Sprintf(
		"SELECT payload_id, request_id, source, subject, namespace, destination, collection, content, model, role, timestamp, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, 1 - (embedding <=> $1) AS score FROM memories %s ORDER BY embedding <=> $1 LIMIT $%d",
		whereClause, idx,
	)

	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("destination: postgres: semantic search: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("close rows", "err", err)
		}
	}()

	scored := make([]ScoredRecord, 0, limit)
	for rows.Next() {
		var tp TranslatedPayload
		var metadataStr string
		var sensitivityLabelsStr string
		var score float32

		if err := rows.Scan(
			&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
			&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
			&tp.Timestamp, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
			&tp.ActorType, &tp.ActorID, &metadataStr,
			&sensitivityLabelsStr, &tp.ClassificationTier, &score,
		); err != nil {
			return nil, fmt.Errorf("destination: postgres: semantic search: scan: %w", err)
		}
		tp.SensitivityLabels = parsePGTextArray(sensitivityLabelsStr)
		if metadataStr != "" && metadataStr != "{}" {
			_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
		}
		scored = append(scored, ScoredRecord{Payload: tp, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("destination: postgres: semantic search: rows: %w", err)
	}
	return scored, nil
}

// Close closes the database connection pool.
func (d *PostgresDestination) Close() error {
	if err := d.db.Close(); err != nil {
		return fmt.Errorf("destination: postgres: close: %w", err)
	}
	return nil
}

// parsePGTextArray parses a PostgreSQL TEXT[] literal string (e.g. "{pii,financial}")
// back to a Go string slice. Returns nil for empty arrays "{}".
func parsePGTextArray(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return nil
	}
	// Strip surrounding braces.
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// pgTextArray formats a string slice as a PostgreSQL TEXT[] literal.
// Example: ["pii","financial"] → "{pii,financial}". Nil/empty → "{}".
func pgTextArray(labels []string) string {
	if len(labels) == 0 {
		return "{}"
	}
	return "{" + strings.Join(labels, ",") + "}"
}

// float32SliceToPGVector formats a []float32 as a pgvector literal string
// compatible with the $N placeholder syntax used by pgx.
// Format: "[v0,v1,...,vN]"
func float32SliceToPGVector(v []float32) string {
	if len(v) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%g", f)
	}
	b.WriteByte(']')
	return b.String()
}
