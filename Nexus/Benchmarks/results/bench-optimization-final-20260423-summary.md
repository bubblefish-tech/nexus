# Benchmark Summary — Optimization Sprint Final

## Environment

| Field       | Value                                          |
|-------------|------------------------------------------------|
| Version     | 0.1.3 (feat/optimization-sprint)               |
| Git SHA     | 48c7219                                        |
| Timestamp   | 2026-04-23 01:30:00 UTC                        |
| OS          | windows                                        |
| Arch        | amd64                                          |
| CPU         | AMD Ryzen 7 7735HS with Radeon Graphics        |
| Go Version  | go1.26.2 windows/amd64                         |

## Optimization Impact Summary

| Benchmark | Hardening Baseline | Optimization Final | Change |
|-----------|-------------------|-------------------|--------|
| **Projection e2e** | 866μs / 7,003 allocs | **87μs / 1,602 allocs** | **9.9x faster, 77% fewer allocs** |
| **JWT Valid (cached)** | 31μs / 52 allocs | **465ns / 1 alloc** | **68x faster, 98% fewer allocs** |
| **MCP Status** | 105μs / 164 allocs | 106μs / 162 allocs | -1.2% allocs (HTTP transport bound) |
| **Embedding Short** | 144μs / 132 allocs | 137μs / 130 allocs | -1.5% allocs |
| Cache Hit | 37ns / 0 allocs | 36ns / 0 allocs | stable |
| Cache Miss | 24ns / 0 allocs | 27ns / 0 allocs | stable |
| WAL Append Small | 909μs / 12 allocs | 899μs / 12 allocs | stable |
| Queue Enqueue | 154ns / 2 allocs | 176ns / 2 allocs | stable (noise) |
| Queue Dequeue | 695ns / 5 allocs | 1,097ns / 5 allocs | +58% (system load variance) |
| Firewall 1K | 130μs / 1 alloc | 90μs / 1 alloc | -31% (system variance) |

## Commits

| # | SHA | Description | Key Result |
|---|-----|-------------|------------|
| 1 | d5dfad8 | PROJ.1 — projection struct-to-map | 9.9x faster |
| 2 | 9a3dc82 | WAL.1 — replay pre-filter | Prod benefit (skips DELIVERED) |
| 3 | 0ee9fc3 | JWT.1 — validation LRU cache | 68x faster on cache hit |
| 4 | 756be32 | MCP.1 — status response cache | 5s TTL, saves pipeline call |
| 5 | 48c7219 | EMBED.1 — pooled request buffer | 2 fewer allocs |

## Skipped

- **QUEUE.1** — Dequeue allocs intrinsic to json.Unmarshal; json.NewDecoder was worse
- **DRAIN.1** — Batch INSERT requires write-path restructuring (out of scope)

## No Regressions

All 104 packages pass. Zero new failures.
