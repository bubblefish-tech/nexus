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

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/registry"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
	"github.com/bubblefish-tech/nexus/internal/actions"
	"github.com/bubblefish-tech/nexus/internal/approvals"
	"github.com/bubblefish-tech/nexus/internal/grants"
	"github.com/bubblefish-tech/nexus/internal/mcp"
	"github.com/bubblefish-tech/nexus/internal/policy"
	"github.com/bubblefish-tech/nexus/internal/tasks"
)

// ---------------------------------------------------------------------------
// Test fixture and adapter
// ---------------------------------------------------------------------------

type controlFixture struct {
	reg        *registry.Store
	grantSt    *grants.Store
	approvalSt *approvals.Store
	taskSt     *tasks.Store
	actionSt   *actions.Store
}

func newControlFixture(t *testing.T) *controlFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.db")
	reg, err := registry.NewStore(path)
	if err != nil {
		t.Fatalf("newControlFixture: registry.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })
	db := reg.DB()
	return &controlFixture{
		reg:        reg,
		grantSt:    grants.NewStore(db),
		approvalSt: approvals.NewStore(db),
		taskSt:     tasks.NewStore(db),
		actionSt:   actions.NewStore(db),
	}
}

func (f *controlFixture) registerAgent(t *testing.T, agentID string) {
	t.Helper()
	err := f.reg.Register(context.Background(), registry.RegisteredAgent{
		AgentID:     agentID,
		Name:        agentID,
		DisplayName: agentID,
		AgentCard: a2a.AgentCard{
			Name:            agentID,
			URL:             "http://localhost",
			ProtocolVersion: "0.1",
		},
		TransportConfig: transport.TransportConfig{Kind: "http", URL: "http://localhost"},
		Status:          registry.StatusActive,
	})
	if err != nil {
		t.Fatalf("registerAgent %q: %v", agentID, err)
	}
}

func (f *controlFixture) createGrant(t *testing.T, agentID, capability string) string {
	t.Helper()
	g, err := f.grantSt.Create(context.Background(), grants.Grant{
		AgentID:    agentID,
		Capability: capability,
		GrantedBy:  "test",
	})
	if err != nil {
		t.Fatalf("createGrant(%q, %q): %v", agentID, capability, err)
	}
	return g.GrantID
}

func (f *controlFixture) newAdapter(requireApproval []string) mcp.ControlPlaneProvider {
	eng := policy.NewEngine(
		f.reg, f.grantSt, f.approvalSt, f.actionSt,
		policy.EngineConfig{RequireApproval: requireApproval},
		nil,
	)
	return &testControlAdapter{
		engine:    eng,
		grants:    f.grantSt,
		approvals: f.approvalSt,
		tasks:     f.taskSt,
		actions:   f.actionSt,
	}
}

// testControlAdapter is a test-local implementation of mcp.ControlPlaneProvider
// that wraps the real stores and policy engine without importing the daemon.
type testControlAdapter struct {
	engine    *policy.Engine
	grants    *grants.Store
	approvals *approvals.Store
	tasks     *tasks.Store
	actions   *actions.Store
}

func (a *testControlAdapter) EvaluatePolicy(ctx context.Context, agentID, capability string, action json.RawMessage) mcp.ControlDecision {
	d := a.engine.Evaluate(ctx, agentID, capability, action)
	return mcp.ControlDecision{Allowed: d.Allowed, Reason: d.Reason, GrantID: d.GrantID, ApprovalID: d.ApprovalID}
}

func (a *testControlAdapter) ListGrants(ctx context.Context, agentID string) ([]mcp.GrantInfo, error) {
	gs, err := a.grants.List(ctx, grants.ListFilter{AgentID: agentID, Limit: 100})
	if err != nil {
		return nil, err
	}
	out := make([]mcp.GrantInfo, len(gs))
	for i, g := range gs {
		out[i] = mcp.GrantInfo{GrantID: g.GrantID, AgentID: g.AgentID, Capability: g.Capability, Scope: g.Scope, GrantedBy: g.GrantedBy, GrantedAt: g.GrantedAt, ExpiresAt: g.ExpiresAt}
	}
	return out, nil
}

func (a *testControlAdapter) RequestApproval(ctx context.Context, agentID, capability string, action json.RawMessage) (mcp.ApprovalInfo, error) {
	r, err := a.approvals.Create(ctx, approvals.Request{AgentID: agentID, Capability: capability, Action: action})
	if err != nil {
		return mcp.ApprovalInfo{}, err
	}
	return testApprovalToInfo(r), nil
}

func (a *testControlAdapter) GetApproval(ctx context.Context, requestID string) (mcp.ApprovalInfo, error) {
	r, err := a.approvals.Get(ctx, requestID)
	if err != nil {
		return mcp.ApprovalInfo{}, err
	}
	return testApprovalToInfo(*r), nil
}

func (a *testControlAdapter) CreateTask(ctx context.Context, agentID, capability string, input json.RawMessage) (mcp.TaskInfo, error) {
	t, err := a.tasks.Create(ctx, tasks.Task{AgentID: agentID, Capability: capability, Input: input})
	if err != nil {
		return mcp.TaskInfo{}, err
	}
	return mcp.TaskInfo{TaskID: t.TaskID, AgentID: t.AgentID, State: t.State, Capability: t.Capability, Input: t.Input, CreatedAt: t.CreatedAt}, nil
}

func (a *testControlAdapter) GetTask(ctx context.Context, taskID string) (mcp.TaskInfo, error) {
	t, err := a.tasks.Get(ctx, taskID)
	if err != nil {
		return mcp.TaskInfo{}, err
	}
	evts, err := a.tasks.ListEvents(ctx, taskID)
	if err != nil {
		return mcp.TaskInfo{}, err
	}
	info := mcp.TaskInfo{TaskID: t.TaskID, AgentID: t.AgentID, State: t.State, Capability: t.Capability, Input: t.Input, Output: t.Output, CreatedAt: t.CreatedAt}
	for _, e := range evts {
		info.Events = append(info.Events, mcp.TaskEventInfo{EventID: e.EventID, EventType: e.EventType, Payload: e.Payload, CreatedAt: e.CreatedAt})
	}
	return info, nil
}

func (a *testControlAdapter) QueryActionLog(ctx context.Context, agentID string, limit int) ([]mcp.ActionInfo, error) {
	if limit <= 0 {
		limit = 50
	}
	acts, err := a.actions.Query(ctx, actions.QueryFilter{AgentID: agentID, Limit: limit})
	if err != nil {
		return nil, err
	}
	out := make([]mcp.ActionInfo, len(acts))
	for i, act := range acts {
		out[i] = mcp.ActionInfo{ActionID: act.ActionID, AgentID: act.AgentID, Capability: act.Capability, PolicyDecision: act.PolicyDecision, PolicyReason: act.PolicyReason, GrantID: act.GrantID, ExecutedAt: act.ExecutedAt}
	}
	return out, nil
}

func testApprovalToInfo(r approvals.Request) mcp.ApprovalInfo {
	return mcp.ApprovalInfo{
		RequestID: r.RequestID, AgentID: r.AgentID, Capability: r.Capability,
		Action: r.Action, Status: r.Status, RequestedAt: r.RequestedAt,
		DecidedAt: r.DecidedAt, DecidedBy: r.DecidedBy, Decision: r.Decision, Reason: r.Reason,
	}
}

// ---------------------------------------------------------------------------
// HTTP helper for tool calls with X-Agent-ID
// ---------------------------------------------------------------------------

func rpcCallAgent(t *testing.T, client *http.Client, url, key, agentID, method string, params interface{}) map[string]interface{} {
	t.Helper()

	var rawParams json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("rpcCallAgent: marshal params: %v", err)
		}
		rawParams = b
	}
	reqBody := map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": method}
	if rawParams != nil {
		reqBody["params"] = rawParams
	}
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("rpcCallAgent: new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("rpcCallAgent: do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	b, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("rpcCallAgent: unmarshal: %v\nbody: %s", err, b)
	}
	return result
}

