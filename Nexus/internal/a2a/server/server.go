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

// Package server implements the NA2A JSON-RPC method handlers.
package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
)

// contextKey is an unexported type used for context keys to avoid collisions.
type contextKey string

const (
	// CtxKeyAdmin is the context key indicating whether the caller is an admin.
	CtxKeyAdmin contextKey = "admin"

	// CtxKeySourceAgent is the context key for the source agent ID.
	CtxKeySourceAgent contextKey = "source_agent_id"
)

// TaskStore persists task state. Implemented in A2A.4.
type TaskStore interface {
	CreateTask(ctx context.Context, task *a2a.Task) error
	GetTask(ctx context.Context, taskID string) (*a2a.Task, error)
	UpdateTaskStatus(ctx context.Context, taskID string, status a2a.TaskStatus) error
	AddArtifact(ctx context.Context, taskID string, artifact a2a.Artifact) error
	AddHistory(ctx context.Context, taskID string, msg a2a.Message) error
	ListTasks(ctx context.Context, filter TaskFilter) ([]*a2a.Task, error)
}

// TaskFilter describes criteria for listing tasks.
type TaskFilter struct {
	SourceAgentID string
	TargetAgentID string
	State         a2a.TaskState
	Since         time.Time
	Limit         int
}

// GovernanceEngine decides whether a task is allowed. Implemented in A2A.5.
type GovernanceEngine interface {
	Decide(ctx context.Context, req GovernanceReq) (*GovernanceResult, error)
}

// GovernanceReq is the input to a governance decision.
type GovernanceReq struct {
	SourceAgentID        string
	TargetAgentID        string
	Skill                string
	RequiredCapabilities []string
	Destructive          bool
}

// GovernanceResult is the output of a governance decision.
type GovernanceResult struct {
	Decision string // "allow", "deny", "escalate"
	GrantID  string
	Reason   string
	AuditID  string
}

// SkillRegistry resolves skills by name.
type SkillRegistry interface {
	GetSkill(name string) (*a2a.Skill, bool)
	ListSkills() []a2a.Skill
}

// SkillExecutor executes a skill. The Nexus daemon registers one per exposed skill.
type SkillExecutor interface {
	Execute(ctx context.Context, skill string, input *a2a.DataPart, files []a2a.FilePart) (*a2a.DataPart, []a2a.FilePart, error)
}

// AuditSink records audit events.
type AuditSink interface {
	LogTaskEvent(ctx context.Context, taskID string, eventType string, data interface{}) error
}

// PushNotifier sends push notifications.
type PushNotifier interface {
	Notify(ctx context.Context, taskID string, event interface{}) error
}

// PushNotificationConfig holds push notification configuration for a task.
type PushNotificationConfig struct {
	URL            string `json:"url"`
	Token          string `json:"token,omitempty"`
	Authentication string `json:"authentication,omitempty"`
}

// Server is the NA2A JSON-RPC server. It dispatches method calls to
// the appropriate handler using the internal MethodRouter.
type Server struct {
	agentCard     a2a.AgentCard
	skillRegistry SkillRegistry
	skillExecutor SkillExecutor
	taskStore     TaskStore
	governance    GovernanceEngine
	auditSink     AuditSink
	pushNotifier  PushNotifier
	clientPool    ClientPool
	router        *jsonrpc.MethodRouter
	logger        *slog.Logger

	// pushConfigs stores push notification configs keyed by taskID.
	// In production this would be in the task store; here it is in-memory.
	pushConfigs map[string]PushNotificationConfig
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithTaskStore sets the task store.
func WithTaskStore(ts TaskStore) ServerOption {
	return func(s *Server) { s.taskStore = ts }
}

// WithGovernanceEngine sets the governance engine.
func WithGovernanceEngine(ge GovernanceEngine) ServerOption {
	return func(s *Server) { s.governance = ge }
}

// WithSkillRegistry sets the skill registry.
func WithSkillRegistry(sr SkillRegistry) ServerOption {
	return func(s *Server) { s.skillRegistry = sr }
}

// WithSkillExecutor sets the skill executor.
func WithSkillExecutor(se SkillExecutor) ServerOption {
	return func(s *Server) { s.skillExecutor = se }
}

// WithAuditSink sets the audit sink.
func WithAuditSink(as AuditSink) ServerOption {
	return func(s *Server) { s.auditSink = as }
}

// WithPushNotifier sets the push notifier.
func WithPushNotifier(pn PushNotifier) ServerOption {
	return func(s *Server) { s.pushNotifier = pn }
}

// WithLogger sets the logger.
func WithLogger(l *slog.Logger) ServerOption {
	return func(s *Server) { s.logger = l }
}

// NewServer creates a new NA2A Server with the given agent card and options.
func NewServer(card a2a.AgentCard, opts ...ServerOption) *Server {
	s := &Server{
		agentCard:   card,
		router:      jsonrpc.NewMethodRouter(),
		logger:      slog.Default(),
		pushConfigs: make(map[string]PushNotificationConfig),
	}

	for _, opt := range opts {
		opt(s)
	}

	// Register all method handlers.
	s.router.RegisterFunc("message/send", jsonrpc.HandlerFunc(s.handleMessageSend))
	s.router.RegisterFunc("message/stream", jsonrpc.HandlerFunc(s.handleMessageStream))

	s.router.RegisterFunc("tasks/get", jsonrpc.HandlerFunc(s.handleTasksGet))
	s.router.RegisterFunc("tasks/cancel", jsonrpc.HandlerFunc(s.handleTasksCancel))
	s.router.RegisterFunc("tasks/resubscribe", jsonrpc.HandlerFunc(s.handleTasksResubscribe))
	s.router.RegisterFunc("tasks/pushNotificationConfig/set", jsonrpc.HandlerFunc(s.handleTasksPushNotificationConfigSet))
	s.router.RegisterFunc("tasks/pushNotificationConfig/get", jsonrpc.HandlerFunc(s.handleTasksPushNotificationConfigGet))

	s.router.RegisterFunc("agent/card", jsonrpc.HandlerFunc(s.handleAgentCard))
	s.router.RegisterFunc("agent/ping", jsonrpc.HandlerFunc(s.handleAgentPing))
	s.router.RegisterFunc("agent/invoke", jsonrpc.HandlerFunc(s.handleAgentInvoke))

	s.router.RegisterFunc("governance/grants/list", jsonrpc.HandlerFunc(s.handleGovernanceGrantsList))
	s.router.RegisterFunc("governance/grants/create", jsonrpc.HandlerFunc(s.handleGovernanceGrantsCreate))
	s.router.RegisterFunc("governance/grants/revoke", jsonrpc.HandlerFunc(s.handleGovernanceGrantsRevoke))
	s.router.RegisterFunc("governance/approvals/list", jsonrpc.HandlerFunc(s.handleGovernanceApprovalsList))
	s.router.RegisterFunc("governance/approvals/decide", jsonrpc.HandlerFunc(s.handleGovernanceApprovalsDecide))
	s.router.RegisterFunc("governance/audit/query", jsonrpc.HandlerFunc(s.handleGovernanceAuditQuery))

	return s
}

// Router returns the internal MethodRouter for use by transport layers.
func (s *Server) Router() *jsonrpc.MethodRouter {
	return s.router
}

// Dispatch dispatches a single JSON-RPC request.
func (s *Server) Dispatch(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
	return s.router.Dispatch(ctx, req)
}
