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

package supervisor

import (
	"fmt"
	"text/template"
)

// windowsServiceTemplate generates a batch script that uses sc.exe to
// create a Windows service pointing to the nexus binary.
const windowsServiceTemplate = `@echo off
REM BubbleFish Nexus — Windows Service installation script
REM Run this script as Administrator.

set SERVICE_NAME={{.ServiceName}}
set DISPLAY_NAME={{.DisplayName}}
set BINARY_PATH={{.BinaryPath}}
set DESCRIPTION={{.Description}}

REM Create the service
sc.exe create %SERVICE_NAME% ^
    binPath= "%BINARY_PATH% start --foreground" ^
    DisplayName= "%DISPLAY_NAME%" ^
    start= auto ^
    obj= LocalSystem

REM Set the description
sc.exe description %SERVICE_NAME% "%DESCRIPTION%"

REM Configure failure recovery: restart after 5s, 30s, 60s
sc.exe failure %SERVICE_NAME% ^
    reset= 86400 ^
    actions= restart/5000/restart/30000/restart/60000

REM Configure delayed auto-start
sc.exe config %SERVICE_NAME% start= delayed-auto

echo Service "%DISPLAY_NAME%" created successfully.
echo Start with: sc.exe start %SERVICE_NAME%
echo Stop with:  sc.exe stop %SERVICE_NAME%
echo Remove with: sc.exe delete %SERVICE_NAME%
`

// windowsUninstallTemplate generates a batch script to remove the service.
const windowsUninstallTemplate = `@echo off
REM BubbleFish Nexus — Windows Service removal script
REM Run this script as Administrator.

set SERVICE_NAME={{.ServiceName}}

sc.exe stop %SERVICE_NAME% 2>nul
sc.exe delete %SERVICE_NAME%
echo Service "%SERVICE_NAME%" removed.
`

// WindowsServiceConfig holds values for the Windows service scripts.
type WindowsServiceConfig struct {
	ServiceName string
	DisplayName string
	BinaryPath  string
	Description string
}

// DefaultWindowsServiceConfig returns defaults for the Windows service.
func DefaultWindowsServiceConfig() WindowsServiceConfig {
	return WindowsServiceConfig{
		ServiceName: "NexusDaemon",
		DisplayName: "BubbleFish Nexus Memory Daemon",
		BinaryPath:  `C:\Program Files\BubbleFish\nexus.exe`,
		Description: "BubbleFish Nexus AI memory daemon — gateway-first memory management",
	}
}

// GenerateWindowsInstallScript renders the Windows service install batch script.
func GenerateWindowsInstallScript(cfg WindowsServiceConfig) (string, error) {
	tmpl, err := template.New("win-install").Parse(windowsServiceTemplate)
	if err != nil {
		return "", fmt.Errorf("parse windows install template: %w", err)
	}

	var buf stringBuffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("execute windows install template: %w", err)
	}
	return buf.String(), nil
}

// GenerateWindowsUninstallScript renders the Windows service removal batch script.
func GenerateWindowsUninstallScript(cfg WindowsServiceConfig) (string, error) {
	tmpl, err := template.New("win-uninstall").Parse(windowsUninstallTemplate)
	if err != nil {
		return "", fmt.Errorf("parse windows uninstall template: %w", err)
	}

	var buf stringBuffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", fmt.Errorf("execute windows uninstall template: %w", err)
	}
	return buf.String(), nil
}
