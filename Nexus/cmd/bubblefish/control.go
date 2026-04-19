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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/BubbleFish-Nexus/internal/config"
)

// ---------------------------------------------------------------------------
// controlClient
// ---------------------------------------------------------------------------

// controlClient wraps authenticated HTTP calls to the control-plane REST API.
// All state is in struct fields; no package-level variables.
type controlClient struct {
	http    *http.Client
	baseURL string
	token   string
	out     io.Writer
	errOut  io.Writer
}

func newControlClientFromConfig(cfg *config.Config, out, errOut io.Writer) *controlClient {
	return &controlClient{
		http:    &http.Client{Timeout: 10 * time.Second},
		baseURL: fmt.Sprintf("http://%s:%d", cfg.Daemon.Bind, cfg.Daemon.Port),
		token:   cfg.Daemon.AdminToken,
		out:     out,
		errOut:  errOut,
	}
}

func (c *controlClient) get(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return c.http.Do(req)
}

func (c *controlClient) post(path string, body interface{}) (*http.Response, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	return c.http.Do(req)
}

func (c *controlClient) delete(path string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// readJSON decodes the JSON response body into v, closing the body after.
func readJSON(resp *http.Response, v interface{}) error {
	defer func() { _ = resp.Body.Close() }()
	return json.NewDecoder(resp.Body).Decode(v)
}

// loadControlClient loads config and returns a controlClient.
func loadControlClient(out, errOut io.Writer) (*controlClient, error) {
	logger := slog.New(slog.NewTextHandler(errOut, &slog.HandlerOptions{Level: slog.LevelWarn}))
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(configDir, logger)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return newControlClientFromConfig(cfg, out, errOut), nil
}

// parseFlags is a minimal flag parser for commands with only string/bool flags.
func parseFlags(args []string, known map[string]*string, bools map[string]*bool) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return fmt.Errorf("unexpected argument %q", arg)
		}
		key := strings.TrimPrefix(arg, "--")
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			name, val := key[:eq], key[eq+1:]
			if p, ok := known[name]; ok {
				*p = val
				continue
			}
			if p, ok := bools[name]; ok {
				*p = val == "true" || val == "1"
				continue
			}
			return fmt.Errorf("unknown flag --%s", name)
		}
		// Next-token form.
		if p, ok := bools[key]; ok {
			*p = true
			continue
		}
		if p, ok := known[key]; ok {
			i++
			if i >= len(args) {
				return fmt.Errorf("--%s requires a value", key)
			}
			*p = args[i]
			continue
		}
		return fmt.Errorf("unknown flag --%s", key)
	}
	return nil
}

