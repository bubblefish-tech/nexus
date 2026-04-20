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

// Package tidb provides a TiDB destination adapter for BubbleFish Nexus.
//
// TiDB is MySQL-wire-compatible, so this adapter uses the same
// go-sql-driver/mysql driver and DDL patterns as the MySQL adapter. In
// addition, it attempts to use TiDB's native vector search functions
// (VEC_COSINE_DISTANCE, available in TiDB v8.4+) when a TEXT-encoded
// embedding column is populated. It falls back to application-level
// cosine similarity when TiDB vector functions are unavailable.
//
// All SQL uses parameterised ? placeholders. Writes are idempotent via
// INSERT IGNORE. The adapter is safe for concurrent use.
package tidb

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// Compile-time proof that TiDBDestination satisfies the Destination interface.
var _ destination.Destination = (*TiDBDestination)(nil)

// createMemoriesTable is the MySQL-compatible DDL for the memories table.
// TiDB is MySQL-wire-compatible so the same DDL applies.
const createMemoriesTable = `
CREATE TABLE IF NOT EXISTS memories (
    payload_id        VARCHAR(255)   NOT NULL,
    request_id        VARCHAR(255)   NOT NULL DEFAULT '',
    source            VARCHAR(255)   NOT NULL DEFAULT '',
    subject           VARCHAR(255)   NOT NULL DEFAULT '',
    namespace         VARCHAR(255)   NOT NULL DEFAULT '',
    ` + "`destination`" + ` VARCHAR(255)   NOT NULL DEFAULT '',
    collection        VARCHAR(255)   NOT NULL DEFAULT '',
    content           MEDIUMTEXT     NOT NULL,
    model             VARCHAR(255)   NOT NULL DEFAULT '',
    role              VARCHAR(64)    NOT NULL DEFAULT '',
    ` + "`timestamp`" + `       DATETIME(6)    NOT NULL,
    idempotency_key   VARCHAR(255)   NOT NULL DEFAULT '',
    schema_version    INT            NOT NULL DEFAULT 0,
    transform_version VARCHAR(64)    NOT NULL DEFAULT '',
    actor_type        VARCHAR(64)    NOT NULL DEFAULT '',
    actor_id          VARCHAR(255)   NOT NULL DEFAULT '',
    metadata          TEXT           NOT NULL,
    created_at        DATETIME(6)    NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (payload_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`

// Column migration DDL — idempotent via ignoring MySQL error 1060.
const (
	addEmbeddingColumn          = `ALTER TABLE memories ADD COLUMN embedding LONGBLOB`
	addSensitivityLabelsColumn  = `ALTER TABLE memories ADD COLUMN sensitivity_labels VARCHAR(1000) NOT NULL DEFAULT ''`
	addClassificationTierColumn = `ALTER TABLE memories ADD COLUMN classification_tier VARCHAR(64) NOT NULL DEFAULT 'public'`
	addTierColumn               = `ALTER TABLE memories ADD COLUMN tier INT NOT NULL DEFAULT 1`
	// addEmbeddingTVColumn stores the embedding as a JSON float array for TiDB
	// native vector functions (VEC_COSINE_DISTANCE). Populated alongside the
	// LONGBLOB column; kept in sync on every Write.
	addEmbeddingTVColumn = `ALTER TABLE memories ADD COLUMN embedding_tv TEXT`
)

// Index DDL — idempotent via ignoring MySQL error 1061.
const (
	createIdxIdempotency    = `CREATE INDEX idx_memories_idempotency_key ON memories (idempotency_key)`
	createIdxClassification = `CREATE INDEX idx_memories_classification ON memories (classification_tier)`
	createIdxTier           = `CREATE INDEX idx_memories_tier ON memories (tier)`
	createIdxQuery          = `CREATE INDEX idx_memories_query ON memories (namespace, ` + "`destination`" + `, ` + "`timestamp`" + ` DESC)`
	createIdxSubject        = `CREATE INDEX idx_memories_subject ON memories (subject, ` + "`timestamp`" + ` DESC)`
)

