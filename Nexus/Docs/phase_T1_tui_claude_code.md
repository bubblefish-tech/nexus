# Phase T-1 — `bubblefish tui` Bubble Tea TUI

**BubbleFish Nexus · v0.1.x · Claude Code Implementation Prompt**

> Feed this entire file as a single Claude Code prompt after all previous phases are complete and all 31 packages are green.

---

## Context

You are building BubbleFish Nexus v0.1.x in Go. All previous build phases are complete. All 31 packages pass:

```powershell
$env:CGO_ENABLED='1'; go test ./... -race -count=1
```

**Repo:** `D:\BubbleFish\Nexus`

**Task:** Implement the `bubblefish tui` subcommand — a full Bubble Tea terminal UI exposing the complete operational surface of the running Nexus daemon through 7 tabs, a persistent sidebar, live metric refresh, and vim-fluent keybindings.

The TUI connects **exclusively** to the admin API on port 8080. It never calls MCP (7474) or the web dashboard (8081). It reads no files from disk at runtime. All data comes from HTTP. The TUI is **100% read-only** — it never calls POST, PUT, DELETE, or `/api/replay` or `/api/shutdown`.

**Constraint:** Do NOT add any new Go modules. Bubble Tea, bubbles, and lipgloss are already in go.mod. Use them.

---

## Visual Reference

The HTML file `bubblefish_nexus_tui_10M.html` (provided separately) is the exact visual target. Match its color system, layout proportions, component shapes, and interaction patterns precisely. Key design tokens from that reference:

```
--bg:     #0a0e14   (base background)
--bg2:    #0d1219   (panel background)
--bg3:    #111820   (row / hover background)
--teal:   #00b4d8   (primary brand — active tabs, focus borders, live dots)
--green:  #3dd68c   (healthy / ok)
--amber:  #f0a500   (warn / degraded)
--red:    #e05555   (error / critical)
--purple: #a78bfa   (security events, user prov badge)
--blue:   #4d9de0   (info)
--text:   #cdd9e5   (primary text)
--text2:  #8b9db0   (secondary text)
--text3:  #4a6072   (muted)
--text4:  #2d4055   (dim / labels)
```

Rounded borders on all panels. Block chars for sparklines: `▁▂▃▄▅▆▇█`. Prov badges: user=purple bg, agent=teal bg, system=gray bg.

---

## Package Layout

Create every file listed below. Create files before modifying them.

| File | Responsibility |
|------|----------------|
| `cmd/bubblefish/tui.go` | Cobra subcommand entry: load config (admin_token, bind addr via `config.LoadDaemon()`), honor `BUBBLEFISH_HOME` + `--home` flag, launch `tea.NewProgram(model)` |
| `internal/tui/model.go` | Root Elm model: `activeTab int`, sidebar, per-tab sub-models, tickMsg, errMsg |
| `internal/tui/update.go` | Root `Update()`: route KeyMsg to active tab, handle tickMsg (fire API calls), handle errMsg |
| `internal/tui/view.go` | Root `View()`: lipgloss layout — sidebar + main pane + statusbar. Render active tab. |
| `internal/tui/styles/styles.go` | ALL lipgloss styles and colors. No `lipgloss.Color()` calls anywhere else. |
| `internal/tui/api/client.go` | HTTP admin API client — pure HTTP, no rendering, no business logic |
| `internal/tui/api/types.go` | Response structs for every admin endpoint |
| `internal/tui/components/statcard.go` | StatCard component |
| `internal/tui/components/inlinebar.go` | InlineBar component |
| `internal/tui/components/sparkline.go` | Sparkline component |
| `internal/tui/components/stageflow.go` | StageFlow component |
| `internal/tui/components/logtable.go` | LogTable scrollable viewport |
| `internal/tui/components/heatgrid.go` | HeatGrid component |
| `internal/tui/components/prov.go` | ProvBadge component |
| `internal/tui/components/pill.go` | PillStatus component |
| `internal/tui/components/section.go` | SectionTitle component |
| `internal/tui/components/sidebar.go` | Sidebar component |
| `internal/tui/components/statusbar.go` | Statusbar component |
| `internal/tui/tabs/control.go` | Tab 1 — Control |
| `internal/tui/tabs/audit.go` | Tab 2 — Audit |
| `internal/tui/tabs/security.go` | Tab 3 — Security |
| `internal/tui/tabs/pipeline.go` | Tab 4 — Pipeline |
| `internal/tui/tabs/conflicts.go` | Tab 5 — Conflicts |
| `internal/tui/tabs/timetravel.go` | Tab 6 — Time-Travel |
| `internal/tui/tabs/settings.go` | Tab 7 — Settings |

