# BubbleFish Phase 4 — Integration Guide

**Status:** All 6 deliverables written and doctor tests passing (17/17, zero races).  
**Next action:** Drop files into project, run `go mod tidy`, wire into daemon.

---

## 1. Drop Files Into Project

```
internal/destination/types.go          ← shared types (may already exist in core; see note)
internal/destination/openbrain.go
internal/destination/chromadb.go
internal/destination/postgres.go
internal/observability/metrics.go
internal/observability/metrics_test.go
internal/doctor/doctor.go
internal/doctor/diskspace_unix.go
internal/doctor/diskspace_windows.go
internal/doctor/doctor_test.go
cmd/doctor.go
```

**types.go note:** If your Phase 3 daemon already defines `TranslatedPayload`,
`QueryRequest`, `WriteError`, and `DestinationPlugin` in a shared package (e.g.
`internal/core/types.go`), **delete** `internal/destination/types.go` and update
the import paths in the three plugin files to point at your core package.
The types are deliberately re-declared in the destination package for Phase 4
standalone compilation — they're identical to the canonical versions.

---

## 2. Install Dependencies

```powershell
cd ~\Projects\bubblefish

go get github.com/jackc/pgx/v5@latest
go get github.com/prometheus/client_golang@latest
go mod tidy
```

---

## 3. Wire Metrics Into the Daemon HTTP Server

In `internal/daemon/daemon.go` (or wherever `Run()` lives):

```go
import "github.com/bubblefish/bubblefish/internal/observability"

// At startup, after mux is created:
metrics := observability.NewMetrics()
metrics.RegisterAll()
mux.Handle("/metrics", metrics.Handler())

// Pass metrics into your handler via closure or context.
```

**In the inbound POST handler** (`POST /inbound/{endpoint}`):

```go
// After determining statusCode:
metrics.IncRequest(sourceName, strconv.Itoa(statusCode))
```

**In the destination queue worker** (the goroutine that calls `plugin.Write()`):

```go
timer := metrics.StartWriteTimer(destName)
err := plugin.Write(payload)
timer.ObserveDuration()

if err != nil {
    kind := "transient"
    if destination.IsPermanent(err) {
        kind = "permanent"
    }
    metrics.IncError(destName, kind)
}
```

**Queue depth — add a ticker goroutine** next to your existing worker goroutines:

```go
go func() {
    tick := time.NewTicker(5 * time.Second)
    defer tick.Stop()
    for {
        select {
        case <-tick.C:
            metrics.SetQueueDepth(destName, len(destChannel))
        case <-ctx.Done():
            return
        }
    }
}()
```

**WAL metrics — update after every WAL append** in `internal/wal/wal.go`:

```go
// After successful append, count lines and file size:
metrics.SetWALEntries(w.entryCount)       // if you track this
metrics.SetWALSegmentSize(w.currentSize)  // fi.Size() from os.Stat
```

---

## 4. Wire Destinations Into Daemon Startup

In `daemon.Run()`, after loading compiled configs, instantiate plugins:

```go
import (
    "github.com/bubblefish/bubblefish/internal/destination"
)

// For each compiled destination config:
switch cfg.Type {
case "openbrain":
    plugin = destination.NewOpenBrainDestination(destination.OpenBrainConfig{
        BaseURL:        cfg.OpenBrain.BaseURL,
        Table:          cfg.OpenBrain.Table,
        APIKey:         os.Getenv("SUPABASE_API_KEY"),
        TimeoutSeconds: 10,
    })
case "chromadb":
    plugin = destination.NewChromaDBDestination(destination.ChromaDBConfig{
        BaseURL:        os.Getenv("CHROMADB_URL"),
        Collection:     cfg.ChromaDB.CollectionName,
        TimeoutSeconds: 15,
    })
case "postgres":
    plugin = destination.NewPostgresDestination(destination.PostgresConfig{
        ConnString:     os.Getenv("POSTGRES_CONN_STRING"),
        PoolSize:       cfg.Postgres.PoolSize,
        TimeoutSeconds: 10,
    })
}

if err := plugin.Connect(); err != nil {
    log.Fatalf("destination %s: connect failed: %v", cfg.Name, err)
}
```

