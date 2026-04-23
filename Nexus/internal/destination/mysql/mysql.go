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

// Package mysql provides a MySQL/MariaDB destination adapter for BubbleFish Nexus.
//
// Vector search is implemented with application-level cosine similarity —
// there is no native vector extension requirement. All SQL uses parameterised
// ? placeholders. Writes are idempotent via INSERT IGNORE.
package mysql

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

// Compile-time proof that MySQLDestination satisfies the Destination interface.
var _ destination.Destination = (*MySQLDestination)(nil)

// createMemoriesTable is the DDL for the memories table. Backtick-quoting is
// used for `timestamp` and `destination` which are MySQL reserved/semi-reserved
// words. All column values are always provided in INSERT, so TEXT columns carry
// no DEFAULT clause (MySQL 5.7 rejects DEFAULT on TEXT/BLOB).
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

// Migration DDL — idempotent via ignoring MySQL error 1060 (duplicate column name).
const (
	addEmbeddingColumn          = `ALTER TABLE memories ADD COLUMN embedding LONGBLOB`
	addSensitivityLabelsColumn  = `ALTER TABLE memories ADD COLUMN sensitivity_labels VARCHAR(1000) NOT NULL DEFAULT ''`
	addClassificationTierColumn = `ALTER TABLE memories ADD COLUMN classification_tier VARCHAR(64) NOT NULL DEFAULT 'public'`
	addTierColumn               = `ALTER TABLE memories ADD COLUMN tier INT NOT NULL DEFAULT 1`
)

// Index DDL — idempotent via ignoring MySQL error 1061 (duplicate key name).
const (
	createIdxIdempotency    = `CREATE INDEX idx_memories_idempotency_key ON memories (idempotency_key)`
	createIdxClassification = `CREATE INDEX idx_memories_classification ON memories (classification_tier)`
	createIdxTier           = `CREATE INDEX idx_memories_tier ON memories (tier)`
	createIdxQuery          = `CREATE INDEX idx_memories_query ON memories (namespace, ` + "`destination`" + `, ` + "`timestamp`" + ` DESC)`
	createIdxSubject        = `CREATE INDEX idx_memories_subject ON memories (subject, ` + "`timestamp`" + ` DESC)`
)

// MySQLDestination writes TranslatedPayload records to a MySQL or MariaDB
// database. Vector search uses application-level cosine similarity — there
// is no dependency on a native vector extension.
//
// All SQL uses parameterised ? placeholders. Writes are idempotent via
// INSERT IGNORE. The adapter is safe for concurrent use.
type MySQLDestination struct {
	db     *sql.DB
	logger *slog.Logger
}

// Open opens a connection to the MySQL/MariaDB database identified by dsn,
// creates the memories table, and applies idempotent column migrations.
//
// dsn is a go-sql-driver/mysql DSN, e.g.:
// "user:pass@tcp(host:3306)/dbname?parseTime=true&charset=utf8mb4"
// The parseTime=true parameter is required for correct DATETIME scanning.
func Open(dsn string, logger *slog.Logger) (*MySQLDestination, error) {
	if logger == nil {
		panic("destination: mysql: logger must not be nil")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("destination: mysql: open: %w", err)
	}

	d := &MySQLDestination{db: db, logger: logger}
	if err := d.applySchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	logger.Info("destination: mysql opened", "component", "destination")
	return d, nil
}

// applySchema creates the memories table and applies idempotent migrations.
func (d *MySQLDestination) applySchema() error {
	if _, err := d.db.Exec(createMemoriesTable); err != nil {
		return fmt.Errorf("destination: mysql: create memories table: %w", err)
	}

	// Idempotent column migrations — ignore error 1060 (duplicate column name).
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

	// Idempotent index creation — ignore error 1061 (duplicate key name).
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

// execIgnoreDupColumn executes ddl and swallows MySQL error 1060 (duplicate column name),
// making ADD COLUMN idempotent on existing databases.
func (d *MySQLDestination) execIgnoreDupColumn(ddl string) error {
	_, err := d.db.Exec(ddl)
	if err == nil {
		return nil
	}
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) && me.Number == 1060 {
		return nil // column already exists
	}
	return fmt.Errorf("destination: mysql: schema migration: %w", err)
}

// execIgnoreDupKey executes ddl and swallows MySQL error 1061 (duplicate key name),
// making CREATE INDEX idempotent on existing databases.
func (d *MySQLDestination) execIgnoreDupKey(ddl string) error {
	_, err := d.db.Exec(ddl)
	if err == nil {
		return nil
	}
	var me *mysqldrv.MySQLError
	if errors.As(err, &me) && me.Number == 1061 {
		return nil // index already exists
	}
	return fmt.Errorf("destination: mysql: create index: %w", err)
}

// Name returns the stable identifier for this destination.
func (d *MySQLDestination) Name() string { return "mysql" }

