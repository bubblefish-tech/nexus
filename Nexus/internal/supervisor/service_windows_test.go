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
	"strings"
	"testing"
)

func TestWindowsInstallScript_Default(t *testing.T) {
	cfg := DefaultWindowsServiceConfig()
	out, err := GenerateWindowsInstallScript(cfg)
	if err != nil {
		t.Fatalf("GenerateWindowsInstallScript: %v", err)
	}

	checks := []string{
		"sc.exe create",
		cfg.ServiceName,
		cfg.DisplayName,
		cfg.BinaryPath,
		"start --foreground",
		"start= auto",
		"restart/5000",
		"delayed-auto",
	}
	for _, s := range checks {
		if !strings.Contains(out, s) {
			t.Errorf("install script missing %q", s)
		}
	}
}

func TestWindowsInstallScript_CustomConfig(t *testing.T) {
	cfg := WindowsServiceConfig{
		ServiceName: "MyNexus",
		DisplayName: "My Custom Nexus",
		BinaryPath:  `D:\custom\nexus.exe`,
		Description: "custom desc",
	}
	out, err := GenerateWindowsInstallScript(cfg)
	if err != nil {
		t.Fatalf("GenerateWindowsInstallScript: %v", err)
	}

	if !strings.Contains(out, "MyNexus") {
		t.Error("missing custom service name")
	}
	if !strings.Contains(out, `D:\custom\nexus.exe`) {
		t.Error("missing custom binary path")
	}
}

func TestWindowsUninstallScript_Default(t *testing.T) {
	cfg := DefaultWindowsServiceConfig()
	out, err := GenerateWindowsUninstallScript(cfg)
	if err != nil {
		t.Fatalf("GenerateWindowsUninstallScript: %v", err)
	}

	checks := []string{
		"sc.exe stop",
		"sc.exe delete",
		cfg.ServiceName,
	}
	for _, s := range checks {
		if !strings.Contains(out, s) {
			t.Errorf("uninstall script missing %q", s)
		}
	}
}

func TestWindowsUninstallScript_CustomConfig(t *testing.T) {
	cfg := WindowsServiceConfig{
		ServiceName: "CustomSvc",
		DisplayName: "Custom",
		BinaryPath:  `C:\foo\nexus.exe`,
		Description: "test",
	}
	out, err := GenerateWindowsUninstallScript(cfg)
	if err != nil {
		t.Fatalf("GenerateWindowsUninstallScript: %v", err)
	}

	if !strings.Contains(out, "CustomSvc") {
		t.Error("missing custom service name in uninstall")
	}
}

func TestWindowsDefaultConfig_Fields(t *testing.T) {
	cfg := DefaultWindowsServiceConfig()
	if cfg.ServiceName == "" {
		t.Error("ServiceName should not be empty")
	}
	if cfg.DisplayName == "" {
		t.Error("DisplayName should not be empty")
	}
	if cfg.BinaryPath == "" {
		t.Error("BinaryPath should not be empty")
	}
	if cfg.Description == "" {
		t.Error("Description should not be empty")
	}
}

func TestWindowsInstallScript_ContainsRecovery(t *testing.T) {
	cfg := DefaultWindowsServiceConfig()
	out, err := GenerateWindowsInstallScript(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "sc.exe failure") {
		t.Error("missing failure recovery configuration")
	}
	if !strings.Contains(out, "restart/5000/restart/30000/restart/60000") {
		t.Error("missing graduated restart intervals")
	}
}

func TestWindowsInstallScript_ContainsAutoStart(t *testing.T) {
	cfg := DefaultWindowsServiceConfig()
	out, err := GenerateWindowsInstallScript(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "delayed-auto") {
		t.Error("missing delayed-auto start configuration")
	}
}

func TestWindowsInstallScript_ContainsForegroundFlag(t *testing.T) {
	cfg := DefaultWindowsServiceConfig()
	out, err := GenerateWindowsInstallScript(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "--foreground") {
		t.Error("missing --foreground flag in service binary path")
	}
}

func TestWindowsUninstallScript_StopsBeforeDelete(t *testing.T) {
	cfg := DefaultWindowsServiceConfig()
	out, err := GenerateWindowsUninstallScript(cfg)
	if err != nil {
		t.Fatal(err)
	}

	stopIdx := strings.Index(out, "sc.exe stop")
	deleteIdx := strings.Index(out, "sc.exe delete")
	if stopIdx < 0 || deleteIdx < 0 {
		t.Fatal("missing stop or delete commands")
	}
	if stopIdx >= deleteIdx {
		t.Error("stop should come before delete")
	}
}

func TestWindowsInstallScript_ContainsDescription(t *testing.T) {
	cfg := DefaultWindowsServiceConfig()
	out, err := GenerateWindowsInstallScript(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "sc.exe description") {
		t.Error("missing sc.exe description command")
	}
}

func TestWindowsPipePath(t *testing.T) {
	path := PlatformPipePath("")
	if path != `\\.\pipe\nexus-supervisor` {
		t.Errorf("want named pipe path, got %q", path)
	}
}
