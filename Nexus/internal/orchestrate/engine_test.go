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

package orchestrate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bubblefish-tech/nexus/internal/orchestrate"
)

// --- Test doubles ---

type fakeAgentLister struct {
	agents []orchestrate.Agent
	err    error
}

func (f *fakeAgentLister) ListOrchestratableAgents(_ context.Context) ([]orchestrate.Agent, error) {
	return f.agents, f.err
}

type fakeGrantChecker struct {
	grants map[string]bool // "agentID:capability" → true
}

func (f *fakeGrantChecker) HasGrant(_ context.Context, agentID, capability string) (bool, error) {
	return f.grants[agentID+":"+capability], nil
}

type fakeMemoryWriter struct {
	written []string
}

func (f *fakeMemoryWriter) WriteMemory(_ context.Context, content, subject, actorID, derivedFrom string) (string, error) {
	id := "mem_" + content[:min(8, len(content))]
	f.written = append(f.written, id)
	return id, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func agentServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
}

func newEngine(t *testing.T, agents []orchestrate.Agent, grants map[string]bool) *orchestrate.Engine {
	t.Helper()
	return orchestrate.New(orchestrate.Config{
		Agents: &fakeAgentLister{agents: agents},
		Grants: &fakeGrantChecker{grants: grants},
	})
}

// --- Tests ---

func TestListAgents_Empty(t *testing.T) {
	t.Helper()
	eng := newEngine(t, nil, nil)
	list, err := eng.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestListAgents_ReturnsAll(t *testing.T) {
	t.Helper()
	agents := []orchestrate.Agent{
		{AgentID: "a1", Name: "alpha", Status: "active", Orchestratable: true, ConnectionKind: "http"},
		{AgentID: "a2", Name: "beta", Status: "active", Orchestratable: true, ConnectionKind: "http"},
	}
	eng := newEngine(t, agents, nil)
	list, err := eng.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(list))
	}
}

func TestOrchestrate_NoOrchestrationGrant(t *testing.T) {
	t.Helper()
	eng := newEngine(t, nil, map[string]bool{})
	_, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task: "do something",
	})
	if err == nil || !strings.Contains(err.Error(), "orchestrate grant") {
		t.Fatalf("expected orchestrate grant error, got: %v", err)
	}
}

func TestOrchestrate_EmptyTask(t *testing.T) {
	t.Helper()
	eng := newEngine(t, nil, map[string]bool{"caller-1:orchestrate": true})
	_, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{})
	if err == nil || !strings.Contains(err.Error(), "task is required") {
		t.Fatalf("expected task required error, got: %v", err)
	}
}

func TestOrchestrate_NoDispatchGrant(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 200, `{"result":"ok"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Name: "alpha", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		// no dispatch:a1 grant
	})
	_, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "test",
		TargetAgents: []string{"a1"},
	})
	if err == nil || !strings.Contains(err.Error(), "dispatch grant") {
		t.Fatalf("expected dispatch grant error, got: %v", err)
	}
}

func TestOrchestrate_WaitAll_Success(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 200, `{"result":"done"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Name: "alpha", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	res, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "do work",
		TargetAgents: []string{"a1"},
		FailStrategy: "wait_all",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res.Results))
	}
	if res.Results[0].Status != "ok" {
		t.Errorf("expected ok, got %q", res.Results[0].Status)
	}
	if res.Results[0].Output != "done" {
		t.Errorf("expected output 'done', got %q", res.Results[0].Output)
	}
	if res.OrchestrationID == "" {
		t.Error("expected non-empty orchestration_id")
	}
}

func TestOrchestrate_OpenAICompatResponse(t *testing.T) {
	t.Helper()
	body := `{"choices":[{"message":{"content":"hello from agent"}}]}`
	srv := agentServer(t, 200, body)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Name: "alpha", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	res, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "say hello",
		TargetAgents: []string{"a1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Results[0].Output != "hello from agent" {
		t.Errorf("expected OpenAI response content, got %q", res.Results[0].Output)
	}
}

func TestOrchestrate_TokenLimitStatus413(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 413, `{"error":"payload too large"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Name: "alpha", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	res, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "big task",
		TargetAgents: []string{"a1"},
		FailStrategy: "wait_all",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Results[0].Status != "token_limit_error" {
		t.Errorf("expected token_limit_error, got %q", res.Results[0].Status)
	}
}

func TestOrchestrate_TokenLimitStatus429(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 429, `{"error":"rate limit"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	res, _ := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "task",
		TargetAgents: []string{"a1"},
	})
	if res.Results[0].Status != "token_limit_error" {
		t.Errorf("expected token_limit_error for 429, got %q", res.Results[0].Status)
	}
}

