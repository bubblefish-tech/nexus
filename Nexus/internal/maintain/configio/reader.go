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

// Package configio provides a unified interface for reading and writing
// AI tool configuration files in JSON, JSONC, TOML, YAML, plist, INI,
// and SQLite formats.
package configio

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ConfigFile is a parsed configuration file with format-aware read/write ops.
type ConfigFile struct {
	Path     string
	Format   string // "json", "jsonc", "toml", "yaml", "xml_plist", "ini", "sqlite"
	raw      []byte
	data     any
	modified bool
}

// Open reads, detects the format, and parses the config file at path.
func Open(path string) (*ConfigFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("configio: read %s: %w", path, err)
	}
	format := detectFormat(raw, path)
	data, err := parseForFormat(format, raw, path)
	if err != nil {
		return nil, fmt.Errorf("configio: parse %s (format=%s): %w", path, format, err)
	}
	return &ConfigFile{Path: path, Format: format, raw: raw, data: data}, nil
}

// Get retrieves a value by dot-notation keypath (e.g. "mcpServers.nexus.command").
// Array elements use [N] syntax: "servers[0].url". Returns nil, nil if not found.
func (c *ConfigFile) Get(keypath string) (any, error) {
	segs, err := parseKeypath(keypath)
	if err != nil {
		return nil, err
	}
	v, err := navigate(c.data, segs)
	if err != nil {
		return nil, nil // missing key
	}
	return v, nil
}

// Set sets a value at keypath. Intermediate maps are created as needed.
func (c *ConfigFile) Set(keypath string, value any) error {
	if c.Format == "sqlite" {
		return fmt.Errorf("configio: sqlite configs are read-only")
	}
	segs, err := parseKeypath(keypath)
	if err != nil {
		return err
	}
	newRoot, err := navigateSet(c.data, segs, value)
	if err != nil {
		return err
	}
	c.data = newRoot
	c.modified = true
	return nil
}

// Delete removes the key at keypath. No-op if not found.
func (c *ConfigFile) Delete(keypath string) error {
	if c.Format == "sqlite" {
		return fmt.Errorf("configio: sqlite configs are read-only")
	}
	segs, err := parseKeypath(keypath)
	if err != nil {
		return err
	}
	navigateDelete(c.data, segs)
	c.modified = true
	return nil
}

// Has returns true if keypath exists and is non-nil.
func (c *ConfigFile) Has(keypath string) bool {
	v, _ := c.Get(keypath)
	return v != nil
}

// Save writes the file back to its original path in its original format.
func (c *ConfigFile) Save() error {
	return c.SaveTo(c.Path)
}

// SaveTo writes the parsed data to path in the file's original format.
func (c *ConfigFile) SaveTo(path string) error {
	if c.Format == "sqlite" {
		return fmt.Errorf("configio: sqlite configs are read-only")
	}
	out, err := serializeForFormat(c.Format, c.data)
	if err != nil {
		return fmt.Errorf("configio: serialize %s (format=%s): %w", path, c.Format, err)
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return fmt.Errorf("configio: write %s: %w", path, err)
	}
	c.modified = false
	return nil
}

// detectFormat determines the config format from file extension and content.
func detectFormat(raw []byte, path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
	case ".plist":
		return "xml_plist"
	case ".ini", ".cfg":
		return "ini"
	case ".db", ".sqlite", ".sqlite3":
		return "sqlite"
	}
	// Strip BOM before content inspection
	content := stripBOM(raw)
	if ext == ".json" {
		if hasJSONCComments(content) {
			return "jsonc"
		}
		return "json"
	}
	// Ambiguous or no extension: trial detection by content
	if looksLikeJSON(content) {
		if hasJSONCComments(content) {
			return "jsonc"
		}
		return "json"
	}
	if looksLikeTOML(content) {
		return "toml"
	}
	return "yaml"
}

func hasJSONCComments(b []byte) bool {
	s := string(b)
	return strings.Contains(s, "//") || strings.Contains(s, "/*")
}

func looksLikeJSON(b []byte) bool {
	s := strings.TrimSpace(string(b))
	return len(s) > 0 && (s[0] == '{' || s[0] == '[')
}

func looksLikeTOML(b []byte) bool {
	s := string(b)
	return strings.Contains(s, " = ") || strings.Contains(s, "[[")
}

func parseForFormat(format string, raw []byte, path string) (any, error) {
	switch format {
	case "json":
		return parseJSON(raw)
	case "jsonc":
		return parseJSONC(raw)
	case "toml":
		return parseTOML(raw)
	case "yaml":
		return parseYAML(raw)
	case "xml_plist":
		return parsePlist(raw)
	case "ini":
		return parseINI(raw)
	case "sqlite":
		return openSQLite(path)
	default:
		return parseJSON(raw)
	}
}

