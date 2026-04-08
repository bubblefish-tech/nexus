# OAuth 2.1 Known Limitations — v0.1.3

BubbleFish Nexus v0.1.3 ships a working OAuth 2.1 authorization server that
allows ChatGPT, Claude Web UI, and any other OAuth-capable MCP client to
connect. Every request is authenticated, every auth code is single-use, every
JWT is RS256-signed with a 1-hour TTL, and the private key never leaves
disk. This document lists the known gaps honestly so users and contributors
understand the current security posture.

## Per-client source mapping — not yet implemented

The Tech Spec defines a `bfn_source` claim in the JWT access token that
maps an OAuth client to a source config in `sources/*.toml`. The claim is
signed into every issued JWT, but the MCP request pipeline does not yet
read it. As of v0.1.3, all authenticated MCP requests — whether they arrive
via a static `bfn_mcp_` bearer token or via an OAuth-issued JWT — resolve
to the single source configured in `[daemon.mcp]` of `daemon.toml`.

### What this means in practice

- All OAuth clients share the same rate limit, the same allowed
  collections, the same retention policies, and the same destination
  routing as the primary `[daemon.mcp]` source.
- A ChatGPT-issued JWT and a Claude Desktop `bfn_mcp_` token are
  indistinguishable to the rest of the Nexus pipeline once authentication
  succeeds.
- Per-client auditing by source name is not available — the audit log
  records the shared source name, not the OAuth client_id.
- Per-client revocation by policy change is not possible. Revoking a
  client requires removing it from `[[daemon.oauth.clients]]` in
  `daemon.toml` and restarting the daemon; after restart, existing JWTs
  for that client remain valid until their 1-hour `exp` expires
  (because Nexus does not maintain a JWT denylist).

### What this does NOT mean

- Authentication is still enforced on every request. Unauthenticated
  requests still return 401.
- JWT validation (signature, exp, aud, iss) still runs on every request.
- The `bfn_mcp_` static bearer path is still unchanged and still works
  for Claude Desktop, Perplexity Comet, Open WebUI, and any other client
  using the static key.
- Audit logging still records every request with IP, user agent, and
  timestamp — just not tagged with the OAuth client_id.

### Planned resolution

Per-client source mapping will land in a later release. The work required
is:

1. Extend the MCP pipeline's `Write`, `Search`, and `Status` methods to
   accept a per-request source override instead of using the server's
   single `sourceName` field.
2. Wire the JWT's `bfn_source` claim into the request context during
   `authenticate()`.
3. Extract the source from the context in `dispatchRPC` before calling
   pipeline methods.
4. Add integration tests that assert different JWTs land in different
   sources with different policies.

Until that work ships, operators who need hard per-client isolation
should run separate Nexus instances for separate trust boundaries.

## JWT revocation window

Access tokens are valid for 1 hour. There is no refresh token and no
server-side denylist. Revoking an OAuth client takes effect immediately
for new token requests but does not invalidate tokens already issued.
The worst-case revocation window is therefore 1 hour.

Operators who need immediate revocation should rotate the RSA signing key
(delete `oauth_private.key` and restart Nexus). This invalidates all
outstanding JWTs for all clients at once — it is a global reset, not a
per-client revocation.

## Single-tenant assumption

BubbleFish Nexus is designed as a single-user, single-host sovereign
memory daemon. The OAuth server has no concept of end-user identity —
the OAuth flow authorizes a *client application* to access the user's
memory, not a *user within the client application*. If ChatGPT, Claude
Web UI, and Perplexity Comet are all authorized against the same Nexus
instance, they all access the same memory store.

This is a deliberate design choice consistent with the sovereign-data
pitch. Multi-tenant deployments are out of scope for v0.1.x.
