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

// Package mongodb provides a MongoDB destination adapter for BubbleFish Nexus.
//
// Vector search is implemented with application-level cosine similarity —
// there is no dependency on MongoDB Atlas Vector Search. All queries use
// parameterised BSON filters — no string concatenation. Writes are idempotent
// via ReplaceOne with upsert.
package mongodb

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// Compile-time proof that MongoDBDestination satisfies the Destination interface.
var _ destination.Destination = (*MongoDBDestination)(nil)

// collectionName is the MongoDB collection that stores memory records.
const collectionName = "memories"

// defaultDatabase is used when the URI path does not specify a database name.
const defaultDatabase = "nexus"

// memoryDoc is the BSON representation of a TranslatedPayload stored in MongoDB.
// Timestamps are stored as BSON Date (UTC). Embeddings are stored as little-endian
// float32 byte slices. Metadata is a native BSON document (map[string]string).
type memoryDoc struct {
	ID                 string            `bson:"_id"`
	RequestID          string            `bson:"request_id"`
	Source             string            `bson:"source"`
	Subject            string            `bson:"subject"`
	Namespace          string            `bson:"namespace"`
	Destination        string            `bson:"destination"`
	Collection         string            `bson:"collection"`
	Content            string            `bson:"content"`
	Model              string            `bson:"model"`
	Role               string            `bson:"role"`
	Timestamp          time.Time         `bson:"timestamp"`
	IdempotencyKey     string            `bson:"idempotency_key"`
	SchemaVersion      int               `bson:"schema_version"`
	TransformVersion   string            `bson:"transform_version"`
	ActorType          string            `bson:"actor_type"`
	ActorID            string            `bson:"actor_id"`
	Metadata           map[string]string `bson:"metadata"`
	Embedding          []byte            `bson:"embedding,omitempty"`
	SensitivityLabels  []string          `bson:"sensitivity_labels,omitempty"`
	ClassificationTier string            `bson:"classification_tier"`
	Tier               int               `bson:"tier"`
	CreatedAt          time.Time         `bson:"created_at"`
}

// MongoDBDestination writes TranslatedPayload records to a MongoDB database.
// Vector search uses application-level cosine similarity — there is no
// dependency on MongoDB Atlas Vector Search. All queries use parameterised
// BSON filters. Writes are idempotent via ReplaceOne with upsert.
//
// The adapter is safe for concurrent use.
type MongoDBDestination struct {
	client     *mongo.Client
	db         *mongo.Database
	collection *mongo.Collection
	logger     *slog.Logger
}

// Open connects to the MongoDB server identified by uri, selects the database
// named in the URI path (defaulting to "nexus"), ensures the memories
// collection has the required indexes, and returns the adapter.
//
// uri is a standard MongoDB connection string, e.g.:
// "mongodb://localhost:27017" or
// "mongodb+srv://user:pass@cluster.mongodb.net/dbname?retryWrites=true&w=majority"
func Open(uri string, logger *slog.Logger) (*MongoDBDestination, error) {
	if logger == nil {
		panic("destination: mongodb: logger must not be nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("destination: mongodb: connect: %w", err)
	}

	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, fmt.Errorf("destination: mongodb: ping: %w", err)
	}

	dbName := extractDBName(uri)
	db := client.Database(dbName)
	coll := db.Collection(collectionName)

	d := &MongoDBDestination{
		client:     client,
		db:         db,
		collection: coll,
		logger:     logger,
	}

	if err := d.createIndexes(ctx); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}

	logger.Info("destination: mongodb opened", "component", "destination", "db", dbName)
	return d, nil
}

// createIndexes ensures the required indexes exist on the memories collection.
// MongoDB's CreateMany is idempotent for identical index key definitions.
func (d *MongoDBDestination) createIndexes(ctx context.Context) error {
	models := []mongo.IndexModel{
		{Keys: bson.D{{Key: "idempotency_key", Value: 1}}},
		{Keys: bson.D{{Key: "classification_tier", Value: 1}}},
		{Keys: bson.D{{Key: "tier", Value: 1}}},
		{
			Keys: bson.D{
				{Key: "namespace", Value: 1},
				{Key: "destination", Value: 1},
				{Key: "timestamp", Value: -1},
			},
		},
		{
			Keys: bson.D{
				{Key: "subject", Value: 1},
				{Key: "timestamp", Value: -1},
			},
		},
	}
	if _, err := d.collection.Indexes().CreateMany(ctx, models); err != nil {
		return fmt.Errorf("destination: mongodb: create indexes: %w", err)
	}
	return nil
}

// Name returns the stable identifier for this destination.
func (d *MongoDBDestination) Name() string { return "mongodb" }

