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

package topology

import (
	"net"
	"os/exec"
	"runtime"
	"strings"
)

// detectWSL2 returns WSL2 bridge adapter information on Windows.
// On non-Windows platforms it always returns Available=false.
func detectWSL2() *WSL2Topology {
	if runtime.GOOS != "windows" {
		return &WSL2Topology{Available: false}
	}

	bridgeIP := findWSLBridgeIP()
	if bridgeIP == "" {
		return &WSL2Topology{Available: false}
	}

	top := &WSL2Topology{
		Available: true,
		BridgeIP:  bridgeIP,
	}
	top.DistroNames = listWSLDistros()
	return top
}

// findWSLBridgeIP scans network interfaces for the vEthernet (WSL) adapter
// that Windows creates when WSL2 is active, returning its IPv4 address.
func findWSLBridgeIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		name := iface.Name
		if !strings.Contains(name, "WSL") && !strings.Contains(name, "vEthernet") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				continue
			}
			if ip.To4() != nil {
				return ip.String()
			}
		}
	}
	return ""
}

// listWSLDistros runs `wsl --list --quiet` and returns distro names.
// Returns nil if wsl.exe is not available or returns no output.
func listWSLDistros() []string {
	out, err := exec.Command("wsl", "--list", "--quiet").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		// wsl --list output on Windows uses UTF-16LE; strip null bytes from
		// any partial conversion, then trim whitespace.
		name := strings.TrimSpace(strings.ReplaceAll(line, "\x00", ""))
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
