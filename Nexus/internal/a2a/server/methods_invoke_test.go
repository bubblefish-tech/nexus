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
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/jsonrpc"
)

// --- FakeClientPool ---

// FakeClientPool is a mock ClientPool for testing agent/invoke.
type FakeClientPool struct {
	// SendFunc is called when SendMessage is invoked.
	SendFunc func(ctx context.Context, targetAgentID string, msg *a2a.Message, skill string) (*a2a.Task, error)
}

// SendMessage implements ClientPool.
func (f *FakeClientPool) SendMessage(ctx context.Context, targetAgentID string, msg *a2a.Message, skill string) (*a2a.Task, error) {
	if f.SendFunc != nil {
		return f.SendFunc(ctx, targetAgentID, msg, skill)
	}
	task := a2a.NewTask()
	return &task, nil
}

// --- Test helpers ---

func makeInvokeServer(t *testing.T, pool ClientPool, gov GovernanceEngine) *Server {
	t.Helper()
	card := a2a.AgentCard{
		Name:            "test-invoke-agent",
		URL:             "http://localhost:9999",
		ProtocolVersion: a2a.ProtocolVersion,
	}
	opts := []ServerOption{
		WithTaskStore(NewFakeTaskStore()),
		WithSkillRegistry(NewFakeSkillRegistry()),
		WithAuditSink(NewFakeAuditSink()),
		WithLogger(slog.Default()),
	}
	if pool != nil {
		opts = append(opts, WithClientPool(pool))
	}
	if gov != nil {
		opts = append(opts, WithGovernanceEngine(gov))
	}
	return NewServer(card, opts...)
}

func invokeCtx(sourceAgent string) context.Context {
	return context.WithValue(context.Background(), CtxKeySourceAgent, sourceAgent)
}

func marshalParams(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return data
}

func callInvoke(t *testing.T, srv *Server, ctx context.Context, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	t.Helper()
	return srv.handleAgentInvoke(ctx, "agent/invoke", params)
}

// --- Tests ---

func TestInvokeSuccessful(t *testing.T) {
	pool := &FakeClientPool{
		SendFunc: func(_ context.Context, target string, msg *a2a.Message, skill string) (*a2a.Task, error) {
			task := a2a.NewTask()
			task.Status.State = a2a.TaskStateCompleted
			return &task, nil
		},
	}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Skill:         "echo",
		Message:       &msg,
	})

	result, errObj := callInvoke(t, srv, ctx, params)
	if errObj != nil {
		t.Fatalf("expected success, got error: %v", errObj)
	}

	task, ok := result.(*a2a.Task)
	if !ok {
		t.Fatalf("expected *a2a.Task, got %T", result)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected completed, got %q", task.Status.State)
	}
}

func TestInvokeChainDepthExceeded(t *testing.T) {
	pool := &FakeClientPool{}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	// Create a message with chain depth at the max.
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	gov := a2a.GovernanceExtension{ChainDepth: DefaultMaxChainDepth}
	govJSON, _ := json.Marshal(gov)
	extMap := map[string]json.RawMessage{a2a.GovernanceExtensionURI: govJSON}
	msg.Extensions, _ = json.Marshal(extMap)

	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj == nil {
		t.Fatal("expected chain depth error")
	}
	if errObj.Code != a2a.CodeInternalError {
		t.Errorf("expected INTERNAL_ERROR, got %d", errObj.Code)
	}
}

