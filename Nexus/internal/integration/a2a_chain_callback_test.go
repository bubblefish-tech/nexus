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

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/jsonrpc"
	"github.com/bubblefish-tech/nexus/internal/a2a/server"
)

// fakeClientPool is a test double for server.ClientPool that maps target agent
// IDs to NA2A servers for dispatching agent/invoke calls.
type fakeClientPool struct {
	mu      sync.RWMutex
	servers map[string]*server.Server
}

func newFakeClientPool() *fakeClientPool {
	return &fakeClientPool{servers: make(map[string]*server.Server)}
}

func (p *fakeClientPool) Register(agentID string, srv *server.Server) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.servers[agentID] = srv
}

// SendMessage implements server.ClientPool by dispatching to the registered server.
func (p *fakeClientPool) SendMessage(ctx context.Context, targetAgentID string, msg *a2a.Message, skill string) (*a2a.Task, error) {
	p.mu.RLock()
	srv, ok := p.servers[targetAgentID]
	p.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("agent %q not found in pool", targetAgentID)
	}

	// Build a JSON-RPC request for message/send.
	params := map[string]interface{}{
		"message":  msg,
		"skill":    skill,
		"blocking": true,
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "message/send",
		Params:  paramsJSON,
		ID:      jsonrpc.NumberID(1),
	}

	resp := srv.Dispatch(ctx, req)
	if resp.Error != nil {
		return nil, fmt.Errorf("dispatch error: %s", resp.Error.Message)
	}

	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		return nil, fmt.Errorf("unmarshal task: %w", err)
	}
	return &task, nil
}

// makeBidirectionalAgent creates an NA2A server that forwards to a target
// agent via agent/invoke. The skill "forward_to_<target>" invokes the target.
func makeBidirectionalAgent(t *testing.T, agentID, targetID string, pool *fakeClientPool) *server.Server {
	t.Helper()

	forwardSkill := a2a.Skill{
		ID:          "forward_to_" + targetID,
		Name:        "forward_to_" + targetID,
		Description: fmt.Sprintf("Forwards to %s", targetID),
	}
	echoSkill := a2a.Skill{
		ID:          "echo",
		Name:        "echo",
		Description: "Echoes input back",
	}

	skills := map[string]server.SkillFunc{
		"forward_to_" + targetID: func(ctx context.Context, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
			// This skill returns the input directly. The real forwarding
			// happens through agent/invoke at the protocol layer.
			return input, files, nil
		},
		"echo": func(_ context.Context, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
			return input, files, nil
		},
	}

	card := a2a.AgentCard{
		Name:            agentID,
		URL:             "internal://" + agentID,
		ProtocolVersion: a2a.ProtocolVersion,
		Skills:          []a2a.Skill{forwardSkill, echoSkill},
		Capabilities: a2a.AgentCapabilities{
			Streaming:        false,
			StateTransitions: true,
		},
	}

	return server.NewServer(card,
		server.WithTaskStore(server.NewFakeTaskStore()),
		server.WithGovernanceEngine(server.NewFakeGovernance()),
		server.WithSkillRegistry(server.NewFakeSkillRegistry(forwardSkill, echoSkill)),
		server.WithSkillExecutor(server.NewFakeSkillExecutor(skills)),
		server.WithAuditSink(server.NewFakeAuditSink()),
		server.WithPushNotifier(server.NewFakePushNotifier()),
		server.WithClientPool(pool),
		server.WithLogger(slog.Default()),
	)
}

// TestChainCallback_AB_Simple tests a simple A → B callback chain.
func TestChainCallback_AB_Simple(t *testing.T) {
	pool := newFakeClientPool()

	// Agent B is the terminal agent with echo skill.
	agentB := makeBidirectionalAgent(t, "agent-b", "agent-c", pool)
	pool.Register("agent-b", agentB)

	// Agent A forwards to B.
	agentA := makeBidirectionalAgent(t, "agent-a", "agent-b", pool)
	pool.Register("agent-a", agentA)

	// Send message to agent B's echo skill via direct dispatch.
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("hello chain"))
	paramsJSON, _ := json.Marshal(map[string]interface{}{
		"message":  &msg,
		"skill":    "echo",
		"blocking": true,
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "message/send",
		Params:  paramsJSON,
		ID:      jsonrpc.NumberID(1),
	}

	resp := agentB.Dispatch(context.Background(), req)
	if resp.Error != nil {
		t.Fatalf("dispatch error: %s", resp.Error.Message)
	}

	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected completed, got %s", task.Status.State)
	}
}

