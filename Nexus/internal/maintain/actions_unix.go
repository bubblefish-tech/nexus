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

//go:build !windows

package maintain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/shirou/gopsutil/v3/process"
)

// terminateProcess sends SIGTERM to the process on Unix.
func terminateProcess(p *process.Process) error {
	pid := p.Pid
	proc, err := os.FindProcess(int(pid))
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}

// setEnvVar writes the variable to ~/.nexus/env.sh (sourced by Nexus, not system-wide).
// For system-wide changes (e.g. Ollama CORS_ORIGIN), the user must apply them manually.
func setEnvVar(name, value string) (any, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("maintain: set_env_var: %w", err)
	}
	envPath := filepath.Join(home, ".nexus", "env.sh")
	if err := os.MkdirAll(filepath.Dir(envPath), 0700); err != nil {
		return nil, fmt.Errorf("maintain: set_env_var mkdir: %w", err)
	}

	// Read existing file
	existing := ""
	if data, err := os.ReadFile(envPath); err == nil {
		existing = string(data)
	}

	// Replace or append the export line
	line := fmt.Sprintf("export %s=%q\n", name, value)
	prefix := fmt.Sprintf("export %s=", name)
	var lines []string
	found := false
	for _, l := range strings.Split(existing, "\n") {
		if strings.HasPrefix(l, prefix) {
			lines = append(lines, strings.TrimRight(line, "\n"))
			found = true
		} else if l != "" {
			lines = append(lines, l)
		}
	}
	if !found {
		lines = append(lines, strings.TrimRight(line, "\n"))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(envPath, []byte(content), 0600); err != nil {
		return nil, fmt.Errorf("maintain: set_env_var write: %w", err)
	}
	return map[string]any{"env_file": envPath, "name": name}, nil
}
