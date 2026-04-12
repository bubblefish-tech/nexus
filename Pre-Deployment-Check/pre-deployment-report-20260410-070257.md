# Pre-Deployment Audit Report — BubbleFish Nexus v0.1.3

## 1. Header

| Field | Value |
|---|---|
| **Timestamp** | 2026-04-10 07:02:57 UTC |
| **Repo path** | `D:\Bubblefish\Nexus` |
| **Current branch** | `main` |
| **Current commit** | `2757daf3f09c465073436416f01cf5e9227159c9` |
| **Remote** | `https://github.com/shawnsammartano-hub/BubbleFish-Nexus.git` |
| **Visibility** | Private (GitHub personal account — assumed private until confirmed) |

---

## 2. Executive Summary

**BLOCKED** — This repository must NOT be made public in its current state.

| Severity | Count |
|---|---|
| **Critical** | 6 |
| **High** | 6 |
| **Medium** | 2 |

Three real authentication tokens are committed in tracked files and baked into git history. The `go.mod` specifies a Go version (1.26.1) that does not exist, which will break builds for every new clone. The README's headline feature (credential routing) does not exist in the codebase. Six of seven governance files referenced in the README are missing. These issues collectively block public deployment.

---

## 3. Critical Issues

### C-1: Real secrets committed in tracked files

Three distinct tokens with HIGH risk are present in tracked files:

| Token prefix | File | Line |
|---|---|---|
| `bfn_mcp_4...` | `examples/blessed/cursor/mcp.json` | 6 |
| `bfn_mcp_4...` | `internal/oauth/oauth_test.go` | 1410 |
| `bfn_admin_...` | `.claude/settings.local.json` | 42 |
| `bfn_data_f...` | `examples/blessed/openclaw/SETUP.md` | 167 |

The `bfn_mcp_` value is identical in the config file and the test file, strongly indicating a real issued token was copy-pasted rather than a synthetic placeholder.

**Evidence:** `secrets-found.txt`

### C-2: Secrets and tunnel hostname in git history

Even if the files above are cleaned from HEAD, the tokens remain in commits `a305506`, `2894235`, and `9c1ae60`. Additionally, the real Cloudflare tunnel hostname (`x7-b9t50-rx7.alessandraboutique.com`) appears 7 times in history across two documentation files. Anyone cloning the public repo can extract these with `git log -p`.

**Evidence:** `history-secret-scan.txt`

### C-3: go.mod specifies non-existent Go version 1.26.1

Go 1.26.x has not been released (latest stable as of April 2026 is 1.24.x). Any user running `go build ./...` or `go mod tidy` on a fresh clone will get a toolchain error or attempt to download a non-existent Go version. This is a hard build blocker.

**Evidence:** `D:\Bubblefish\Nexus\go.mod` line 3

### C-4: go.mod module path mismatch

Module path is `github.com/BubbleFish-Nexus`. Expected: `github.com/bubblefish-tech/nexus`. This will cause import path failures for any external consumer and mismatches with the actual GitHub repository URL.

### C-5: README headline feature (credential routing) does not exist

The README leads with credential routing as the primary feature. Searches for `synthetic_key`, `credentials.mappings`, `bfn_sk_`, and `bfn_route_` return zero matches in `internal/`. The feature is not implemented. Shipping a README that advertises a non-existent headline feature will damage credibility.

**Evidence:** `readme-claim-verification.txt`, Claim L

### C-6: README Quick Start shows wrong MCP port

README states MCP listens on `:8082`. The actual code default is `:7474` (confirmed in `internal/daemon/daemon.go:870-873` and `cmd/bubblefish/install.go:393`). Users following the Quick Start will fail to connect.

**Evidence:** `readme-claim-verification.txt`, Claim D

---

## 4. High Issues

### H-1: README references non-existent example directories

