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
	"os"
)

// runA2A dispatches A2A management subcommands.
//
// Usage:
//
//	bubblefish a2a <subcommand>
//	bubblefish a2a agent   — manage A2A agents
//	bubblefish a2a grant   — manage governance grants
//	bubblefish a2a task    — inspect tasks
//	bubblefish a2a audit   — audit log
func runA2A(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish a2a <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  agent   manage A2A agents (add, list, show, test, suspend, retire)")
		fmt.Fprintln(os.Stderr, "  grant   manage governance grants (add, list, revoke)")
		fmt.Fprintln(os.Stderr, "  task    inspect tasks (get, cancel, list)")
		fmt.Fprintln(os.Stderr, "  audit   audit log (tail)")
		os.Exit(1)
	}

	switch args[0] {
	case "agent":
		runA2AAgent(args[1:])
	case "grant":
		runA2AGrant(args[1:])
	case "task":
		runA2ATask(args[1:])
	case "audit":
		runA2AAudit(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "bubblefish a2a: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}
