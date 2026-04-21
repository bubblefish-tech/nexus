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

// Package maintain implements the Worm Detection & Maintenance Module:
// a deterministic system automation engine that discovers AI tools, maintains
// a digital twin of their configuration, and applies safe atomic fixes.
package maintain

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/process"

	"github.com/bubblefish-tech/nexus/internal/maintain/configio"
)

// ActionType is one of the 12 safe atomic operations. This set is closed at
// compile time. Adding an action expands the attack surface of every Nexus
// install — do not add without explicit instruction.
type ActionType string

const (
	ActionBackupFile      ActionType = "backup_file"
	ActionRestoreFile     ActionType = "restore_file"
	ActionReadConfig      ActionType = "read_config"
	ActionWriteConfig     ActionType = "write_config"
	ActionSetConfigKey    ActionType = "set_config_key"
	ActionDeleteConfigKey ActionType = "delete_config_key"
	ActionSetEnvVar       ActionType = "set_env_var"
	ActionRestartProcess  ActionType = "restart_process"
	ActionWaitForPort     ActionType = "wait_for_port"
	ActionVerifyConfig    ActionType = "verify_config"
	ActionHTTPCall        ActionType = "http_call"
	ActionCreateFile      ActionType = "create_file"
)

// allowedPathPrefixes is the path allowlist. Populated by InitAllowedPaths at startup.
// Every file operation validates against this list before proceeding.
var allowedPathPrefixes []string

// InitAllowedPaths populates the path allowlist from the current environment.
// Must be called once at daemon startup.
func InitAllowedPaths() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("maintain: cannot determine home dir: %w", err)
	}
	prefixes := []string{home}
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			prefixes = append(prefixes, appdata)
		}
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			prefixes = append(prefixes, local)
		}
	}
	allowedPathPrefixes = prefixes
	return nil
}

// validatePath resolves symlinks and checks the path is within the allowlist.
// Called before every file operation — never bypassed.
func validatePath(path string) error {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("maintain: cannot resolve path %s: %w", path, err)
	}
	// Resolve symlinks if target exists; if not, check the cleaned abs path
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("maintain: cannot eval symlinks for %s: %w", path, err)
	}
	if err == nil {
		abs = resolved
	}
	for _, prefix := range allowedPathPrefixes {
		if strings.HasPrefix(abs, prefix) {
			return nil
		}
	}
	// Also allow nexus temp transaction files
	if strings.Contains(abs, "nexus-") && strings.Contains(filepath.Dir(abs), "tmp") {
		return nil
	}
	return fmt.Errorf("maintain: path %s is outside allowed directories", abs)
}

// ExecuteAction dispatches an action by type. It is the only entry point into
// the action set — all callers go through here.
func ExecuteAction(ctx context.Context, action ActionType, params map[string]any) (any, error) {
	switch action {
	case ActionBackupFile:
		return actionBackupFile(params)
	case ActionRestoreFile:
		return actionRestoreFile(params)
	case ActionReadConfig:
		return actionReadConfig(params)
	case ActionWriteConfig:
		return actionWriteConfig(params)
	case ActionSetConfigKey:
		return actionSetConfigKey(params)
	case ActionDeleteConfigKey:
		return actionDeleteConfigKey(params)
	case ActionSetEnvVar:
		return actionSetEnvVar(params)
	case ActionRestartProcess:
		return actionRestartProcess(ctx, params)
	case ActionWaitForPort:
		return actionWaitForPort(ctx, params)
	case ActionVerifyConfig:
		return actionVerifyConfig(params)
	case ActionHTTPCall:
		return actionHTTPCall(ctx, params)
	case ActionCreateFile:
		return actionCreateFile(params)
	default:
		return nil, fmt.Errorf("maintain: unknown action type %q", action)
	}
}

// --- Action implementations ---

