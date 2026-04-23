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

package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var dateFileRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)
var headingRE = regexp.MustCompile(`(?m)^#{1,2} `)

func classifyMarkdownFile(name string) (memoryType string, date time.Time) {
	lower := strings.ToLower(name)

	if dateFileRE.MatchString(name) {
		t, err := time.Parse("2006-01-02", strings.TrimSuffix(name, ".md"))
		if err == nil {
			return "diary", t
		}
	}

	switch lower {
	case "memory.md":
		return "long-term", time.Time{}
	case "soul.md":
		return "personality", time.Time{}
	case "user.md":
		return "preferences", time.Time{}
	}

	return "document", time.Time{}
}

func splitMarkdownMemories(content string) []string {
	locs := headingRE.FindAllStringIndex(content, -1)

	if len(locs) > 0 {
		var sections []string
		for i, loc := range locs {
			start := loc[0]
			end := len(content)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			section := strings.TrimSpace(content[start:end])
			if len(section) >= 10 {
				sections = append(sections, section)
			}
		}
		if len(sections) > 0 {
			return sections
		}
	}

	paragraphs := strings.Split(content, "\n\n")
	var result []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if len(p) >= 10 {
			result = append(result, p)
		}
	}
	return result
}

// MarkdownDiaryResult extends Result with type breakdown information.
type MarkdownDiaryResult struct {
	FilesScanned     int
	MemoriesImported int
	DuplicatesSkipped int
	FragmentsSkipped int
	TypeBreakdown    map[string]int
}

func parseMarkdownDiaryDir(path string) ([]Memory, *MarkdownDiaryResult, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("markdown-diary: %w", err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("markdown-diary: %s is not a directory", path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, nil, fmt.Errorf("markdown-diary: read dir: %w", err)
	}

	stats := &MarkdownDiaryResult{
		TypeBreakdown: make(map[string]int),
	}

	var memories []Memory
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		stats.FilesScanned++

		filePath := filepath.Join(path, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		memType, fileDate := classifyMarkdownFile(entry.Name())

		if fileDate.IsZero() && memType == "document" {
			fi, err := entry.Info()
			if err == nil {
				fileDate = fi.ModTime()
			}
		}

		sections := splitMarkdownMemories(string(data))
		stats.FragmentsSkipped += countFragments(string(data)) - len(sections)

		for _, section := range sections {
			meta := map[string]string{
				"original_file": entry.Name(),
				"memory_type":   memType,
			}
			if !fileDate.IsZero() {
				meta["original_date"] = fileDate.Format(time.RFC3339)
			}

			var ts int64
			if !fileDate.IsZero() {
				ts = fileDate.UnixMilli()
			}

			memories = append(memories, Memory{
				Content:   section,
				Role:      "user",
				Timestamp: ts,
				Meta:      meta,
			})
			stats.TypeBreakdown[memType]++
		}
	}

	stats.MemoriesImported = len(memories)
	return memories, stats, nil
}

func countFragments(content string) int {
	locs := headingRE.FindAllStringIndex(content, -1)
	if len(locs) > 0 {
		count := 0
		for i, loc := range locs {
			start := loc[0]
			end := len(content)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			section := strings.TrimSpace(content[start:end])
			if len(section) > 0 {
				count++
			}
		}
		return count
	}
	paragraphs := strings.Split(content, "\n\n")
	count := 0
	for _, p := range paragraphs {
		if strings.TrimSpace(p) != "" {
			count++
		}
	}
	return count
}

func dirContainsDatedMarkdown(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && dateFileRE.MatchString(e.Name()) {
			return true
		}
	}
	return false
}
