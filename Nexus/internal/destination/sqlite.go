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
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	// Pure-Go SQLite driver (modernc.org/sqlite). No CGO required for production.
	// Registers the "sqlite" driver name with database/sql.
	_ "modernc.org/sqlite"
)

const (
	// sqliteDriverName is the driver name registered by modernc.org/sqlite.
	sqliteDriverName = "sqlite"

	// createMemoriesTable is the DDL for the memories table. Uses
	// IF NOT EXISTS so it is idempotent across restarts.
	createMemoriesTable = `
CREATE TABLE IF NOT EXISTS memories (
    payload_id        TEXT    PRIMARY KEY,
    request_id        TEXT    NOT NULL DEFAULT '',
    source            TEXT    NOT NULL DEFAULT '',
    subject           TEXT    NOT NULL DEFAULT '',
    namespace         TEXT    NOT NULL DEFAULT '',
    destination       TEXT    NOT NULL DEFAULT '',
    collection        TEXT    NOT NULL DEFAULT '',
    content           TEXT    NOT NULL DEFAULT '',
    model             TEXT    NOT NULL DEFAULT '',
    role              TEXT    NOT NULL DEFAULT '',
    timestamp         TEXT    NOT NULL DEFAULT '',
    idempotency_key   TEXT    NOT NULL DEFAULT '',
    schema_version    INTEGER NOT NULL DEFAULT 0,
    transform_version TEXT    NOT NULL DEFAULT '',
    actor_type        TEXT    NOT NULL DEFAULT '',
    actor_id          TEXT    NOT NULL DEFAULT '',
    metadata          TEXT    NOT NULL DEFAULT '{}',
    created_at        TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
)`

	createIdempotencyKeyIndex = `
CREATE INDEX IF NOT EXISTS idx_memories_idempotency_key
    ON memories (idempotency_key)`

	// addEmbeddingColumn migrates an existing memories table to include the
	// embedding column. SQLite silently succeeds if the column already exists
	// when using "IF NOT EXISTS" via the ALTER TABLE syntax, but standard SQLite
	// does not support "ADD COLUMN IF NOT EXISTS". We execute and ignore the
	// "duplicate column name" error code (SQLITE_ERROR, 1) to make the
	// operation idempotent across restarts on pre-existing databases.
	//
	// The column is nullable (no NOT NULL) because not every write payload
	// carries an embedding. INSERT OR IGNORE must never discard a row solely
	// because the embedding is absent.
	addEmbeddingColumn = `ALTER TABLE memories ADD COLUMN embedding BLOB`

	// addSensitivityLabelsColumn adds the sensitivity_labels column for the
	// Retrieval Firewall. Existing rows get empty string (no labels).
	// Reference: Tech Spec Addendum Section A4.3.
	addSensitivityLabelsColumn = `ALTER TABLE memories ADD COLUMN sensitivity_labels TEXT NOT NULL DEFAULT ''`

	// addClassificationTierColumn adds the classification_tier column for the
	// Retrieval Firewall. Existing rows default to "public".
	// Reference: Tech Spec Addendum Section A4.3.
	addClassificationTierColumn = `ALTER TABLE memories ADD COLUMN classification_tier TEXT NOT NULL DEFAULT 'public'`

	// createClassificationTierIndex adds a B-tree index on classification_tier.
	// Reference: Tech Spec Addendum Section A4.3.
	createClassificationTierIndex = `CREATE INDEX IF NOT EXISTS idx_memories_classification ON memories(classification_tier)`

	// addTierColumn adds the numeric access tier column.
	// Default 1 (internal) for backward compat with pre-v0.1.3 entries.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	addTierColumn = `ALTER TABLE memories ADD COLUMN tier INTEGER NOT NULL DEFAULT 1`

	// createTierIndex covers the tier <= ? WHERE condition used in every query
	// that enforces source-level access control.
	createTierIndex = `CREATE INDEX IF NOT EXISTS idx_memories_tier ON memories(tier)`

	// createQueryIndex covers the primary Query() WHERE clause and ORDER BY:
	// WHERE namespace = ? AND destination = ? ORDER BY timestamp DESC.
	createQueryIndex = `CREATE INDEX IF NOT EXISTS idx_memories_query ON memories(namespace, destination, timestamp DESC)`

	// createSubjectIndex covers subject-filtered queries:
	// WHERE subject = ? ORDER BY timestamp DESC.
	createSubjectIndex = `CREATE INDEX IF NOT EXISTS idx_memories_subject ON memories(subject, timestamp DESC)`
)