// toolCall issues a tools/call RPC and returns the parsed result/error maps.
func toolCall(t *testing.T, client *http.Client, url, key, agentID, toolName string, args interface{}) map[string]interface{} {
	t.Helper()
	return rpcCallAgent(t, client, url, key, agentID, "tools/call", map[string]interface{}{
		"name":      toolName,
		"arguments": args,
	})
}

// extractToolResult extracts the first content block text from a tools/call
// result and JSON-decodes it into a map.
func extractToolResult(t *testing.T, resp map[string]interface{}) (map[string]interface{}, bool) {
	t.Helper()
	res, ok := resp["result"].(map[string]interface{})
	if !ok {
		return nil, false
	}
	content, ok := res["content"].([]interface{})
	if !ok || len(content) == 0 {
		return nil, false
	}
	block, ok := content[0].(map[string]interface{})
	if !ok {
		return nil, false
	}
	text, ok := block["text"].(string)
	if !ok {
		return nil, false
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, false
	}
	return out, true
}

func isToolError(resp map[string]interface{}) bool {
	res, ok := resp["result"].(map[string]interface{})
	if !ok {
		return false
	}
	v, _ := res["isError"].(bool)
	return v
}

// startControlServer starts an MCP test server wired with the given provider.
func startControlServer(t *testing.T, provider mcp.ControlPlaneProvider) (*mcp.Server, string, func()) {
	t.Helper()
	srv, url, stop := startServer(t, &mcp.TestPipeline{}, "bfn_mcp_testkey")
	srv.SetControlPlane(provider)
	return srv, url, stop
}

