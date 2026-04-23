# Benchmark Summary — BubbleFish Nexus (post-hardening)

## Environment

| Field       | Value                                          |
|-------------|------------------------------------------------|
| Version     | 0.1.3 (feat/hardening-complete)                |
| Git SHA     | f80c6ce                                        |
| Timestamp   | 2026-04-23 00:34:00 UTC                        |
| OS          | windows                                        |
| Arch        | amd64                                          |
| CPU         | AMD Ryzen 7 7735HS with Radeon Graphics        |
| Go Version  | go1.26.1 windows/amd64                         |

## Run Statistics

- **Packages tested:** 8 (with benchmarks)
- **Individual benchmarks run:** 17 unique × 3 iterations = 51 runs
- **Total wall time:** ~160s
- **Failures / panics:** None

## Results (median of 3 runs)

### WAL (internal/wal)

| Benchmark                      | ns/op       | B/op    | allocs/op |
|--------------------------------|-------------|---------|-----------|
| WAL_Append_SmallEntry          | 912,583     | 2,148   | 12        |
| WAL_Append_LargeEntry          | 899,199     | 15,200  | 12        |
| WAL_Append_Batch100            | 89,095,785  | 212,997 | 1,103     |
| WAL_Replay_1000Entries         | 6,363,465   | 13.1 MB | 20,068    |
| WAL_MarkStatus                 | 12,893,272  | 10.8 MB | 1,880     |

### Queue (internal/queue)

| Benchmark                      | ns/op    | B/op    | allocs/op |
|--------------------------------|----------|---------|-----------|
| Queue_Enqueue_Single           | 125.3    | 70      | 2         |
| Queue_Dequeue_Single           | 647.1    | 556     | 5         |
| Queue_DrainToSQLite_100        | 218,037  | 131 KB  | 1,143     |

### Cache (internal/cache)

| Benchmark                      | ns/op  | B/op | allocs/op |
|--------------------------------|--------|------|-----------|
| Cache_Hit                      | 37.31  | 0    | 0         |
| Cache_Miss                     | 26.74  | 0    | 0         |
| Cache_Set                      | 1,016  | 608  | 11        |

### Embedding (internal/embedding)

| Benchmark                            | ns/op    | B/op   | allocs/op |
|--------------------------------------|----------|--------|-----------|
| Embedding_Generate_ShortText         | 137,954  | 18,950 | 132       |
| Embedding_Generate_ParagraphText     | 170,928  | 19,696 | 133       |

### MCP (internal/mcp)

| Benchmark                      | ns/op    | B/op   | allocs/op |
|--------------------------------|----------|--------|-----------|
| MCP_NexusStatus                | 101,439  | 13,567 | 164       |
| MCP_NexusWrite_SmallMemory     | 155,949  | 15,529 | 180       |
| MCP_NexusSearch_10Results      | 188,356  | 40,986 | 218       |

### Projection (internal/projection)

| Benchmark                      | ns/op    | B/op    | allocs/op |
|--------------------------------|----------|---------|-----------|
| Projection_Stage_End2End       | 866,324  | 411 KB  | 7,003     |

### Firewall (internal/firewall)

| Benchmark                      | ns/op    | B/op    | allocs/op |
|--------------------------------|----------|---------|-----------|
| PostFilter_1000Records         | 121,698  | 426 KB  | 1         |

### JWT Auth (internal/jwtauth)

| Benchmark                      | ns/op  | B/op  | allocs/op |
|--------------------------------|--------|-------|-----------|
| JWT_Validate_ValidToken        | 31,202 | 3,424 | 52        |
| JWT_Validate_ExpiredToken      | 31,168 | 3,416 | 52        |

## Comparison vs v0.1.2 (2026-04-08)

| Benchmark                 | v0.1.2 (ns/op) | Hardening (ns/op) | Change |
|---------------------------|-----------------|-------------------|--------|
| PostFilter_1000Records    | 117,295         | 121,698           | +3.8%  |

Only one benchmark overlaps with the v0.1.2 run (firewall). The +3.8% is within noise (v0.1.2 ran 5 iterations ranging 106K–129K; hardening median 121K falls within that range).

## Notes

- WAL append is ~900μs, dominated by fsync — the durability cost
- Cache hits are zero-alloc (37ns), misses 27ns — no overhead on the read path
- Queue enqueue is sub-microsecond (125ns) — channel-based design is efficient
- MCP round-trips 100–190μs including full JSON-RPC encode/decode
- No regressions detected from the hardening sprint
