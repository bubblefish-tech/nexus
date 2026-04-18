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

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
)

// helper to dispatch a JSON-RPC request against the test server.
func dispatch(t *testing.T, srv *Server, method string, params interface{}) *jsonrpc.Response {
	t.Helper()
	req, err := jsonrpc.NewRequest(jsonrpc.StringID("test-1"), method, params)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return srv.Dispatch(context.Background(), req)
}

// helper to dispatch with a context.
func dispatchCtx(t *testing.T, srv *Server, ctx context.Context, method string, params interface{}) *jsonrpc.Response {
	t.Helper()
	req, err := jsonrpc.NewRequest(jsonrpc.StringID("test-1"), method, params)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return srv.Dispatch(ctx, req)
}

// adminCtx returns a context with admin=true.
func adminCtx() context.Context {
	return context.WithValue(context.Background(), CtxKeyAdmin, true)
}

// validMessage creates a valid Message for testing.
func validMessage() a2a.Message {
	return a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
}

// --- message/send tests ---

func TestMessageSend(t *testing.T) {
	tests := []struct {
		name        string
		params      interface{}
		setup       func(*TestServerDeps)
		wantErr     bool
		wantCode    int
		checkResult func(t *testing.T, result json.RawMessage)
	}{
		{
			name: "happy path echo skill",
			params: messageSendParams{
				Message: validMessage(),
				Skill:   "echo",
			},
			checkResult: func(t *testing.T, result json.RawMessage) {
				t.Helper()
				var task a2a.Task
				if err := json.Unmarshal(result, &task); err != nil {
					t.Fatalf("unmarshal task: %v", err)
				}
				if task.Status.State != a2a.TaskStateCompleted {
					t.Errorf("want state completed, got %s", task.Status.State)
				}
				if task.TaskID == "" {
					t.Error("task ID is empty")
				}
			},
		},
		{
			name: "skill not found",
			params: messageSendParams{
				Message: validMessage(),
				Skill:   "nonexistent",
			},
			wantErr:  true,
			wantCode: a2a.CodeSkillNotFound,
		},
		{
			name: "governance deny",
			params: messageSendParams{
				Message: validMessage(),
				Skill:   "echo",
			},
			setup: func(deps *TestServerDeps) {
				deps.Governance.mu.Lock()
				deps.Governance.DenyAll = true
				deps.Governance.DenyReason = "test deny"
				deps.Governance.mu.Unlock()
			},
			wantErr:  true,
			wantCode: a2a.CodePermissionDenied,
		},
		{
			name: "governance escalate",
			params: messageSendParams{
				Message: validMessage(),
				Skill:   "echo",
			},
			setup: func(deps *TestServerDeps) {
				deps.Governance.mu.Lock()
				deps.Governance.EscalateAll = true
				deps.Governance.EscalateReason = "needs approval"
				deps.Governance.mu.Unlock()
			},
			wantErr:  true,
			wantCode: a2a.CodeApprovalRequired,
		},
		{
			name: "executor error",
			params: messageSendParams{
				Message: validMessage(),
				Skill:   "echo",
			},
			setup: func(deps *TestServerDeps) {
				deps.SkillExecutor.RegisterSkill("echo", func(_ context.Context, _ *a2a.DataPart, _ []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
					return nil, nil, fmt.Errorf("boom")
				})
			},
			wantErr:  true,
			wantCode: a2a.CodeInternalError,
		},
		{
			name:     "missing skill param",
			params:   messageSendParams{Message: validMessage(), Skill: ""},
			wantErr:  true,
			wantCode: a2a.CodeInvalidParams,
		},
		{
			name:     "invalid params JSON",
			params:   "not valid json",
			wantErr:  true,
			wantCode: a2a.CodeInvalidParams,
		},
		{
			name: "invalid message missing parts",
			params: messageSendParams{
				Message: a2a.Message{Kind: "message", MessageID: a2a.NewMessageID(), Role: a2a.RoleUser},
				Skill:   "echo",
			},
			wantErr:  true,
			wantCode: a2a.CodeInvalidParams,
		},
		{
			name: "with explicit taskId",
			params: func() messageSendParams {
				tid := a2a.NewTaskID()
				return messageSendParams{
					Message: validMessage(),
					Skill:   "echo",
					TaskID:  &tid,
				}
			}(),
			checkResult: func(t *testing.T, result json.RawMessage) {
				t.Helper()
				var task a2a.Task
				if err := json.Unmarshal(result, &task); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if task.Status.State != a2a.TaskStateCompleted {
					t.Errorf("want completed, got %s", task.Status.State)
				}
			},
		},
		{
			name: "with data part input",
			params: messageSendParams{
				Message: a2a.NewMessage(a2a.RoleUser, a2a.NewDataPart(json.RawMessage(`{"key":"value"}`))),
				Skill:   "echo",
			},
			checkResult: func(t *testing.T, result json.RawMessage) {
				t.Helper()
				var task a2a.Task
				if err := json.Unmarshal(result, &task); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if len(task.Artifacts) == 0 {
					t.Error("expected at least one artifact")
				}
			},
		},
		{
			name: "destructive skill allowed",
			params: messageSendParams{
				Message: validMessage(),
				Skill:   "dangerous",
			},
			checkResult: func(t *testing.T, result json.RawMessage) {
				t.Helper()
				var task a2a.Task
				if err := json.Unmarshal(result, &task); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if task.Status.State != a2a.TaskStateCompleted {
					t.Errorf("want completed, got %s", task.Status.State)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := NewTestServer()
			if tt.setup != nil {
				tt.setup(deps)
			}

			resp := dispatch(t, deps.Server, "message/send", tt.params)

			if tt.wantErr {
				if resp.Error == nil {
					t.Fatal("expected error, got success")
				}
				if resp.Error.Code != tt.wantCode {
					t.Errorf("want error code %d, got %d: %s", tt.wantCode, resp.Error.Code, resp.Error.Message)
				}
				return
			}

			if resp.Error != nil {
				t.Fatalf("unexpected error: %d %s", resp.Error.Code, resp.Error.Message)
			}

			if tt.checkResult != nil {
				tt.checkResult(t, resp.Result)
			}
		})
	}
}

// --- message/stream tests ---

func TestMessageStream(t *testing.T) {
	tests := []struct {
		name     string
		params   interface{}
		wantErr  bool
		wantCode int
	}{
		{
			name: "happy path returns streaming flag",
			params: messageSendParams{
				Message: validMessage(),
				Skill:   "echo",
			},
		},
		{
			name: "skill not found",
			params: messageSendParams{
				Message: validMessage(),
				Skill:   "nope",
			},
			wantErr:  true,
			wantCode: a2a.CodeSkillNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := NewTestServer()
			resp := dispatch(t, deps.Server, "message/stream", tt.params)

			if tt.wantErr {
				if resp.Error == nil {
					t.Fatal("expected error")
				}
				if resp.Error.Code != tt.wantCode {
					t.Errorf("want code %d, got %d", tt.wantCode, resp.Error.Code)
				}
				return
			}

			if resp.Error != nil {
				t.Fatalf("unexpected error: %s", resp.Error.Message)
			}

			var result struct {
				Task      *a2a.Task `json:"task"`
				Streaming bool      `json:"streaming"`
			}
			if err := json.Unmarshal(resp.Result, &result); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !result.Streaming {
				t.Error("expected streaming=true")
			}
			if result.Task == nil {
				t.Error("expected task in result")
			}
		})
	}
}

