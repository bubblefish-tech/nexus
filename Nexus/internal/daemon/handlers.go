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
	"context"
	"crypto/rand"
	"database/sql"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/tidwall/gjson"
	"golang.org/x/time/rate"

	"github.com/bubblefish-tech/nexus/internal/audit"
	"github.com/bubblefish-tech/nexus/internal/health"
	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/demo"
	"github.com/bubblefish-tech/nexus/internal/destination"
	"github.com/bubblefish-tech/nexus/internal/eventsink"
	"github.com/bubblefish-tech/nexus/internal/immune"
	"github.com/bubblefish-tech/nexus/internal/lint"
	"github.com/bubblefish-tech/nexus/internal/provenance"
	"github.com/bubblefish-tech/nexus/internal/quarantine"
	"github.com/bubblefish-tech/nexus/internal/subscribe"
	"github.com/bubblefish-tech/nexus/internal/vizpipe"
	"github.com/bubblefish-tech/nexus/internal/query"
	"github.com/bubblefish-tech/nexus/internal/temporal"
	"github.com/bubblefish-tech/nexus/internal/version"
	"github.com/bubblefish-tech/nexus/internal/wal"
)

// embedContent computes a vector embedding for the given content string.
// Returns nil without blocking the write if the embedding client is nil,
// content is empty/whitespace, or the embed call fails.
func (d *Daemon) embedContent(ctx context.Context, payloadID, content string) []float32 {
	if d.embeddingClient == nil {
		d.logger.Info("daemon: embedContent skipped — client nil",
			"component", "daemon", "payload_id", payloadID)
		return nil
	}
	if strings.TrimSpace(content) == "" {
		d.logger.Info("daemon: embedContent skipped — empty content",
			"component", "daemon", "payload_id", payloadID)
		return nil
	}
	embedCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	vec, err := d.embeddingClient.Embed(embedCtx, content)
	if err != nil {
		d.logger.Warn("daemon: embed content failed",
			"component", "daemon",
			"payload_id", payloadID,
			"error", err,
		)
		return nil
	}
	d.logger.Info("daemon: embed content success",
		"component", "daemon",
		"payload_id", payloadID,
		"dimensions", len(vec),
	)
	return vec
}

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
	Results []enrichedRecord `json:"results"`
	Nexus   nexusMetadata    `json:"_nexus"`
}

type enrichedRecord struct {
	destination.TranslatedPayload
	TemporalBin   int    `json:"temporal_bin"`
	TemporalLabel string `json:"temporal_label"`
	AgeHuman      string `json:"age_human"`
}

func enrichRecords(records []destination.TranslatedPayload) []enrichedRecord {
	now := time.Now().UTC()
	out := make([]enrichedRecord, len(records))
	for i, r := range records {
		bin := temporal.ComputeBin(r.Timestamp, now)
		out[i] = enrichedRecord{
			TranslatedPayload: r,
			TemporalBin:       bin,
			TemporalLabel:     temporal.BinLabel(bin),
			AgeHuman:          temporal.HumanRelativeTime(r.Timestamp, now),
		}
	}
	return out
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
	ResultCount              int              `json:"result_count"`
	HasMore                  bool             `json:"has_more"`
	NextCursor               string           `json:"next_cursor,omitempty"`
	Profile                  string           `json:"profile"`
	Stage                    string           `json:"stage"`
	RetrievalStage           int              `json:"retrieval_stage"`
	SemanticUnavailable        bool             `json:"semantic_unavailable,omitempty"`
	SemanticUnavailableReason  string           `json:"semantic_unavailable_reason,omitempty"`
	RetrievalFirewallFiltered  bool             `json:"retrieval_firewall_filtered,omitempty"`
	ClusterExpanded            bool             `json:"cluster_expanded,omitempty"`
	Conflict                   bool             `json:"conflict,omitempty"`
	TemporalAwareness          bool             `json:"temporal_awareness"`
	SearchModes                []string         `json:"search_modes"`
	Debug                      *query.DebugInfo `json:"debug,omitempty"`
}

// healthSubsystem is a single subsystem entry in the structured health response.
type healthSubsystem struct {
	Status  string `json:"status"`            // "ok", "degraded", "disabled", "enabled"
	Details string `json:"details,omitempty"` // human-readable qualifier
}

// healthResponse is returned by /health and /ready.
type healthResponse struct {
	Status     string                     `json:"status"`
	Version    string                     `json:"version"`
	Subsystems map[string]healthSubsystem `json:"subsystems,omitempty"`
	Reasons    []string                   `json:"reasons,omitempty"`
}

// ---------------------------------------------------------------------------
// Rate Limiter
// ---------------------------------------------------------------------------

// rateLimiter implements a per-source fixed-window rate limiter.
// Each source gets a separate window that resets every minute.
// All state is in struct fields — no package-level variables.
//
// The windows map is keyed by source name (from config) or the hardcoded
// constant "_audit_admin". Both are bounded by the number of configured
// sources (typically <20). The map does not require eviction. If you
// change the key to anything derived from request data (IP, header,
// JWT claim), you MUST add eviction logic to prevent unbounded growth.
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

// bytesRateLimiter implements per-source bytes/sec rate limiting using
// token bucket (golang.org/x/time/rate). Each source gets its own limiter.
// A zero or negative limit means unlimited (no limiter is created).
type bytesRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func newBytesRateLimiter() *bytesRateLimiter {
	return &bytesRateLimiter{
		limiters: make(map[string]*rate.Limiter),
	}
}

// Allow checks whether n bytes are allowed for the given source. Returns
// true if allowed. When rejected, retryAfter is the estimated seconds
// until enough tokens are available.
func (bl *bytesRateLimiter) Allow(sourceName string, bytesPerSecond int64, n int) (bool, int) {
	if bytesPerSecond <= 0 {
		return true, 0
	}

	bl.mu.Lock()
	lim, ok := bl.limiters[sourceName]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(bytesPerSecond), int(bytesPerSecond))
		bl.limiters[sourceName] = lim
	}
	bl.mu.Unlock()

	if lim.AllowN(time.Now(), n) {
		return true, 0
	}

	// Estimate retry-after from the token refill rate.
	tokensNeeded := float64(n) - float64(lim.Burst())
	if tokensNeeded < 0 {
		tokensNeeded = float64(n)
	}
	retryAfter := int(tokensNeeded/float64(bytesPerSecond)) + 1
	if retryAfter < 1 {
		retryAfter = 1
	}
	return false, retryAfter
}

// ---------------------------------------------------------------------------
// Tier-aware rate limit resolution
// ---------------------------------------------------------------------------

// effectiveRPM returns the effective requests-per-minute for src, following
// the precedence chain: source config → tier config → global config.
//
// Per-source overrides per-tier; per-tier overrides global.
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.4.
func effectiveRPM(cfg *config.Config, src *config.Source) int {
	if src.RateLimit.RequestsPerMinute > 0 {
		return src.RateLimit.RequestsPerMinute
	}
	// Check tier-level override.
	for _, tc := range cfg.Daemon.Tiers {
		if tc.Level == src.Tier && tc.RequestsPerMinute > 0 {
			return tc.RequestsPerMinute
		}
	}
	return cfg.Daemon.RateLimit.GlobalRequestsPerMinute
}

