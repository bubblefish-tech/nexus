# Benchmark Summary — BubbleFish Nexus

## Environment

| Field       | Value                                          |
|-------------|------------------------------------------------|
| Version     | 0.1.2                                          |
| Git SHA     | a288863                                        |
| Timestamp   | 2026-04-08 15:30:20 UTC                        |
| OS          | windows                                        |
| Arch        | amd64                                          |
| CPU         | AMD Ryzen 7 7735HS with Radeon Graphics        |
| Go Version  | go1.26.1 windows/amd64                         |

## Run Statistics

- **Packages tested:** 28 (with tests), 7 skipped (no test files)
- **Packages with benchmarks:** 1 (`internal/firewall`)
- **Individual benchmarks run:** 1 (`BenchmarkPostFilter_1000Records`) x 5 iterations
- **Total wall time:** ~28s (all packages)
- **Failures / panics:** None

## Top 10 Slowest Benchmarks (by ns/op)

| # | Benchmark                           | ns/op   | B/op    | allocs/op |
|---|-------------------------------------|---------|---------|-----------|
| 1 | BenchmarkPostFilter_1000Records-16  | 129,215 | 335,874 | 1         |
| 2 | BenchmarkPostFilter_1000Records-16  | 117,295 | 335,874 | 1         |
| 3 | BenchmarkPostFilter_1000Records-16  | 116,407 | 335,874 | 1         |
| 4 | BenchmarkPostFilter_1000Records-16  | 106,526 | 335,874 | 1         |
| 5 | BenchmarkPostFilter_1000Records-16  |  74,708 | 335,875 | 1         |

## Top 10 Highest-Allocation Benchmarks (by allocs/op)

| # | Benchmark                           | allocs/op | B/op    | ns/op   |
|---|-------------------------------------|-----------|---------|---------|
| 1 | BenchmarkPostFilter_1000Records-16  | 1         | 335,875 | 74,708  |
| 2 | BenchmarkPostFilter_1000Records-16  | 1         | 335,874 | 106,526 |
| 3 | BenchmarkPostFilter_1000Records-16  | 1         | 335,874 | 117,295 |
| 4 | BenchmarkPostFilter_1000Records-16  | 1         | 335,874 | 116,407 |
| 5 | BenchmarkPostFilter_1000Records-16  | 1         | 335,874 | 129,215 |

## Failures

None.

## Output Files

- **Raw:**     `D:\BubbleFish\Nexus\Benchmarks\results\bench-0.1.2-20260408-153020-raw.txt`
- **Cleaned:** `D:\BubbleFish\Nexus\Benchmarks\results\bench-0.1.2-20260408-153020.txt`
- **Summary:** `D:\BubbleFish\Nexus\Benchmarks\results\bench-0.1.2-20260408-153020-summary.md`