func serializeForFormat(format string, data any) ([]byte, error) {
	switch format {
	case "json", "jsonc":
		return serializeJSON(data)
	case "toml":
		return serializeTOML(data)
	case "yaml":
		return serializeYAML(data)
	case "xml_plist":
		return serializePlist(data)
	case "ini":
		return serializeINI(data)
	default:
		return serializeJSON(data)
	}
}

// --- Keypath navigation ---

type segment struct {
	key   string
	index int
	isIdx bool
}

// parseKeypath parses a dot-notation keypath with optional [N] array indices.
func parseKeypath(keypath string) ([]segment, error) {
	if keypath == "" {
		return nil, fmt.Errorf("configio: empty keypath")
	}
	var segs []segment
	parts := strings.Split(keypath, ".")
	for _, part := range parts {
		// Peel off leading [N] index suffixes
		for {
			bracket := strings.Index(part, "[")
			if bracket < 0 {
				break
			}
			if bracket > 0 {
				segs = append(segs, segment{key: part[:bracket]})
			}
			close := strings.Index(part[bracket:], "]")
			if close < 0 {
				return nil, fmt.Errorf("configio: unclosed [ in keypath %q", keypath)
			}
			idxStr := part[bracket+1 : bracket+close]
			idx, err := strconv.Atoi(idxStr)
			if err != nil {
				return nil, fmt.Errorf("configio: non-integer array index %q in keypath %q", idxStr, keypath)
			}
			segs = append(segs, segment{index: idx, isIdx: true})
			part = part[bracket+close+1:]
		}
		if part != "" {
			segs = append(segs, segment{key: part})
		}
	}
	return segs, nil
}

func navigate(data any, segs []segment) (any, error) {
	cur := data
	for _, seg := range segs {
		if cur == nil {
			return nil, fmt.Errorf("nil at segment %q", seg.key)
		}
		if seg.isIdx {
			arr, ok := cur.([]any)
			if !ok {
				return nil, fmt.Errorf("expected array at index [%d]", seg.index)
			}
			if seg.index < 0 || seg.index >= len(arr) {
				return nil, fmt.Errorf("index [%d] out of range (len=%d)", seg.index, len(arr))
			}
			cur = arr[seg.index]
		} else {
			m, ok := toStringMap(cur)
			if !ok {
				return nil, fmt.Errorf("expected object for key %q", seg.key)
			}
			v, ok := m[seg.key]
			if !ok {
				return nil, fmt.Errorf("key %q not found", seg.key)
			}
			cur = v
		}
	}
	return cur, nil
}

// navigateSet sets value at segs, returning the new root (data may be nil initially).
func navigateSet(data any, segs []segment, value any) (any, error) {
	if len(segs) == 0 {
		return value, nil
	}
	if data == nil {
		data = map[string]any{}
	}
	seg := segs[0]
	if seg.isIdx {
		arr, ok := data.([]any)
		if !ok {
			return nil, fmt.Errorf("configio: expected array for [%d]", seg.index)
		}
		if seg.index < 0 || seg.index >= len(arr) {
			return nil, fmt.Errorf("configio: index [%d] out of range", seg.index)
		}
		child, err := navigateSet(arr[seg.index], segs[1:], value)
		if err != nil {
			return nil, err
		}
		arr[seg.index] = child
		return arr, nil
	}
	m, ok := toStringMap(data)
	if !ok {
		m = map[string]any{}
	}
	child, err := navigateSet(m[seg.key], segs[1:], value)
	if err != nil {
		return nil, err
	}
	m[seg.key] = child
	return m, nil
}

// navigateDelete removes the key at segs from data.
func navigateDelete(data any, segs []segment) {
	if len(segs) == 0 || data == nil {
		return
	}
	seg := segs[0]
	if len(segs) == 1 {
		if !seg.isIdx {
			if m, ok := toStringMap(data); ok {
				delete(m, seg.key)
			}
		}
		return
	}
	if seg.isIdx {
		if arr, ok := data.([]any); ok && seg.index >= 0 && seg.index < len(arr) {
			navigateDelete(arr[seg.index], segs[1:])
		}
	} else {
		if m, ok := toStringMap(data); ok {
			navigateDelete(m[seg.key], segs[1:])
		}
	}
}

// toStringMap converts a value to map[string]any if possible.
func toStringMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case map[any]any: // yaml.v2 produces this; yaml.v3 should not, kept for safety
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprintf("%v", k)] = val
		}
		return out, true
	}
	return nil, false
}