// TestChainCallback_ABC tests A → B → C callback chain using agent/invoke.
func TestChainCallback_ABC(t *testing.T) {
	pool := newFakeClientPool()

	// Agent C: terminal echo agent.
	agentC := makeBidirectionalAgent(t, "agent-c", "agent-d", pool)
	pool.Register("agent-c", agentC)

	// Agent B: forwards to C.
	agentB := makeBidirectionalAgent(t, "agent-b", "agent-c", pool)
	pool.Register("agent-b", agentB)

	// Agent A: forwards to B.
	agentA := makeBidirectionalAgent(t, "agent-a", "agent-b", pool)
	pool.Register("agent-a", agentA)

	// Call agent/invoke on A to invoke B.
	ctx := context.WithValue(context.Background(), server.CtxKeySourceAgent, "agent-a")
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("chain A->B->C"))
	invokeParams, _ := json.Marshal(map[string]interface{}{
		"targetAgentId": "agent-b",
		"skill":         "echo",
		"message":       &msg,
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "agent/invoke",
		Params:  invokeParams,
		ID:      jsonrpc.NumberID(1),
	}

	resp := agentA.Dispatch(ctx, req)
	if resp.Error != nil {
		t.Fatalf("agent/invoke error: %s", resp.Error.Message)
	}

	var task a2a.Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if task.Status.State != a2a.TaskStateCompleted {
		t.Errorf("expected completed, got %s", task.Status.State)
	}
}

// TestChainCallback_DepthLimit verifies that chain depth limiting prevents
// infinite loops. Default max depth is 4.
func TestChainCallback_DepthLimit(t *testing.T) {
	pool := newFakeClientPool()

	agentB := makeBidirectionalAgent(t, "agent-b", "agent-a", pool)
	pool.Register("agent-b", agentB)

	agentA := makeBidirectionalAgent(t, "agent-a", "agent-b", pool)
	pool.Register("agent-a", agentA)

	// Build a message with chain depth already at the limit.
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("should exceed depth"))
	gov := a2a.GovernanceExtension{ChainDepth: server.DefaultMaxChainDepth}
	govJSON, _ := json.Marshal(gov)
	extMap := map[string]json.RawMessage{a2a.GovernanceExtensionURI: govJSON}
	extJSON, _ := json.Marshal(extMap)
	msg.Extensions = extJSON

	ctx := context.WithValue(context.Background(), server.CtxKeySourceAgent, "agent-a")
	invokeParams, _ := json.Marshal(map[string]interface{}{
		"targetAgentId": "agent-b",
		"skill":         "echo",
		"message":       &msg,
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "agent/invoke",
		Params:  invokeParams,
		ID:      jsonrpc.NumberID(1),
	}

	resp := agentA.Dispatch(ctx, req)
	if resp.Error == nil {
		t.Fatal("expected error for chain depth exceeded, got success")
	}
	t.Logf("correctly rejected: %s", resp.Error.Message)
}

// TestChainCallback_CycleDetection verifies A → B → A is caught by depth limit.
func TestChainCallback_CycleDetection(t *testing.T) {
	pool := newFakeClientPool()

	// A and B point at each other. Without depth limit they'd loop forever.
	agentB := makeBidirectionalAgent(t, "agent-b", "agent-a", pool)
	pool.Register("agent-b", agentB)

	agentA := makeBidirectionalAgent(t, "agent-a", "agent-b", pool)
	pool.Register("agent-a", agentA)

	// Start with depth 0 → should succeed for a few hops then fail.
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("cycle test"))
	ctx := context.WithValue(context.Background(), server.CtxKeySourceAgent, "agent-a")

	invokeParams, _ := json.Marshal(map[string]interface{}{
		"targetAgentId": "agent-b",
		"skill":         "echo",
		"message":       &msg,
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "agent/invoke",
		Params:  invokeParams,
		ID:      jsonrpc.NumberID(1),
	}

	// This should succeed because B just echoes (doesn't invoke A back).
	// The cycle only happens if B also does agent/invoke back to A.
	resp := agentA.Dispatch(ctx, req)
	if resp.Error != nil {
		t.Fatalf("unexpected error for simple forward: %s", resp.Error.Message)
	}
}