const testKey = "bfn_mcp_testkey"
const testAgent = "agent-alpha"

// ---------------------------------------------------------------------------
// Tests: control plane disabled (nil provider)
// ---------------------------------------------------------------------------

func TestControlNotEnabled(t *testing.T) {
	_, url, stop := startServer(t, &mcp.TestPipeline{}, testKey)
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	tools := []string{
		"nexus_grant_list",
		"nexus_approval_request",
		"nexus_approval_status",
		"nexus_task_create",
		"nexus_task_status",
		"nexus_action_log",
	}
	for _, toolName := range tools {
		t.Run(toolName, func(t *testing.T) {
			resp := toolCall(t, client, url, testKey, testAgent, toolName, map[string]interface{}{})
			if !isToolError(resp) {
				t.Fatalf("%s: expected isError=true when control plane not enabled, got: %v", toolName, resp)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests: missing X-Agent-ID
// ---------------------------------------------------------------------------

func TestControlMissingAgentID(t *testing.T) {
	f := newControlFixture(t)
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	// Call without X-Agent-ID (pass empty string so helper omits header).
	resp := toolCall(t, client, url, testKey, "", "nexus_grant_list", map[string]interface{}{})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true when X-Agent-ID missing, got: %v", resp)
	}
}

// ---------------------------------------------------------------------------
// Tests: nexus_grant_list
// ---------------------------------------------------------------------------

func TestNexusGrantList_PolicyDenied(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent) // active but no grant for nexus_grant_list
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_grant_list", map[string]interface{}{})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true (no grant), got: %v", resp)
	}
}

func TestNexusGrantList_HappyPath(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_grant_list")
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_grant_list", map[string]interface{}{})
	if isToolError(resp) {
		t.Fatalf("unexpected isError: %v", resp)
	}
	data, ok := extractToolResult(t, resp)
	if !ok {
		t.Fatalf("could not parse tool result: %v", resp)
	}
	if _, ok := data["grants"]; !ok {
		t.Fatalf("result missing 'grants' key: %v", data)
	}
}

// ---------------------------------------------------------------------------
// Tests: nexus_approval_request
// ---------------------------------------------------------------------------

func TestNexusApprovalRequest_MissingCapability(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_approval_request")
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_approval_request",
		map[string]interface{}{"action": map[string]interface{}{"k": "v"}})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true for missing capability, got: %v", resp)
	}
}

func TestNexusApprovalRequest_MissingAction(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_approval_request")
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_approval_request",
		map[string]interface{}{"capability": "do_thing"})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true for missing action, got: %v", resp)
	}
}

func TestNexusApprovalRequest_PolicyDenied(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent) // no grant for nexus_approval_request
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_approval_request",
		map[string]interface{}{"capability": "do_thing", "action": map[string]interface{}{"k": "v"}})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true (no grant for nexus_approval_request), got: %v", resp)
	}
}

func TestNexusApprovalRequest_HappyPath(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_approval_request")
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_approval_request",
		map[string]interface{}{"capability": "do_thing", "action": map[string]interface{}{"op": "write"}})
	if isToolError(resp) {
		t.Fatalf("unexpected isError: %v", resp)
	}
	data, ok := extractToolResult(t, resp)
	if !ok {
		t.Fatalf("could not parse tool result: %v", resp)
	}
	if data["request_id"] == nil {
		t.Fatalf("result missing 'request_id': %v", data)
	}
	if data["status"] != "pending" {
		t.Fatalf("expected status=pending, got: %v", data["status"])
	}
}