// --- tasks/get tests ---

func TestTasksGet(t *testing.T) {
	tests := []struct {
		name     string
		taskID   string
		setup    func(*TestServerDeps) string
		wantErr  bool
		wantCode int
	}{
		{
			name: "found",
			setup: func(deps *TestServerDeps) string {
				task := a2a.NewTask()
				_ = deps.TaskStore.CreateTask(context.Background(), &task)
				return task.TaskID
			},
		},
		{
			name:     "not found",
			taskID:   "tsk_01NOTEXIST0000000000000000",
			wantErr:  true,
			wantCode: a2a.CodeTaskNotFound,
		},
		{
			name:     "empty taskId",
			taskID:   "",
			wantErr:  true,
			wantCode: a2a.CodeInvalidParams,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := NewTestServer()
			taskID := tt.taskID
			if tt.setup != nil {
				taskID = tt.setup(deps)
			}

			resp := dispatch(t, deps.Server, "tasks/get", taskIDParams{TaskID: taskID})

			if tt.wantErr {
				if resp.Error == nil {
					t.Fatal("expected error")
				}
				if resp.Error.Code != tt.wantCode {
					t.Errorf("want code %d, got %d", tt.wantCode, resp.Error.Code)
				}
				return
			}

			if resp.Error != nil {
				t.Fatalf("unexpected error: %s", resp.Error.Message)
			}

			var task a2a.Task
			if err := json.Unmarshal(resp.Result, &task); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if task.TaskID != taskID {
				t.Errorf("want taskID %s, got %s", taskID, task.TaskID)
			}
		})
	}
}