// effectiveBPS returns the effective bytes-per-second limit for src,
// following the same precedence chain as effectiveRPM.
// Returns 0 (unlimited) if no limit is configured at any level.
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.4.
func effectiveBPS(cfg *config.Config, src *config.Source) int64 {
	if src.RateLimit.BytesPerSecond > 0 {
		return src.RateLimit.BytesPerSecond
	}
	for _, tc := range cfg.Daemon.Tiers {
		if tc.Level == src.Tier && tc.BytesPerSecond > 0 {
			return tc.BytesPerSecond
		}
	}
	return 0 // unlimited
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
	stageStart := writeStart

	// Admin tokens are not permitted on write endpoints.
	if isAdminFromContext(r.Context()) {
		d.emitAuditRecord(audit.InteractionRecord{
			RecordID:       audit.NewRecordID(),
			RequestID:      middleware.GetReqID(r.Context()),
			Timestamp:      writeStart,
			EffectiveIP:    effectiveClientIPFromContext(r.Context()),
			OperationType:  "write",
			Endpoint:       r.URL.Path,
			HTTPMethod:     r.Method,
			HTTPStatusCode: http.StatusUnauthorized,
			PolicyDecision: "denied",
			PolicyReason:   "wrong_token_class",
			LatencyMs:      float64(time.Since(writeStart).Microseconds()) / 1000.0,
		})
		d.writeErrorResponse(w, r, http.StatusUnauthorized, "wrong_token_class",
			"admin token cannot be used for write operations", 0)
		return
	}

	src := sourceFromContext(r.Context())
	if src == nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"source context missing", 0)
		return
	}

	// Step 1 — CanWrite check.
	// Reference: Tech Spec Section 6.1, Phase 0C Behavioral Contract item 12.
	if !src.CanWrite {
		d.emitPolicyDenied(r, src.Name, src.Namespace, "write", src.TargetDest, "source does not have write permission")
		d.emitAuditRecord(audit.InteractionRecord{
			RecordID:       audit.NewRecordID(),
			RequestID:      middleware.GetReqID(r.Context()),
			Timestamp:      writeStart,
			Source:         src.Name,
			EffectiveIP:    effectiveClientIPFromContext(r.Context()),
			OperationType:  "write",
			Endpoint:       r.URL.Path,
			HTTPMethod:     r.Method,
			HTTPStatusCode: http.StatusForbidden,
			Destination:    src.TargetDest,
			PolicyDecision: "denied",
			PolicyReason:   "source_not_permitted_to_write",
			LatencyMs:      float64(time.Since(writeStart).Microseconds()) / 1000.0,
		})
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
		d.emitPolicyDenied(r, src.Name, subject, "write", dest, "destination not permitted for this source")
		d.writeErrorResponse(w, r, http.StatusForbidden, "policy_denied",
			"destination not permitted for this source", 0)
		return
	}
	if len(src.Policy.AllowedOperations) > 0 && !containsString(src.Policy.AllowedOperations, "write") {
		d.emitPolicyDenied(r, src.Name, subject, "write", dest, "write operation not permitted for this source")
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

	d.pipeMetrics.writeStages["auth"].record(time.Since(stageStart))
	d.pipeMetrics.writeStages["policy"].record(time.Since(stageStart))
	stageStart = time.Now()

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
			d.emitAuditRecord(audit.InteractionRecord{
				RecordID:       audit.NewRecordID(),
				RequestID:      middleware.GetReqID(r.Context()),
				Timestamp:      writeStart,
				Source:         src.Name,
				EffectiveIP:    effectiveClientIPFromContext(r.Context()),
				OperationType:  "write",
				Endpoint:       r.URL.Path,
				HTTPMethod:     r.Method,
				HTTPStatusCode: http.StatusOK,
				PayloadID:      existingPayloadID,
				IdempotencyKey: idempotencyKey,
				IsDuplicate:    true,
				PolicyDecision: "allowed",
				LatencyMs:      float64(time.Since(writeStart).Microseconds()) / 1000.0,
			})
			d.writeJSON(w, http.StatusOK, writeResponse{
				PayloadID: existingPayloadID,
				Status:    "accepted",
			})
			return
		}
	}

	// Step 6 — Rate limit check AFTER idempotency.
	// Effective RPM follows precedence: source config → tier config → global.
	// Reference: Phase 0C Behavioral Contract item 11, Invariant 4.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.4.
	cfg := d.getConfig()
	rpm := effectiveRPM(cfg, src)
	if allowed, retryAfter := d.rl.Allow(src.Name, rpm); !allowed {
		d.logger.Warn("daemon: rate limit exceeded",
			"component", "daemon",
			"source", src.Name,
			"rpm", rpm,
			"request_id", middleware.GetReqID(r.Context()),
		)
		d.metrics.RateLimitHitsTotal.WithLabelValues(src.Name).Inc()
		d.emitRateLimitHit(r, src.Name, rpm)
		d.emitAuditRecord(audit.InteractionRecord{
			RecordID:       audit.NewRecordID(),
			RequestID:      middleware.GetReqID(r.Context()),
			Timestamp:      writeStart,
			Source:         src.Name,
			EffectiveIP:    effectiveClientIPFromContext(r.Context()),
			OperationType:  "write",
			Endpoint:       r.URL.Path,
			HTTPMethod:     r.Method,
			HTTPStatusCode: http.StatusTooManyRequests,
			PolicyDecision: "denied",
			PolicyReason:   "rate_limit_exceeded",
			LatencyMs:      float64(time.Since(writeStart).Microseconds()) / 1000.0,
		})
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		d.writeErrorResponse(w, r, http.StatusTooManyRequests, "rate_limit_exceeded",
			"rate limit exceeded; back off and retry", retryAfter)
		return
	}

	// Step 6b — Bytes/sec rate limit check (tier-aware).
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.4.
	bps := effectiveBPS(cfg, src)
	if bps > 0 {
		if allowed, retryAfter := d.bytesRL.Allow(src.Name, bps, len(bodyStr)); !allowed {
			d.logger.Warn("daemon: bytes rate limit exceeded",
				"component", "daemon",
				"source", src.Name,
				"bytes_per_second", bps,
				"body_bytes", len(bodyStr),
				"request_id", middleware.GetReqID(r.Context()),
			)
			d.metrics.RateLimitBytesRejectedTotal.WithLabelValues(src.Name).Inc()
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			d.writeErrorResponse(w, r, http.StatusTooManyRequests, "bytes_rate_limit_exceeded",
				"bytes/sec rate limit exceeded; back off and retry", retryAfter)
			return
		}
		d.metrics.RateLimitBytesTotal.WithLabelValues(src.Name).Add(float64(len(bodyStr)))
	}

	d.pipeMetrics.writeStages["idempotency"].record(time.Since(stageStart))
	d.pipeMetrics.writeStages["rate_limit"].record(time.Since(stageStart))
	stageStart = time.Now()

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

	// Parse sensitivity labels and classification tier from headers.
	// Reference: Tech Spec Addendum Section A3.2.
	var sensitivityLabels []string
	if labelsHeader := r.Header.Get("X-Sensitivity-Labels"); labelsHeader != "" {
		for _, l := range strings.Split(labelsHeader, ",") {
			trimmed := strings.TrimSpace(l)
			if trimmed != "" {
				sensitivityLabels = append(sensitivityLabels, trimmed)
			}
		}
	}
	classificationTier := strings.TrimSpace(r.Header.Get("X-Classification-Tier"))
	if classificationTier == "" {
		classificationTier = src.Policy.RetrievalFirewall.DefaultClassificationTier
	}

	// Validate classification tier against configured tier_order.
	// Reference: Tech Spec Addendum Section A3.3 — unknown tiers are rejected.
	if classificationTier != "" {
		tierOrder := cfg.Daemon.RetrievalFirewall.TierOrder
		if len(tierOrder) > 0 && !containsString(tierOrder, classificationTier) {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_classification_tier",
				"classification_tier must be one of the configured tier_order values", 0)
			return
		}
	}

	// Determine the numeric tier for this entry from the source's default.
	// Callers can override via the "tier" field in the request body in future;
	// for now the source's DefaultWriteTier is authoritative.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	writeTier := src.DefaultWriteTier
	if writeTier == 0 {
		writeTier = 1 // internal
	}

	tp := destination.TranslatedPayload{
		PayloadID:          payloadID,
		RequestID:          requestID,
		Source:             src.Name,
		Subject:            subject,
		Namespace:          src.Namespace,
		Destination:        dest,
		Collection:         mapped["collection"],
		Content:            mapped["content"],
		Model:              mapped["model"],
		Role:               mapped["role"],
		Timestamp:          time.Now().UTC(),
		IdempotencyKey:     idempotencyKey,
		SchemaVersion:      1,
		TransformVersion:   "1.0",
		ActorType:          actorType,
		ActorID:            actorID,
		Metadata:           metadata,
		SensitivityLabels:  sensitivityLabels,
		ClassificationTier: classificationTier,
		Tier:               writeTier,
	}
	embedStart := time.Now()
	tp.Embedding = d.embedContent(r.Context(), payloadID, tp.Content)
	d.pipeMetrics.writeStages["embedding"].record(time.Since(embedStart))

	// Sign write envelope if source has signing enabled.
	// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.2.
	d.signWriteEnvelope(&tp)

	// DEF.2 — Tier-0 immune scan. If the scanner intercepts the write,
	// store it in the quarantine table and return an indistinguishable 200
	// response. The caller cannot determine which path was taken.
	if d.immuneScanner != nil {
		immuneStart := time.Now()
		metaAny := make(map[string]any, len(tp.Metadata))
		for k, v := range tp.Metadata {
			metaAny[k] = v
		}
		scan := d.immuneScanner.ScanWrite(tp.Content, metaAny, tp.Embedding)
		d.pipeMetrics.writeStages["immune_scan"].record(time.Since(immuneStart))
		d.pipeMetrics.immuneScans.Add(1)
		switch scan.Action {
		case "quarantine", "reject":
			d.pipeMetrics.quarantineTotal.Add(1)
			metaBytes, _ := json.Marshal(tp.Metadata)
			d.interceptWrite(w, r, payloadID, requestID, src.Name, actorType, actorID,
				dest, subject, tp.Content, string(metaBytes), scan, writeStart)
			return
		case "normalize":
			if scan.NormalizedContent != "" {
				tp.Content = scan.NormalizedContent
			}
		}
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
	d.pipeMetrics.writeStages["wal_append"].record(time.Since(walStart))

	// Emit event to webhook sinks — non-blocking.
	// Reference: Tech Spec Section 10.1 — emission after WAL append.
	d.emitWriteEvent(entry, payloadBytes)

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

	d.pipeMetrics.writeStages["queue_send"].record(time.Since(walStart))
	d.pipeMetrics.recordWrite()

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

	// Step 19a — Emit interaction record. Failure MUST NOT cause request failure.
	// Reference: Tech Spec Addendum Section A2.4.
	d.emitAuditRecord(audit.InteractionRecord{
		RecordID:             audit.NewRecordID(),
		RequestID:            requestID,
		Timestamp:            writeStart,
		Source:               src.Name,
		ActorType:            actorType,
		ActorID:              actorID,
		EffectiveIP:          effectiveClientIPFromContext(r.Context()),
		OperationType:        "write",
		Endpoint:             r.URL.Path,
		HTTPMethod:           r.Method,
		HTTPStatusCode:       http.StatusOK,
		PayloadID:            payloadID,
		Destination:          dest,
		Subject:              subject,
		IdempotencyKey:       idempotencyKey,
		SensitivityLabelsSet: sensitivityLabels,
		PolicyDecision:       "allowed",
		LatencyMs:            float64(time.Since(writeStart).Microseconds()) / 1000.0,
		WALAppendMs:          float64(time.Since(walStart).Microseconds()) / 1000.0,
	})

	// Emit pipeline visualization event for WRITE — non-blocking.
	// Reference: dashboard-contract.md GET /api/viz/events.
	writeActorType := actorType
	if writeActorType == "" {
		writeActorType = "user"
	}
	d.vizPipe.Emit(vizpipe.Event{
		RequestID:   requestID,
		Source:      src.Name,
		Op:          "WRITE",
		Subject:     subject,
		ActorType:   writeActorType,
		Status:      "ALLOWED",
		Labels:      []string{},
		ResultCount: 0,
		TotalMs:     float64(time.Since(writeStart).Microseconds()) / 1000.0,
		Stages:      nil,
	})
	d.liteBus.Emit("memory_written", map[string]any{"source": src.Name, "payload_id": payloadID})

	if d.subscribeMatcher != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			matches, err := d.subscribeMatcher.Match(ctx, tp.Content)
			if err == nil {
				for _, sub := range matches {
					d.subscribeStore.IncrementMatch(sub.ID)
					d.liteBus.Emit("subscription.matched", map[string]any{
						"subscription_id": sub.ID,
						"agent_id":        sub.AgentID,
						"filter":          sub.Filter,
						"memory_id":       payloadID,
					})
					d.emitAuditRecord(audit.InteractionRecord{
						RecordID:      audit.NewRecordID(),
						Timestamp:     time.Now(),
						Source:        sub.AgentID,
						OperationType: "subscription.matched",
						PolicyDecision: "allowed",
					})
				}
			}
		}()
	}

	// Step 13 — Return 200 + payload_id.
	d.writeJSON(w, http.StatusOK, writeResponse{
		PayloadID: payloadID,
		Status:    "accepted",
	})
}

