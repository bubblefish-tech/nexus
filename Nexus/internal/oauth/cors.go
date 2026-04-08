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

import "net/http"

// oauthCORSAllowedHeaders lists all headers OAuth clients may send.
const oauthCORSAllowedHeaders = "Content-Type, Accept, Authorization, X-Requested-With"

// oauthCORSAllowedMethods lists all HTTP methods OAuth endpoints handle.
const oauthCORSAllowedMethods = "GET, POST, OPTIONS"

// setOAuthCORSHeaders writes permissive CORS headers suitable for OAuth
// discovery, token exchange, and JWKS fetches from browser-based clients
// (Claude Web UI, SPAs, etc.). Server-to-server OAuth clients (ChatGPT) are
// unaffected by CORS and will ignore these headers.
//
// The wildcard origin is acceptable because OAuth endpoints return no
// user-specific cookies or credentials; all authentication state is carried
// in the JWT access token and PKCE code_verifier, both of which are proof-of-
// possession rather than ambient credentials.
func setOAuthCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", oauthCORSAllowedMethods)
	w.Header().Set("Access-Control-Allow-Headers", oauthCORSAllowedHeaders)
	w.Header().Set("Access-Control-Max-Age", "86400")
}
