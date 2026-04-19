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

package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
)

// handleToken handles POST /oauth/token.
// It validates the authorization code, verifies PKCE, and issues a JWT.
func (s *OAuthServer) handleToken(w http.ResponseWriter, r *http.Request) {
	setOAuthCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeTokenError(w, http.StatusMethodNotAllowed, "invalid_request", "method not allowed")
		return
	}

	if err := r.ParseForm(); err != nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}

	grantType := r.FormValue("grant_type")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	code := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")

	// Validate grant_type.
	if grantType != "authorization_code" {
		writeTokenError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code")
		return
	}

	// Validate required fields.
	if code == "" || clientID == "" || codeVerifier == "" {
		writeTokenError(w, http.StatusBadRequest, "invalid_request", "missing required parameter")
		return
	}

	// Consume the auth code (single use — deleted from store before JWT is issued).
	ac := s.codeStore.Consume(code)
	if ac == nil {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "invalid, expired, or already used authorization code")
		return
	}

	// Validate client_id matches the code's stored client_id.
	if subtle.ConstantTimeCompare([]byte(ac.ClientID), []byte(clientID)) != 1 {
		writeTokenError(w, http.StatusUnauthorized, "invalid_client", "client_id mismatch")
		return
	}

	// Validate redirect_uri matches exactly.
	if subtle.ConstantTimeCompare([]byte(ac.RedirectURI), []byte(redirectURI)) != 1 {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}

	// PKCE verification — subtle.ConstantTimeCompare, NEVER ==.
	hash := sha256.Sum256([]byte(codeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	if subtle.ConstantTimeCompare([]byte(challenge), []byte(ac.CodeChallenge)) != 1 {
		writeTokenError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}

	// Issue JWT access token.
	tokenStr, err := SignJWT(
		s.privateKey,
		s.config.IssuerURL,
		clientID,
		ac.Scope,
		ac.SourceName,
		s.config.AccessTokenTTL,
	)
	if err != nil {
		s.logger.Error("oauth: issue JWT", "error", err)
		writeTokenError(w, http.StatusInternalServerError, "server_error", "failed to issue token")
		return
	}

	// Token response.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(tokenResponse{
		AccessToken: tokenStr,
		TokenType:   "Bearer",
		ExpiresIn:   int(s.config.AccessTokenTTL.Seconds()),
	})
}

// tokenResponse is the JSON response for a successful token exchange.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// writeTokenError writes a standard OAuth token error response.
func writeTokenError(w http.ResponseWriter, status int, errCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":             errCode,
		"error_description": description,
	})
}
