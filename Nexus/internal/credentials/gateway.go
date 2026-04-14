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

// Package credentials implements the credential gateway for the BubbleFish
// Nexus Agent Gateway. Agents authenticate with synthetic API keys; Nexus
// substitutes real provider credentials at the upstream call. Real keys NEVER
// appear in logs, audit entries, error messages, or Prometheus metrics.
package credentials

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
)

// Provider identifies an upstream AI provider.
type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
)

// Mapping describes a synthetic-to-real key mapping loaded from config.
type Mapping struct {
	SyntheticPrefix  string
	Provider         Provider
	RealKeyRef       string   // "env:VAR" or "file:/path" — NEVER resolved at config time
	AllowedModels    []string // empty = allow all
	RateLimitRPM     int
	RateLimitTokens  int64 // tokens per day; 0 = unlimited
}

// Gateway performs synthetic key validation, real key resolution, model
// allowlist enforcement, and per-synthetic-key rate limiting.
//
// SECURITY INVARIANT: The resolved real key is returned ONLY to the proxy
// function that makes the upstream HTTP call. It MUST NOT be logged, stored
// in structs that are serialized, included in error messages, or passed to
// any Prometheus metric label.
type Gateway struct {
	mu       sync.RWMutex
	mappings []Mapping
	logger   *slog.Logger
	rl       *keyRateLimiter
}

// NewGateway creates a credential gateway from the provided mappings.
// Mappings are sorted by prefix length descending so that longer prefixes
// match before shorter ones (e.g., "bfn_sk_anth_" before "bfn_sk_").
func NewGateway(mappings []Mapping, logger *slog.Logger) *Gateway {
	sorted := make([]Mapping, len(mappings))
	copy(sorted, mappings)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].SyntheticPrefix) > len(sorted[j].SyntheticPrefix)
	})
	return &Gateway{
		mappings: sorted,
		logger:   logger,
		rl:       newKeyRateLimiter(),
	}
}

// ReloadMappings atomically replaces the credential mappings (hot reload).
// Mappings are re-sorted by prefix length descending.
func (g *Gateway) ReloadMappings(mappings []Mapping) {
	sorted := make([]Mapping, len(mappings))
	copy(sorted, mappings)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].SyntheticPrefix) > len(sorted[j].SyntheticPrefix)
	})
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mappings = sorted
}

// ValidateResult is returned by Validate on success.
type ValidateResult struct {
	Provider    Provider
	RealKeyRef  string   // still a reference — resolve only at call time
	AllowedModels []string
	SyntheticPrefix string
	RateLimitRPM int
}

// Validate checks a bearer token against all configured synthetic key prefixes.
// Uses constant-time comparison on the prefix portion to prevent timing attacks.
//
// Returns (nil, err) if the key doesn't match any mapping.
//
// SECURITY: This function does NOT resolve the real key. The caller must
// call ResolveRealKey separately, and only at the point of upstream dispatch.
func (g *Gateway) Validate(bearerToken string) (*ValidateResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	for _, m := range g.mappings {
		prefix := m.SyntheticPrefix
		if len(bearerToken) < len(prefix) {
			continue
		}

		// Constant-time compare on the prefix portion.
		// Both slices are the same length (len(prefix)), so ConstantTimeCompare
		// won't short-circuit on length mismatch.
		tokenPrefix := bearerToken[:len(prefix)]
		if subtle.ConstantTimeCompare([]byte(tokenPrefix), []byte(prefix)) == 1 {
			return &ValidateResult{
				Provider:        m.Provider,
				RealKeyRef:      m.RealKeyRef,
				AllowedModels:   m.AllowedModels,
				SyntheticPrefix: m.SyntheticPrefix,
				RateLimitRPM:    m.RateLimitRPM,
			}, nil
		}
	}

	return nil, fmt.Errorf("invalid synthetic key")
}

// CheckModelAllowed returns nil if the model is in the allowlist,
// or an error with the denial reason.
func CheckModelAllowed(allowed []string, requested string) error {
	if len(allowed) == 0 {
		return nil // empty allowlist = all models allowed
	}
	for _, m := range allowed {
		if m == requested {
			return nil
		}
	}
	return fmt.Errorf("model %q not in allowed list", requested)
}

// ResolveRealKey resolves a key reference (env:/file:/literal) to its
// plaintext value. The result MUST be used only for the upstream HTTP call
// and MUST NOT be logged, stored, or included in error messages.
//
// SECURITY: Callers MUST NOT wrap the returned error with additional context
// that could leak the key value. The error from config.ResolveEnv is safe
// because it only includes the reference format, not the resolved value.
func ResolveRealKey(ref string, logger *slog.Logger) (string, error) {
	resolved, err := config.ResolveEnv(ref, logger)
	if err != nil {
		return "", fmt.Errorf("credential gateway: resolve key reference: %w", err)
	}
	if resolved == "" {
		return "", fmt.Errorf("credential gateway: key reference resolved to empty value")
	}
	return resolved, nil
}

// CheckRateLimit checks the per-synthetic-key rate limit. Returns (allowed, retryAfterSeconds).
func (g *Gateway) CheckRateLimit(syntheticPrefix string, rpm int) (bool, int) {
	if rpm <= 0 {
		return true, 0
	}
	return g.rl.Allow(syntheticPrefix, rpm)
}

// keyRateLimiter is a fixed-window rate limiter keyed by synthetic key prefix.
// The map is bounded by the number of credential mappings (typically <10).
type keyRateLimiter struct {
	mu      sync.Mutex
	windows map[string]*keyRateWindow
}

type keyRateWindow struct {
	count       int
	windowStart time.Time
	rpm         int
}

func newKeyRateLimiter() *keyRateLimiter {
	return &keyRateLimiter{
		windows: make(map[string]*keyRateWindow),
	}
}

func (rl *keyRateLimiter) Allow(key string, rpm int) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	w, ok := rl.windows[key]
	if !ok {
		rl.windows[key] = &keyRateWindow{
			count:       1,
			windowStart: now,
			rpm:         rpm,
		}
		return true, 0
	}

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

// SanitizeKeyForLog returns a redacted representation of a key suitable for
// logging. Shows only the prefix portion, never the secret suffix.
func SanitizeKeyForLog(key string) string {
	// Show at most 12 characters of the prefix.
	if len(key) <= 12 {
		return strings.Repeat("*", len(key))
	}
	return key[:12] + "***"
}