// SQLiteDestination writes TranslatedPayload records to a SQLite database.
// The database is opened with PRAGMA journal_mode=WAL and
// PRAGMA busy_timeout=5000 to maximise concurrent read throughput and avoid
// "database is locked" errors under moderate write load.
//
// All SQL queries use parameterized statements — string concatenation for SQL
// is never used. Write is idempotent: INSERT OR IGNORE means re-delivering a
// payload_id is a no-op.
//
// All state is held in struct fields; there are no package-level variables.
type SQLiteDestination struct {
	db     *sql.DB
	path   string
	logger *slog.Logger
}

// OpenSQLite opens (or creates) a SQLite database at path, applies the
// required PRAGMAs, and creates the memories schema if absent. The parent
// directory is created with 0700 permissions; the database file is created
// with 0600 permissions.
//
// Panics if logger is nil. Returns an error if the database cannot be opened
// or the schema cannot be applied.
func OpenSQLite(path string, logger *slog.Logger) (*SQLiteDestination, error) {
	if logger == nil {
		panic("destination: SQLite logger must not be nil")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("destination: sqlite: create directory %q: %w", dir, err)
	}

	db, err := sql.Open(sqliteDriverName, path)
	if err != nil {
		return nil, fmt.Errorf("destination: sqlite: open %q: %w", path, err)
	}

	// SQLite is not safe for concurrent writes from multiple connections when
	// using the modernc driver unless we serialise through a single connection.
	// A pool of 1 write connection avoids "database is locked" errors from
	// concurrent goroutines hitting the same file handle.
	db.SetMaxOpenConns(1)

	d := &SQLiteDestination{
		db:     db,
		path:   path,
		logger: logger,
	}

	if err := d.applyPragmasAndSchema(); err != nil {
		if err := db.Close(); err != nil {
			slog.Default().Debug("close db", "err", err)
		}
		return nil, err
	}

	// Ensure the database file has restricted permissions (0600).
	// Best-effort on platforms where chmod is not supported (e.g. some Windows
	// configurations). The parent directory (0700) still limits access.
	_ = os.Chmod(path, 0600)

	logger.Info("destination: sqlite opened",
		"component", "destination",
		"path", path,
	)

	return d, nil
}

// applyPragmasAndSchema configures WAL journal mode, busy timeout, and
// creates the memories table + index.
func (d *SQLiteDestination) applyPragmasAndSchema() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
	}
	for _, p := range pragmas {
		if _, err := d.db.Exec(p); err != nil {
			return fmt.Errorf("destination: sqlite: exec %q: %w", p, err)
		}
	}

	if _, err := d.db.Exec(createMemoriesTable); err != nil {
		return fmt.Errorf("destination: sqlite: create memories table: %w", err)
	}
	if _, err := d.db.Exec(createIdempotencyKeyIndex); err != nil {
		return fmt.Errorf("destination: sqlite: create idempotency_key index: %w", err)
	}
	// Idempotent migration: add embedding column when absent.
	// SQLite does not support "ADD COLUMN IF NOT EXISTS", so we execute and
	// swallow the duplicate-column error on pre-existing databases.
	if _, err := d.db.Exec(addEmbeddingColumn); err != nil {
		// Error is expected when the column already exists. Log nothing —
		// subsequent queries will surface any true schema problems.
		_ = err
	}
	// Idempotent migration: add sensitivity_labels and classification_tier
	// columns for the Retrieval Firewall. Existing rows get safe defaults.
	// Reference: Tech Spec Addendum Section A4.3.
	if _, err := d.db.Exec(addSensitivityLabelsColumn); err != nil {
		_ = err // duplicate column — expected on existing databases
	}
	if _, err := d.db.Exec(addClassificationTierColumn); err != nil {
		_ = err // duplicate column — expected on existing databases
	}
	if _, err := d.db.Exec(createClassificationTierIndex); err != nil {
		return fmt.Errorf("destination: sqlite: create classification_tier index: %w", err)
	}
	// Idempotent migration: add numeric tier column for access control.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	if _, err := d.db.Exec(addTierColumn); err != nil {
		_ = err // duplicate column — expected on existing databases
	}
	if _, err := d.db.Exec(createTierIndex); err != nil {
		return fmt.Errorf("destination: sqlite: create tier index: %w", err)
	}
	if _, err := d.db.Exec(createQueryIndex); err != nil {
		return fmt.Errorf("destination: sqlite: create query index: %w", err)
	}
	if _, err := d.db.Exec(createSubjectIndex); err != nil {
		return fmt.Errorf("destination: sqlite: create subject index: %w", err)
	}
	return nil
}

