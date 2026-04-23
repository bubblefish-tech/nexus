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
github.com/bubblefish-tech/nexus

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
CLI --version output: "nexus v{version} (pre-1.0, API subject to change)"

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

# Nexus TUI - Claude Code Instructions

## Project
BubbleFish Nexus terminal user interface. Go project using the Charm ecosystem.

## Available Libraries
All of these are already in go.mod — USE THEM instead of building from scratch:

- **charm.land/bubbletea/v2** — Core TUI framework (Elm architecture: Model/Update/View)
- **charm.land/bubbles/v2** — Pre-built components: use these FIRST before writing custom ones
  - textinput, textarea — text entry
  - viewport — scrollable content
  - list — filterable lists with delegation
  - table — column-based data display
  - spinner — loading indicators
  - progress — progress bars
  - paginator — page navigation
  - filepicker — file selection
  - help — keybinding help display
  - key — keybinding definitions
- **charm.land/lipgloss/v2** — Styling and layout. This is our CSS equivalent.
- **charm.land/huh/v2** — Interactive forms (text inputs, selects, confirms, multi-selects)
- **github.com/charmbracelet/harmonica** — Physics-based animations (spring, smooth damping)
- **charm.land/log/v2** — Structured logger

## Architecture Rules
- Every screen/view is its own Model in its own file under /internal/tui/
- The root Model holds a state enum and delegates to the active sub-model
- All Lip Gloss styles go in styles.go — never inline style definitions
- All keybindings go in keys.go using the bubbles/key package
- Messages flow through root Update, then dispatch to active sub-model

## Critical Patterns
- ALWAYS handle tea.WindowSizeMsg — store width/height on model, propagate to sub-models
- ALWAYS handle tea.KeyMsg for "q", "esc", ctrl+c at root level
- Viewport content MUST be set AFTER setting viewport dimensions, not before
- Never return nil from View() — return "" at minimum
- Use lipgloss.Place() and lipgloss.JoinVertical/JoinHorizontal for layout
- Use list.DefaultDelegate or implement list.ItemDelegate — don't hand-roll list rendering
- Use viewport.Model for any scrollable content — don't hand-roll scroll logic
- Use table.Model for tabular data — don't hand-roll column alignment

## Debugging
- Use tea.LogToFile("debug.log", "debug") when DEBUG env var is set
- Run `go build ./...` after every change
- Run `go test ./internal/tui/...` to validate
- Write teatest tests for each view before implementing

## File Structure
internal/tui/
├── root.go          # root model, state machine, message dispatch
├── styles.go        # all lipgloss styles
├── keys.go          # all keybinding definitions
├── statusbar.go     # persistent status bar component
├── agentlist.go     # agent listing view
├── memoryview.go    # memory search and display
├── dashboard.go     # main dashboard/overview
└── common.go        # shared types, helper functions

## Don'ts
- Do NOT build custom components when Bubbles has one
- Do NOT put everything in one file
- Do NOT use fmt.Print or log.Print — use tea.LogToFile
- Do NOT hardcode terminal dimensions — always use stored width/height
- Do NOT create styles inline in View() — define them in styles.go

## ASCII/ANSI Art Tools

- **ansizalizer** is installed at D:\ansizalizer\ansizalizer.exe
  - Converts PNG/JPG images to ANSI terminal art
  - Exports .ansi files that can be embedded as string literals in Go
  - Use it to generate the BubbleFish fish emblem for the TUI logo
  - The output .ansi file is raw ANSI escape codes — read it with os.ReadFile and print directly
  - For the logo in logo.go, store the .ansi output as a const string or embed via go:embed