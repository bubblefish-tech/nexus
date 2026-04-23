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
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"database/sql"

	_ "modernc.org/sqlite"

	"github.com/bubblefish-tech/nexus/internal/config"
	"github.com/bubblefish-tech/nexus/internal/doctor"
	"github.com/bubblefish-tech/nexus/internal/health"
)

// runDoctor executes the `nexus doctor` command.
//
// Runs environment, config, and connectivity health checks. For each failed
// check it emits a self-heal proposal so the user knows exactly what to run.
//
// Reference: Tech Spec WIRE.5 / Section 6.4.
func runDoctor() {
	for _, arg := range os.Args[2:] {
		if arg == "--fsync-test" {
			runFsyncTest()
			return
		}
		if arg == "--memory-health" {
			runMemoryHealth()
			return
		}
		if arg == "--repair" {
			runDoctorRepair()
			return
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus doctor: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(configDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus doctor: config load failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "  SELF-HEAL: run 'nexus install' to create a default configuration")
		os.Exit(1)
	}

	hasErrors := false
	var proposals []string

	check := func(ok bool, tag, msg, heal string) {
		if ok {
			fmt.Printf("  [ok]    %s: %s\n", tag, msg)
		} else {
			fmt.Printf("  [ERROR] %s: %s\n", tag, msg)
			if heal != "" {
				proposals = append(proposals, heal)
			}
			hasErrors = true
		}
	}
	warn := func(tag, msg string) {
		fmt.Printf("  [WARN]  %s: %s\n", tag, msg)
	}

	fmt.Println("nexus doctor: checking environment...")

	// 1. Config directory exists.
	_, statErr := os.Stat(configDir)
	check(statErr == nil, "config_dir", configDir, "run 'nexus install' to create the config directory")

	// 2. daemon.toml exists.
	daemonTOML := filepath.Join(configDir, "daemon.toml")
	_, tomlErr := os.Stat(daemonTOML)
	check(tomlErr == nil, "daemon_toml", daemonTOML, "run 'nexus install' to generate daemon.toml")

	// 3. WAL directory is writable.
	walDir := filepath.Join(configDir, "wal")
	walOK := checkWritable(walDir)
	check(walOK, "wal_writable", walDir, fmt.Sprintf("run: mkdir -p %s && chmod 700 %s", walDir, walDir))

	// 4. Logs directory exists.
	logsDir := filepath.Join(configDir, "logs")
	_, logsDirErr := os.Stat(logsDir)
	if logsDirErr != nil {
		warn("logs_dir", fmt.Sprintf("%s missing — daemon will create on next start", logsDir))
	} else {
		fmt.Printf("  [ok]    logs_dir: %s\n", logsDir)
	}

	// 5. Daemon alive check (non-fatal warn if down).
	port := cfg.Daemon.Port
	if port > 0 {
		daemonAlive := checkDaemonAlive(port)
		if daemonAlive {
			fmt.Printf("  [ok]    daemon_alive: http://127.0.0.1:%d/health responded ok\n", port)
		} else {
			fmt.Printf("  [WARN]  daemon_alive: daemon not responding on port %d\n", port)
			fmt.Printf("          SELF-HEAL: run 'nexus start' to start the daemon\n")
		}
	}

	// 6. MCP enabled check.
	if cfg.Daemon.MCP.Port == 0 {
		warn("mcp", "mcp.port = 0 — MCP server disabled (set mcp.port in daemon.toml to enable)")
	} else {
		fmt.Printf("  [ok]    mcp: port %d configured\n", cfg.Daemon.MCP.Port)
	}

	// 7. OAuth checks.
	fmt.Println()
	fmt.Println("nexus doctor: checking auth configuration...")
	if cfg.Daemon.OAuth.Enabled {
		fmt.Println("  [oauth] enabled = true")

		if cfg.Daemon.OAuth.IssuerURL == "" {
			check(false, "oauth.issuer_url", "empty", "set oauth.issuer_url in daemon.toml")
		} else {
			fmt.Printf("  [ok]    oauth.issuer_url = %s\n", cfg.Daemon.OAuth.IssuerURL)
		}

		if cfg.Daemon.OAuth.IssuerURL != "" &&
			!strings.HasPrefix(cfg.Daemon.OAuth.IssuerURL, "https://") {
			if !strings.Contains(cfg.Daemon.OAuth.IssuerURL, "localhost") &&
				!strings.Contains(cfg.Daemon.OAuth.IssuerURL, "127.0.0.1") {
				warn("oauth.issuer_url", "should use HTTPS in production")
			}
		}

		pkf := cfg.Daemon.OAuth.PrivateKeyFile
		if pkf == "" {
			warn("oauth.private_key_file", "empty — will auto-generate on start")
		} else if strings.HasPrefix(pkf, "file:") {
			path := strings.TrimPrefix(pkf, "file:")
			_, pkfErr := os.Stat(path)
			check(pkfErr == nil, "oauth.private_key_file", path,
				fmt.Sprintf("create or copy your private key to %s", path))
		}

		if len(cfg.Daemon.OAuth.Clients) == 0 {
			warn("oauth.clients", "no clients registered")
		} else {
			fmt.Printf("  [ok]    oauth: %d client(s) registered\n", len(cfg.Daemon.OAuth.Clients))
		}
	} else {
		fmt.Println("  [ok]    oauth: disabled")
	}

	// 8. Destination health check (if daemon is alive).
	if port > 0 && checkDaemonAlive(port) {
		fmt.Println()
		fmt.Println("nexus doctor: checking destination health...")
		checkDestinationHealth(port, cfg, &hasErrors, &proposals)
	}

	// 9. Expanded checks: cloud sync, disk space, ports, permissions, filesystem.
	fmt.Println()
	fmt.Println("nexus doctor: running expanded checks...")
	expandedResults := RunAllChecks(configDir)
	for _, r := range expandedResults {
		switch r.Status {
		case "OK":
			fmt.Printf("  [ok]    %s: %s\n", r.Name, r.Message)
		case "WARN":
			warn(r.Name, r.Message)
		case "CRITICAL":
			check(false, r.Name, r.Message, "")
		}
	}

	// Print self-heal summary.
	if len(proposals) > 0 {
		fmt.Println()
		fmt.Println("nexus doctor: self-heal proposals:")
		for _, p := range proposals {
			fmt.Printf("  → %s\n", p)
		}
	}

	fmt.Println()
	if hasErrors {
		fmt.Println("nexus doctor: UNHEALTHY — issues found (see above)")
		os.Exit(1)
	}
	fmt.Println("nexus doctor: HEALTHY")
}

// checkWritable returns true if path exists and a temp file can be created in it.
func checkWritable(path string) bool {
	if err := os.MkdirAll(path, 0700); err != nil {
		return false
	}
	f, err := os.CreateTemp(path, ".nexus-writable-*")
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return true
}

// checkDaemonAlive pings GET /health and returns true if the response is 200.
func checkDaemonAlive(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// checkDestinationHealth calls GET /health and inspects the subsystems map.
func checkDestinationHealth(port int, cfg *config.Config, hasErrors *bool, proposals *[]string) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		fmt.Printf("  [WARN]  health_endpoint: %v\n", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Status     string                 `json:"status"`
		Subsystems map[string]interface{} `json:"subsystems"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Printf("  [WARN]  health_endpoint: parse: %v\n", err)
		return
	}

	for name, sub := range result.Subsystems {
		subMap, _ := sub.(map[string]interface{})
		status, _ := subMap["status"].(string)
		details, _ := subMap["details"].(string)

		if status == "ok" {
			fmt.Printf("  [ok]    subsystem.%s\n", name)
		} else if status == "degraded" {
			fmt.Printf("  [WARN]  subsystem.%s: %s\n", name, details)
		} else if status == "unavailable" {
			fmt.Printf("  [WARN]  subsystem.%s: %s (feature disabled)\n", name, details)
		}
	}

	if result.Status != "ok" {
		fmt.Printf("  [ERROR] overall health: %s\n", result.Status)
		*hasErrors = true
		*proposals = append(*proposals, "check daemon logs: run 'nexus logs --level warn'")
	}
}

// runFsyncTest executes `nexus doctor --fsync-test`.
// Writes data, fsyncs, reads back via fresh fd, and verifies the bytes match.
// Detects broken fsync on network storage and some consumer SSDs.
//
// Reference: v0.1.3 Build Plan Phase 1 Subtask 1.6.
func runFsyncTest() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus doctor --fsync-test: %v\n", err)
		os.Exit(1)
	}
	walDir := filepath.Join(home, ".nexus", "Nexus", "wal")
	if err := os.MkdirAll(walDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "nexus doctor --fsync-test: create WAL dir: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("nexus doctor --fsync-test: testing fsync in %s\n", walDir)
	result := doctor.FsyncTest(walDir)
	if result.OK {
		fmt.Printf("nexus doctor --fsync-test: ok — fsync verified in %s\n", result.Duration)
	} else {
		fmt.Fprintf(os.Stderr, "nexus doctor --fsync-test: FAIL — %s\n", result.Error)
		fmt.Fprintln(os.Stderr, "  WARNING: fsync may not be flushing data to durable storage.")
		fmt.Fprintln(os.Stderr, "  This filesystem may silently lose data on power failure.")
		os.Exit(1)
	}
}

func runMemoryHealth() {
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus doctor --memory-health: %v\n", err)
		os.Exit(1)
	}

	regPath := filepath.Join(configDir, "a2a", "registry.db")
	db, err := sql.Open("sqlite", regPath+"?_pragma=busy_timeout%3d5000")
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus doctor --memory-health: cannot open registry DB: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	h, err := health.CalculateMemoryHealth(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus doctor --memory-health: %v\n", err)
		os.Exit(1)
	}

	pct := h.ContinuityScore * 100
	fmt.Fprintf(os.Stderr, "\nMemory Health Report (last 7 days)\n")
	fmt.Fprintf(os.Stderr, "══════════════════════════════════\n")
	fmt.Fprintf(os.Stderr, "  Continuity Score:   %.1f%%  (%d/%d memories retrievable)\n",
		pct, h.RetrievableCount, h.TotalMemories7d)
	fmt.Fprintf(os.Stderr, "  Crash Recoveries:   %d\n", h.CrashRecoveries)
	fmt.Fprintf(os.Stderr, "  Quarantined:        %d       (blocked before entering memory pool)\n",
		h.QuarantineCount7d)
	fmt.Fprintf(os.Stderr, "\n  Cross-Agent Coverage:\n")
	fmt.Fprintf(os.Stderr, "    Writing agents:   %d\n", h.CrossAgentCoverage.WritingAgents)
	fmt.Fprintf(os.Stderr, "    Reading agents:   %d\n", h.CrossAgentCoverage.ReadingAgents)

	if len(h.CrossAgentCoverage.AgentBreakdown) > 0 {
		fmt.Fprintf(os.Stderr, "\n    Agent Breakdown:\n")
		for _, a := range h.CrossAgentCoverage.AgentBreakdown {
			fmt.Fprintf(os.Stderr, "      %-22s %4d writes %4d reads   last active %s\n",
				a.AgentID, a.Writes, a.Reads, a.LastActive)
		}
	}
	fmt.Fprintln(os.Stderr)
}

// runDoctorRepair attempts to fix common configuration issues.
func runDoctorRepair() {
	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus doctor --repair: %v\n", err)
		os.Exit(1)
	}

	repairDirs := []string{
		filepath.Join(configDir, "keys"),
		filepath.Join(configDir, "logs"),
		filepath.Join(configDir, "wal"),
		filepath.Join(configDir, "discovery"),
		filepath.Join(configDir, "tools"),
	}

	for _, d := range repairDirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			fmt.Fprintf(os.Stderr, "  [ERROR] create %s: %v\n", d, err)
		} else {
			fmt.Printf("  [ok]    ensured %s exists\n", d)
		}
	}

	// Fix keys directory permissions (Unix only).
	keysDir := filepath.Join(configDir, "keys")
	if err := os.Chmod(keysDir, 0700); err == nil {
		fmt.Printf("  [ok]    keys directory permissions set to 0700\n")
	}

	fmt.Println()
	fmt.Println("nexus doctor --repair: complete")
}
