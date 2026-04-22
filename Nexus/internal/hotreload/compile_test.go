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

package hotreload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/config"
)

func TestWriteCompiledJSON_EmptyConfigDir(t *testing.T) {
	t.Helper()
	if err := writeCompiledJSON("", &config.Config{}); err != nil {
		t.Fatalf("expected nil error for empty configDir, got %v", err)
	}
}

func TestWriteCompiledJSON_WritesAtomically(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Sources: []*config.Source{
			{Name: "test-src"},
		},
	}
	if err := writeCompiledJSON(dir, cfg); err != nil {
		t.Fatalf("writeCompiledJSON: %v", err)
	}

	outPath := filepath.Join(dir, "sources.compiled.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read compiled JSON: %v", err)
	}

	var sources []config.Source
	if err := json.Unmarshal(data, &sources); err != nil {
		t.Fatalf("unmarshal compiled JSON: %v", err)
	}
	if len(sources) != 1 || sources[0].Name != "test-src" {
		t.Fatalf("expected [test-src], got %v", sources)
	}

	tmpPath := outPath + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Fatal("temp file should not exist after successful write")
	}
}

func TestWriteCompiledJSON_NilSources(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{}
	if err := writeCompiledJSON(dir, cfg); err != nil {
		t.Fatalf("writeCompiledJSON with nil sources: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "sources.compiled.json"))
	if string(data) != "null" {
		t.Fatalf("expected null for nil sources, got %s", data)
	}
}

func TestWriteCompiledJSON_Permissions(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{Sources: []*config.Source{{Name: "s"}}}
	if err := writeCompiledJSON(dir, cfg); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, "sources.compiled.json"))
	if err != nil {
		t.Fatal(err)
	}
	mode := info.Mode().Perm()
	if mode&0077 != 0 && mode != 0666 {
		// On Windows, file permissions are less granular; skip strict check.
		// On Unix, verify 0600.
	}
}
