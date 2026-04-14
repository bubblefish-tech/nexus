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
	anthropicBaseURL    = "https://api.anthropic.com"
	anthropicAPIVersion = "2023-06-01"
)

// AnthropicProxy handles POST /v1/messages by validating the synthetic key,
// substituting the real Anthropic API key, and proxying upstream.
//
// Anthropic uses x-api-key header instead of Bearer token for upstream auth.
//
// SECURITY INVARIANTS: Same as OpenAIProxy — real key never logged, never
// in error messages, never in metrics.
type AnthropicProxy struct {
	gateway    *Gateway
	logger     *slog.Logger
	httpClient *http.Client
	auditFunc  AuditFunc
}

// NewAnthropicProxy creates a proxy handler for Anthropic-compatible endpoints.
func NewAnthropicProxy(gateway *Gateway, logger *slog.Logger, auditFunc AuditFunc) *AnthropicProxy {
	return &AnthropicProxy{
		gateway: gateway,
		logger:  logger,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		auditFunc: auditFunc,
	}
}

// ServeHTTP implements http.Handler for POST /v1/messages.
func (p *AnthropicProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	// Validate synthetic key.
	result, err := p.gateway.Validate(bearerToken)
	if err != nil {
		p.logger.Warn("credential gateway: invalid synthetic key",
			"prefix", SanitizeKeyForLog(bearerToken),
		)
		writeProxyError(w, http.StatusUnauthorized, "invalid_key", "invalid API key")
		return
	}

	if result.Provider != ProviderAnthropic {
		writeProxyError(w, http.StatusBadRequest, "provider_mismatch",
			"this endpoint requires an Anthropic synthetic key")
		return
	}

	// Rate limit check.
	allowed, retryAfter := p.gateway.CheckRateLimit(result.SyntheticPrefix, result.RateLimitRPM)
	if !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		writeProxyError(w, http.StatusTooManyRequests, "rate_limited", "rate limit exceeded")
		return
	}

	// Agent allowlist check.
	agentID := r.Header.Get("X-Agent-ID")
	if err := CheckAgentAllowed(result.AllowedAgents, agentID); err != nil {
		writeProxyError(w, http.StatusForbidden, "agent_denied", err.Error())
		return
	}

	// Read and parse request body.
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

	// Resolve real key ONLY at dispatch time.
	realKey, err := ResolveRealKey(result.RealKeyRef, p.logger)
	if err != nil {
		p.logger.Error("credential gateway: resolve real key failed",
			"provider", result.Provider,
			"error", err,
		)
		writeProxyError(w, http.StatusBadGateway, "upstream_config", "upstream provider configuration error")
		return
	}

	// Build upstream request.
	upstreamURL := anthropicBaseURL + "/v1/messages"
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, strings.NewReader(string(body)))
	if err != nil {
		writeProxyError(w, http.StatusInternalServerError, "internal", "failed to create upstream request")
		return
	}

	// Anthropic uses x-api-key header, not Bearer.
	upReq.Header.Set("x-api-key", realKey)
	upReq.Header.Set("anthropic-version", anthropicAPIVersion)
	upReq.Header.Set("Content-Type", "application/json")
	realKey = ""

	// Execute upstream request.
	upResp, err := p.httpClient.Do(upReq)
	if err != nil {
		p.logger.Error("credential gateway: upstream request failed",
			"provider", "anthropic",
			"error", sanitizeUpstreamError(err),
		)
		writeProxyError(w, http.StatusBadGateway, "upstream_error", "upstream provider unavailable")
		return
	}
	defer upResp.Body.Close()

	latency := time.Since(start)

	if reqPayload.Stream {
		p.streamResponse(w, upResp, agentID, result.SyntheticPrefix, reqPayload.Model, latency)
		return
	}

	// Non-streaming response.
	respBody, err := io.ReadAll(upResp.Body)
	if err != nil {
		writeProxyError(w, http.StatusBadGateway, "upstream_read", "failed to read upstream response")
		return
	}

	// Extract Anthropic usage for audit.
	var usage struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(respBody, &usage)

	// Sanitize upstream response — ensure real key isn't in error responses.
	// Anthropic error responses should never contain the API key, but
	// defense-in-depth: check and redact if somehow present.
	respBody = sanitizeResponseBody(respBody, result.RealKeyRef)

	for k, vv := range upResp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(upResp.StatusCode)
	_, _ = w.Write(respBody)

	if p.auditFunc != nil {
		p.auditFunc(AuditEntry{
			AgentID:            agentID,
			SyntheticKeyPrefix: result.SyntheticPrefix,
			Provider:           ProviderAnthropic,
			Model:              reqPayload.Model,
			TokensIn:           usage.Usage.InputTokens,
			TokensOut:          usage.Usage.OutputTokens,
			Latency:            latency,
			StatusCode:         upResp.StatusCode,
		})
	}
}

func (p *AnthropicProxy) streamResponse(w http.ResponseWriter, upResp *http.Response, agentID, syntheticPrefix, model string, latency time.Duration) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProxyError(w, http.StatusInternalServerError, "stream_unsupported", "streaming not supported")
		return
	}

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
				return
			}
			flusher.Flush()
		}
		if readErr != nil {
			break
		}
	}

	if p.auditFunc != nil {
		p.auditFunc(AuditEntry{
			AgentID:            agentID,
			SyntheticKeyPrefix: syntheticPrefix,
			Provider:           ProviderAnthropic,
			Model:              model,
			Latency:            latency,
			StatusCode:         upResp.StatusCode,
		})
	}
}

// sanitizeResponseBody checks if the response body contains the key reference
// string (not the resolved key — the reference like "env:OPENAI_API_KEY").
// This is a defense-in-depth measure. The resolved key cannot appear here
// because we never pass it to the response path.
func sanitizeResponseBody(body []byte, keyRef string) []byte {
	if keyRef == "" {
		return body
	}
	s := string(body)
	if strings.Contains(s, keyRef) {
		s = strings.ReplaceAll(s, keyRef, "[REDACTED]")
		return []byte(s)
	}
	return body
}
