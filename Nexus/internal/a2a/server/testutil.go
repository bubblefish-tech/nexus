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
	"fmt"
	"log/slog"
	"sync"

	"github.com/bubblefish-tech/nexus/internal/a2a"
)

// --- FakeTaskStore ---

// FakeTaskStore is an in-memory TaskStore for testing.
type FakeTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*a2a.Task
}

// NewFakeTaskStore creates a new in-memory FakeTaskStore.
func NewFakeTaskStore() *FakeTaskStore {
	return &FakeTaskStore{tasks: make(map[string]*a2a.Task)}
}

// CreateTask implements TaskStore.
func (s *FakeTaskStore) CreateTask(_ context.Context, task *a2a.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *task
	s.tasks[task.TaskID] = &cp
	return nil
}

// GetTask implements TaskStore.
func (s *FakeTaskStore) GetTask(_ context.Context, taskID string) (*a2a.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return nil, fmt.Errorf("task %q not found", taskID)
	}
	cp := *t
	return &cp, nil
}

// UpdateTaskStatus implements TaskStore.
func (s *FakeTaskStore) UpdateTaskStatus(_ context.Context, taskID string, status a2a.TaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}
	t.Status = status
	return nil
}

// AddArtifact implements TaskStore.
func (s *FakeTaskStore) AddArtifact(_ context.Context, taskID string, artifact a2a.Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}
	t.Artifacts = append(t.Artifacts, artifact)
	return nil
}

// AddHistory implements TaskStore.
func (s *FakeTaskStore) AddHistory(_ context.Context, taskID string, msg a2a.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[taskID]
	if !ok {
		return fmt.Errorf("task %q not found", taskID)
	}
	t.History = append(t.History, msg)
	return nil
}

// ListTasks implements TaskStore.
func (s *FakeTaskStore) ListTasks(_ context.Context, filter TaskFilter) ([]*a2a.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*a2a.Task
	for _, t := range s.tasks {
		if filter.State != "" && t.Status.State != filter.State {
			continue
		}
		cp := *t
		result = append(result, &cp)
		if filter.Limit > 0 && len(result) >= filter.Limit {
			break
		}
	}
	return result, nil
}

// --- FakeGovernance ---

// FakeGovernance is an always-allow governance engine for testing.
// Set DenyAll or EscalateAll to change behavior.
type FakeGovernance struct {
	mu             sync.RWMutex
	DenyAll        bool
	DenyReason     string
	EscalateAll    bool
	EscalateReason string
	grants         []Grant
	approvals      []Approval
	auditLog       []AuditEntry
}

// NewFakeGovernance creates a FakeGovernance that allows everything.
func NewFakeGovernance() *FakeGovernance {
	return &FakeGovernance{
		grants:    make([]Grant, 0),
		approvals: make([]Approval, 0),
		auditLog:  make([]AuditEntry, 0),
	}
}

// Decide implements GovernanceEngine.
func (g *FakeGovernance) Decide(_ context.Context, _ GovernanceReq) (*GovernanceResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.DenyAll {
		return &GovernanceResult{
			Decision: "deny",
			Reason:   g.DenyReason,
			AuditID:  a2a.NewAuditID(),
		}, nil
	}
	if g.EscalateAll {
		return &GovernanceResult{
			Decision: "escalate",
			Reason:   g.EscalateReason,
			AuditID:  a2a.NewAuditID(),
		}, nil
	}
	return &GovernanceResult{
		Decision: "allow",
		GrantID:  a2a.NewGrantID(),
		AuditID:  a2a.NewAuditID(),
	}, nil
}

// ListGrants implements GrantsListEngine.
func (g *FakeGovernance) ListGrants(_ context.Context) ([]Grant, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Grant, len(g.grants))
	copy(out, g.grants)
	return out, nil
}

// CreateGrant implements GrantsCreateEngine.
func (g *FakeGovernance) CreateGrant(_ context.Context, grant Grant) (*Grant, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.grants = append(g.grants, grant)
	return &grant, nil
}