func TestOrchestrate_TokenLimitBodyPhrase(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 200, `{"error":"context length exceeded, please reduce your input"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	res, _ := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "task",
		TargetAgents: []string{"a1"},
	})
	if res.Results[0].Status != "token_limit_error" {
		t.Errorf("expected token_limit_error from body phrase, got %q", res.Results[0].Status)
	}
}

func TestOrchestrate_UnsupportedConnectionKind(t *testing.T) {
	t.Helper()
	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "stdio"}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	res, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "task",
		TargetAgents: []string{"a1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Results[0].Status != "unsupported" {
		t.Errorf("expected unsupported, got %q", res.Results[0].Status)
	}
}

func TestOrchestrate_ReturnPartial(t *testing.T) {
	t.Helper()
	goodSrv := agentServer(t, 200, `{"result":"good"}`)
	defer goodSrv.Close()
	badSrv := agentServer(t, 500, `{"error":"fail"}`)
	defer badSrv.Close()

	agents := []orchestrate.Agent{
		{AgentID: "good", Status: "active", Orchestratable: true, ConnectionKind: "http", Endpoint: goodSrv.URL},
		{AgentID: "bad", Status: "active", Orchestratable: true, ConnectionKind: "http", Endpoint: badSrv.URL},
	}
	grants := map[string]bool{
		"caller-1:orchestrate":     true,
		"caller-1:dispatch:good":   true,
		"caller-1:dispatch:bad":    true,
	}
	eng := newEngine(t, agents, grants)
	res, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "task",
		FailStrategy: "return_partial",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, r := range res.Results {
		if r.Status != "ok" {
			t.Errorf("return_partial: expected only ok results, got %q for agent %q", r.Status, r.AgentID)
		}
	}
}

func TestOrchestrate_StoreResults(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 200, `{"result":"memory content"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	mw := &fakeMemoryWriter{}
	eng := orchestrate.New(orchestrate.Config{
		Agents: &fakeAgentLister{agents: agents},
		Grants: &fakeGrantChecker{grants: map[string]bool{
			"caller-1:orchestrate": true,
			"caller-1:dispatch:a1": true,
		}},
		Memory: mw,
	})
	res, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "store me",
		TargetAgents: []string{"a1"},
		StoreResults: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.StoredMemoryIDs) == 0 {
		t.Error("expected stored memory IDs when store_results=true")
	}
	if len(mw.written) == 0 {
		t.Error("expected memory writer to be called")
	}
}

func TestCouncil_NoGrant(t *testing.T) {
	t.Helper()
	eng := newEngine(t, nil, map[string]bool{})
	_, err := eng.Council(context.Background(), "caller-1", orchestrate.CouncilRequest{
		Question: "what should we do?",
	})
	if err == nil || !strings.Contains(err.Error(), "orchestrate grant") {
		t.Fatalf("expected grant error, got: %v", err)
	}
}

func TestCouncil_EmptyQuestion(t *testing.T) {
	t.Helper()
	eng := newEngine(t, nil, map[string]bool{"caller-1:orchestrate": true})
	_, err := eng.Council(context.Background(), "caller-1", orchestrate.CouncilRequest{})
	if err == nil || !strings.Contains(err.Error(), "question is required") {
		t.Fatalf("expected question required error, got: %v", err)
	}
}