// Write persists p to the memories collection. Idempotent via ReplaceOne with
// upsert=true keyed on payload_id (_id). If p.Embedding is non-empty it is
// serialised as a little-endian float32 byte slice.
func (d *MongoDBDestination) Write(p destination.TranslatedPayload) error {
	doc := docFromPayload(p)
	filter := bson.D{{Key: "_id", Value: doc.ID}}
	opts := options.Replace().SetUpsert(true)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := d.collection.ReplaceOne(ctx, filter, doc, opts); err != nil {
		return fmt.Errorf("destination: mongodb: write payload_id %q: %w", p.PayloadID, err)
	}

	d.logger.Debug("destination: mongodb: write",
		"component", "destination",
		"payload_id", p.PayloadID,
	)
	return nil
}

// Read retrieves a single memory record by its PayloadID. Returns nil, nil
// when the record does not exist.
func (d *MongoDBDestination) Read(ctx context.Context, id string) (*destination.Memory, error) {
	filter := bson.D{{Key: "_id", Value: id}}
	var doc memoryDoc
	err := d.collection.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("destination: mongodb: read %q: %w", id, err)
	}
	p := payloadFromDoc(doc)
	return &p, nil
}

// Search returns memories matching query, converting the result to a slice of
// pointers. Returns an empty (non-nil) slice when no records match.
func (d *MongoDBDestination) Search(ctx context.Context, query *destination.Query) ([]*destination.Memory, error) {
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
func (d *MongoDBDestination) Delete(ctx context.Context, id string) error {
	filter := bson.D{{Key: "_id", Value: id}}
	if _, err := d.collection.DeleteOne(ctx, filter); err != nil {
		return fmt.Errorf("destination: mongodb: delete %q: %w", id, err)
	}
	return nil
}

// VectorSearch returns up to limit memories ranked by cosine similarity to
// embedding using application-level computation. Returns an empty slice for
// a nil or empty embedding.
func (d *MongoDBDestination) VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*destination.Memory, error) {
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

// Migrate is a no-op for MongoDB: all indexes are created idempotently at
// open time inside createIndexes. The version argument is reserved for future
// use when explicit versioned migrations are required.
func (d *MongoDBDestination) Migrate(_ context.Context, _ int) error {
	return nil
}

// Health performs a lightweight liveness probe by pinging the MongoDB server
// and measuring round-trip latency. Does NOT modify any stored data.
func (d *MongoDBDestination) Health(ctx context.Context) (*destination.HealthStatus, error) {
	start := time.Now()
	if err := d.client.Ping(ctx, readpref.Primary()); err != nil {
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

// Close disconnects the MongoDB client and releases all resources.
func (d *MongoDBDestination) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return d.client.Disconnect(ctx)
}

// Ping verifies the database connection is alive. Satisfies DestinationWriter.
func (d *MongoDBDestination) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := d.client.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("destination: mongodb: ping: %w", err)
	}
	return nil
}

// Exists reports whether a record with payloadID exists in the memories collection.
func (d *MongoDBDestination) Exists(payloadID string) (bool, error) {
	filter := bson.D{{Key: "_id", Value: payloadID}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := d.collection.FindOne(ctx, filter).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("destination: mongodb: exists %q: %w", payloadID, err)
	}
	return true, nil
}

