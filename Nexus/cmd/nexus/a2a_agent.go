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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/client"
	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
)

// runA2AAgent dispatches agent management subcommands for A2A.
func runA2AAgent(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: nexus a2a agent <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  add      register a remote A2A agent")
		fmt.Fprintln(os.Stderr, "  list     list all registered A2A agents")
		fmt.Fprintln(os.Stderr, "  show     show details for an A2A agent by name")
		fmt.Fprintln(os.Stderr, "  test     ping an A2A agent to verify connectivity")
		fmt.Fprintln(os.Stderr, "  suspend  suspend an A2A agent")
		fmt.Fprintln(os.Stderr, "  retire   retire an A2A agent")
		os.Exit(1)
	}

	switch args[0] {
	case "add":
		runA2AAgentAdd(args[1:])
	case "list":
		runA2AAgentList()
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus a2a agent show <name>")
			os.Exit(1)
		}
		runA2AAgentShow(args[1])
	case "test":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus a2a agent test <name>")
			os.Exit(1)
		}
		runA2AAgentTest(args[1])
	case "suspend":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus a2a agent suspend <name>")
			os.Exit(1)
		}
		runA2AAgentStatusChange(args[1], registry.StatusSuspended)
	case "retire":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: nexus a2a agent retire <name>")
			os.Exit(1)
		}
		runA2AAgentStatusChange(args[1], registry.StatusRetired)
	default:
		fmt.Fprintf(os.Stderr, "nexus a2a agent: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// openA2AStore opens the A2A SQLite registry store.
func openA2AStore() *registry.Store {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a: resolve home dir: %v\n", err)
		os.Exit(1)
	}

	dbDir := filepath.Join(home, ".nexus", "Nexus", "a2a")
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a: create database directory: %v\n", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(dbDir, "a2a.db")
	store, err := registry.NewStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a: open database: %v\n", err)
		os.Exit(1)
	}
	return store
}

func runA2AAgentAdd(args []string) {
	var (
		name      string
		tKind     string
		url       string
		authType  string
	)

	for i := 0; i < len(args); i++ {
		switch {
		case (args[i] == "--transport" || args[i] == "-t") && i+1 < len(args):
			tKind = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--transport="):
			tKind = strings.TrimPrefix(args[i], "--transport=")
		case (args[i] == "--url" || args[i] == "-u") && i+1 < len(args):
			url = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--url="):
			url = strings.TrimPrefix(args[i], "--url=")
		case args[i] == "--auth" && i+1 < len(args):
			authType = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--auth="):
			authType = strings.TrimPrefix(args[i], "--auth=")
		default:
			// First positional argument is the name.
			if name == "" && !strings.HasPrefix(args[i], "-") {
				name = args[i]
			} else {
				fmt.Fprintf(os.Stderr, "nexus a2a agent add: unknown flag %q\n", args[i])
				os.Exit(1)
			}
		}
	}

	if name == "" {
		fmt.Fprintln(os.Stderr, "usage: nexus a2a agent add <name> --transport http --url <url> [--auth bearer]")
		os.Exit(1)
	}
	if tKind == "" {
		tKind = "http"
	}
	if authType == "" {
		authType = "none"
	}

	tcfg := transport.TransportConfig{
		Kind:     tKind,
		URL:      url,
		AuthType: authType,
	}

	if err := tcfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a agent add: %v\n", err)
		os.Exit(1)
	}

	agentID := a2a.NewTaskID() // use ULID generation for a unique ID
	// Generate a proper agent ID with a custom prefix.
	agentID = "agt_" + agentID[4:] // replace tsk_ prefix

	card := a2a.AgentCard{
		Name:            name,
		URL:             url,
		ProtocolVersion: a2a.ProtocolVersion,
		Endpoints: []a2a.Endpoint{
			{URL: url, Transport: a2a.TransportKind(tKind)},
		},
	}

	agent := registry.RegisteredAgent{
		AgentID:         agentID,
		Name:            name,
		DisplayName:     name,
		AgentCard:       card,
		TransportConfig: tcfg,
		Status:          registry.StatusActive,
	}

	store := openA2AStore()
	defer store.Close()

	ctx := context.Background()
	if err := store.Register(ctx, agent); err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a agent add: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(map[string]string{
		"agentId":   agentID,
		"name":      name,
		"transport": tKind,
		"url":       url,
		"status":    registry.StatusActive,
	}, "", "  ")
	fmt.Println(string(out))
}

func runA2AAgentList() {
	store := openA2AStore()
	defer store.Close()

	ctx := context.Background()
	agents, err := store.List(ctx, registry.ListFilter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a agent list: %v\n", err)
		os.Exit(1)
	}

	type agentSummary struct {
		AgentID   string  `json:"agentId"`
		Name      string  `json:"name"`
		Status    string  `json:"status"`
		Transport string  `json:"transport"`
		URL       string  `json:"url,omitempty"`
		LastSeen  *string `json:"lastSeen,omitempty"`
	}

	summaries := make([]agentSummary, len(agents))
	for i, ag := range agents {
		s := agentSummary{
			AgentID:   ag.AgentID,
			Name:      ag.Name,
			Status:    ag.Status,
			Transport: ag.TransportConfig.Kind,
			URL:       ag.TransportConfig.URL,
		}
		if ag.LastSeenAt != nil {
			ts := a2a.FormatTime(*ag.LastSeenAt)
			s.LastSeen = &ts
		}
		summaries[i] = s
	}

	out, _ := json.MarshalIndent(summaries, "", "  ")
	fmt.Println(string(out))
}

func runA2AAgentShow(name string) {
	store := openA2AStore()
	defer store.Close()

	ctx := context.Background()
	agent, err := store.GetByName(ctx, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a agent show: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(agent, "", "  ")
	fmt.Println(string(out))
}

func runA2AAgentTest(name string) {
	store := openA2AStore()
	defer store.Close()

	ctx := context.Background()
	agent, err := store.GetByName(ctx, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a agent test: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	factory := client.NewFactory(logger)
	c, err := factory.NewClient(ctx, *agent)
	if err != nil {
		result, _ := json.MarshalIndent(map[string]interface{}{
			"ok":    false,
			"error": err.Error(),
		}, "", "  ")
		fmt.Println(string(result))
		os.Exit(1)
	}
	defer c.Close()

	// The factory already pings as part of NewClient, so if we got here, it's OK.
	result, _ := json.MarshalIndent(map[string]interface{}{
		"ok":      true,
		"agentId": agent.AgentID,
		"name":    agent.Name,
	}, "", "  ")
	fmt.Println(string(result))
}

func runA2AAgentStatusChange(name, status string) {
	store := openA2AStore()
	defer store.Close()

	ctx := context.Background()

	// Look up by name to get the ID.
	agent, err := store.GetByName(ctx, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a agent %s: %v\n", status, err)
		os.Exit(1)
	}

	if err := store.UpdateStatus(ctx, agent.AgentID, status); err != nil {
		fmt.Fprintf(os.Stderr, "nexus a2a agent %s: %v\n", status, err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(map[string]string{
		"agentId": agent.AgentID,
		"name":    agent.Name,
		"status":  status,
	}, "", "  ")
	fmt.Println(string(out))
}