// TiDBDestination writes TranslatedPayload records to a TiDB database. It is
// MySQL-wire-compatible and uses go-sql-driver/mysql. In addition to the
// MySQL adapter's LONGBLOB embedding column, it maintains an embedding_tv TEXT
// column with JSON float-array encoding for TiDB native vector functions.
//
// The adapter is safe for concurrent use.
type TiDBDestination struct {
	db         *sql.DB
	logger     *slog.Logger
	hasVectorTV bool // true after successful embedding_tv column creation
}

// Open opens a connection to the TiDB database identified by dsn, creates the
// memories table, and applies idempotent column migrations.
//
// dsn is a go-sql-driver/mysql DSN, e.g.:
// "user:pass@tcp(host:4000)/dbname?parseTime=true&charset=utf8mb4"
func Open(dsn string, logger *slog.Logger) (*TiDBDestination, error) {
	if logger == nil {
		panic("destination: tidb: logger must not be nil")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("destination: tidb: open: %w", err)
	}

	d := &TiDBDestination{db: db, logger: logger}
	if err := d.applySchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	logger.Info("destination: tidb opened", "component", "destination", "vector_tv", d.hasVectorTV)
	return d, nil
}

// applySchema creates the memories table and applies idempotent migrations.
func (d *TiDBDestination) applySchema() error {
	if _, err := d.db.Exec(createMemoriesTable); err != nil {
		return fmt.Errorf("destination: tidb: create memories table: %w", err)
	}

	for _, ddl := range []string{
		addEmbeddingColumn,
		addSensitivityLabelsColumn,
		addClassificationTierColumn,
		addTierColumn,
	} {
		if err := d.execIgnoreDupColumn(ddl); err != nil {
			return err
		}
	}

	// embedding_tv TEXT — attempt to add; mark hasVectorTV on success or if
	// the column already exists. Skip silently if the schema rejects it.
	err := d.execIgnoreDupColumn(addEmbeddingTVColumn)
	d.hasVectorTV = (err == nil)

	for _, ddl := range []string{
		createIdxIdempotency,
		createIdxClassification,
		createIdxTier,
		createIdxQuery,
		createIdxSubject,
	} {
		if err := d.execIgnoreDupKey(ddl); err != nil {
			return err
		}
	}

	return nil
}

func (d *TiDBDestination) execIgnoreDupColumn(ddl string) error {
	_, err := d.db.Exec(ddl)
	if err == nil {
		return nil
	}
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) && me.Number == 1060 {
		return nil
	}
	return fmt.Errorf("destination: tidb: schema migration: %w", err)
}

func (d *TiDBDestination) execIgnoreDupKey(ddl string) error {
	_, err := d.db.Exec(ddl)
	if err == nil {
		return nil
	}
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) && me.Number == 1061 {
		return nil
	}
	return fmt.Errorf("destination: tidb: create index: %w", err)
}

// Name returns the stable identifier for this destination.
func (d *TiDBDestination) Name() string { return "tidb" }