// interceptWrite stores an immune-scanner interception in the quarantine table,
// emits a memory.quarantined audit event, and returns an indistinguishable
// writeResponse so the caller cannot determine the write was quarantined.
func (d *Daemon) interceptWrite(
	w http.ResponseWriter, r *http.Request,
	payloadID, requestID, sourceName, actorType, actorID, dest, subject,
	content, metadataJSON string,
	scan immune.ScanResult,
	writeStart time.Time,
) {
	d.logger.Info("daemon: write quarantined by Tier-0 immune scanner",
		"component", "immune",
		"payload_id", payloadID,
		"rule", scan.Rule,
		"action", scan.Action,
	)
	if d.quarantineStore != nil {
		rec := quarantine.Record{
			ID:                quarantine.NewID(),
			OriginalPayloadID: payloadID,
			Content:           content,
			MetadataJSON:      metadataJSON,
			SourceName:        sourceName,
			AgentID:           actorID,
			QuarantineReason:  scan.Details,
			RuleID:            scan.Rule,
			QuarantinedAtMs:   time.Now().UnixMilli(),
		}
		if err := d.quarantineStore.Insert(rec); err != nil {
			d.logger.Error("daemon: quarantine store insert failed",
				"component", "quarantine",
				"error", err,
			)
		}
	}
	d.emitControlEvent(
		audit.ControlEventMemoryQuarantined,
		actorID, payloadID, "memory",
		actorID, scan.Rule,
		"quarantined", scan.Details,
		map[string]string{"source": sourceName, "scan_action": scan.Action},
	)
	d.emitAuditRecord(audit.InteractionRecord{
		RecordID:       audit.NewRecordID(),
		RequestID:      requestID,
		Timestamp:      writeStart,
		Source:         sourceName,
		ActorType:      actorType,
		ActorID:        actorID,
		EffectiveIP:    effectiveClientIPFromContext(r.Context()),
		OperationType:  "write",
		Endpoint:       r.URL.Path,
		HTTPMethod:     r.Method,
		HTTPStatusCode: http.StatusOK,
		PayloadID:      payloadID,
		Destination:    dest,
		Subject:        subject,
		PolicyDecision: "quarantined",
		PolicyReason:   scan.Rule,
		LatencyMs:      float64(time.Since(writeStart).Microseconds()) / 1000.0,
	})
	d.liteBus.Emit("quarantine_event", map[string]any{"source": sourceName, "rule": scan.Rule, "action": scan.Action, "payload_id": payloadID})
	d.liteBus.Emit("immune_detection", map[string]any{"source": sourceName, "rule": scan.Rule, "action": scan.Action, "payload_id": payloadID})
	// Identical shape to a successful write — response-shape indistinguishability.
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
	isAdmin := isAdminFromContext(r.Context())
	src := sourceFromContext(r.Context())
	if src == nil && !isAdmin {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"source context missing", 0)
		return
	}

	// Admin tokens bypass CanRead and rate-limit checks.
	// They are allowed on data endpoints for debug_stages.
	// Reference: Tech Spec Section 7.3.
	if src == nil && isAdmin {
		// Synthesise a permissive source for admin queries.
		src = &config.Source{
			Name:           "_admin",
			Namespace:      "default",
			CanRead:        true,
			CanWrite:       false,
			DefaultProfile: "deep",
		}
	}

	// Pre-cascade CanRead guard — checked before rate limiting so that
	// unauthorised sources do not consume rate-limit budget.
	// Reference: Phase 0C Behavioral Contract item 12.
	if !src.CanRead {
		d.emitPolicyDenied(r, src.Name, src.Namespace, "read", chi.URLParam(r, "destination"), "source does not have read permission")
		d.emitAuditRecord(audit.InteractionRecord{
			RecordID:       audit.NewRecordID(),
			RequestID:      middleware.GetReqID(r.Context()),
			Timestamp:      queryStart,
			Source:         src.Name,
			EffectiveIP:    effectiveClientIPFromContext(r.Context()),
			OperationType:  "query",
			Endpoint:       r.URL.Path,
			HTTPMethod:     r.Method,
			HTTPStatusCode: http.StatusForbidden,
			PolicyDecision: "denied",
			PolicyReason:   "source_not_permitted_to_read",
			LatencyMs:      float64(time.Since(queryStart).Microseconds()) / 1000.0,
		})
		d.writeErrorResponse(w, r, http.StatusForbidden, "source_not_permitted_to_read",
			"this source does not have read permission", 0)
		return
	}

	// Rate limit — applies to reads too (tier-aware).
	// Reference: Phase 0C Behavioral Contract item 11.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.4.
	qcfg := d.getConfig()
	rpm := effectiveRPM(qcfg, src)
	if allowed, retryAfter := d.rl.Allow(src.Name+":read", rpm); !allowed {
		d.metrics.RateLimitHitsTotal.WithLabelValues(src.Name).Inc()
		d.emitRateLimitHit(r, src.Name, rpm)
		d.emitAuditRecord(audit.InteractionRecord{
			RecordID:       audit.NewRecordID(),
			RequestID:      middleware.GetReqID(r.Context()),
			Timestamp:      queryStart,
			Source:         src.Name,
			EffectiveIP:    effectiveClientIPFromContext(r.Context()),
			OperationType:  "query",
			Endpoint:       r.URL.Path,
			HTTPMethod:     r.Method,
			HTTPStatusCode: http.StatusTooManyRequests,
			PolicyDecision: "denied",
			PolicyReason:   "rate_limit_exceeded",
			LatencyMs:      float64(time.Since(queryStart).Microseconds()) / 1000.0,
		})
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
	// TierFilter is enabled for source tokens; admin tokens see all tiers.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.1.
	tierFilter := !isAdmin
	sourceTier := src.Tier
	if isAdmin || src.Tier == 0 {
		sourceTier = 3 // admin and synthesized sources get unrestricted access
	}
	cq, err := query.Normalize(destination.QueryParams{
		Destination: destName,
		Namespace:   src.Namespace,
		Subject:     subject,
		Q:           r.URL.Query().Get("q"),
		Limit:       limit,
		Cursor:      r.URL.Query().Get("cursor"),
		Profile:     profile,
		ActorType:   actorTypeFilter,
		TierFilter:  tierFilter,
		SourceTier:  sourceTier,
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

	// Debug stages: when ?debug_stages=true AND the request carries an admin
	// token, populate _nexus.debug in the response. Data tokens with
	// debug_stages=true are silently ignored — normal response returned.
	// Reference: Tech Spec Section 7.3.
	debugStages := r.URL.Query().Get("debug_stages") == "true" && isAdmin

	// Execute the 6-stage retrieval cascade.
	// Reference: Tech Spec Section 3.4.
	runner := query.New(d.querier, d.logger).
		WithExactCache(d.exactCache).
		WithSemanticCache(d.semanticCache).
		WithEmbeddingClient(d.embeddingClient, d.metrics.EmbeddingLatency).
		WithRetrievalConfig(qcfg.Retrieval).
		WithDecayCounter(d.metrics.TemporalDecayApplied).
		WithDebug(debugStages).
		WithFirewall(d.retrievalFirewall).
		WithBM25Searcher(d.bm25Searcher)

	// Wire cluster querier for cluster-aware retrieval profile.
	// Reference: v0.1.3 Build Plan Phase 3 Subtask 3.4.
	if clusterQ, ok := d.querier.(destination.ClusterQuerier); ok {
		runner = runner.WithClusterQuerier(clusterQ)
	}
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
		d.emitPolicyDenied(r, src.Name, subject, "read", destName, cascResult.Denial.Reason)
		// Emit specific retrieval firewall denied event when the denial
		// originates from the retrieval firewall.
		if cascResult.Denial.Code == "retrieval_firewall_denied" {
			d.emitRetrievalFirewallDenied(r, src.Name, subject, cascResult.Denial.Reason)
			d.metrics.FirewallDeniedTotal.WithLabelValues(src.Name).Inc()
		}
		d.emitAuditRecord(audit.InteractionRecord{
			RecordID:       audit.NewRecordID(),
			RequestID:      middleware.GetReqID(r.Context()),
			Timestamp:      queryStart,
			Source:         src.Name,
			EffectiveIP:    effectiveClientIPFromContext(r.Context()),
			OperationType:  "query",
			Endpoint:       r.URL.Path,
			HTTPMethod:     r.Method,
			HTTPStatusCode: http.StatusForbidden,
			Subject:        subject,
			PolicyDecision: "denied",
			PolicyReason:   cascResult.Denial.Reason,
			LatencyMs:      float64(time.Since(queryStart).Microseconds()) / 1000.0,
		})
		d.writeErrorResponse(w, r, http.StatusForbidden, cascResult.Denial.Code,
			cascResult.Denial.Reason, 0)
		return
	}

	if d.subscribeStore != nil && d.subscribeMatcher != nil {
		agentID := r.Header.Get("X-Agent-ID")
		if agentID == "" {
			agentID = src.Name
		}
		agentSubs := d.subscribeStore.ListForAgent(agentID)
		if len(agentSubs) > 0 && len(cascResult.Records) > 1 {
			d.boostSubscribedResults(r.Context(), cascResult.Records, agentSubs)
		}
	}

	queryDuration := time.Since(queryStart)
	d.metrics.ReadLatency.WithLabelValues(src.Name, "/query").Observe(queryDuration.Seconds())

	// Emit pipeline visualization event — non-blocking.
	// Reference: dashboard-contract.md GET /api/viz/events.
	vizStatus := "ALLOWED"
	var vizLabels []string
	if cascResult.FirewallResult != nil && cascResult.FirewallResult.Filtered {
		vizStatus = "FILTERED"
		vizLabels = cascResult.FirewallResult.FilteredLabels
	}
	if vizLabels == nil {
		vizLabels = []string{}
	}

	// Build 6-element stages array. The winning stage gets hit:true.
	stageNames := [6]string{"policy", "cache", "semantic", "lookup", "vector", "merge"}
	stages := make([]vizpipe.StageInfo, 6)
	for i := range stages {
		stages[i] = vizpipe.StageInfo{Stage: i, Name: stageNames[i]}
	}
	winStage := cascResult.RetrievalStage
	if winStage >= 0 && winStage < 6 {
		stages[winStage].Hit = true
		stages[winStage].Ms = float64(queryDuration.Microseconds()) / 1000.0
	}

	actorType := src.DefaultActorType
	if actorType == "" {
		actorType = "user"
	}

	d.vizPipe.Emit(vizpipe.Event{
		RequestID:   middleware.GetReqID(r.Context()),
		Source:      src.Name,
		Op:          "QUERY",
		Subject:     subject,
		ActorType:   actorType,
		Status:      vizStatus,
		Labels:      vizLabels,
		ResultCount: len(cascResult.Records),
		TotalMs:     float64(queryDuration.Microseconds()) / 1000.0,
		Stages:      stages,
	})
	d.liteBus.Emit("memory_queried", map[string]any{"source": src.Name, "result_count": len(cascResult.Records)})
	d.pipeMetrics.recordRead()

	searchModes := []string{"semantic", "keyword", "hybrid"}
	meta := nexusMetadata{
		ResultCount:               len(cascResult.Records),
		HasMore:                   cascResult.HasMore,
		NextCursor:                cascResult.NextCursor,
		Profile:                   cascResult.Profile,
		Stage:                     query.StageName(cascResult.RetrievalStage),
		RetrievalStage:            cascResult.RetrievalStage,
		SemanticUnavailable:       cascResult.SemanticUnavailable,
		SemanticUnavailableReason: cascResult.SemanticUnavailableReason,
		ClusterExpanded:           cascResult.ClusterExpanded,
		Conflict:                  cascResult.Conflict,
		TemporalAwareness:         true,
		SearchModes:               searchModes,
		Debug:                     cascResult.Debug,
	}

	// When retrieval firewall filtered results, set the metadata flag and
	// emit a security event. Reference: Tech Spec Addendum Section A3.5, A3.7.
	if cascResult.FirewallResult != nil && cascResult.FirewallResult.Filtered {
		meta.RetrievalFirewallFiltered = true
		d.emitRetrievalFirewallFiltered(r, src.Name, subject,
			cascResult.FirewallResult.FilteredLabels,
			cascResult.FirewallResult.TierFiltered,
			cascResult.FirewallResult.CountRemoved,
			cascResult.FirewallResult.CountRemaining,
		)
		// When ALL results removed, increment denied metric.
		if cascResult.FirewallResult.CountRemaining == 0 {
			d.metrics.FirewallDeniedTotal.WithLabelValues(src.Name).Inc()
		}
	}

	// Step 14a — Emit interaction record for the read path.
	// Reference: Tech Spec Addendum Section A2.4.
	queryAuditRec := audit.InteractionRecord{
		RecordID:         audit.NewRecordID(),
		RequestID:        middleware.GetReqID(r.Context()),
		Timestamp:        queryStart,
		Source:           src.Name,
		EffectiveIP:      effectiveClientIPFromContext(r.Context()),
		OperationType:    "query",
		Endpoint:         r.URL.Path,
		HTTPMethod:       r.Method,
		HTTPStatusCode:   http.StatusOK,
		Subject:          subject,
		RetrievalProfile: cascResult.Profile,
		StagesHit:        []string{query.StageName(cascResult.RetrievalStage)},
		ResultCount:      len(cascResult.Records),
		CacheHit:         cascResult.RetrievalStage <= 2 && cascResult.RetrievalStage >= 0,
		PolicyDecision:   "allowed",
		LatencyMs:        float64(queryDuration.Microseconds()) / 1000.0,
	}
	if cascResult.FirewallResult != nil && cascResult.FirewallResult.Filtered {
		queryAuditRec.PolicyDecision = "filtered"
		queryAuditRec.SensitivityLabelsFiltered = cascResult.FirewallResult.FilteredLabels
		queryAuditRec.TierFiltered = cascResult.FirewallResult.TierFiltered
	}
	d.emitAuditRecord(queryAuditRec)

	// SSE streaming — when client sends Accept: text/event-stream.
	// Reference: Tech Spec Section 12, Phase 7 Behavioral Contract 8.
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		d.streamQuerySSE(w, enrichRecords(cascResult.Records), meta)
		return
	}

	d.writeJSON(w, http.StatusOK, queryResponse{
		Results: enrichRecords(cascResult.Records),
		Nexus:   meta,
	})
}

