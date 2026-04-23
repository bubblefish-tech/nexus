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
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// Compile-time proof that SupabaseDestination satisfies the Destination interface.
var _ Destination = (*SupabaseDestination)(nil)

// Name returns the stable identifier for this destination.
func (d *SupabaseDestination) Name() string { return "supabase" }

// Read retrieves a single memory record by its PayloadID via
// GET /rest/v1/memories?payload_id=eq.{id}&limit=1.
// Returns nil, nil when the record does not exist.
func (d *SupabaseDestination) Read(ctx context.Context, id string) (*Memory, error) {
	u, err := url.Parse(d.baseURL + "/rest/v1/memories")
	if err != nil {
		return nil, fmt.Errorf("destination: supabase: read: parse URL: %w", err)
	}
	q := u.Query()
	q.Set("payload_id", "eq."+id)
	q.Set("select", "payload_id,request_id,source,subject,namespace,destination,collection,content,model,role,timestamp,idempotency_key,schema_version,transform_version,actor_type,actor_id,metadata")
	q.Set("limit", "1")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("destination: supabase: read: create request: %w", err)
	}
	d.setHeaders(req)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("destination: supabase: read: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Default().Debug("close body", "err", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("destination: supabase: read: HTTP %d for payload_id %q",
			resp.StatusCode, id)
	}

	var rows []supabaseQueryRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("destination: supabase: read: decode: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	tp := supabaseRowToPayload(rows[0])
	return &tp, nil
}

// Search returns memories matching query, converting the result to a slice of
// pointers. Returns an empty (non-nil) slice when no records match.
func (d *SupabaseDestination) Search(ctx context.Context, query *Query) ([]*Memory, error) {
	_ = ctx
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

// Delete removes the record with the given PayloadID via
// DELETE /rest/v1/memories?payload_id=eq.{id}.
// Deletion of a non-existent ID is a no-op (idempotent).
func (d *SupabaseDestination) Delete(ctx context.Context, id string) error {
	u, err := url.Parse(d.baseURL + "/rest/v1/memories")
	if err != nil {
		return fmt.Errorf("destination: supabase: delete: parse URL: %w", err)
	}
	q := u.Query()
	q.Set("payload_id", "eq."+id)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return fmt.Errorf("destination: supabase: delete: create request: %w", err)
	}
	d.setHeaders(req)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("destination: supabase: delete: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Default().Debug("close body", "err", err)
		}
	}()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("destination: supabase: delete: HTTP %d for payload_id %q",
			resp.StatusCode, id)
	}
	return nil
}

// VectorSearch returns up to limit memories ranked by cosine similarity to
// embedding. Delegates to SemanticSearch with an empty QueryParams so no
// namespace/destination filter is applied.
func (d *SupabaseDestination) VectorSearch(ctx context.Context, embedding []float32, limit int) ([]*Memory, error) {
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

// Migrate is a no-op for Supabase: schema is managed via the Supabase dashboard
// or migration scripts applied externally. The version argument is reserved for
// future use when explicit versioned migrations are required.
func (d *SupabaseDestination) Migrate(_ context.Context, _ int) error {
	return nil
}

// Health performs a lightweight liveness probe by sending a HEAD request to
// /rest/v1/memories and measuring round-trip latency. Does NOT modify any
// stored data. HTTP 5xx is treated as unhealthy; 2xx/4xx as healthy (the API
// key may be restricted but the service is reachable).
func (d *SupabaseDestination) Health(ctx context.Context) (*HealthStatus, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, d.baseURL+"/rest/v1/memories", nil)
	if err != nil {
		return &HealthStatus{OK: false, Error: err.Error()}, nil
	}
	d.setHeaders(req)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return &HealthStatus{OK: false, Error: err.Error()}, nil
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			slog.Default().Debug("close body", "err", err)
		}
	}()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 256))

	if resp.StatusCode >= 500 {
		return &HealthStatus{
			OK:    false,
			Error: fmt.Sprintf("HTTP %d", resp.StatusCode),
		}, nil
	}
	return &HealthStatus{
		OK:      true,
		Latency: time.Since(start),
	}, nil
}