// Write persists p to the memories table. Idempotent via INSERT IGNORE.
// When TiDB vector column is available, embedding_tv is also populated with
// JSON float-array encoding for use by VEC_COSINE_DISTANCE.
func (d *TiDBDestination) Write(p destination.TranslatedPayload) error {
	metadataJSON, err := marshalMetadata(p.Metadata)
	if err != nil {
		return fmt.Errorf("destination: tidb: marshal metadata: %w", err)
	}

	embeddingBlob := encodeEmbedding(p.Embedding)
	embeddingTV := marshalEmbeddingTV(p.Embedding)

	sensitivityLabels := strings.Join(p.SensitivityLabels, ",")
	classificationTier := p.ClassificationTier
	if classificationTier == "" {
		classificationTier = "public"
	}

	var q string
	var args []interface{}

	if d.hasVectorTV {
		q = `INSERT IGNORE INTO memories (
    payload_id, request_id, source, subject, namespace, ` + "`destination`" + `,
    collection, content, model, role, ` + "`timestamp`" + `, idempotency_key,
    schema_version, transform_version, actor_type, actor_id, metadata, embedding,
    sensitivity_labels, classification_tier, tier, embedding_tv
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		args = append(args,
			p.PayloadID, p.RequestID, p.Source, p.Subject, p.Namespace, p.Destination,
			p.Collection, p.Content, p.Model, p.Role, p.Timestamp.UTC(), p.IdempotencyKey,
			p.SchemaVersion, p.TransformVersion, p.ActorType, p.ActorID,
			metadataJSON, embeddingBlob, sensitivityLabels, classificationTier, p.Tier,
			embeddingTV,
		)
	} else {
		q = `INSERT IGNORE INTO memories (
    payload_id, request_id, source, subject, namespace, ` + "`destination`" + `,
    collection, content, model, role, ` + "`timestamp`" + `, idempotency_key,
    schema_version, transform_version, actor_type, actor_id, metadata, embedding,
    sensitivity_labels, classification_tier, tier
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		args = append(args,
			p.PayloadID, p.RequestID, p.Source, p.Subject, p.Namespace, p.Destination,
			p.Collection, p.Content, p.Model, p.Role, p.Timestamp.UTC(), p.IdempotencyKey,
			p.SchemaVersion, p.TransformVersion, p.ActorType, p.ActorID,
			metadataJSON, embeddingBlob, sensitivityLabels, classificationTier, p.Tier,
		)
	}

	if _, err := d.db.Exec(q, args...); err != nil {
		return fmt.Errorf("destination: tidb: write payload_id %q: %w", p.PayloadID, err)
	}

	d.logger.Debug("destination: tidb: write", "component", "destination", "payload_id", p.PayloadID)
	return nil
}

// Read retrieves a single memory record by its PayloadID. Returns nil, nil
// when the record does not exist.
func (d *TiDBDestination) Read(ctx context.Context, id string) (*destination.Memory, error) {
	const q = `SELECT payload_id, request_id, source, subject, namespace, ` + "`destination`" + `,
		collection, content, model, role, ` + "`timestamp`" + `, idempotency_key,
		schema_version, transform_version, actor_type, actor_id, metadata,
		sensitivity_labels, classification_tier, tier
		FROM memories WHERE payload_id = ? LIMIT 1`

	row := d.db.QueryRowContext(ctx, q, id)
	tp, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("destination: tidb: read %q: %w", id, err)
	}
	return tp, nil
}

// Search returns memories matching query. Returns an empty (non-nil) slice
// when no records match.
func (d *TiDBDestination) Search(ctx context.Context, query *destination.Query) ([]*destination.Memory, error) {
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

// Delete removes the record with the given PayloadID. Idempotent.
func (d *TiDBDestination) Delete(ctx context.Context, id string) error {
	const q = "DELETE FROM memories WHERE payload_id = ?"
	if _, err := d.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("destination: tidb: delete %q: %w", id, err)
	}
	return nil
}

// VectorSearch returns up to limit memories ranked by cosine similarity.
// Uses TiDB native VEC_COSINE_DISTANCE when the embedding_tv column is
// available; falls back to application-level cosine similarity otherwise.
func (d *TiDBDestination) VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*destination.Memory, error) {
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

// Migrate is a no-op for TiDB: all schema migrations are applied idempotently
// at open time inside applySchema.
func (d *TiDBDestination) Migrate(_ context.Context, _ int) error { return nil }

// Health performs a lightweight liveness probe.
func (d *TiDBDestination) Health(ctx context.Context) (*destination.HealthStatus, error) {
	start := time.Now()
	if err := d.db.PingContext(ctx); err != nil {
		return &destination.HealthStatus{OK: false, Error: err.Error()}, nil
	}
	return &destination.HealthStatus{OK: true, Latency: time.Since(start)}, nil
}

// Close releases all resources held by the destination.
func (d *TiDBDestination) Close() error { return d.db.Close() }

// Ping verifies the database connection is alive.
func (d *TiDBDestination) Ping() error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("destination: tidb: ping: %w", err)
	}
	return nil
}

// Exists reports whether a record with payloadID exists.
func (d *TiDBDestination) Exists(payloadID string) (bool, error) {
	const q = "SELECT 1 FROM memories WHERE payload_id = ? LIMIT 1"
	var dummy int
	err := d.db.QueryRow(q, payloadID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("destination: tidb: exists %q: %w", payloadID, err)
	}
	return true, nil
}