// ---------------------------------------------------------------------------
// Health / Ready Handlers
// ---------------------------------------------------------------------------

// handleHealth is the liveness probe. Always returns 200 while the process
// is alive. Returns structured JSON with all subsystem statuses.
// No authentication required.
// Reference: Tech Spec Section 11.4, Phase 0C Behavioral Contract item 13.
func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	subs := make(map[string]healthSubsystem, 7)

	// WAL — in-memory flag set by watchdog; no I/O.
	if d.walHealthy.Load() == 1 {
		subs["wal"] = healthSubsystem{Status: "ok"}
	} else {
		subs["wal"] = healthSubsystem{Status: "degraded", Details: "WAL unhealthy"}
	}

	// Database — presence check only (no ping here; that lives in /ready).
	if d.dest != nil {
		subs["database"] = healthSubsystem{Status: "ok"}
	} else {
		subs["database"] = healthSubsystem{Status: "degraded", Details: "no destination"}
	}

	// Audit WAL.
	if d.auditWAL != nil {
		subs["audit"] = healthSubsystem{Status: "ok"}
	} else {
		subs["audit"] = healthSubsystem{Status: "disabled"}
	}

	// BF-Sketch substrate.
	if d.substrate != nil && d.substrate.Enabled() {
		subs["substrate"] = healthSubsystem{Status: "ok"}
	} else {
		subs["substrate"] = healthSubsystem{Status: "disabled"}
	}

	// At-rest encryption.
	if d.mkm != nil && d.mkm.IsEnabled() {
		subs["encryption"] = healthSubsystem{Status: "enabled"}
	} else {
		subs["encryption"] = healthSubsystem{Status: "disabled"}
	}

	// MCP server.
	if d.mcpServer != nil {
		subs["mcp"] = healthSubsystem{Status: "ok"}
	} else {
		subs["mcp"] = healthSubsystem{Status: "disabled"}
	}

	// Event bus.
	if d.eventBus != nil {
		subs["eventbus"] = healthSubsystem{Status: "ok"}
	} else {
		subs["eventbus"] = healthSubsystem{Status: "disabled"}
	}

	// Subsystem goroutine health — report any recovered panics.
	if d.subsystemHealth != nil {
		for name, reason := range d.subsystemHealth.Degraded() {
			subs[name] = healthSubsystem{Status: "degraded", Details: "panic recovered: " + reason}
		}
	}

	// Collect reasons for degraded subsystems.
	var reasons []string
	overall := "ok"
	for name, s := range subs {
		if s.Status == "degraded" {
			overall = "degraded"
			reasons = append(reasons, name+":"+s.Status)
		}
	}

	// Saturation metrics: goroutines and heap.
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	subs["goroutines"] = healthSubsystem{
		Status:  "ok",
		Details: fmt.Sprintf("count=%d", runtime.NumGoroutine()),
	}
	subs["heap"] = healthSubsystem{
		Status:  "ok",
		Details: fmt.Sprintf("inuse=%dMiB gc_pause_ns=%d num_gc=%d", memStats.HeapInuse/(1<<20), memStats.PauseTotalNs, memStats.NumGC),
	}

	d.writeJSON(w, http.StatusOK, healthResponse{
		Status:     overall,
		Version:    version.Version,
		Subsystems: subs,
		Reasons:    reasons,
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

	if pinger, ok := d.dest.(interface{ Ping() error }); ok {
		if err := pinger.Ping(); err != nil {
			d.logger.Warn("daemon: readiness probe: destination unhealthy",
				"component", "daemon",
				"error", err,
			)
			d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "destination_unavailable",
				"destination is not reachable", 0)
			return
		}
	}
	d.writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ready",
		Version: version.Version,
	})
}

