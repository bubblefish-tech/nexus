# BubbleFish Nexus — Benchmark Results

## Layout

Results are stored in `results/` as:

```
bench-<version>-<YYYYMMDD>-<HHMMSS>-raw.txt   # Raw go test output
bench-<version>-<YYYYMMDD>-<HHMMSS>.txt        # Cleaned, benchstat-compatible
bench-<version>-<YYYYMMDD>-<HHMMSS>-summary.md # Human-readable summary
```

## Format

Output is standard Go `testing.B` format, compatible with
[`golang.org/x/perf/cmd/benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat).

## Comparing runs

```sh
benchstat results/bench-old.txt results/bench-new.txt
```

Install benchstat if needed:

```sh
go install golang.org/x/perf/cmd/benchstat@latest
```