// Query returns a page of memories using parameterised structured queries.
func (d *TiDBDestination) Query(params destination.QueryParams) (destination.QueryResult, error) {
	limit := destination.ClampLimit(params.Limit)
	offset, err := destination.DecodeCursor(params.Cursor)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: tidb: query: %w", err)
	}

	args := make([]interface{}, 0, 6)
	conditions := make([]string, 0, 4)

	if params.Namespace != "" {
		conditions = append(conditions, "namespace = ?")
		args = append(args, params.Namespace)
	}
	if params.Destination != "" {
		conditions = append(conditions, "`destination` = ?")
		args = append(args, params.Destination)
	}
	if params.Subject != "" {
		conditions = append(conditions, "subject = ?")
		args = append(args, params.Subject)
	}
	if params.Q != "" {
		conditions = append(conditions, "content LIKE ?")
		args = append(args, "%"+params.Q+"%")
	}
	if params.ActorType != "" {
		conditions = append(conditions, "actor_type = ?")
		args = append(args, params.ActorType)
	}
	if params.TierFilter {
		conditions = append(conditions, "tier <= ?")
		args = append(args, params.SourceTier)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	fetchLimit := limit + 1
	args = append(args, fetchLimit, offset)

	//nolint:gosec // whereClause built from fixed condition strings, no user input.
	q := "SELECT payload_id, request_id, source, subject, namespace, `destination`, collection, content, model, role, `timestamp`, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, tier FROM memories " + whereClause + " ORDER BY `timestamp` DESC LIMIT ? OFFSET ?"

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: tidb: query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("destination: tidb: close rows", "err", err)
		}
	}()

	records := make([]destination.TranslatedPayload, 0, limit)
	for rows.Next() {
		tp, err := scanRow(rows)
		if err != nil {
			return destination.QueryResult{}, fmt.Errorf("destination: tidb: query: scan: %w", err)
		}
		records = append(records, *tp)
	}
	if err := rows.Err(); err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: tidb: query: rows: %w", err)
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

// CanSemanticSearch reports whether any row has a non-NULL embedding.
func (d *TiDBDestination) CanSemanticSearch() bool {
	const q = `SELECT 1 FROM memories WHERE embedding IS NOT NULL AND LENGTH(embedding) > 0 LIMIT 1`
	var dummy int
	return d.db.QueryRow(q).Scan(&dummy) == nil
}

// SemanticSearch performs cosine similarity search. When the embedding_tv
// column exists and TiDB vector functions are available, it uses
// VEC_COSINE_DISTANCE for native ranking. Otherwise it falls back to
// application-level cosine computation over the LONGBLOB column.
func (d *TiDBDestination) SemanticSearch(ctx context.Context, vec []float32, params destination.QueryParams) ([]destination.ScoredRecord, error) {
	if d.hasVectorTV {
		result, err := d.tidbVectorSearch(ctx, vec, params)
		if err == nil {
			return result, nil
		}
		// Fall back to app-level on any TiDB vector error.
		d.logger.Debug("destination: tidb: native vector search failed, falling back",
			"err", err)
	}
	return d.appLevelSemanticSearch(ctx, vec, params)
}