// Write persists p to the memories table. The operation is idempotent:
// INSERT OR IGNORE silently discards a write whose payload_id already exists.
// All values are bound via parameterized placeholders — no string interpolation.
// If p.Embedding is non-empty it is serialized as a little-endian float32 BLOB
// and stored in the embedding column for Stage 4 semantic retrieval.
func (d *SQLiteDestination) Write(p TranslatedPayload) error {
	metadataJSON, err := marshalMetadata(p.Metadata)
	if err != nil {
		return fmt.Errorf("destination: sqlite: marshal metadata: %w", err)
	}

	embeddingBlob := encodeEmbedding(p.Embedding)

	// Encode sensitivity_labels as comma-separated string for SQLite TEXT column.
	// Reference: Tech Spec Addendum Section A3.2.
	sensitivityLabelsStr := strings.Join(p.SensitivityLabels, ",")
	classificationTier := p.ClassificationTier
	if classificationTier == "" {
		classificationTier = "public"
	}

	const query = `
INSERT OR IGNORE INTO memories (
    payload_id, request_id, source, subject, namespace, destination,
    collection, content, model, role, timestamp, idempotency_key,
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
		p.Timestamp.UTC().Format("2006-01-02T15:04:05.999999999Z"),
		p.IdempotencyKey,
		p.SchemaVersion,
		p.TransformVersion,
		p.ActorType,
		p.ActorID,
		metadataJSON,
		embeddingBlob,
		sensitivityLabelsStr,
		classificationTier,
		p.Tier,
	)
	if err != nil {
		return fmt.Errorf("destination: sqlite: write payload_id %q: %w", p.PayloadID, err)
	}

	d.logger.Debug("destination: sqlite: write",
		"component", "destination",
		"payload_id", p.PayloadID,
		"source", p.Source,
	)
	return nil
}

// CanSemanticSearch reports whether this SQLite destination has any rows with a
// stored embedding. Returns false when the table is empty or no records have
// embeddings, signalling Stage 4 to degrade gracefully.
//
// Reference: Tech Spec Section 3.4 — Stage 4.
func (d *SQLiteDestination) CanSemanticSearch() bool {
	const q = `SELECT 1 FROM memories WHERE embedding IS NOT NULL AND length(embedding) > 0 LIMIT 1`
	var dummy int
	err := d.db.QueryRow(q).Scan(&dummy)
	return err == nil
}

// SemanticSearch performs application-level cosine similarity search over all
// stored embeddings. It fetches candidate rows filtered by namespace and
// destination, computes cosine similarity against vec in Go, and returns the
// top-N results by score descending.
//
// This implementation does not use sqlite-vec (which requires CGO). It provides
// correct cosine-ranked results using the modernc pure-Go driver.
//
// The over-sample factor is applied to the SQL LIMIT to increase recall before
// in-process reranking: we fetch min(overSample * limit, totalRows) candidates.
//
// Reference: Tech Spec Section 3.4 — Stage 4.
func (d *SQLiteDestination) SemanticSearch(ctx context.Context, vec []float32, params QueryParams) ([]ScoredRecord, error) {
	limit := ClampLimit(params.Limit)

	// The caller (CascadeRunner) already applies over-sampling before calling
	// SemanticSearch, so params.Limit is already the over-sampled value.
	// We use it directly as the candidate fetch limit, with a floor of 100
	// to ensure adequate recall even when small limits are requested.
	candidateLimit := limit
	if candidateLimit < 100 {
		candidateLimit = 100
	}

	// Build parameterized WHERE clause filtering by namespace and destination.
	args := make([]interface{}, 0, 4)
	conditions := []string{"embedding IS NOT NULL AND length(embedding) > 0"}

	if params.Namespace != "" {
		conditions = append(conditions, "namespace = ?")
		args = append(args, params.Namespace)
	}
	if params.Destination != "" {
		conditions = append(conditions, "destination = ?")
		args = append(args, params.Destination)
	}
	// SQL-layer tier enforcement mirrors the Query() implementation.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	if params.TierFilter {
		conditions = append(conditions, "tier <= ?")
		args = append(args, params.SourceTier)
	}

	whereClause := "WHERE " + joinConditions(conditions)
	args = append(args, candidateLimit)

	//nolint:gosec // whereClause is built from a fixed set of conditions — no user input.
	q := "SELECT payload_id, request_id, source, subject, namespace, destination, collection, content, model, role, timestamp, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, embedding, sensitivity_labels, classification_tier, tier FROM memories " + whereClause + " ORDER BY timestamp DESC LIMIT ?"

	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("destination: sqlite: semantic search: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Default().Debug("close rows", "err", err)
		}
	}()

	scored := make([]ScoredRecord, 0, candidateLimit)
	for rows.Next() {
		var tp TranslatedPayload
		var timestampStr, metadataStr string
		var embeddingBlob []byte
		var sensitivityLabelsStr string

		if err := rows.Scan(
			&tp.PayloadID, &tp.RequestID, &tp.Source, &tp.Subject, &tp.Namespace,
			&tp.Destination, &tp.Collection, &tp.Content, &tp.Model, &tp.Role,
			&timestampStr, &tp.IdempotencyKey, &tp.SchemaVersion, &tp.TransformVersion,
			&tp.ActorType, &tp.ActorID, &metadataStr, &embeddingBlob,
			&sensitivityLabelsStr, &tp.ClassificationTier, &tp.Tier,
		); err != nil {
			return nil, fmt.Errorf("destination: sqlite: semantic search: scan row: %w", err)
		}
		if sensitivityLabelsStr != "" {
			tp.SensitivityLabels = strings.Split(sensitivityLabelsStr, ",")
		}

		rowVec := decodeEmbedding(embeddingBlob)
		if len(rowVec) == 0 {
			continue
		}

		score := cosineSimilarity(vec, rowVec)

		if t, parseErr := parseTimestamp(timestampStr); parseErr == nil {
			tp.Timestamp = t
		}
		if metadataStr != "" && metadataStr != "{}" {
			_ = json.Unmarshal([]byte(metadataStr), &tp.Metadata)
		}
		tp.Embedding = rowVec

		scored = append(scored, ScoredRecord{Payload: tp, Score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("destination: sqlite: semantic search: rows error: %w", err)
	}

	// Sort by cosine similarity descending (highest similarity first).
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// Trim to the requested limit.
	if len(scored) > limit {
		scored = scored[:limit]
	}

	return scored, nil
}

// encodeEmbedding serialises a []float32 to a little-endian byte slice for
// storage as a BLOB. Returns nil for an empty or nil slice.
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
// Returns nil for a nil or empty or malformed blob.
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
// Returns 0 when either vector is zero-length or has zero norm.
func cosineSimilarity(a, b []float32) float32 {
	n := len(a)
	if n == 0 || len(b) < n {
		return 0
	}
	var dot, normA, normB float32
	for i := 0; i < n; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))
}

// MemoryCount returns the total number of memory records in the SQLite
// destination. Used by the /api/status admin endpoint.
func (d *SQLiteDestination) MemoryCount() (int64, error) {
	var count int64
	if err := d.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count); err != nil {
		return 0, fmt.Errorf("destination: sqlite: memory count: %w", err)
	}
	return count, nil
}

// Ping verifies the database connection is alive by executing a lightweight
// query. Used by the doctor command and /ready health endpoint.
func (d *SQLiteDestination) Ping() error {
	if err := d.db.Ping(); err != nil {
		return fmt.Errorf("destination: sqlite: ping: %w", err)
	}
	return nil
}

// Exists reports whether a record with payloadID exists in the memories table.
// Used by consistency assertions (Phase R-10).
func (d *SQLiteDestination) Exists(payloadID string) (bool, error) {
	const query = `SELECT 1 FROM memories WHERE payload_id = ? LIMIT 1`
	var dummy int
	err := d.db.QueryRow(query, payloadID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("destination: sqlite: exists %q: %w", payloadID, err)
	}
	return true, nil
}

// DeletePayload removes a single record by payload_id. Used by consistency
// assertion tests to verify that the checker detects missing entries.
// Returns the number of rows deleted (0 or 1).
func (d *SQLiteDestination) DeletePayload(payloadID string) (int64, error) {
	const query = `DELETE FROM memories WHERE payload_id = ?`
	res, err := d.db.Exec(query, payloadID)
	if err != nil {
		return 0, fmt.Errorf("destination: sqlite: delete %q: %w", payloadID, err)
	}
	return res.RowsAffected()
}

// Query returns a page of memories matching params using a basic structured
// query on the memories table. It implements the Querier interface for Phase 0C.
// The full 6-stage retrieval cascade (Phase 3+) replaces this for production reads.
//
// All SQL is parameterized — no string concatenation. Limit and offset are
// computed from params.Limit (clamped) and the decoded cursor.
//
// Reference: Tech Spec Section 3.3, Phase 0C Behavioral Contract item 16.
func (d *SQLiteDestination) Query(params QueryParams) (QueryResult, error) {
	limit := ClampLimit(params.Limit)
	offset, err := DecodeCursor(params.Cursor)
	if err != nil {
		return QueryResult{}, fmt.Errorf("destination: sqlite: query: decode cursor: %w", err)
	}

	// Build parameterized WHERE conditions.
	// NEVER use string concatenation for SQL.
	args := make([]interface{}, 0, 5)
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
	// SQL-layer tier enforcement: source can only read entries at tier <= its own
	// tier level. Applied in the WHERE clause so the DB never touches unauthorised
	// rows — closes timing side-channels from row-count variation.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	if params.TierFilter {
		conditions = append(conditions, "tier <= ?")
		args = append(args, params.SourceTier)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + joinConditions(conditions)
	}

	// Fetch limit+1 rows to determine HasMore without a separate COUNT query.
	fetchLimit := limit + 1
	args = append(args, fetchLimit, offset)

	//nolint:gosec // whereClause is built from a fixed set of conditions — no user input.
	query := "SELECT payload_id, request_id, source, subject, namespace, destination, collection, content, model, role, timestamp, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, tier FROM memories " + whereClause + " ORDER BY timestamp DESC LIMIT ? OFFSET ?"

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return QueryResult{}, fmt.Errorf("destination: sqlite: query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Debug("destination: sqlite: close rows", "error", err)
		}
	}()

	records := make([]TranslatedPayload, 0, limit)
	for rows.Next() {
		var tp TranslatedPayload
		var timestampStr string
		var metadataStr string
		var sensitivityLabelsStr string
		if err := rows.Scan(
			&tp.PayloadID,
			&tp.RequestID,
			&tp.Source,
			&tp.Subject,
			&tp.Namespace,
			&tp.Destination,
			&tp.Collection,
			&tp.Content,
			&tp.Model,
			&tp.Role,
			&timestampStr,
			&tp.IdempotencyKey,
			&tp.SchemaVersion,
			&tp.TransformVersion,
			&tp.ActorType,
			&tp.ActorID,
			&metadataStr,
			&sensitivityLabelsStr,
			&tp.ClassificationTier,
			&tp.Tier,
		); err != nil {
			return QueryResult{}, fmt.Errorf("destination: sqlite: query: scan row: %w", err)
		}

		// Parse sensitivity_labels from comma-separated TEXT.
		if sensitivityLabelsStr != "" {
			tp.SensitivityLabels = strings.Split(sensitivityLabelsStr, ",")
		}

		// Parse timestamp — stored as RFC3339Nano.
		if t, parseErr := parseTimestamp(timestampStr); parseErr == nil {
			tp.Timestamp = t
		}

		// Parse metadata JSON back to map.
		if metadataStr != "" && metadataStr != "{}" {
			if err := json.Unmarshal([]byte(metadataStr), &tp.Metadata); err != nil {
				d.logger.Warn("destination: sqlite: query: unmarshal metadata",
					"component", "destination",
					"payload_id", tp.PayloadID,
					"error", err,
				)
			}
		}

		records = append(records, tp)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("destination: sqlite: query: rows error: %w", err)
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}

	var nextCursor string
	if hasMore {
		nextCursor = EncodeCursor(offset + limit)
	}

	return QueryResult{
		Records:    records,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// joinConditions joins SQL WHERE conditions with " AND ".
// Inputs are fixed condition strings, never user-supplied values.
// Returns "" if conds is empty.
func joinConditions(conds []string) string {
	if len(conds) == 0 {
		return ""
	}
	result := conds[0]
	for _, c := range conds[1:] {
		result += " AND " + c
	}
	return result
}

// parseTimestamp parses a stored timestamp string in RFC3339Nano or
// "2006-01-02T15:04:05.999999999Z" format.
func parseTimestamp(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// Close closes the underlying database connection. Safe to call once.
func (d *SQLiteDestination) Close() error {
	if err := d.db.Close(); err != nil {
		return fmt.Errorf("destination: sqlite: close: %w", err)
	}
	return nil
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

// QueryConflicts returns groups of contradictory memories. It groups by
// subject + collection (entity_key) and returns groups where multiple distinct
// content values exist.
//
// All SQL is parameterized. NEVER string concatenation.
// Reference: Tech Spec Section 13.2.
func (d *SQLiteDestination) QueryConflicts(params ConflictParams) ([]ConflictGroup, error) {
	limit := ClampLimit(params.Limit)

	args := make([]interface{}, 0, 4)
	conditions := make([]string, 0, 3)

	if params.Source != "" {
		conditions = append(conditions, "source = ?")
		args = append(args, params.Source)
	}
	if params.Subject != "" {
		conditions = append(conditions, "subject = ?")
		args = append(args, params.Subject)
	}
	if params.ActorType != "" {
		conditions = append(conditions, "actor_type = ?")
		args = append(args, params.ActorType)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + joinConditions(conditions)
	}

	// Find subject+collection groups with more than one distinct content value.
	//nolint:gosec // whereClause from fixed conditions — no user input.
	query := `SELECT subject, collection, COUNT(DISTINCT content) as cnt
		FROM memories ` + whereClause + `
		GROUP BY subject, collection
		HAVING COUNT(DISTINCT content) > 1
		ORDER BY cnt DESC
		LIMIT ? OFFSET ?`

	args = append(args, limit, params.Offset)
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("destination: sqlite: query conflicts: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Debug("destination: sqlite: close rows", "error", err)
		}
	}()

	type groupKey struct {
		subject    string
		collection string
	}
	keys := make([]groupKey, 0, limit)
	for rows.Next() {
		var gk groupKey
		var cnt int
		if err := rows.Scan(&gk.subject, &gk.collection, &cnt); err != nil {
			return nil, fmt.Errorf("destination: sqlite: scan conflict group: %w", err)
		}
		keys = append(keys, gk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("destination: sqlite: conflict rows: %w", err)
	}

	// For each group, fetch the distinct values.
	groups := make([]ConflictGroup, 0, len(keys))
	for _, gk := range keys {
		g, scanErr := d.scanConflictDetail(gk.subject, gk.collection)
		if scanErr != nil {
			return nil, scanErr
		}
		groups = append(groups, g)
	}

	return groups, nil
}

// scanConflictDetail fetches distinct content/source/timestamp entries for a
// single subject+collection conflict group.
func (d *SQLiteDestination) scanConflictDetail(subject, collection string) (ConflictGroup, error) {
	rows, err := d.db.Query(
		"SELECT DISTINCT content, source, timestamp FROM memories WHERE subject = ? AND collection = ? ORDER BY timestamp DESC",
		subject, collection,
	)
	if err != nil {
		return ConflictGroup{}, fmt.Errorf("destination: sqlite: conflict detail query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	g := ConflictGroup{
		Subject:   subject,
		EntityKey: collection,
	}
	for rows.Next() {
		var content, src, ts string
		if err := rows.Scan(&content, &src, &ts); err != nil {
			return ConflictGroup{}, fmt.Errorf("destination: sqlite: scan conflict detail: %w", err)
		}
		g.ConflictingValues = append(g.ConflictingValues, content)
		g.Sources = append(g.Sources, src)
		parsed, _ := time.Parse(time.RFC3339Nano, ts)
		g.Timestamps = append(g.Timestamps, parsed)
	}
	if err := rows.Err(); err != nil {
		return ConflictGroup{}, fmt.Errorf("destination: sqlite: conflict detail rows: %w", err)
	}
	g.Count = len(g.ConflictingValues)
	return g, nil
}

// QueryTimeTravel returns memories as of the specified timestamp. Results are
// filtered to timestamp <= as_of and ordered by timestamp DESC.
//
// All SQL is parameterized. NEVER string concatenation.
// Reference: Tech Spec Section 13.2.
func (d *SQLiteDestination) QueryTimeTravel(params TimeTravelParams) (QueryResult, error) {
	limit := ClampLimit(params.Limit)
	offset := params.Offset

	args := make([]interface{}, 0, 5)
	conditions := []string{"timestamp <= ?"}
	args = append(args, params.AsOf.Format(time.RFC3339Nano))

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

	whereClause := "WHERE " + joinConditions(conditions)

	fetchLimit := limit + 1
	args = append(args, fetchLimit, offset)

	//nolint:gosec // whereClause from fixed conditions — no user input.
	query := "SELECT payload_id, request_id, source, subject, namespace, destination, collection, content, model, role, timestamp, idempotency_key, schema_version, transform_version, actor_type, actor_id, metadata, sensitivity_labels, classification_tier, tier FROM memories " + whereClause + " ORDER BY timestamp DESC LIMIT ? OFFSET ?"

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return QueryResult{}, fmt.Errorf("destination: sqlite: time travel query: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slog.Debug("destination: sqlite: close rows", "error", err)
		}
	}()

	records := make([]TranslatedPayload, 0, limit)
	for rows.Next() {
		var rec TranslatedPayload
		var ts, metaStr, sensitivityLabelsStr string
		if err := rows.Scan(
			&rec.PayloadID, &rec.RequestID, &rec.Source, &rec.Subject,
			&rec.Namespace, &rec.Destination, &rec.Collection, &rec.Content,
			&rec.Model, &rec.Role, &ts, &rec.IdempotencyKey,
			&rec.SchemaVersion, &rec.TransformVersion, &rec.ActorType,
			&rec.ActorID, &metaStr, &sensitivityLabelsStr, &rec.ClassificationTier,
			&rec.Tier,
		); err != nil {
			return QueryResult{}, fmt.Errorf("destination: sqlite: time travel scan: %w", err)
		}
		if sensitivityLabelsStr != "" {
			rec.SensitivityLabels = strings.Split(sensitivityLabelsStr, ",")
		}
		rec.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		if metaStr != "" && metaStr != "{}" {
			_ = json.Unmarshal([]byte(metaStr), &rec.Metadata)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("destination: sqlite: time travel rows: %w", err)
	}

	result := QueryResult{Records: records}
	if len(records) > limit {
		result.Records = records[:limit]
		result.HasMore = true
		result.NextCursor = EncodeCursor(offset + limit)
	}
	return result, nil
}
