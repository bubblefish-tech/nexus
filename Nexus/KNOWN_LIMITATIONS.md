# Known Limitations — BubbleFish Nexus v0.1.0

## Go 1.26.1 Race Detector Linker Bug

**Affects**: `go test -race` on packages that transitively import `modernc.org/sqlite`

**Symptom**: The Go 1.26.1 linker (`cmd/link`) panics during dead code elimination
when building race-instrumented test binaries:

```
panic: runtime error: index out of range [16777213] with length 11

goroutine 1 [running]:
cmd/link/internal/loader.(*Loader).resolve(...)
    cmd/link/internal/loader/loader.go:703
cmd/link/internal/ld.(*deadcodePass).flood(...)
    cmd/link/internal/ld/deadcode.go:287
```

**Root cause**: `modernc.org/sqlite` v1.48.1 contributes ~25,000 symbols from its
transpiled C code. When combined with race instrumentation (which roughly doubles
the symbol count to ~366,000), a package index overflows in the linker's auxiliary
symbol resolution (`r.pkg[p]` where `p = 16777213` and `len(r.pkg) = 11`).

**Scope**: Every package that transitively imports `modernc.org/sqlite`:
`destination`, `daemon`, `queue`, `cache`, `projection`, `query`, `mcp`,
`cmd/bubblefish`.

**Not affected**: `wal`, `config`, `idempotency`, `policy`, `metrics`,
`hotreload`, `doctor`, `embedding`, `tray`, `web` — these pass `-race` cleanly.

**Verification**: A completely empty test function (`t.Log("hello")`) in an affected
package triggers the identical crash. This confirms the bug is in the Go toolchain,
not in Nexus code.

**Workaround**: Run tests without `-race` on affected packages. The race detector
works correctly on the 10 unaffected packages, which cover the WAL, idempotency
store, config loader, policy engine, and other concurrency-critical paths.

```bash
# Full suite (all packages, no race):
CGO_ENABLED=1 go test ./... -count=1

# Race-safe packages only:
CGO_ENABLED=1 go test -race -count=1 \
  ./internal/wal/ \
  ./internal/config/ \
  ./internal/idempotency/ \
  ./internal/policy/ \
  ./internal/metrics/ \
  ./internal/hotreload/ \
  ./internal/doctor/ \
  ./internal/embedding/ \
  ./internal/tray/ \
  ./internal/web/
```

**Resolution**: Awaiting Go toolchain patch. This will be retested when Go 1.26.2
or a newer `modernc.org/sqlite` version ships.

## SQLite Write Serialization

SQLite enforces single-writer semantics (`MaxOpenConns(1)`). Under high concurrent
write load (e.g., 1000 writes), queue drain time scales linearly. This is acceptable
for personal/single-user deployments. PostgreSQL destination is recommended for
multi-user or high-throughput scenarios.

## In-Memory Cache Lost on Restart

The exact-match and semantic caches are in-memory only. Cache contents are lost on
daemon restart. TTL expiration and watermark invalidation ensure no stale data is
served after restart. Persistent cache is planned for v3.

## Source-Only Hot Reload

Source configuration changes are hot-reloaded without restart. Destination
configuration changes require a daemon restart. This is intentional: destination
changes affect durable state and are fail-safe by requiring explicit restart.