// Write persists p to the memories table. Idempotent via INSERT IGNORE.
// All values are bound via parameterised ? placeholders — no string concatenation.
// If p.Embedding is non-empty it is serialised as a little-endian float32 LONGBLOB.
func (d *MySQLDestination) Write(p destination.TranslatedPayload) error {
	metadataJSON, err := marshalMetadata(p.Metadata)
	if err != nil {
		return fmt.Errorf("destination: mysql: marshal metadata: %w", err)
	}

	embeddingBlob := encodeEmbedding(p.Embedding)

	sensitivityLabels := strings.Join(p.SensitivityLabels, ",")
	classificationTier := p.ClassificationTier
	if classificationTier == "" {
		classificationTier = "public"
	}

	const query = `
INSERT IGNORE INTO memories (
    payload_id, request_id, source, subject, namespace, ` + "`destination`" + `,
    collection, content, model, role, ` + "`timestamp`" + `, idempotency_key,
    schema_version, transform_version, actor_type, actor_id, metadata, embedding,
    sensitivity_labels, classification_tier, tier
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

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
		embeddingBlob,
		sensitivityLabels,
		classificationTier,
		p.Tier,
	)
	if err != nil {
		return fmt.Errorf("destination: mysql: write payload_id %q: %w", p.PayloadID, err)
	}

	d.logger.Debug("destination: mysql: write",
		"component", "destination",
		"payload_id", p.PayloadID,
	)
	return nil
}

// Read retrieves a single memory record by its PayloadID. Returns nil, nil when
// the record does not exist.
func (d *MySQLDestination) Read(ctx context.Context, id string) (*destination.Memory, error) {
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
		return nil, fmt.Errorf("destination: mysql: read %q: %w", id, err)
	}
	return tp, nil
}

// Search returns memories matching query, converting the result to a slice of
// pointers. Returns an empty (non-nil) slice when no records match.
func (d *MySQLDestination) Search(ctx context.Context, query *destination.Query) ([]*destination.Memory, error) {
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
func (d *MySQLDestination) Delete(ctx context.Context, id string) error {
	const q = "DELETE FROM memories WHERE payload_id = ?"
	if _, err := d.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("destination: mysql: delete %q: %w", id, err)
	}
	return nil
}

// VectorSearch returns up to limit memories ranked by cosine similarity to
// embedding using application-level computation. Returns an empty slice for a
// nil or empty embedding.
func (d *MySQLDestination) VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*destination.Memory, error) {
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

// Migrate is a no-op for MySQL: all schema migrations are applied idempotently
// at open time inside applySchema. The version argument is reserved for future
// use when explicit versioned migrations are required.
func (d *MySQLDestination) Migrate(_ context.Context, _ int) error {
	return nil
}

// Health performs a lightweight liveness probe by pinging the database and
// measuring round-trip latency. Does NOT modify any stored data.
func (d *MySQLDestination) Health(ctx context.Context) (*destination.HealthStatus, error) {
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
func (d *MySQLDestination) Close() error {
	return d.db.Close()
}

// Ping verifies the database connection is alive. Satisfies DestinationWriter.
func (d *MySQLDestination) Ping() error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("destination: mysql: ping: %w", err)
	}
	return nil
}

// Exists reports whether a record with payloadID exists in the memories table.
func (d *MySQLDestination) Exists(payloadID string) (bool, error) {
	const q = "SELECT 1 FROM memories WHERE payload_id = ? LIMIT 1"
	var dummy int
	err := d.db.QueryRow(q, payloadID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("destination: mysql: exists %q: %w", payloadID, err)
	}
	return true, nil
}

// Query returns a page of memories using parameterised structured queries.
// MySQL uses ? placeholders. LIKE is case-insensitive under utf8mb4_unicode_ci.
func (d *MySQLDestination) Query(params destination.QueryParams) (destination.QueryResult, error) {
	limit := destination.ClampLimit(params.Limit)
	offset, err := destination.DecodeCursor(params.Cursor)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: mysql: query: %w", err)
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
		return destination.QueryResult{}, fmt.Errorf("destination: mysql: query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("destination: mysql: close rows", "err", err)
		}
	}()

	records := make([]destination.TranslatedPayload, 0, limit)
	for rows.Next() {
		tp, err := scanRow(rows)
		if err != nil {
			return destination.QueryResult{}, fmt.Errorf("destination: mysql: query: scan: %w", err)
		}
		records = append(records, *tp)
	}
	if err := rows.Err(); err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: mysql: query: rows: %w", err)
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
// non-NULL embedding, indicating app-level vector search is usable.
func (d *MySQLDestination) CanSemanticSearch() bool {
	const q = `SELECT 1 FROM memories WHERE embedding IS NOT NULL AND LENGTH(embedding) > 0 LIMIT 1`
	var dummy int
	return d.db.QueryRow(q).Scan(&dummy) == nil
}

// SemanticSearch performs application-level cosine similarity search.
// It fetches rows with non-null embeddings (filtered by namespace/destination
// when set), decodes them in Go, ranks by cosine similarity, and returns the
// top params.Limit results.
//
// This approach is correct but O(n) in embedding count. For large datasets
// operators should consider a dedicated vector store.
func (d *MySQLDestination) SemanticSearch(ctx context.Context, vec []float32, params destination.QueryParams) ([]destination.ScoredRecord, error) {
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
		return nil, fmt.Errorf("destination: mysql: semantic search: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("destination: mysql: close rows", "err", err)
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
			return nil, fmt.Errorf("destination: mysql: semantic search: scan: %w", err)
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
		return nil, fmt.Errorf("destination: mysql: semantic search: rows: %w", err)
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

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanMemory scans a single *sql.Row result (for Read).
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

// scanRow scans one row from a *sql.Rows result set (for Query).
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

// parseSensitivityLabels splits the comma-separated sensitivity_labels column
// value into a string slice. Returns nil for empty input.
func parseSensitivityLabels(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
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
// storage in a LONGBLOB column. Returns nil for an empty slice.
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

// decodeEmbedding deserialises a little-endian BLOB back to []float32.
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
