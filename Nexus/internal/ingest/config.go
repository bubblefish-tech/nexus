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

package ingest

import (
	"fmt"
	"time"
)

// Config holds the [ingest] TOML section. Controls proactive filesystem-based
// ingestion of AI client conversations.
type Config struct {
	Enabled          bool          `toml:"enabled"`
	KillSwitch       bool          `toml:"kill_switch"`
	DebounceDuration time.Duration `toml:"debounce_duration"`
	ParseConcurrency int           `toml:"parse_concurrency"`
	MaxFileSize      int64         `toml:"max_file_size"`
	MaxLineLength    int           `toml:"max_line_length"`
	AllowlistPaths   []string      `toml:"allowlist_paths"`

	// Per-watcher toggles.
	ClaudeCodeEnabled   bool     `toml:"claude_code_enabled"`
	CursorEnabled       bool     `toml:"cursor_enabled"`
	GenericJSONLEnabled bool     `toml:"generic_jsonl_enabled"`
	GenericJSONLPaths   []string `toml:"generic_jsonl_paths"`

	// Scaffolded (experimental), default false.
	ChatGPTDesktopEnabled  bool `toml:"chatgpt_desktop_enabled"`
	ClaudeDesktopEnabled   bool `toml:"claude_desktop_enabled"`
	LMStudioEnabled        bool `toml:"lm_studio_enabled"`
	OpenWebUIEnabled       bool `toml:"open_webui_enabled"`
	PerplexityCometEnabled bool `toml:"perplexity_comet_enabled"`
}

// DefaultConfig returns a Config with sensible defaults. Ingest is enabled
// by default with Claude Code, Cursor, and Generic JSONL watchers active.
func DefaultConfig() Config {
	return Config{
		Enabled:             true,
		KillSwitch:          false,
		DebounceDuration:    500 * time.Millisecond,
		ParseConcurrency:    4,
		MaxFileSize:         100 * 1024 * 1024, // 100 MB
		MaxLineLength:       4 * 1024 * 1024,   // 4 MB
		ClaudeCodeEnabled:   true,
		CursorEnabled:       true,
		GenericJSONLEnabled: true,
	}
}

// Validate checks Config for internal consistency.
func (c Config) Validate() error {
	if c.DebounceDuration < 0 {
		return fmt.Errorf("ingest: debounce_duration must be non-negative")
	}
	if c.ParseConcurrency < 1 {
		return fmt.Errorf("ingest: parse_concurrency must be >= 1")
	}
	if c.MaxFileSize < 1 {
		return fmt.Errorf("ingest: max_file_size must be >= 1")
	}
	if c.MaxLineLength < 1 {
		return fmt.Errorf("ingest: max_line_length must be >= 1")
	}
	return nil
}

// IsDisabled returns true if Ingest should not start.
func (c Config) IsDisabled() bool {
	return c.KillSwitch || !c.Enabled
}
