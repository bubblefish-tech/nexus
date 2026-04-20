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

// Package cockroachdb provides a CockroachDB destination adapter for BubbleFish Nexus.
//
// CockroachDB is PostgreSQL-wire-compatible and uses the pgx driver, but does
// not support the pgvector extension or IVFFlat indexes. Embeddings are stored
// as BYTEA (little-endian float32) and vector search uses application-level
// cosine similarity. All other capabilities (JSONB, TEXT[], TIMESTAMPTZ,
// ON CONFLICT DO NOTHING) work identically to the PostgreSQL adapter.
//
// Default connection string format:
//
//	postgresql://user:pass@host:26257/db?sslmode=require
//
// CockroachDB requires TLS by default; pass sslmode=disable only for local
// insecure clusters used in development.
package cockroachdb

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	// pgx/v5 stdlib adapter registers the "pgx" driver with database/sql.
	// CockroachDB is wire-compatible with PostgreSQL and works with this driver.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// Compile-time proof that CockroachDBDestination satisfies the Destination interface.
var _ destination.Destination = (*CockroachDBDestination)(nil)

// createMemoriesTable is the DDL for the memories table. Uses BYTEA for the
// embedding column because CockroachDB does not ship with the pgvector extension.
// Embeddings are stored as little-endian float32 blobs and ranked app-side.
const createMemoriesTable = `
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
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// Migration DDL. CockroachDB 22.1+ supports ADD COLUMN IF NOT EXISTS, making
// these statements fully idempotent on existing databases.
const (
	addEmbeddingColumn          = `ALTER TABLE memories ADD COLUMN IF NOT EXISTS embedding BYTEA`
	addSensitivityLabelsColumn  = `ALTER TABLE memories ADD COLUMN IF NOT EXISTS sensitivity_labels TEXT[] NOT NULL DEFAULT '{}'`
	addClassificationTierColumn = `ALTER TABLE memories ADD COLUMN IF NOT EXISTS classification_tier TEXT NOT NULL DEFAULT 'public'`
	addTierColumn               = `ALTER TABLE memories ADD COLUMN IF NOT EXISTS tier INTEGER NOT NULL DEFAULT 1`
)

// Index DDL. CREATE INDEX IF NOT EXISTS is supported by CockroachDB.
// The IVFFlat (pgvector) and GIN (PostgreSQL-specific) index types are omitted;
// standard B-tree indexes are used instead.
const (
	createIdxIdempotency    = `CREATE INDEX IF NOT EXISTS idx_memories_idempotency_key ON memories (idempotency_key)`
	createIdxClassification = `CREATE INDEX IF NOT EXISTS idx_memories_classification ON memories (classification_tier)`
	createIdxTier           = `CREATE INDEX IF NOT EXISTS idx_memories_tier ON memories (tier)`
	createIdxQuery          = `CREATE INDEX IF NOT EXISTS idx_memories_query ON memories (namespace, destination, timestamp DESC)`
	createIdxSubject        = `CREATE INDEX IF NOT EXISTS idx_memories_subject ON memories (subject, timestamp DESC)`
)

// CockroachDBDestination writes TranslatedPayload records to a CockroachDB
// cluster. It uses the pgx driver and PostgreSQL SQL syntax with the following
// adaptations for CockroachDB compatibility:
//
//   - No pgvector extension; embeddings stored as BYTEA.
//   - No IVFFlat index; vector search is application-level cosine similarity.
//   - ADD COLUMN IF NOT EXISTS for idempotent schema migrations.
//
// The adapter is safe for concurrent use.
type CockroachDBDestination struct {
	db     *sql.DB
	logger *slog.Logger
}

// Open opens a connection to the CockroachDB cluster identified by dsn,
// creates the memories table, and applies idempotent column migrations.
//
// dsn is a PostgreSQL-style DSN compatible with CockroachDB, e.g.:
// "postgresql://root@localhost:26257/nexus?sslmode=disable"
func Open(dsn string, logger *slog.Logger) (*CockroachDBDestination, error) {
	if logger == nil {
		panic("destination: cockroachdb: logger must not be nil")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("destination: cockroachdb: open: %w", err)
	}

	d := &CockroachDBDestination{db: db, logger: logger}
	if err := d.applySchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	logger.Info("destination: cockroachdb opened", "component", "destination")
	return d, nil
}

// applySchema creates the memories table and applies idempotent migrations.
func (d *CockroachDBDestination) applySchema() error {
	if _, err := d.db.Exec(createMemoriesTable); err != nil {
		return fmt.Errorf("destination: cockroachdb: create memories table: %w", err)
	}

	for _, ddl := range []string{
		addEmbeddingColumn,
		addSensitivityLabelsColumn,
		addClassificationTierColumn,
		addTierColumn,
	} {
		if _, err := d.db.Exec(ddl); err != nil {
			return fmt.Errorf("destination: cockroachdb: schema migration %q: %w", ddl, err)
		}
	}

	for _, ddl := range []string{
		createIdxIdempotency,
		createIdxClassification,
		createIdxTier,
		createIdxQuery,
		createIdxSubject,
	} {
		if _, err := d.db.Exec(ddl); err != nil {
			return fmt.Errorf("destination: cockroachdb: create index %q: %w", ddl, err)
		}
	}

	return nil
}

// Name returns the stable identifier for this destination.
func (d *CockroachDBDestination) Name() string { return "cockroachdb" }

// Write persists p to the memories table. Idempotent via ON CONFLICT DO NOTHING.
// All values are bound via parameterised $N placeholders — no string concatenation.
// If p.Embedding is non-empty it is serialised as a little-endian float32 BYTEA.
func (d *CockroachDBDestination) Write(p destination.TranslatedPayload) error {
	metadataJSON, err := marshalMetadata(p.Metadata)
	if err != nil {
		return fmt.Errorf("destination: cockroachdb: marshal metadata: %w", err)
	}

	var embeddingVal interface{}
	if len(p.Embedding) > 0 {
		embeddingVal = encodeEmbedding(p.Embedding)
	}

	sensitivityLabels := pgTextArray(p.SensitivityLabels)
	classificationTier := p.ClassificationTier
	if classificationTier == "" {
		classificationTier = "public"
	}

	const query = `
