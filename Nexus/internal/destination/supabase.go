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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const supabaseDefaultTimeout = 15 * time.Second

// supabaseWriteRow is the JSON body for a POST /rest/v1/memories insert.
type supabaseWriteRow struct {
	PayloadID        string            `json:"payload_id"`
	RequestID        string            `json:"request_id"`
	Source           string            `json:"source"`
	Subject          string            `json:"subject"`
	Namespace        string            `json:"namespace"`
	Destination      string            `json:"destination"`
	Collection       string            `json:"collection"`
	Content          string            `json:"content"`
	Model            string            `json:"model"`
	Role             string            `json:"role"`
	Timestamp        string            `json:"timestamp"`
	IdempotencyKey   string            `json:"idempotency_key"`
	SchemaVersion    int               `json:"schema_version"`
	TransformVersion string            `json:"transform_version"`
	ActorType        string            `json:"actor_type"`
	ActorID          string            `json:"actor_id"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Embedding        []float32         `json:"embedding,omitempty"`
}

// supabaseMatchRequest is the JSON body for the match_memories RPC function.
type supabaseMatchRequest struct {
	QueryEmbedding []float32 `json:"query_embedding"`
	MatchCount     int       `json:"match_count"`
	Namespace      string    `json:"namespace,omitempty"`
	DestinationID  string    `json:"destination_id,omitempty"`
}

// supabaseMatchRow is one row returned by the match_memories RPC.
type supabaseMatchRow struct {
	PayloadID        string            `json:"payload_id"`
	RequestID        string            `json:"request_id"`
	Source           string            `json:"source"`
	Subject          string            `json:"subject"`
	Namespace        string            `json:"namespace"`
	Destination      string            `json:"destination"`
	Collection       string            `json:"collection"`
	Content          string            `json:"content"`
	Model            string            `json:"model"`
	Role             string            `json:"role"`
	Timestamp        string            `json:"timestamp"`
	IdempotencyKey   string            `json:"idempotency_key"`
	SchemaVersion    int               `json:"schema_version"`
	TransformVersion string            `json:"transform_version"`
	ActorType        string            `json:"actor_type"`
	ActorID          string            `json:"actor_id"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	Similarity       float32           `json:"similarity"`
}

