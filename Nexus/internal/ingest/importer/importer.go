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

// Package importer implements bulk historical ingest from export files.
// It supports Claude export ZIP, ChatGPT export ZIP, Claude Code project
// directories, Cursor history directories, and generic JSONL files.
package importer

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Format identifies the type of export being imported.
type Format string

const (
	FormatAuto          Format = "auto"
	FormatClaudeZIP     Format = "claude-zip"
	FormatChatGPTZIP    Format = "chatgpt-zip"
	FormatClaudeCodeDir Format = "claude-code-dir"
	FormatCursorDir     Format = "cursor-dir"
	FormatJSONL         Format = "jsonl"
	FormatMarkdownDiary Format = "markdown-diary"
)

// Memory is a single memory extracted during import.
type Memory struct {
	Content   string
	Role      string
	Model     string
	Timestamp int64 // Unix milliseconds
	Source    string
	Meta      map[string]string
}

// Result holds import statistics.
type Result struct {
	Format         Format
	Total          int
	Written        int
	Skipped        int
	Errored        int
	Duration       time.Duration
	FilesScanned   int
	FragmentsSkipped int
	TypeBreakdown  map[string]int
}

// Writer is the interface for writing imported memories to Nexus.
type Writer interface {
	Write(source string, memory Memory) error
}

// Options configures an import run.
type Options struct {
	Path       string
	Format     Format
	SourceName string // custom source name; empty uses format default
	DryRun     bool
	Writer     Writer // nil in dry-run mode
}

// Run executes the import and returns statistics.
func Run(opts Options) (*Result, error) {
	format := opts.Format
	if format == FormatAuto || format == "" {
		detected, err := detectFormat(opts.Path)
		if err != nil {
			return nil, err
		}
		format = detected
	}

	start := time.Now()
	sourceName := opts.SourceName
	if sourceName == "" {
		sourceName = defaultSourceName(format)
	}

	var memories []Memory
	var err error
	var filesScanned, fragmentsSkipped int
	var typeBreakdown map[string]int

	switch format {
	case FormatClaudeZIP:
		memories, err = parseClaudeExportZIP(opts.Path)
	case FormatChatGPTZIP:
		memories, err = parseChatGPTExportZIP(opts.Path)
	case FormatClaudeCodeDir:
		memories, err = parseClaudeCodeDir(opts.Path)
	case FormatCursorDir:
		memories, err = parseCursorDir(opts.Path)
	case FormatJSONL:
		memories, err = parseGenericJSONL(opts.Path)
	case FormatMarkdownDiary:
		var diaryStats *MarkdownDiaryResult
		memories, diaryStats, err = parseMarkdownDiaryDir(opts.Path)
		if err == nil && diaryStats != nil {
			filesScanned = diaryStats.FilesScanned
			fragmentsSkipped = diaryStats.FragmentsSkipped
			typeBreakdown = diaryStats.TypeBreakdown
			for i := range memories {
				if memories[i].Meta == nil {
					memories[i].Meta = make(map[string]string)
				}
				memories[i].Meta["source"] = sourceName
				memories[i].Source = sourceName
			}
		}
	default:
		return nil, fmt.Errorf("import: unsupported format %q", format)
	}
	if err != nil {
		return nil, fmt.Errorf("import: parse %s: %w", format, err)
	}

	result := &Result{
		Format:           format,
		Total:            len(memories),
		FilesScanned:     filesScanned,
		FragmentsSkipped: fragmentsSkipped,
		TypeBreakdown:    typeBreakdown,
	}

	for _, mem := range memories {
		if opts.DryRun {
			result.Written++
			continue
		}
		if opts.Writer == nil {
			result.Skipped++
			continue
		}
		if err := opts.Writer.Write(sourceName, mem); err != nil {
			result.Errored++
			continue
		}
		result.Written++
	}

	result.Skipped = result.Total - result.Written - result.Errored
	result.Duration = time.Since(start)
	return result, nil
}