// --- tasks/cancel tests ---

func TestTasksCancel(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*TestServerDeps) string
		wantErr  bool
		wantCode int
	}{
		{
			name: "cancel active task",
			setup: func(deps *TestServerDeps) string {
				task := a2a.NewTask()
				task.Status = a2a.TaskStatus{State: a2a.TaskStateWorking, Timestamp: a2a.Now()}
				_ = deps.TaskStore.CreateTask(context.Background(), &task)
				return task.TaskID
			},
		},
		{
			name: "cancel submitted task",
			setup: func(deps *TestServerDeps) string {
				task := a2a.NewTask()
				_ = deps.TaskStore.CreateTask(context.Background(), &task)
				return task.TaskID
			},
		},
		{
			name: "cancel input-required task",
			setup: func(deps *TestServerDeps) string {
				task := a2a.NewTask()
				task.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Timestamp: a2a.Now()}
				_ = deps.TaskStore.CreateTask(context.Background(), &task)
				return task.TaskID
			},
		},
		{
			name: "terminal task not cancelable - completed",
			setup: func(deps *TestServerDeps) string {
				task := a2a.NewTask()
				task.Status = a2a.TaskStatus{State: a2a.TaskStateCompleted, Timestamp: a2a.Now()}
				_ = deps.TaskStore.CreateTask(context.Background(), &task)
				return task.TaskID
			},
			wantErr:  true,
			wantCode: a2a.CodeTaskNotCancelable,
		},
		{
			name: "terminal task not cancelable - failed",
			setup: func(deps *TestServerDeps) string {
				task := a2a.NewTask()
				task.Status = a2a.TaskStatus{State: a2a.TaskStateFailed, Timestamp: a2a.Now()}
				_ = deps.TaskStore.CreateTask(context.Background(), &task)
				return task.TaskID
			},
			wantErr:  true,
			wantCode: a2a.CodeTaskNotCancelable,
		},
		{
			name: "terminal task not cancelable - canceled",
			setup: func(deps *TestServerDeps) string {
				task := a2a.NewTask()
				task.Status = a2a.TaskStatus{State: a2a.TaskStateCanceled, Timestamp: a2a.Now()}
				_ = deps.TaskStore.CreateTask(context.Background(), &task)
				return task.TaskID
			},
			wantErr:  true,
			wantCode: a2a.CodeTaskNotCancelable,
		},
		{
			name: "task not found",
			setup: func(deps *TestServerDeps) string {
				return "tsk_01NOTEXIST0000000000000000"
			},
			wantErr:  true,
			wantCode: a2a.CodeTaskNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := NewTestServer()
			taskID := tt.setup(deps)

			resp := dispatch(t, deps.Server, "tasks/cancel", taskIDParams{TaskID: taskID})

			if tt.wantErr {
				if resp.Error == nil {
					t.Fatal("expected error")
				}
				if resp.Error.Code != tt.wantCode {
					t.Errorf("want code %d, got %d: %s", tt.wantCode, resp.Error.Code, resp.Error.Message)
				}
				return
			}

			if resp.Error != nil {
				t.Fatalf("unexpected error: %s", resp.Error.Message)
			}

			var task a2a.Task
			if err := json.Unmarshal(resp.Result, &task); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if task.Status.State != a2a.TaskStateCanceled {
				t.Errorf("want state canceled, got %s", task.Status.State)
			}
		})
	}
}

// --- tasks/resubscribe tests ---

