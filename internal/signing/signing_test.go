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

package signing

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

func TestSignAndVerifyFile(t *testing.T) {
	t.Helper()

	tests := []struct {
		name      string
		content   string
		key       []byte
		wantErr   bool
	}{
		{
			name:    "valid round trip",
			content: `{"version":"0.1.0","policies":[]}`,
			key:     []byte("test-signing-key-32-bytes-long!!"),
		},
		{
			name:    "empty JSON object",
			content: `{}`,
			key:     []byte("another-key"),
		},
		{
			name:    "large content",
			content: string(make([]byte, 64*1024)), // 64 KiB
			key:     []byte("large-content-key"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			jsonPath := filepath.Join(dir, "test.json")

			if err := os.WriteFile(jsonPath, []byte(tt.content), 0600); err != nil {
				t.Fatalf("write test file: %v", err)
			}

			// Sign.
			if err := SignFile(jsonPath, tt.key); err != nil {
				t.Fatalf("SignFile: %v", err)
			}

			// Sig file should exist.
			sigPath := jsonPath + ".sig"
			if _, err := os.Stat(sigPath); err != nil {
				t.Fatalf("sig file missing: %v", err)
			}

			// Verify should pass.
			if err := VerifyFile(jsonPath, tt.key, nil); err != nil {
				t.Fatalf("VerifyFile: %v", err)
			}
		})
	}
}

func TestVerifyFile_TamperedContent(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "policies.json")
	key := []byte("tamper-test-key")

	if err := os.WriteFile(jsonPath, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := SignFile(jsonPath, key); err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Tamper with the content.
	if err := os.WriteFile(jsonPath, []byte(`{"ok":false}`), 0600); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if err := VerifyFile(jsonPath, key, nil); err == nil {
		t.Fatal("expected verification to fail after tampering, but it passed")
	}
}

func TestVerifyFile_MissingSigFile(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "nosig.json")

	if err := os.WriteFile(jsonPath, []byte(`{}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := VerifyFile(jsonPath, []byte("key"), nil); err == nil {
		t.Fatal("expected error for missing .sig file, but got nil")
	}
}

func TestVerifyFile_WrongKey(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "wrongkey.json")

	if err := os.WriteFile(jsonPath, []byte(`{"data":"value"}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := SignFile(jsonPath, []byte("correct-key")); err != nil {
		t.Fatalf("sign: %v", err)
	}

	if err := VerifyFile(jsonPath, []byte("wrong-key"), nil); err == nil {
		t.Fatal("expected verification to fail with wrong key, but it passed")
	}
}

func TestVerifyFile_SecurityEventEmitted(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "event.json")

	if err := os.WriteFile(jsonPath, []byte(`{"x":1}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := SignFile(jsonPath, []byte("key-a")); err != nil {
		t.Fatalf("sign: %v", err)
	}

	// Tamper so verification fails.
	if err := os.WriteFile(jsonPath, []byte(`{"x":2}`), 0600); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	var gotEvent string
	var gotFile string
	onEvent := func(eventType string, attrs ...slog.Attr) {
		gotEvent = eventType
		for _, a := range attrs {
			if a.Key == "file" {
				gotFile = a.Value.String()
			}
		}
	}

	_ = VerifyFile(jsonPath, []byte("key-a"), onEvent)

	if gotEvent != "config_signature_invalid" {
		t.Errorf("event type = %q, want %q", gotEvent, "config_signature_invalid")
	}
	if gotFile != jsonPath {
		t.Errorf("event file = %q, want %q", gotFile, jsonPath)
	}
}

func TestSignAll_And_VerifyAll(t *testing.T) {
	dir := t.TempDir()
	key := []byte("signall-key")
	logger := testLogger(t)

	// Create multiple JSON files.
	files := []string{"sources.json", "destinations.json", "policies.json"}
	for _, name := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(`{"file":"`+name+`"}`), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// SignAll.
	if err := SignAll(dir, key, logger); err != nil {
		t.Fatalf("SignAll: %v", err)
	}

	// All sig files should exist.
	for _, name := range files {
		sigPath := filepath.Join(dir, name+".sig")
		if _, err := os.Stat(sigPath); err != nil {
			t.Errorf("sig file missing for %s: %v", name, err)
		}
	}

	// VerifyAll should pass.
	if err := VerifyAll(dir, key, nil, logger); err != nil {
		t.Fatalf("VerifyAll: %v", err)
	}
}

func TestVerifyAll_FailsOnTamperedFile(t *testing.T) {
	dir := t.TempDir()
	key := []byte("verifyall-key")
	logger := testLogger(t)

	files := []string{"a.json", "b.json"}
	for _, name := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(`{"name":"`+name+`"}`), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := SignAll(dir, key, logger); err != nil {
		t.Fatalf("SignAll: %v", err)
	}

	// Tamper with b.json.
	if err := os.WriteFile(filepath.Join(dir, "b.json"), []byte(`{"name":"tampered"}`), 0600); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if err := VerifyAll(dir, key, nil, logger); err == nil {
		t.Fatal("expected VerifyAll to fail after tampering, but it passed")
	}
}

func TestSignAll_NoJSONFiles(t *testing.T) {
	dir := t.TempDir()
	key := []byte("empty-key")
	logger := testLogger(t)

	if err := SignAll(dir, key, logger); err == nil {
		t.Fatal("expected error for empty directory, but got nil")
	}
}

func TestVerifyAll_NoJSONFiles(t *testing.T) {
	dir := t.TempDir()
	key := []byte("empty-key")
	logger := testLogger(t)

	if err := VerifyAll(dir, key, nil, logger); err == nil {
		t.Fatal("expected error for empty directory, but got nil")
	}
}

func TestSigFilePermissions(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "perms.json")
	key := []byte("perm-key")

	if err := os.WriteFile(jsonPath, []byte(`{}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := SignFile(jsonPath, key); err != nil {
		t.Fatalf("sign: %v", err)
	}

	info, err := os.Stat(jsonPath + ".sig")
	if err != nil {
		t.Fatalf("stat sig: %v", err)
	}
	// On Windows, os.Chmod does not fully support Unix-style permissions,
	// so we only check on non-Windows platforms.
	if perm := info.Mode().Perm(); perm&0077 != 0 && os.Getenv("OS") != "Windows_NT" {
		t.Errorf("sig file permissions = %o, want 0600 (no group/other access)", perm)
	}
}