func TestCouncil_SynthesisFormed(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 200, `{"result":"my opinion"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	res, err := eng.Council(context.Background(), "caller-1", orchestrate.CouncilRequest{
		Question: "what is 2+2?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Synthesis == "" {
		t.Error("expected non-empty synthesis")
	}
	if !strings.Contains(res.Synthesis, "my opinion") {
		t.Errorf("synthesis missing agent output: %q", res.Synthesis)
	}
}

func TestBroadcast_NoGrant(t *testing.T) {
	t.Helper()
	eng := newEngine(t, nil, map[string]bool{})
	err := eng.Broadcast(context.Background(), "caller-1", "hello", nil)
	if err == nil || !strings.Contains(err.Error(), "orchestrate grant") {
		t.Fatalf("expected grant error, got: %v", err)
	}
}

func TestBroadcast_EmptySignal(t *testing.T) {
	t.Helper()
	eng := newEngine(t, nil, map[string]bool{"caller-1:orchestrate": true})
	err := eng.Broadcast(context.Background(), "caller-1", "", nil)
	if err == nil || !strings.Contains(err.Error(), "signal is required") {
		t.Fatalf("expected signal required error, got: %v", err)
	}
}

func TestBroadcast_FireAndForget(t *testing.T) {
	t.Helper()
	var received int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{"caller-1:orchestrate": true})
	err := eng.Broadcast(context.Background(), "caller-1", "wake up", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCollect_NotFound(t *testing.T) {
	t.Helper()
	eng := newEngine(t, nil, nil)
	_, err := eng.Collect(context.Background(), "caller-1", "orch_nonexistent")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got: %v", err)
	}
}

func TestCollect_EmptyID(t *testing.T) {
	t.Helper()
	eng := newEngine(t, nil, nil)
	_, err := eng.Collect(context.Background(), "caller-1", "")
	if err == nil || !strings.Contains(err.Error(), "orchestration_id is required") {
		t.Fatalf("expected id required error, got: %v", err)
	}
}

func TestCollect_AfterOrchestrate(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 200, `{"result":"cached"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	orchRes, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "task",
		TargetAgents: []string{"a1"},
	})
	if err != nil {
		t.Fatalf("orchestrate error: %v", err)
	}

	collected, err := eng.Collect(context.Background(), "caller-1", orchRes.OrchestrationID)
	if err != nil {
		t.Fatalf("collect error: %v", err)
	}
	if collected.OrchestrationID != orchRes.OrchestrationID {
		t.Errorf("collect returned wrong orchestration_id")
	}
}

func TestIsTokenLimitBody_Phrases(t *testing.T) {
	t.Helper()
	// Verify token limit detection via end-to-end dispatch.
	phrases := []string{
		`{"error":"token limit reached"}`,
		`{"error":"context length exceeded"}`,
		`{"error":"context_length_exceeded"}`,
		`{"error":"maximum context length is 4096"}`,
	}
	for _, phrase := range phrases {
		srv := agentServer(t, 200, phrase)
		agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
			Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
		eng := newEngine(t, agents, map[string]bool{
			"caller-1:orchestrate": true,
			"caller-1:dispatch:a1": true,
		})
		res, _ := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
			Task:         "task",
			TargetAgents: []string{"a1"},
		})
		if res.Results[0].Status != "token_limit_error" {
			t.Errorf("phrase %q: expected token_limit_error, got %q", phrase, res.Results[0].Status)
		}
		srv.Close()
	}
}

func TestOrchestrate_AgentIDInResponse(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 200, `{"result":"ok"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "my-agent", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate":       true,
		"caller-1:dispatch:my-agent": true,
	})
	res, err := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "task",
		TargetAgents: []string{"my-agent"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Results[0].AgentID != "my-agent" {
		t.Errorf("expected AgentID=my-agent, got %q", res.Results[0].AgentID)
	}
}

func TestOrchestrate_LatencyPopulated(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 200, `{"result":"ok"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	res, _ := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "task",
		TargetAgents: []string{"a1"},
	})
	if res.Results[0].LatencyMs < 0 {
		t.Errorf("expected non-negative latency, got %d", res.Results[0].LatencyMs)
	}
}

func TestOrchestrate_ImmuneScanResult(t *testing.T) {
	t.Helper()
	srv := agentServer(t, 200, `{"result":"clean output"}`)
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	res, _ := eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "task",
		TargetAgents: []string{"a1"},
	})
	if res.Results[0].ScanResult.Action != "accept" {
		t.Errorf("expected immune scan accept, got %q", res.Results[0].ScanResult.Action)
	}
}

func TestOrchestrate_RequestPayloadSentAsJSON(t *testing.T) {
	t.Helper()
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer srv.Close()

	agents := []orchestrate.Agent{{AgentID: "a1", Status: "active",
		Orchestratable: true, ConnectionKind: "http", Endpoint: srv.URL}}
	eng := newEngine(t, agents, map[string]bool{
		"caller-1:orchestrate": true,
		"caller-1:dispatch:a1": true,
	})
	_, _ = eng.Orchestrate(context.Background(), "caller-1", orchestrate.OrchestrationRequest{
		Task:         "my task",
		TargetAgents: []string{"a1"},
	})
	if gotBody["task"] != "my task" {
		t.Errorf("dispatch body missing task field: %v", gotBody)
	}
}