func TestTasksResubscribe(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*TestServerDeps) string
		wantErr  bool
		wantCode int
	}{
		{
			name: "returns current state",
			setup: func(deps *TestServerDeps) string {
				task := a2a.NewTask()
				task.Status = a2a.TaskStatus{State: a2a.TaskStateWorking, Timestamp: a2a.Now()}
				_ = deps.TaskStore.CreateTask(context.Background(), &task)
				return task.TaskID
			},
		},
		{
			name: "not found",
			setup: func(deps *TestServerDeps) string {
				return "tsk_01NOTEXIST0000000000000000"
			},
			wantErr:  true,
			wantCode: a2a.CodeTaskNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := NewTestServer()
			taskID := tt.setup(deps)

			resp := dispatch(t, deps.Server, "tasks/resubscribe", taskIDParams{TaskID: taskID})

			if tt.wantErr {
				if resp.Error == nil {
					t.Fatal("expected error")
				}
				if resp.Error.Code != tt.wantCode {
					t.Errorf("want code %d, got %d", tt.wantCode, resp.Error.Code)
				}
				return
			}

			if resp.Error != nil {
				t.Fatalf("unexpected error: %s", resp.Error.Message)
			}
		})
	}
}

// --- tasks/pushNotificationConfig tests ---

func TestTasksPushNotificationConfigSet(t *testing.T) {
	deps := NewTestServer()
	task := a2a.NewTask()
	_ = deps.TaskStore.CreateTask(context.Background(), &task)

	resp := dispatch(t, deps.Server, "tasks/pushNotificationConfig/set",
		pushNotificationConfigSetParams{
			TaskID: task.TaskID,
			Config: PushNotificationConfig{URL: "https://example.com/push", Token: "tok"},
		})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestTasksPushNotificationConfigSetMissingURL(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "tasks/pushNotificationConfig/set",
		pushNotificationConfigSetParams{
			TaskID: "tsk_dummy",
			Config: PushNotificationConfig{},
		})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != a2a.CodeInvalidParams {
		t.Errorf("want code %d, got %d", a2a.CodeInvalidParams, resp.Error.Code)
	}
}

func TestTasksPushNotificationConfigGetNotFound(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "tasks/pushNotificationConfig/get",
		pushNotificationConfigGetParams{TaskID: "tsk_noconfig"})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != a2a.CodeTaskNotFound {
		t.Errorf("want code %d, got %d", a2a.CodeTaskNotFound, resp.Error.Code)
	}
}

func TestTasksPushNotificationConfigSetThenGet(t *testing.T) {
	deps := NewTestServer()
	task := a2a.NewTask()
	_ = deps.TaskStore.CreateTask(context.Background(), &task)

	setResp := dispatch(t, deps.Server, "tasks/pushNotificationConfig/set",
		pushNotificationConfigSetParams{
			TaskID: task.TaskID,
			Config: PushNotificationConfig{URL: "https://example.com/hook"},
		})
	if setResp.Error != nil {
		t.Fatalf("set failed: %s", setResp.Error.Message)
	}

	getResp := dispatch(t, deps.Server, "tasks/pushNotificationConfig/get",
		pushNotificationConfigGetParams{TaskID: task.TaskID})
	if getResp.Error != nil {
		t.Fatalf("get failed: %s", getResp.Error.Message)
	}

	var cfg PushNotificationConfig
	if err := json.Unmarshal(getResp.Result, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.URL != "https://example.com/hook" {
		t.Errorf("want URL https://example.com/hook, got %s", cfg.URL)
	}
}

func TestTasksPushNotificationConfigSetMissingTaskID(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "tasks/pushNotificationConfig/set",
		pushNotificationConfigSetParams{
			Config: PushNotificationConfig{URL: "https://example.com"},
		})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != a2a.CodeInvalidParams {
		t.Errorf("want code %d, got %d", a2a.CodeInvalidParams, resp.Error.Code)
	}
}

func TestTasksPushNotificationConfigGetMissingTaskID(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "tasks/pushNotificationConfig/get",
		pushNotificationConfigGetParams{})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != a2a.CodeInvalidParams {
		t.Errorf("want code %d, got %d", a2a.CodeInvalidParams, resp.Error.Code)
	}
}

// --- agent/card tests ---

func TestAgentCard(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "agent/card", nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var card a2a.AgentCard
	if err := json.Unmarshal(resp.Result, &card); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if card.Name != "test-agent" {
		t.Errorf("want name test-agent, got %s", card.Name)
	}
	if card.ProtocolVersion != a2a.ProtocolVersion {
		t.Errorf("want protocol version %s, got %s", a2a.ProtocolVersion, card.ProtocolVersion)
	}
	if len(card.Skills) != 2 {
		t.Errorf("want 2 skills, got %d", len(card.Skills))
	}
}

