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

// Package oauth implements an OAuth 2.1 authorization server for BubbleFish
// Nexus, enabling ChatGPT and other OAuth-only MCP clients to connect.
//
// The implementation is additive — all existing Bearer token auth
// (bfn_mcp_, bfn_data_, bfn_admin_) is preserved unchanged.
//
// Reference: Post-Build Add-On Update Technical Specification Section 3.
package oauth

import (
	"crypto/rsa"
	"log/slog"
	"net/http"
	"time"
)

// OAuthServer is the OAuth 2.1 authorization server for BubbleFish Nexus.
// It manages RSA key pairs, authorization codes, and JWT access tokens.
type OAuthServer struct {
	config     OAuthConfig
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	codeStore  *CodeStore
	logger     *slog.Logger
}

// OAuthConfig holds configuration for the OAuth 2.1 server.
type OAuthConfig struct {
	Enabled        bool
	IssuerURL      string
	PrivateKeyFile string
	AccessTokenTTL time.Duration // default 1hr
	AuthCodeTTL    time.Duration // default 5min
	Clients        []OAuthClient
}

// OAuthClient represents a registered OAuth client (e.g., ChatGPT).
type OAuthClient struct {
	ClientID        string
	ClientName      string
	RedirectURIs    []string
	OAuthSourceName string // maps to sources/*.toml
	AllowedScopes   []string
}

// NewOAuthServer creates an OAuthServer with the given config and RSA key pair.
func NewOAuthServer(cfg OAuthConfig, key *rsa.PrivateKey, logger *slog.Logger) *OAuthServer {
	if cfg.AccessTokenTTL == 0 {
		cfg.AccessTokenTTL = time.Hour
	}
	if cfg.AuthCodeTTL == 0 {
		cfg.AuthCodeTTL = 5 * time.Minute
	}
	return &OAuthServer{
		config:     cfg,
		privateKey: key,
		publicKey:  &key.PublicKey,
		codeStore:  NewCodeStore(),
		logger:     logger,
	}
}

// FindClient looks up a registered client by client_id. Returns nil if not found.
func (s *OAuthServer) FindClient(clientID string) *OAuthClient {
	for i := range s.config.Clients {
		if s.config.Clients[i].ClientID == clientID {
			return &s.config.Clients[i]
		}
	}
	return nil
}

// PublicKey returns the RSA public key for JWKS and JWT validation.
func (s *OAuthServer) PublicKey() *rsa.PublicKey {
	return s.publicKey
}

// IssuerURL returns the configured issuer URL.
func (s *OAuthServer) IssuerURL() string {
	return s.config.IssuerURL
}

// RegisterHandlers registers OAuth HTTP endpoints on the given ServeMux.
func (s *OAuthServer) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/.well-known/oauth-authorization-server", s.handleMetadata)
	mux.HandleFunc("/oauth/authorize", s.handleAuthorize)
	mux.HandleFunc("/oauth/authorize/allow", s.handleAllow)
	mux.HandleFunc("/oauth/authorize/deny", s.handleDeny)
	mux.HandleFunc("/oauth/token", s.handleToken)
	mux.HandleFunc("/oauth/jwks", s.handleJWKS)
}

// handleJWKS serves the JSON Web Key Set containing the server's RSA public key.
// No auth required.
func (s *OAuthServer) handleJWKS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := MarshalJWKS(s.publicKey)
	if err != nil {
		s.logger.Error("oauth: marshal JWKS", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(data)
}