// ---------------------------------------------------------------------------
// Tests: nexus_approval_status
// ---------------------------------------------------------------------------

func TestNexusApprovalStatus_MissingRequestID(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_approval_status")
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_approval_status", map[string]interface{}{})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true for missing request_id, got: %v", resp)
	}
}

func TestNexusApprovalStatus_PolicyDenied(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent) // no grant for nexus_approval_status
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_approval_status",
		map[string]interface{}{"request_id": "apr_fake"})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true (no grant), got: %v", resp)
	}
}

func TestNexusApprovalStatus_HappyPath(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_approval_status")
	f.createGrant(t, testAgent, "nexus_approval_request")

	// Create an approval request directly so we have a real request_id.
	req, err := f.approvalSt.Create(context.Background(), approvals.Request{
		AgentID:    testAgent,
		Capability: "do_thing",
		Action:     json.RawMessage(`{"op":"read"}`),
	})
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}

	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_approval_status",
		map[string]interface{}{"request_id": req.RequestID})
	if isToolError(resp) {
		t.Fatalf("unexpected isError: %v", resp)
	}
	data, ok := extractToolResult(t, resp)
	if !ok {
		t.Fatalf("could not parse tool result: %v", resp)
	}
	if data["request_id"] != req.RequestID {
		t.Fatalf("expected request_id=%q, got: %v", req.RequestID, data["request_id"])
	}
}

// ---------------------------------------------------------------------------
// Tests: nexus_task_create
// ---------------------------------------------------------------------------

func TestNexusTaskCreate_MissingCapability(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_task_create",
		map[string]interface{}{"input": map[string]interface{}{}})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true for missing capability, got: %v", resp)
	}
}

func TestNexusTaskCreate_PolicyDenied(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent) // no grant for "run_script"
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_task_create",
		map[string]interface{}{"capability": "run_script"})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true (no grant for run_script), got: %v", resp)
	}
}

func TestNexusTaskCreate_HappyPath(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "run_script")
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_task_create",
		map[string]interface{}{"capability": "run_script", "input": map[string]interface{}{"cmd": "ls"}})
	if isToolError(resp) {
		t.Fatalf("unexpected isError: %v", resp)
	}
	data, ok := extractToolResult(t, resp)
	if !ok {
		t.Fatalf("could not parse tool result: %v", resp)
	}
	if data["task_id"] == nil {
		t.Fatalf("result missing 'task_id': %v", data)
	}
	if data["state"] != "submitted" {
		t.Fatalf("expected state=submitted, got: %v", data["state"])
	}
}

// ---------------------------------------------------------------------------
// Tests: nexus_task_status
// ---------------------------------------------------------------------------

func TestNexusTaskStatus_MissingTaskID(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_task_status")
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_task_status", map[string]interface{}{})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true for missing task_id, got: %v", resp)
	}
}

func TestNexusTaskStatus_PolicyDenied(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent) // no grant for nexus_task_status
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_task_status",
		map[string]interface{}{"task_id": "tsk_fake"})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true (no grant), got: %v", resp)
	}
}

func TestNexusTaskStatus_HappyPath(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_task_status")
	f.createGrant(t, testAgent, "run_script")

	task, err := f.taskSt.Create(context.Background(), tasks.Task{
		AgentID:    testAgent,
		Capability: "run_script",
		Input:      json.RawMessage(`{"cmd":"ls"}`),
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_task_status",
		map[string]interface{}{"task_id": task.TaskID})
	if isToolError(resp) {
		t.Fatalf("unexpected isError: %v", resp)
	}
	data, ok := extractToolResult(t, resp)
	if !ok {
		t.Fatalf("could not parse tool result: %v", resp)
	}
	if data["task_id"] != task.TaskID {
		t.Fatalf("expected task_id=%q, got: %v", task.TaskID, data["task_id"])
	}
}

// ---------------------------------------------------------------------------
// Tests: nexus_action_log
// ---------------------------------------------------------------------------

func TestNexusActionLog_PolicyDenied(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent) // no grant for nexus_action_log
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_action_log", map[string]interface{}{})
	if !isToolError(resp) {
		t.Fatalf("expected isError=true (no grant), got: %v", resp)
	}
}

