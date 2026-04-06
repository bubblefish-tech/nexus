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
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/BubbleFish-Nexus/internal/config"
	"github.com/BubbleFish-Nexus/internal/securitylog"
)

// contextKey is used for type-safe context values within the daemon package.
type contextKey int

const (
	// ctxSource is the context key for the authenticated *config.Source.
	ctxSource contextKey = iota

	// ctxEffectiveClientIP is the context key for the effective client IP
	// derived from trusted proxy headers or TCP source. Set by
	// loggingMiddleware. Reference: Tech Spec Section 6.3.
	ctxEffectiveClientIP
)

// authResult carries the outcome of authentication for a single request.
type authResult struct {
	source    *config.Source
	isAdmin   bool
	isMCP     bool
	provided  []byte // the raw token bytes — never log
}

// authenticate extracts the Bearer token from the Authorization header and
// performs a constant-time comparison against all known keys (source keys and
// admin key). Returns the matched source (or isAdmin=true) on success.
//
// Invariants:
//   - NEVER uses == for token comparison.
//   - NEVER calls os.Getenv — all keys resolved at startup.
//   - Iterates ALL source keys to avoid timing side-channels from early exit.
//   - Uses configMu.RLock() for hot-reload safety (NEVER Lock on the auth path).
//
// Reference: Tech Spec Section 6.1, Phase 0C Behavioral Contract items 1–6,
// Phase 0D Behavioral Contract item 5.
func (d *Daemon) authenticate(r *http.Request) (authResult, bool) {
	token := extractBearerToken(r)
	if token == "" {
		d.metrics.AuthFailuresTotal.WithLabelValues("unknown").Inc()
		return authResult{}, false
	}
	provided := []byte(token)

	// Snapshot config under RLock. The Config struct is immutable — hot reload
	// only swaps the pointer, never mutates fields in-place. Reading fields
	// from the snapshot after releasing the lock is race-free.
	//
	// INVARIANT: NEVER use Lock() here. Only RLock().
	d.configMu.RLock()
	cfg := d.cfg
	d.configMu.RUnlock()

	// Compare against admin key using constant-time comparison.
	// We must still compare ALL keys (admin, source, MCP) to avoid timing leaks.
	adminMatch := subtle.ConstantTimeCompare(provided, cfg.ResolvedAdminKey) == 1

	// Compare against every source key. Continue through ALL entries so that
	// timing does not reveal which key matched.
	var matchedSource *config.Source
	for _, src := range cfg.Sources {
		key := cfg.ResolvedSourceKeys[src.Name]
		if subtle.ConstantTimeCompare(provided, key) == 1 {
			matchedSource = src
		}
	}

	// Compare against MCP key if configured. Must always run to prevent
	// timing side-channels even when MCP is disabled.
	mcpMatch := len(cfg.ResolvedMCPKey) > 0 &&
		subtle.ConstantTimeCompare(provided, cfg.ResolvedMCPKey) == 1

	if matchedSource != nil {
		return authResult{source: matchedSource, provided: provided}, true
	}
	if adminMatch {
		return authResult{isAdmin: true, provided: provided}, true
	}
	if mcpMatch {
		return authResult{isMCP: true, provided: provided}, true
	}
	d.metrics.AuthFailuresTotal.WithLabelValues("unknown").Inc()
	return authResult{}, false
}

// extractBearerToken parses the Authorization header and returns the Bearer
// token value. Returns "" if the header is absent or not a Bearer scheme.
func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// requireDataToken is an HTTP middleware that requires a valid data-plane
// token (source key). Admin tokens are rejected on data endpoints with
// 401 wrong_token_class. Unauthenticated requests get 401 unauthorized.
//
// On success the authenticated *config.Source is stored in the request context
// under ctxSource.
//
// Reference: Tech Spec Section 6.1, Phase 0C Behavioral Contract item 5.
func (d *Daemon) requireDataToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, ok := d.authenticate(r)
		if !ok {
			// If JWT is enabled, attempt JWT validation before rejecting.
			// Reference: Tech Spec Section 6.6.
			if d.jwtValidator != nil {
				if src := d.authenticateJWT(r); src != nil {
					ctx := setSourceInContext(r.Context(), src)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			d.emitAuthFailure(r, "unknown")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
				"invalid or missing API key", 0)
			return
		}
		if result.isAdmin || result.isMCP {
			// Admin and MCP tokens must not be used on data endpoints.
			tokenClass := "admin"
			if result.isMCP {
				tokenClass = "mcp"
			}
			d.emitAuthFailure(r, tokenClass)
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "wrong_token_class",
				"wrong token class for this endpoint", 0)
			return
		}
		// Embed source in context for downstream handlers.
		ctx := setSourceInContext(r.Context(), result.source)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdminToken is an HTTP middleware that requires the admin token.
// Data-plane tokens are rejected with 401 wrong_token_class.
//
// Reference: Tech Spec Section 6.1, Phase 0C Behavioral Contract item 5.
func (d *Daemon) requireAdminToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, ok := d.authenticate(r)
		if !ok {
			d.emitAuthFailure(r, "unknown")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
				"invalid or missing admin token", 0)
			return
		}
		if !result.isAdmin {
			// Data-plane and MCP tokens must not be used on admin endpoints.
			d.emitAuthFailure(r, "wrong_token_class")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "wrong_token_class",
				"wrong token class for this endpoint", 0)
			return
		}
		// Emit admin_access for all authenticated admin requests.
		d.emitAdminAccess(r)
		next.ServeHTTP(w, r)
	})
}

// setSourceInContext stores the authenticated *config.Source in ctx.
func setSourceInContext(ctx context.Context, src *config.Source) context.Context {
	return context.WithValue(ctx, ctxSource, src)
}

// sourceFromContext retrieves the authenticated *config.Source from ctx.
// Returns nil if not present (should not happen after requireDataToken).
func sourceFromContext(ctx context.Context) *config.Source {
	v := ctx.Value(ctxSource)
	if v == nil {
		return nil
	}
	s, _ := v.(*config.Source)
	return s
}

// effectiveClientIPFromContext retrieves the effective client IP from ctx.
// Returns "" if not present (should not happen after loggingMiddleware).
// Reference: Tech Spec Section 6.3.
func effectiveClientIPFromContext(ctx context.Context) string {
	v := ctx.Value(ctxEffectiveClientIP)
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// authenticateJWT attempts to validate the Bearer token as a JWT and map the
// configured claim to a source. Returns the matched *config.Source or nil if
// JWT validation fails or the claim does not match any known source.
//
// Reference: Tech Spec Section 6.6.
func (d *Daemon) authenticateJWT(r *http.Request) *config.Source {
	token := extractBearerToken(r)
	if token == "" {
		return nil
	}

	result, err := d.jwtValidator.Validate(token)
	if err != nil {
		d.logger.Debug("daemon: JWT validation failed",
			"component", "daemon",
			"error", err,
		)
		d.emitSecurityEvent(securitylog.Event{
			EventType: "auth_failure",
			IP:        effectiveClientIPFromContext(r.Context()),
			Endpoint:  r.URL.Path,
			Details: map[string]interface{}{
				"token_class": "jwt",
				"reason":      err.Error(),
			},
		})
		return nil
	}

	// Map the JWT claim value to a configured source.
	cfg := d.getConfig()
	for _, src := range cfg.Sources {
		if src.Name == result.SourceName {
			return src
		}
	}

	d.logger.Warn("daemon: JWT claim mapped to unknown source",
		"component", "daemon",
		"source", result.SourceName,
	)
	return nil
}