---

## Architecture

### Elm Model

```go
// Root model
type Model struct {
    activeTab   int
    tabs        []tab.Tab  // interface: Init, Update, View
    sidebar     sidebar.Model
    statusbar   statusbar.Model
    client      *api.Client
    width       int
    height      int
    lastErr     error
    tickEnabled bool
}

// Tick-driven API calls use commands, never goroutines
type tickMsg time.Time
type apiResponseMsg[T any] struct { data T; err error }

func tickCmd() tea.Cmd {
    return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}
```

Each tab is a sub-model with its own `Init / Update / View`. Root `Update` fans messages to the active tab's `Update` and merges commands. Tabs communicate up via custom Msg types — never by mutating shared state directly.

### Layout Math

Terminal width is dynamic. Reflow correctly from 80 cols (minimum) to 220+ cols. Use `WindowSizeMsg` to store width/height in root model and pass sizes down to each tab's `View` call.

| Region | Width | Height | Notes |
|--------|-------|--------|-------|
| Sidebar | 22 cols fixed | full − 3 (titlebar) − 2 (tabbar) − 1 (statusbar) | Scrollable. Fixed left. Collapsible via `H`. |
| Main pane | width − 22 cols (or full if collapsed) | same as sidebar | Each tab renders here. |
| Statusbar | full width | 1 row | Always visible. Key hints at right. |
| Tabbar | full width | 2 rows | Tab name + hotkey number. Active tab highlighted teal. |
| Metric cards | `floor(main_width / 4)` wide, 6 rows each | — | 4 per row when `main_width >= 80`, else 2 per row. |

### Refresh Strategy

| Tab | Endpoint(s) | Refresh |
|-----|------------|---------|
| 1 Control | `/api/status`, `/health`, `/ready`, `/api/cache` | 5s tick |
| 2 Audit | `/api/security/events?limit=200` | 5s tick |
| 3 Security | `/api/policies`, `/api/security/summary`, `/api/lint` | 30s tick |
| 4 Pipeline | `/api/status` (viz stats) | 5s tick |
| 5 Conflicts | `/api/conflicts?limit=50` | 30s tick |
| 6 Time-Travel | `/api/timetravel?as_of=RFC3339&subject=&limit=20` | Manual (Enter key only — never ticked) |
| 7 Settings | `/api/status` (config fields), `/api/policies` | 60s tick |

Inactive tabs do NOT refresh — they refresh once on first activation (lazy init).

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tickMsg:
        cmd := m.tabs[m.activeTab].FireRefresh(m.client)
        return m, tea.Batch(cmd, tickCmd())
    case tea.WindowSizeMsg:
        m.width, m.height = msg.Width, msg.Height
        // propagate to all tabs
        ...
    }
}
```

---

## API Client

File: `internal/tui/api/client.go`

```go
package api

type Client struct {
    base  string       // http://127.0.0.1:8080
    token string       // admin_token from daemon.toml
    http  *http.Client // 5s timeout
}