func TestAgentCardIdempotent(t *testing.T) {
	deps := NewTestServer()
	resp1 := dispatch(t, deps.Server, "agent/card", nil)
	resp2 := dispatch(t, deps.Server, "agent/card", nil)

	if resp1.Error != nil || resp2.Error != nil {
		t.Fatal("unexpected error")
	}

	var c1, c2 a2a.AgentCard
	_ = json.Unmarshal(resp1.Result, &c1)
	_ = json.Unmarshal(resp2.Result, &c2)
	if c1.Name != c2.Name {
		t.Error("agent/card not idempotent")
	}
}

// --- agent/ping tests ---

func TestAgentPing(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "agent/ping", nil)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var pr pingResponse
	if err := json.Unmarshal(resp.Result, &pr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !pr.OK {
		t.Error("expected ok=true")
	}
	if pr.Timestamp == "" {
		t.Error("expected non-empty timestamp")
	}
	if pr.Version == "" {
		t.Error("expected non-empty version")
	}
}

func TestAgentPingVersion(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "agent/ping", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
	var pr pingResponse
	_ = json.Unmarshal(resp.Result, &pr)
	if pr.Version != a2a.ImplementationVersion() {
		t.Errorf("want version %s, got %s", a2a.ImplementationVersion(), pr.Version)
	}
}

// --- agent/invoke tests ---

func TestAgentInvokeNilParams(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "agent/invoke", nil)

	if resp.Error == nil {
		t.Fatal("expected error for nil params")
	}
	// With no params, the handler returns INVALID_PARAMS because
	// json.Unmarshal(nil, ...) fails.
	if resp.Error.Code != a2a.CodeInvalidParams {
		t.Errorf("want code %d, got %d", a2a.CodeInvalidParams, resp.Error.Code)
	}
}

// --- governance tests (require admin) ---

func TestGovernanceRequiresAdmin(t *testing.T) {
	methods := []string{
		"governance/grants/list",
		"governance/grants/create",
		"governance/grants/revoke",
		"governance/approvals/list",
		"governance/approvals/decide",
		"governance/audit/query",
	}

	for _, method := range methods {
		t.Run(method+" no admin", func(t *testing.T) {
			deps := NewTestServer()
			resp := dispatch(t, deps.Server, method, json.RawMessage(`{}`))
			if resp.Error == nil {
				t.Fatal("expected permission denied")
			}
			if resp.Error.Code != a2a.CodePermissionDenied {
				t.Errorf("want code %d, got %d", a2a.CodePermissionDenied, resp.Error.Code)
			}
		})
	}
}

func TestGovernanceGrantsList(t *testing.T) {
	deps := NewTestServer()
	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/grants/list", grantsListParams{})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var grants []Grant
	if err := json.Unmarshal(resp.Result, &grants); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("want 0 grants, got %d", len(grants))
	}
}

func TestGovernanceGrantsCreate(t *testing.T) {
	deps := NewTestServer()
	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/grants/create", grantCreateParams{
		SourceAgentID: "agent-a",
		TargetAgentID: "agent-b",
		Decision:      "allow",
		Reason:        "test grant",
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var grant Grant
	if err := json.Unmarshal(resp.Result, &grant); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if grant.SourceAgentID != "agent-a" {
		t.Errorf("want source agent-a, got %s", grant.SourceAgentID)
	}
	if grant.GrantID == "" {
		t.Error("want non-empty grantId")
	}
}

func TestGovernanceGrantsCreateMissingParams(t *testing.T) {
	deps := NewTestServer()
	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/grants/create", grantCreateParams{
		SourceAgentID: "",
		TargetAgentID: "",
	})

	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != a2a.CodeInvalidParams {
		t.Errorf("want code %d, got %d", a2a.CodeInvalidParams, resp.Error.Code)
	}
}

func TestGovernanceGrantsRevoke(t *testing.T) {
	deps := NewTestServer()

	createResp := dispatchCtx(t, deps.Server, adminCtx(), "governance/grants/create", grantCreateParams{
		SourceAgentID: "agent-a",
		TargetAgentID: "agent-b",
		Decision:      "allow",
	})
	if createResp.Error != nil {
		t.Fatalf("create failed: %s", createResp.Error.Message)
	}
	var grant Grant
	_ = json.Unmarshal(createResp.Result, &grant)

	revokeResp := dispatchCtx(t, deps.Server, adminCtx(), "governance/grants/revoke", grantRevokeParams{
		GrantID: grant.GrantID,
	})
	if revokeResp.Error != nil {
		t.Fatalf("revoke failed: %s", revokeResp.Error.Message)
	}
}

