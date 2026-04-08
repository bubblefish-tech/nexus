# Post-Build Add-On Update — BubbleFish Nexus v0.1.x
# OAuth 2.1 Authorization Server — State & Verification Guide

**Version:** 1.0  
**Date:** April 2026  
**Companion:** Post-Build_Add-On-Update_BubbleFish_Nexus_V0.1.0_Technical_Specification.md  
**Phase Commands:** Update_OAuth-Claude_Code_Phase_Commands.md  
**Audience:** Claude Code (primary build executor)  
**Base State:** v0.1.1 — all 31 packages green  

---

## CRITICAL RULES FOR CLAUDE CODE

1. **Every file is created before it is ever modified.** Never `str_replace` a file that hasn't been created yet in this session.
2. **One phase at a time.** Run the quality gate after every phase. Do not proceed if it fails.
3. **Never modify existing auth code** in `internal/mcp/server.go` until Phase OA-5. The `bfn_mcp_` path must remain identical.
4. **Never skip security invariants.** All 12 invariants in the Tech Spec Section 8 are mandatory.
5. **Check go.mod before adding dependencies.** Use existing deps where possible.

---

## Pre-Flight: Verify Base State

Before starting any OAuth phase, verify the codebase is clean:

```powershell
cd D:\BubbleFish\Nexus
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./... -race -count=1
```

**Expected:** All 31 packages green. Zero failures. If any failures exist, fix them before proceeding.

```powershell
git status
```

**Expected:** Clean working tree. If uncommitted changes exist, commit or stash before starting.

---

## Phase OA-1: Key Infrastructure

**Goal:** RSA-2048 key generation, JWT sign/verify, JWKS endpoint.

### Files Created This Phase

```
internal/oauth/              ← new package directory
internal/oauth/keys.go       ← RSA key gen, PEM load/save
internal/oauth/jwt.go        ← JWT sign, validate, claims
internal/oauth/server.go     ← OAuthServer struct (skeleton)
internal/oauth/oauth_test.go ← tests for this phase
```

### State After OA-1

- [ ] `internal/oauth/` directory exists
- [ ] `internal/oauth/keys.go` exists — `GenerateRSAKey()`, `LoadRSAKey()`, `SaveRSAKey()`, `PublicKeyToJWK()`
- [ ] `internal/oauth/jwt.go` exists — `SignJWT()`, `ValidateJWT()`, `nexusClaims` struct
- [ ] `internal/oauth/server.go` exists — `OAuthServer` struct, `OAuthConfig`, `OAuthClient`
- [ ] `internal/oauth/oauth_test.go` exists

### Verification Tests (OA-1)

```powershell
# Build must pass
go build ./...
go vet ./...

# Run OAuth package tests
$env:CGO_ENABLED='1'; go test ./internal/oauth/... -race -count=1 -v -run TestKey
$env:CGO_ENABLED='1'; go test ./internal/oauth/... -race -count=1 -v -run TestJWT
```

**Expected test output:**
```
--- PASS: TestKeyGeneration
--- PASS: TestKeyLoadSave
--- PASS: TestJWTSignAndVerify
--- PASS: TestJWTExpiredRejected
--- PASS: TestJWTWrongAudienceRejected
--- PASS: TestJWTWrongIssuerRejected
--- PASS: TestJWTWrongKeyRejected
--- PASS: TestPublicKeyToJWK
```

**Manual JWKS format check:**
```go
// PublicKeyToJWK must return valid JSON with these fields:
// kty, use, alg, kid, n, e
// n and e must be base64url encoded (no padding)
```

### Commit

```powershell
git add -A
git commit -s -m "Phase OA-1: RSA key infrastructure and JWT sign/verify"
```

---

## Phase OA-2: Server Metadata Endpoint

**Goal:** `/.well-known/oauth-authorization-server` discovery document.

### Files Created This Phase

```
internal/oauth/metadata.go   ← metadata handler
```

### Files Modified This Phase

```
internal/oauth/server.go     ← add RegisterHandlers() method
```

### State After OA-2