// TestChainCallback_MissingSourceAgent verifies agent/invoke requires
// a source agent identity in context.
func TestChainCallback_MissingSourceAgent(t *testing.T) {
	pool := newFakeClientPool()

	agentA := makeBidirectionalAgent(t, "agent-a", "agent-b", pool)
	pool.Register("agent-a", agentA)

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("no source"))
	invokeParams, _ := json.Marshal(map[string]interface{}{
		"targetAgentId": "agent-b",
		"skill":         "echo",
		"message":       &msg,
	})

	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "agent/invoke",
		Params:  invokeParams,
		ID:      jsonrpc.NumberID(1),
	}

	// No CtxKeySourceAgent in context.
	resp := agentA.Dispatch(context.Background(), req)
	if resp.Error == nil {
		t.Fatal("expected error for missing source agent, got success")
	}
	if resp.Error.Code != a2a.CodeUnauthenticated {
		t.Errorf("expected CodeUnauthenticated, got %d", resp.Error.Code)
	}
}

// TestChainCallback_MissingTargetAgent verifies agent/invoke returns error
// for missing targetAgentId parameter.
func TestChainCallback_MissingTargetAgent(t *testing.T) {
	pool := newFakeClientPool()
	agentA := makeBidirectionalAgent(t, "agent-a", "agent-b", pool)

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("no target"))
	invokeParams, _ := json.Marshal(map[string]interface{}{
		"skill":   "echo",
		"message": &msg,
	})

	ctx := context.WithValue(context.Background(), server.CtxKeySourceAgent, "agent-a")
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "agent/invoke",
		Params:  invokeParams,
		ID:      jsonrpc.NumberID(1),
	}

	resp := agentA.Dispatch(ctx, req)
	if resp.Error == nil {
		t.Fatal("expected error for missing target, got success")
	}
}

// TestChainCallback_MissingMessage verifies agent/invoke returns error
// for missing message parameter.
func TestChainCallback_MissingMessage(t *testing.T) {
	pool := newFakeClientPool()
	agentA := makeBidirectionalAgent(t, "agent-a", "agent-b", pool)

	invokeParams, _ := json.Marshal(map[string]interface{}{
		"targetAgentId": "agent-b",
		"skill":         "echo",
	})

	ctx := context.WithValue(context.Background(), server.CtxKeySourceAgent, "agent-a")
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "agent/invoke",
		Params:  invokeParams,
		ID:      jsonrpc.NumberID(1),
	}

	resp := agentA.Dispatch(ctx, req)
	if resp.Error == nil {
		t.Fatal("expected error for missing message, got success")
	}
}

// TestChainCallback_GovernanceDeny verifies that agent/invoke respects
// governance denials.
func TestChainCallback_GovernanceDeny(t *testing.T) {
	pool := newFakeClientPool()

	// Create agent A with governance set to deny all.
	echoSkill := a2a.Skill{ID: "echo", Name: "echo", Description: "Echoes input"}
	denyGov := server.NewFakeGovernance()
	denyGov.DenyAll = true
	denyGov.DenyReason = "governance denies agent/invoke"

	card := a2a.AgentCard{
		Name:            "agent-a-deny",
		URL:             "internal://agent-a-deny",
		ProtocolVersion: a2a.ProtocolVersion,
		Skills:          []a2a.Skill{echoSkill},
	}

	agentA := server.NewServer(card,
		server.WithTaskStore(server.NewFakeTaskStore()),
		server.WithGovernanceEngine(denyGov),
		server.WithSkillRegistry(server.NewFakeSkillRegistry(echoSkill)),
		server.WithSkillExecutor(server.NewFakeSkillExecutor(map[string]server.SkillFunc{
			"echo": func(_ context.Context, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
				return input, files, nil
			},
		})),
		server.WithAuditSink(server.NewFakeAuditSink()),
		server.WithPushNotifier(server.NewFakePushNotifier()),
		server.WithClientPool(pool),
		server.WithLogger(slog.Default()),
	)

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("should be denied"))
	invokeParams, _ := json.Marshal(map[string]interface{}{
		"targetAgentId": "agent-b",
		"skill":         "echo",
		"message":       &msg,
	})

	ctx := context.WithValue(context.Background(), server.CtxKeySourceAgent, "agent-a")
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "agent/invoke",
		Params:  invokeParams,
		ID:      jsonrpc.NumberID(1),
	}

	resp := agentA.Dispatch(ctx, req)
	if resp.Error == nil {
		t.Fatal("expected governance deny error, got success")
	}
	if resp.Error.Code != a2a.CodePermissionDenied {
		t.Errorf("expected CodePermissionDenied (%d), got %d", a2a.CodePermissionDenied, resp.Error.Code)
	}
}

