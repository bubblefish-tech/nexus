# Benchmark Summary — Post PROJ.1 Optimization

## Environment

| Field       | Value                                          |
|-------------|------------------------------------------------|
| Version     | 0.1.3 (feat/optimization-sprint)               |
| Git SHA     | d5dfad8                                        |
| Timestamp   | 2026-04-23 01:03:00 UTC                        |
| OS          | windows                                        |
| Arch        | amd64                                          |
| CPU         | AMD Ryzen 7 7735HS with Radeon Graphics        |
| Go Version  | go1.26.2 windows/amd64                         |

## PROJ.1 Impact — Projection Stage

| Metric     | Hardening Baseline | Post-PROJ.1 | Change |
|------------|-------------------|-------------|--------|
| ns/op      | 866,324           | 85,448      | **-90% (9.9x faster)** |
| B/op       | 411,419           | 145,040     | **-65%** |
| allocs/op  | 7,003             | 1,602       | **-77%** |

## All Benchmarks (median of 3)

| Benchmark | ns/op | B/op | allocs/op |
|-----------|-------|------|-----------|
| WAL_Append_SmallEntry | 908,492 | 2,147 | 12 |
| WAL_Append_LargeEntry | 938,324 | 15,193 | 12 |
| WAL_Append_Batch100 | 94,317,250 | 212,978 | 1,104 |
| WAL_Replay_1000Entries | 7,875,556 | 13.1MB | 20,068 |
| WAL_MarkStatus | 16,367,521 | 10.7MB | 1,653 |
| Queue_Enqueue_Single | 154.3 | 74 | 2 |
| Queue_Dequeue_Single | 695.1 | 561 | 5 |
| Queue_DrainToSQLite_100 | 162,728 | 131KB | 1,142 |
| Cache_Hit | 37.23 | 0 | 0 |
| Cache_Miss | 24.22 | 0 | 0 |
| Cache_Set | 875.2 | 552 | 11 |
| Embedding_ShortText | 144,000 | 19,080 | 132 |
| Embedding_ParagraphText | 199,327 | 19,746 | 133 |
| MCP_NexusStatus | 105,340 | 13,760 | 164 |
| MCP_NexusWrite | 165,022 | 15,548 | 180 |
| MCP_NexusSearch | 209,901 | 40,808 | 218 |
| Projection_Stage_End2End | 85,448 | 145,040 | 1,602 |
| PostFilter_1000Records | 130,407 | 425,988 | 1 |
| JWT_Validate_ValidToken | 35,013 | 3,424 | 52 |
| JWT_Validate_ExpiredToken | 42,561 | 3,416 | 52 |

## No Regressions

All non-projection benchmarks within noise of hardening baseline.