- [ ] `internal/oauth/metadata.go` exists — `handleMetadata()` handler
- [ ] `OAuthServer.RegisterHandlers(mux *http.ServeMux)` method exists
- [ ] `RegisterHandlers` registers ONLY `/.well-known/oauth-authorization-server` in this phase
- [ ] Metadata response is valid RFC 8414 JSON
- [ ] No auth required on metadata endpoint

### Verification Tests (OA-2)

```powershell
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./internal/oauth/... -race -count=1 -v -run TestMetadata
```

**Manual verification (start test HTTP server in test):**
```
GET /.well-known/oauth-authorization-server
Expected fields: issuer, authorization_endpoint, token_endpoint, jwks_uri,
                 response_types_supported, grant_types_supported,
                 code_challenge_methods_supported, token_endpoint_auth_methods_supported
code_challenge_methods_supported must NOT include "plain"
```

### Commit

```powershell
git add -A
git commit -s -m "Phase OA-2: OAuth server metadata endpoint (RFC 8414)"
```

---

## Phase OA-3: Authorization Endpoint + Code Store

**Goal:** `/oauth/authorize` consent page, in-memory code store, PKCE validation.

### Files Created This Phase

```
internal/oauth/store.go      ← CodeStore, authCode, TTL purge goroutine
internal/oauth/authorize.go  ← handleAuthorize(), consentPageHTML, handleAllow(), handleDeny()
```

### Files Modified This Phase

```
internal/oauth/server.go     ← add codeStore field, register /oauth/authorize routes
```

### State After OA-3

- [ ] `internal/oauth/store.go` exists
- [ ] `CodeStore` is thread-safe (`sync.RWMutex`)
- [ ] `CodeStore` purge goroutine runs every 60 seconds
- [ ] `authCode` struct has: `Code`, `ClientID`, `RedirectURI`, `CodeChallenge`, `Scope`, `SourceName`, `IssuedAt`, `ExpiresAt`, `Used`
- [ ] Auth codes are 32 bytes of `crypto/rand` (64 hex chars)
- [ ] Auth code TTL is 5 minutes
- [ ] `internal/oauth/authorize.go` exists
- [ ] `handleAuthorize()` validates ALL required params before rendering page
- [ ] `redirect_uri` mismatch → 400, NEVER redirect
- [ ] `code_challenge_method=plain` → redirect with `error=invalid_request`
- [ ] Consent page HTML is self-contained (no external deps)
- [ ] `handleAllow()` stores code, redirects with `code` and `state`
- [ ] `handleDeny()` redirects with `error=access_denied` and `state`
- [ ] `state` is embedded as hidden form field in consent page

### Verification Tests (OA-3)

```powershell
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./internal/oauth/... -race -count=1 -v -run TestAuthorize
$env:CGO_ENABLED='1'; go test ./internal/oauth/... -race -count=1 -v -run TestCodeStore
```

**Expected test coverage:**
```
--- PASS: TestAuthorizeValidRequest (renders consent page)
--- PASS: TestAuthorizeInvalidClientID (400, not redirect)
--- PASS: TestAuthorizeRedirectURIMismatch (400, not redirect)
--- PASS: TestAuthorizeMissingCodeChallenge (redirect with error)
--- PASS: TestAuthorizePlainMethodRejected (redirect with error)
--- PASS: TestAuthorizeMissingState (redirect with error)
--- PASS: TestAllowFlow (code in store, redirect with code+state)
--- PASS: TestDenyFlow (redirect with error=access_denied)
--- PASS: TestCodeStoreExpiry (expired code not retrievable)
--- PASS: TestCodeStorePurge (expired codes removed by purge)
--- PASS: TestCodeStoreConcurrent (race-free under concurrent access)
```

**Consent page manual check:**
- Contains `<meta name="viewport">`
- Contains Allow and Deny buttons
- Contains hidden `state` field on both forms
- No `<link>` or `<script src>` external resources
- Contains application name from client config

### Commit

```powershell
git add -A
git commit -s -m "Phase OA-3: Authorization endpoint, consent page, code store"
```

---

## Phase OA-4: Token Endpoint

**Goal:** `/oauth/token` — code exchange, PKCE verification, JWT issuance.

### Files Created This Phase

```
internal/oauth/token.go      ← handleToken(), PKCE verification, JWT issuance
```