INSERT INTO memories (
    payload_id, request_id, source, subject, namespace, destination,
    collection, content, model, role, timestamp, idempotency_key,
    schema_version, transform_version, actor_type, actor_id, metadata, embedding,
    sensitivity_labels, classification_tier, tier
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
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
		sensitivityLabels,
		classificationTier,
		p.Tier,
	)
	if err != nil {
		return fmt.Errorf("destination: cockroachdb: write payload_id %q: %w", p.PayloadID, err)
	}

	d.logger.Debug("destination: cockroachdb: write",
		"component", "destination",
		"payload_id", p.PayloadID,
	)
	return nil
}

// Read retrieves a single memory record by its PayloadID. Returns nil, nil when
// the record does not exist.
func (d *CockroachDBDestination) Read(ctx context.Context, id string) (*destination.Memory, error) {
	const q = `SELECT payload_id, request_id, source, subject, namespace, destination,
		collection, content, model, role, timestamp, idempotency_key,
		schema_version, transform_version, actor_type, actor_id, metadata,
		sensitivity_labels, classification_tier, tier
		FROM memories WHERE payload_id = $1 LIMIT 1`

	row := d.db.QueryRowContext(ctx, q, id)

	var tp destination.TranslatedPayload
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
		return nil, fmt.Errorf("destination: cockroachdb: read %q: %w", id, err)
	}

	tp.SensitivityLabels = parsePGTextArray(sensitivityLabelsStr)
	if metadataStr != "" && metadataStr != "{}" {
		_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
	}
	return &tp, nil
}

// Search returns memories matching query, converting the result to a slice of
// pointers. Returns an empty (non-nil) slice when no records match.
func (d *CockroachDBDestination) Search(ctx context.Context, query *destination.Query) ([]*destination.Memory, error) {
	_ = ctx
	result, err := d.Query(*query)
	if err != nil {
		return nil, err
	}
	out := make([]*destination.Memory, len(result.Records))
	for i := range result.Records {
		cp := result.Records[i]
		out[i] = &cp
	}
	return out, nil
}

