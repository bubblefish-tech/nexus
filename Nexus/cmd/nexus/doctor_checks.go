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

package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/disk"
)

// CheckResult describes the outcome of a single doctor check.
type CheckResult struct {
	Name    string
	Status  string // OK, WARN, CRITICAL
	Message string
}

// CheckResults is a collection of check outcomes.
type CheckResults []CheckResult

// Criticals returns checks with CRITICAL status.
func (r CheckResults) Criticals() CheckResults {
	var out CheckResults
	for _, c := range r {
		if c.Status == "CRITICAL" {
			out = append(out, c)
		}
	}
	return out
}

// Warnings returns checks with WARN status.
func (r CheckResults) Warnings() CheckResults {
	var out CheckResults
	for _, c := range r {
		if c.Status == "WARN" {
			out = append(out, c)
		}
	}
	return out
}

// RunAllChecks runs all expanded doctor checks and returns results.
func RunAllChecks(configDir string) CheckResults {
	var results CheckResults
	results = append(results, checkCloudSync(configDir))
	results = append(results, checkDiskSpace(configDir))
	results = append(results, checkPorts(8080, 7474, 8081)...)
	results = append(results, checkPermissions(configDir))
	if runtime.GOOS != "windows" {
		results = append(results, checkFDLimit())
	}
	return results
}

func checkCloudSync(configDir string) CheckResult {
	markers := []string{".dropbox", ".dropbox.attr", "OneDrive", "Google Drive", ".icloud"}
	dir := configDir
	for {
		for _, m := range markers {
			checkPath := filepath.Join(dir, m)
			if _, err := os.Stat(checkPath); err == nil {
				return CheckResult{
					Name:    "cloud_sync",
					Status:  "WARN",
					Message: fmt.Sprintf("Data directory is inside a cloud-synced folder (%s). This causes silent WAL corruption. Run `nexus doctor --repair` to relocate.", checkPath),
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return CheckResult{Name: "cloud_sync", Status: "OK", Message: "not inside a cloud-synced folder"}
}

func checkDiskSpace(configDir string) CheckResult {
	usage, err := disk.Usage(configDir)
	if err != nil {
		return CheckResult{Name: "disk_space", Status: "OK", Message: "could not read disk usage (skipped)"}
	}
	freeMiB := usage.Free / (1 << 20)
	if freeMiB < 512 {
		return CheckResult{
			Name:    "disk_space",
			Status:  "CRITICAL",
			Message: fmt.Sprintf("Insufficient disk space (%d MiB free). Nexus requires at least 512 MiB.", freeMiB),
		}
	}
	if freeMiB < 1024 {
		return CheckResult{
			Name:    "disk_space",
			Status:  "WARN",
			Message: fmt.Sprintf("Low disk space (%d MiB free). Consider freeing space.", freeMiB),
		}
	}
	return CheckResult{Name: "disk_space", Status: "OK", Message: fmt.Sprintf("%d MiB free", freeMiB)}
}

func checkPorts(ports ...int) CheckResults {
	var results CheckResults
	for _, port := range ports {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", port), 500*time.Millisecond)
		if err != nil {
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("port_%d", port),
				Status:  "OK",
				Message: fmt.Sprintf("port %d is available", port),
			})
			continue
		}
		conn.Close()

		client := &http.Client{Timeout: 1 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/health", port))
		if err == nil {
			resp.Body.Close()
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("port_%d", port),
				Status:  "OK",
				Message: fmt.Sprintf("port %d in use by Nexus", port),
			})
		} else {
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("port_%d", port),
				Status:  "WARN",
				Message: fmt.Sprintf("Port %d in use by another application.", port),
			})
		}
	}
	return results
}

func checkPermissions(configDir string) CheckResult {
	keysDir := filepath.Join(configDir, "keys")
	info, err := os.Stat(keysDir)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{Name: "permissions", Status: "WARN", Message: "keys directory does not exist"}
		}
		return CheckResult{Name: "permissions", Status: "OK", Message: "could not stat keys dir (skipped)"}
	}
	if runtime.GOOS != "windows" {
		mode := info.Mode().Perm()
		if mode&0o077 != 0 {
			return CheckResult{
				Name:    "permissions",
				Status:  "CRITICAL",
				Message: fmt.Sprintf("keys directory %s is world-readable (mode %o). Run: chmod 700 %s", keysDir, mode, keysDir),
			}
		}
	}
	return CheckResult{Name: "permissions", Status: "OK", Message: "keys directory permissions correct"}
}

func checkFDLimit() CheckResult {
	return CheckResult{Name: "fd_limit", Status: "OK", Message: "file descriptor limit check (platform-specific, skipped)"}
}

func checkFilesystem(configDir string) CheckResult {
	partitions, err := disk.Partitions(true)
	if err != nil {
		return CheckResult{Name: "filesystem", Status: "OK", Message: "could not read partitions (skipped)"}
	}

	var bestMatch string
	var bestFS string
	for _, p := range partitions {
		if strings.HasPrefix(configDir, p.Mountpoint) && len(p.Mountpoint) > len(bestMatch) {
			bestMatch = p.Mountpoint
			bestFS = p.Fstype
		}
	}
	if bestFS == "" {
		return CheckResult{Name: "filesystem", Status: "OK", Message: "filesystem type unknown (skipped)"}
	}

	lower := strings.ToLower(bestFS)
	if lower == "vfat" || lower == "exfat" {
		return CheckResult{
			Name:    "filesystem",
			Status:  "CRITICAL",
			Message: fmt.Sprintf("Data directory on %s filesystem (%s). This does not support file locking or fsync. Use ext4, NTFS, or APFS.", bestFS, bestMatch),
		}
	}
	unsafeFS := []string{"nfs", "cifs", "smb", "fuse", "overlayfs"}
	for _, fs := range unsafeFS {
		if strings.Contains(lower, fs) {
			return CheckResult{
				Name:    "filesystem",
				Status:  "WARN",
				Message: fmt.Sprintf("Data directory on %s filesystem (%s). Network/overlay filesystems may not honor fsync.", bestFS, bestMatch),
			}
		}
	}
	return CheckResult{Name: "filesystem", Status: "OK", Message: fmt.Sprintf("filesystem %s on %s", bestFS, bestMatch)}
}