---

## 5. Wire Doctor Into CLI Router

In `main.go` (or wherever you dispatch subcommands):

```go
case "doctor":
    configDir := ""
    if len(os.Args) > 2 {
        configDir = os.Args[2]
    }
    cmd.DoctorMain(configDir)
```

**Full integration** (replace the stub in `cmd/doctor.go`): once the daemon's
config loader is available, replace `buildDoctorConfig()` with a version that:

1. Loads `daemon.toml` from `configDir`
2. Loads compiled destination configs
3. Instantiates plugin instances and calls `Connect()` on each
4. Passes live plugins into `DoctorConfig.Destinations`
5. Reads actual ports from `daemon.toml` instead of hardcoding 8080/9100
6. Reads all source API keys from compiled sources

The doctor report will then show real pass/fail for every destination rather
than the "no destinations configured" stub message.

---

## 6. Environment Variables Required

Add these to `~/.bubblefish/.env` (and `.env.example`):

```env
# OpenBrain / Supabase
SUPABASE_URL=https://yourproject.supabase.co
SUPABASE_API_KEY=your-service-role-key

# ChromaDB
CHROMADB_URL=http://localhost:8000

# PostgreSQL
POSTGRES_CONN_STRING=postgresql://user:pass@host:5432/dbname
# sslmode=require is automatically enforced by the plugin — do not add it here
# unless you need a different mode (e.g. sslmode=verify-full)
```

---

## 7. Acceptance Criteria Verification

Run these after wiring is complete:

```powershell
# Build
go build ./...
go vet ./...
go test ./... -v -race

# Start daemon
.\bubblefish.exe daemon

# --- Second terminal ---

# Doctor
.\bubblefish.exe doctor
# Expected: checklist with ✓/✗ per check, exit 0 if all pass

# Metrics baseline
Invoke-RestMethod -Method GET -Uri http://localhost:9100/metrics
# Expected: bubblefish_* metrics in Prometheus text format

# Send a request
$h = @{ "Authorization" = "Bearer $env:CLAUDE_API_KEY"; "Content-Type" = "application/json" }
$b = '{"message":{"content":"metrics test"},"model":"test"}'
Invoke-RestMethod -Method POST -Uri http://localhost:8080/inbound/claude -Headers $h -Body $b

# Verify counter incremented
(Invoke-WebRequest -Uri http://localhost:9100/metrics).Content |
    Select-String 'bubblefish_requests_total'
# Expected: ...{source="claude",status="200"} 1
```

### ChromaDB permanent error test:

```powershell
# Send payload with no vector to a ChromaDB-backed source
# Expected: daemon logs PERMANENT error, payload dropped, no retry
# Queue worker must check destination.IsPermanent(err) — see section 3
```

### Postgres pool_size test:

```powershell
# Set pool_size = 3 in destinations/postgres.toml
# Run concurrent load — pgxpool.MaxConns will cap at 3
# Verify in pg_stat_activity: SELECT count(*) FROM pg_stat_activity WHERE application_name = 'bubblefish';
```

---

## 8. What Is NOT in Phase 4 (by design)

- No TUI, Web UI, or hot-reload (Phase 5+)
- No agentic chaining changes
- `cmd/doctor.go` uses a stub config loader — full wiring is a Phase 5 task
- SQLite destination is unchanged from Phase 3

---

## Phase 5 Preview

- Full daemon config loader integration for doctor
- WAL metrics hooks in `internal/wal/wal.go`
- Queue depth ticker goroutines per destination
- TUI dashboard (Bubble Tea) polling `/api/status`
- Basic Web UI (embedded HTML/JS)
- Hot-reload file watcher
