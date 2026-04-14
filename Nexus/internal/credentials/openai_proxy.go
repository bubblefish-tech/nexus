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

package credentials

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	openAIBaseURL = "https://api.openai.com"
	// maxRequestBodyBytes limits the request body to 10 MB to prevent abuse.
	maxRequestBodyBytes = 10 * 1024 * 1024
)

// OpenAIProxy handles POST /v1/chat/completions by validating the synthetic
// key, substituting the real provider key, and proxying to OpenAI.
//
// SECURITY INVARIANTS:
//   - The real API key appears ONLY in the upstream Authorization header.
//   - Error messages from upstream are sanitized — any response containing the
//     real key in error text is replaced with a generic error.
//   - The real key is NEVER logged, even at DEBUG level.
//   - On panic recovery, the real key is not captured in the stack trace
//     because it is a local variable, not a struct field.
type OpenAIProxy struct {
	gateway    *Gateway
	logger     *slog.Logger
	httpClient *http.Client
	auditFunc  AuditFunc
}

// AuditFunc is called after each proxied request for WAL audit logging.
// SECURITY: tokens_in/tokens_out come from the upstream response, not from
// the request body. The synthetic_key_prefix is safe to log; the real key
// MUST NOT be passed to this function.
type AuditFunc func(entry AuditEntry)

// AuditEntry contains the fields logged to the audit WAL for each proxied request.
type AuditEntry struct {
	AgentID           string        `json:"agent_id"`
	SyntheticKeyPrefix string       `json:"synthetic_key_prefix"`
	Provider          Provider      `json:"provider"`
	Model             string        `json:"model"`
	TokensIn          int           `json:"tokens_in"`
	TokensOut         int           `json:"tokens_out"`
	Latency           time.Duration `json:"latency_ms"`
	StatusCode        int           `json:"status_code"`
}

// NewOpenAIProxy creates a proxy handler for OpenAI-compatible endpoints.
func NewOpenAIProxy(gateway *Gateway, logger *slog.Logger, auditFunc AuditFunc) *OpenAIProxy {
	return &OpenAIProxy{
		gateway: gateway,
		logger:  logger,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // LLM requests can be slow
		},
		auditFunc: auditFunc,
	}
}

