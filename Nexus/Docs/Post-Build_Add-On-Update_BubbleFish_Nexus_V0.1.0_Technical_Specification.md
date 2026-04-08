# Post-Build Add-On Update — BubbleFish Nexus v0.1.x
# OAuth 2.1 Authorization Server — Technical Specification

**Version:** 1.0  
**Date:** April 2026  
**Companion:** Post-Build_Add-On-Update_BubbleFish_Nexus_V0.1.0_State_Verification_Guide.md  
**Phase Commands:** Update_OAuth-Claude_Code_Phase_Commands.md  
**Audience:** Claude Code (primary build executor), engineers  
**Base:** BubbleFish Nexus v0.1.1 — all 31 packages green, daemon running  

---

## 1. Purpose

This specification adds a full OAuth 2.1 authorization server to BubbleFish Nexus, enabling ChatGPT and any other OAuth-only MCP client to connect to Nexus. The implementation is additive — all existing Bearer token auth (bfn_mcp_, bfn_data_, bfn_admin_) is preserved unchanged.

---

## 2. Scope

### 2.1 What This Adds

| Item | Description |
|------|-------------|
| `internal/oauth/` package | New package — ~800 lines Go |
| 4 new HTTP endpoints | `/.well-known/oauth-authorization-server`, `/oauth/authorize`, `/oauth/token`, `/oauth/jwks` |
| RSA-2048 key pair | Auto-generated on first start, stored as PEM file |
| JWT access tokens | RS256 signed, 1hr TTL, bfn_source claim for source mapping |
| Consent page | Self-contained HTML, no external dependencies |
| In-memory code store | Thread-safe, 5-minute TTL, single-use |
| Config block | `[daemon.oauth]` in daemon.toml |
| Install flag | `bubblefish install --oauth-issuer <url>` |

### 2.2 What This Does NOT Change

- `internal/mcp/server.go` auth logic for `bfn_mcp_` tokens — unchanged
- `internal/daemon/server.go` auth logic for `bfn_data_` / `bfn_admin_` tokens — unchanged
- All existing source configs — unchanged
- Claude Desktop, Perplexity Comet, Open WebUI connections — all unchanged
- WAL, queue, destination, retrieval pipeline — unchanged

### 2.3 authenticate() Extension (MCP Only)

The `authenticate()` and `authenticateSSE()` functions in `internal/mcp/server.go` are extended to accept JWT Bearer tokens in addition to the static `bfn_mcp_` key. The extension is additive — existing path is identical:

```
Token received →
  if starts with "bfn_mcp_" → existing constant-time comparison (unchanged)
  else → JWT validation path (new)
    validate RS256 signature
    validate exp, aud, iss claims
    extract bfn_source claim → resolve to source config
    return authenticated source context
```

---

## 3. New Package: internal/oauth/

### 3.1 File Map

| File | Responsibility | Est. Lines |
|------|---------------|------------|
| `internal/oauth/server.go` | `OAuthServer` struct, handler registration, config wiring | 150 |
| `internal/oauth/keys.go` | RSA-2048 key gen, PEM load/save, JWKS serialization | 120 |
| `internal/oauth/jwt.go` | JWT sign (RS256), validate, claims struct | 130 |
| `internal/oauth/store.go` | In-memory auth code store, TTL goroutine, thread-safe | 100 |
| `internal/oauth/authorize.go` | GET+POST `/oauth/authorize`, consent page HTML | 200 |
| `internal/oauth/token.go` | POST `/oauth/token`, PKCE verification, JWT issuance | 160 |
| `internal/oauth/metadata.go` | GET `/.well-known/oauth-authorization-server` | 60 |
| `internal/oauth/oauth_test.go` | Full flow integration tests | 250 |

### 3.2 OAuthServer Struct

```go
// internal/oauth/server.go

package oauth

type OAuthServer struct {
    config     OAuthConfig
    privateKey *rsa.PrivateKey
    publicKey  *rsa.PublicKey
    codeStore  *CodeStore
    logger     *slog.Logger
}

type OAuthConfig struct {
    Enabled              bool
    IssuerURL            string
    PrivateKeyFile       string
    AccessTokenTTL       time.Duration // default 1hr
    AuthCodeTTL          time.Duration // default 5min
    Clients              []OAuthClient
}

type OAuthClient struct {
    ClientID          string
    ClientName        string
    RedirectURIs      []string
    OAuthSourceName   string // maps to sources/*.toml
    AllowedScopes     []string
}
```