func (c *Client) Status() (*StatusResponse, error)
func (c *Client) Cache() (*CacheResponse, error)
func (c *Client) Policies() (*PoliciesResponse, error)
func (c *Client) Lint() (*LintResponse, error)
func (c *Client) SecurityEvents(limit int) (*SecurityEventsResponse, error)
func (c *Client) SecuritySummary() (*SecuritySummaryResponse, error)
func (c *Client) Conflicts(opts ConflictOpts) (*ConflictsResponse, error)
func (c *Client) TimeTravel(opts TimeTravelOpts) (*TimeTravelResponse, error)
func (c *Client) Health() (bool, error)
func (c *Client) Ready() (bool, error)
```

Config loading: use the same `config.LoadDaemon()` function already used by other commands. Honor `BUBBLEFISH_HOME` env var and `--home` flag.

If `/health` returns non-200 or connection refused: show a full-screen "Daemon not running. Start with: `bubblefish start`" error state with retry count. `r` retries immediately. `q` quits. Do not crash.

---

## Style System

File: `internal/tui/styles/styles.go` — all colors and computed styles live here. Zero `lipgloss.Color()` calls anywhere else.

```go
package styles

import "github.com/charmbracelet/lipgloss"

var (
    // Background layers
    BgBase  = lipgloss.Color("#0a0e14")
    BgPanel = lipgloss.Color("#0d1219")
    BgRow   = lipgloss.Color("#111820")
    BgHover = lipgloss.Color("#151d27")

    // Borders
    BorderBase  = lipgloss.Color("#1e2d3d")
    BorderHover = lipgloss.Color("#2d4055")
    BorderFocus = lipgloss.Color("#00b4d8")

    // Text
    TextPrimary   = lipgloss.Color("#cdd9e5")
    TextSecondary = lipgloss.Color("#8b9db0")
    TextMuted     = lipgloss.Color("#4a6072")
    TextDim       = lipgloss.Color("#2d4055")

    // Semantic
    ColorGreen  = lipgloss.Color("#3dd68c")
    ColorTeal   = lipgloss.Color("#00b4d8")
    ColorBlue   = lipgloss.Color("#4d9de0")
    ColorAmber  = lipgloss.Color("#f0a500")
    ColorRed    = lipgloss.Color("#e05555")
    ColorPurple = lipgloss.Color("#a78bfa")

    // Computed styles
    ActiveTab = lipgloss.NewStyle().
        Foreground(ColorTeal).
        BorderBottom(true).
        BorderForeground(ColorTeal)

    PanelBorder = lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(BorderBase)

    StatValue = lipgloss.NewStyle().
        Bold(true).
        Foreground(TextPrimary)
)
```

---

## Shared Components

| Component | File | Description |
|-----------|------|-------------|
| StatCard | `components/statcard.go` | Label (uppercase 9px), Value (22px bold), subtitle line, bottom accent stripe (colored by severity). Width: `floor(main/4)`. |
| InlineBar | `components/inlinebar.go` | Single track + fill + right-aligned value label. Height: 1 row. Fill color parameterized. Width: stretch. |
| Sparkline | `components/sparkline.go` | 12–20 bar mini chart. Bars are block characters `▁▂▃▄▅▆▇█`. Max value normalizes. Color parameterized. |
| StageFlow | `components/stageflow.go` | N boxes connected by `›` glyphs. Each box: 4 lines (stage num, name, status, latency). HIT/SKIP state changes border and color. |
| LogTable | `components/logtable.go` | Scrollable viewport (`bubbles/viewport`). Rows: left-border colored by log level. Columns: ts, src, msg, code. Supports text filter. |
| HeatGrid | `components/heatgrid.go` | NxM grid of colored cells. Values map to 5 intensity stops (`■` chars). Legend row below. |
| ProvBadge | `components/prov.go` | Inline colored pill: user (purple), agent (teal), system (gray). 3 chars wide. |
| PillStatus | `components/pill.go` | LIVE/IDLE/DEAD/OK/WARN/ERR pills with lipgloss borders and background shading. |
| SectionTitle | `components/section.go` | Uppercase gray label with bottom border. Used as panel header. |
| Sidebar | `components/sidebar.go` | Fixed-width left pane. Scrollable. Sections with headers. Items with name + right-aligned value + optional dot indicator. |
| Statusbar | `components/statusbar.go` | Full-width bottom strip. Left: key-value pairs (status, wal, consistency, sources, wps, rps, cache). Right: key hint cheat-sheet. |

---

## Tab Specifications

### Tab 1 — Control

Landing tab. Every critical health signal visible without scrolling on a 120-col terminal.

| Panel | Position | Content |
|-------|----------|---------|
| Metric Cards Row 1 | Top, 4 cards | daemon status (● LIVE / ● DOWN), queue_depth, wal_pending_entries, consistency_score. Color: green=healthy, amber=degraded, red=critical. |
| Metric Cards Row 2 | Second row, 4 cards | writes/sec (with sparkline), reads/sec (with sparkline), exact cache hit%, p99 write latency |
| Sources Table | Middle-left, 60% width | Per-source: name, status pill, write count, read count, actor type (prov badge), profile, rate/min (inline bar) |
| WAL Panel | Middle-right, 40% width | integrity mode, encryption, pending entries, last replay ms, disk free, segment size, watchdog health, append p99. Mini bar chart of writes per 3s (20 bars). |
| Cache Performance Table | Lower-left | Exact LRU and semantic cache: hits, misses, hit rate (inline bar), evictions |
| MCP Server Panel | Lower-right | protocol, transport, protocolVersion, tools exposed, tunnel status, active sessions |
| Write Path Stepper | Near bottom | 9 numbered steps: `1.auth → 2.canWrite → 3.idempotency → 4.rate_limit → 5.WAL_append → 6.queue_send → 7.dest_write → 8.DELIVERED → 9.event_sink`. Each step: name, status (✓/⚠/✗), latency_ms. Source from `/api/status` last_write_op fields. |
| Write Activity Heatmap | Bottom | 24 cells (hourly, oldest left). 5 intensity levels: 0=empty (#1e2d3d), 1–10=dim green, 11–50=mid, 51–100=bright, 100+=max. Source: hourly_writes[] from status, or estimate from total_writes/uptime_hours. |

### Tab 2 — Audit

| Element | Detail |
|---------|--------|
| Filter bar | Top strip: filter buttons (all / 200 / 401-403 / WAL / security / debug). Active filter highlighted teal. `/` opens text filter input. |
| Column headers | time (64px) · source (80px) · action (flex) · result code (right-aligned) |
| Row coloring | Left border: blue=info, green=ok, amber=warn, red=err, purple=security event |
| Content | timestamp, source, actor_type, subject namespace, endpoint, status code, latency_ms, error_code. Content truncated at 80 chars. |
| Count strip | Top-right: `N events · last 24h · auto-refresh 5s` |
| Scroll | j/k/ctrl+d/ctrl+u. g=top, G=bottom. Auto-scroll to bottom on new events (toggleable with `a`). |

### Tab 3 — Security

| Panel | Content |
|-------|---------|
| Hardening Matrix | Feature grid: WAL integrity mode, WAL encryption, config signing, HMAC, provenance tracking, rate limiting, audit engine, TLS. Each: feature name + status pill (ENABLED/DISABLED/PARTIAL). |
| Source Policies Table | Per-source: source name, can_read, can_write, allowed_destinations, rate_limit/min, token type. |
| Auth Failures | auth_failures_total, policy_denials_total from `/api/security/summary`. Trend sparkline. |
| Lint Warnings | All warnings from `/api/lint`. Color: amber=warn, red=err. Count badge in tab header. |
| Recent Security Events | Last 10 events filtered to security category. |

### Tab 4 — Pipeline

| Panel | Content |
|-------|---------|
| 6-Stage Cascade | 6 boxes in a row connected by `›`. Per box: stage N (header), stage name, HIT/SKIP/MISS status, latency, candidate count. HIT: teal border + teal bg tint. SKIP: dim. MISS: amber border. Source: `/api/status` retrieval_stats. |
| Black Box Mode (`b`) | Replace 6-box row with single box: `Memory Request ──[Nexus]──› Response  total: Xms  results: N` |
| Per-Stage Latency | Table: stage name, p50, p99, hit count, skip count, miss count. |
| Throughput Gauges | wps (inline bar), rps (inline bar), temporal decay applied count, event sink delivered/failed. |
| Network Topology | Source → Nexus → Destination flows with arrow indicators and write counts. |

### Tab 5 — Conflicts

| Element | Detail |
|---------|--------|
| Conflict cards | Per conflict: subject (amber header), entity_key, two or more entries each showing: source, content (truncated 80 chars), timestamp, decay_score (color: green>0.7, amber 0.4–0.7, red<0.4), actor_type (prov badge). |
| Navigation | `p`/`n` previous/next conflict group. Count: `Conflict N of M`. |
| Copy | `c` copies conflict subject to clipboard via OSC 52. |
| Resolution hint | Per conflict: which source wins based on decay scores (highest wins). |

### Tab 6 — Time-Travel

| Element | Detail |
|---------|--------|
| Controls bar | `as_of` RFC3339 input (editable, `bubbles/textinput`), subject filter input, profile pills (fast/balanced/deep), Enter to query, Esc to cancel. |
| State banner | `Showing memory state at [timestamp] — N entries — WHERE timestamp <= as_of — read-only — no data modification` |
| Result table | # · payload_id (short) · subject · content (truncated 60 chars) · source · actor_type (prov badge) · timestamp · decay_score (color: green>0.7, amber 0.4–0.7, red<0.4) |
| Metadata footer | `stage=timetravel · result_count=N · as_of=... · consistency_score=... · temporal_decay_applied=...` |
| Empty state | `No memories existed at [timestamp]. Try an earlier timestamp or remove the subject filter.` |

### Tab 7 — Settings

Read-only display of all effective daemon.toml values from `/api/status` and `/api/policies`. Grouped by section:

> Daemon · WAL · WAL Integrity · WAL Encryption · WAL Watchdog · Rate Limiting · Embedding · MCP Server · Web Dashboard · TLS · Retrieval · Consistency Assertions · Security Events · Sources (one sub-group per source)

Footer text: `Settings are read-only in TUI. To change: edit daemon.toml → bubblefish build → restart daemon (or bubblefish dev for source-only hot-reload).`

`e` key prints this instruction to the terminal after TUI exits.

---

## Global Keybindings

| Key | Scope | Action |
|-----|-------|--------|
| `1`–`7` | Global | Switch to tab N |
| `q` / `ctrl+c` | Global | Quit (`tea.Quit`) |
| `?` | Global | Toggle help overlay |
| `r` | Global | Force refresh active tab |
| `tab` | Global | Next tab |
| `shift+tab` | Global | Previous tab |
| `j`/`k`/`↑`/`↓` | Any scrollable | Scroll down/up one row |
| `ctrl+d`/`ctrl+u` | Any scrollable | Scroll half-page down/up |
| `g`/`G` | Any scrollable | Jump to top/bottom |
| `/` + text + Enter | Audit, Security, Conflicts | Set filter. Esc to clear. |
| `esc` | Filter input, TT input | Cancel / clear input |
| `Enter` | Time-Travel input | Execute query |
| `f` | Pipeline tab | Toggle fast/balanced/deep retrieval profile display |
| `b` | Pipeline tab | Toggle Black Box Mode |
| `p`/`n` | Conflicts tab | Previous/next conflict group |
| `c` | Conflict detail | Copy subject to clipboard (OSC 52) |
| `e` | Settings tab | Print edit instruction |
| `ctrl+r` | Global | Toggle auto-refresh on/off. Show `[PAUSED]` in statusbar when paused. |
| `ctrl+l` | Global | Clear log scroll buffer (Audit tab) |
| `H` | Global | Toggle sidebar collapse |
| `a` | Audit tab | Toggle auto-scroll to bottom |

---

## Implementation Constraints

| Topic | Requirement |
|-------|-------------|
| Bubble Tea | Use `charmbracelet/bubbletea` (already in go.mod). Use `bubbles/viewport` for scrollable panes, `bubbles/textinput` for time-travel and filter inputs. |
| Lipgloss | Use `charmbracelet/lipgloss` (already in go.mod). All layout via `lipgloss.JoinHorizontal` / `JoinVertical`. No direct terminal escape sequences. |
| No new dependencies | Do NOT add any new Go modules. go.mod must be unchanged from pre-phase. |
| Terminal safety | Never write to stdout/stderr directly inside the TUI. Use `tea.Println` for debug output. All errors displayed inline in the relevant panel, never crashing. |
| Daemon not running | If `/health` returns non-200 or connection refused: show full-screen error state with retry count. `r` retries. `q` quits. No crash. |
| Alt screen | Use `tea.WithAltScreen()` — TUI must not pollute the terminal scroll buffer. Restore on exit. |
| Mouse support | `tea.WithMouseCellMotion()` for scroll wheel in log/table panes. Keyboard navigation must work without mouse. |
| Minimum terminal size | If `width < 80` or `height < 20`: show `Terminal too small (minimum 80x20). Current: WxH.` centered. Do not render the full UI. No crash. |
| Quit | `q` or `ctrl+c` sends `tea.Quit`. The main.go context cancel propagates cleanly. |
| Read-only | TUI calls only GET endpoints. Never calls `/api/replay`, `/api/shutdown`, or any write endpoint. |
| Sparkline chars | Block elements: `▁▂▃▄▅▆▇█` (8 levels). Fall back to `#` if terminal does not support Unicode. |
| Time formatting | All timestamps displayed in local time using `time.Local`. UTC stored, displayed local. |

