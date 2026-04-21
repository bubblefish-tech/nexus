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

//go:build windows

package maintain

import (
	"fmt"
	"os/exec"
	"strconv"

	"github.com/shirou/gopsutil/v3/process"
	"golang.org/x/sys/windows/registry"
)

// terminateProcess uses taskkill on Windows.
func terminateProcess(p *process.Process) error {
	pid := p.Pid
	return exec.Command("taskkill", "/F", "/PID", strconv.Itoa(int(pid))).Run()
}

// setEnvVar writes the variable to HKCU\Environment on Windows.
// This makes it available to new processes launched after the change.
func setEnvVar(name, value string) (any, error) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return nil, fmt.Errorf("maintain: set_env_var open registry: %w", err)
	}
	defer key.Close()
	if err := key.SetStringValue(name, value); err != nil {
		return nil, fmt.Errorf("maintain: set_env_var set %s: %w", name, err)
	}
	return map[string]any{"registry_key": `HKCU\Environment`, "name": name}, nil
}
