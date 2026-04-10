# Changelog

All notable changes to BubbleFish Nexus are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/). Versions follow [Semantic Versioning](https://semver.org/).

---

## [Unreleased — v0.1.3]

### Added
- OAuth 2.1 authorization server with PKCE support
- ChatGPT MCP integration verified end-to-end

### Fixed
- Consent page input sanitization hardened
- CORS handling on OAuth endpoints

---

## [0.1.2] - 2026-04-08

### Added
- LM Studio MCP integration
- 7-client simultaneous federation verified (Claude Desktop, Claude Web, ChatGPT Desktop, ChatGPT Web, Perplexity Comet, OpenWebUI, LM Studio)
- `bubblefish stop` command
- `bubblefish status --paths` subcommand

### Fixed
- MCP stdio transport reliability
- Source field mapping for OpenAI-compatible payloads
- WAL file handle cleanup on success path
- Install now generates MCP key automatically

### Changed
- MCP server rebuilt for broad client compatibility
- Version info now included in `nexus_status` response

---

## [0.1.1] - 2026-04-07

### Added
- `nexus_status` MCP tool
- Cloudflare Tunnel deployment guide
- Prometheus metrics
- `bubblefish doctor` diagnostic command

### Fixed
- WAL queue drain on restart after unclean shutdown

---

## [0.1.0] - 2026-04-06

### Initial release

- Single Go binary daemon, zero runtime dependencies
- HTTP API and MCP server with shared memory across all connected clients
- Write-ahead log with crash recovery, checksums, and optional encryption
- SQLite, PostgreSQL (pgvector), and Supabase destinations
- Multi-stage retrieval with temporal decay reranking
- Per-source API keys, policies, and rate limits
- Retrieval profiles (fast, balanced, deep)
- Simple Mode install (`bubblefish install --mode simple`)
- Web dashboard
- Built-in benchmarks and reliability demo
- Structured security event log
- Prometheus metrics
- AGPL-3.0 license
