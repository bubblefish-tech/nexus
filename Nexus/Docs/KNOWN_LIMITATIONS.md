# Known Limitations — BubbleFish Nexus v0.1.3

This document is an honest accounting of current architectural and operational
limitations in BubbleFish Nexus. We publish it because transparency builds
trust, and because knowing what a system *cannot* do is as important as
knowing what it can.

## No Multi-Replica Consensus

Nexus v0.1.3 runs as a single-process daemon. There is no built-in
multi-replica consensus protocol (Raft, Paxos, etc.). The cluster scaffolding
in `internal/cluster/` defines the interfaces (`HACluster`, `LeaderElection`,
`Replicator`, `ConsensusProtocol`) but every method returns
`ErrFeatureLocked` in the Community edition.

**Impact**: No automatic failover. If the daemon process exits, memory
operations are unavailable until it restarts. The WAL ensures no data is lost
during unclean shutdown.

**Planned**: Multi-replica consensus is targeted for v0.3.

## No Geo-Replication

There is no cross-region or cross-datacenter replication. All data resides on
the node where the daemon runs.

**Impact**: Not suitable for deployments that require geographic redundancy or
disaster recovery across regions.

**Planned**: Geo-replication is targeted for v0.4+.

## SQLite Single-Writer Lock

Nexus uses SQLite as its default destination store with `MaxOpenConns(1)`.
SQLite enforces single-writer semantics at the file level. Under high
concurrent write load, queue drain time scales linearly with volume.

**Impact**: Write throughput is bounded by SQLite's single-writer lock.
Acceptable for personal, single-user, and small-team deployments.

**Mitigation**: For multi-user or high-throughput scenarios, configure a
PostgreSQL or MySQL destination instead. The daemon supports multiple
destination backends via `nexus.toml`.

## No FIPS 140-2 Validated Crypto

Nexus uses Go's standard library `crypto` packages and `golang.org/x/crypto`.
These are not FIPS 140-2 validated. The `internal/crypto/` package implements
AES-256-GCM encryption, Ed25519 signing, and X25519 key exchange using
standard Go primitives.

**Impact**: Nexus cannot be deployed in environments that mandate FIPS 140-2
validated cryptographic modules (e.g., certain U.S. federal systems).

**Mitigation**: The `edition.FeatureFIPS` flag exists in the edition system
but is not enabled in the Community build. FIPS-validated builds are planned
for the Enterprise edition.

## FTS5 BM25 English-Optimized

Full-text search uses SQLite FTS5 with BM25 ranking. The default tokenizer
(`unicode61`) and ranking function are optimized for English text. Non-English
languages may produce suboptimal relevance ranking due to:

- No language-specific stemming (FTS5 uses simple Unicode tokenization)
- BM25 term-frequency statistics tuned for English corpus distributions
- No CJK bigram tokenization (requires custom tokenizer)

**Impact**: Retrieval quality for non-English content may be lower than
expected. Semantic search (via embeddings) is language-agnostic and partially
compensates for this.

**Mitigation**: The cascade query pipeline (Stages 1-6) blends FTS5 results
with embedding-based semantic search, reducing dependence on BM25 alone.

## Embedding Requires Local Model or External API

Nexus does not ship a built-in embedding model. To use semantic search,
embedding-based clustering, or LSH bucket assignment, you must configure
either:

1. A local embedding model endpoint (e.g., Ollama, llama.cpp server)
2. An external embedding API (e.g., OpenAI, Cohere, Voyage)

**Impact**: Without an embedding provider, the following features are
unavailable:
- Semantic search (cascade Stage 4)
- LSH bucket prefiltering
- Cluster assignment (`internal/cluster/`)
- Embedding-based deduplication

**Mitigation**: Configure `[embedding]` in `nexus.toml`. The daemon operates
correctly without embeddings — FTS5 full-text search remains available as
the primary retrieval path. See `docs/CONFIGURATION.md` for setup.

## Go 1.26.1 Race Detector Linker Bug

`go test -race` crashes on packages that transitively import
`modernc.org/sqlite` due to a linker bug in Go 1.26.1. This is a Go
toolchain issue, not a Nexus bug. See the root `KNOWN_LIMITATIONS.md` for
the full technical writeup and workaround commands.

**Resolution**: Awaiting Go 1.26.2 or a patched `modernc.org/sqlite` release.