// Query returns a page of memories using parameterised BSON filters.
// Pagination uses skip/limit with a base64-encoded offset cursor (same scheme
// as the SQLite, PostgreSQL, MySQL, and CockroachDB adapters).
func (d *MongoDBDestination) Query(params destination.QueryParams) (destination.QueryResult, error) {
	limit := destination.ClampLimit(params.Limit)
	offset, err := destination.DecodeCursor(params.Cursor)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: mongodb: query: %w", err)
	}

	filter := bson.D{}
	if params.Namespace != "" {
		filter = append(filter, bson.E{Key: "namespace", Value: params.Namespace})
	}
	if params.Destination != "" {
		filter = append(filter, bson.E{Key: "destination", Value: params.Destination})
	}
	if params.Subject != "" {
		filter = append(filter, bson.E{Key: "subject", Value: params.Subject})
	}
	if params.Q != "" {
		filter = append(filter, bson.E{Key: "content", Value: bson.D{
			{Key: "$regex", Value: regexp.QuoteMeta(params.Q)},
			{Key: "$options", Value: "i"},
		}})
	}
	if params.ActorType != "" {
		filter = append(filter, bson.E{Key: "actor_type", Value: params.ActorType})
	}
	if params.TierFilter {
		filter = append(filter, bson.E{Key: "tier", Value: bson.D{{Key: "$lte", Value: params.SourceTier}}})
	}

	fetchLimit := int64(limit + 1)
	findOpts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetSkip(int64(offset)).
		SetLimit(fetchLimit)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := d.collection.Find(ctx, filter, findOpts)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: mongodb: query: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			d.logger.Debug("destination: mongodb: close cursor", "err", err)
		}
	}()

	var docs []memoryDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: mongodb: query: decode: %w", err)
	}

	records := make([]destination.TranslatedPayload, 0, len(docs))
	for _, doc := range docs {
		records = append(records, payloadFromDoc(doc))
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

// CanSemanticSearch reports whether any document in the collection has a
// stored embedding, indicating app-level vector search is usable.
func (d *MongoDBDestination) CanSemanticSearch() bool {
	filter := bson.D{{Key: "embedding", Value: bson.D{{Key: "$exists", Value: true}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return d.collection.FindOne(ctx, filter).Err() == nil
}

// SemanticSearch performs application-level cosine similarity search.
// It fetches documents with stored embeddings (filtered by namespace/destination/
// tier when set), decodes them in Go, ranks by cosine similarity, and returns
// the top params.Limit results.
//
// This approach is O(n) in embedding count. For large datasets, operators
// should consider MongoDB Atlas Vector Search.
func (d *MongoDBDestination) SemanticSearch(ctx context.Context, vec []float32, params destination.QueryParams) ([]destination.ScoredRecord, error) {
	limit := destination.ClampLimit(params.Limit)

	filter := bson.D{{Key: "embedding", Value: bson.D{{Key: "$exists", Value: true}}}}
	if params.Namespace != "" {
		filter = append(filter, bson.E{Key: "namespace", Value: params.Namespace})
	}
	if params.Destination != "" {
		filter = append(filter, bson.E{Key: "destination", Value: params.Destination})
	}
	if params.TierFilter {
		filter = append(filter, bson.E{Key: "tier", Value: bson.D{{Key: "$lte", Value: params.SourceTier}}})
	}

	cursor, err := d.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("destination: mongodb: semantic search: %w", err)
	}
	defer func() {
		if err := cursor.Close(ctx); err != nil {
			d.logger.Debug("destination: mongodb: close semantic cursor", "err", err)
		}
	}()

	type candidate struct {
		payload destination.TranslatedPayload
		score   float32
	}
	var candidates []candidate

	for cursor.Next(ctx) {
		var doc memoryDoc
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("destination: mongodb: semantic search: decode: %w", err)
		}
		rowVec := decodeEmbedding(doc.Embedding)
		if len(rowVec) == 0 {
			continue
		}
		score := cosineSimilarity(vec, rowVec)
		candidates = append(candidates, candidate{payload: payloadFromDoc(doc), score: score})
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("destination: mongodb: semantic search: cursor: %w", err)
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

// docFromPayload converts a TranslatedPayload to a memoryDoc for BSON storage.
// ClassificationTier defaults to "public" when empty.
func docFromPayload(p destination.TranslatedPayload) memoryDoc {
	ct := p.ClassificationTier
	if ct == "" {
		ct = "public"
	}
	return memoryDoc{
		ID:                 p.PayloadID,
		RequestID:          p.RequestID,
		Source:             p.Source,
		Subject:            p.Subject,
		Namespace:          p.Namespace,
		Destination:        p.Destination,
		Collection:         p.Collection,
		Content:            p.Content,
		Model:              p.Model,
		Role:               p.Role,
		Timestamp:          p.Timestamp.UTC(),
		IdempotencyKey:     p.IdempotencyKey,
		SchemaVersion:      p.SchemaVersion,
		TransformVersion:   p.TransformVersion,
		ActorType:          p.ActorType,
		ActorID:            p.ActorID,
		Metadata:           p.Metadata,
		Embedding:          encodeEmbedding(p.Embedding),
		SensitivityLabels:  p.SensitivityLabels,
		ClassificationTier: ct,
		Tier:               p.Tier,
		CreatedAt:          time.Now().UTC(),
	}
}

// payloadFromDoc converts a stored memoryDoc back to a TranslatedPayload.
func payloadFromDoc(d memoryDoc) destination.TranslatedPayload {
	return destination.TranslatedPayload{
		PayloadID:          d.ID,
		RequestID:          d.RequestID,
		Source:             d.Source,
		Subject:            d.Subject,
		Namespace:          d.Namespace,
		Destination:        d.Destination,
		Collection:         d.Collection,
		Content:            d.Content,
		Model:              d.Model,
		Role:               d.Role,
		Timestamp:          d.Timestamp.UTC(),
		IdempotencyKey:     d.IdempotencyKey,
		SchemaVersion:      d.SchemaVersion,
		TransformVersion:   d.TransformVersion,
		ActorType:          d.ActorType,
		ActorID:            d.ActorID,
		Metadata:           d.Metadata,
		Embedding:          decodeEmbedding(d.Embedding),
		SensitivityLabels:  d.SensitivityLabels,
		ClassificationTier: d.ClassificationTier,
		Tier:               d.Tier,
	}
}

// extractDBName parses the database name from a MongoDB URI path component.
// Falls back to defaultDatabase when not specified.
func extractDBName(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return defaultDatabase
	}
	path := strings.TrimPrefix(u.Path, "/")
	if path == "" {
		return defaultDatabase
	}
	return path
}

// encodeEmbedding serialises a []float32 to a little-endian byte slice for
// storage. Returns nil for an empty slice.
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

// decodeEmbedding deserialises a little-endian byte slice back to []float32.
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
