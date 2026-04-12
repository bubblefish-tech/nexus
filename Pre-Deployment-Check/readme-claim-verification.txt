========================================================
README CLAIM VERIFICATION
D:\Bubblefish\Nexus\README.md
Scan date: 2026-04-10
========================================================

CLAIM A: `bubblefish demo` command exists
STATUS: VERIFIED
EVIDENCE:
  - D:\Bubblefish\Nexus\cmd\bubblefish\demo.go implements runDemo()
  - D:\Bubblefish\Nexus\cmd\bubblefish\main.go:107-108 dispatches case "demo"
  - Help text at main.go:54 lists "demo" subcommand

CLAIM B: `bubblefish install --mode simple` command and flag exist
STATUS: VERIFIED
EVIDENCE:
  - D:\Bubblefish\Nexus\cmd\bubblefish\install.go:85 defines --mode flag with "simple" as valid value
  - install.go:341,346 handles case "simple" in buildDaemonTOML

CLAIM C: HTTP API listens on port 8080 by default
STATUS: VERIFIED
EVIDENCE:
  - D:\Bubblefish\Nexus\internal\config\loader.go:32 — defaultPort = 8080
  - D:\Bubblefish\Nexus\cmd\bubblefish\install.go:330 — port := 8080

CLAIM D: MCP server listens on port 7474 by default
STATUS: VERIFIED (but README Quick Start contains error)
EVIDENCE:
  - D:\Bubblefish\Nexus\internal\daemon\daemon.go:870-873 — MCP default port = 7474
  - D:\Bubblefish\Nexus\cmd\bubblefish\install.go:393 — port = 7474 in daemon.toml template
  - CAVEAT: README.md line ~17 says "MCP on :8082" — this is INCORRECT. Code uses 7474.

CLAIM E: Web dashboard listens on port 8081 by default
STATUS: VERIFIED
EVIDENCE:
  - D:\Bubblefish\Nexus\internal\web\dashboard.go:20 — comment says default 8081
  - D:\Bubblefish\Nexus\cmd\bubblefish\install.go:337 — webPort := 8081

CLAIM F: examples/integrations/openwebui/ directory with Python pipeline
STATUS: NOT FOUND
EVIDENCE:
  - D:\Bubblefish\Nexus\examples\integrations\openwebui\ does NOT exist
  - Actual location: D:\Bubblefish\Nexus\examples\blessed\openwebui\
  - That directory contains: bubblefish_nexus_pipeline.py, bubblefish_nexus_pipeline.py.bak,
    bubblefish_ollama_pipe.py, SETUP.md
  - Files exist but README path is wrong.

CLAIM G: examples/integrations/openclaw/ with index.ts, package.json, openclaw.plugin.json
STATUS: NOT FOUND
EVIDENCE:
  - D:\Bubblefish\Nexus\examples\integrations\openclaw\ does NOT exist
  - Actual location: D:\Bubblefish\Nexus\examples\blessed\openclaw\
  - That directory contains: index.ts, package.json, openclaw.plugin.json, SETUP.md
  - All claimed files exist but README path is wrong.

CLAIM H: docs/integrations/ directory with per-client setup files
STATUS: NOT FOUND
EVIDENCE:
  - D:\Bubblefish\Nexus\docs\integrations\ does NOT exist
  - D:\Bubblefish\Nexus\docs\ contains only: air-gapped.md, dev-laptop.md, home-lab.md, internal/
  - No integrations subdirectory anywhere under docs/

CLAIM I: docs/THREAT_MODEL.md, docs/ARCHITECTURE.md, docs/DEPLOYMENT.md,
         docs/CONFIGURATION.md, docs/API.md, docs/BENCHMARKS.md exist
STATUS: PARTIAL
EVIDENCE:
  - THREAT_MODEL.md — EXISTS at repo root (D:\Bubblefish\Nexus\THREAT_MODEL.md), NOT in docs/
  - ARCHITECTURE.md — NOT FOUND anywhere
  - DEPLOYMENT.md — NOT FOUND anywhere
  - CONFIGURATION.md — NOT FOUND anywhere
  - API.md — NOT FOUND anywhere
  - BENCHMARKS.md — NOT FOUND anywhere
  Only 1 of 6 exists, and in the wrong location.

CLAIM J: LICENSE file at repo root with AGPL-3.0 text
STATUS: VERIFIED
EVIDENCE:
  - D:\Bubblefish\Nexus\LICENSE exists
  - Line 1: "GNU AFFERO GENERAL PUBLIC LICENSE"
  - Line 2: "Version 3, 19 November 2007"

CLAIM K: COMMERCIAL_LICENSE.md, CLA.md, CLA-CORPORATE.md, CONTRIBUTING.md,
         CODE_OF_CONDUCT.md, SECURITY.md, CHANGELOG.md at repo root
STATUS: PARTIAL
EVIDENCE:
  - CHANGELOG.md — EXISTS
  - COMMERCIAL_LICENSE.md — NOT FOUND
  - CLA.md — NOT FOUND
  - CLA-CORPORATE.md — NOT FOUND
  - CONTRIBUTING.md — NOT FOUND
  - CODE_OF_CONDUCT.md — NOT FOUND
  - SECURITY.md — NOT FOUND
  Only 1 of 7 exists.

CLAIM L: Credential routing feature (synthetic_key, credentials.mappings, bfn_sk_, bfn_route_)
STATUS: NOT FOUND
EVIDENCE:
  - Searched all of D:\Bubblefish\Nexus\internal\ for:
    "synthetic_key" — no matches
    "credentials.mappings" — no matches
    "bfn_sk_" — no matches
    "bfn_route_" — no matches
  - The install-generated key prefixes are bfn_admin_, bfn_data_, and bfn_mcp_ only.
  - The headline feature described in the README does not exist in the codebase.

========================================================
SUMMARY:
  VERIFIED:   6 (A, B, C, D, E, J)
  PARTIAL:    2 (I, K)
  NOT FOUND:  4 (F, G, H, L)

  Note: Claim D is verified in code but README Quick Start has wrong port number.
========================================================
