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

package maintain_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/maintain"
)

func init() {
	if err := maintain.InitAllowedPaths(); err != nil {
		panic("InitAllowedPaths: " + err.Error())
	}
}

func tempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	return path
}

// --- ActionBackupFile ---

func TestAction_BackupFile(t *testing.T) {
	path := tempFile(t, "cfg.json", `{"key":"value"}`)
	result, err := maintain.ExecuteAction(context.Background(), maintain.ActionBackupFile, map[string]any{
		"path": path,
	})
	if err != nil {
		t.Fatalf("BackupFile: %v", err)
	}
	m := result.(map[string]any)
	backupPath := m["backup_path"].(string)
	if _, err := os.Stat(backupPath); err != nil {
		t.Errorf("backup file not created: %v", err)
	}
}

func TestAction_BackupFile_PathTraversal(t *testing.T) {
	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionBackupFile, map[string]any{
		"path": "/etc/passwd",
	})
	if err == nil {
		t.Error("expected path traversal rejection, got nil")
	}
}

// --- ActionRestoreFile ---

func TestAction_RestoreFile(t *testing.T) {
	dir := t.TempDir()
	origPath := filepath.Join(dir, "orig.json")
	backupPath := filepath.Join(dir, "orig.json.nexus-backup-999")
	if err := os.WriteFile(origPath, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupPath, []byte("backup content"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionRestoreFile, map[string]any{
		"path":        origPath,
		"backup_path": backupPath,
	})
	if err != nil {
		t.Fatalf("RestoreFile: %v", err)
	}
	data, _ := os.ReadFile(origPath)
	if string(data) != "backup content" {
		t.Errorf("expected restored content, got %q", string(data))
	}
}

// --- ActionSetConfigKey ---

func TestAction_SetConfigKey(t *testing.T) {
	path := tempFile(t, "cfg.json", `{"a":1}`)
	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionSetConfigKey, map[string]any{
		"path":  path,
		"key":   "mcpServers.nexus",
		"value": map[string]any{"command": "nexus", "args": []any{"mcp-stdio"}},
	})
	if err != nil {
		t.Fatalf("SetConfigKey: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "mcpServers") {
		t.Error("SetConfigKey: key not written to file")
	}
}

// --- ActionDeleteConfigKey ---

func TestAction_DeleteConfigKey(t *testing.T) {
	path := tempFile(t, "cfg.json", `{"keep":1,"remove":2}`)
	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionDeleteConfigKey, map[string]any{
		"path": path,
		"key":  "remove",
	})
	if err != nil {
		t.Fatalf("DeleteConfigKey: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "remove") {
		t.Error("key should have been deleted")
	}
	if !strings.Contains(string(data), "keep") {
		t.Error("keep key should still be present")
	}
}

// --- ActionVerifyConfig ---

func TestAction_VerifyConfig_Valid(t *testing.T) {
	path := tempFile(t, "cfg.json", `{"valid":true}`)
	result, err := maintain.ExecuteAction(context.Background(), maintain.ActionVerifyConfig, map[string]any{
		"path": path,
	})
	if err != nil {
		t.Fatalf("VerifyConfig: %v", err)
	}
	m := result.(map[string]any)
	if m["valid"] != true {
		t.Errorf("expected valid=true, got %v", m["valid"])
	}
}

func TestAction_VerifyConfig_Invalid(t *testing.T) {
	path := tempFile(t, "cfg.json", `{invalid json`)
	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionVerifyConfig, map[string]any{
		"path": path,
	})
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// --- ActionCreateFile ---

func TestAction_CreateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.json")
	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionCreateFile, map[string]any{
		"path":    path,
		"content": `{"created":true}`,
	})
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "created") {
		t.Error("CreateFile: content not written")
	}
}

func TestAction_CreateFile_ExistsRejected(t *testing.T) {
	path := tempFile(t, "existing.json", `{}`)
	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionCreateFile, map[string]any{
		"path":    path,
		"content": "new content",
	})
	if err == nil {
		t.Error("expected error when file already exists, got nil")
	}
}

// --- Unknown action ---

func TestAction_UnknownAction(t *testing.T) {
	_, err := maintain.ExecuteAction(context.Background(), maintain.ActionType("exec_shell"), map[string]any{})
	if err == nil {
		t.Error("expected error for unknown action type")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// --- HTTPCall ---

func TestAction_HTTPCall(t *testing.T) {
	// Use a known-good URL. Skip if no network.
	result, err := maintain.ExecuteAction(context.Background(), maintain.ActionHTTPCall, map[string]any{
		"method":          "GET",
		"url":             "http://localhost:1", // will fail — port not open
		"expected_status": 200,
	})
	_ = result
	// Just verifying it returns an error, not panics
	if err == nil {
		t.Skip("unexpected success connecting to port 1")
	}
}

// --- WaitForPort timeout ---

func TestAction_WaitForPort_Timeout(t *testing.T) {
	// Port 19999 should not be listening. Use short deadline via cancelled ctx.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := maintain.ExecuteAction(ctx, maintain.ActionWaitForPort, map[string]any{
		"port": 19999,
	})
	if err == nil {
		t.Skip("port 19999 unexpectedly open")
	}
}