// detectFormat sniffs the path to determine the export format.
func detectFormat(path string) (Format, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("import: stat %s: %w", path, err)
	}

	if info.IsDir() {
		// Check for Cursor chat-history subdirectory.
		if _, err := os.Stat(filepath.Join(path, "chat-history")); err == nil {
			return FormatCursorDir, nil
		}
		// Check for dated markdown files (YYYY-MM-DD.md).
		if dirContainsDatedMarkdown(path) {
			return FormatMarkdownDiary, nil
		}
		// Check for Claude Code JSONL files.
		matches, _ := filepath.Glob(filepath.Join(path, "**", "*.jsonl"))
		if len(matches) == 0 {
			matches, _ = filepath.Glob(filepath.Join(path, "*.jsonl"))
		}
		if len(matches) > 0 {
			return FormatClaudeCodeDir, nil
		}
		return "", fmt.Errorf("import: directory %s does not match any known format (no chat-history/, dated .md, or *.jsonl found)", path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".zip":
		return detectZIPFormat(path)
	case ".jsonl":
		return FormatJSONL, nil
	}

	return "", fmt.Errorf("import: cannot detect format of %s (unknown extension %q)", path, ext)
}

// detectZIPFormat opens a ZIP and checks for signature files.
func detectZIPFormat(path string) (Format, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("import: open zip %s: %w", path, err)
	}
	defer r.Close()

	hasConversations := false
	hasUsers := false
	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if name == "conversations.json" {
			hasConversations = true
		}
		if name == "users.json" {
			hasUsers = true
		}
	}

	if hasConversations && hasUsers {
		return FormatClaudeZIP, nil
	}
	if hasConversations {
		return FormatChatGPTZIP, nil
	}
	return "", fmt.Errorf("import: zip %s does not contain conversations.json", path)
}

func defaultSourceName(format Format) string {
	switch format {
	case FormatClaudeZIP:
		return "import.claude_export"
	case FormatChatGPTZIP:
		return "import.chatgpt_export"
	case FormatClaudeCodeDir:
		return "import.claude_code"
	case FormatCursorDir:
		return "import.cursor"
	case FormatJSONL:
		return "import.generic_jsonl"
	case FormatMarkdownDiary:
		return "import.markdown_diary"
	default:
		return "import.unknown"
	}
}

// parseClaudeExportZIP reads a Claude data export ZIP (contains conversations.json + users.json).
func parseClaudeExportZIP(path string) ([]Memory, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var convData []byte
	for _, f := range r.File {
		if filepath.Base(f.Name) == "conversations.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			convData, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			break
		}
	}
	if convData == nil {
		return nil, fmt.Errorf("conversations.json not found in zip")
	}

	var convs []struct {
		UUID    string `json:"uuid"`
		Name    string `json:"name"`
		ChatMessages []struct {
			Text      string `json:"text"`
			Sender    string `json:"sender"`
			CreatedAt string `json:"created_at"`
		} `json:"chat_messages"`
	}
	if err := json.Unmarshal(convData, &convs); err != nil {
		return nil, fmt.Errorf("parse conversations.json: %w", err)
	}

	var memories []Memory
	for _, conv := range convs {
		for _, msg := range conv.ChatMessages {
			if msg.Text == "" {
				continue
			}
			role := "user"
			if msg.Sender == "assistant" {
				role = "assistant"
			}
			memories = append(memories, Memory{
				Content:   msg.Text,
				Role:      role,
				Timestamp: parseTS(msg.CreatedAt),
				Meta: map[string]string{
					"conversation_id":   conv.UUID,
					"conversation_name": conv.Name,
				},
			})
		}
	}
	return memories, nil
}