### Files Modified This Phase

```
internal/oauth/server.go     ← register /oauth/token route
```

### State After OA-4

- [ ] `internal/oauth/token.go` exists
- [ ] `handleToken()` validates `grant_type == "authorization_code"`
- [ ] `handleToken()` validates `client_id` matches code's stored client_id
- [ ] `handleToken()` validates `redirect_uri` matches exactly
- [ ] PKCE verification uses `subtle.ConstantTimeCompare` — NEVER `==`
- [ ] Code deleted from store BEFORE JWT is issued (single use, delete on redemption)
- [ ] Second use of same code → `{"error":"invalid_grant"}`
- [ ] JWT issued with correct claims: `iss`, `sub`, `aud`, `exp`, `iat`, `jti`, `scope`, `bfn_source`
- [ ] `jti` is a unique UUID per token
- [ ] Token response: `{"access_token":"<JWT>","token_type":"Bearer","expires_in":3600}`
- [ ] Content-Type for token response: `application/json`

### PKCE Verification Code (must match exactly)

```go
hash := sha256.Sum256([]byte(codeVerifier))
challenge := base64.RawURLEncoding.EncodeToString(hash[:])
if subtle.ConstantTimeCompare([]byte(challenge), []byte(storedChallenge)) != 1 {
    writeTokenError(w, "invalid_grant", "PKCE verification failed")
    return
}
```

### Verification Tests (OA-4)

```powershell
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./internal/oauth/... -race -count=1 -v -run TestToken
```

**Expected test coverage:**
```
--- PASS: TestTokenValidFlow (code + verifier → JWT)
--- PASS: TestTokenWrongVerifier (invalid_grant)
--- PASS: TestTokenExpiredCode (invalid_grant)
--- PASS: TestTokenCodeSingleUse (second use → invalid_grant)
--- PASS: TestTokenWrongClientID (invalid_client)
--- PASS: TestTokenWrongRedirectURI (invalid_grant)
--- PASS: TestTokenJWTClaims (all claims present and correct)
--- PASS: TestTokenJWTExpiry (exp = now + 3600)
```

**Full flow test:**
```
--- PASS: TestOAuthFullFlow (authorize → allow → token → JWT valid)
```

### Commit

```powershell
git add -A
git commit -s -m "Phase OA-4: Token endpoint, PKCE verification, JWT issuance"
```

---

## Phase OA-5: MCP authenticate() Extension

**Goal:** JWT acceptance in `authenticate()` and `authenticateSSE()` in `internal/mcp/server.go`.

### Files Modified This Phase

```
internal/oauth/jwt.go        ← add ValidateAccessToken() method on OAuthServer
internal/mcp/server.go       ← extend authenticate() and authenticateSSE()
```

**NO other files in internal/mcp/ are modified.**

### State After OA-5

- [ ] `OAuthServer.ValidateAccessToken(tokenString string) bool` method exists
- [ ] `ValidateAccessToken` checks: RS256 signature, `exp`, `aud == "bubblefish-nexus"`, `iss == config.IssuerURL`
- [ ] `ValidateAccessToken` returns false if ANY check fails
- [ ] `Server` struct in `internal/mcp/server.go` has optional `oauthServer *oauth.OAuthServer` field
- [ ] `authenticate()` bfn_mcp_ path is IDENTICAL to before — no behavioral change
- [ ] `authenticate()` JWT path only runs if `s.oauthServer != nil`
- [ ] `authenticateSSE()` receives same extension
- [ ] If OAuth disabled (oauthServer == nil): only bfn_mcp_ auth works — unchanged

### Verification Tests (OA-5)

```powershell
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./internal/mcp/... -race -count=1 -v
$env:CGO_ENABLED='1'; go test ./internal/oauth/... -race -count=1 -v -run TestMCP
```

