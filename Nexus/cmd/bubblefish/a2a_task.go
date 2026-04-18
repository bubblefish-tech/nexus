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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/server"
	"github.com/BubbleFish-Nexus/internal/a2a/store"
)

// runA2ATask dispatches task inspection subcommands for A2A.
func runA2ATask(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish a2a task <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  get     get a task by ID")
		fmt.Fprintln(os.Stderr, "  cancel  cancel a task by ID")
		fmt.Fprintln(os.Stderr, "  list    list tasks")
		os.Exit(1)
	}

	switch args[0] {
	case "get":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: bubblefish a2a task get <task_id>")
			os.Exit(1)
		}
		runA2ATaskGet(args[1])
	case "cancel":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: bubblefish a2a task cancel <task_id>")
			os.Exit(1)
		}
		runA2ATaskCancel(args[1])
	case "list":
		runA2ATaskList(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "bubblefish a2a task: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

// openA2ATaskStore opens the A2A task store.
func openA2ATaskStore() *store.SQLiteTaskStore {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a task: resolve home dir: %v\n", err)
		os.Exit(1)
	}

	dbDir := filepath.Join(home, ".bubblefish", "Nexus", "a2a")
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a task: create database directory: %v\n", err)
		os.Exit(1)
	}

	dbPath := filepath.Join(dbDir, "a2a.db")
	ts, err := store.NewSQLiteTaskStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a task: open database: %v\n", err)
		os.Exit(1)
	}
	return ts
}

func runA2ATaskGet(taskID string) {
	ts := openA2ATaskStore()
	defer ts.Close()

	ctx := context.Background()
	task, err := ts.GetTask(ctx, taskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a task get: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(task, "", "  ")
	fmt.Println(string(out))
}

func runA2ATaskCancel(taskID string) {
	ts := openA2ATaskStore()
	defer ts.Close()

	ctx := context.Background()

	status := a2a.TaskStatus{
		State:     a2a.TaskStateCanceled,
		Timestamp: a2a.Now(),
	}
	if err := ts.UpdateTaskStatus(ctx, taskID, status); err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a task cancel: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(map[string]interface{}{
		"ok":     true,
		"taskId": taskID,
		"state":  string(a2a.TaskStateCanceled),
	}, "", "  ")
	fmt.Println(string(out))
}

func runA2ATaskList(args []string) {
	var (
		since string
		state string
	)

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--since" && i+1 < len(args):
			since = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--since="):
			since = strings.TrimPrefix(args[i], "--since=")
		case args[i] == "--state" && i+1 < len(args):
			state = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--state="):
			state = strings.TrimPrefix(args[i], "--state=")
		default:
			fmt.Fprintf(os.Stderr, "bubblefish a2a task list: unknown flag %q\n", args[i])
			os.Exit(1)
		}
	}

	filter := server.TaskFilter{
		Limit: 100,
	}

	if since != "" {
		dur, err := time.ParseDuration(since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish a2a task list: invalid duration %q: %v\n", since, err)
			os.Exit(1)
		}
		filter.Since = time.Now().Add(-dur)
	}

	if state != "" {
		ts, ok := a2a.ParseTaskState(state)
		if !ok {
			fmt.Fprintf(os.Stderr, "bubblefish a2a task list: unknown state %q\n", state)
			os.Exit(1)
		}
		filter.State = ts
	}

	ts := openA2ATaskStore()
	defer ts.Close()

	ctx := context.Background()
	tasks, err := ts.ListTasks(ctx, filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish a2a task list: %v\n", err)
		os.Exit(1)
	}

	out, _ := json.MarshalIndent(tasks, "", "  ")
	fmt.Println(string(out))
}
