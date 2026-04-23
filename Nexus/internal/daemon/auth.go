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

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/securitylog"
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

	// ctxIsAdmin is the context key for a boolean flag indicating the
	// request was authenticated with the admin token on a data endpoint.
	// Used by debug_stages. Reference: Tech Spec Section 7.3.
	ctxIsAdmin

	// ctxReviewTokenClass stores the review token class: "list" or "read".
	// Set by requireReviewToken middleware.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
	ctxReviewTokenClass
)

// authResult carries the outcome of authentication for a single request.
type authResult struct {
	source          *config.Source
	isAdmin         bool
	isMCP           bool
	isReviewList    bool // bfn_review_list_ token class
	isReviewRead    bool // bfn_review_read_ token class
	provided        []byte // the raw token bytes — never log
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

	// Compare against review tokens. Both comparisons always run.
	// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
	reviewListMatch := len(cfg.ResolvedReviewListKey) > 0 &&
		subtle.ConstantTimeCompare(provided, cfg.ResolvedReviewListKey) == 1
	reviewReadMatch := len(cfg.ResolvedReviewReadKey) > 0 &&
		subtle.ConstantTimeCompare(provided, cfg.ResolvedReviewReadKey) == 1

	if matchedSource != nil {
		return authResult{source: matchedSource, provided: provided}, true
	}
	if adminMatch {
		return authResult{isAdmin: true, provided: provided}, true
	}
	if mcpMatch {
		return authResult{isMCP: true, provided: provided}, true
	}
	if reviewListMatch {
		return authResult{isReviewList: true, provided: provided}, true
	}
	if reviewReadMatch {
		return authResult{isReviewRead: true, provided: provided}, true
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
// token (source key). MCP tokens are rejected on data endpoints with 401
// wrong_token_class. Unauthenticated requests get 401 unauthorized.
//
// Admin tokens are allowed through with a ctxIsAdmin flag in context (no
// source set). This enables admin-only features like debug_stages on read
// endpoints. Reference: Tech Spec Section 7.3.
//
// On success the authenticated *config.Source is stored in the request context
// under ctxSource (nil for admin tokens).
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
		if result.isMCP {
			d.emitAuthFailure(r, "mcp")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "wrong_token_class",
				"wrong token class for this endpoint", 0)
			return
		}
		// Review tokens are scoped to /api/review/* only. Reject on data endpoints.
		// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
		if result.isReviewList || result.isReviewRead {
			d.emitAuthFailure(r, "review")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "wrong_token_class",
				"wrong token class for this endpoint", 0)
			return
		}
		if result.isAdmin {
			// Admin tokens are allowed on data endpoints with an admin flag.
			// Handlers that need a source must check for nil and handle
			// accordingly. Reference: Tech Spec Section 7.3.
			ctx := context.WithValue(r.Context(), ctxIsAdmin, true)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		// Embed source in context for downstream handlers.
		ctx := setSourceInContext(r.Context(), result.source)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdminToken is an HTTP middleware that requires the admin token.
// Data-plane, MCP, and review tokens are rejected with 401 wrong_token_class.
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
			// Data-plane, MCP, and review tokens must not be used on admin endpoints.
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

// isAdminFromContext returns true if the request was authenticated with the
// admin token on a data endpoint. Reference: Tech Spec Section 7.3.
func isAdminFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(ctxIsAdmin).(bool)
	return v
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

// requireReviewListToken is a middleware that allows only bfn_review_list_
// tokens. Any other token class receives 401 wrong_token_class.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
func (d *Daemon) requireReviewListToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, ok := d.authenticate(r)
		if !ok {
			d.emitAuthFailure(r, "unknown")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
				"invalid or missing review token", 0)
			return
		}
		if !result.isReviewList {
			d.emitAuthFailure(r, "wrong_token_class")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "wrong_token_class",
				"wrong token class for this endpoint", 0)
			return
		}
		ctx := context.WithValue(r.Context(), ctxReviewTokenClass, "list")
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireReviewReadToken is a middleware that allows bfn_review_read_ tokens
// (and also bfn_review_list_ tokens for list access). Any other token class
// receives 401 wrong_token_class.
//
// Reference: v0.1.3 Build Plan Phase 2 Subtask 2.3.
func (d *Daemon) requireReviewReadToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, ok := d.authenticate(r)
		if !ok {
			d.emitAuthFailure(r, "unknown")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "unauthorized",
				"invalid or missing review token", 0)
			return
		}
		if !result.isReviewRead && !result.isReviewList {
			d.emitAuthFailure(r, "wrong_token_class")
			d.writeErrorResponse(w, r, http.StatusUnauthorized, "wrong_token_class",
				"wrong token class for this endpoint", 0)
			return
		}
		class := "read"
		if result.isReviewList {
			class = "list"
		}
		ctx := context.WithValue(r.Context(), ctxReviewTokenClass, class)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