func TestGovernanceGrantsRevokeMissingID(t *testing.T) {
	deps := NewTestServer()
	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/grants/revoke", grantRevokeParams{GrantID: ""})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != a2a.CodeInvalidParams {
		t.Errorf("want code %d, got %d", a2a.CodeInvalidParams, resp.Error.Code)
	}
}

func TestGovernanceApprovalsList(t *testing.T) {
	deps := NewTestServer()
	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/approvals/list", nil)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

func TestGovernanceApprovalsDecideMissingApprovalID(t *testing.T) {
	deps := NewTestServer()
	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/approvals/decide",
		approvalDecideParams{Decision: "allow"})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != a2a.CodeInvalidParams {
		t.Errorf("want code %d, got %d", a2a.CodeInvalidParams, resp.Error.Code)
	}
}

func TestGovernanceApprovalsDecideMissingDecision(t *testing.T) {
	deps := NewTestServer()
	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/approvals/decide",
		approvalDecideParams{ApprovalID: "apr_dummy"})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != a2a.CodeInvalidParams {
		t.Errorf("want code %d, got %d", a2a.CodeInvalidParams, resp.Error.Code)
	}
}

func TestGovernanceAuditQuery(t *testing.T) {
	deps := NewTestServer()
	deps.Governance.AddAuditEntry(AuditEntry{
		AuditID:   a2a.NewAuditID(),
		TaskID:    "tsk_test",
		EventType: "task.started",
		Timestamp: a2a.Now(),
	})

	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/audit/query", auditQueryParams{
		TaskID: "tsk_test",
		Limit:  10,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var entries []AuditEntry
	if err := json.Unmarshal(resp.Result, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("want 1 entry, got %d", len(entries))
	}
}

func TestGovernanceAuditQueryByEventType(t *testing.T) {
	deps := NewTestServer()
	deps.Governance.AddAuditEntry(AuditEntry{
		AuditID:   a2a.NewAuditID(),
		TaskID:    "tsk_a",
		EventType: "task.started",
		Timestamp: a2a.Now(),
	})
	deps.Governance.AddAuditEntry(AuditEntry{
		AuditID:   a2a.NewAuditID(),
		TaskID:    "tsk_a",
		EventType: "task.completed",
		Timestamp: a2a.Now(),
	})

	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/audit/query", auditQueryParams{
		EventType: "task.completed",
		Limit:     10,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var entries []AuditEntry
	_ = json.Unmarshal(resp.Result, &entries)
	if len(entries) != 1 {
		t.Errorf("want 1 completed entry, got %d", len(entries))
	}
}

func TestGovernanceAuditQueryDefaultLimit(t *testing.T) {
	deps := NewTestServer()
	// Just ensure no error with empty params.
	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/audit/query", auditQueryParams{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

// --- method not found test ---

func TestMethodNotFound(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "nonexistent/method", nil)
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	if resp.Error.Code != a2a.CodeMethodNotFound {
		t.Errorf("want code %d, got %d", a2a.CodeMethodNotFound, resp.Error.Code)
	}
}

// --- concurrent access test ---

func TestConcurrentMessageSend(t *testing.T) {
	deps := NewTestServer()
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		go func(i int) {
			resp := dispatch(t, deps.Server, "message/send", messageSendParams{
				Message: validMessage(),
				Skill:   "echo",
			})
			if resp.Error != nil {
				errs <- fmt.Errorf("request %d failed: %s", i, resp.Error.Message)
			} else {
				errs <- nil
			}
		}(i)
	}

	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

// --- audit sink test ---