### 3.3 AuthCode Struct

```go
// internal/oauth/store.go

type authCode struct {
    Code          string
    ClientID      string
    RedirectURI   string
    CodeChallenge string // base64url(SHA256(verifier))
    Scope         string
    SourceName    string
    IssuedAt      time.Time
    ExpiresAt     time.Time
    Used          bool
}
```

### 3.4 JWT Claims

```go
// internal/oauth/jwt.go

type nexusClaims struct {
    jwt.RegisteredClaims          // iss, sub, aud, exp, iat, jti
    Scope     string `json:"scope"`
    BFNSource string `json:"bfn_source"` // maps to source config name
}
```

JWT header: `{"alg":"RS256","typ":"JWT","kid":"nexus-1"}`

JWT payload example:
```json
{
  "iss": "https://x7-b9t50-rx7.alessandraboutique.com",
  "sub": "chatgpt",
  "aud": "bubblefish-nexus",
  "exp": 1712534400,
  "iat": 1712530800,
  "jti": "550e8400-e29b-41d4-a716-446655440000",
  "scope": "mcp",
  "bfn_source": "default"
}
```

---

## 4. New HTTP Endpoints

### 4.1 GET /.well-known/oauth-authorization-server

No auth required. Returns RFC 8414 server metadata.

**Response:**
```json
{
  "issuer": "https://[issuer_url]",
  "authorization_endpoint": "https://[issuer_url]/oauth/authorize",
  "token_endpoint": "https://[issuer_url]/oauth/token",
  "jwks_uri": "https://[issuer_url]/oauth/jwks",
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code"],
  "code_challenge_methods_supported": ["S256"],
  "token_endpoint_auth_methods_supported": ["none"]
}
```

### 4.2 GET /oauth/authorize

No auth required (user browses to this URL).

**Required query params:**
- `response_type` — must be `"code"`
- `client_id` — must match a registered client
- `redirect_uri` — must match exactly one registered URI for that client
- `code_challenge` — base64url(SHA256(verifier)), required
- `code_challenge_method` — must be `"S256"`, reject `"plain"`
- `state` — required for CSRF protection

**Validation failures (do NOT redirect on redirect_uri errors):**
- Unknown `client_id` → 400 `{"error":"invalid_client"}`
- `redirect_uri` mismatch → 400 `{"error":"invalid_request"}` (never redirect)
- Missing `code_challenge` → redirect with `error=invalid_request`
- `code_challenge_method=plain` → redirect with `error=invalid_request`

**On valid request:** render HTML consent page (see Section 4.3).

### 4.3 POST /oauth/authorize/allow (consent page submit)

Rendered by consent page form. No auth required.

**Form fields:** `client_id`, `redirect_uri`, `code_challenge`, `scope`, `state` (hidden)

**On Allow:**
1. Generate 32-byte random auth code (hex encoded)
2. Store in CodeStore with 5-minute TTL
3. Redirect to `redirect_uri?code=<code>&state=<state>`

**On Deny:** Redirect to `redirect_uri?error=access_denied&state=<state>`

### 4.4 Consent Page HTML Requirements

Self-contained HTML — no external CSS, JS, or font dependencies. Inline styles only.

Must include:
- BubbleFish branding (text logo, blue color scheme matching `#1F4E79`)
- Application name from client config (`ClientName`)
- Clear statement: "BubbleFish Nexus will allow [App] to read and write your AI memories."
- Allow button (submits to `/oauth/authorize/allow`)
- Deny button (submits to `/oauth/authorize/deny`)
- CSRF: `state` embedded in hidden form fields on both buttons
- Mobile-friendly: `<meta name="viewport" content="width=device-width, initial-scale=1">`

### 4.5 POST /oauth/token

No auth required (PKCE replaces client secret).

