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

// Package firestore provides a Firebase/Firestore destination adapter for
// BubbleFish Nexus.
//
// Memories are stored as Firestore documents with payload_id as the document
// ID. Authentication uses Application Default Credentials; set the
// GOOGLE_APPLICATION_CREDENTIALS environment variable to a service account
// JSON file when running outside Google Cloud. Vector search is not supported
// — VectorSearch returns ErrVectorSearchUnsupported and Stage 4 of the
// retrieval cascade is skipped.
//
// Firestore does not support substring text search natively; the Query
// content filter (params.Q) is applied client-side after fetching all
// matching documents from other filters.
package firestore

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gcfirestore "cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"github.com/bubblefish-tech/nexus/internal/destination"
)

// Compile-time proof that FirestoreDestination satisfies the Destination interface.
var _ destination.Destination = (*FirestoreDestination)(nil)

// memoriesCollection is the Firestore collection that stores memory documents.
const memoriesCollection = "memories"

// memoryDoc is the Firestore document representation of a TranslatedPayload.
// Embedding is stored as []float64 (Firestore native float array).
// Metadata is stored as a native Firestore map.
type memoryDoc struct {
	PayloadID          string            `firestore:"payload_id"`
	RequestID          string            `firestore:"request_id"`
	Source             string            `firestore:"source"`
	Subject            string            `firestore:"subject"`
	Namespace          string            `firestore:"namespace"`
	Destination        string            `firestore:"destination"`
	Collection         string            `firestore:"collection"`
	Content            string            `firestore:"content"`
	Model              string            `firestore:"model"`
	Role               string            `firestore:"role"`
	Timestamp          time.Time         `firestore:"timestamp"`
	IdempotencyKey     string            `firestore:"idempotency_key"`
	SchemaVersion      int               `firestore:"schema_version"`
	TransformVersion   string            `firestore:"transform_version"`
	ActorType          string            `firestore:"actor_type"`
	ActorID            string            `firestore:"actor_id"`
	Metadata           map[string]string `firestore:"metadata"`
	Embedding          []float64         `firestore:"embedding,omitempty"`
	SensitivityLabels  []string          `firestore:"sensitivity_labels,omitempty"`
	ClassificationTier string            `firestore:"classification_tier"`
	Tier               int               `firestore:"tier"`
	CreatedAt          time.Time         `firestore:"created_at"`
}

// FirestoreDestination writes TranslatedPayload records to Google Cloud
// Firestore. Writes are idempotent via document Set (overwrite). Vector
// search is not supported — VectorSearch returns ErrVectorSearchUnsupported.
//
// The adapter is safe for concurrent use.
type FirestoreDestination struct {
	client *gcfirestore.Client
	coll   *gcfirestore.CollectionRef
	logger *slog.Logger
}

// Open connects to Firestore in the given Google Cloud project and returns the
// adapter. projectID is the Google Cloud project identifier (e.g.
// "my-project-123"). If credentialsFile is non-empty, it is used for
// authentication; otherwise Application Default Credentials are used.
func Open(projectID string, logger *slog.Logger) (*FirestoreDestination, error) {
	return OpenWithCredentials(projectID, "", logger)
}