// msToTime formats a Unix millisecond timestamp as RFC3339.
func msToTime(ms int64) string {
	if ms == 0 {
		return "—"
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// ---------------------------------------------------------------------------
// grant commands
// ---------------------------------------------------------------------------

// runGrant dispatches `bubblefish grant <subcommand>`.
func runGrant(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish grant <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  create  create a capability grant")
		fmt.Fprintln(os.Stderr, "  list    list capability grants")
		fmt.Fprintln(os.Stderr, "  revoke  revoke a capability grant")
		os.Exit(1)
	}
	cl, err := loadControlClient(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish grant: %v\n", err)
		os.Exit(1)
	}
	switch args[0] {
	case "create":
		if err := doGrantCreate(cl, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish grant create: %v\n", err)
			os.Exit(1)
		}
	case "list":
		if err := doGrantList(cl, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish grant list: %v\n", err)
			os.Exit(1)
		}
	case "revoke":
		if err := doGrantRevoke(cl, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish grant revoke: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "bubblefish grant: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func doGrantCreate(cl *controlClient, args []string) error {
	var agent, capability, scope, expires, grantedBy string
	var jsonOut bool
	if err := parseFlags(args,
		map[string]*string{
			"agent": &agent, "capability": &capability,
			"scope": &scope, "expires": &expires, "granted-by": &grantedBy,
		},
		map[string]*bool{"json": &jsonOut},
	); err != nil {
		return fmt.Errorf("%v\nusage: bubblefish grant create --agent <id> --capability <cap> [--scope <json>] [--expires <duration>] [--granted-by <id>] [--json]", err)
	}
	if agent == "" || capability == "" {
		return fmt.Errorf("--agent and --capability are required")
	}
	body := map[string]interface{}{
		"agent_id":   agent,
		"capability": capability,
	}
	if scope != "" {
		body["scope"] = json.RawMessage(scope)
	}
	if grantedBy != "" {
		body["granted_by"] = grantedBy
	} else {
		body["granted_by"] = "cli"
	}
	if expires != "" {
		dur, err := time.ParseDuration(expires)
		if err != nil {
			return fmt.Errorf("invalid --expires duration %q: %w", expires, err)
		}
		body["expires_at_ms"] = time.Now().Add(dur).UnixMilli()
	}
	resp, err := cl.post("/api/control/grants", body)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	var result map[string]interface{}
	if err := readJSON(resp, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if jsonOut {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cl.out, string(b))
		return nil
	}
	grant, _ := result["grant"].(map[string]interface{})
	if grant == nil {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cl.out, string(b))
		return nil
	}
	fmt.Fprintf(cl.out, "grant created\n")
	fmt.Fprintf(cl.out, "  grant_id:   %v\n", grant["grant_id"])
	fmt.Fprintf(cl.out, "  agent_id:   %v\n", grant["agent_id"])
	fmt.Fprintf(cl.out, "  capability: %v\n", grant["capability"])
	fmt.Fprintf(cl.out, "  granted_by: %v\n", grant["granted_by"])
	return nil
}

func doGrantList(cl *controlClient, args []string) error {
	var agentID string
	var jsonOut bool
	if err := parseFlags(args,
		map[string]*string{"agent": &agentID},
		map[string]*bool{"json": &jsonOut},
	); err != nil {
		return fmt.Errorf("%v\nusage: bubblefish grant list [--agent <id>] [--json]", err)
	}
	path := "/api/control/grants"
	if agentID != "" {
		path += "?agent_id=" + url.QueryEscape(agentID)
	}
	resp, err := cl.get(path)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	var result struct {
		Grants []map[string]interface{} `json:"grants"`
	}
	if err := readJSON(resp, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if jsonOut {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cl.out, string(b))
		return nil
	}
	if len(result.Grants) == 0 {
		fmt.Fprintln(cl.out, "No grants.")
		return nil
	}
	tw := tabwriter.NewWriter(cl.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "GRANT ID\tAGENT ID\tCAPABILITY\tGRANTED BY\tEXPIRES\tSTATUS")
	for _, g := range result.Grants {
		expires := "never"
		if v, ok := g["expires_at_ms"]; ok && v != nil {
			if ms, ok := v.(float64); ok && ms > 0 {
				expires = msToTime(int64(ms))
			}
		}
		status := "active"
		if v, ok := g["revoked_at_ms"]; ok && v != nil {
			if ms, ok := v.(float64); ok && ms > 0 {
				status = "revoked"
			}
		}
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%s\t%s\n",
			g["grant_id"], g["agent_id"], g["capability"], g["granted_by"],
			expires, status)
	}
	_ = tw.Flush()
	return nil
}

func doGrantRevoke(cl *controlClient, args []string) error {
	var id, reason string
	var jsonOut bool
	if err := parseFlags(args,
		map[string]*string{"id": &id, "reason": &reason},
		map[string]*bool{"json": &jsonOut},
	); err != nil {
		return fmt.Errorf("%v\nusage: bubblefish grant revoke --id <grant_id> [--reason <text>] [--json]", err)
	}
	if id == "" {
		return fmt.Errorf("--id is required")
	}
	var body interface{}
	if reason != "" {
		body = map[string]string{"reason": reason}
	}
	resp, err := cl.delete("/api/control/grants/"+id, body)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	var result map[string]interface{}
	if err := readJSON(resp, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if jsonOut {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cl.out, string(b))
		return nil
	}
	fmt.Fprintf(cl.out, "grant %s revoked\n", id)
	return nil
}

// ---------------------------------------------------------------------------
// approval commands
// ---------------------------------------------------------------------------

// runApproval dispatches `bubblefish approval <subcommand>`.
func runApproval(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish approval <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  list    list approval requests")
		fmt.Fprintln(os.Stderr, "  decide  approve or deny a request")
		os.Exit(1)
	}
	cl, err := loadControlClient(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish approval: %v\n", err)
		os.Exit(1)
	}
	switch args[0] {
	case "list":
		if err := doApprovalList(cl, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish approval list: %v\n", err)
			os.Exit(1)
		}
	case "decide":
		if err := doApprovalDecide(cl, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish approval decide: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "bubblefish approval: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func doApprovalList(cl *controlClient, args []string) error {
	var status string
	var jsonOut bool
	if err := parseFlags(args,
		map[string]*string{"status": &status},
		map[string]*bool{"json": &jsonOut},
	); err != nil {
		return fmt.Errorf("%v\nusage: bubblefish approval list [--status pending|approved|denied] [--json]", err)
	}
	path := "/api/control/approvals"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	resp, err := cl.get(path)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	var result struct {
		Approvals []map[string]interface{} `json:"approvals"`
	}
	if err := readJSON(resp, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if jsonOut {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cl.out, string(b))
		return nil
	}
	if len(result.Approvals) == 0 {
		fmt.Fprintln(cl.out, "No approval requests.")
		return nil
	}
	tw := tabwriter.NewWriter(cl.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REQUEST ID\tAGENT ID\tCAPABILITY\tSTATUS\tREQUESTED AT\tDECIDED BY")
	for _, a := range result.Approvals {
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\t%v\n",
			a["request_id"], a["agent_id"], a["capability"],
			a["status"],
			msToTime(int64AsFloat(a["requested_at_ms"])),
			strOrDash(a["decided_by"]))
	}
	_ = tw.Flush()
	return nil
}

func doApprovalDecide(cl *controlClient, args []string) error {
	var id, decision, reason string
	var jsonOut bool
	if err := parseFlags(args,
		map[string]*string{"id": &id, "decision": &decision, "reason": &reason},
		map[string]*bool{"json": &jsonOut},
	); err != nil {
		return fmt.Errorf("%v\nusage: bubblefish approval decide --id <id> --decision approve|deny [--reason <text>] [--json]", err)
	}
	if id == "" || decision == "" {
		return fmt.Errorf("--id and --decision are required")
	}
	if decision != "approve" && decision != "deny" {
		return fmt.Errorf("--decision must be 'approve' or 'deny', got %q", decision)
	}
	body := map[string]string{"decision": decision}
	if reason != "" {
		body["reason"] = reason
	}
	resp, err := cl.post("/api/control/approvals/"+id, body)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	var result map[string]interface{}
	if err := readJSON(resp, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if jsonOut {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cl.out, string(b))
		return nil
	}
	fmt.Fprintf(cl.out, "approval %s: %sd\n", id, decision)
	return nil
}

// ---------------------------------------------------------------------------
// task commands
// ---------------------------------------------------------------------------

// runTask dispatches `bubblefish task <subcommand>`.
func runTask(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish task <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  list     list tasks")
		fmt.Fprintln(os.Stderr, "  inspect  show a task and its events")
		os.Exit(1)
	}
	cl, err := loadControlClient(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish task: %v\n", err)
		os.Exit(1)
	}
	switch args[0] {
	case "list":
		if err := doTaskList(cl, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish task list: %v\n", err)
			os.Exit(1)
		}
	case "inspect":
		if err := doTaskInspect(cl, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish task inspect: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "bubblefish task: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func doTaskList(cl *controlClient, args []string) error {
	var agentID, state string
	var jsonOut bool
	if err := parseFlags(args,
		map[string]*string{"agent": &agentID, "state": &state},
		map[string]*bool{"json": &jsonOut},
	); err != nil {
		return fmt.Errorf("%v\nusage: bubblefish task list [--agent <id>] [--state <state>] [--json]", err)
	}
	var params []string
	if agentID != "" {
		params = append(params, "agent_id="+url.QueryEscape(agentID))
	}
	if state != "" {
		params = append(params, "state="+url.QueryEscape(state))
	}
	path := "/api/control/tasks"
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}
	resp, err := cl.get(path)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	var result struct {
		Tasks []map[string]interface{} `json:"tasks"`
	}
	if err := readJSON(resp, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if jsonOut {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cl.out, string(b))
		return nil
	}
	if len(result.Tasks) == 0 {
		fmt.Fprintln(cl.out, "No tasks.")
		return nil
	}
	tw := tabwriter.NewWriter(cl.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TASK ID\tAGENT ID\tCAPABILITY\tSTATE\tCREATED AT")
	for _, t := range result.Tasks {
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\n",
			t["task_id"], t["agent_id"], t["capability"], t["state"],
			msToTime(int64AsFloat(t["created_at_ms"])))
	}
	_ = tw.Flush()
	return nil
}

func doTaskInspect(cl *controlClient, args []string) error {
	var id string
	var jsonOut bool
	if err := parseFlags(args,
		map[string]*string{"id": &id},
		map[string]*bool{"json": &jsonOut},
	); err != nil {
		return fmt.Errorf("%v\nusage: bubblefish task inspect --id <task_id> [--json]", err)
	}
	if id == "" {
		return fmt.Errorf("--id is required")
	}
	resp, err := cl.get("/api/control/tasks/" + id)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	var result map[string]interface{}
	if err := readJSON(resp, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if jsonOut {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cl.out, string(b))
		return nil
	}
	fmt.Fprintf(cl.out, "task_id:    %v\n", result["task_id"])
	fmt.Fprintf(cl.out, "agent_id:   %v\n", result["agent_id"])
	fmt.Fprintf(cl.out, "capability: %v\n", result["capability"])
	fmt.Fprintf(cl.out, "state:      %v\n", result["state"])
	fmt.Fprintf(cl.out, "created_at: %v\n", msToTime(int64AsFloat(result["created_at_ms"])))
	if events, ok := result["events"].([]interface{}); ok && len(events) > 0 {
		fmt.Fprintln(cl.out, "\nevents:")
		for _, ev := range events {
			if e, ok := ev.(map[string]interface{}); ok {
				fmt.Fprintf(cl.out, "  [%v] %v\n",
					msToTime(int64AsFloat(e["created_at_ms"])), e["event_type"])
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// action commands
// ---------------------------------------------------------------------------

// runAction dispatches `bubblefish action <subcommand>`.
func runAction(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bubblefish action <subcommand>")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  log  query the action log")
		os.Exit(1)
	}
	cl, err := loadControlClient(os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bubblefish action: %v\n", err)
		os.Exit(1)
	}
	switch args[0] {
	case "log":
		if err := doActionLog(cl, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "bubblefish action log: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "bubblefish action: unknown subcommand %q\n", args[0])
		os.Exit(1)
	}
}

func doActionLog(cl *controlClient, args []string) error {
	var agentID, capability, since string
	var jsonOut bool
	if err := parseFlags(args,
		map[string]*string{"agent": &agentID, "capability": &capability, "since": &since},
		map[string]*bool{"json": &jsonOut},
	); err != nil {
		return fmt.Errorf("%v\nusage: bubblefish action log [--agent <id>] [--capability <cap>] [--since <duration>] [--json]", err)
	}
	var params []string
	if agentID != "" {
		params = append(params, "agent_id="+url.QueryEscape(agentID))
	}
	if capability != "" {
		params = append(params, "capability="+url.QueryEscape(capability))
	}
	if since != "" {
		dur, err := time.ParseDuration(since)
		if err != nil {
			return fmt.Errorf("invalid --since duration %q: %w", since, err)
		}
		params = append(params, fmt.Sprintf("since_ms=%d", time.Now().Add(-dur).UnixMilli()))
	}
	path := "/api/control/actions"
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}
	resp, err := cl.get(path)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	var result struct {
		Actions []map[string]interface{} `json:"actions"`
	}
	if err := readJSON(resp, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if jsonOut {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Fprintln(cl.out, string(b))
		return nil
	}
	if len(result.Actions) == 0 {
		fmt.Fprintln(cl.out, "No actions.")
		return nil
	}
	tw := tabwriter.NewWriter(cl.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTION ID\tAGENT ID\tCAPABILITY\tDECISION\tREASON\tEXECUTED AT")
	for _, a := range result.Actions {
		fmt.Fprintf(tw, "%v\t%v\t%v\t%v\t%v\t%v\n",
			a["action_id"], a["agent_id"], a["capability"],
			a["policy_decision"], strOrDash(a["policy_reason"]),
			msToTime(int64AsFloat(a["executed_at_ms"])))
	}
	_ = tw.Flush()
	return nil
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

func strOrDash(v interface{}) string {
	if v == nil {
		return "—"
	}
	s, _ := v.(string)
	if s == "" {
		return "—"
	}
	return s
}

func int64AsFloat(v interface{}) int64 {
	if v == nil {
		return 0
	}
	f, _ := v.(float64)
	return int64(f)
}