func TestInvokeChainDepthAtLimit(t *testing.T) {
	// Depth 3 with max 4 should succeed (will be incremented to 4 on forward).
	pool := &FakeClientPool{}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	gov := a2a.GovernanceExtension{ChainDepth: DefaultMaxChainDepth - 1}
	govJSON, _ := json.Marshal(gov)
	extMap := map[string]json.RawMessage{a2a.GovernanceExtensionURI: govJSON}
	msg.Extensions, _ = json.Marshal(extMap)

	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	result, errObj := callInvoke(t, srv, ctx, params)
	if errObj != nil {
		t.Fatalf("expected success at depth %d, got error: %v", DefaultMaxChainDepth-1, errObj)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestInvokeGovernanceDenied(t *testing.T) {
	pool := &FakeClientPool{}
	gov := NewFakeGovernance()
	gov.DenyAll = true
	gov.DenyReason = "test denial"

	srv := makeInvokeServer(t, pool, gov)
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj == nil {
		t.Fatal("expected governance denial")
	}
	if errObj.Code != a2a.CodePermissionDenied {
		t.Errorf("expected PERMISSION_DENIED, got %d", errObj.Code)
	}
}

func TestInvokeGovernanceEscalated(t *testing.T) {
	pool := &FakeClientPool{}
	gov := NewFakeGovernance()
	gov.EscalateAll = true
	gov.EscalateReason = "requires approval"

	srv := makeInvokeServer(t, pool, gov)
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj == nil {
		t.Fatal("expected governance escalation")
	}
	if errObj.Code != a2a.CodeApprovalRequired {
		t.Errorf("expected APPROVAL_REQUIRED, got %d", errObj.Code)
	}
}

func TestInvokeNoClientPool(t *testing.T) {
	srv := makeInvokeServer(t, nil, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj == nil {
		t.Fatal("expected METHOD_NOT_FOUND for nil client pool")
	}
	if errObj.Code != a2a.CodeMethodNotFound {
		t.Errorf("expected METHOD_NOT_FOUND, got %d", errObj.Code)
	}
}

func TestInvokeMissingSourceAgent(t *testing.T) {
	pool := &FakeClientPool{}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	// No source agent in context.
	ctx := context.Background()

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj == nil {
		t.Fatal("expected UNAUTHENTICATED error")
	}
	if errObj.Code != a2a.CodeUnauthenticated {
		t.Errorf("expected UNAUTHENTICATED, got %d", errObj.Code)
	}
}

func TestInvokeMissingTargetAgent(t *testing.T) {
	pool := &FakeClientPool{}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj == nil {
		t.Fatal("expected INVALID_PARAMS error")
	}
	if errObj.Code != a2a.CodeInvalidParams {
		t.Errorf("expected INVALID_PARAMS, got %d", errObj.Code)
	}
}

func TestInvokeMissingMessage(t *testing.T) {
	pool := &FakeClientPool{}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       nil,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj == nil {
		t.Fatal("expected INVALID_PARAMS error")
	}
	if errObj.Code != a2a.CodeInvalidParams {
		t.Errorf("expected INVALID_PARAMS, got %d", errObj.Code)
	}
}

func TestInvokeInvalidParams(t *testing.T) {
	pool := &FakeClientPool{}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	_, errObj := callInvoke(t, srv, ctx, json.RawMessage(`{invalid`))
	if errObj == nil {
		t.Fatal("expected INVALID_PARAMS error for malformed JSON")
	}
	if errObj.Code != a2a.CodeInvalidParams {
		t.Errorf("expected INVALID_PARAMS, got %d", errObj.Code)
	}
}

func TestInvokeDispatchError(t *testing.T) {
	pool := &FakeClientPool{
		SendFunc: func(_ context.Context, _ string, _ *a2a.Message, _ string) (*a2a.Task, error) {
			return nil, errors.New("connection refused")
		},
	}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj == nil {
		t.Fatal("expected error for dispatch failure")
	}
	if errObj.Code != a2a.CodeInternalError {
		t.Errorf("expected INTERNAL_ERROR, got %d", errObj.Code)
	}
}

func TestInvokeCycleDetection(t *testing.T) {
	// Simulate A -> B -> A by incrementing depth each time.
	// At depth 0: A calls B (depth becomes 1) - OK
	// At depth 1: B calls A (depth becomes 2) - OK
	// ... until depth reaches max.
	var callCount int
	pool := &FakeClientPool{
		SendFunc: func(ctx context.Context, target string, msg *a2a.Message, skill string) (*a2a.Task, error) {
			callCount++
			task := a2a.NewTask()
			return &task, nil
		},
	}

	srv := makeInvokeServer(t, pool, NewFakeGovernance())

	// Simulate depth 3 (one below max of 4), should succeed.
	ctx := invokeCtx("agent-a")
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("ping"))
	gov := a2a.GovernanceExtension{ChainDepth: 3}
	govJSON, _ := json.Marshal(gov)
	extMap := map[string]json.RawMessage{a2a.GovernanceExtensionURI: govJSON}
	msg.Extensions, _ = json.Marshal(extMap)

	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	result, errObj := callInvoke(t, srv, ctx, params)
	if errObj != nil {
		t.Fatalf("depth 3 should succeed, got: %v", errObj)
	}
	if result == nil {
		t.Fatal("expected result")
	}

	// Now at depth 4 (at max), should fail.
	msg2 := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("ping"))
	gov2 := a2a.GovernanceExtension{ChainDepth: 4}
	govJSON2, _ := json.Marshal(gov2)
	extMap2 := map[string]json.RawMessage{a2a.GovernanceExtensionURI: govJSON2}
	msg2.Extensions, _ = json.Marshal(extMap2)

	params2 := marshalParams(t, invokeParams{
		TargetAgentID: "agent-a",
		Message:       &msg2,
	})

	_, errObj2 := callInvoke(t, srv, ctx, params2)
	if errObj2 == nil {
		t.Fatal("depth 4 should fail")
	}
}

func TestInvokeWithSkill(t *testing.T) {
	var capturedSkill string
	pool := &FakeClientPool{
		SendFunc: func(_ context.Context, _ string, _ *a2a.Message, skill string) (*a2a.Task, error) {
			capturedSkill = skill
			task := a2a.NewTask()
			return &task, nil
		},
	}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Skill:         "memory-search",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj != nil {
		t.Fatalf("expected success, got: %v", errObj)
	}
	if capturedSkill != "memory-search" {
		t.Errorf("expected skill memory-search, got %q", capturedSkill)
	}
}

func TestInvokeNoGovernanceEngine(t *testing.T) {
	// When no governance engine is configured, invoke should still work.
	pool := &FakeClientPool{}
	srv := makeInvokeServer(t, pool, nil)
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	result, errObj := callInvoke(t, srv, ctx, params)
	if errObj != nil {
		t.Fatalf("expected success without governance, got: %v", errObj)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestInvokeAuditLogged(t *testing.T) {
	pool := &FakeClientPool{}
	audit := NewFakeAuditSink()
	card := a2a.AgentCard{
		Name:            "test-audit-agent",
		URL:             "http://localhost:9999",
		ProtocolVersion: a2a.ProtocolVersion,
	}
	srv := NewServer(card,
		WithTaskStore(NewFakeTaskStore()),
		WithSkillRegistry(NewFakeSkillRegistry()),
		WithGovernanceEngine(NewFakeGovernance()),
		WithAuditSink(audit),
		WithClientPool(pool),
		WithLogger(slog.Default()),
	)

	ctx := invokeCtx("agent-a")
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj != nil {
		t.Fatalf("expected success, got: %v", errObj)
	}

	events := audit.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	if events[0].EventType != "agent.invoke" {
		t.Errorf("expected event type agent.invoke, got %q", events[0].EventType)
	}
}

func TestInvokeForwardedMessageHasIncrementedDepth(t *testing.T) {
	var capturedMsg *a2a.Message
	pool := &FakeClientPool{
		SendFunc: func(_ context.Context, _ string, msg *a2a.Message, _ string) (*a2a.Task, error) {
			capturedMsg = msg
			task := a2a.NewTask()
			return &task, nil
		},
	}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj != nil {
		t.Fatalf("expected success, got: %v", errObj)
	}

	if capturedMsg == nil {
		t.Fatal("expected message to be forwarded")
	}

	depth := ExtractChainDepth(capturedMsg)
	if depth != 1 {
		t.Errorf("expected forwarded depth 1, got %d", depth)
	}
}

func TestInvokeMultipleTargets(t *testing.T) {
	targets := make(map[string]int)
	pool := &FakeClientPool{
		SendFunc: func(_ context.Context, target string, _ *a2a.Message, _ string) (*a2a.Task, error) {
			targets[target]++
			task := a2a.NewTask()
			return &task, nil
		},
	}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	for _, target := range []string{"agent-b", "agent-c", "agent-d"} {
		msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
		params := marshalParams(t, invokeParams{
			TargetAgentID: target,
			Message:       &msg,
		})
		_, errObj := callInvoke(t, srv, ctx, params)
		if errObj != nil {
			t.Fatalf("invoke %s: %v", target, errObj)
		}
	}

	if len(targets) != 3 {
		t.Errorf("expected 3 unique targets, got %d", len(targets))
	}
}

func TestInvokeGovernanceCapabilityGlob(t *testing.T) {
	// Verify the governance request includes the correct capability.
	var capturedReq GovernanceReq
	gov := &capturingGovernance{captured: &capturedReq}

	pool := &FakeClientPool{}
	card := a2a.AgentCard{
		Name:            "test-cap-agent",
		URL:             "http://localhost:9999",
		ProtocolVersion: a2a.ProtocolVersion,
	}
	srv := NewServer(card,
		WithTaskStore(NewFakeTaskStore()),
		WithSkillRegistry(NewFakeSkillRegistry()),
		WithGovernanceEngine(gov),
		WithClientPool(pool),
		WithLogger(slog.Default()),
	)

	ctx := invokeCtx("agent-x")
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-y",
		Skill:         "search",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj != nil {
		t.Fatalf("expected success, got: %v", errObj)
	}

	if capturedReq.SourceAgentID != "agent-x" {
		t.Errorf("expected source agent-x, got %q", capturedReq.SourceAgentID)
	}
	if capturedReq.TargetAgentID != "agent-y" {
		t.Errorf("expected target agent-y, got %q", capturedReq.TargetAgentID)
	}
	if len(capturedReq.RequiredCapabilities) != 1 || capturedReq.RequiredCapabilities[0] != "agent.invoke:agent-y" {
		t.Errorf("expected capability agent.invoke:agent-y, got %v", capturedReq.RequiredCapabilities)
	}
}

// capturingGovernance captures the governance request for inspection.
type capturingGovernance struct {
	captured *GovernanceReq
}

func (g *capturingGovernance) Decide(_ context.Context, req GovernanceReq) (*GovernanceResult, error) {
	*g.captured = req
	return &GovernanceResult{
		Decision: "allow",
		GrantID:  a2a.NewGrantID(),
		AuditID:  a2a.NewAuditID(),
	}, nil
}

// --- Chain guard unit tests ---

func TestExtractChainDepthNoExtensions(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	if depth := ExtractChainDepth(&msg); depth != 0 {
		t.Errorf("expected 0, got %d", depth)
	}
}

func TestExtractChainDepthNilMessage(t *testing.T) {
	if depth := ExtractChainDepth(nil); depth != 0 {
		t.Errorf("expected 0 for nil message, got %d", depth)
	}
}

func TestExtractChainDepthWithGovernance(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	gov := a2a.GovernanceExtension{ChainDepth: 3}
	govJSON, _ := json.Marshal(gov)
	extMap := map[string]json.RawMessage{a2a.GovernanceExtensionURI: govJSON}
	msg.Extensions, _ = json.Marshal(extMap)

	if depth := ExtractChainDepth(&msg); depth != 3 {
		t.Errorf("expected 3, got %d", depth)
	}
}

func TestIncrementChainDepthFromZero(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	inc, err := IncrementChainDepth(&msg, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if depth := ExtractChainDepth(inc); depth != 1 {
		t.Errorf("expected 1, got %d", depth)
	}
}

func TestIncrementChainDepthExceedsMax(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	gov := a2a.GovernanceExtension{ChainDepth: 4}
	govJSON, _ := json.Marshal(gov)
	extMap := map[string]json.RawMessage{a2a.GovernanceExtensionURI: govJSON}
	msg.Extensions, _ = json.Marshal(extMap)

	_, err := IncrementChainDepth(&msg, 4)
	if err == nil {
		t.Fatal("expected error for exceeding max depth")
	}
}

func TestIncrementChainDepthPreservesOtherExtensions(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	extMap := map[string]json.RawMessage{
		"custom.ext/v1": json.RawMessage(`{"key":"value"}`),
	}
	msg.Extensions, _ = json.Marshal(extMap)

	inc, err := IncrementChainDepth(&msg, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify custom extension is preserved.
	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(inc.Extensions, &resultMap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resultMap["custom.ext/v1"]; !ok {
		t.Error("custom extension was not preserved")
	}
}

func TestIncrementChainDepthDoesNotMutateOriginal(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	originalExtensions := msg.Extensions

	_, err := IncrementChainDepth(&msg, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original message should not be modified (both nil in this case).
	if fmt.Sprintf("%v", msg.Extensions) != fmt.Sprintf("%v", originalExtensions) {
		t.Error("original message extensions were modified")
	}
}

func TestIncrementChainDepthDefaultMax(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	// Pass 0 to use default max depth.
	inc, err := IncrementChainDepth(&msg, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if depth := ExtractChainDepth(inc); depth != 1 {
		t.Errorf("expected 1, got %d", depth)
	}
}

func TestIncrementChainDepthSequential(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	for i := 1; i <= 4; i++ {
		inc, err := IncrementChainDepth(&msg, 4)
		if err != nil {
			t.Fatalf("increment %d: unexpected error: %v", i, err)
		}
		if depth := ExtractChainDepth(inc); depth != i {
			t.Errorf("increment %d: expected depth %d, got %d", i, i, depth)
		}
		msg = *inc
	}
	// Fifth increment should fail.
	_, err := IncrementChainDepth(&msg, 4)
	if err == nil {
		t.Fatal("expected error for 5th increment exceeding max 4")
	}
}

func TestInvokePreservesMessageParts(t *testing.T) {
	var capturedMsg *a2a.Message
	pool := &FakeClientPool{
		SendFunc: func(_ context.Context, _ string, msg *a2a.Message, _ string) (*a2a.Task, error) {
			capturedMsg = msg
			task := a2a.NewTask()
			return &task, nil
		},
	}
	srv := makeInvokeServer(t, pool, NewFakeGovernance())
	ctx := invokeCtx("agent-a")

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("important data"))
	params := marshalParams(t, invokeParams{
		TargetAgentID: "agent-b",
		Message:       &msg,
	})

	_, errObj := callInvoke(t, srv, ctx, params)
	if errObj != nil {
		t.Fatalf("expected success, got: %v", errObj)
	}

	if capturedMsg == nil {
		t.Fatal("expected message to be captured")
	}
	if len(capturedMsg.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(capturedMsg.Parts))
	}
	tp, ok := capturedMsg.Parts[0].Part.(a2a.TextPart)
	if !ok {
		t.Fatal("expected TextPart")
	}
	if tp.Text != "important data" {
		t.Errorf("expected text 'important data', got %q", tp.Text)
	}
}

func TestExtractChainDepthInvalidJSON(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	msg.Extensions = json.RawMessage(`{invalid}`)
	if depth := ExtractChainDepth(&msg); depth != 0 {
		t.Errorf("expected 0 for invalid JSON, got %d", depth)
	}
}

func TestExtractChainDepthNoGovernanceKey(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello"))
	extMap := map[string]json.RawMessage{
		"other.ext/v1": json.RawMessage(`{"foo":"bar"}`),
	}
	msg.Extensions, _ = json.Marshal(extMap)
	if depth := ExtractChainDepth(&msg); depth != 0 {
		t.Errorf("expected 0 for missing governance key, got %d", depth)
	}
}