// ServeHTTP implements http.Handler for POST /v1/chat/completions.
func (p *OpenAIProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if r.Method != http.MethodPost {
		writeProxyError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}

	// Extract synthetic bearer token.
	bearerToken := extractBearer(r)
	if bearerToken == "" {
		writeProxyError(w, http.StatusUnauthorized, "missing_auth", "Authorization: Bearer <token> required")
		return
	}

	// Validate synthetic key (constant-time prefix comparison).
	result, err := p.gateway.Validate(bearerToken)
	if err != nil {
		p.logger.Warn("credential gateway: invalid synthetic key",
			"prefix", SanitizeKeyForLog(bearerToken),
		)
		writeProxyError(w, http.StatusUnauthorized, "invalid_key", "invalid API key")
		return
	}

	if result.Provider != ProviderOpenAI {
		writeProxyError(w, http.StatusBadRequest, "provider_mismatch",
			"this endpoint requires an OpenAI synthetic key")
		return
	}

	// Rate limit check — do NOT reveal the configured limit.
	allowed, retryAfter := p.gateway.CheckRateLimit(result.SyntheticPrefix, result.RateLimitRPM)
	if !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		writeProxyError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
		return
	}

	// Read and parse request body to extract model for allowlist check.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		writeProxyError(w, http.StatusBadRequest, "read_body", "failed to read request body")
		return
	}

	var reqPayload struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &reqPayload); err != nil {
		writeProxyError(w, http.StatusBadRequest, "invalid_json", "invalid JSON in request body")
		return
	}

	// Model allowlist enforcement.
	if err := CheckModelAllowed(result.AllowedModels, reqPayload.Model); err != nil {
		writeProxyError(w, http.StatusForbidden, "model_denied", err.Error())
		return
	}

	// Resolve real key ONLY at the point of dispatch.
	// SECURITY: realKey is a local variable. It is NOT stored in any struct,
	// NOT logged, NOT included in error messages.
	realKey, err := ResolveRealKey(result.RealKeyRef, p.logger)
	if err != nil {
		// Log the resolution failure (which is safe — it logs the ref format,
		// not the resolved value).
		p.logger.Error("credential gateway: resolve real key failed",
			"provider", result.Provider,
			"error", err,
		)
		writeProxyError(w, http.StatusBadGateway, "upstream_config", "upstream provider configuration error")
		return
	}

	// Build upstream request.
	upstreamURL := openAIBaseURL + "/v1/chat/completions"
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, strings.NewReader(string(body)))
	if err != nil {
		writeProxyError(w, http.StatusInternalServerError, "internal", "failed to create upstream request")
		return
	}

	// Set headers. Authorization carries the real key — this is the ONLY
	// place the real key is used.
	upReq.Header.Set("Authorization", "Bearer "+realKey)
	upReq.Header.Set("Content-Type", "application/json")
	// Clear realKey from this scope as soon as the header is set.
	// (Go doesn't guarantee zeroing, but this makes intent clear.)
	realKey = ""

	// Extract agent ID from original request for audit.
	agentID := r.Header.Get("X-Agent-ID")

	// Execute upstream request.
	upResp, err := p.httpClient.Do(upReq)
	if err != nil {
		p.logger.Error("credential gateway: upstream request failed",
			"provider", "openai",
			"error", sanitizeUpstreamError(err),
		)
		writeProxyError(w, http.StatusBadGateway, "upstream_error", "upstream provider unavailable")
		return
	}
	defer upResp.Body.Close()

	latency := time.Since(start)

	// Stream or copy response back to client.
	if reqPayload.Stream {
		p.streamResponse(w, upResp, agentID, result.SyntheticPrefix, reqPayload.Model, latency)
		return
	}

	// Non-streaming: read response, extract token counts, return.
	respBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		writeProxyError(w, http.StatusBadGateway, "upstream_read", "failed to read upstream response")
		return
	}

	// Extract token usage for audit (best-effort).
	var usage struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(respBody, &usage)

	// Sanitize upstream response — defense-in-depth against key reference leaks.
	respBody = sanitizeResponseBody(respBody, result.RealKeyRef)

	// Copy upstream headers.
	for k, vv := range upResp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(upResp.StatusCode)
	_, _ = w.Write(respBody)

	// Audit log.
	if p.auditFunc != nil {
		p.auditFunc(AuditEntry{
			AgentID:            agentID,
			SyntheticKeyPrefix: result.SyntheticPrefix,
			Provider:           ProviderOpenAI,
			Model:              reqPayload.Model,
			TokensIn:           usage.Usage.PromptTokens,
			TokensOut:          usage.Usage.CompletionTokens,
			Latency:            latency,
			StatusCode:         upResp.StatusCode,
		})
	}
}

// streamResponse proxies a streaming SSE response from upstream to the client.
func (p *OpenAIProxy) streamResponse(w http.ResponseWriter, upResp *http.Response, agentID, syntheticPrefix, model string, latency time.Duration) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProxyError(w, http.StatusInternalServerError, "stream_unsupported", "streaming not supported")
		return
	}

	// Copy upstream headers.
	for k, vv := range upResp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(upResp.StatusCode)

	buf := make([]byte, 4096)
	for {
		n, readErr := upResp.Body.Read(buf)
		if n > 0 {
			_, writeErr := w.Write(buf[:n])
			if writeErr != nil {
				// Client disconnected — stop proxying.
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			break
		}
	}

	// Audit log for streaming (token counts not available in SSE).
	if p.auditFunc != nil {
		p.auditFunc(AuditEntry{
			AgentID:            agentID,
			SyntheticKeyPrefix: syntheticPrefix,
			Provider:           ProviderOpenAI,
			Model:              model,
			Latency:            latency,
			StatusCode:         upResp.StatusCode,
		})
	}
}

// extractBearer extracts the bearer token from the Authorization header.
func extractBearer(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

// writeProxyError writes a JSON error response matching the Nexus error format.
// SECURITY: msg MUST NOT contain any real API key material.
func writeProxyError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]interface{}{
		"error":   code,
		"message": msg,
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// sanitizeUpstreamError removes any potential key material from upstream
// error strings. The upstream URL is safe to include; query parameters and
// auth headers are not present in Go's http.Client error strings.
func sanitizeUpstreamError(err error) string {
	s := err.Error()
	// Go's http.Client errors don't include Authorization headers,
	// but defense-in-depth: strip anything after "Bearer " if present.
	if idx := strings.Index(s, "Bearer "); idx >= 0 {
		return s[:idx] + "Bearer [REDACTED]"
	}
	return s
}