// Delete removes the record with the given PayloadID. Deletion of a
// non-existent ID is a no-op (idempotent).
func (d *CockroachDBDestination) Delete(ctx context.Context, id string) error {
	const q = `DELETE FROM memories WHERE payload_id = $1`
	if _, err := d.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("destination: cockroachdb: delete %q: %w", id, err)
	}
	return nil
}

// VectorSearch returns up to limit memories ranked by cosine similarity to
// embedding using application-level computation. Returns an empty slice for a
// nil or empty embedding.
func (d *CockroachDBDestination) VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*destination.Memory, error) {
	if len(embedding) == 0 {
		return []*destination.Memory{}, nil
	}
	params := destination.QueryParams{Limit: limit}
	scored, err := d.SemanticSearch(ctx, embedding, params)
	if err != nil {
		return nil, err
	}
	out := make([]*destination.Memory, len(scored))
	for i := range scored {
		cp := scored[i].Payload
		out[i] = &cp
	}
	return out, nil
}

// Migrate is a no-op for CockroachDB: all schema migrations are applied
// idempotently at open time inside applySchema.
func (d *CockroachDBDestination) Migrate(_ context.Context, _ int) error {
	return nil
}

// Health performs a lightweight liveness probe by pinging the cluster and
// measuring round-trip latency. Does NOT modify any stored data.
func (d *CockroachDBDestination) Health(ctx context.Context) (*destination.HealthStatus, error) {
	start := time.Now()
	if err := d.db.PingContext(ctx); err != nil {
		return &destination.HealthStatus{
			OK:    false,
			Error: err.Error(),
		}, nil
	}
	return &destination.HealthStatus{
		OK:      true,
		Latency: time.Since(start),
	}, nil
}

// Close releases all resources held by the destination.
func (d *CockroachDBDestination) Close() error {
	return d.db.Close()
}

// Ping verifies the cluster connection is alive. Satisfies DestinationWriter.
func (d *CockroachDBDestination) Ping() error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("destination: cockroachdb: ping: %w", err)
	}
	return nil
}

// Exists reports whether a record with payloadID exists in the memories table.
func (d *CockroachDBDestination) Exists(payloadID string) (bool, error) {
	const q = `SELECT 1 FROM memories WHERE payload_id = $1 LIMIT 1`
	var dummy int
	err := d.db.QueryRow(q, payloadID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("destination: cockroachdb: exists %q: %w", payloadID, err)
	}
	return true, nil
}

// Query returns a page of memories using parameterised structured queries.
// Uses PostgreSQL $N placeholders compatible with CockroachDB.
func (d *CockroachDBDestination) Query(params destination.QueryParams) (destination.QueryResult, error) {
	limit := destination.ClampLimit(params.Limit)
	offset, err := destination.DecodeCursor(params.Cursor)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: cockroachdb: query: %w", err)
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
	if params.TierFilter {
		conditions = append(conditions, fmt.Sprintf("tier <= $%d", idx))
		args = append(args, params.SourceTier)
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
		"SELECT payload_id, request_id, source, subject, namespace, destination, collection, content, model, role, timestamp, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, tier FROM memories %s ORDER BY timestamp DESC LIMIT $%d OFFSET $%d",
		whereClause, idx, idx+1,
	)

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: cockroachdb: query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("destination: cockroachdb: close rows", "err", err)
		}
	}()

	records := make([]destination.TranslatedPayload, 0, limit)
	for rows.Next() {
		var tp destination.TranslatedPayload
		var metadataStr, sensitivityLabelsStr string

		if err := rows.Scan(
			&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
			&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
			&tp.Timestamp, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
			&tp.ActorType, &tp.ActorID, &metadataStr,
			&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
		); err != nil {
			return destination.QueryResult{}, fmt.Errorf("destination: cockroachdb: query: scan: %w", err)
		}
		tp.SensitivityLabels = parsePGTextArray(sensitivityLabelsStr)
		if metadataStr != "" && metadataStr != "{}" {
			_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
		}
		records = append(records, tp)
	}
	if err := rows.Err(); err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: cockroachdb: query: rows: %w", err)
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}
	var nextCursor string
	if hasMore {
		nextCursor = destination.EncodeCursor(offset + limit)
	}
	return destination.QueryResult{Records: records, NextCursor: nextCursor, HasMore: hasMore}, nil
}

