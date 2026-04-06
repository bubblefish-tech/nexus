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

package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tidwall/gjson"

	"github.com/BubbleFish-Nexus/internal/destination"
	"github.com/BubbleFish-Nexus/internal/query"
	"github.com/BubbleFish-Nexus/internal/version"
	"github.com/BubbleFish-Nexus/internal/wal"
)

// errorResponse is the canonical error envelope.
// Reference: Tech Spec Section 7.4, Phase 0C Behavioral Contract item 14.
type errorResponse struct {
	Error              string                 `json:"error"`
	Message            string                 `json:"message"`
	RetryAfterSeconds  int                    `json:"retry_after_seconds,omitempty"`
	Details            map[string]interface{} `json:"details"`
}

// writeResponse is the success response for the write handler.
type writeResponse struct {
	PayloadID string `json:"payload_id"`
	Status    string `json:"status"`
}

// queryResponse is the success response for the read handler.
type queryResponse struct {
	Results []destination.TranslatedPayload `json:"results"`
	Nexus   nexusMetadata                   `json:"_nexus"`
}

// openAIMessage is a single entry in the OpenAI chat messages array.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIMemoriesRequest is the request body for POST /v1/memories.
type openAIMemoriesRequest struct {
	Messages   []openAIMessage `json:"messages"`
	Subject    string          `json:"subject,omitempty"`
	Collection string          `json:"collection,omitempty"`
}

// openAIMemoriesResponse is the success response for POST /v1/memories.
type openAIMemoriesResponse struct {
	PayloadIDs []string `json:"payload_ids"`
	Status     string   `json:"status"`
}

// nexusMetadata is the _nexus metadata block returned on every query response.
// Reference: Tech Spec Section 3.4, Phase 5 Behavioral Contract 4, Section 3.7.
type nexusMetadata struct {
	ResultCount              int    `json:"result_count"`
	HasMore                  bool   `json:"has_more"`
	NextCursor               string `json:"next_cursor,omitempty"`
	Profile                  string `json:"profile"`
	Stage                    string `json:"stage"`
	RetrievalStage           int    `json:"retrieval_stage"`
	SemanticUnavailable      bool   `json:"semantic_unavailable,omitempty"`
	SemanticUnavailableReason string `json:"semantic_unavailable_reason,omitempty"`
}

// healthResponse is returned by /health and /ready.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// ---------------------------------------------------------------------------
// Rate Limiter
// ---------------------------------------------------------------------------

// rateLimiter implements a per-source fixed-window rate limiter.
// Each source gets a separate window that resets every minute.
// All state is in struct fields — no package-level variables.
type rateLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
}

type rateWindow struct {
	count       int
	windowStart time.Time
	rpm         int
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		windows: make(map[string]*rateWindow),
	}
}

// Allow returns true if the request for sourceName is within the rpm budget.
// It also returns the number of seconds until the current window resets,
// which is used in the Retry-After header on rejection.
func (rl *rateLimiter) Allow(sourceName string, rpm int) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	w, ok := rl.windows[sourceName]
	if !ok {
		rl.windows[sourceName] = &rateWindow{
			count:       1,
			windowStart: now,
			rpm:         rpm,
		}
		return true, 0
	}

	// If the window has expired, reset it.
	if now.Sub(w.windowStart) >= time.Minute {
		w.count = 1
		w.windowStart = now
		w.rpm = rpm
		return true, 0
	}

	if w.count >= w.rpm {
		remaining := time.Until(w.windowStart.Add(time.Minute))
		retryAfter := int(remaining.Seconds()) + 1
		return false, retryAfter
	}

	w.count++
	return true, 0
}

// ---------------------------------------------------------------------------
// ID generation
// ---------------------------------------------------------------------------

