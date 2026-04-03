// Command bubblefish is the single-binary entrypoint for BubbleFish Nexus.
package main

import (
	"fmt"
	"os"

	"github.com/shawnsammartano/bubblefish-nexus/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		cmdInit()
	case "build":
		cmdBuild()
	case "daemon":
		cmdDaemon()
	case "doctor":
		cmdDoctor()
	case "version":
		fmt.Println("BubbleFish Nexus", version.Version)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `BubbleFish Nexus %s

Usage:
  bubblefish <command>

Commands:
  init      Scaffold ~/.bubblefish/ with starter TOML config templates
  build     Validate TOMLs and write compiled JSON configs
  daemon    Start the HTTP routing daemon
  doctor    Run pre-flight health checks
  version   Print version and exit

`, version.Version)
}

// cmdInit scaffolds the user config directory with starter TOML templates.
// Implemented in Phase 1.
func cmdInit() {
	fmt.Println("init: not yet implemented (Phase 1)")
}

// cmdBuild validates all TOMLs and writes pre-compiled JSON configs.
// Implemented in Phase 1.
func cmdBuild() {
	fmt.Println("build: not yet implemented (Phase 1)")
}

// cmdDaemon starts the HTTP routing daemon.
// Implemented in Phase 3.
func cmdDaemon() {
	fmt.Println("daemon: not yet implemented (Phase 3)")
}

// cmdDoctor runs pre-flight health checks on all destinations and config.
// Implemented in Phase 4.
func cmdDoctor() {
	fmt.Println("doctor: not yet implemented (Phase 4)")
}