// CanSemanticSearch reports whether any row in the memories table has a
// non-NULL BYTEA embedding, indicating app-level vector search is usable.
func (d *CockroachDBDestination) CanSemanticSearch() bool {
	const q = `SELECT 1 FROM memories WHERE embedding IS NOT NULL AND length(embedding) > 0 LIMIT 1`
	var dummy int
	return d.db.QueryRow(q).Scan(&dummy) == nil
}

// SemanticSearch performs application-level cosine similarity search.
// It fetches rows with non-null BYTEA embeddings (filtered by namespace/destination
// when set), decodes them in Go, ranks by cosine similarity, and returns the
// top params.Limit results.
func (d *CockroachDBDestination) SemanticSearch(ctx context.Context, vec []float32, params destination.QueryParams) ([]destination.ScoredRecord, error) {
	limit := destination.ClampLimit(params.Limit)

	args := make([]interface{}, 0, 3)
	conditions := []string{"embedding IS NOT NULL AND length(embedding) > 0"}
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
	if params.TierFilter {
		conditions = append(conditions, fmt.Sprintf("tier <= $%d", idx))
		args = append(args, params.SourceTier)
		idx++
	}
	_ = idx

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	//nolint:gosec // whereClause from fixed condition strings — no user input.
	q := "SELECT payload_id, request_id, source, subject, namespace, destination, collection, content, model, role, timestamp, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, tier, embedding FROM memories " + whereClause

	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("destination: cockroachdb: semantic search: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("destination: cockroachdb: close rows", "err", err)
		}
	}()

	type candidate struct {
		payload destination.TranslatedPayload
		score   float32
	}
	var candidates []candidate

	for rows.Next() {
		var tp destination.TranslatedPayload
		var metadataStr, sensitivityLabelsStr string
		var embeddingBlob []byte

		if err := rows.Scan(
			&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
			&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
			&tp.Timestamp, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
			&tp.ActorType, &tp.ActorID, &metadataStr,
			&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
			&embeddingBlob,
		); err != nil {
			return nil, fmt.Errorf("destination: cockroachdb: semantic search: scan: %w", err)
		}
		tp.SensitivityLabels = parsePGTextArray(sensitivityLabelsStr)
		if metadataStr != "" && metadataStr != "{}" {
			_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
		}

		rowVec := decodeEmbedding(embeddingBlob)
		if len(rowVec) == 0 {
			continue
		}
		score := cosineSimilarity(vec, rowVec)
		candidates = append(candidates, candidate{payload: tp, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("destination: cockroachdb: semantic search: rows: %w", err)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	out := make([]destination.ScoredRecord, len(candidates))
	for i, c := range candidates {
		out[i] = destination.ScoredRecord{Payload: c.payload, Score: c.score}
	}
	return out, nil
}

// parsePGTextArray parses a PostgreSQL TEXT[] literal (e.g. "{pii,financial}")
// back to a Go string slice. Returns nil for empty arrays "{}".
func parsePGTextArray(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return nil
	}
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// pgTextArray formats a string slice as a PostgreSQL TEXT[] literal.
func pgTextArray(labels []string) string {
	if len(labels) == 0 {
		return "{}"
	}
	return "{" + strings.Join(labels, ",") + "}"
}

// marshalMetadata serialises metadata to JSON. Returns "{}" for nil maps.
func marshalMetadata(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// encodeEmbedding serialises a []float32 to a little-endian byte slice for
// storage as a BYTEA column. Returns nil for an empty slice.
func encodeEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// decodeEmbedding deserialises a little-endian BYTEA back to []float32.
// Returns nil for blobs that are not a multiple of 4 bytes.
func decodeEmbedding(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// cosineSimilarity computes the cosine similarity between two float32 vectors.
// Returns 0 when either vector is all-zero (undefined similarity).
func cosineSimilarity(a, b []float32) float32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, normA, normB float32
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB)))
	if denom == 0 {
		return 0
	}
	return dot / denom
}