// newID generates a random 16-byte hex-encoded identifier.
// Used for both payload_id and request_id. No external dependencies.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is catastrophic — panic so the operator knows.
		panic(fmt.Sprintf("daemon: crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// Write Handler — POST /inbound/{source}
// ---------------------------------------------------------------------------

// handleWrite implements the inbound write path. The operation order is
// exact and must not be reordered.
//
// Exact operation order (reference: Tech Spec Section 3.2):
//  1. Auth (done in middleware) → CanWrite check
//  2. Subject namespace resolved
//  3. Policy gate
//  4. MaxBytesReader applied (BEFORE reading body)
//  5. Idempotency check (BEFORE rate limiting)
//  6. Rate limit check
//  7. Field mapping via gjson dot-path
//  8. Transforms applied
//  9. Build TranslatedPayload
// 10. WAL append
// 11. Idempotency key registered
// 12. Queue enqueue
// 13. Return 200 + payload_id
func (d *Daemon) handleWrite(w http.ResponseWriter, r *http.Request) {
	writeStart := time.Now()
	src := sourceFromContext(r.Context())
	if src == nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"source context missing", 0)
		return
	}

	// Step 1 — CanWrite check.
	// Reference: Tech Spec Section 6.1, Phase 0C Behavioral Contract item 12.
	if !src.CanWrite {
		d.writeErrorResponse(w, r, http.StatusForbidden, "source_not_permitted_to_write",
			"this source does not have write permission", 0)
		return
	}

	// Verify the path parameter matches the authenticated source name.
	// This prevents a source from writing as a different source.
	pathSource := chi.URLParam(r, "source")
	if pathSource != src.Name {
		d.writeErrorResponse(w, r, http.StatusForbidden, "source_mismatch",
			"path source does not match authenticated source", 0)
		return
	}

	// Step 2 — Resolve subject namespace.
	// X-Subject header overrides the source namespace default.
	subject := r.Header.Get("X-Subject")
	if subject == "" {
		subject = src.Namespace
	}

	// Step 3 — Policy gate (basic for Phase 0C).
	// Full policy engine is in Phase 1.
	dest := src.TargetDest
	if len(src.Policy.AllowedDestinations) > 0 && !containsString(src.Policy.AllowedDestinations, dest) {
		d.writeErrorResponse(w, r, http.StatusForbidden, "policy_denied",
			"destination not permitted for this source", 0)
		return
	}
	if len(src.Policy.AllowedOperations) > 0 && !containsString(src.Policy.AllowedOperations, "write") {
		d.writeErrorResponse(w, r, http.StatusForbidden, "policy_denied",
			"write operation not permitted for this source", 0)
		return
	}

	// Step 4 — Apply MaxBytesReader BEFORE reading any body bytes.
	// Reference: Phase 0C Behavioral Contract item 10, Invariant 5.
	maxBytes := src.PayloadLimits.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024 // 10 MiB default
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	// Validate Content-Type on POST.
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		d.writeErrorResponse(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/json", 0)
		return
	}

	// Read body — MaxBytesReader is already applied.
	var rawBody json.RawMessage
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&rawBody); err != nil {
		// Check if this is a MaxBytesReader error (payload too large).
		if strings.Contains(err.Error(), "request body too large") ||
			strings.Contains(err.Error(), "http: request body too large") {
			d.writeErrorResponse(w, r, http.StatusRequestEntityTooLarge, "payload_too_large",
				"request body exceeds maximum allowed size", 0)
			return
		}
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_json",
			"request body must be valid JSON", 0)
		return
	}
	bodyStr := string(rawBody)

	// Step 5 — Idempotency check BEFORE rate limiting.
	// Reference: Phase 0C Behavioral Contract item 9, Invariant 4.
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		idempotencyKey = r.Header.Get("X-Idempotency-Key")
	}

	if src.Idempotency.Enabled && idempotencyKey != "" {
		if existingPayloadID, seen := d.idem.Seen(idempotencyKey); seen {
			d.logger.Info("daemon: duplicate write — returning original payload_id",
				"component", "daemon",
				"source", src.Name,
				"idempotency_key_prefix", safePrefix(idempotencyKey, 8),
				"payload_id", existingPayloadID,
				"request_id", middleware.GetReqID(r.Context()),
			)
			d.writeJSON(w, http.StatusOK, writeResponse{
				PayloadID: existingPayloadID,
				Status:    "accepted",
			})
			return
		}
	}

	// Step 6 — Rate limit check AFTER idempotency.
	// Reference: Phase 0C Behavioral Contract item 11, Invariant 4.
	cfg := d.getConfig()
	rpm := src.RateLimit.RequestsPerMinute
	if rpm <= 0 {
		rpm = cfg.Daemon.RateLimit.GlobalRequestsPerMinute
	}
	if allowed, retryAfter := d.rl.Allow(src.Name, rpm); !allowed {
		d.logger.Warn("daemon: rate limit exceeded",
			"component", "daemon",
			"source", src.Name,
			"rpm", rpm,
			"request_id", middleware.GetReqID(r.Context()),
		)
		d.metrics.RateLimitHitsTotal.WithLabelValues(src.Name).Inc()
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		d.writeErrorResponse(w, r, http.StatusTooManyRequests, "rate_limit_exceeded",
			"rate limit exceeded; back off and retry", retryAfter)
		return
	}

	// Step 7 — Field mapping via gjson dot-path.
	// Each entry in src.Mapping is "output_field" → "gjson_path".
	// Unmapped top-level JSON keys go into metadata.
	// Reference: Tech Spec Section 3.2 Step 9.
	mapped := make(map[string]string, len(src.Mapping))
	usedTopLevelKeys := make(map[string]bool)
	for outField, gPath := range src.Mapping {
		val := gjson.Get(bodyStr, gPath)
		if val.Exists() {
			mapped[outField] = val.String()
		}
		// Track top-level key of the gjson path (everything before the first dot).
		topKey := gPath
		if idx := strings.Index(gPath, "."); idx >= 0 {
			topKey = gPath[:idx]
		}
		usedTopLevelKeys[topKey] = true
	}

	// Collect unmapped top-level keys into Metadata.
	metadata := make(map[string]string)
	gjson.Parse(bodyStr).ForEach(func(key, val gjson.Result) bool {
		k := key.String()
		if !usedTopLevelKeys[k] {
			metadata[k] = val.String()
		}
		return true
	})

	// Step 8 — Apply transforms.
	// Supported: "trim", "coalesce:<default>".
	for field, transforms := range src.Transform {
		val := mapped[field]
		for _, t := range transforms {
			val = applyTransform(val, t)
		}
		mapped[field] = val
	}

	// Step 9 — Build TranslatedPayload.
	// Reference: Tech Spec Section 7.1.
	requestID := middleware.GetReqID(r.Context())
	if requestID == "" {
		requestID = newID()
	}
	payloadID := newID()

	// Actor type/ID: X-Actor-Type/X-Actor-ID headers override source defaults.
	// Reference: Tech Spec Section 7.1 — Provenance Semantics.
	actorType := r.Header.Get("X-Actor-Type")
	if actorType == "" {
		actorType = src.DefaultActorType
	}
	if !destination.ValidActorType(actorType) {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_actor_type",
			"actor_type must be one of: user, agent, system", 0)
		return
	}
	actorID := r.Header.Get("X-Actor-ID")
	if actorID == "" {
		actorID = src.DefaultActorID
	}

	tp := destination.TranslatedPayload{
		PayloadID:        payloadID,
		RequestID:        requestID,
		Source:           src.Name,
		Subject:          subject,
		Namespace:        src.Namespace,
		Destination:      dest,
		Collection:       mapped["collection"],
		Content:          mapped["content"],
		Model:            mapped["model"],
		Role:             mapped["role"],
		Timestamp:        time.Now().UTC(),
		IdempotencyKey:   idempotencyKey,
		SchemaVersion:    1,
		TransformVersion: "1.0",
		ActorType:        actorType,
		ActorID:          actorID,
		Metadata:         metadata,
	}

	// Build WAL entry payload.
	payloadBytes, err := json.Marshal(tp)
	if err != nil {
		d.logger.Error("daemon: marshal payload",
			"component", "daemon",
			"source", src.Name,
			"error", err,
			"request_id", requestID,
		)
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"failed to encode payload", 0)
		return
	}

	// Step 10 — WAL append. If WAL fails, return 500 — do NOT proceed.
	// Reference: Tech Spec Section 4, Behavioral Contract item 8.
	entry := wal.Entry{
		PayloadID:      payloadID,
		IdempotencyKey: idempotencyKey,
		Source:         src.Name,
		Destination:    dest,
		Subject:        subject,
		ActorType:      actorType,
		ActorID:        actorID,
		Payload:        payloadBytes,
	}
	walStart := time.Now()
	if err := d.wal.Append(entry); err != nil {
		d.logger.Error("daemon: WAL append failed",
			"component", "daemon",
			"source", src.Name,
			"payload_id", payloadID,
			"error", err,
			"request_id", requestID,
		)
		d.metrics.ErrorsTotal.WithLabelValues("wal_append").Inc()
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"durable write failed; operator: check disk", 0)
		return
	}
	d.metrics.WALAppendLatency.Observe(time.Since(walStart).Seconds())

	// Step 11 — Register idempotency key AFTER WAL.
	// Reference: Tech Spec Section 3.2 Step 15.
	if src.Idempotency.Enabled && idempotencyKey != "" {
		d.idem.Register(idempotencyKey, payloadID)
	}

	// Step 12 — Non-blocking enqueue.
	// If queue is full (load shed), return 429 queue_full.
	// The WAL entry is already durable — data is safe.
	// Reference: Tech Spec Section 3.2 Step 16.
	if err := d.queue.Enqueue(entry); err != nil {
		d.logger.Warn("daemon: queue full — load shedding",
			"component", "daemon",
			"source", src.Name,
			"payload_id", payloadID,
			"request_id", requestID,
		)
		w.Header().Set("Retry-After", "5")
		d.writeErrorResponse(w, r, http.StatusTooManyRequests, "queue_full",
			"queue full; data is durable in WAL and will be replayed on restart", 5)
		return
	}

	d.logger.Info("daemon: write accepted",
		"component", "daemon",
		"source", src.Name,
		"payload_id", payloadID,
		"subject", subject,
		"destination", dest,
		"request_id", requestID,
	)

	// Record write path metrics.
	d.metrics.ThroughputPerSource.WithLabelValues(src.Name).Inc()
	d.metrics.PayloadProcessingLatency.WithLabelValues(src.Name).Observe(time.Since(writeStart).Seconds())
	d.metrics.QueueDepth.Set(float64(d.queue.Len()))

	// Step 13 — Return 200 + payload_id.
	d.writeJSON(w, http.StatusOK, writeResponse{
		PayloadID: payloadID,
		Status:    "accepted",
	})
}