// ---------------------------------------------------------------------------
// Admin Handlers (minimal for Phase 0C)
// ---------------------------------------------------------------------------

// handleAdminStatus returns the full daemon status matching the dashboard
// contract shape exactly.
// Reference: dashboard-contract.md GET /api/status.
func (d *Daemon) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/status").Inc()

	cfg := d.getConfig()

	queueDepth := 0
	if d.queue != nil {
		queueDepth = d.queue.Len()
	}

	consistencyScore := math.Float64frombits(d.consistencyScore.Load())

	// Memory stats.
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// WAL state.
	walPending := int64(0)
	walSegment := ""
	if d.wal != nil {
		walPending = d.wal.PendingCount()
		walSegment = d.wal.CurrentSegment()
	}
	walHealthy := d.walHealthy.Load() == 1

	// Cache hit rates.
	exactHits := int64(0)
	exactMisses := int64(0)
	semHits := int64(0)
	semMisses := int64(0)
	if d.exactStats != nil {
		exactHits = d.exactStats.HitCount()
		exactMisses = d.exactStats.MissCount()
	}
	if d.semanticStats != nil {
		semHits = d.semanticStats.HitCount()
		semMisses = d.semanticStats.MissCount()
	}
	totalHits := exactHits + semHits
	totalMisses := exactMisses + semMisses
	totalQueries := totalHits + totalMisses

	hitRate := 0.0
	exactRate := 0.0
	semanticRate := 0.0
	if totalQueries > 0 {
		hitRate = float64(totalHits) / float64(totalQueries)
		exactRate = float64(exactHits) / float64(totalQueries)
		semanticRate = float64(semHits) / float64(totalQueries)
	}

	// Destination health.
	destName := "sqlite"
	if len(cfg.Destinations) > 0 {
		destName = cfg.Destinations[0].Name
	}
	destHealthy := true
	var destLastError interface{} = nil
	if d.dest != nil {
		if pinger, ok := d.dest.(interface{ Ping() error }); ok {
			if err := pinger.Ping(); err != nil {
				destHealthy = false
				destLastError = err.Error()
			}
		}
	}

	// Memory count from destination.
	var memoriesTotal int64
	if mc, ok := d.dest.(destination.MemoryCounter); ok {
		if count, err := mc.MemoryCount(); err == nil {
			memoriesTotal = count
		}
	}

	// Source health.
	sourceHealth := make([]map[string]interface{}, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		sourceHealth = append(sourceHealth, map[string]interface{}{
			"name":   s.Name,
			"status": "active",
		})
	}

	// Audit status.
	auditEnabled := d.auditLogger != nil

	d.writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":               "ok",
		"version":              version.Version,
		"uptime_seconds":       int(time.Since(d.startedAt).Seconds()),
		"pid":                  os.Getpid(),
		"bind":                 fmt.Sprintf("%s:%d", cfg.Daemon.Bind, cfg.Daemon.Port),
		"web_port":             cfg.Daemon.Web.Port,
		"memory_resident_bytes": memStats.Sys,
		"goroutines":           runtime.NumGoroutine(),
		"queue_depth":          queueDepth,
		"wal": map[string]interface{}{
			"pending_entries":           walPending,
			"healthy":                   walHealthy,
			"last_checkpoint_seconds_ago": 0, // checkpoint not yet tracked
			"integrity_mode":            cfg.Daemon.WAL.Integrity.Mode,
			"current_segment":           walSegment,
		},
		"consistency_score": consistencyScore,
		"destinations": []map[string]interface{}{
			{
				"name":       destName,
				"healthy":    destHealthy,
				"last_error": destLastError,
			},
		},
		"cache": map[string]interface{}{
			"hit_rate":      hitRate,
			"exact_rate":    exactRate,
			"semantic_rate": semanticRate,
		},
		"sources_total":    len(cfg.Sources),
		"memories_total":   memoriesTotal,
		"writes_total":     d.pipeMetrics.writesTotal.Load(),
		"writes_1m":        d.pipeMetrics.writes1m(),
		"reads_total":      d.pipeMetrics.readsTotal.Load(),
		"reads_1m":         d.pipeMetrics.reads1m(),
		"errors_1m":        d.pipeMetrics.errors1m(),
		"immune_scans":     d.pipeMetrics.immuneScans.Load(),
		"quarantine_total": d.pipeMetrics.quarantineTotal.Load(),
		"audit_enabled":    auditEnabled,
		"cascade_stages":   d.pipeMetrics.stageSnapshot(d.pipeMetrics.cascadeStages),
		"write_stages":     d.pipeMetrics.stageSnapshot(d.pipeMetrics.writeStages),
		"source_health":    sourceHealth,
	})
}

