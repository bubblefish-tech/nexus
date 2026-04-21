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

package configio_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/maintain/configio"
)

// write creates a temp file with the given content and returns its path.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// --- Format detection ---

func TestDetectFormat_JSON(t *testing.T) {
	t.Helper()
	path := write(t, "cfg.json", `{"key":"value"}`)
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if cf.Format != "json" {
		t.Errorf("expected json, got %s", cf.Format)
	}
}

func TestDetectFormat_JSONC(t *testing.T) {
	path := write(t, "cfg.json", `// comment
{"key":"value"}`)
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if cf.Format != "jsonc" {
		t.Errorf("expected jsonc, got %s", cf.Format)
	}
}

func TestDetectFormat_TOML(t *testing.T) {
	path := write(t, "cfg.toml", `key = "value"`)
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if cf.Format != "toml" {
		t.Errorf("expected toml, got %s", cf.Format)
	}
}

func TestDetectFormat_YAML(t *testing.T) {
	path := write(t, "cfg.yaml", "key: value\n")
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if cf.Format != "yaml" {
		t.Errorf("expected yaml, got %s", cf.Format)
	}
}

func TestDetectFormat_INI(t *testing.T) {
	path := write(t, "cfg.ini", "[section]\nkey = value\n")
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if cf.Format != "ini" {
		t.Errorf("expected ini, got %s", cf.Format)
	}
}

// --- JSON round-trip ---

func TestJSON_RoundTrip(t *testing.T) {
	path := write(t, "cfg.json", `{"a":{"b":"hello"}}`)
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := cf.Set("a.b", "world"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cf.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cf2, err := configio.Open(path)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	v, err := cf2.Get("a.b")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "world" {
		t.Errorf("expected world, got %v", v)
	}
}

// --- JSONC comment stripping ---

func TestJSONC_CommentStripping(t *testing.T) {
	src := `// top-level comment
{
  // line comment
  "key": "value", /* inline block */
  "nested": {
    "a": 1 // trailing
  }
}`
	path := write(t, "cfg.json", src)
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open JSONC: %v", err)
	}
	v, err := cf.Get("key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "value" {
		t.Errorf("expected value, got %v", v)
	}
}

// --- BOM handling ---

func TestJSON_BOMStripped(t *testing.T) {
	bom := "\xEF\xBB\xBF"
	path := write(t, "cfg.json", bom+`{"key":"bom"}`)
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open with BOM: %v", err)
	}
	v, _ := cf.Get("key")
	if v != "bom" {
		t.Errorf("BOM not stripped: got %v", v)
	}
}

// --- Keypath operations ---

func TestGet_MissingKeyReturnsNil(t *testing.T) {
	path := write(t, "cfg.json", `{"a":1}`)
	cf, _ := configio.Open(path)
	v, err := cf.Get("missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Errorf("expected nil for missing key, got %v", v)
	}
}

func TestSet_NestedKeypathCreation(t *testing.T) {
	path := write(t, "cfg.json", `{}`)
	cf, _ := configio.Open(path)
	if err := cf.Set("a.b.c", "deep"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, _ := cf.Get("a.b.c")
	if v != "deep" {
		t.Errorf("expected deep, got %v", v)
	}
}

func TestDelete_KeyRemoved(t *testing.T) {
	path := write(t, "cfg.json", `{"x":1,"y":2}`)
	cf, _ := configio.Open(path)
	if err := cf.Delete("x"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	v, _ := cf.Get("x")
	if v != nil {
		t.Errorf("expected nil after delete, got %v", v)
	}
	v2, _ := cf.Get("y")
	if v2 == nil {
		t.Error("y should still exist")
	}
}

func TestHas(t *testing.T) {
	path := write(t, "cfg.json", `{"present":true}`)
	cf, _ := configio.Open(path)
	if !cf.Has("present") {
		t.Error("Has: expected true for present key")
	}
	if cf.Has("absent") {
		t.Error("Has: expected false for absent key")
	}
}

func TestGet_ArrayIndex(t *testing.T) {
	path := write(t, "cfg.json", `{"servers":[{"url":"a"},{"url":"b"}]}`)
	cf, _ := configio.Open(path)
	v, err := cf.Get("servers[1].url")
	if err != nil {
		t.Fatalf("Get array: %v", err)
	}
	if v != "b" {
		t.Errorf("expected b, got %v", v)
	}
}

// --- TOML round-trip ---

func TestTOML_RoundTrip(t *testing.T) {
	path := write(t, "cfg.toml", "name = \"test\"\n")
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open TOML: %v", err)
	}
	if err := cf.Set("name", "updated"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cf.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cf2, _ := configio.Open(path)
	v, _ := cf2.Get("name")
	if v != "updated" {
		t.Errorf("TOML round-trip: expected updated, got %v", v)
	}
}

// --- YAML round-trip ---

func TestYAML_RoundTrip(t *testing.T) {
	path := write(t, "cfg.yaml", "key: original\n")
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open YAML: %v", err)
	}
	if err := cf.Set("key", "modified"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := cf.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cf2, _ := configio.Open(path)
	v, _ := cf2.Get("key")
	if v != "modified" {
		t.Errorf("YAML round-trip: expected modified, got %v", v)
	}
}

// --- INI round-trip ---

func TestINI_RoundTrip(t *testing.T) {
	src := "[database]\nhost = localhost\nport = 5432\n"
	path := write(t, "cfg.ini", src)
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open INI: %v", err)
	}
	v, _ := cf.Get("database.host")
	if v != "localhost" {
		t.Errorf("INI Get: expected localhost, got %v", v)
	}
	if err := cf.Set("database.host", "remotehost"); err != nil {
		t.Fatalf("Set INI: %v", err)
	}
	if err := cf.Save(); err != nil {
		t.Fatalf("Save INI: %v", err)
	}
	cf2, _ := configio.Open(path)
	v2, _ := cf2.Get("database.host")
	if v2 != "remotehost" {
		t.Errorf("INI round-trip: expected remotehost, got %v", v2)
	}
}

// --- SQLite read-only ---

func TestSQLite_ReadOnly(t *testing.T) {
	// Use an empty temp file named .sqlite so format detection picks sqlite
	path := write(t, "state.sqlite", "")
	// SQLite with an empty/non-DB file should return empty map, not error
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open sqlite: %v", err)
	}
	if err := cf.Set("key", "value"); err == nil {
		t.Error("expected error on Set for sqlite, got nil")
	}
	if err := cf.Save(); err == nil {
		t.Error("expected error on Save for sqlite, got nil")
	}
}

// --- SaveTo ---

func TestSaveTo(t *testing.T) {
	src := write(t, "src.json", `{"msg":"hello"}`)
	dst := filepath.Join(t.TempDir(), "dst.json")
	cf, _ := configio.Open(src)
	if err := cf.SaveTo(dst); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	cf2, _ := configio.Open(dst)
	v, _ := cf2.Get("msg")
	if v != "hello" {
		t.Errorf("SaveTo: expected hello, got %v", v)
	}
}

// --- Plist ---

func TestPlist_RoundTrip(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>mcpEnabled</key>
	<true/>
	<key>port</key>
	<integer>8081</integer>
</dict>
</plist>`
	path := write(t, "cfg.plist", src)
	cf, err := configio.Open(path)
	if err != nil {
		t.Fatalf("Open plist: %v", err)
	}
	v, err := cf.Get("mcpEnabled")
	if err != nil {
		t.Fatalf("Get plist key: %v", err)
	}
	if v != true {
		t.Errorf("plist: expected true, got %v", v)
	}
}