// ---------------------------------------------------------------------------
// Query Handler — GET /query/{destination}
// ---------------------------------------------------------------------------

// handleQuery implements the read path via the 6-stage retrieval cascade.
// Stage 0 (policy gate) and Stage 3 (structured lookup) are fully operational.
// Stages 1, 2, 4, and 5 are stub pass-throughs pending later phases.
//
// Reference: Tech Spec Section 3.4, Phase 0C Behavioral Contract items 11, 16.
func (d *Daemon) handleQuery(w http.ResponseWriter, r *http.Request) {
	queryStart := time.Now()
	src := sourceFromContext(r.Context())
	if src == nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"source context missing", 0)
		return
	}

	// Pre-cascade CanRead guard — checked before rate limiting so that
	// unauthorised sources do not consume rate-limit budget.
	// Reference: Phase 0C Behavioral Contract item 12.
	if !src.CanRead {
		d.writeErrorResponse(w, r, http.StatusForbidden, "source_not_permitted_to_read",
			"this source does not have read permission", 0)
		return
	}

	// Rate limit — applies to reads too.
	// Reference: Phase 0C Behavioral Contract item 11.
	qcfg := d.getConfig()
	rpm := src.RateLimit.RequestsPerMinute
	if rpm <= 0 {
		rpm = qcfg.Daemon.RateLimit.GlobalRequestsPerMinute
	}
	if allowed, retryAfter := d.rl.Allow(src.Name+":read", rpm); !allowed {
		d.metrics.RateLimitHitsTotal.WithLabelValues(src.Name).Inc()
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		d.writeErrorResponse(w, r, http.StatusTooManyRequests, "rate_limit_exceeded",
			"rate limit exceeded; back off and retry", retryAfter)
		return
	}

	// Resolve subject.
	subject := r.URL.Query().Get("subject")
	if subject == "" {
		subject = r.Header.Get("X-Subject")
	}

	destName := chi.URLParam(r, "destination")

	// Parse limit (Normalize will clamp it).
	// Reference: Phase 0C Behavioral Contract item 16.
	limitStr := r.URL.Query().Get("limit")
	limit := 0
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil {
			limit = n
		}
	}

	profile := r.URL.Query().Get("profile")
	if profile == "" {
		profile = src.DefaultProfile
	}
	if profile == "" {
		profile = qcfg.Retrieval.DefaultProfile
	}

	// Parse actor_type provenance filter.
	// Reference: Tech Spec Section 7.1 — actor_type query filter.
	actorTypeFilter := r.URL.Query().Get("actor_type")
	if actorTypeFilter != "" && !destination.ValidActorType(actorTypeFilter) {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_actor_type",
			"actor_type must be one of: user, agent, system", 0)
		return
	}

	// Normalize query params into a CanonicalQuery. Invalid cursors → 400.
	cq, err := query.Normalize(destination.QueryParams{
		Destination: destName,
		Namespace:   src.Namespace,
		Subject:     subject,
		Q:           r.URL.Query().Get("q"),
		Limit:       limit,
		Cursor:      r.URL.Query().Get("cursor"),
		Profile:     profile,
		ActorType:   actorTypeFilter,
	})
	if err != nil {
		// Distinguish profile validation errors from cursor decode errors.
		if !query.ValidProfile(profile) {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_profile",
				"profile must be one of: fast, balanced, deep", 0)
		} else {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_cursor",
				"cursor is not a valid opaque pagination token", 0)
		}
		return
	}

	// Execute the 6-stage retrieval cascade.
	// Reference: Tech Spec Section 3.4.
	runner := query.New(d.querier, d.logger).
		WithExactCache(d.exactCache).
		WithSemanticCache(d.semanticCache).
		WithEmbeddingClient(d.embeddingClient, d.metrics.EmbeddingLatency).
		WithRetrievalConfig(qcfg.Retrieval).
		WithDecayCounter(d.metrics.TemporalDecayApplied)
	cascResult, err := runner.Run(r.Context(), src, cq)
	if err != nil {
		d.logger.Error("daemon: cascade failed",
			"component", "daemon",
			"source", src.Name,
			"destination", destName,
			"error", err,
			"request_id", middleware.GetReqID(r.Context()),
		)
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"query execution failed", 0)
		return
	}

	// Stage 0 denial → 403.
	if cascResult.Denial != nil {
		d.writeErrorResponse(w, r, http.StatusForbidden, cascResult.Denial.Code,
			cascResult.Denial.Reason, 0)
		return
	}

	d.metrics.ReadLatency.WithLabelValues(src.Name, "/query").Observe(time.Since(queryStart).Seconds())

	meta := nexusMetadata{
		ResultCount:               len(cascResult.Records),
		HasMore:                   cascResult.HasMore,
		NextCursor:                cascResult.NextCursor,
		Profile:                   cascResult.Profile,
		Stage:                     query.StageName(cascResult.RetrievalStage),
		RetrievalStage:            cascResult.RetrievalStage,
		SemanticUnavailable:       cascResult.SemanticUnavailable,
		SemanticUnavailableReason: cascResult.SemanticUnavailableReason,
	}

	// SSE streaming — when client sends Accept: text/event-stream.
	// Reference: Tech Spec Section 12, Phase 7 Behavioral Contract 8.
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		d.streamQuerySSE(w, cascResult.Records, meta)
		return
	}

	d.writeJSON(w, http.StatusOK, queryResponse{
		Results: cascResult.Records,
		Nexus:   meta,
	})
}

