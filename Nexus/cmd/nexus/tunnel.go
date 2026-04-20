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
	"bufio"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/bubblefish-tech/nexus/internal/config"
)

// runTunnel executes the `nexus tunnel` command family.
//
// Subcommands:
//
//	tunnel setup   — interactive tunnel configuration wizard
//	tunnel doctor  — diagnose tunnel issues
//	tunnel status  — show configured tunnels and their reachability
//
// Reference: Tech Spec WIRE.7.
func runTunnel(args []string) {
	if len(args) == 0 {
		printTunnelUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "setup":
		runTunnelSetup()
	case "doctor":
		runTunnelDoctor()
	case "status":
		runTunnelStatus()
	default:
		fmt.Fprintf(os.Stderr, "nexus tunnel: unknown subcommand %q\n", args[0])
		printTunnelUsage()
		os.Exit(1)
	}
}

func printTunnelUsage() {
	fmt.Fprintln(os.Stderr, "usage: nexus tunnel <subcommand>")
	fmt.Fprintln(os.Stderr, "subcommands:")
	fmt.Fprintln(os.Stderr, "  setup    add a [[tunnels]] entry to daemon.toml interactively")
	fmt.Fprintln(os.Stderr, "  doctor   diagnose tunnel configuration and connectivity")
	fmt.Fprintln(os.Stderr, "  status   show configured tunnels and whether they appear reachable")
}

