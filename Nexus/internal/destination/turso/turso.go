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

// Package turso provides a Turso/libSQL destination adapter for BubbleFish
// Nexus.
//
// Turso is SQLite-compatible with edge replication. The adapter uses the
// libsql database/sql driver (github.com/tursodatabase/libsql-client-go)
// and SQLite-compatible DDL. Application-level cosine similarity is used
// for vector search — the same O(n) approach as the SQLite, MySQL, and
// CockroachDB adapters.
//
// Connection string format (examples):
//
//	libsql://database.turso.io?authToken=TOKEN          (remote Turso cloud)
//	https://database.turso.io?authToken=TOKEN           (HTTPS variant)
//	file:path/to/local.db                               (local libSQL file)
//
// All SQL uses parameterised ? placeholders. Writes are idempotent via
// INSERT OR IGNORE. The adapter is safe for concurrent use.
package turso

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

	_ "github.com/tursodatabase/libsql-client-go/libsql" // register "libsql" driver

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// Compile-time proof that TursoDestination satisfies the Destination interface.
var _ destination.Destination = (*TursoDestination)(nil)

// createMemoriesTable is the SQLite-compatible DDL for the memories table.
const createMemoriesTable = `
CREATE TABLE IF NOT EXISTS memories (
    payload_id          TEXT     NOT NULL PRIMARY KEY,
    request_id          TEXT     NOT NULL DEFAULT '',
    source              TEXT     NOT NULL DEFAULT '',
    subject             TEXT     NOT NULL DEFAULT '',
    namespace           TEXT     NOT NULL DEFAULT '',
    destination         TEXT     NOT NULL DEFAULT '',
    collection          TEXT     NOT NULL DEFAULT '',
    content             TEXT     NOT NULL DEFAULT '',
    model               TEXT     NOT NULL DEFAULT '',
    role                TEXT     NOT NULL DEFAULT '',
    timestamp           INTEGER  NOT NULL DEFAULT 0,
    idempotency_key     TEXT     NOT NULL DEFAULT '',
    schema_version      INTEGER  NOT NULL DEFAULT 0,
    transform_version   TEXT     NOT NULL DEFAULT '',
    actor_type          TEXT     NOT NULL DEFAULT '',
    actor_id            TEXT     NOT NULL DEFAULT '',
    metadata            TEXT     NOT NULL DEFAULT '{}',
    embedding           BLOB,
    sensitivity_labels  TEXT     NOT NULL DEFAULT '',
    classification_tier TEXT     NOT NULL DEFAULT 'public',
    tier                INTEGER  NOT NULL DEFAULT 1,
    created_at          INTEGER  NOT NULL DEFAULT 0
)`

// Index DDL — idempotent via IF NOT EXISTS.
const (
	createIdxIdempotency    = `CREATE INDEX IF NOT EXISTS idx_memories_idempotency_key ON memories (idempotency_key)`
	createIdxClassification = `CREATE INDEX IF NOT EXISTS idx_memories_classification ON memories (classification_tier)`
	createIdxTier           = `CREATE INDEX IF NOT EXISTS idx_memories_tier ON memories (tier)`
	createIdxQuery          = `CREATE INDEX IF NOT EXISTS idx_memories_query ON memories (namespace, destination, timestamp DESC)`
	createIdxSubject        = `CREATE INDEX IF NOT EXISTS idx_memories_subject ON memories (subject, timestamp DESC)`
)

// TursoDestination writes TranslatedPayload records to a Turso/libSQL
// database. Vector search uses application-level cosine similarity over
// BLOB-encoded float32 embeddings (same encoding as the SQLite adapter).
//
// Timestamps are stored as Unix milliseconds (INTEGER) for SQLite/libSQL
// compatibility.
//
// The adapter is safe for concurrent use.
type TursoDestination struct {
	db     *sql.DB
	logger *slog.Logger
}

// Open connects to the Turso/libSQL database identified by url, creates the
// memories table and indexes, and returns the adapter.
//
// url is a libSQL connection string, e.g.:
// "libsql://database.turso.io?authToken=TOKEN" or "file:./local.db"
func Open(url string, logger *slog.Logger) (*TursoDestination, error) {
	if logger == nil {
		panic("destination: turso: logger must not be nil")
	}

	db, err := sql.Open("libsql", url)
	if err != nil {
		return nil, fmt.Errorf("destination: turso: open: %w", err)
	}

	d := &TursoDestination{db: db, logger: logger}
	if err := d.applySchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	logger.Info("destination: turso opened", "component", "destination")
	return d, nil
}

