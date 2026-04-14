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

	"github.com/BubbleFish-Nexus/internal/a2a"
	"github.com/BubbleFish-Nexus/internal/a2a/jsonrpc"
)

// requireAdmin checks that the context has the admin key set.
func requireAdmin(ctx context.Context) *jsonrpc.ErrorObject {
	admin, _ := ctx.Value(CtxKeyAdmin).(bool)
	if !admin {
		return &jsonrpc.ErrorObject{
			Code:    a2a.CodePermissionDenied,
			Message: "admin access required",
		}
	}
	return nil
}

// Grant is a governance grant record.
type Grant struct {
	GrantID              string   `json:"grantId"`
	SourceAgentID        string   `json:"sourceAgentId"`
	TargetAgentID        string   `json:"targetAgentId"`
	Skill                string   `json:"skill,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
	Decision             string   `json:"decision"`
	Reason               string   `json:"reason,omitempty"`
	CreatedAt            string   `json:"createdAt"`
}

// Approval is a pending governance approval request.
type Approval struct {
	ApprovalID    string `json:"approvalId"`
	SourceAgentID string `json:"sourceAgentId"`
	TargetAgentID string `json:"targetAgentId"`
	Skill         string `json:"skill"`
	Reason        string `json:"reason,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// AuditEntry is a governance audit log entry.
type AuditEntry struct {
	AuditID   string      `json:"auditId"`
	TaskID    string      `json:"taskId"`
	EventType string      `json:"eventType"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp string      `json:"timestamp"`
}

// GrantsListEngine extends GovernanceEngine for listing grants.
type GrantsListEngine interface {
	ListGrants(ctx context.Context) ([]Grant, error)
}

// GrantsCreateEngine extends GovernanceEngine for creating grants.
type GrantsCreateEngine interface {
	CreateGrant(ctx context.Context, grant Grant) (*Grant, error)
}

// GrantsRevokeEngine extends GovernanceEngine for revoking grants.
type GrantsRevokeEngine interface {
	RevokeGrant(ctx context.Context, grantID string) error
}

// ApprovalsListEngine extends GovernanceEngine for listing approvals.
type ApprovalsListEngine interface {
	ListApprovals(ctx context.Context) ([]Approval, error)
}

// ApprovalsDecideEngine extends GovernanceEngine for deciding approvals.
type ApprovalsDecideEngine interface {
	DecideApproval(ctx context.Context, approvalID string, decision string, reason string) error
}

// AuditQueryEngine extends GovernanceEngine for querying audit events.
type AuditQueryEngine interface {
	QueryAudit(ctx context.Context, taskID string, eventType string, limit int) ([]AuditEntry, error)
}

// grantsListParams is the parameter object for governance/grants/list.
type grantsListParams struct {
	SourceAgentID string `json:"sourceAgentId,omitempty"`
	TargetAgentID string `json:"targetAgentId,omitempty"`
}

// grantCreateParams is the parameter object for governance/grants/create.
type grantCreateParams struct {
	SourceAgentID        string   `json:"sourceAgentId"`
	TargetAgentID        string   `json:"targetAgentId"`
	Skill                string   `json:"skill,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
	Decision             string   `json:"decision"`
	Reason               string   `json:"reason,omitempty"`
}

// grantRevokeParams is the parameter object for governance/grants/revoke.
type grantRevokeParams struct {
	GrantID string `json:"grantId"`
}

// approvalDecideParams is the parameter object for governance/approvals/decide.
type approvalDecideParams struct {
	ApprovalID string `json:"approvalId"`
	Decision   string `json:"decision"`
	Reason     string `json:"reason,omitempty"`
}

// auditQueryParams is the parameter object for governance/audit/query.
type auditQueryParams struct {
	TaskID    string `json:"taskId,omitempty"`
	EventType string `json:"eventType,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

// handleGovernanceGrantsList lists governance grants.
func (s *Server) handleGovernanceGrantsList(ctx context.Context, _ string, _ json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	if errObj := requireAdmin(ctx); errObj != nil {
		return nil, errObj
	}

	if s.governance == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no governance engine configured",
		}
	}

	lister, ok := s.governance.(GrantsListEngine)
	if !ok {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "governance engine does not support listing grants",
		}
	}

	grants, err := lister.ListGrants(ctx)
	if err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: fmt.Sprintf("failed to list grants: %v", err),
		}
	}

	return grants, nil
}

// handleGovernanceGrantsCreate creates a governance grant.
func (s *Server) handleGovernanceGrantsCreate(ctx context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	if errObj := requireAdmin(ctx); errObj != nil {
		return nil, errObj
	}

	var p grantCreateParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if p.SourceAgentID == "" || p.TargetAgentID == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "sourceAgentId and targetAgentId are required",
		}
	}

	if s.governance == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no governance engine configured",
		}
	}

	creator, ok := s.governance.(GrantsCreateEngine)
	if !ok {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "governance engine does not support creating grants",
		}
	}

	grant := Grant{
		GrantID:              a2a.NewGrantID(),
		SourceAgentID:        p.SourceAgentID,
		TargetAgentID:        p.TargetAgentID,
		Skill:                p.Skill,
		RequiredCapabilities: p.RequiredCapabilities,
		Decision:             p.Decision,
		Reason:               p.Reason,
		CreatedAt:            a2a.Now(),
	}

	created, err := creator.CreateGrant(ctx, grant)
	if err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: fmt.Sprintf("failed to create grant: %v", err),
		}
	}

	return created, nil
}

// handleGovernanceGrantsRevoke revokes a governance grant.
func (s *Server) handleGovernanceGrantsRevoke(ctx context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	if errObj := requireAdmin(ctx); errObj != nil {
		return nil, errObj
	}

	var p grantRevokeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if p.GrantID == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "grantId is required",
		}
	}

	if s.governance == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no governance engine configured",
		}
	}

	revoker, ok := s.governance.(GrantsRevokeEngine)
	if !ok {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "governance engine does not support revoking grants",
		}
	}

	if err := revoker.RevokeGrant(ctx, p.GrantID); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: fmt.Sprintf("failed to revoke grant: %v", err),
		}
	}

	return map[string]bool{"ok": true}, nil
}

// handleGovernanceApprovalsList lists pending approvals.
func (s *Server) handleGovernanceApprovalsList(ctx context.Context, _ string, _ json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	if errObj := requireAdmin(ctx); errObj != nil {
		return nil, errObj
	}

	if s.governance == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no governance engine configured",
		}
	}

	lister, ok := s.governance.(ApprovalsListEngine)
	if !ok {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "governance engine does not support listing approvals",
		}
	}

	approvals, err := lister.ListApprovals(ctx)
	if err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: fmt.Sprintf("failed to list approvals: %v", err),
		}
	}

	return approvals, nil
}

// handleGovernanceApprovalsDecide decides on a pending approval.
func (s *Server) handleGovernanceApprovalsDecide(ctx context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	if errObj := requireAdmin(ctx); errObj != nil {
		return nil, errObj
	}

	var p approvalDecideParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if p.ApprovalID == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "approvalId is required",
		}
	}

	if p.Decision == "" {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: "decision is required",
		}
	}

	if s.governance == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no governance engine configured",
		}
	}

	decider, ok := s.governance.(ApprovalsDecideEngine)
	if !ok {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "governance engine does not support deciding approvals",
		}
	}

	if err := decider.DecideApproval(ctx, p.ApprovalID, p.Decision, p.Reason); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: fmt.Sprintf("failed to decide approval: %v", err),
		}
	}

	return map[string]bool{"ok": true}, nil
}

// handleGovernanceAuditQuery queries audit events.
func (s *Server) handleGovernanceAuditQuery(ctx context.Context, _ string, params json.RawMessage) (interface{}, *jsonrpc.ErrorObject) {
	if errObj := requireAdmin(ctx); errObj != nil {
		return nil, errObj
	}

	var p auditQueryParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInvalidParams,
			Message: fmt.Sprintf("invalid params: %v", err),
		}
	}

	if s.governance == nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "no governance engine configured",
		}
	}

	querier, ok := s.governance.(AuditQueryEngine)
	if !ok {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: "governance engine does not support audit queries",
		}
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 100
	}

	entries, err := querier.QueryAudit(ctx, p.TaskID, p.EventType, limit)
	if err != nil {
		return nil, &jsonrpc.ErrorObject{
			Code:    a2a.CodeInternalError,
			Message: fmt.Sprintf("failed to query audit: %v", err),
		}
	}

	return entries, nil
}
