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
	"os"
	"path/filepath"
	"testing"
)

func writeTempFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownDiary_DatedFilesWithHeadings(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "2026-04-20.md", "# Meeting Notes\n\nDiscussed Q2 roadmap with team.\n\n# Action Items\n\nFollow up on competitor analysis.\n")
	writeTempFile(t, dir, "2026-04-21.md", "# Morning Standup\n\nReviewed sprint progress.\n\n# Afternoon Review\n\nClosed 3 tickets.\n")
	writeTempFile(t, dir, "2026-04-22.md", "# Design Review\n\nNew dashboard mockups reviewed.\n\n# Deploy Notes\n\nDeployed v2.1 to staging.\n")

	memories, stats, err := parseMarkdownDiaryDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 6 {
		t.Errorf("expected 6 memories, got %d", len(memories))
	}
	if stats.FilesScanned != 3 {
		t.Errorf("expected 3 files scanned, got %d", stats.FilesScanned)
	}
	for _, m := range memories {
		if m.Meta["memory_type"] != "diary" {
			t.Errorf("expected type diary, got %q", m.Meta["memory_type"])
		}
		if m.Meta["original_date"] == "" {
			t.Error("expected original_date to be set for diary entries")
		}
	}
}

func TestMarkdownDiary_SpecialFiles(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "MEMORY.md", "# Long-term Goals\n\nShip v2 by end of year.\n")
	writeTempFile(t, dir, "SOUL.md", "# Personality\n\nProfessional but friendly tone.\n")
	writeTempFile(t, dir, "USER.md", "# Preferences\n\nBoss likes concise writing.\n")

	memories, _, err := parseMarkdownDiaryDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) < 3 {
		t.Errorf("expected at least 3 memories, got %d", len(memories))
	}

	types := make(map[string]bool)
	for _, m := range memories {
		types[m.Meta["memory_type"]] = true
	}
	for _, want := range []string{"long-term", "personality", "preferences"} {
		if !types[want] {
			t.Errorf("expected memory type %q, not found", want)
		}
	}
}

func TestMarkdownDiary_NoHeadingsSplitOnParagraph(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "2026-04-20.md", "First paragraph with enough length.\n\nSecond paragraph with enough length.\n\nThird paragraph with enough length.\n")

	memories, _, err := parseMarkdownDiaryDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 3 {
		t.Errorf("expected 3 memories, got %d", len(memories))
	}
}

func TestMarkdownDiary_ShortFragmentsSkipped(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "2026-04-20.md", "Short.\n\nThis paragraph is long enough to be kept.\n\nTiny.\n")

	memories, stats, err := parseMarkdownDiaryDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Errorf("expected 1 memory (2 too short), got %d", len(memories))
	}
	if stats.FragmentsSkipped != 2 {
		t.Errorf("expected 2 fragments skipped, got %d", stats.FragmentsSkipped)
	}
}

func TestMarkdownDiary_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	memories, stats, err := parseMarkdownDiaryDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 0 {
		t.Errorf("expected 0 memories, got %d", len(memories))
	}
	if stats.FilesScanned != 0 {
		t.Errorf("expected 0 files scanned, got %d", stats.FilesScanned)
	}
}

func TestMarkdownDiary_NonExistentPath(t *testing.T) {
	_, _, err := parseMarkdownDiaryDir(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Error("expected error for non-existent path")
	}
}

func TestMarkdownDiary_MixedFileTypes(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "2026-04-20.md", "# Diary Entry\n\nToday was productive and full of progress.\n")
	writeTempFile(t, dir, "MEMORY.md", "# Core Memory\n\nAlways remember the mission statement.\n")
	writeTempFile(t, dir, "SOUL.md", "# Soul Values\n\nKindness above all other values in life.\n")
	writeTempFile(t, dir, "USER.md", "# User Prefs\n\nPrefer dark mode and monospace fonts.\n")
	writeTempFile(t, dir, "random-notes.md", "# Random Notes\n\nSome interesting observations about the world.\n")

	memories, _, err := parseMarkdownDiaryDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	typeCount := make(map[string]int)
	for _, m := range memories {
		typeCount[m.Meta["memory_type"]]++
	}

	if typeCount["diary"] < 1 {
		t.Error("expected at least 1 diary memory")
	}
	if typeCount["long-term"] < 1 {
		t.Error("expected at least 1 long-term memory")
	}
	if typeCount["personality"] < 1 {
		t.Error("expected at least 1 personality memory")
	}
	if typeCount["preferences"] < 1 {
		t.Error("expected at least 1 preferences memory")
	}
	if typeCount["document"] < 1 {
		t.Error("expected at least 1 document memory")
	}
}

func TestMarkdownDiary_ContentHashDedup(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "2026-04-20.md", "# Duplicate Entry\n\nThis content appears in the import twice.\n")

	memories1, _, err := parseMarkdownDiaryDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	memories2, _, err := parseMarkdownDiaryDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(memories1) != len(memories2) {
		t.Errorf("second parse returned different count: %d vs %d", len(memories1), len(memories2))
	}
	if memories1[0].Content != memories2[0].Content {
		t.Error("content mismatch between runs — dedup depends on identical content")
	}
}

func TestDirContainsDatedMarkdown(t *testing.T) {
	dir := t.TempDir()
	if dirContainsDatedMarkdown(dir) {
		t.Error("empty dir should not match")
	}
	writeTempFile(t, dir, "notes.md", "not dated")
	if dirContainsDatedMarkdown(dir) {
		t.Error("non-dated .md should not match")
	}
	writeTempFile(t, dir, "2026-04-20.md", "dated")
	if !dirContainsDatedMarkdown(dir) {
		t.Error("dated .md should match")
	}
}

func TestClassifyMarkdownFile(t *testing.T) {
	tests := []struct {
		name     string
		wantType string
	}{
		{"2026-04-20.md", "diary"},
		{"2026-12-31.md", "diary"},
		{"MEMORY.md", "long-term"},
		{"memory.md", "long-term"},
		{"SOUL.md", "personality"},
		{"soul.md", "personality"},
		{"USER.md", "preferences"},
		{"user.md", "preferences"},
		{"random.md", "document"},
		{"notes-2026.md", "document"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := classifyMarkdownFile(tt.name)
			if got != tt.wantType {
				t.Errorf("classifyMarkdownFile(%q) = %q, want %q", tt.name, got, tt.wantType)
			}
		})
	}
}