// handleAdminConfig returns a sanitized view of the daemon configuration.
// NEVER includes secrets (admin_token, api_key, key_file contents, etc.).
func (d *Daemon) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/config").Inc()
	cfg := d.getConfig()

	// Source names (no keys).
	sourceNames := make([]string, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		sourceNames = append(sourceNames, s.Name)
	}

	// Destination names (no keys).
	destNames := make([]string, 0, len(cfg.Destinations))
	for _, dst := range cfg.Destinations {
		destNames = append(destNames, dst.Name)
	}

	d.writeJSON(w, http.StatusOK, map[string]interface{}{
		"daemon": map[string]interface{}{
			"bind":       cfg.Daemon.Bind,
			"port":       cfg.Daemon.Port,
			"log_level":  cfg.Daemon.LogLevel,
			"mode":       cfg.Daemon.Mode,
			"queue_size": cfg.Daemon.QueueSize,
		},
		"wal": map[string]interface{}{
			"path":                 cfg.Daemon.WAL.Path,
			"max_segment_size_mb":  cfg.Daemon.WAL.MaxSegmentSizeMB,
			"integrity_mode":       cfg.Daemon.WAL.Integrity.Mode,
			"encryption_enabled":   cfg.Daemon.WAL.Encryption.Enabled,
			"watchdog_interval_s":  cfg.Daemon.WAL.Watchdog.IntervalSeconds,
			"watchdog_min_disk":    cfg.Daemon.WAL.Watchdog.MinDiskBytes,
		},
		"mcp": map[string]interface{}{
			"enabled":     cfg.Daemon.MCP.Enabled,
			"port":        cfg.Daemon.MCP.Port,
			"bind":        cfg.Daemon.MCP.Bind,
			"source_name": cfg.Daemon.MCP.SourceName,
		},
		"web": map[string]interface{}{
			"port":         cfg.Daemon.Web.Port,
			"require_auth": cfg.Daemon.Web.RequireAuth,
		},
		"embedding": map[string]interface{}{
			"enabled":    cfg.Daemon.Embedding.Enabled,
			"provider":   cfg.Daemon.Embedding.Provider,
			"model":      cfg.Daemon.Embedding.Model,
			"dimensions": cfg.Daemon.Embedding.Dimensions,
		},
		"tls": map[string]interface{}{
			"enabled":     cfg.Daemon.TLS.Enabled,
			"min_version": cfg.Daemon.TLS.MinVersion,
		},
		"rate_limit": map[string]interface{}{
			"global_rpm": cfg.Daemon.RateLimit.GlobalRequestsPerMinute,
		},
		"retrieval": map[string]interface{}{
			"time_decay":       cfg.Retrieval.TimeDecay,
			"half_life_days":   cfg.Retrieval.HalfLifeDays,
			"default_profile":  cfg.Retrieval.DefaultProfile,
		},
		"audit": map[string]interface{}{
			"enabled":    cfg.Daemon.Audit.Enabled,
			"dual_write": cfg.Daemon.Audit.AuditDualWriteEnabled(),
		},
		"sources":      sourceNames,
		"destinations": destNames,
	})
}

