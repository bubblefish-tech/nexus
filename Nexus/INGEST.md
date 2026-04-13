# Ingest — Proactive AI Conversation Ingestion

Ingest is a filesystem-based ingestion subsystem that watches AI client data
directories and writes new conversation content into Nexus as memories. It
runs inside the daemon and uses the same write pipeline as manual API writes
— full cryptographic provenance, idempotency, WAL durability, and policy
enforcement apply.

## Supported AI Clients

| Client | Status | Data Location |
|--------|--------|---------------|
| Claude Code | Supported | `~/.claude/projects/**/*.jsonl` |
| Cursor | Supported | `~/.cursor/chat-history/*.json` |
| Generic JSONL | Supported | User-configured paths |
| ChatGPT Desktop | Detected, v0.1.4 | `~/Library/Application Support/ChatGPT` (macOS) |
| Claude Desktop | Detected, v0.1.4 | `~/Library/Application Support/Claude` (macOS) |
| LM Studio | Detected, v0.1.4 | `~/.lmstudio/conversations/` |
| Open WebUI | Detected, v0.1.4 | `~/.open-webui/` |
| Perplexity Comet | Detected, v0.1.5 | Browser profile cache |

"Detected" means Ingest checks whether the application's data directory
exists and reports it in `bubblefish ingest status`, but does not parse
content yet.

## How It Works

1. Daemon starts, Ingest detects which AI clients are installed
2. fsnotify watches the data directories for file changes
3. Per-file debouncer coalesces rapid events (500ms default)
4. Worker pool parses new content (4 concurrent parsers default)
5. Each extracted memory writes through the standard pipeline
6. File offset is persisted in `ingest_file_state` SQLite table
7. On next change, only new bytes are parsed (no re-ingestion)

## Configuration

Add an `[ingest]` section to `daemon.toml`:

```toml
[ingest]
enabled = true                    # default: true
kill_switch = false               # emergency off switch
debounce_duration_ms = 500        # ms before parsing after last event
parse_concurrency = 4             # max concurrent parse goroutines
max_file_size = 104857600         # 100 MB per file
max_line_length = 4194304         # 4 MB per line

# Per-watcher toggles
claude_code_enabled = true
cursor_enabled = true
generic_jsonl_enabled = true
generic_jsonl_paths = [
    "/home/user/my-conversations.jsonl",
    "/home/user/logs/*.jsonl"
]

# Scaffolded (disabled by default)
chatgpt_desktop_enabled = false
claude_desktop_enabled = false
lm_studio_enabled = false
open_webui_enabled = false
perplexity_comet_enabled = false

# Enterprise: restrict which paths Ingest can read
# allowlist_paths = ["/home/user/.claude", "/home/user/.cursor"]
```

## Generic JSONL Format

The generic JSONL parser accepts any file where each line matches:

```json
{"role": "user", "content": "What is 2+2?", "timestamp": "2026-04-10T12:00:00Z"}
{"role": "assistant", "content": "4.", "timestamp": "2026-04-10T12:00:01Z"}
```

- `role` (required): `"user"`, `"assistant"`, or `"system"`
- `content` (required): the message text
- `timestamp` (optional): ISO 8601; daemon fills in current time if absent

This is the escape hatch for any AI tool not natively supported. Convert
your data to this format and point `generic_jsonl_paths` at it.

## CLI Commands

```
bubblefish ingest status            List all watchers with state and ingest counts
bubblefish ingest pause <watcher>   Pause a named watcher
bubblefish ingest resume <watcher>  Resume a paused watcher
bubblefish ingest reset <watcher>   Forget file state (re-parse from offset 0)
```

## Bulk Import

For historical data, use `bubblefish import`:

```
bubblefish import ~/Downloads/claude-export.zip
bubblefish import ~/Downloads/chatgpt-export.zip
bubblefish import ~/.claude/projects/my-project/    --format claude-code-dir
bubblefish import ~/.cursor/                         --format cursor-dir
bubblefish import ~/conversations.jsonl              --format jsonl
bubblefish import ~/data.zip --dry-run               # count without writing
```

Auto-detection works for most formats. Use `--format` to override.

## Security

- **Symlinks are never followed.** Every path is checked with `os.Lstat`.
- **File size cap.** Files larger than `max_file_size` (default 100 MB) are refused.
- **Line length cap.** Lines longer than `max_line_length` (default 4 MB) are truncated.
- **Read-only.** Ingest never writes to watched files. Ever.
- **No network access.** Ingest never phones home or fetches remote files.
- **Path allowlist.** Enterprise deployments can set `allowlist_paths` to restrict what Ingest reads.

## FAQ

**Can Ingest modify my AI client's files?**
No. All file handles are read-only. Ingest only reads; it never writes back.

**Does it phone home?**
No. Ingest has no network access. All data stays on your machine.

**What if I don't want it watching something?**
Set `kill_switch = true` to disable everything, or toggle individual watchers
with `claude_code_enabled = false`, etc. You can also use `allowlist_paths`
to restrict which directories Ingest can access.

**What happens if the daemon crashes mid-parse?**
The file offset is persisted after each successful parse. On restart, Ingest
resumes from the last saved offset. The content hash idempotency layer
deduplicates any overlap.

**What if two watchers ingest the same content?**
The content hash idempotency layer deduplicates automatically. No special
handling needed.