**Request body** (`application/x-www-form-urlencoded`):
- `grant_type` — must be `"authorization_code"`
- `client_id` — must match code's stored client_id
- `redirect_uri` — must match exactly
- `code` — authorization code from step 4.3
- `code_verifier` — PKCE verifier string

**PKCE Verification (INVARIANT — never skip):**
```go
hash := sha256.Sum256([]byte(codeVerifier))
challenge := base64.RawURLEncoding.EncodeToString(hash[:])
if subtle.ConstantTimeCompare([]byte(challenge), []byte(storedChallenge)) != 1 {
    return error("invalid_grant")
}
```

**On success:**
1. Delete code from store (single use — delete before issuing token)
2. Issue JWT access token
3. Return:
```json
{
  "access_token": "<JWT>",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

**Error responses:**
- Wrong `code_verifier` → `{"error":"invalid_grant"}`
- Expired code → `{"error":"invalid_grant"}`
- Code already used → `{"error":"invalid_grant"}`
- Wrong `client_id` → `{"error":"invalid_client"}`
- Wrong `redirect_uri` → `{"error":"invalid_grant"}`

### 4.6 GET /oauth/jwks

No auth required. Returns RSA public key in JWK Set format.

```json
{
  "keys": [{
    "kty": "RSA",
    "use": "sig",
    "alg": "RS256",
    "kid": "nexus-1",
    "n": "<base64url modulus>",
    "e": "<base64url exponent>"
  }]
}
```

---

## 5. Key Management

### 5.1 Key Generation

RSA-2048 key pair auto-generated at startup if `[daemon.oauth] enabled = true` and the key file doesn't exist.

```
On startup with OAuth enabled:
  if private_key_file doesn't exist:
    generate RSA-2048 key pair
    write private key as PKCS#8 PEM to private_key_file
    set file permissions 0600
    log INFO "oauth: generated RSA-2048 key pair" with path
  else:
    load private key from file
    derive public key from private key
```

### 5.2 Invariants

- `private_key_file` MUST use `file:` reference. Plain literal private keys in daemon.toml → `SCHEMA_ERROR` at startup.
- Key file permissions MUST be 0600. If readable by others → log WARN.
- Private key NEVER appears in logs, responses, or error messages.
- Public key derivation is always from the private key — never stored separately.

---

## 6. Configuration

### 6.1 daemon.toml Block

```toml
# OAuth 2.1 Authorization Server
# Enables ChatGPT and other OAuth-only MCP clients
[daemon.oauth]
enabled = false

# Public URL Nexus is reachable at (Cloudflare tunnel or TLS domain)
# Required when enabled = true
issuer_url = ""

# RSA-2048 private key — MUST use file: reference
# Auto-generated on first start if file doesn't exist
private_key_file = "file:~/.bubblefish/Nexus/oauth_private.key"

# JWT access token lifetime (default 1 hour)
access_token_ttl_seconds = 3600

# Authorization code lifetime (default 5 minutes)
auth_code_ttl_seconds = 300

# Registered OAuth clients — one block per client
[[daemon.oauth.clients]]
client_id = "chatgpt"
client_name = "ChatGPT"
redirect_uris = [
  "https://chatgpt.com/connector/oauth/mLp1vR_7Z6RW"
]
# Must match a name in sources/*.toml
oauth_source_name = "default"
allowed_scopes = ["openid", "mcp"]
```

### 6.2 Startup Validation

On startup with `[daemon.oauth] enabled = true`:
- `issuer_url` empty → refuse to start, `SCHEMA_ERROR: oauth.issuer_url is required`
- `private_key_file` is plain literal (not `file:` or `env:`) → refuse to start, `SCHEMA_ERROR`
- No clients configured → log `WARN: oauth enabled but no clients registered`
- `issuer_url` does not start with `https://` → log `WARN: oauth issuer_url should use HTTPS`

### 6.3 Install Flag

```
bubblefish install --oauth-issuer https://[tunnel-url]
```

Effect:
- Sets `[daemon.oauth] enabled = true`
- Sets `issuer_url = "<provided URL>"`
- Writes `private_key_file = "file:~/.bubblefish/Nexus/oauth_private.key"`
- Adds ChatGPT client block with placeholder redirect_uri
- Prints install summary including: OAuth enabled, issuer URL, instruction to update `redirect_uris` with ChatGPT's actual callback URL