// handleLint runs config lint checks and returns warnings matching the
// dashboard contract shape exactly.
// Reference: dashboard-contract.md GET /api/lint.
func (d *Daemon) handleLint(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/lint").Inc()

	cfg := d.getConfig()

	configDir, err := config.ConfigDir()
	if err != nil {
		d.writeJSON(w, http.StatusInternalServerError, errorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "failed to resolve config directory",
			Details: map[string]interface{}{},
		})
		return
	}

	result := lint.Run(cfg, configDir)
	d.metrics.ConfigLintWarnings.Set(float64(result.WarningCount()))

	// Transform to dashboard contract shape: warnings[] with code, file, line.
	type contractWarning struct {
		Severity string `json:"severity"`
		Code     string `json:"code"`
		Message  string `json:"message"`
		File     string `json:"file"`
		Line     int    `json:"line"`
	}

	warnings := make([]contractWarning, len(result.Findings))
	for i, f := range result.Findings {
		warnings[i] = contractWarning{
			Severity: string(f.Severity),
			Code:     f.Check,
			Message:  f.Message,
			File:     "daemon.toml",
			Line:     0,
		}
	}

	d.writeJSON(w, http.StatusOK, map[string]interface{}{
		"warnings": warnings,
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
func (d *Daemon) streamQuerySSE(w http.ResponseWriter, records []enrichedRecord, meta nexusMetadata) {
	flusher, ok := w.(http.Flusher)
	if !ok {
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

	if isAdminFromContext(r.Context()) {
		d.writeErrorResponse(w, r, http.StatusUnauthorized, "wrong_token_class",
			"admin token cannot be used for write operations", 0)
		return
	}

	src := sourceFromContext(r.Context())
	if src == nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"source context missing", 0)
		return
	}

	if !src.CanWrite {
		d.emitPolicyDenied(r, src.Name, src.Namespace, "write", src.TargetDest, "source does not have write permission")
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
		d.emitPolicyDenied(r, src.Name, src.Namespace, "write", dest, "destination not permitted for this source")
		d.writeErrorResponse(w, r, http.StatusForbidden, "policy_denied",
			"destination not permitted for this source", 0)
		return
	}
	if len(src.Policy.AllowedOperations) > 0 && !containsString(src.Policy.AllowedOperations, "write") {
		d.emitPolicyDenied(r, src.Name, src.Namespace, "write", dest, "write operation not permitted for this source")
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
		tp.Embedding = d.embedContent(r.Context(), payloadID, tp.Content)

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

		// Emit event to webhook sinks — non-blocking.
		d.emitWriteEvent(entry, payloadBytes)

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

	// Emit interaction record for OpenAI write path.
	// Reference: Tech Spec Addendum Section A2.4.
	d.emitAuditRecord(audit.InteractionRecord{
		RecordID:       audit.NewRecordID(),
		RequestID:      middleware.GetReqID(r.Context()),
		Timestamp:      writeStart,
		Source:         src.Name,
		ActorType:      src.DefaultActorType,
		EffectiveIP:    effectiveClientIPFromContext(r.Context()),
		OperationType:  "write",
		Endpoint:       r.URL.Path,
		HTTPMethod:     r.Method,
		HTTPStatusCode: http.StatusOK,
		Destination:    dest,
		Subject:        subject,
		PolicyDecision: "allowed",
		LatencyMs:      float64(time.Since(writeStart).Microseconds()) / 1000.0,
	})

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
// Conflict Inspector + Time-Travel
// ---------------------------------------------------------------------------

// handleConflicts serves GET /api/conflicts — returns groups of contradictory
// memories sharing the same subject + collection but with divergent content.
// Read-only. NEVER modifies data.
//
// Query parameters:
//
//	?source=NAME    filter by source
//	?subject=NAME   filter by subject
//	?actor_type=T   filter by actor_type
//	?limit=N        max groups (default 50, clamped to 200)
//	?offset=N       pagination offset
//
// Reference: Tech Spec Section 13.2, Phase R-22.
func (d *Daemon) handleConflicts(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/conflicts").Inc()

	cq, ok := d.querier.(destination.ConflictQuerier)
	if !ok {
		d.writeErrorResponse(w, r, http.StatusNotImplemented, "not_implemented",
			"conflict detection not supported by this destination", 0)
		return
	}

	limit := 50
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if os := r.URL.Query().Get("offset"); os != "" {
		if n, err := strconv.Atoi(os); err == nil && n >= 0 {
			offset = n
		}
	}

	groups, err := cq.QueryConflicts(destination.ConflictParams{
		Source:    r.URL.Query().Get("source"),
		Subject:   r.URL.Query().Get("subject"),
		ActorType: r.URL.Query().Get("actor_type"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		d.logger.Error("daemon: conflict query failed",
			"component", "daemon",
			"error", err,
		)
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"conflict query failed", 0)
		return
	}

	// Transform to dashboard contract shape.
	type contractMemory struct {
		Source    string `json:"source"`
		ActorType string `json:"actor_type"`
		TS        string `json:"ts"`
		Content   string `json:"content"`
	}
	type contractConflict struct {
		ID        string           `json:"id"`
		Subject   string           `json:"subject"`
		Entity    string           `json:"entity"`
		GroupSize int              `json:"group_size"`
		Memories  []contractMemory `json:"memories"`
	}

	out := make([]contractConflict, len(groups))
	for i, g := range groups {
		// Stable ID from subject + entity_key.
		idHash := sha256.Sum256([]byte(g.Subject + g.EntityKey))
		id := "cf_" + hex.EncodeToString(idHash[:])[:6]

		memories := make([]contractMemory, g.Count)
		for j := 0; j < g.Count; j++ {
			src := ""
			if j < len(g.Sources) {
				src = g.Sources[j]
			}
			content := ""
			if j < len(g.ConflictingValues) {
				content = g.ConflictingValues[j]
			}
			ts := ""
			if j < len(g.Timestamps) {
				ts = g.Timestamps[j].UTC().Format(time.RFC3339)
			}
			memories[j] = contractMemory{
				Source:    src,
				ActorType: "user",
				TS:        ts,
				Content:   content,
			}
		}

		out[i] = contractConflict{
			ID:        id,
			Subject:   g.Subject,
			Entity:    g.EntityKey,
			GroupSize: g.Count,
			Memories:  memories,
		}
	}

	d.writeJSON(w, http.StatusOK, map[string]interface{}{
		"conflicts": out,
	})
}

// handleTimeTravel serves GET /api/timetravel — returns memories as of a
// specific timestamp. Read-only. NEVER modifies data.
//
// Query parameters:
//
//	?as_of=RFC3339  required — return memories with timestamp <= this value
//	?subject=NAME   filter by subject
//	?namespace=NS   filter by namespace
//	?destination=D  filter by destination
//	?limit=N        max records (default 50, clamped to 200)
//	?offset=N       pagination offset
//
// Reference: Tech Spec Section 13.2, Phase R-22.
func (d *Daemon) handleTimeTravel(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/timetravel").Inc()

	ttq, ok := d.querier.(destination.TimeTravelQuerier)
	if !ok {
		d.writeErrorResponse(w, r, http.StatusNotImplemented, "not_implemented",
			"time-travel not supported by this destination", 0)
		return
	}

	asOfStr := r.URL.Query().Get("as_of")
	if asOfStr == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_parameter",
			"as_of query parameter is required (RFC3339 timestamp)", 0)
		return
	}
	asOf, err := time.Parse(time.RFC3339, asOfStr)
	if err != nil {
		// Try RFC3339Nano as well.
		asOf, err = time.Parse(time.RFC3339Nano, asOfStr)
		if err != nil {
			d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_timestamp",
				"as_of must be a valid RFC3339 timestamp", 0)
			return
		}
	}

	limit := 50
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if n, parseErr := strconv.Atoi(ls); parseErr == nil && n > 0 {
			limit = n
		}
	}
	offset := 0
	if os := r.URL.Query().Get("offset"); os != "" {
		if n, parseErr := strconv.Atoi(os); parseErr == nil && n >= 0 {
			offset = n
		}
	}

	result, err := ttq.QueryTimeTravel(destination.TimeTravelParams{
		AsOf:        asOf,
		Namespace:   r.URL.Query().Get("namespace"),
		Subject:     r.URL.Query().Get("subject"),
		Destination: r.URL.Query().Get("destination"),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		d.logger.Error("daemon: time-travel query failed",
			"component", "daemon",
			"error", err,
		)
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"time-travel query failed", 0)
		return
	}

	d.writeJSON(w, http.StatusOK, map[string]interface{}{
		"records":     result.Records,
		"has_more":    result.HasMore,
		"next_cursor": result.NextCursor,
		"as_of":       asOf.Format(time.RFC3339Nano),
	})
}

// ---------------------------------------------------------------------------
// Pipeline visualization SSE
// ---------------------------------------------------------------------------

// handleVizEvents serves GET /api/viz/events as a Server-Sent Events stream.
// Admin auth required (via middleware). Streams live pipeline visualization
// events to connected dashboard clients.
//
// Reference: Tech Spec Section 12, Section 13.2.
func (d *Daemon) handleVizEvents(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/viz/events").Inc()

	flusher, ok := w.(http.Flusher)
	if !ok {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"streaming not supported", 0)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, unsub := d.vizPipe.Subscribe()
	defer unsub()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-ch:
			data, err := vizpipe.MarshalSSE(e)
			if err != nil {
				continue
			}
			if _, err := w.Write(data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleVizEventsWithQueryAuth wraps handleVizEvents with auth that accepts
// the admin token from either the Authorization header OR ?token= query param.
// EventSource cannot send custom headers, so SSE clients pass the token as a
// query parameter.
// Reference: dashboard-contract.md Authentication section.
func (d *Daemon) handleVizEventsWithQueryAuth(w http.ResponseWriter, r *http.Request) {
	// Try standard Authorization header first.
	result, ok := d.authenticate(r)
	if !ok || !result.isAdmin {
		// Fall back to ?token= query param.
		token := r.URL.Query().Get("token")
		if token == "" {
			d.emitAuthFailure(r, "missing")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
				"admin token required (header or ?token= query param)", 0)
			return
		}
		cfg := d.getConfig()
		if subtle.ConstantTimeCompare([]byte(token), cfg.ResolvedAdminKey) != 1 {
			d.emitAuthFailure(r, "invalid_query_token")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
				"invalid admin token", 0)
			return
		}
	}
	d.emitAdminAccess(r)
	d.handleVizEvents(w, r)
}

// ---------------------------------------------------------------------------
// Event sink helpers
// ---------------------------------------------------------------------------