// runTunnelSetup prints an interactive wizard that emits TOML the user can
// paste into daemon.toml. It does NOT write the file automatically — the user
// controls their config.
func runTunnelSetup() {
	fmt.Println("nexus tunnel setup: interactive tunnel configurator")
	fmt.Println()
	fmt.Println("Supported providers:")
	fmt.Println("  1. cloudflare  — Cloudflare Tunnel (cloudflared)")
	fmt.Println("  2. ngrok       — ngrok HTTP tunnel")
	fmt.Println("  3. tailscale   — Tailscale Funnel")
	fmt.Println("  4. bore        — Bore (open-source)")
	fmt.Println("  5. custom      — custom shell command")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	provider := prompt(scanner, "Provider [cloudflare]: ")
	if provider == "" {
		provider = "cloudflare"
	}
	provider = strings.ToLower(provider)

	localPortStr := prompt(scanner, "Local daemon port [6482]: ")
	if localPortStr == "" {
		localPortStr = "6482"
	}

	var toml strings.Builder
	toml.WriteString("\n[[tunnels]]\n")
	toml.WriteString(fmt.Sprintf("provider   = %q\n", provider))
	toml.WriteString(fmt.Sprintf("local_port = %s\n", localPortStr))
	toml.WriteString("enabled    = true\n")

	switch provider {
	case "cloudflare":
		token := prompt(scanner, "Cloudflare tunnel token (or env:VAR_NAME): ")
		hostname := prompt(scanner, "Hostname (e.g. nexus.example.com) [optional]: ")
		if token != "" {
			toml.WriteString(fmt.Sprintf("auth_token = %q\n", token))
		}
		if hostname != "" {
			toml.WriteString(fmt.Sprintf("hostname   = %q\n", hostname))
		}
		fmt.Println()
		fmt.Println("Prerequisites: install cloudflared and authenticate:")
		fmt.Println("  https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/install-and-setup/installation/")
		fmt.Println("Start tunnel manually: cloudflared tunnel --url http://127.0.0.1:" + localPortStr)

	case "ngrok":
		token := prompt(scanner, "ngrok authtoken (or env:VAR_NAME): ")
		region := prompt(scanner, "Region [us]: ")
		if region == "" {
			region = "us"
		}
		if token != "" {
			toml.WriteString(fmt.Sprintf("auth_token = %q\n", token))
		}
		toml.WriteString(fmt.Sprintf("region     = %q\n", region))
		fmt.Println()
		fmt.Println("Prerequisites: install ngrok and authenticate:")
		fmt.Println("  ngrok config add-authtoken <your-token>")
		fmt.Println("Start tunnel manually: ngrok http " + localPortStr)

	case "tailscale":
		domain := prompt(scanner, "Tailscale domain (e.g. myhost.tailnet-name.ts.net) [optional]: ")
		if domain != "" {
			toml.WriteString(fmt.Sprintf("domain = %q\n", domain))
		}
		fmt.Println()
		fmt.Println("Prerequisites: install Tailscale and enable HTTPS Funnel:")
		fmt.Println("  tailscale funnel " + localPortStr)

	case "bore":
		address := prompt(scanner, "Bore server address [bore.pub]: ")
		if address == "" {
			address = "bore.pub"
		}
		toml.WriteString(fmt.Sprintf("address = %q\n", address))
		fmt.Println()
		fmt.Println("Prerequisites: install bore (cargo install bore-cli):")
		fmt.Println("  bore local " + localPortStr + " --to " + address)

	case "custom":
		command := prompt(scanner, "Tunnel command ({port} = local daemon port): ")
		toml.WriteString(fmt.Sprintf("command = %q\n", command))

	default:
		fmt.Fprintf(os.Stderr, "nexus tunnel setup: unknown provider %q\n", provider)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────────")
	fmt.Println("Add the following to your daemon.toml:")
	fmt.Println("─────────────────────────────────────────────────────")
	fmt.Print(toml.String())
	fmt.Println("─────────────────────────────────────────────────────")
	fmt.Println()
	fmt.Println("After editing daemon.toml, restart the daemon:")
	fmt.Println("  nexus stop && nexus start")
}

// runTunnelDoctor checks each configured tunnel for obvious issues.
func runTunnelDoctor() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus tunnel doctor: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(configDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus tunnel doctor: config: %v\n", err)
		os.Exit(1)
	}

	if len(cfg.Tunnels) == 0 {
		fmt.Println("nexus tunnel doctor: no tunnels configured")
		fmt.Println("  Run 'nexus tunnel setup' to add a tunnel.")
		return
	}

	hasErrors := false
	for i, t := range cfg.Tunnels {
		prefix := fmt.Sprintf("  tunnel[%d] provider=%s", i, t.Provider)
		if !t.Enabled {
			fmt.Printf("%s: DISABLED\n", prefix)
			continue
		}
		if t.LocalPort == 0 {
			fmt.Printf("%s: [ERROR] local_port not set\n", prefix)
			hasErrors = true
			continue
		}

		switch t.Provider {
		case "cloudflare":
			if t.AuthToken == "" {
				fmt.Printf("%s: [WARN]  auth_token not set (required for cloudflared tunnel run)\n", prefix)
			} else {
				fmt.Printf("%s: [ok]    auth_token configured\n", prefix)
			}
		case "ngrok":
			if t.AuthToken == "" {
				fmt.Printf("%s: [WARN]  auth_token not set (ngrok requires authentication)\n", prefix)
			} else {
				fmt.Printf("%s: [ok]    auth_token configured\n", prefix)
			}
		case "tailscale":
			fmt.Printf("%s: [ok]    tailscale funnel (no token required in config)\n", prefix)
		case "bore":
			if t.Address == "" {
				fmt.Printf("%s: [WARN]  address not set (defaulting to bore.pub)\n", prefix)
			} else {
				fmt.Printf("%s: [ok]    address = %s\n", prefix, t.Address)
			}
		case "custom":
			if t.Command == "" {
				fmt.Printf("%s: [ERROR] command not set\n", prefix)
				hasErrors = true
			} else {
				fmt.Printf("%s: [ok]    command configured\n", prefix)
			}
		default:
			fmt.Printf("%s: [ERROR] unknown provider %q (supported: cloudflare, ngrok, tailscale, bore, custom)\n", prefix, t.Provider)
			hasErrors = true
		}
	}

	fmt.Println()
	if hasErrors {
		fmt.Println("nexus tunnel doctor: issues found — see above")
		os.Exit(1)
	}
	fmt.Println("nexus tunnel doctor: ok")
}

// runTunnelStatus shows configured tunnels and probes whether the local port
// is listening (a proxy for whether the tunnel is likely active).
func runTunnelStatus() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	configDir, err := config.ConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus tunnel status: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(configDir, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus tunnel status: config: %v\n", err)
		os.Exit(1)
	}

	if len(cfg.Tunnels) == 0 {
		fmt.Println("nexus tunnel status: no tunnels configured")
		return
	}

	fmt.Printf("%-12s %-10s %-10s %s\n", "PROVIDER", "PORT", "ENABLED", "LOCAL STATUS")
	fmt.Println(strings.Repeat("-", 55))

	client := &http.Client{Timeout: 2 * time.Second}
	for _, t := range cfg.Tunnels {
		enabledStr := "yes"
		if !t.Enabled {
			enabledStr = "no"
		}

		status := "unknown"
		if t.LocalPort > 0 {
			resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", t.LocalPort))
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					status = "daemon ok"
				} else {
					status = fmt.Sprintf("http %d", resp.StatusCode)
				}
			} else {
				status = "not reachable"
			}
		}

		fmt.Printf("%-12s %-10d %-10s %s\n", t.Provider, t.LocalPort, enabledStr, status)
	}
}

func prompt(scanner *bufio.Scanner, msg string) string {
	fmt.Print(msg)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}
