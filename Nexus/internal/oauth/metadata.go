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
	"encoding/json"
	"net/http"
)

// serverMetadata represents the OAuth 2.0 Authorization Server Metadata
// document defined by RFC 8414.
type serverMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	JWKSURI                           string   `json:"jwks_uri"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

// protectedResourceMetadata represents the OAuth 2.0 Protected Resource
// Metadata document defined by RFC 9728.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
	ResourceDocumentation  string   `json:"resource_documentation"`
}

// handleProtectedResource serves the OAuth 2.0 Protected Resource Metadata
// document at /.well-known/oauth-protected-resource (RFC 9728).
// Required by ChatGPT's MCP connector for OAuth discovery.
// No authentication is required.
func (s *OAuthServer) handleProtectedResource(w http.ResponseWriter, r *http.Request) {
	setOAuthCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	issuer := s.config.IssuerURL
	meta := protectedResourceMetadata{
		Resource:               issuer,
		AuthorizationServers:   []string{issuer},
		BearerMethodsSupported: []string{"header"},
		ResourceDocumentation:  "https://github.com/bubblefish-tech/nexus",
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		s.logger.Error("oauth: encode protected resource metadata", "error", err)
	}
}

// handleMetadata serves the OAuth 2.0 Authorization Server Metadata document
// at /.well-known/oauth-authorization-server (RFC 8414).
// No authentication is required.
func (s *OAuthServer) handleMetadata(w http.ResponseWriter, r *http.Request) {
	setOAuthCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	issuer := s.config.IssuerURL
	meta := serverMetadata{
		Issuer:                            issuer,
		AuthorizationEndpoint:             issuer + "/oauth/authorize",
		TokenEndpoint:                     issuer + "/oauth/token",
		JWKSURI:                           issuer + "/oauth/jwks",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported:                   []string{"mcp", "openid"},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		s.logger.Error("oauth: encode metadata", "error", err)
	}
}