**Expected test coverage:**
```
# Existing MCP tests — must ALL still pass (backward compat)
--- PASS: TestMCPAuthenticate_StaticKey
--- PASS: TestMCPAuthenticate_InvalidKey
--- PASS: TestMCPInitialize
--- PASS: TestMCPToolsList
--- PASS: TestMCPToolsCall

# New tests
--- PASS: TestMCPAuthenticate_ValidJWT
--- PASS: TestMCPAuthenticate_ExpiredJWT (rejected)
--- PASS: TestMCPAuthenticate_WrongAudJWT (rejected)
--- PASS: TestMCPAuthenticate_WrongIssuJWT (rejected)
--- PASS: TestMCPAuthenticate_WrongKeyJWT (rejected)
--- PASS: TestMCPAuthenticate_OAuthDisabled_JWTRejected
```

### Manual Backward Compat Verification

```powershell
# Start daemon (no OAuth config)
.\bubblefish start --home D:\Test\BubbleFish\v010-dogfood\home

# Existing bfn_mcp_ key must still work
Invoke-RestMethod -Uri "http://127.0.0.1:7474/mcp" -Method POST `
  -Headers @{"Authorization"="Bearer bfn_mcp_473a2b20795677cab8015b0cf384e7d5c40a48bc47cf276bed586c27db18a057"; "Content-Type"="application/json"} `
  -Body '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'

# Expected: {"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05",...}}
```

### Commit

```powershell
git add -A
git commit -s -m "Phase OA-5: JWT acceptance in MCP authenticate(), backward compat preserved"
```

---

## Phase OA-6: Config, Daemon Wiring, Install Flag

**Goal:** `[daemon.oauth]` config block, daemon startup wiring, `--oauth-issuer` install flag, doctor checks.

### Files Modified This Phase

```
internal/config/config.go    ← add OAuthConfig, OAuthClientConfig structs and parsing
internal/daemon/server.go    ← instantiate OAuthServer, pass to MCP server
cmd/bubblefish/install.go    ← add --oauth-issuer flag
cmd/bubblefish/doctor.go     ← add OAuth-specific doctor checks
```

### State After OA-6

- [ ] `internal/config/config.go` has `OAuthConfig` struct with all fields from Tech Spec Section 6.1
- [ ] Config parsing handles `[[daemon.oauth.clients]]` array correctly
- [ ] Startup validation: `issuer_url` empty with enabled=true → refuse to start
- [ ] Startup validation: `private_key_file` is plain literal → `SCHEMA_ERROR`
- [ ] Startup validation: no clients → `WARN` (not error)
- [ ] Daemon wires `OAuthServer` into HTTP mux if OAuth enabled
- [ ] Daemon passes `OAuthServer` reference to MCP `Server`
- [ ] `bubblefish install --oauth-issuer <url>` flag exists and works
- [ ] Doctor checks: issuer_url empty, key file missing, no clients, http:// issuer

### Verification Tests (OA-6)

```powershell
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./... -race -count=1
```

**Config validation tests:**
```
--- PASS: TestOAuthConfigEnabledNoIssuer (refuse to start)
--- PASS: TestOAuthConfigPlainLiteralKey (SCHEMA_ERROR)
--- PASS: TestOAuthConfigDisabled (no endpoints registered)
--- PASS: TestOAuthConfigNoClients (WARN logged)
```

**Install flag test:**
```powershell
# Test in temp dir
.\bubblefish install --mode simple --oauth-issuer https://example.com --home D:\Temp\oauth-test
Get-Content D:\Temp\oauth-test\daemon.toml | Select-String "oauth"
# Expected: enabled = true, issuer_url = "https://example.com"
```

### Commit

```powershell
git add -A
git commit -s -m "Phase OA-6: OAuth config, daemon wiring, install flag, doctor checks"
```

---

## Phase OA-7: JWKS Endpoint + Full Integration Tests

**Goal:** `/oauth/jwks` endpoint, full end-to-end flow integration tests.

### Files Modified This Phase

```
internal/oauth/server.go     ← register /oauth/jwks route (handleJWKS)
internal/oauth/oauth_test.go ← add full flow and security tests
```

### State After OA-7

- [ ] `/oauth/jwks` returns valid JWK Set JSON
- [ ] JWK Set contains `kty`, `use`, `alg`, `kid`, `n`, `e`
- [ ] `n` and `e` are base64url encoded without padding
- [ ] Full flow test passes end-to-end
- [ ] All 11 security tests pass (see below)
- [ ] All 31 existing packages remain green