// RevokeGrant implements GrantsRevokeEngine.
func (g *FakeGovernance) RevokeGrant(_ context.Context, grantID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, gr := range g.grants {
		if gr.GrantID == grantID {
			g.grants = append(g.grants[:i], g.grants[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("grant %q not found", grantID)
}

// ListApprovals implements ApprovalsListEngine.
func (g *FakeGovernance) ListApprovals(_ context.Context) ([]Approval, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Approval, len(g.approvals))
	copy(out, g.approvals)
	return out, nil
}

// DecideApproval implements ApprovalsDecideEngine.
func (g *FakeGovernance) DecideApproval(_ context.Context, approvalID string, _ string, _ string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, ap := range g.approvals {
		if ap.ApprovalID == approvalID {
			g.approvals = append(g.approvals[:i], g.approvals[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("approval %q not found", approvalID)
}

// QueryAudit implements AuditQueryEngine.
func (g *FakeGovernance) QueryAudit(_ context.Context, taskID string, eventType string, limit int) ([]AuditEntry, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var result []AuditEntry
	for _, e := range g.auditLog {
		if taskID != "" && e.TaskID != taskID {
			continue
		}
		if eventType != "" && e.EventType != eventType {
			continue
		}
		result = append(result, e)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

// AddAuditEntry adds an audit entry for testing.
func (g *FakeGovernance) AddAuditEntry(entry AuditEntry) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.auditLog = append(g.auditLog, entry)
}

// AddApproval adds a pending approval for testing.
func (g *FakeGovernance) AddApproval(approval Approval) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.approvals = append(g.approvals, approval)
}

// --- FakeSkillExecutor ---

// NewFakeSkillExecutor creates a SkillExecutor backed by a map of functions.
func NewFakeSkillExecutor(skills map[string]SkillFunc) *InMemorySkillExecutor {
	return NewInMemorySkillExecutor(skills)
}

// --- FakeAuditSink ---

// FakeAuditSink records audit events in memory for testing.
type FakeAuditSink struct {
	mu     sync.Mutex
	events []AuditEvent
}

// AuditEvent records a single audit event for testing.
type AuditEvent struct {
	TaskID    string
	EventType string
	Data      interface{}
}

// NewFakeAuditSink creates a new FakeAuditSink.
func NewFakeAuditSink() *FakeAuditSink {
	return &FakeAuditSink{}
}

// LogTaskEvent implements AuditSink.
func (s *FakeAuditSink) LogTaskEvent(_ context.Context, taskID string, eventType string, data interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, AuditEvent{TaskID: taskID, EventType: eventType, Data: data})
	return nil
}

// Events returns a copy of all recorded events.
func (s *FakeAuditSink) Events() []AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuditEvent, len(s.events))
	copy(out, s.events)
	return out
}

// --- FakeSkillRegistry ---

// FakeSkillRegistry is an in-memory SkillRegistry for testing.
type FakeSkillRegistry struct {
	mu     sync.RWMutex
	skills map[string]a2a.Skill
}

// NewFakeSkillRegistry creates a FakeSkillRegistry with the given skills.
func NewFakeSkillRegistry(skills ...a2a.Skill) *FakeSkillRegistry {
	m := make(map[string]a2a.Skill, len(skills))
	for _, sk := range skills {
		m[sk.ID] = sk
	}
	return &FakeSkillRegistry{skills: m}
}

// GetSkill implements SkillRegistry.
func (r *FakeSkillRegistry) GetSkill(name string) (*a2a.Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sk, ok := r.skills[name]
	if !ok {
		return nil, false
	}
	return &sk, true
}

// ListSkills implements SkillRegistry.
func (r *FakeSkillRegistry) ListSkills() []a2a.Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]a2a.Skill, 0, len(r.skills))
	for _, sk := range r.skills {
		out = append(out, sk)
	}
	return out
}

// --- FakePushNotifier ---

// FakePushNotifier is a no-op PushNotifier for testing.
type FakePushNotifier struct {
	mu            sync.Mutex
	notifications []PushNotification
}

// PushNotification records a push notification for testing.
type PushNotification struct {
	TaskID string
	Event  interface{}
}

// NewFakePushNotifier creates a new FakePushNotifier.
func NewFakePushNotifier() *FakePushNotifier {
	return &FakePushNotifier{}
}

// Notify implements PushNotifier.
func (n *FakePushNotifier) Notify(_ context.Context, taskID string, event interface{}) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notifications = append(n.notifications, PushNotification{TaskID: taskID, Event: event})
	return nil
}

// --- NewTestServer ---

// TestServerDeps holds the fake dependencies for a test server.
type TestServerDeps struct {
	TaskStore     *FakeTaskStore
	Governance    *FakeGovernance
	SkillRegistry *FakeSkillRegistry
	SkillExecutor *InMemorySkillExecutor
	AuditSink     *FakeAuditSink
	PushNotifier  *FakePushNotifier
	Server        *Server
}

// NewTestServer constructs a Server with all fakes wired up.
// It registers default "echo" and "dangerous" skills.
func NewTestServer() *TestServerDeps {
	echoSkill := a2a.Skill{
		ID:          "echo",
		Name:        "echo",
		Description: "Echoes input back",
	}
	destructiveSkill := a2a.Skill{
		ID:                   "dangerous",
		Name:                 "dangerous",
		Description:          "A destructive skill for testing",
		Destructive:          true,
		RequiredCapabilities: []string{"shell.exec"},
	}

	registry := NewFakeSkillRegistry(echoSkill, destructiveSkill)

	executor := NewFakeSkillExecutor(map[string]SkillFunc{
		"echo": func(_ context.Context, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
			return input, files, nil
		},
		"dangerous": func(_ context.Context, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error) {
			return input, files, nil
		},
	})

	store := NewFakeTaskStore()
	gov := NewFakeGovernance()
	audit := NewFakeAuditSink()
	push := NewFakePushNotifier()

	card := a2a.AgentCard{
		Name:            "test-agent",
		URL:             "http://localhost:9999",
		ProtocolVersion: a2a.ProtocolVersion,
		Version:         a2a.ImplementationVersion(),
		Implementation:  a2a.ImplementationName,
		Endpoints: []a2a.Endpoint{
			{URL: "http://localhost:9999/a2a", Transport: a2a.TransportJSONRPC},
		},
		Capabilities: a2a.AgentCapabilities{
			Streaming:         true,
			PushNotifications: true,
			StateTransitions:  true,
		},
		Skills: []a2a.Skill{echoSkill, destructiveSkill},
	}

	srv := NewServer(card,
		WithTaskStore(store),
		WithGovernanceEngine(gov),
		WithSkillRegistry(registry),
		WithSkillExecutor(executor),
		WithAuditSink(audit),
		WithPushNotifier(push),
		WithLogger(slog.Default()),
	)

	return &TestServerDeps{
		TaskStore:     store,
		Governance:    gov,
		SkillRegistry: registry,
		SkillExecutor: executor,
		AuditSink:     audit,
		PushNotifier:  push,
		Server:        srv,
	}
}