// ---------------------------------------------------------------------------
// Health / Ready Handlers
// ---------------------------------------------------------------------------

// handleHealth is the liveness probe. Always returns 200 while the process
// is alive. No authentication required.
// Reference: Tech Spec Section 11.4, Phase 0C Behavioral Contract item 13.
func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	d.writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		Version: version.Version,
	})
}

// handleReady is the readiness probe. Returns 200 when both the WAL and
// destination are healthy, 503 otherwise. No authentication required.
// Reference: Tech Spec Section 4.4, Section 11.4.
func (d *Daemon) handleReady(w http.ResponseWriter, r *http.Request) {
	// WAL health check — set by watchdog goroutine.
	if d.walHealthy.Load() == 0 {
		d.logger.Warn("daemon: readiness probe: WAL unhealthy",
			"component", "daemon",
		)
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "wal_unhealthy",
			"WAL directory is not writable or disk space is below threshold", 0)
		return
	}

	if err := d.dest.Ping(); err != nil {
		d.logger.Warn("daemon: readiness probe: destination unhealthy",
			"component", "daemon",
			"error", err,
		)
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "destination_unavailable",
			"destination is not reachable", 0)
		return
	}
	d.writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ready",
		Version: version.Version,
	})
}

// ---------------------------------------------------------------------------
// Admin Handlers (minimal for Phase 0C)
// ---------------------------------------------------------------------------