// supabaseQueryRow is one row returned by GET /rest/v1/memories.
type supabaseQueryRow struct {
	PayloadID        string            `json:"payload_id"`
	RequestID        string            `json:"request_id"`
	Source           string            `json:"source"`
	Subject          string            `json:"subject"`
	Namespace        string            `json:"namespace"`
	Destination      string            `json:"destination"`
	Collection       string            `json:"collection"`
	Content          string            `json:"content"`
	Model            string            `json:"model"`
	Role             string            `json:"role"`
	Timestamp        string            `json:"timestamp"`
	IdempotencyKey   string            `json:"idempotency_key"`
	SchemaVersion    int               `json:"schema_version"`
	TransformVersion string            `json:"transform_version"`
	ActorType        string            `json:"actor_type"`
	ActorID          string            `json:"actor_id"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// SupabaseDestination writes and reads TranslatedPayload records via the
// Supabase REST API. Semantic search uses the match_memories RPC function which
// must be created in the Supabase database:
//
//	CREATE OR REPLACE FUNCTION match_memories(
//	    query_embedding vector(1536),
//	    match_count int,
//	    namespace text DEFAULT NULL,
//	    destination_id text DEFAULT NULL
//	) RETURNS TABLE ( ... ) ...
//
// The resolved API key is never logged at any level.
//
// Reference: Tech Spec Section 3.4 — Stage 4, Phase 5 Behavioral Contract 3.
type SupabaseDestination struct {
	httpClient  *http.Client
	baseURL     string
	// resolvedKey is the pre-resolved anon/service-role key. NEVER log.
	resolvedKey string
	logger      *slog.Logger
}

// OpenSupabase creates a SupabaseDestination. baseURL is the Supabase project
// URL (e.g. "https://xyzproject.supabase.co"). resolvedKey is the pre-resolved
// service-role or anon key — never re-resolved per request.
//
// INVARIANT: resolvedKey is never logged at any level.
func OpenSupabase(baseURL, resolvedKey string, logger *slog.Logger) (*SupabaseDestination, error) {
	if logger == nil {
		panic("destination: supabase logger must not be nil")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("destination: supabase: baseURL must not be empty")
	}

	d := &SupabaseDestination{
		httpClient:  &http.Client{Timeout: supabaseDefaultTimeout},
		baseURL:     baseURL,
		resolvedKey: resolvedKey,
		logger:      logger,
	}

	logger.Info("destination: supabase opened",
		"component", "destination",
		"url", baseURL,
	)
	return d, nil
}

// Write inserts p into the Supabase memories table via POST /rest/v1/memories.
// Uses Prefer: resolution=ignore-duplicates for idempotent behaviour.
// INVARIANT: resolvedKey is never logged.
func (d *SupabaseDestination) Write(p TranslatedPayload) error {
	row := supabaseWriteRow{
		PayloadID:        p.PayloadID,
		RequestID:        p.RequestID,
		Source:           p.Source,
		Subject:          p.Subject,
		Namespace:        p.Namespace,
		Destination:      p.Destination,
		Collection:       p.Collection,
		Content:          p.Content,
		Model:            p.Model,
		Role:             p.Role,
		Timestamp:        p.Timestamp.UTC().Format(time.RFC3339Nano),
		IdempotencyKey:   p.IdempotencyKey,
		SchemaVersion:    p.SchemaVersion,
		TransformVersion: p.TransformVersion,
		ActorType:        p.ActorType,
		ActorID:          p.ActorID,
		Metadata:         p.Metadata,
		Embedding:        p.Embedding,
	}

	body, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("destination: supabase: marshal write row: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, d.baseURL+"/rest/v1/memories", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("destination: supabase: create write request: %w", err)
	}
	d.setHeaders(req)
	req.Header.Set("Prefer", "resolution=ignore-duplicates")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("destination: supabase: write: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Default().Debug("close body", "err", err)
		}
	}()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("destination: supabase: write: HTTP %d for payload_id %q",
			resp.StatusCode, p.PayloadID)
	}

	d.logger.Debug("destination: supabase: write",
		"component", "destination",
		"payload_id", p.PayloadID,
	)
	return nil
}

// Ping checks connectivity by sending a HEAD request to /rest/v1/memories.
func (d *SupabaseDestination) Ping() error {
	req, err := http.NewRequest(http.MethodHead, d.baseURL+"/rest/v1/memories", nil)
	if err != nil {
		return fmt.Errorf("destination: supabase: ping: create request: %w", err)
	}
	d.setHeaders(req)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("destination: supabase: ping: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Default().Debug("close body", "err", err)
		}
	}()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))

	if resp.StatusCode >= 500 {
		return fmt.Errorf("destination: supabase: ping: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Exists reports whether a record with payloadID exists.
func (d *SupabaseDestination) Exists(payloadID string) (bool, error) {
	u, err := url.Parse(d.baseURL + "/rest/v1/memories")
	if err != nil {
		return false, fmt.Errorf("destination: supabase: exists: parse URL: %w", err)
	}
	q := u.Query()
	q.Set("payload_id", "eq."+payloadID)
	q.Set("select", "payload_id")
	q.Set("limit", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return false, fmt.Errorf("destination: supabase: exists: create request: %w", err)
	}
	d.setHeaders(req)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("destination: supabase: exists: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Default().Debug("close body", "err", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
		return false, fmt.Errorf("destination: supabase: exists: HTTP %d", resp.StatusCode)
	}

	var rows []struct {
		PayloadID string `json:"payload_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return false, fmt.Errorf("destination: supabase: exists: decode: %w", err)
	}
	return len(rows) > 0, nil
}