// OpenWithCredentials connects to Firestore in projectID. If credentialsFile
// is non-empty it is used; otherwise Application Default Credentials are used.
func OpenWithCredentials(projectID, credentialsFile string, logger *slog.Logger) (*FirestoreDestination, error) {
	if logger == nil {
		panic("destination: firestore: logger must not be nil")
	}
	if projectID == "" {
		return nil, fmt.Errorf("destination: firestore: projectID must not be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var opts []option.ClientOption
	if credentialsFile != "" {
		opts = append(opts, option.WithCredentialsFile(credentialsFile))
	}

	client, err := gcfirestore.NewClient(ctx, projectID, opts...)
	if err != nil {
		return nil, fmt.Errorf("destination: firestore: new client: %w", err)
	}

	d := &FirestoreDestination{
		client: client,
		coll:   client.Collection(memoriesCollection),
		logger: logger,
	}

	logger.Info("destination: firestore opened", "component", "destination", "project", projectID)
	return d, nil
}

// Name returns the stable identifier for this destination.
func (d *FirestoreDestination) Name() string { return "firestore" }

// Write persists p to Firestore. Idempotent via document Set which overwrites
// an existing document with the same payload_id. All field values are typed
// natively — no string concatenation.
func (d *FirestoreDestination) Write(p destination.TranslatedPayload) error {
	doc := docFromPayload(p)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := d.coll.Doc(p.PayloadID).Set(ctx, doc); err != nil {
		return fmt.Errorf("destination: firestore: write payload_id %q: %w", p.PayloadID, err)
	}

	d.logger.Debug("destination: firestore: write",
		"component", "destination",
		"payload_id", p.PayloadID,
	)
	return nil
}

// Read retrieves a single memory record by its PayloadID. Returns nil, nil
// when the document does not exist.
func (d *FirestoreDestination) Read(ctx context.Context, id string) (*destination.Memory, error) {
	snap, err := d.coll.Doc(id).Get(ctx)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("destination: firestore: read %q: %w", id, err)
	}
	var doc memoryDoc
	if err := snap.DataTo(&doc); err != nil {
		return nil, fmt.Errorf("destination: firestore: read %q: decode: %w", id, err)
	}
	p := payloadFromDoc(doc)
	return &p, nil
}

// Search returns memories matching query. Content text filter (params.Q) is
// applied client-side because Firestore does not support substring search.
// Returns an empty (non-nil) slice when no records match.
func (d *FirestoreDestination) Search(ctx context.Context, query *destination.Query) ([]*destination.Memory, error) {
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

// Delete removes the document with the given PayloadID. Deletion of a
// non-existent document is a no-op (idempotent).
func (d *FirestoreDestination) Delete(ctx context.Context, id string) error {
	if _, err := d.coll.Doc(id).Delete(ctx); err != nil {
		return fmt.Errorf("destination: firestore: delete %q: %w", id, err)
	}
	return nil
}

// VectorSearch returns ErrVectorSearchUnsupported. Firestore does not
// support vector similarity search; Stage 4 of the retrieval cascade
// is skipped when this adapter is in use.
func (d *FirestoreDestination) VectorSearch(_ context.Context, _ []float32, _ int) ([]*destination.Memory, error) {
	return nil, destination.ErrVectorSearchUnsupported
}

// Migrate is a no-op for Firestore. Firestore is schemaless; there are no
// migrations to apply. The version argument is reserved for future use.
func (d *FirestoreDestination) Migrate(_ context.Context, _ int) error {
	return nil
}

// Health performs a lightweight liveness probe by listing at most one document
// from the memories collection and measuring round-trip latency.
// Does NOT modify any stored data.
func (d *FirestoreDestination) Health(ctx context.Context) (*destination.HealthStatus, error) {
	start := time.Now()
	iter := d.coll.Limit(1).Documents(ctx)
	defer iter.Stop()
	if _, err := iter.Next(); err != nil && err != iterator.Done {
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
func (d *FirestoreDestination) Close() error {
	return d.client.Close()
}

// Ping verifies the Firestore connection is alive. Satisfies DestinationWriter.
func (d *FirestoreDestination) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	iter := d.coll.Limit(1).Documents(ctx)
	defer iter.Stop()
	if _, err := iter.Next(); err != nil && err != iterator.Done {
		return fmt.Errorf("destination: firestore: ping: %w", err)
	}
	return nil
}

// Exists reports whether a document with payloadID exists in the collection.
func (d *FirestoreDestination) Exists(payloadID string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snap, err := d.coll.Doc(payloadID).Get(ctx)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("destination: firestore: exists %q: %w", payloadID, err)
	}
	return snap.Exists(), nil
}

// Query returns a page of memories using Firestore structured queries.
// Pagination uses skip/limit with a base64-encoded offset cursor.
// Content text search (params.Q) is applied client-side.
func (d *FirestoreDestination) Query(params destination.QueryParams) (destination.QueryResult, error) {
	limit := destination.ClampLimit(params.Limit)
	offset, err := destination.DecodeCursor(params.Cursor)
	if err != nil {
		return destination.QueryResult{}, fmt.Errorf("destination: firestore: query: %w", err)
	}

	q := d.coll.Query
	if params.Namespace != "" {
		q = q.Where("namespace", "==", params.Namespace)
	}
	if params.Destination != "" {
		q = q.Where("destination", "==", params.Destination)
	}
	if params.Subject != "" {
		q = q.Where("subject", "==", params.Subject)
	}
	if params.ActorType != "" {
		q = q.Where("actor_type", "==", params.ActorType)
	}
	if params.TierFilter {
		q = q.Where("tier", "<=", params.SourceTier)
	}

	// Firestore requires OrderBy when using inequality filters (tier <=).
	// We always order by timestamp DESC for consistency.
	if params.TierFilter {
		q = q.OrderBy("tier", gcfirestore.Asc)
	}
	q = q.OrderBy("timestamp", gcfirestore.Desc)

	// Fetch limit+1 to detect HasMore, plus enough to apply client-side Q filter.
	fetchLimit := limit + 1
	if params.Q != "" {
		// Over-fetch to allow for client-side filtering; cap at 5× limit.
		fetchLimit = limit*5 + 1
	}

	q = q.Offset(offset).Limit(fetchLimit)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	iter := q.Documents(ctx)
	defer iter.Stop()

	var records []destination.TranslatedPayload
	for {
		snap, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return destination.QueryResult{}, fmt.Errorf("destination: firestore: query: %w", err)
		}
		var doc memoryDoc
		if err := snap.DataTo(&doc); err != nil {
			return destination.QueryResult{}, fmt.Errorf("destination: firestore: query: decode: %w", err)
		}
		tp := payloadFromDoc(doc)
		if params.Q != "" && !strings.Contains(strings.ToLower(tp.Content), strings.ToLower(params.Q)) {
			continue
		}
		records = append(records, tp)
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

// docFromPayload converts a TranslatedPayload to a memoryDoc for Firestore.
// ClassificationTier defaults to "public" when empty.
// Embedding is converted from []float32 to []float64 for Firestore storage.
func docFromPayload(p destination.TranslatedPayload) memoryDoc {
	ct := p.ClassificationTier
	if ct == "" {
		ct = "public"
	}
	emb := float32SliceToFloat64(p.Embedding)
	return memoryDoc{
		PayloadID:          p.PayloadID,
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
		Embedding:          emb,
		SensitivityLabels:  p.SensitivityLabels,
		ClassificationTier: ct,
		Tier:               p.Tier,
		CreatedAt:          time.Now().UTC(),
	}
}

// payloadFromDoc converts a stored memoryDoc back to a TranslatedPayload.
func payloadFromDoc(d memoryDoc) destination.TranslatedPayload {
	return destination.TranslatedPayload{
		PayloadID:          d.PayloadID,
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
		Embedding:          float64SliceToFloat32(d.Embedding),
		SensitivityLabels:  d.SensitivityLabels,
		ClassificationTier: d.ClassificationTier,
		Tier:               d.Tier,
	}
}

// float32SliceToFloat64 converts []float32 to []float64 for Firestore storage.
// Returns nil for an empty slice.
func float32SliceToFloat64(v []float32) []float64 {
	if len(v) == 0 {
		return nil
	}
	out := make([]float64, len(v))
	for i, f := range v {
		out[i] = float64(f)
	}
	return out
}

// float64SliceToFloat32 converts []float64 from Firestore back to []float32.
// Returns nil for an empty slice.
func float64SliceToFloat32(v []float64) []float32 {
	if len(v) == 0 {
		return nil
	}
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = float32(f)
	}
	return out
}

// isNotFound reports whether err is a Firestore document-not-found error.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "NotFound") ||
		strings.Contains(err.Error(), "not found") ||
		strings.Contains(err.Error(), "does not exist")
}