func TestNexusActionLog_HappyPath(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_action_log")
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_action_log", map[string]interface{}{})
	if isToolError(resp) {
		t.Fatalf("unexpected isError: %v", resp)
	}
	data, ok := extractToolResult(t, resp)
	if !ok {
		t.Fatalf("could not parse tool result: %v", resp)
	}
	if _, ok := data["entries"]; !ok {
		t.Fatalf("result missing 'entries' key: %v", data)
	}
}

func TestNexusActionLog_WithLimit(t *testing.T) {
	f := newControlFixture(t)
	f.registerAgent(t, testAgent)
	f.createGrant(t, testAgent, "nexus_action_log")

	// Seed 5 action log entries.
	for i := 0; i < 5; i++ {
		_, err := f.actionSt.Record(context.Background(), actions.Action{
			AgentID:        testAgent,
			Capability:     "seed_cap",
			PolicyDecision: actions.DecisionAllow,
		})
		if err != nil {
			t.Fatalf("seed action: %v", err)
		}
	}

	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := toolCall(t, client, url, testKey, testAgent, "nexus_action_log",
		map[string]interface{}{"limit": 2})
	if isToolError(resp) {
		t.Fatalf("unexpected isError: %v", resp)
	}
	data, ok := extractToolResult(t, resp)
	if !ok {
		t.Fatalf("could not parse tool result: %v", resp)
	}
	entries, _ := data["entries"].([]interface{})
	if len(entries) > 2 {
		t.Fatalf("expected at most 2 entries with limit=2, got %d", len(entries))
	}
}

// ---------------------------------------------------------------------------
// Tests: tools/list includes control tools when provider is set
// ---------------------------------------------------------------------------

func TestControlToolsInToolsList(t *testing.T) {
	f := newControlFixture(t)
	_, url, stop := startControlServer(t, f.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := rpcCallAgent(t, client, url, testKey, "", "tools/list", nil)
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("tools/list: no result: %v", resp)
	}
	toolsRaw, _ := result["tools"].([]interface{})
	toolNames := make(map[string]bool)
	for _, v := range toolsRaw {
		if m, ok := v.(map[string]interface{}); ok {
			if n, ok := m["name"].(string); ok {
				toolNames[n] = true
			}
		}
	}
	want := []string{
		"nexus_grant_list", "nexus_approval_request", "nexus_approval_status",
		"nexus_task_create", "nexus_task_status", "nexus_action_log",
	}
	for _, name := range want {
		if !toolNames[name] {
			t.Errorf("tools/list missing %q; found: %v", name, toolNames)
		}
	}
}

// ---------------------------------------------------------------------------
// E2E test: agent requests approval → admin approves → agent retries → allowed
// ---------------------------------------------------------------------------

// TestControlE2E_ApprovalFlow verifies the complete governed-task lifecycle:
//
//  1. Agent lacks approval for "governed_action" → nexus_task_create denied
//  2. Agent requests approval via nexus_approval_request
//  3. Admin approves directly via approvals.Store.Decide (REST handler path
//     is tested in handlers_control_test.go)
//  4. Agent retries nexus_task_create → allowed, task created
func TestControlE2E_ApprovalFlow(t *testing.T) {
	const govCap = "governed_action"

	f := newControlFixture(t)
	f.registerAgent(t, testAgent)

	// Agent needs a grant for "governed_action" and for "nexus_approval_request".
	f.createGrant(t, testAgent, govCap)
	f.createGrant(t, testAgent, "nexus_approval_request")

	// Engine requires approval for "governed_action".
	_, url, stop := startControlServer(t, f.newAdapter([]string{govCap}))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	// Step 1: nexus_task_create for governed_action → DENIED (approval required).
	resp1 := toolCall(t, client, url, testKey, testAgent, "nexus_task_create",
		map[string]interface{}{"capability": govCap, "input": map[string]interface{}{"op": "write"}})
	if !isToolError(resp1) {
		t.Fatalf("step 1: expected policy denial, got: %v", resp1)
	}

	// Step 2: nexus_approval_request for governed_action → creates pending request.
	resp2 := toolCall(t, client, url, testKey, testAgent, "nexus_approval_request",
		map[string]interface{}{
			"capability": govCap,
			"action":     map[string]interface{}{"op": "write"},
		})
	if isToolError(resp2) {
		t.Fatalf("step 2: unexpected error from nexus_approval_request: %v", resp2)
	}
	apData, ok := extractToolResult(t, resp2)
	if !ok {
		t.Fatalf("step 2: could not parse approval result: %v", resp2)
	}
	requestID, _ := apData["request_id"].(string)
	if requestID == "" {
		t.Fatalf("step 2: no request_id in result: %v", apData)
	}
	if apData["status"] != "pending" {
		t.Fatalf("step 2: expected status=pending, got: %v", apData["status"])
	}

	// Step 3: admin approves via the store directly (REST path covered by MT.2 tests).
	if err := f.approvalSt.Decide(context.Background(), requestID, approvals.DecideInput{
		Decision:  approvals.DecisionApprove,
		DecidedBy: "test-admin",
	}); err != nil {
		t.Fatalf("step 3: decide: %v", err)
	}

	// Step 4: agent retries nexus_task_create → ALLOWED, task created.
	resp4 := toolCall(t, client, url, testKey, testAgent, "nexus_task_create",
		map[string]interface{}{"capability": govCap, "input": map[string]interface{}{"op": "write"}})
	if isToolError(resp4) {
		t.Fatalf("step 4: expected success after approval, got: %v", resp4)
	}
	taskData, ok := extractToolResult(t, resp4)
	if !ok {
		t.Fatalf("step 4: could not parse task result: %v", resp4)
	}
	if taskData["task_id"] == nil {
		t.Fatalf("step 4: no task_id in result: %v", taskData)
	}
	if taskData["state"] != "submitted" {
		t.Fatalf("step 4: expected state=submitted, got: %v", taskData["state"])
	}
}