func actionBackupFile(params map[string]any) (any, error) {
	path, err := stringParam(params, "path")
	if err != nil {
		return nil, err
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	ts := time.Now().Unix()
	backupPath := fmt.Sprintf("%s.nexus-backup-%d", path, ts)
	if err := validatePath(backupPath); err != nil {
		return nil, err
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("maintain: backup read %s: %w", path, err)
	}
	if err := os.WriteFile(backupPath, src, 0600); err != nil {
		return nil, fmt.Errorf("maintain: backup write %s: %w", backupPath, err)
	}
	return map[string]any{"backup_path": backupPath}, nil
}

func actionRestoreFile(params map[string]any) (any, error) {
	backupPath, err := stringParam(params, "backup_path")
	if err != nil {
		return nil, err
	}
	origPath, err := stringParam(params, "path")
	if err != nil {
		return nil, err
	}
	if err := validatePath(backupPath); err != nil {
		return nil, err
	}
	if err := validatePath(origPath); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return nil, fmt.Errorf("maintain: restore read %s: %w", backupPath, err)
	}
	if err := os.WriteFile(origPath, data, 0600); err != nil {
		return nil, fmt.Errorf("maintain: restore write %s: %w", origPath, err)
	}
	return map[string]any{"restored": origPath}, nil
}

func actionReadConfig(params map[string]any) (any, error) {
	path, err := stringParam(params, "path")
	if err != nil {
		return nil, err
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	cf, err := configio.Open(path)
	if err != nil {
		return nil, err
	}
	return cf, nil
}

func actionWriteConfig(params map[string]any) (any, error) {
	path, err := stringParam(params, "path")
	if err != nil {
		return nil, err
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	cf, err := configio.Open(path)
	if err != nil {
		return nil, err
	}
	return map[string]any{"format": cf.Format, "path": path}, nil
}

func actionSetConfigKey(params map[string]any) (any, error) {
	path, err := stringParam(params, "path")
	if err != nil {
		return nil, err
	}
	key, err := stringParam(params, "key")
	if err != nil {
		return nil, err
	}
	value, ok := params["value"]
	if !ok {
		return nil, fmt.Errorf("maintain: set_config_key requires 'value' param")
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	cf, err := configio.Open(path)
	if err != nil {
		return nil, err
	}
	if err := cf.Set(key, value); err != nil {
		return nil, err
	}
	if err := cf.Save(); err != nil {
		return nil, err
	}
	return map[string]any{"key": key, "path": path}, nil
}

func actionDeleteConfigKey(params map[string]any) (any, error) {
	path, err := stringParam(params, "path")
	if err != nil {
		return nil, err
	}
	key, err := stringParam(params, "key")
	if err != nil {
		return nil, err
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	cf, err := configio.Open(path)
	if err != nil {
		return nil, err
	}
	if err := cf.Delete(key); err != nil {
		return nil, err
	}
	return nil, cf.Save()
}

func actionSetEnvVar(params map[string]any) (any, error) {
	name, err := stringParam(params, "name")
	if err != nil {
		return nil, err
	}
	value, err := stringParam(params, "value")
	if err != nil {
		return nil, err
	}
	return setEnvVar(name, value)
}

func actionRestartProcess(ctx context.Context, params map[string]any) (any, error) {
	procName, err := stringParam(params, "process_name")
	if err != nil {
		return nil, err
	}
	execPath, _ := stringParam(params, "exec_path") // optional re-launch path

	const maxAttempts = 3
	backoffs := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoffs[attempt-1]):
			}
		}

		// Find the process
		procs, err := process.ProcessesWithContext(ctx)
		if err != nil {
			continue
		}
		var target *process.Process
		for _, p := range procs {
			name, err := p.NameWithContext(ctx)
			if err != nil {
				continue
			}
			// Case-insensitive, strip .exe suffix for Windows comparison
			cleanName := strings.TrimSuffix(strings.ToLower(name), ".exe")
			cleanTarget := strings.TrimSuffix(strings.ToLower(procName), ".exe")
			if cleanName == cleanTarget {
				target = p
				break
			}
		}

		if target != nil {
			// Terminate the process
			if err := terminateProcess(target); err != nil {
				continue
			}
			// Wait up to 5 seconds for graceful exit
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				running, _ := target.IsRunning()
				if !running {
					break
				}
				time.Sleep(200 * time.Millisecond)
			}
		}

		// Re-launch if exec path provided
		if execPath != "" {
			if err := exec.CommandContext(ctx, execPath).Start(); err != nil {
				continue
			}
			// Verify it's running after 2 seconds
			time.Sleep(2 * time.Second)
			procs2, _ := process.ProcessesWithContext(ctx)
			for _, p := range procs2 {
				name, _ := p.NameWithContext(ctx)
				cleanName := strings.TrimSuffix(strings.ToLower(name), ".exe")
				cleanTarget := strings.TrimSuffix(strings.ToLower(procName), ".exe")
				if cleanName == cleanTarget {
					pid := p.Pid
					return map[string]any{"restarted": procName, "pid": pid}, nil
				}
			}
			continue
		}
		return map[string]any{"terminated": procName}, nil
	}
	return nil, fmt.Errorf("maintain: failed to restart %s after %d attempts", procName, maxAttempts)
}