// handleAdminStatus returns a status response including queue and WAL state.
func (d *Daemon) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/status").Inc()

	queueDepth := 0
	if d.queue != nil {
		queueDepth = d.queue.Len()
	}

	d.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "running",
		"version":     version.Version,
		"queue_depth": queueDepth,
	})
}

// ---------------------------------------------------------------------------
// SSE streaming for GET /query/{destination}
// ---------------------------------------------------------------------------

// streamQuerySSE writes cascade results as a Server-Sent Events stream.
// Each record is sent as `data: <json>\n\n`. A final event carries the
// `_nexus` metadata envelope. The response ends when all records are sent.
//
// If the ResponseWriter does not support http.Flusher (e.g. in tests using
// httptest.ResponseRecorder), this method falls back to regular JSON.
//
// Reference: Tech Spec Section 12, Phase 7 Behavioral Contract 8.
func (d *Daemon) streamQuerySSE(w http.ResponseWriter, records []destination.TranslatedPayload, meta nexusMetadata) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fall back to regular JSON when the writer cannot flush.
		d.writeJSON(w, http.StatusOK, queryResponse{Results: records, Nexus: meta})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx proxy buffering
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)

	// Stream each result record.
	for i := range records {
		if _, err := fmt.Fprintf(w, "data: "); err != nil {
			return
		}
		if err := enc.Encode(records[i]); err != nil {
			return
		}
		// enc.Encode appends \n; SSE requires \n\n between events.
		if _, err := fmt.Fprintf(w, "\n"); err != nil {
			return
		}
		flusher.Flush()
	}

	// Final event: _nexus metadata envelope.
	metaEvent := map[string]nexusMetadata{"_nexus": meta}
	if _, err := fmt.Fprintf(w, "data: "); err != nil {
		return
	}
	if err := enc.Encode(metaEvent); err != nil {
		return
	}
	if _, err := fmt.Fprintf(w, "\n"); err != nil {
		return
	}
	flusher.Flush()
}