// emitWriteEvent sends a memory_written event to the event sink (if enabled).
// Non-blocking — NEVER blocks the write path.
// Reference: Tech Spec Section 10.1.
func (d *Daemon) emitWriteEvent(entry wal.Entry, payload json.RawMessage) {
	if d.eventSink == nil {
		return
	}
	d.eventSink.Emit(eventsink.Event{
		EventType:   "memory_written",
		PayloadID:   entry.PayloadID,
		Source:      entry.Source,
		Subject:     entry.Subject,
		Destination: entry.Destination,
		Timestamp:   entry.Timestamp,
		ActorType:   entry.ActorType,
		ActorID:     entry.ActorID,
		Content:     payload,
	})
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

// ---------------------------------------------------------------------------
// Reliability Demo — POST /api/demo/reliability
// ---------------------------------------------------------------------------

// handleDemoReliability runs the reliability demo in-process (no SIGKILL).
// It writes 50 memories, queries them back, and asserts 50 present with 0
// duplicates. Returns JSON result. Admin-only.
//
// Reference: Tech Spec Section 12, Section 13.3, Phase R-26.
func (d *Daemon) handleDemoReliability(w http.ResponseWriter, r *http.Request) {
	cfg := d.getConfig()

	// Resolve source and API key for the demo writes.
	// Use the first available source with write permission, or "default".
	var demoSource string
	var demoAPIKey string
	for _, src := range cfg.Sources {
		if src.CanWrite {
			demoSource = src.Name
			if key, ok := cfg.ResolvedSourceKeys[src.Name]; ok {
				demoAPIKey = string(key)
			}
			break
		}
	}
	if demoSource == "" || demoAPIKey == "" {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "demo_no_source",
			"no writable source with an API key is configured", 0)
		return
	}

	// Determine destination — use the first source's target_dest.
	demoDestination := "sqlite"
	for _, src := range cfg.Sources {
		if src.Name == demoSource && src.TargetDest != "" {
			demoDestination = src.TargetDest
			break
		}
	}

	// Build the daemon URL from bind address.
	demoURL := fmt.Sprintf("http://%s:%d", cfg.Daemon.Bind, cfg.Daemon.Port)

	result, err := demo.RunInProcess(demoURL, demoSource, demoDestination, demoAPIKey, string(cfg.ResolvedAdminKey), d.logger)
	if err != nil {
		d.logger.Error("demo: reliability demo failed",
			"component", "daemon",
			"error", err,
		)
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "demo_failed",
			fmt.Sprintf("reliability demo failed: %v", err), 0)
		return
	}

	d.writeJSON(w, http.StatusOK, result)
}

// handleShutdown serves POST /api/shutdown — triggers graceful daemon shutdown.
// Returns 202 Accepted immediately; the actual shutdown fires after a brief
// delay so the HTTP response is delivered to the client.
func (d *Daemon) handleShutdown(w http.ResponseWriter, r *http.Request) {
	d.metrics.AdminCallsTotal.WithLabelValues("/api/shutdown").Inc()
	d.logger.Info("daemon: shutdown requested via API",
		"component", "daemon",
	)
	d.writeJSON(w, http.StatusAccepted, map[string]string{
		"status": "shutting_down",
	})
	// Trigger shutdown after a brief delay so the 202 response is flushed.
	go func() {
		time.Sleep(75 * time.Millisecond)
		d.RequestShutdown()
	}()
}

// handleVerify returns a cryptographic proof bundle for the given memory_id.
// The bundle contains the memory, its signature (if signed), the audit chain
// from genesis, and the daemon's public key — everything needed for
// standalone offline verification.
//
// GET /verify/{memory_id}  (admin token required)
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.6.
func (d *Daemon) handleVerify(w http.ResponseWriter, r *http.Request) {
	memoryID := chi.URLParam(r, "memory_id")
	if memoryID == "" {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_memory_id", "memory_id path parameter is required", 0)
		return
	}

	// Query the destination for the memory.
	q, ok := d.querier.(destination.Querier)
	if !ok || q == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "no_querier", "destination does not support queries", 0)
		return
	}

	result, err := q.Query(destination.QueryParams{
		Limit: 200,
	})
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "query_failed", "failed to query destination: "+err.Error(), 0)
		return
	}

	// Find the specific memory by payload_id.
	var memory *destination.TranslatedPayload
	for i := range result.Records {
		if result.Records[i].PayloadID == memoryID {
			memory = &result.Records[i]
			break
		}
	}

	if memory == nil {
		d.writeErrorResponse(w, r, http.StatusNotFound, "memory_not_found", "no memory found with payload_id "+memoryID, 0)
		return
	}

	// Build proof bundle.
	bundle := provenance.ProofBundle{
		Version: 1,
		Memory: provenance.ProofMemory{
			PayloadID:      memory.PayloadID,
			Source:         memory.Source,
			Subject:        memory.Subject,
			Content:        memory.Content,
			Timestamp:      memory.Timestamp.UTC().Format(time.RFC3339Nano),
			IdempotencyKey: memory.IdempotencyKey,
			ContentHash:    provenance.ContentHash(memory.Content),
		},
		Signature:    memory.Signature,
		SignatureAlg: memory.SignatureAlg,
		SourcePubKey: d.sourcePublicKeyHex(memory.Source),
		SigningKeyID: memory.SigningKeyID,
		GeneratedAt:  time.Now().UTC(),
	}

	// Include daemon pubkey and genesis entry if available.
	if d.daemonKeyPair != nil {
		bundle.DaemonPubKey = hex.EncodeToString(d.daemonKeyPair.PublicKey)
	}

	d.writeJSON(w, http.StatusOK, bundle)
}

// handleProve creates a cryptographic attestation for a query and its results.
// The daemon signs a proof that the query produced the exact result set.
//
// POST /api/prove  (admin token required)
// Body: {"query": {...}, "destination": "..."}
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.9.
func (d *Daemon) handleProve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Query       json.RawMessage `json:"query"`
		Destination string          `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "invalid_body", "cannot parse request body: "+err.Error(), 0)
		return
	}
	if len(body.Query) == 0 {
		d.writeErrorResponse(w, r, http.StatusBadRequest, "missing_query", "query field is required", 0)
		return
	}

	if d.daemonKeyPair == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "no_daemon_key",
			"daemon Ed25519 key not available — cannot sign attestation", 0)
		return
	}

	q, ok := d.querier.(destination.Querier)
	if !ok || q == nil {
		d.writeErrorResponse(w, r, http.StatusServiceUnavailable, "no_querier",
			"destination does not support queries", 0)
		return
	}

	// Extract query text from the JSON body for destination query.
	var qParams struct {
		Q     string `json:"q"`
		Limit int    `json:"limit"`
	}
	_ = json.Unmarshal(body.Query, &qParams)
	if qParams.Limit <= 0 {
		qParams.Limit = 50
	}

	result, err := q.Query(destination.QueryParams{
		Q:     qParams.Q,
		Limit: qParams.Limit,
	})
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "query_failed",
			"failed to query destination: "+err.Error(), 0)
		return
	}

	// Serialize each result record for hashing.
	resultPayloads := make([][]byte, len(result.Records))
	for i, rec := range result.Records {
		b, mErr := json.Marshal(rec)
		if mErr != nil {
			d.writeErrorResponse(w, r, http.StatusInternalServerError, "marshal_failed",
				"failed to marshal result record", 0)
			return
		}
		resultPayloads[i] = b
	}

	att, err := provenance.BuildQueryAttestation(body.Query, resultPayloads, d.daemonKeyPair)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "attestation_failed",
			"failed to build query attestation: "+err.Error(), 0)
		return
	}

	d.writeJSON(w, http.StatusOK, att)
}

func (d *Daemon) handleMemoryHealth(w http.ResponseWriter, r *http.Request) {
	var db *sql.DB
	if d.registryStore != nil {
		db = d.registryStore.DB()
	}
	h, err := health.CalculateMemoryHealth(db)
	if err != nil {
		d.writeErrorResponse(w, r, http.StatusInternalServerError, "internal_error",
			"failed to calculate memory health: "+err.Error(), 0)
		return
	}
	d.writeJSON(w, http.StatusOK, h)
}

func (d *Daemon) boostSubscribedResults(ctx context.Context, records []destination.TranslatedPayload, subs []*subscribe.Subscription) {
	if d.subscribeMatcher == nil || len(subs) == 0 || len(records) <= 1 {
		return
	}

	boosted := make([]bool, len(records))
	for i, rec := range records {
		for _, sub := range subs {
			filterVec, err := d.subscribeMatcher.GetFilterEmbedding(ctx, sub)
			if err != nil || len(filterVec) == 0 || len(rec.Embedding) == 0 {
				continue
			}
			sim := subscribe.CosineSimilarity(filterVec, rec.Embedding)
			if sim >= 0.65 {
				boosted[i] = true
				break
			}
		}
	}

	var top, rest []destination.TranslatedPayload
	for i, rec := range records {
		if boosted[i] {
			top = append(top, rec)
		} else {
			rest = append(rest, rec)
		}
	}
	copy(records, append(top, rest...))
}