// tidbVectorSearch uses TiDB's VEC_COSINE_DISTANCE function for native
// vector similarity ranking (TiDB v8.4+).
func (d *TiDBDestination) tidbVectorSearch(ctx context.Context, vec []float32, params destination.QueryParams) ([]destination.ScoredRecord, error) {
	limit := destination.ClampLimit(params.Limit)
	vecJSON := marshalEmbeddingTV(vec)
	if vecJSON == "" {
		return nil, fmt.Errorf("destination: tidb: empty query vector")
	}

	args := make([]interface{}, 0, 5)
	conditions := []string{"embedding_tv IS NOT NULL"}

	if params.Namespace != "" {
		conditions = append(conditions, "namespace = ?")
		args = append(args, params.Namespace)
	}
	if params.Destination != "" {
		conditions = append(conditions, "`destination` = ?")
		args = append(args, params.Destination)
	}
	if params.TierFilter {
		conditions = append(conditions, "tier <= ?")
		args = append(args, params.SourceTier)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")
	args = append(args, vecJSON, limit)

	//nolint:gosec // whereClause from fixed condition strings — no user input.
	q := "SELECT payload_id, request_id, source, subject, namespace, `destination`, collection, content, model, role, `timestamp`, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, tier, (1 - VEC_COSINE_DISTANCE(embedding_tv, ?)) AS score FROM memories " + whereClause + " ORDER BY score DESC LIMIT ?"

	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("destination: tidb: tidb vector search: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("destination: tidb: close vector rows", "err", err)
		}
	}()

	var out []destination.ScoredRecord
	for rows.Next() {
		var tp destination.TranslatedPayload
		var metadataStr, sensitivityLabelsStr string
		var ts time.Time
		var score float32

		if err := rows.Scan(
			&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
			&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
			&ts, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
			&tp.ActorType, &tp.ActorID, &metadataStr,
			&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
			&score,
		); err != nil {
			return nil, fmt.Errorf("destination: tidb: tidb vector search: scan: %w", err)
		}
		tp.Timestamp = ts.UTC()
		tp.SensitivityLabels = parseSensitivityLabels(sensitivityLabelsStr)
		if metadataStr != "" && metadataStr != "{}" {
			_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
		}
		out = append(out, destination.ScoredRecord{Payload: tp, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("destination: tidb: tidb vector search: rows: %w", err)
	}
	return out, nil
}

// appLevelSemanticSearch is the application-level cosine similarity fallback.
func (d *TiDBDestination) appLevelSemanticSearch(ctx context.Context, vec []float32, params destination.QueryParams) ([]destination.ScoredRecord, error) {
	limit := destination.ClampLimit(params.Limit)

	args := make([]interface{}, 0, 3)
	conditions := []string{"embedding IS NOT NULL AND LENGTH(embedding) > 0"}

	if params.Namespace != "" {
		conditions = append(conditions, "namespace = ?")
		args = append(args, params.Namespace)
	}
	if params.Destination != "" {
		conditions = append(conditions, "`destination` = ?")
		args = append(args, params.Destination)
	}
	if params.TierFilter {
		conditions = append(conditions, "tier <= ?")
		args = append(args, params.SourceTier)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")
	//nolint:gosec // whereClause from fixed condition strings — no user input.
	q := "SELECT payload_id, request_id, source, subject, namespace, `destination`, collection, content, model, role, `timestamp`, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, tier, embedding FROM memories " + whereClause

	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("destination: tidb: semantic search: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("destination: tidb: close rows", "err", err)
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
		var ts time.Time

		if err := rows.Scan(
			&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
			&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
			&ts, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
			&tp.ActorType, &tp.ActorID, &metadataStr,
			&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
			&embeddingBlob,
		); err != nil {
			return nil, fmt.Errorf("destination: tidb: semantic search: scan: %w", err)
		}
		tp.Timestamp = ts.UTC()
		tp.SensitivityLabels = parseSensitivityLabels(sensitivityLabelsStr)
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
		return nil, fmt.Errorf("destination: tidb: semantic search: rows: %w", err)
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

// ── Scan helpers ─────────────────────────────────────────────────────────────

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanMemory(row rowScanner) (*destination.TranslatedPayload, error) {
	var tp destination.TranslatedPayload
	var metadataStr, sensitivityLabelsStr string
	var ts time.Time

	err := row.Scan(
		&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
		&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
		&ts, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
		&tp.ActorType, &tp.ActorID, &metadataStr,
		&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
	)
	if err != nil {
		return nil, err
	}
	tp.Timestamp = ts.UTC()
	tp.SensitivityLabels = parseSensitivityLabels(sensitivityLabelsStr)
	if metadataStr != "" && metadataStr != "{}" {
		_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
	}
	return &tp, nil
}

func scanRow(rows *sql.Rows) (*destination.TranslatedPayload, error) {
	var tp destination.TranslatedPayload
	var metadataStr, sensitivityLabelsStr string
	var ts time.Time

	if err := rows.Scan(
		&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
		&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
		&ts, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
		&tp.ActorType, &tp.ActorID, &metadataStr,
		&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
	); err != nil {
		return nil, err
	}
	tp.Timestamp = ts.UTC()
	tp.SensitivityLabels = parseSensitivityLabels(sensitivityLabelsStr)
	if metadataStr != "" && metadataStr != "{}" {
		_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
	}
	return &tp, nil
}

// ── Encoding helpers ──────────────────────────────────────────────────────────

func parseSensitivityLabels(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

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

// marshalEmbeddingTV serialises a []float32 as a JSON float array string
// for the embedding_tv TEXT column used by TiDB vector functions.
// Returns empty string for a nil/empty slice.
func marshalEmbeddingTV(v []float32) string {
	if len(v) == 0 {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

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
