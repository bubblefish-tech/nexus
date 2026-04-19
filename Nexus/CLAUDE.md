# BubbleFish Nexus — Claude Code Project Instructions

## What This Project Is
BubbleFish Nexus v2.2 is a gateway-first AI memory daemon written in Go.
It sits between AI clients and memory databases, providing crash-safe,
policy-aware, retrieval-optimized memory management.

## Work Ledger (MANDATORY)

This repo maintains a build ledger at `BUILD_LEDGER.md` (repo root).

- **Before any code work:** read `BUILD_LEDGER.md` and confirm the current subtask against it.
- **After every subtask commit:** update `BUILD_LEDGER.md` in the same commit or in the immediately following commit.

The ledger is the single source of truth for which phase/subtask is active, which branches are in-flight, and what's merged. If the ledger and reality disagree, stop and reconcile before continuing.

## Go Module
github.com/BubbleFish-Nexus

## Build Commands
- Build: `go build ./...`
- Test: `CGO_ENABLED=1 go test ./... -race -count=1`
- Vet: `go vet ./...`

## Critical Rules (NEVER violate)
- WAL first, queue second, DB third. ALWAYS.
- All token comparisons use subtle.ConstantTimeCompare.
- All SQL uses parameterized queries. NEVER string concatenation.
- Temp WAL files in filepath.Dir(wal.path), NEVER os.TempDir().
- No hashicorp/golang-lru (license). Use zero-dep LRU with Go generics.
- No valyala/fastjson. Use encoding/json + sync.Pool.
- No secrets in logs at ANY level including DEBUG.
- Every file 0600, every directory 0700 for sensitive paths.
- Non-blocking channel: select { case ch <- v: default: } pattern.
- sync.Once for Drain/Stop. NEVER mutex + bool flag.
- No package-level vars for state. All state via struct fields.
- Error format: {"error":"code","message":"text","retry_after_seconds":N}

## Version
Public version is v0.1.3 (pre-1.0). Internal development version was 2.2.
Use the current version from internal/version/version.go in all code, docs, and CLI output.
CLI --version output: "bubblefish nexus v{version} (pre-1.0, API subject to change)"

## Testing
- Every package gets _test.go files.
- Table-driven tests preferred.
- Tests must pass `go test ./... -race`.
- t.Helper() on all test helpers.

## Style
- Structured logging via log/slog.
- Use filepath.Join for all paths.
- os.UserHomeDir() errors are fatal.

## Copyright Header (REQUIRED on every .go file)
Every .go file MUST start with this exact copyright header before the package declaration:

// Copyright © 2026 BubbleFish Technologies, Inc.
//
// This file is part of BubbleFish Nexus.
//
// BubbleFish Nexus is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// BubbleFish Nexus is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with BubbleFish Nexus. If not, see <https://www.gnu.org/licenses/>.

This header goes ABOVE the package line. No exceptions. Test files too.