---

## Quality Gate

Run all of these before committing. All must pass.

```powershell
go build ./...
go vet ./...
$env:CGO_ENABLED='1'; go test ./... -race -count=1
```

Manual verification:
1. Run `.\bubblefish tui` with daemon running. Cycle all 7 tabs with `1`–`7`.
2. Run `.\bubblefish tui` with daemon stopped. Verify full-screen error state, retry with `r`, quit with `q`.
3. Resize terminal to 80×20 and 200×50 during TUI. Verify layout reflows, no panic.
4. Press `H` — sidebar collapses. Press again — restores.
5. Tab 2: press `/`, type text, verify filter. Press `Esc`, verify clear.
6. Tab 4: press `b` — cascade replaced by single box. Press `b` again — restores.
7. Tab 5: press `p`/`n` — verify conflict group navigation.
8. Tab 6: set timestamp, press Enter — verify API fires and results render.
9. Press `?` — verify help overlay. Press `?` or `Esc` to close.
10. Press `ctrl+r` — verify `[PAUSED]` appears in statusbar, no API calls fire. Press again to resume.

```powershell
git add -A
git commit -s -m "Phase T-1: Bubble Tea TUI — 7 tabs, live metrics, full admin API"
```

---

## Quality Checklist

Verify every item before committing.