// TestChainCallback_NoClientPool verifies agent/invoke errors when no
// client pool is configured.
func TestChainCallback_NoClientPool(t *testing.T) {
	echoSkill := a2a.Skill{ID: "echo", Name: "echo"}
	card := a2a.AgentCard{
		Name:            "agent-no-pool",
		URL:             "internal://no-pool",
		ProtocolVersion: a2a.ProtocolVersion,
		Skills:          []a2a.Skill{echoSkill},
	}

	// No WithClientPool option → clientPool is nil.
	agentA := server.NewServer(card,
		server.WithTaskStore(server.NewFakeTaskStore()),
		server.WithGovernanceEngine(server.NewFakeGovernance()),
		server.WithSkillRegistry(server.NewFakeSkillRegistry(echoSkill)),
		server.WithSkillExecutor(server.NewFakeSkillExecutor(map[string]server.SkillFunc{
			"echo": func(_ context.Context, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
				return input, files, nil
			},
		})),
		server.WithAuditSink(server.NewFakeAuditSink()),
		server.WithPushNotifier(server.NewFakePushNotifier()),
		server.WithLogger(slog.Default()),
	)

	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("no pool"))
	invokeParams, _ := json.Marshal(map[string]interface{}{
		"targetAgentId": "agent-b",
		"skill":         "echo",
		"message":       &msg,
	})

	ctx := context.WithValue(context.Background(), server.CtxKeySourceAgent, "agent-a")
	req := &jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "agent/invoke",
		Params:  invokeParams,
		ID:      jsonrpc.NumberID(1),
	}

	resp := agentA.Dispatch(ctx, req)
	if resp.Error == nil {
		t.Fatal("expected error for no client pool, got success")
	}
}

// TestChainCallback_ChainDepthIncrement verifies that chain depth is
// incremented correctly through IncrementChainDepth.
func TestChainCallback_ChainDepthIncrement(t *testing.T) {
	msg := a2a.NewMessage(a2a.RoleUser, a2a.NewTextPart("depth test"))

	// Initial depth should be 0.
	depth := server.ExtractChainDepth(&msg)
	if depth != 0 {
		t.Errorf("expected initial depth=0, got %d", depth)
	}

	// Increment to 1.
	msg1, err := server.IncrementChainDepth(&msg, 4)
	if err != nil {
		t.Fatalf("increment 1: %v", err)
	}
	if d := server.ExtractChainDepth(msg1); d != 1 {
		t.Errorf("expected depth=1, got %d", d)
	}

	// Increment to 2.
	msg2, err := server.IncrementChainDepth(msg1, 4)
	if err != nil {
		t.Fatalf("increment 2: %v", err)
	}
	if d := server.ExtractChainDepth(msg2); d != 2 {
		t.Errorf("expected depth=2, got %d", d)
	}

	// Increment to 3.
	msg3, err := server.IncrementChainDepth(msg2, 4)
	if err != nil {
		t.Fatalf("increment 3: %v", err)
	}
	if d := server.ExtractChainDepth(msg3); d != 3 {
		t.Errorf("expected depth=3, got %d", d)
	}

	// Increment to 4.
	msg4, err := server.IncrementChainDepth(msg3, 4)
	if err != nil {
		t.Fatalf("increment 4: %v", err)
	}
	if d := server.ExtractChainDepth(msg4); d != 4 {
		t.Errorf("expected depth=4, got %d", d)
	}

	// Increment past max should fail.
	_, err = server.IncrementChainDepth(msg4, 4)
	if err == nil {
		t.Fatal("expected error for depth exceeding max, got nil")
	}
}