func actionWaitForPort(ctx context.Context, params map[string]any) (any, error) {
	port, err := intParam(params, "port")
	if err != nil {
		return nil, err
	}
	timeoutSec := 30
	if v, ok := params["timeout_seconds"]; ok {
		if n, ok := toInt(v); ok && n > 0 {
			timeoutSec = n
		}
	}
	url := fmt.Sprintf("http://localhost:%d/", port)
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return map[string]any{"port": port, "ready": true}, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil, fmt.Errorf("maintain: port %d not ready after 30s", port)
}

func actionVerifyConfig(params map[string]any) (any, error) {
	path, err := stringParam(params, "path")
	if err != nil {
		return nil, err
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	_, err = configio.Open(path)
	if err != nil {
		return nil, fmt.Errorf("maintain: verify_config failed for %s: %w", path, err)
	}
	return map[string]any{"valid": true, "path": path}, nil
}

func actionHTTPCall(ctx context.Context, params map[string]any) (any, error) {
	method, err := stringParam(params, "method")
	if err != nil {
		return nil, err
	}
	url, err := stringParam(params, "url")
	if err != nil {
		return nil, err
	}
	expectedStatus, _ := intParam(params, "expected_status")
	if expectedStatus == 0 {
		expectedStatus = 200
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("maintain: http_call build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("maintain: http_call %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != expectedStatus {
		return nil, fmt.Errorf("maintain: http_call got %d, expected %d: %s",
			resp.StatusCode, expectedStatus, string(body))
	}
	return map[string]any{"status": resp.StatusCode, "body": string(body)}, nil
}

func actionCreateFile(params map[string]any) (any, error) {
	path, err := stringParam(params, "path")
	if err != nil {
		return nil, err
	}
	content, err := stringParam(params, "content")
	if err != nil {
		return nil, err
	}
	if err := validatePath(path); err != nil {
		return nil, err
	}
	// Fail if file already exists — create_file is not overwrite
	if _, err := os.Stat(path); err == nil {
		return nil, fmt.Errorf("maintain: create_file: %s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("maintain: create_file mkdir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("maintain: create_file write %s: %w", path, err)
	}
	return map[string]any{"created": path}, nil
}

// --- param helpers ---

func stringParam(params map[string]any, key string) (string, error) {
	v, ok := params[key]
	if !ok {
		return "", fmt.Errorf("maintain: missing required param %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("maintain: param %q must be a string, got %T", key, v)
	}
	return s, nil
}

func intParam(params map[string]any, key string) (int, error) {
	v, ok := params[key]
	if !ok {
		return 0, fmt.Errorf("maintain: missing required param %q", key)
	}
	if n, ok := toInt(v); ok {
		return n, nil
	}
	return 0, fmt.Errorf("maintain: param %q must be numeric, got %T", key, v)
}

// toInt converts numeric types (int, int64, float64) to int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
