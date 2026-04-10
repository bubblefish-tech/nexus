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
	"html/template"
	"net/http"
	"net/url"
	"time"

	"github.com/bubblefish-tech/nexus/internal/version"
)

// consentPageTemplate is the self-contained HTML/template-escaped consent page.
// All user-controlled values are HTML-escaped automatically by html/template.
// No external CSS, JS, or font dependencies. All styles are inline.
var consentPageTemplate = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Authorize {{.ClientName}} — BubbleFish Nexus</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;
background:#f0f4f8;display:flex;justify-content:center;align-items:center;
min-height:100vh;padding:1rem}
.card{background:#fff;border-radius:12px;box-shadow:0 4px 24px rgba(0,0,0,.1);
max-width:420px;width:100%;padding:2.5rem 2rem}
.logo{color:#1F4E79;font-size:1.5rem;font-weight:700;text-align:center;
margin-bottom:.25rem}
.tagline{color:#6b7280;font-size:.85rem;text-align:center;margin-bottom:2rem}
h1{font-size:1.25rem;color:#111827;text-align:center;margin-bottom:1rem}
.perm{background:#f9fafb;border:1px solid #e5e7eb;border-radius:8px;
padding:1rem;margin-bottom:1.5rem;text-align:center;color:#374151;
font-size:.95rem;line-height:1.5}
.buttons{display:flex;gap:.75rem}
.buttons form{flex:1}
.btn{width:100%;padding:.75rem;border:none;border-radius:8px;font-size:1rem;
font-weight:600;cursor:pointer;transition:background .15s}
.btn-allow{background:#1F4E79;color:#fff}
.btn-allow:hover{background:#163a5c}
.btn-deny{background:#e5e7eb;color:#374151}
.btn-deny:hover{background:#d1d5db}
.footer{margin-top:1.5rem;text-align:center;color:#9ca3af;font-size:.75rem}
</style>
</head>
<body>
<div class="card">
<div class="logo">BubbleFish Nexus</div>
<div class="tagline">AI Memory Gateway</div>
<h1>Authorize {{.ClientName}}</h1>
<div class="perm">
BubbleFish Nexus will allow <strong>{{.ClientName}}</strong> to read and write your AI memories.
</div>
<div class="buttons">
<form method="post" action="/oauth/authorize/allow">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
<input type="hidden" name="scope" value="{{.Scope}}">
<input type="hidden" name="state" value="{{.State}}">
<button type="submit" class="btn btn-allow">Allow</button>
</form>
<form method="post" action="/oauth/authorize/deny">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="state" value="{{.State}}">
<button type="submit" class="btn btn-deny">Deny</button>
</form>
</div>
<div class="footer">BubbleFish Nexus {{.Version}}</div>
</div>
</body>
</html>`))

// consentPageData holds all values rendered into the consent page template.
// All string fields are HTML-escaped automatically by html/template.
type consentPageData struct {
	ClientName    string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	Scope         string
	State         string
	Version       string
}

// handleAuthorize handles GET /oauth/authorize.
// It validates all required parameters and renders the consent page.
func (s *OAuthServer) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	setOAuthCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	responseType := q.Get("response_type")
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	scope := q.Get("scope")
	state := q.Get("state")

	// Validate response_type.
	if responseType != "code" {
		writeAuthorizeError(w, http.StatusBadRequest, "unsupported_response_type", "response_type must be \"code\"")
		return
	}

	// Validate client_id — unknown client → 400, never redirect.
	client := s.FindClient(clientID)
	if client == nil {
		writeAuthorizeError(w, http.StatusBadRequest, "invalid_client", "unknown client_id")
		return
	}

	// Validate redirect_uri — mismatch → 400, NEVER redirect.
	if !clientHasRedirectURI(client, redirectURI) {
		writeAuthorizeError(w, http.StatusBadRequest, "invalid_request", "redirect_uri mismatch")
		return
	}

	// From here, redirect_uri is validated — errors redirect with error params.

	// Validate state is present.
	if state == "" {
		redirectWithError(w, r, redirectURI, "invalid_request", "state is required", "")
		return
	}

	// Validate code_challenge is present.
	if codeChallenge == "" {
		redirectWithError(w, r, redirectURI, "invalid_request", "code_challenge is required", state)
		return
	}

	// Reject code_challenge_method=plain — S256 only.
	if codeChallengeMethod == "plain" {
		redirectWithError(w, r, redirectURI, "invalid_request", "code_challenge_method \"plain\" is not supported, use S256", state)
		return
	}

	// Default to S256 if not specified.
	if codeChallengeMethod == "" {
		codeChallengeMethod = "S256"
	}

	// Reject anything other than S256.
	if codeChallengeMethod != "S256" {
		redirectWithError(w, r, redirectURI, "invalid_request", "code_challenge_method must be S256", state)
		return
	}

	// Default scope.
	if scope == "" {
		scope = "mcp"
	}

	// Render consent page via html/template (auto-escapes all fields).
	data := consentPageData{
		ClientName:    client.ClientName,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: codeChallenge,
		Scope:         scope,
		State:         state,
		Version:       version.Version,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := consentPageTemplate.Execute(w, data); err != nil {
		s.logger.Error("oauth: render consent page", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}

// handleAllow handles POST /oauth/authorize/allow.
// It generates an authorization code, stores it, and redirects with code+state.
func (s *OAuthServer) handleAllow(w http.ResponseWriter, r *http.Request) {
	setOAuthCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	codeChallenge := r.FormValue("code_challenge")
	scope := r.FormValue("scope")
	state := r.FormValue("state")

	// Validate client.
	client := s.FindClient(clientID)
	if client == nil {
		writeAuthorizeError(w, http.StatusBadRequest, "invalid_client", "unknown client_id")
		return
	}

	// Validate redirect_uri.
	if !clientHasRedirectURI(client, redirectURI) {
		writeAuthorizeError(w, http.StatusBadRequest, "invalid_request", "redirect_uri mismatch")
		return
	}

	// Validate state is present (matches handleAuthorize strictness).
	if state == "" {
		writeAuthorizeError(w, http.StatusBadRequest, "invalid_request", "state is required")
		return
	}

	// Validate code_challenge is present.
	if codeChallenge == "" {
		writeAuthorizeError(w, http.StatusBadRequest, "invalid_request", "code_challenge is required")
		return
	}

	// Generate auth code.
	code, err := GenerateCode()
	if err != nil {
		s.logger.Error("oauth: generate auth code", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	ac := &authCode{
		Code:          code,
		ClientID:      clientID,
		RedirectURI:   redirectURI,
		CodeChallenge: codeChallenge,
		Scope:         scope,
		SourceName:    client.OAuthSourceName,
		IssuedAt:      now,
		ExpiresAt:     now.Add(s.config.AuthCodeTTL),
		Used:          false,
	}
	s.codeStore.Store(ac)

	// Redirect with code and state.
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("code", code)
	q.Set("state", state)
	u.RawQuery = q.Encode()

	http.Redirect(w, r, u.String(), http.StatusFound)
}

// handleDeny handles POST /oauth/authorize/deny.
// It redirects with error=access_denied and the original state.
func (s *OAuthServer) handleDeny(w http.ResponseWriter, r *http.Request) {
	setOAuthCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")

	// Validate client and redirect_uri before redirecting.
	client := s.FindClient(clientID)
	if client == nil {
		writeAuthorizeError(w, http.StatusBadRequest, "invalid_client", "unknown client_id")
		return
	}
	if !clientHasRedirectURI(client, redirectURI) {
		writeAuthorizeError(w, http.StatusBadRequest, "invalid_request", "redirect_uri mismatch")
		return
	}

	// Validate state is present (matches handleAuthorize strictness).
	if state == "" {
		writeAuthorizeError(w, http.StatusBadRequest, "invalid_request", "state is required")
		return
	}

	redirectWithError(w, r, redirectURI, "access_denied", "user denied the request", state)
}

// clientHasRedirectURI checks if the redirect_uri exactly matches one of the
// client's registered URIs.
func clientHasRedirectURI(client *OAuthClient, uri string) bool {
	for _, registered := range client.RedirectURIs {
		if registered == uri {
			return true
		}
	}
	return false
}

// writeAuthorizeError writes a JSON error response for pre-redirect validation
// failures (unknown client, redirect_uri mismatch).
func writeAuthorizeError(w http.ResponseWriter, status int, errCode, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error":             errCode,
		"error_description": description,
	})
}

// redirectWithError redirects to the given URI with error, error_description,
// and state query parameters.
func redirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, errCode, description, state string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("error", errCode)
	q.Set("error_description", description)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()

	http.Redirect(w, r, u.String(), http.StatusFound)
}