// ---------------------------------------------------------------------------
// OpenAI-compatible write — POST /v1/memories
// ---------------------------------------------------------------------------

// handleOpenAIWrite implements the OpenAI-compatible memory write endpoint.
// It accepts a messages array in OpenAI chat format and writes each message
// through the same WAL → queue → destination pipeline as handleWrite.
//
// Request body:
//
//	{"messages":[{"role":"user","content":"..."}],"subject":"...","collection":"..."}
//
// Response:
//
//	{"payload_ids":["...","..."],"status":"accepted"}
//
// Reference: Tech Spec Section 12, Phase 7 Behavioral Contract 7.
func (d *Daemon) handleOpenAIWrite(w http.ResponseWriter, r *http.Request) {
	writeStart := time.Now()
	src := sourceFromContext(r.Context())
	if src == nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"source context missing", 0)
		return
	}

	if !src.CanWrite {
		d.writeErrorResponse(w, r, http.StatusForbidden, "source_not_permitted_to_write",
			"this source does not have write permission", 0)
		return
	}

	// Apply MaxBytesReader before reading body.
	maxBytes := src.PayloadLimits.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		d.writeErrorResponse(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/json", 0)
		return
	}

	var req openAIMemoriesRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		if strings.Contains(err.Error(), "request body too large") ||
			strings.Contains(err.Error(), "http: request body too large") {
			d.writeErrorResponse(w, r, http.StatusRequestEntityTooLarge, "payload_too_large",
				"request body exceeds maximum allowed size", 0)
			return
		}
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_json",
			"request body must be valid JSON", 0)
		return
	}

	if len(req.Messages) == 0 {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_request",
			"messages array must not be empty", 0)
		return
	}

	// Policy gate.
	dest := src.TargetDest
	if len(src.Policy.AllowedDestinations) > 0 && !containsString(src.Policy.AllowedDestinations, dest) {
		d.writeErrorResponse(w, r, http.StatusForbidden, "policy_denied",
			"destination not permitted for this source", 0)
		return
	}
	if len(src.Policy.AllowedOperations) > 0 && !containsString(src.Policy.AllowedOperations, "write") {
		d.writeErrorResponse(w, r, http.StatusForbidden, "policy_denied",
			"write operation not permitted for this source", 0)
		return
	}

	// Resolve subject.
	subject := req.Subject
	if subject == "" {
		subject = r.Header.Get("X-Subject")
	}
	if subject == "" {
		subject = src.Namespace
	}

	cfg := d.getConfig()
	rpm := src.RateLimit.RequestsPerMinute
	if rpm <= 0 {
		rpm = cfg.Daemon.RateLimit.GlobalRequestsPerMinute
	}

	payloadIDs := make([]string, 0, len(req.Messages))

	for _, msg := range req.Messages {
		if msg.Content == "" {
			continue // skip empty messages
		}

		// Rate limit per message write.
		if allowed, retryAfter := d.rl.Allow(src.Name, rpm); !allowed {
			d.metrics.RateLimitHitsTotal.WithLabelValues(src.Name).Inc()
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			d.writeErrorResponse(w, r, http.StatusTooManyRequests, "rate_limit_exceeded",
				"rate limit exceeded; back off and retry", retryAfter)
			return
		}

		payloadID := newID()
		requestID := newID()

		tp := destination.TranslatedPayload{
			PayloadID:        payloadID,
			RequestID:        requestID,
			Source:           src.Name,
			Subject:          subject,
			Namespace:        src.Namespace,
			Destination:      dest,
			Collection:       req.Collection,
			Content:          msg.Content,
			Role:             msg.Role,
			Timestamp:        time.Now().UTC(),
			SchemaVersion:    1,
			TransformVersion: "1.0",
			ActorType:        src.DefaultActorType,
		}

		payloadBytes, err := json.Marshal(tp)
		if err != nil {
			d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
				"failed to encode payload", 0)
			return
		}

		entry := wal.Entry{
			PayloadID:   payloadID,
			Source:      src.Name,
			Destination: dest,
			Subject:     subject,
			ActorType:   src.DefaultActorType,
			Payload:     payloadBytes,
		}
		walStart := time.Now()
		if err := d.wal.Append(entry); err != nil {
			d.logger.Error("daemon: openai write: WAL append failed",
				"component", "daemon",
				"source", src.Name,
				"payload_id", payloadID,
				"error", err,
			)
			d.metrics.ErrorsTotal.WithLabelValues("wal_append").Inc()
			d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
				"durable write failed; operator: check disk", 0)
			return
		}
		d.metrics.WALAppendLatency.Observe(time.Since(walStart).Seconds())

		if err := d.queue.Enqueue(entry); err != nil {
			w.Header().Set("Retry-After", "5")
			d.writeErrorResponse(w, r, http.StatusTooManyRequests, "queue_full",
				"queue full; data is durable in WAL and will be replayed on restart", 5)
			return
		}

		payloadIDs = append(payloadIDs, payloadID)
		d.metrics.ThroughputPerSource.WithLabelValues(src.Name).Inc()
	}

	d.metrics.PayloadProcessingLatency.WithLabelValues(src.Name).Observe(time.Since(writeStart).Seconds())

	d.writeJSON(w, http.StatusOK, openAIMemoriesResponse{
		PayloadIDs: payloadIDs,
		Status:     "accepted",
	})
}