### 6.4 Doctor Checks (bubblefish doctor additions)

- OAuth enabled but `issuer_url` empty → ERROR
- OAuth enabled but `private_key_file` missing/unreadable → ERROR
- OAuth enabled but no clients → WARN
- OAuth enabled but `issuer_url` is `http://` (not https) → WARN (except localhost)

---

## 7. MCP authenticate() Extension

### 7.1 File: internal/mcp/server.go

Extend `authenticate()` and `authenticateSSE()` to accept JWT tokens. The existing `bfn_mcp_` path is identical — no behavioral change for current clients.

```go
func (s *Server) authenticate(r *http.Request) bool {
    token := extractBearerToken(r)
    if token == "" {
        return false
    }

    // Path 1: Static bfn_mcp_ key (existing — unchanged)
    if strings.HasPrefix(token, "bfn_mcp_") {
        provided := []byte(token)
        return subtle.ConstantTimeCompare(provided, s.resolvedKey) == 1
    }

    // Path 2: JWT access token (new — OAuth clients)
    if s.oauthServer != nil {
        return s.oauthServer.ValidateAccessToken(token)
    }

    return false
}
```

### 7.2 ValidateAccessToken

```go
// internal/oauth/jwt.go

func (s *OAuthServer) ValidateAccessToken(tokenString string) bool {
    // Parse and validate JWT
    // Check: signature (RS256 with our public key)
    // Check: exp (must be in future)
    // Check: aud == "bubblefish-nexus"
    // Check: iss == s.config.IssuerURL
    // Extract: bfn_source claim → verify source exists in config
    // Return: true only if ALL checks pass
}
```

**INVARIANT:** ALL of signature, exp, aud, iss must be validated. Never skip any check.

---

## 8. Security Invariants

All invariants must be enforced. These are non-negotiable.

| # | Invariant |
|---|-----------|
| 1 | `code_challenge_method=plain` ALWAYS rejected at authorize endpoint |
| 2 | PKCE verification uses `subtle.ConstantTimeCompare` — never `==` |
| 3 | Auth codes are single-use — deleted from store before JWT is issued |
| 4 | `redirect_uri` mismatch at authorize → 400, never redirect |
| 5 | JWT `exp` validated on every MCP request |
| 6 | JWT `aud` validated on every MCP request |
| 7 | JWT `iss` validated on every MCP request |
| 8 | Private key never appears in logs, responses, or errors |
| 9 | `private_key_file` as plain literal → `SCHEMA_ERROR` at startup |
| 10 | Key file written with permissions 0600 |
| 11 | Code store purge goroutine runs every 60 seconds |
| 12 | OAuth disabled (default) → zero `/oauth/*` endpoints registered |

---

## 9. Dependencies

Use Go standard library only where possible:
- `crypto/rsa` — RSA key generation and operations
- `crypto/rand` — cryptographic random for code generation and JTI
- `crypto/sha256` — PKCE S256 verification
- `encoding/base64` — base64url encoding/decoding
- `crypto/x509` — PEM key encoding/decoding
- `sync` — RWMutex for code store

For JWT:
- `github.com/golang-jwt/jwt/v5` — already in Nexus go.mod or add it

Check go.mod before adding any dependency. Prefer what's already there.

---

## 10. Quality Gate (all phases)

```powershell
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./... -race -count=1
```

All 31 existing packages must remain green. New `internal/oauth/` package must have zero test failures.

---

## 11. Backward Compatibility Guarantee

After full implementation:
- `.\bubblefish start` with no `[daemon.oauth]` config → OAuth endpoints not registered, zero behavior change
- Claude Desktop via stdio bridge → unchanged
- Perplexity Comet via SSE → unchanged  
- Open WebUI via Pipelines → unchanged
- All `bfn_*` key auth → unchanged
- All existing tests → pass

---

## 12. Version Target

This add-on targets **v0.1.2**. Tag after all 8 OA phases pass their acceptance criteria and the ChatGPT end-to-end verification succeeds.

```powershell
git tag v0.1.2
git push origin v0.1.2
```
