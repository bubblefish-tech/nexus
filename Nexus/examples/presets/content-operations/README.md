# Content Operations Preset

A multi-agent content production setup with 5 specialized agents sharing memory through Nexus.

## Agents

| Agent | Role | Nexus Permissions | Subscriptions |
|---|---|---|---|
| content-creator | Writes articles, blog posts, social content | read + write | "editorial feedback", "competitor updates" |
| competitor-monitor | Tracks competitor activity, new products | read + write | — |
| rss-aggregator | Ingests RSS feeds, summarizes news | read + write | — |
| social-media | Manages social posting schedule | read + write | "content drafts", "trending topics" |
| editorial-review | Reviews drafts, provides feedback | read only | "content drafts" |

## Quick Start

1. Start Nexus: `nexus start`
2. Import existing memory: `nexus import ./memory/ --source-name my-team`
3. Configure agents (see daemon.toml snippet below)
4. Run: each agent connects to `http://localhost:7474/mcp` with its token