### Verification Tests (OA-7) — Complete Test Suite

```powershell
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./... -race -count=1 -v
```

**Required passing tests — OAuth package:**
```
--- PASS: TestKeyGeneration
--- PASS: TestKeyLoadSave
--- PASS: TestJWTSignAndVerify
--- PASS: TestJWTExpiredRejected
--- PASS: TestJWTWrongAudienceRejected
--- PASS: TestJWTWrongIssuerRejected
--- PASS: TestJWTWrongKeyRejected
--- PASS: TestPublicKeyToJWK
--- PASS: TestMetadataEndpoint
--- PASS: TestMetadataFields
--- PASS: TestAuthorizeValidRequest
--- PASS: TestAuthorizeInvalidClientID
--- PASS: TestAuthorizeRedirectURIMismatch
--- PASS: TestAuthorizeMissingCodeChallenge
--- PASS: TestAuthorizePlainMethodRejected
--- PASS: TestAuthorizeMissingState
--- PASS: TestAllowFlow
--- PASS: TestDenyFlow
--- PASS: TestCodeStoreExpiry
--- PASS: TestCodeStorePurge
--- PASS: TestCodeStoreConcurrent
--- PASS: TestTokenValidFlow
--- PASS: TestTokenWrongVerifier
--- PASS: TestTokenExpiredCode
--- PASS: TestTokenCodeSingleUse
--- PASS: TestTokenWrongClientID
--- PASS: TestTokenWrongRedirectURI
--- PASS: TestTokenJWTClaims
--- PASS: TestTokenJWTExpiry
--- PASS: TestOAuthFullFlow
--- PASS: TestJWKSEndpoint
--- PASS: TestJWKSFields
--- PASS: TestMCPAuthenticate_ValidJWT
--- PASS: TestMCPAuthenticate_ExpiredJWT
--- PASS: TestMCPAuthenticate_WrongAudJWT
--- PASS: TestMCPAuthenticate_WrongKeyJWT
--- PASS: TestMCPAuthenticate_OAuthDisabled_JWTRejected
--- PASS: TestBackwardCompatibility_StaticKey
--- PASS: TestConcurrentSessions
--- PASS: TestOAuthConfigEnabledNoIssuer
--- PASS: TestOAuthConfigPlainLiteralKey
--- PASS: TestOAuthConfigDisabled
```

### Commit

```powershell
git add -A
git commit -s -m "Phase OA-7: JWKS endpoint, full integration tests, all green"
```

---

## Phase OA-8: ChatGPT End-to-End Verification

**Goal:** Verify ChatGPT can connect to Nexus through the Cloudflare tunnel.

### Pre-Requisites

- Nexus daemon running with `[daemon.oauth]` enabled
- Cloudflare tunnel pointing to localhost:7474 (MCP port)
- `issuer_url` set to tunnel public URL

### Step 1: Enable OAuth in Dogfood Config

Add to `D:\Test\BubbleFish\v010-dogfood\home\daemon.toml`:
```toml
[daemon.oauth]
enabled = true
issuer_url = "https://x7-b9t50-rx7.alessandraboutique.com"
private_key_file = "file:D:/Test/BubbleFish/v010-dogfood/home/oauth_private.key"
access_token_ttl_seconds = 3600
auth_code_ttl_seconds = 300

[[daemon.oauth.clients]]
client_id = "chatgpt"
client_name = "ChatGPT"
redirect_uris = ["https://chatgpt.com/connector/oauth/mLp1vR_7Z6RW"]
oauth_source_name = "default"
allowed_scopes = ["openid", "mcp"]
```

### Step 2: Restart Daemon

```powershell
.\bubblefish stop
.\bubblefish start --home D:\Test\BubbleFish\v010-dogfood\home
```

**Expected log lines:**
```
level=INFO msg="oauth: server started" component=oauth issuer=https://x7-b9t50-rx7.alessandraboutique.com
level=INFO msg="oauth: generated RSA-2048 key pair" component=oauth path=...oauth_private.key
level=INFO msg="mcp: server started" component=mcp addr=127.0.0.1:7474
```

### Step 3: Verify Metadata Endpoint