func TestAuditSinkRecordsEvents(t *testing.T) {
	deps := NewTestServer()
	resp := dispatch(t, deps.Server, "message/send", messageSendParams{
		Message: validMessage(),
		Skill:   "echo",
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	events := deps.AuditSink.Events()
	if len(events) < 2 {
		t.Errorf("expected at least 2 audit events (started + completed), got %d", len(events))
	}

	var foundStarted, foundCompleted bool
	for _, e := range events {
		if e.EventType == "task.started" {
			foundStarted = true
		}
		if e.EventType == "task.completed" {
			foundCompleted = true
		}
	}
	if !foundStarted {
		t.Error("missing task.started event")
	}
	if !foundCompleted {
		t.Error("missing task.completed event")
	}
}

func TestAuditSinkRecordsCanceledEvent(t *testing.T) {
	deps := NewTestServer()
	task := a2a.NewTask()
	task.Status = a2a.TaskStatus{State: a2a.TaskStateWorking, Timestamp: a2a.Now()}
	_ = deps.TaskStore.CreateTask(context.Background(), &task)

	resp := dispatch(t, deps.Server, "tasks/cancel", taskIDParams{TaskID: task.TaskID})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	events := deps.AuditSink.Events()
	var found bool
	for _, e := range events {
		if e.EventType == "task.canceled" {
			found = true
		}
	}
	if !found {
		t.Error("missing task.canceled event")
	}
}

// --- file part handling test ---

func TestMessageSendWithFilePart(t *testing.T) {
	deps := NewTestServer()
	uri := "https://example.com/file.txt"
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewFilePart(a2a.FileRef{
		Name:     "test.txt",
		MimeType: "text/plain",
		URI:      &uri,
	}))

	resp := dispatch(t, deps.Server, "message/send", messageSendParams{
		Message: msg,
		Skill:   "echo",
	})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("want completed, got %s", task.Status.State)
	}
}

// --- FakeSkillRegistry tests ---

func TestFakeSkillRegistryListSkills(t *testing.T) {
	reg := NewFakeSkillRegistry(
		a2a.Skill{ID: "a", Name: "a"},
		a2a.Skill{ID: "b", Name: "b"},
	)
	skills := reg.ListSkills()
	if len(skills) != 2 {
		t.Errorf("want 2 skills, got %d", len(skills))
	}
}

