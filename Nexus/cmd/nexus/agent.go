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
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/bubblefish-tech/nexus/internal/agent"
	_ "modernc.org/sqlite"
)

// runAgent dispatches agent management subcommands.
//
// Usage:
//
//	nexus agent register --name <name> [--description <desc>]
//	nexus agent list
//	nexus agent show <agent_id>
//	nexus agent suspend <agent_id>
//	nexus agent retire <agent_id>
//
// Reference: v0.1.3 Agent Gateway Build Plan AG.1.
func runAgent(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: nexus agent <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  register   register a new agent identity")
		fmt.Fprintln(os.Stderr, "  list       list all registered agents")
		fmt.Fprintln(os.Stderr, "  show       show full details for an agent")
		fmt.Fprintln(os.Stderr, "  suspend    suspend an agent (reject future writes)")
		fmt.Fprintln(os.Stderr, "  retire     retire an agent (soft delete, preserve audit)")
		fmt.Fprintln(os.Stderr, "  health     show health status for all agents")
		os.Exit(1)
	}

	switch args[0] {
	case "register":
		runAgentRegister(args[1:])
	case "list":
		runAgentList()
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus agent show <agent_id>")
			os.Exit(1)
		}
		runAgentShow(args[1])
	case "suspend":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus agent suspend <agent_id>")
			os.Exit(1)
		}
		runAgentSuspend(args[1])
	case "retire":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus agent retire <agent_id>")
			os.Exit(1)
		}
		runAgentRetire(args[1])
	case "health":
		runAgentHealth()
	default:
		fmt.Fprintf(os.Stderr, "nexus agent: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// openAgentRegistry opens the nexus.db SQLite database and returns a ready
// Registry. The caller must close the returned *sql.DB when done.
func openAgentRegistry() (*agent.Registry, *sql.DB) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus agent: resolve home dir: %v\n", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(home, ".nexus", "Nexus", "nexus.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus agent: open database: %v\n", err)
		os.Exit(1)
	}

	reg, err := agent.NewRegistry(db)
	if err != nil {
		db.Close()
		fmt.Fprintf(os.Stderr, "nexus agent: init registry: %v\n", err)
		os.Exit(1)
	}

	return reg, db
}

func runAgentRegister(args []string) {
	var name, description string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--name" && i+1 < len(args):
			name = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--name="):
			name = strings.TrimPrefix(args[i], "--name=")
		case args[i] == "--description" && i+1 < len(args):
			description = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--description="):
			description = strings.TrimPrefix(args[i], "--description=")
		default:
			fmt.Fprintf(os.Stderr, "nexus agent register: unknown flag %q\n", args[i])
			os.Exit(1)
		}
	}

	if name == "" {
		fmt.Fprintln(os.Stderr, "usage: nexus agent register --name <name> [--description <desc>]")
		os.Exit(1)
	}

	reg, db := openAgentRegistry()
	defer db.Close()

	id, err := reg.Register(name, description)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus agent register: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("nexus agent register: ok\n")
	fmt.Printf("  agent_id: %s\n", id)
	fmt.Printf("  name:     %s\n", name)
}

func runAgentList() {
	reg, db := openAgentRegistry()
	defer db.Close()

	agents, err := reg.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus agent list: %v\n", err)
		os.Exit(1)
	}

	if len(agents) == 0 {
		fmt.Println("no agents registered")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT_ID\tNAME\tSTATUS\tLAST SEEN")
	for _, a := range agents {
		lastSeen := "-"
		if !a.LastSeenAt.IsZero() {
			lastSeen = a.LastSeenAt.Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, a.Name, a.Status, lastSeen)
	}
	w.Flush()
}

func runAgentShow(idOrName string) {
	reg, db := openAgentRegistry()
	defer db.Close()

	a, err := reg.Get(idOrName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus agent show: %v\n", err)
		os.Exit(1)
	}

	// Try by name if not found by ID.
	if a == nil {
		a, err = reg.GetByName(idOrName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nexus agent show: %v\n", err)
			os.Exit(1)
		}
	}

	if a == nil {
		fmt.Fprintf(os.Stderr, "nexus agent show: agent %q not found\n", idOrName)
		os.Exit(1)
	}

	fmt.Printf("agent_id:    %s\n", a.ID)
	fmt.Printf("name:        %s\n", a.Name)
	fmt.Printf("description: %s\n", a.Description)
	fmt.Printf("status:      %s\n", a.Status)
	fmt.Printf("created_at:  %s\n", a.CreatedAt.Format("2006-01-02 15:04:05"))
	lastSeen := "-"
	if !a.LastSeenAt.IsZero() {
		lastSeen = a.LastSeenAt.Format("2006-01-02 15:04:05")
	}
	fmt.Printf("last_seen:   %s\n", lastSeen)
	if len(a.Metadata) > 0 {
		fmt.Printf("metadata:\n")
		for k, v := range a.Metadata {
			fmt.Printf("  %s: %s\n", k, v)
		}
	}
}

func runAgentSuspend(id string) {
	reg, db := openAgentRegistry()
	defer db.Close()

	if err := reg.Suspend(id); err != nil {
		fmt.Fprintf(os.Stderr, "nexus agent suspend: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("nexus agent suspend: ok — agent %s suspended\n", id)
}

func runAgentRetire(id string) {
	reg, db := openAgentRegistry()
	defer db.Close()

	if err := reg.Retire(id); err != nil {
		fmt.Fprintf(os.Stderr, "nexus agent retire: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("nexus agent retire: ok — agent %s retired\n", id)
}

func runAgentHealth() {
	reg, db := openAgentRegistry()
	defer db.Close()

	agents, err := reg.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus agent health: %v\n", err)
		os.Exit(1)
	}

	if len(agents) == 0 {
		fmt.Println("no agents registered")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT_ID\tNAME\tSTATUS\tHEALTH\tLAST SEEN")
	for _, a := range agents {
		lastSeen := "-"
		health := "dormant"
		if !a.LastSeenAt.IsZero() {
			lastSeen = a.LastSeenAt.Format("2006-01-02 15:04:05")
			elapsed := time.Since(a.LastSeenAt)
			switch {
			case elapsed >= 24*time.Hour:
				health = "dormant"
			case elapsed >= 1*time.Hour:
				health = "inactive"
			case elapsed >= 5*time.Minute:
				health = "stale"
			default:
				health = "active"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", a.ID, a.Name, a.Status, health, lastSeen)
	}
	w.Flush()
}