// parseChatGPTExportZIP reads a ChatGPT data export ZIP (contains conversations.json, no users.json).
func parseChatGPTExportZIP(path string) ([]Memory, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var convData []byte
	for _, f := range r.File {
		if filepath.Base(f.Name) == "conversations.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			convData, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
			break
		}
	}
	if convData == nil {
		return nil, fmt.Errorf("conversations.json not found in zip")
	}

	// ChatGPT export format: array of conversations, each with a "mapping"
	// object where values have "message" objects.
	var convs []struct {
		Title   string `json:"title"`
		ID      string `json:"id"`
		Mapping map[string]struct {
			Message *struct {
				Author struct {
					Role string `json:"role"`
				} `json:"author"`
				Content struct {
					Parts []interface{} `json:"parts"`
				} `json:"content"`
				CreateTime float64 `json:"create_time"`
			} `json:"message"`
		} `json:"mapping"`
	}
	if err := json.Unmarshal(convData, &convs); err != nil {
		return nil, fmt.Errorf("parse conversations.json: %w", err)
	}

	var memories []Memory
	for _, conv := range convs {
		for _, node := range conv.Mapping {
			if node.Message == nil {
				continue
			}
			role := node.Message.Author.Role
			if role != "user" && role != "assistant" {
				continue
			}
			var content string
			for _, part := range node.Message.Content.Parts {
				if s, ok := part.(string); ok && s != "" {
					if content != "" {
						content += "\n"
					}
					content += s
				}
			}
			if content == "" {
				continue
			}
			ts := int64(0)
			if node.Message.CreateTime > 0 {
				ts = int64(node.Message.CreateTime * 1000)
			}
			memories = append(memories, Memory{
				Content:   content,
				Role:      role,
				Timestamp: ts,
				Meta: map[string]string{
					"conversation_id":    conv.ID,
					"conversation_title": conv.Title,
				},
			})
		}
	}
	return memories, nil
}

// parseClaudeCodeDir walks a Claude Code project directory for *.jsonl files.
func parseClaudeCodeDir(path string) ([]Memory, error) {
	var memories []Memory
	err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(p, ".jsonl") {
			return nil
		}
		mems, err := parseJSONLFile(p, "claude_code")
		if err != nil {
			return nil // skip bad files
		}
		memories = append(memories, mems...)
		return nil
	})
	return memories, err
}

// parseCursorDir reads Cursor chat-history/*.json files.
func parseCursorDir(path string) ([]Memory, error) {
	chatDir := filepath.Join(path, "chat-history")
	entries, err := os.ReadDir(chatDir)
	if err != nil {
		return nil, fmt.Errorf("read cursor chat-history: %w", err)
	}

	var memories []Memory
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(chatDir, entry.Name()))
		if err != nil {
			continue
		}
		var cf struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Messages []struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				Timestamp string `json:"timestamp"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(data, &cf); err != nil {
			continue
		}
		for _, msg := range cf.Messages {
			if msg.Content == "" {
				continue
			}
			memories = append(memories, Memory{
				Content:   msg.Content,
				Role:      msg.Role,
				Timestamp: parseTS(msg.Timestamp),
				Meta: map[string]string{
					"cursor_chat_id": cf.ID,
					"cursor_title":   cf.Title,
				},
			})
		}
	}
	return memories, nil
}

// parseGenericJSONL reads a single JSONL file.
func parseGenericJSONL(path string) ([]Memory, error) {
	return parseJSONLFile(path, "generic_jsonl")
}

// parseJSONLFile parses a JSONL file where each line has role, content, timestamp.
func parseJSONLFile(path string, source string) ([]Memory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var memories []Memory
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content interface{} `json:"content"`
			Message struct {
				Role    string      `json:"role"`
				Content interface{} `json:"content"`
				Model   string      `json:"model"`
			} `json:"message"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		role := entry.Role
		content := extractContent(entry.Content)
		model := ""

		// Handle Claude Code format (nested message).
		if role == "" && entry.Message.Role != "" {
			role = entry.Message.Role
			content = extractContent(entry.Message.Content)
			model = entry.Message.Model
		}

		if entry.Type != "" && entry.Type != "user" && entry.Type != "assistant" {
			continue
		}

		if content == "" || role == "" {
			continue
		}

		memories = append(memories, Memory{
			Content:   content,
			Role:      role,
			Model:     model,
			Timestamp: parseTS(entry.Timestamp),
			Meta: map[string]string{
				"source_format": source,
			},
		})
	}
	return memories, nil
}

// extractContent handles both string and array content formats.
func extractContent(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if arr, ok := v.([]interface{}); ok {
		var parts []string
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			}
			if s, ok := item.(string); ok && s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// parseTS parses an ISO 8601 timestamp to Unix milliseconds.
func parseTS(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05Z", s)
		if err != nil {
			return 0
		}
	}
	return t.UnixMilli()
}