func TestFakeSkillRegistryGetSkillNotFound(t *testing.T) {
	reg := NewFakeSkillRegistry()
	_, ok := reg.GetSkill("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestFakeSkillRegistryGetSkillFound(t *testing.T) {
	reg := NewFakeSkillRegistry(a2a.Skill{ID: "test", Name: "test"})
	sk, ok := reg.GetSkill("test")
	if !ok {
		t.Fatal("expected found")
	}
	if sk.ID != "test" {
		t.Errorf("want ID test, got %s", sk.ID)
	}
}

// --- InMemorySkillExecutor tests ---

func TestInMemorySkillExecutorNotRegistered(t *testing.T) {
	exec := NewInMemorySkillExecutor(nil)
	_, _, err := exec.Execute(context.Background(), "nope", nil, nil)
	if err == nil {
		t.Error("expected error for unregistered skill")
	}
}

func TestInMemorySkillExecutorRegister(t *testing.T) {
	exec := NewInMemorySkillExecutor(nil)
	exec.RegisterSkill("test", func(_ context.Context, input *a2a.DataPart, _ []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
		return input, nil, nil
	})
	_, _, err := exec.Execute(context.Background(), "test", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- Server option tests ---

func TestServerOptionsApplied(t *testing.T) {
	card := a2a.AgentCard{
		Name:            "opt-test",
		URL:             "http://localhost",
		ProtocolVersion: a2a.ProtocolVersion,
		Endpoints:       []a2a.Endpoint{{URL: "http://localhost/a2a", Transport: a2a.TransportJSONRPC}},
	}

	store := NewFakeTaskStore()
	gov := NewFakeGovernance()
	audit := NewFakeAuditSink()
	push := NewFakePushNotifier()

	srv := NewServer(card,
		WithTaskStore(store),
		WithGovernanceEngine(gov),
		WithAuditSink(audit),
		WithPushNotifier(push),
	)

	if srv.taskStore == nil {
		t.Error("taskStore not set")
	}
	if srv.governance == nil {
		t.Error("governance not set")
	}
	if srv.auditSink == nil {
		t.Error("auditSink not set")
	}
	if srv.pushNotifier == nil {
		t.Error("pushNotifier not set")
	}
}

// --- Router accessor test ---

func TestServerRouterNotNil(t *testing.T) {
	deps := NewTestServer()
	if deps.Server.Router() == nil {
		t.Error("Router() returned nil")
	}
}

// --- Task store tests ---

func TestFakeTaskStoreListTasks(t *testing.T) {
	store := NewFakeTaskStore()

	task1 := a2a.NewTask()
	task1.Status.State = a2a.TaskStateCompleted
	_ = store.CreateTask(context.Background(), &task1)

	task2 := a2a.NewTask()
	task2.Status.State = a2a.TaskStateWorking
	_ = store.CreateTask(context.Background(), &task2)

	tasks, err := store.ListTasks(context.Background(), TaskFilter{State: a2a.TaskStateCompleted})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("want 1 completed task, got %d", len(tasks))
	}
}

func TestFakeTaskStoreListTasksWithLimit(t *testing.T) {
	store := NewFakeTaskStore()

	for i := 0; i < 5; i++ {
		task := a2a.NewTask()
		_ = store.CreateTask(context.Background(), &task)
	}

	tasks, err := store.ListTasks(context.Background(), TaskFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) > 2 {
		t.Errorf("want at most 2 tasks, got %d", len(tasks))
	}
}

func TestFakeTaskStoreGetNotFound(t *testing.T) {
	store := NewFakeTaskStore()
	_, err := store.GetTask(context.Background(), "tsk_nope")
	if err == nil {
		t.Error("expected error")
	}
}

func TestFakeTaskStoreUpdateNotFound(t *testing.T) {
	store := NewFakeTaskStore()
	err := store.UpdateTaskStatus(context.Background(), "tsk_nope", a2a.TaskStatus{State: a2a.TaskStateCompleted})
	if err == nil {
		t.Error("expected error")
	}
}

func TestFakeTaskStoreAddArtifactNotFound(t *testing.T) {
	store := NewFakeTaskStore()
	err := store.AddArtifact(context.Background(), "tsk_nope", a2a.Artifact{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestFakeTaskStoreAddHistoryNotFound(t *testing.T) {
	store := NewFakeTaskStore()
	err := store.AddHistory(context.Background(), "tsk_nope", a2a.Message{})
	if err == nil {
		t.Error("expected error")
	}
}

// --- Governance with source agent context ---

func TestMessageSendWithSourceAgentContext(t *testing.T) {
	deps := NewTestServer()
	ctx := context.WithValue(context.Background(), CtxKeySourceAgent, "agent-caller")

	req, _ := jsonrpc.NewRequest(jsonrpc.StringID("test-1"), "message/send", messageSendParams{
		Message: validMessage(),
		Skill:   "echo",
	})
	resp := deps.Server.Dispatch(ctx, req)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}
}

// --- Governance revoke non-existent grant ---

func TestGovernanceGrantsRevokeNonExistent(t *testing.T) {
	deps := NewTestServer()
	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/grants/revoke",
		grantRevokeParams{GrantID: a2a.NewGrantID()})
	if resp.Error == nil {
		t.Fatal("expected error")
	}
	// The fake returns an error which the handler wraps as internal error.
	if resp.Error.Code != a2a.CodeInternalError {
		t.Errorf("want code %d, got %d", a2a.CodeInternalError, resp.Error.Code)
	}
}

// --- Create grant then list ---

func TestGovernanceGrantsCreateThenList(t *testing.T) {
	deps := NewTestServer()
	dispatchCtx(t, deps.Server, adminCtx(), "governance/grants/create", grantCreateParams{
		SourceAgentID: "agent-a",
		TargetAgentID: "agent-b",
		Decision:      "allow",
	})

	resp := dispatchCtx(t, deps.Server, adminCtx(), "governance/grants/list", grantsListParams{})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	var grants []Grant
	_ = json.Unmarshal(resp.Result, &grants)
	if len(grants) != 1 {
		t.Errorf("want 1 grant, got %d", len(grants))
	}
}

// --- Executor fail records audit ---

func TestExecutorFailRecordsFailedAudit(t *testing.T) {
	deps := NewTestServer()
	deps.SkillExecutor.RegisterSkill("echo", func(_ context.Context, _ *a2a.DataPart, _ []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
		return nil, nil, fmt.Errorf("skill exploded")
	})

	resp := dispatch(t, deps.Server, "message/send", messageSendParams{
		Message: validMessage(),
		Skill:   "echo",
	})
	if resp.Error == nil {
		t.Fatal("expected error")
	}

	events := deps.AuditSink.Events()
	var foundFailed bool
	for _, e := range events {
		if e.EventType == "task.failed" {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Error("missing task.failed audit event")
	}
}