```powershell
Invoke-RestMethod -Uri "https://x7-b9t50-rx7.alessandraboutique.com/.well-known/oauth-authorization-server"
```

**Expected:** JSON with `issuer`, `authorization_endpoint`, `token_endpoint`, `jwks_uri`, and `code_challenge_methods_supported: ["S256"]`

### Step 4: Verify JWKS Endpoint

```powershell
Invoke-RestMethod -Uri "https://x7-b9t50-rx7.alessandraboutique.com/oauth/jwks"
```

**Expected:** JSON with `keys` array containing RSA public key fields

### Step 5: Verify Existing Auth Still Works

```powershell
# bfn_mcp_ key must still work
Invoke-RestMethod -Uri "https://x7-b9t50-rx7.alessandraboutique.com/mcp" -Method POST `
  -Headers @{"Authorization"="Bearer bfn_mcp_473a2b20795677cab8015b0cf384e7d5c40a48bc47cf276bed586c27db18a057"; "Content-Type"="application/json"} `
  -Body '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
```

**Expected:** `protocolVersion: 2024-11-05` — Comet and Claude Desktop unaffected.

### Step 6: ChatGPT Connection

1. Open ChatGPT → GPT configuration or Plugin settings
2. Add MCP server: `https://x7-b9t50-rx7.alessandraboutique.com/mcp`
3. ChatGPT detects OAuth requirements, opens browser to `/oauth/authorize`
4. Nexus consent page renders
5. Click Allow
6. ChatGPT completes token exchange
7. Ask ChatGPT: "Call nexus_status"
8. Expected: `{"status":"OK","version":"0.1.2","queue_depth":0}`

### Step 7: Concurrent Session Verification

With ChatGPT connected:
- Verify Claude Desktop `nexus_status` still works
- Verify Perplexity Comet `nexus_status` still works
- All three return OK simultaneously

### Step 8: Tag Release

```powershell
git add -A
git commit -s -m "Phase OA-8: ChatGPT verified, all clients concurrent"
git tag v0.1.2
git push origin v0.1.2
git push origin --tags
```

---

## Ship Checklist

### Build
- [ ] `go build ./...` — zero errors
- [ ] `go vet ./...` — zero warnings
- [ ] `go test ./... -race -count=1` — all packages green including internal/oauth

### Functional
- [ ] `/.well-known/oauth-authorization-server` returns RFC 8414 compliant JSON
- [ ] `/oauth/jwks` returns valid JWK Set
- [ ] `/oauth/authorize` renders consent page for valid ChatGPT request
- [ ] Allow flow: code issued, redirect correct
- [ ] Deny flow: error redirect correct
- [ ] Token endpoint: valid PKCE exchange issues JWT
- [ ] JWT accepted on MCP endpoint
- [ ] `nexus_write` works from ChatGPT — memory written to SQLite
- [ ] `nexus_search` works from ChatGPT — retrieves memories
- [ ] `nexus_status` works from ChatGPT

### Security
- [ ] Expired JWT rejected (manual test with `exp` in past)
- [ ] Wrong signing key rejected
- [ ] Code used twice: second use returns `invalid_grant`
- [ ] PKCE with wrong verifier: `invalid_grant`
- [ ] `code_challenge_method=plain` rejected at authorize
- [ ] `redirect_uri` mismatch → 400, no redirect
- [ ] `private_key_file` as plain literal → startup failure
- [ ] OAuth disabled: `/oauth/*` endpoints return 404

### Backward Compatibility
- [ ] `bfn_mcp_` key auth: Claude Desktop and Comet unaffected
- [ ] `bfn_data_` key auth: Open WebUI pipeline unaffected
- [ ] `bfn_admin_` key auth: admin endpoints unaffected
- [ ] SSE transport (Comet): unaffected
- [ ] Stdio transport (Claude Desktop MCPB): unaffected
- [ ] All 31 pre-OAuth packages: still green

### Release
- [ ] `CHANGELOG.md` updated with v0.1.2 entry
- [ ] `internal/version/version.go` updated to `"0.1.2"`
- [ ] Git tag `v0.1.2` pushed
- [ ] GitHub release created with binaries