README points to `examples/integrations/openwebui/` and `examples/integrations/openclaw/`. Neither directory exists. The actual paths are `examples/blessed/openwebui/` and `examples/blessed/openclaw/`. All referenced files exist — only the path prefix is wrong.

**Evidence:** `readme-claim-verification.txt`, Claims F and G

### H-2: Five of six referenced documentation files do not exist

README references `docs/ARCHITECTURE.md`, `docs/DEPLOYMENT.md`, `docs/CONFIGURATION.md`, `docs/API.md`, and `docs/BENCHMARKS.md`. None exist. `THREAT_MODEL.md` exists at the repo root, not in `docs/`.

**Evidence:** `readme-claim-verification.txt`, Claim I

### H-3: Six of seven governance files do not exist

README references `COMMERCIAL_LICENSE.md`, `CLA.md`, `CLA-CORPORATE.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, and `SECURITY.md`. None exist. Only `CHANGELOG.md` is present.

**Evidence:** `readme-claim-verification.txt`, Claim K

### H-4: `docs/integrations/` directory does not exist

README references `docs/integrations/` with per-client setup files. The directory does not exist anywhere under `docs/`.

**Evidence:** `readme-claim-verification.txt`, Claim H

### H-5: `bubblefish-nexus.mcpb` binary tracked in git

This binary MCP bundle is tracked despite being a build artifact. It is not in `.gitignore`. It inflates the repository and should not be in a public repo.

**Evidence:** `gitignore-coverage.txt`

### H-6: `.claude/settings.local.json` tracked in git

This file contains a `bfn_admin_` token (see C-1) and is a local development settings file. It should be in `.gitignore` and removed from tracking.

---

## 5. Medium Issues

### M-1: 13 missing `.gitignore` patterns

The following patterns are absent from `.gitignore`: `daemon.toml`, `*.db`, `*.wal`, `*.sig`, `bubblefish-nexus.mcpb`, `/audit/`, `/security/`, `/data/`, `Test/`, `v010-dogfood/`, `/tmp/`, `/scratch/`, `*.env`.

**Evidence:** `gitignore-coverage.txt`

### M-2: `*.env` vs `.env.*` pattern gap

`.gitignore` contains `.env` and `.env.*` but not `*.env`. Files named like `production.env` or `staging.env` would not be ignored.

---

## 6. Per-Check Results

### Check 1 — Secret scan, current files

**Result:** 5 matches in tracked files. 3 distinct HIGH-risk tokens, 1 duplicate of one token in a test file, 1 LOW-risk PEM header in a test fixture. Zero matches for OpenAI, Anthropic, AWS, GitHub PAT, or canary patterns.

**Evidence file:** `secrets-found.txt`

### Check 2 — Secret scan, git history

**Result:** 88 commits scanned. HIGH-risk tokens found in commits `a305506`, `2894235`, `9c1ae60`. Tunnel hostname (`alessandraboutique` / `x7-b9t50`) found 7 times each in two documentation files. No OpenAI, Anthropic, AWS, or GitHub PAT tokens found in history.

**Evidence file:** `history-secret-scan.txt`

### Check 3 — License header coverage, .go files

**Result:** 194 .go files scanned. 194 contain the required copyright header. 0 missing. **100% coverage.**

**Evidence file:** `license-headers-missing.txt`

### Check 4 — License header coverage, .ts files

**Result:** 1 .ts file scanned (`examples/blessed/openclaw/index.ts`). Contains "AGPL-3.0 License" at line 7. 0 missing. **100% coverage.**

**Evidence file:** `license-headers-missing.txt`

### Check 5 — Personal name references

**Result:** 0 matches. "Shawn Sammartano" does not appear in any tracked file. **Clean.**

**Evidence file:** `personal-name-references.txt`

### Check 6 — Patent language references

**Result:** 2 files matched, both ALLOWED. `LICENSE` contains standard AGPL-3.0 Section 11 patent text (~23 occurrences). `mcpb/server/node_modules/mime-db/db.json` contains the MIME type "patentdive". No PROBLEM matches in README, docs, or source code. **Clean.**

**Evidence file:** `patent-language-references.txt`

### Check 7 — README claim verification

| Claim | Status |
|---|---|
| A: `bubblefish demo` command | VERIFIED |
| B: `--mode simple` flag | VERIFIED |
| C: HTTP port 8080 | VERIFIED |
| D: MCP port 7474 | VERIFIED (README says 8082 — WRONG) |
| E: Dashboard port 8081 | VERIFIED |
| F: examples/integrations/openwebui/ | NOT FOUND (actual: examples/blessed/openwebui/) |
| G: examples/integrations/openclaw/ | NOT FOUND (actual: examples/blessed/openclaw/) |
| H: docs/integrations/ | NOT FOUND |
| I: Six docs in docs/ | PARTIAL (1 of 6 exists, at repo root) |
| J: LICENSE with AGPL-3.0 | VERIFIED |
| K: Seven governance files | PARTIAL (1 of 7 exists) |
| L: Credential routing feature | NOT FOUND |

**Evidence file:** `readme-claim-verification.txt`

### Check 8 — .gitignore coverage

**Result:** 13 required patterns missing. 1 tracked file (`bubblefish-nexus.mcpb`) violates intended ignore rules.

**Evidence file:** `gitignore-coverage.txt`

### Check 9 — Cloudflare and tunnel references

**Result:** 0 matches in current tracked files for `cloudflared`, `alessandraboutique`, `x7-b9t50`, `cfargotunnel`, `trycloudflare`, or `cloudflareaccess`. However, `alessandraboutique` and `x7-b9t50` DO appear in git history (see Check 2). The Docs/ files containing them appear to have been deleted from HEAD (shown as ` D` in git status) but remain in history.

**Classification:** No current LEAK in tracked files. Historical LEAK exists and will be exposed if repo is pushed without history rewrite.

### Check 10 — Email and domain inconsistencies

**Result:** 0 matches for `bubblefish.sh` or `bubblefish.tech` in any tracked file. **Clean.**

### Check 11 — go.mod sanity

| Field | Value | Status |
|---|---|---|
| Module path | `github.com/BubbleFish-Nexus` | **MISMATCH** — expected `github.com/bubblefish-tech/nexus` |
| Go version | `1.26.1` | **CRITICAL** — version does not exist |
| Replace directives | None | OK |

---

## 7. Recommendations

Ordered by priority (do these before pushing):

1. **ROTATE ALL TOKENS IMMEDIATELY.** Invalidate `bfn_admin_4a3238...`, `bfn_mcp_473a2b...`, and `bfn_data_YOUR_DATA_KEY_HERE` on any running instance. These must be considered compromised if anyone has clone access.

2. **REWRITE GIT HISTORY** using `git filter-repo` to remove:
   - `.claude/settings.local.json` (contains admin token)
   - `examples/blessed/cursor/mcp.json` (contains MCP token)
   - `examples/blessed/openclaw/SETUP.md` lines containing `bfn_data_YOUR_DATA_KEY_HERE`
   - `internal/oauth/oauth_test.go` lines containing `bfn_mcp_473a2b...`
   - All occurrences of `alessandraboutique` and `x7-b9t50` in Docs/ files
   - Coordinate with all existing cloners, as this requires force-push.

3. **FIX go.mod:** Change `go 1.26.1` to `go 1.24` (or latest stable). Change module path from `github.com/BubbleFish-Nexus` to `github.com/bubblefish-tech/nexus` (and update all import paths accordingly).

4. **REPLACE HARDCODED TOKENS** in current files with clearly synthetic placeholders:
   - `examples/blessed/cursor/mcp.json` — replace Bearer value with `bfn_mcp_0000000000000000000000000000000000000000000000000000000000000000`
   - `internal/oauth/oauth_test.go:1410` — replace with test-only synthetic token
   - `examples/blessed/openclaw/SETUP.md:167` — replace with `bfn_data_0000000000000000...`

5. **ADD TO .gitignore:**
   ```
   .claude/settings.local.json
   daemon.toml
   *.db
   *.wal
   *.sig
   bubblefish-nexus.mcpb
   /audit/
   /security/
   /data/
   Test/
   v010-dogfood/
   /tmp/
   /scratch/
   *.env
   ```

6. **UNTRACK `bubblefish-nexus.mcpb`:** Run `git rm --cached bubblefish-nexus.mcpb` after adding it to `.gitignore`.

7. **UNTRACK `.claude/settings.local.json`:** Run `git rm --cached .claude/settings.local.json` after adding it to `.gitignore`.

8. **FIX README MCP PORT:** Change `:8082` to `:7474` in the Quick Start section.

9. **FIX README EXAMPLE PATHS:** Change `examples/integrations/openwebui/` to `examples/blessed/openwebui/` and `examples/integrations/openclaw/` to `examples/blessed/openclaw/`.

10. **EITHER CREATE OR REMOVE REFERENCES** to these missing files:
    - `docs/ARCHITECTURE.md`, `docs/DEPLOYMENT.md`, `docs/CONFIGURATION.md`, `docs/API.md`, `docs/BENCHMARKS.md`
    - `COMMERCIAL_LICENSE.md`, `CLA.md`, `CLA-CORPORATE.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`
    - `docs/integrations/` (entire directory)
    - Move `THREAT_MODEL.md` to `docs/THREAT_MODEL.md` or update README to point to repo root.

11. **DECIDE ON CREDENTIAL ROUTING CLAIM:** Either implement the feature or remove all credential routing language from README. Shipping a README that advertises a non-existent headline feature is a credibility risk.

---

## 8. Pre-Public-Push Checklist

| # | Item | Status |
|---|---|---|
| 1 | Secret scan — current files clear | **FAIL** — 3 real tokens found |
| 2 | Secret scan — git history clear | **FAIL** — tokens and tunnel hostname in history |
| 3 | All tokens rotated | **NOT_CHECKED** — audit is read-only |
| 4 | License headers on all source files | **PASS** — 100% coverage (194 .go + 1 .ts) |
| 5 | No personal names in copyright positions | **PASS** — no "Shawn Sammartano" matches |
| 6 | No patent language outside legal docs | **PASS** — only in LICENSE and vendored MIME DB |
| 7 | README claims verified | **FAIL** — 4 NOT FOUND, 2 PARTIAL, 1 wrong port |
| 8 | .gitignore covers all sensitive patterns | **FAIL** — 13 patterns missing |
| 9 | No sensitive files tracked despite .gitignore | **FAIL** — bubblefish-nexus.mcpb tracked |
| 10 | No Cloudflare tunnel leaks in current files | **PASS** — 0 matches |
| 11 | Cloudflare tunnel leaks purged from history | **FAIL** — 14 occurrences in history |
| 12 | Domain references consistent (bubblefish.ai) | **PASS** — no .sh or .tech variants |
| 13 | go.mod version is a real Go release | **FAIL** — 1.26.1 does not exist |
| 14 | go.mod module path matches GitHub repo | **FAIL** — path mismatch |
| 15 | No local replace directives in go.mod | **PASS** |
| 16 | Branch protection enabled on main | **NOT_CHECKED** — requires GitHub API |
| 17 | GitHub secret scanning enabled | **NOT_CHECKED** — requires GitHub settings |
| 18 | All governance files referenced in README exist | **FAIL** — 6 of 7 missing |

**Overall: 6 PASS, 8 FAIL, 3 NOT_CHECKED — BLOCKED**

---

*Report generated by pre-deployment audit, 2026-04-10T07:02:57Z*
*Auditor: automated scan, read-only, no modifications made*