// ---------------------------------------------------------------------------
// writeJSON / writeErrorResponse
// ---------------------------------------------------------------------------

// writeJSON serialises v to JSON and writes it to w with the given status
// code. It is a method on Daemon per Phase 0C Behavioral Contract item 18.
//
// Every Write return value is checked (no `_ = w.Write(...)`) per
// Phase 0C Behavioral Contract item 20.
func (d *Daemon) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		d.logger.Error("daemon: writeJSON encode failed",
			"component", "daemon",
			"error", err,
		)
	}
}

// writeErrorResponse writes a canonical error response.
// No raw stack traces or internal routing are exposed to clients.
// Reference: Tech Spec Section 7.4, Phase 0C Behavioral Contract item 14.
func (d *Daemon) writeErrorResponse(w http.ResponseWriter, r *http.Request, status int, code, msg string, retryAfter int) {
	resp := errorResponse{
		Error:             code,
		Message:           msg,
		RetryAfterSeconds: retryAfter,
		Details:           map[string]interface{}{},
	}
	d.writeJSON(w, status, resp)
}

// ---------------------------------------------------------------------------
// Transform helpers
// ---------------------------------------------------------------------------

// applyTransform applies a single named transform to val.
// Supported transforms: "trim", "coalesce:<default>".
func applyTransform(val, transform string) string {
	switch {
	case transform == "trim":
		return strings.TrimSpace(val)
	case strings.HasPrefix(transform, "coalesce:"):
		defaultVal := strings.TrimPrefix(transform, "coalesce:")
		if val == "" {
			return defaultVal
		}
		return val
	default:
		return val
	}
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// containsString reports whether s is in the slice.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// safePrefix returns the first n bytes of s, or all of s if len(s) < n.
// Used to log non-sensitive prefixes of opaque keys for correlation.
func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