// Query returns a page of memories using the Supabase REST API filter syntax.
// All filter values are passed as URL query parameters — no string interpolation
// in SQL.
func (d *SupabaseDestination) Query(params QueryParams) (QueryResult, error) {
	limit := ClampLimit(params.Limit)
	offset, err := DecodeCursor(params.Cursor)
	if err != nil {
		return QueryResult{}, fmt.Errorf("destination: supabase: query: %w", err)
	}

	u, err := url.Parse(d.baseURL + "/rest/v1/memories")
	if err != nil {
		return QueryResult{}, fmt.Errorf("destination: supabase: query: parse URL: %w", err)
	}
	q := u.Query()
	q.Set("select", "payload_id,request_id,source,subject,namespace,destination,collection,content,model,role,timestamp,idempotency_key,schema_version,transform_version,actor_type,actor_id,metadata")
	q.Set("order", "timestamp.desc")
	q.Set("limit", fmt.Sprintf("%d", limit+1)) // +1 for hasMore detection
	q.Set("offset", fmt.Sprintf("%d", offset))

	if params.Namespace != "" {
		q.Set("namespace", "eq."+params.Namespace)
	}
	if params.Destination != "" {
		q.Set("destination", "eq."+params.Destination)
	}
	if params.Subject != "" {
		q.Set("subject", "eq."+params.Subject)
	}
	if params.Q != "" {
		q.Set("content", "ilike.*"+params.Q+"*")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return QueryResult{}, fmt.Errorf("destination: supabase: query: create request: %w", err)
	}
	d.setHeaders(req)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return QueryResult{}, fmt.Errorf("destination: supabase: query: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Default().Debug("close body", "err", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
		return QueryResult{}, fmt.Errorf("destination: supabase: query: HTTP %d", resp.StatusCode)
	}

	var rows []supabaseQueryRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return QueryResult{}, fmt.Errorf("destination: supabase: query: decode: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	records := make([]TranslatedPayload, 0, len(rows))
	for _, row := range rows {
		tp := supabaseRowToPayload(row)
		records = append(records, tp)
	}

	var nextCursor string
	if hasMore {
		nextCursor = EncodeCursor(offset + limit)
	}
	return QueryResult{Records: records, NextCursor: nextCursor, HasMore: hasMore}, nil
}

// CanSemanticSearch reports whether the match_memories RPC is available.
// We always return true for Supabase — if the function doesn't exist, the RPC
// call returns an HTTP error which SemanticSearch will propagate.
func (d *SupabaseDestination) CanSemanticSearch() bool { return true }

// SemanticSearch calls the match_memories PostgreSQL function via the Supabase
// RPC endpoint. The function must exist in the Supabase database.
//
// Reference: Tech Spec Section 3.4 — Stage 4.
func (d *SupabaseDestination) SemanticSearch(ctx context.Context, vec []float32, params QueryParams) ([]ScoredRecord, error) {
	limit := ClampLimit(params.Limit)

	matchReq := supabaseMatchRequest{
		QueryEmbedding: vec,
		MatchCount:     limit,
		Namespace:      params.Namespace,
		DestinationID:  params.Destination,
	}
	body, err := json.Marshal(matchReq)
	if err != nil {
		return nil, fmt.Errorf("destination: supabase: semantic search: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.baseURL+"/rest/v1/rpc/match_memories", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("destination: supabase: semantic search: create request: %w", err)
	}
	d.setHeaders(req)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("destination: supabase: semantic search: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Default().Debug("close body", "err", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("destination: supabase: semantic search: HTTP %d", resp.StatusCode)
	}

	var matchRows []supabaseMatchRow
	if err := json.NewDecoder(resp.Body).Decode(&matchRows); err != nil {
		return nil, fmt.Errorf("destination: supabase: semantic search: decode: %w", err)
	}

	scored := make([]ScoredRecord, 0, len(matchRows))
	for _, row := range matchRows {
		tp := TranslatedPayload{
			PayloadID:        row.PayloadID,
			RequestID:        row.RequestID,
			Source:           row.Source,
			Subject:          row.Subject,
			Namespace:        row.Namespace,
			Destination:      row.Destination,
			Collection:       row.Collection,
			Content:          row.Content,
			Model:            row.Model,
			Role:             row.Role,
			IdempotencyKey:   row.IdempotencyKey,
			SchemaVersion:    row.SchemaVersion,
			TransformVersion: row.TransformVersion,
			ActorType:        row.ActorType,
			ActorID:          row.ActorID,
			Metadata:         row.Metadata,
		}
		if t, parseErr := parseTimestamp(row.Timestamp); parseErr == nil {
			tp.Timestamp = t
		}
		scored = append(scored, ScoredRecord{Payload: tp, Score: row.Similarity})
	}
	return scored, nil
}

// Close is a no-op for the Supabase destination (http.Client has no close).
func (d *SupabaseDestination) Close() error { return nil }

// setHeaders adds the Authorization and Content-Type headers required by the
// Supabase REST API.
// INVARIANT: resolvedKey is never logged at any level.
func (d *SupabaseDestination) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", d.resolvedKey)
	req.Header.Set("Authorization", "Bearer "+d.resolvedKey)
}

// supabaseRowToPayload converts a supabaseQueryRow to TranslatedPayload.
func supabaseRowToPayload(row supabaseQueryRow) TranslatedPayload {
	tp := TranslatedPayload{
		PayloadID:        row.PayloadID,
		RequestID:        row.RequestID,
		Source:           row.Source,
		Subject:          row.Subject,
		Namespace:        row.Namespace,
		Destination:      row.Destination,
		Collection:       row.Collection,
		Content:          row.Content,
		Model:            row.Model,
		Role:             row.Role,
		IdempotencyKey:   row.IdempotencyKey,
		SchemaVersion:    row.SchemaVersion,
		TransformVersion: row.TransformVersion,
		ActorType:        row.ActorType,
		ActorID:          row.ActorID,
		Metadata:         row.Metadata,
	}
	if t, err := parseTimestamp(row.Timestamp); err == nil {
		tp.Timestamp = t
	}
	return tp
}