| Area | Check | Pass Criteria |
|------|-------|---------------|
| Build | `go build ./...` | Zero warnings, zero errors |
| Lint | `go vet ./...` | Zero findings |
| Tests | `go test ./... -race -count=1` | Zero failures, zero race reports |
| Happy path | `bubblefish tui` with daemon running | All 7 tabs render. Metrics update on 5s tick. Tab switching via `1`–`7` works. |
| Error state | `bubblefish tui` with daemon stopped | Full-screen error. Retry count. `r` retries. `q` quits. |
| Resize | Resize terminal during TUI | No crash. Layout reflows. Min-size guard triggers at <80×20. |
| Sidebar collapse | Press `H` | Sidebar hides. Main pane expands. `H` again restores. |
| Filter | Tab 2: press `/` | Filter input appears. Typing filters log rows. Esc clears. |
| Time-travel | Tab 6: set timestamp, Enter | API fires. Results render. Invalid timestamp shows error inline. |
| Black box | Tab 4: press `b` | Cascade replaced with single-box summary. `b` again restores. |
| Help overlay | Press `?` | Full keybinding list overlay. `?` or `Esc` closes. |
| Pause refresh | Press `ctrl+r` | `[PAUSED]` appears in statusbar. No API calls fire. `ctrl+r` resumes. |
| No writes | All tabs | No POST/PUT/DELETE API calls from TUI. Read-only verified. |
| No new deps | `go.mod` | go.mod unchanged from pre-phase. No new modules added. |

---

*BubbleFish Technologies, Inc. · Phase T-1 TUI Prompt · v0.1.x · AGPL-3.0*
