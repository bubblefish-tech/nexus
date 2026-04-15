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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/governance"
	_ "modernc.org/sqlite"
)

// runA2AGrant dispatches grant management subcommands for A2A.
func runA2AGrant(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish a2a grant <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  add     create a governance grant")
		fmt.Fprintln(os.Stderr, "  list    list governance grants")
		fmt.Fprintln(os.Stderr, "  revoke  revoke a governance grant")
		os.Exit(1)
	}

	switch args[0] {
	case "add":
		runA2AGrantAdd(args[1:])
	case "list":
		runA2AGrantList(args[1:])
	case "revoke":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: bubblefish a2a grant revoke <grant_id>")
			os.Exit(1)
		}
		runA2AGrantRevoke(args[1])
	default:
		fmt.Fprintf(os.Stderr, "bubblefish a2a grant: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// openA2AGrantStore opens the governance grant store.
func openA2AGrantStore() (*governance.GrantStore, *sql.DB) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a grant: resolve home dir: %v\n", err)
		os.Exit(1)
	}

	dbDir := filepath.Join(home, ".bubblefish", "Nexus", "a2a")
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a grant: create database directory: %v\n", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(dbDir, "a2a.db")
	dsn := dbPath + "?_pragma=busy_timeout%3d5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a grant: open database: %v\n", err)
		os.Exit(1)
	}

	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			fmt.Fprintf(os.Stderr, "bubblefish a2a grant: %s: %v\n", pragma, err)
			os.Exit(1)
		}
	}

	if err := governance.MigrateGrants(db); err != nil {
		db.Close()
		fmt.Fprintf(os.Stderr, "bubblefish a2a grant: migrate: %v\n", err)
		os.Exit(1)
	}

	return governance.NewGrantStore(db), db
}

func runA2AGrantAdd(args []string) {
	var (
		source     string
		target     string
		capability string
		expires    string
	)

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--source" && i+1 < len(args):
			source = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--source="):
			source = strings.TrimPrefix(args[i], "--source=")
		case args[i] == "--target" && i+1 < len(args):
			target = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--target="):
			target = strings.TrimPrefix(args[i], "--target=")
		case args[i] == "--capability" && i+1 < len(args):
			capability = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--capability="):
			capability = strings.TrimPrefix(args[i], "--capability=")
		case args[i] == "--expires" && i+1 < len(args):
			expires = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--expires="):
			expires = strings.TrimPrefix(args[i], "--expires=")
		default:
			fmt.Fprintf(os.Stderr, "bubblefish a2a grant add: unknown flag %q\n", args[i])
			os.Exit(1)
		}
	}

	if source == "" || target == "" || capability == "" {
		fmt.Fprintln(os.Stderr, "usage: bubblefish a2a grant add --source <src> --target <tgt> --capability <glob> [--expires <duration>]")
		os.Exit(1)
	}

	now := time.Now()
	grant := &governance.Grant{
		GrantID:        a2a.NewGrantID(),
		SourceAgentID:  source,
		TargetAgentID:  target,
		CapabilityGlob: capability,
		Scope:          "SCOPED",
		Decision:       "allow",
		IssuedBy:       "cli",
		IssuedAt:       now,
	}

	if expires != "" {
		dur, err := time.ParseDuration(expires)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish a2a grant add: invalid duration %q: %v\n", expires, err)
			os.Exit(1)
		}
		exp := now.Add(dur)
		grant.ExpiresAt = &exp
	}

	store, db := openA2AGrantStore()
	defer db.Close()

	if err := store.CreateGrant(grant); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a grant add: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(map[string]interface{}{
		"grantId":        grant.GrantID,
		"sourceAgentId":  grant.SourceAgentID,
		"targetAgentId":  grant.TargetAgentID,
		"capabilityGlob": grant.CapabilityGlob,
		"decision":       grant.Decision,
		"issuedAt":       a2a.FormatTime(grant.IssuedAt),
	}, "", "  ")
	fmt.Println(string(out))
}

func runA2AGrantList(args []string) {
	var source, target string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--source" && i+1 < len(args):
			source = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--source="):
			source = strings.TrimPrefix(args[i], "--source=")
		case args[i] == "--target" && i+1 < len(args):
			target = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--target="):
			target = strings.TrimPrefix(args[i], "--target=")
		default:
			fmt.Fprintf(os.Stderr, "bubblefish a2a grant list: unknown flag %q\n", args[i])
			os.Exit(1)
		}
	}

	store, db := openA2AGrantStore()
	defer db.Close()

	var grants []*governance.Grant
	var err error

	if source != "" && target != "" {
		grants, err = store.FindMatchingGrants(source, target)
	} else {
		grants, err = store.ListGrants()
		// Client-side filter if only source or target specified.
		if err == nil && (source != "" || target != "") {
			var filtered []*governance.Grant
			for _, g := range grants {
				if source != "" && g.SourceAgentID != source {
					continue
				}
				if target != "" && g.TargetAgentID != target {
					continue
				}
				filtered = append(filtered, g)
			}
			grants = filtered
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a grant list: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(grants, "", "  ")
	fmt.Println(string(out))
}

func runA2AGrantRevoke(grantID string) {
	store, db := openA2AGrantStore()
	defer db.Close()

	if err := store.RevokeGrant(grantID, time.Now()); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a grant revoke: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(map[string]interface{}{
		"ok":      true,
		"grantId": grantID,
	}, "", "  ")
	fmt.Println(string(out))
}