// applySchema creates the memories table and all indexes.
func (d *TursoDestination) applySchema() error {
	if _, err := d.db.Exec(createMemoriesTable); err != nil {
		return fmt.Errorf("destination: turso: create memories table: %w", err)
	}
	for _, ddl := range []string{
		createIdxIdempotency,
		createIdxClassification,
		createIdxTier,
		createIdxQuery,
		createIdxSubject,
	} {
		if _, err := d.db.Exec(ddl); err != nil {
			return fmt.Errorf("destination: turso: create index: %w", err)
		}
	}
	return nil
}

// Name returns the stable identifier for this destination.
func (d *TursoDestination) Name() string { return "turso" }

// Write persists p to the memories table. Idempotent via INSERT OR IGNORE.
// Timestamp is stored as Unix milliseconds. Embedding is stored as a
// little-endian float32 BLOB.
func (d *TursoDestination) Write(p destination.TranslatedPayload) error {
	metadataJSON, err := marshalMetadata(p.Metadata)
	if err != nil {
		return fmt.Errorf("destination: turso: marshal metadata: %w", err)
	}

	embeddingBlob := encodeEmbedding(p.Embedding)
	sensitivityLabels := strings.Join(p.SensitivityLabels, ",")
	ct := p.ClassificationTier
	if ct == "" {
		ct = "public"
	}

	const q = `INSERT OR IGNORE INTO memories (
    payload_id, request_id, source, subject, namespace, destination,
    collection, content, model, role, timestamp, idempotency_key,
    schema_version, transform_version, actor_type, actor_id, metadata,
    embedding, sensitivity_labels, classification_tier, tier, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = d.db.Exec(q,
		p.PayloadID, p.RequestID, p.Source, p.Subject, p.Namespace, p.Destination,
		p.Collection, p.Content, p.Model, p.Role,
		p.Timestamp.UTC().UnixMilli(),
		p.IdempotencyKey, p.SchemaVersion, p.TransformVersion,
		p.ActorType, p.ActorID, metadataJSON, embeddingBlob,
		sensitivityLabels, ct, p.Tier,
		time.Now().UTC().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("destination: turso: write payload_id %q: %w", p.PayloadID, err)
	}

	d.logger.Debug("destination: turso: write", "component", "destination", "payload_id", p.PayloadID)
	return nil
}

// Read retrieves a single memory record by its PayloadID. Returns nil, nil
// when the record does not exist.
func (d *TursoDestination) Read(ctx context.Context, id string) (*destination.Memory, error) {
	const q = `SELECT payload_id, request_id, source, subject, namespace, destination,
		collection, content, model, role, timestamp, idempotency_key,
		schema_version, transform_version, actor_type, actor_id, metadata,
		sensitivity_labels, classification_tier, tier
		FROM memories WHERE payload_id = ? LIMIT 1`

	row := d.db.QueryRowContext(ctx, q, id)
	tp, err := scanMemory(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("destination: turso: read %q: %w", id, err)
	}
	return tp, nil
}

// Search returns memories matching query. Returns an empty (non-nil) slice
// when no records match.
func (d *TursoDestination) Search(ctx context.Context, query *destination.Query) ([]*destination.Memory, error) {
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
func (d *TursoDestination) Delete(ctx context.Context, id string) error {
	const q = "DELETE FROM memories WHERE payload_id = ?"
	if _, err := d.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("destination: turso: delete %q: %w", id, err)
	}
	return nil
}

// VectorSearch returns up to limit memories ranked by application-level
// cosine similarity. Returns an empty slice for a nil or empty embedding.
func (d *TursoDestination) VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*destination.Memory, error) {
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

// Migrate is a no-op for Turso: all schema migrations are applied
// idempotently at open time inside applySchema.
func (d *TursoDestination) Migrate(_ context.Context, _ int) error { return nil }

// Health performs a lightweight liveness probe by pinging the database.
func (d *TursoDestination) Health(ctx context.Context) (*destination.HealthStatus, error) {
	start := time.Now()
	if err := d.db.PingContext(ctx); err != nil {
		return &destination.HealthStatus{OK: false, Error: err.Error()}, nil
	}
	return &destination.HealthStatus{OK: true, Latency: time.Since(start)}, nil
}

// Close releases all resources held by the destination.
func (d *TursoDestination) Close() error { return d.db.Close() }

// Ping verifies the database connection is alive.
func (d *TursoDestination) Ping() error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("destination: turso: ping: %w", err)
	}
	return nil
}

// Exists reports whether a record with payloadID exists.
func (d *TursoDestination) Exists(payloadID string) (bool, error) {
	const q = "SELECT 1 FROM memories WHERE payload_id = ? LIMIT 1"
	var dummy int
	err := d.db.QueryRow(q, payloadID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("destination: turso: exists %q: %w", payloadID, err)
	}
	return true, nil
}

// Query returns a page of memories using parameterised SQL. Pagination uses
// skip/limit with a base64-encoded offset cursor.
func (d *TursoDestination) Query(params destination.QueryParams) (destination.QueryResult, error) {
	limit := destination.ClampLimit(params.Limit)
	offset, err := destination.DecodeCursor(params.Cursor)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: turso: query: %w", err)
	}

	args := make([]interface{}, 0, 6)
	conditions := make([]string, 0, 4)

	if params.Namespace != "" {
		conditions = append(conditions, "namespace = ?")
		args = append(args, params.Namespace)
	}
	if params.Destination != "" {
		conditions = append(conditions, "destination = ?")
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
	q := "SELECT payload_id, request_id, source, subject, namespace, destination, collection, content, model, role, timestamp, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, tier FROM memories " + whereClause + " ORDER BY timestamp DESC LIMIT ? OFFSET ?"

	rows, err := d.db.Query(q, args...)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: turso: query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("destination: turso: close rows", "err", err)
		}
	}()

	records := make([]destination.TranslatedPayload, 0, limit)
	for rows.Next() {
		tp, err := scanRow(rows)
		if err != nil {
			return destination.QueryResult{}, fmt.Errorf("destination: turso: query: scan: %w", err)
		}
		records = append(records, *tp)
	}
	if err := rows.Err(); err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: turso: query: rows: %w", err)
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
func (d *TursoDestination) CanSemanticSearch() bool {
	const q = `SELECT 1 FROM memories WHERE embedding IS NOT NULL LIMIT 1`
	var dummy int
	return d.db.QueryRow(q).Scan(&dummy) == nil
}

// SemanticSearch performs application-level cosine similarity search.
// It fetches rows with non-null embeddings, decodes them in Go, ranks by
// cosine similarity, and returns the top params.Limit results.
func (d *TursoDestination) SemanticSearch(ctx context.Context, vec []float32, params destination.QueryParams) ([]destination.ScoredRecord, error) {
	limit := destination.ClampLimit(params.Limit)

	args := make([]interface{}, 0, 3)
	conditions := []string{"embedding IS NOT NULL"}

	if params.Namespace != "" {
		conditions = append(conditions, "namespace = ?")
		args = append(args, params.Namespace)
	}
	if params.Destination != "" {
		conditions = append(conditions, "destination = ?")
		args = append(args, params.Destination)
	}
	if params.TierFilter {
		conditions = append(conditions, "tier <= ?")
		args = append(args, params.SourceTier)
	}

	whereClause := "WHERE " + strings.Join(conditions, " AND ")

	//nolint:gosec // whereClause from fixed condition strings — no user input.
	q := "SELECT payload_id, request_id, source, subject, namespace, destination, collection, content, model, role, timestamp, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, tier, embedding FROM memories " + whereClause

	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("destination: turso: semantic search: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("destination: turso: close rows", "err", err)
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
		var tsMS int64

		if err := rows.Scan(
			&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
			&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
			&tsMS, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
			&tp.ActorType, &tp.ActorID, &metadataStr,
			&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
			&embeddingBlob,
		); err != nil {
			return nil, fmt.Errorf("destination: turso: semantic search: scan: %w", err)
		}
		tp.Timestamp = time.UnixMilli(tsMS).UTC()
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
		return nil, fmt.Errorf("destination: turso: semantic search: rows: %w", err)
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
	var tsMS int64

	err := row.Scan(
		&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
		&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
		&tsMS, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
		&tp.ActorType, &tp.ActorID, &metadataStr,
		&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
	)
	if err != nil {
		return nil, err
	}
	tp.Timestamp = time.UnixMilli(tsMS).UTC()
	tp.SensitivityLabels = parseSensitivityLabels(sensitivityLabelsStr)
	if metadataStr != "" && metadataStr != "{}" {
		_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
	}
	return &tp, nil
}

func scanRow(rows *sql.Rows) (*destination.TranslatedPayload, error) {
	var tp destination.TranslatedPayload
	var metadataStr, sensitivityLabelsStr string
	var tsMS int64

	if err := rows.Scan(
		&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
		&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
		&tsMS, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
		&tp.ActorType, &tp.ActorID, &metadataStr,
		&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
	); err != nil {
		return nil, err
	}
	tp.Timestamp = time.UnixMilli(tsMS).UTC()
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