// TestApprovalStatusIDOR verifies that agent-B cannot read agent-A's approval.
func TestApprovalStatusIDOR(t *testing.T) {
	fix := newControlFixture(t)
	fix.registerAgent(t, "agent-a")
	fix.registerAgent(t, "agent-b")
	fix.createGrant(t, "agent-a", "nexus_approval_status")
	fix.createGrant(t, "agent-b", "nexus_approval_status")

	req, err := fix.approvalSt.Create(context.Background(), approvals.Request{
		AgentID:    "agent-a",
		Capability: "nexus_delete",
		Action:     json.RawMessage(`{"test":true}`),
	})
	if err != nil {
		t.Fatalf("create approval: %v", err)
	}

	_, url, stop := startControlServer(t, fix.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := rpcCallAgent(t, client, url, testKey, "agent-b", "tools/call", map[string]interface{}{
		"name":      "nexus_approval_status",
		"arguments": map[string]string{"request_id": req.RequestID},
	})

	text := extractToolText(t, resp)
	if text != "" && !stringContains(text, "not found") {
		t.Errorf("agent-b should not see agent-a's approval, got: %s", text)
	}
}

// TestTaskStatusIDOR verifies that agent-B cannot read agent-A's task.
func TestTaskStatusIDOR(t *testing.T) {
	fix := newControlFixture(t)
	fix.registerAgent(t, "agent-a")
	fix.registerAgent(t, "agent-b")
	fix.createGrant(t, "agent-a", "nexus_task_status")
	fix.createGrant(t, "agent-b", "nexus_task_status")
	fix.createGrant(t, "agent-a", "nexus_write")

	task, err := fix.taskSt.Create(context.Background(), tasks.Task{
		AgentID:    "agent-a",
		Capability: "nexus_write",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, url, stop := startControlServer(t, fix.newAdapter(nil))
	defer stop()
	client := &http.Client{Timeout: 5 * time.Second}

	resp := rpcCallAgent(t, client, url, testKey, "agent-b", "tools/call", map[string]interface{}{
		"name":      "nexus_task_status",
		"arguments": map[string]string{"task_id": task.TaskID},
	})

	text := extractToolText(t, resp)
	if text != "" && !stringContains(text, "not found") {
		t.Errorf("agent-b should not see agent-a's task, got: %s", text)
	}
}

func extractToolText(t *testing.T, resp map[string]interface{}) string {
	t.Helper()
	result, ok := resp["result"].(map[string]interface{})
	if !ok {
		return ""
	}
	content, ok := result["content"].([]interface{})
	if !ok || len(content) == 0 {
		return ""
	}
	first, ok := content[0].(map[string]interface{})
	if !ok {
		return ""
	}
	text, _ := first["text"].(string)
	return text
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
