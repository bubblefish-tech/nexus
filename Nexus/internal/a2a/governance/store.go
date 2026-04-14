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

package governance

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// grantMigrations is the DDL for the governance tables.
var grantMigrations = []string{
	`CREATE TABLE IF NOT EXISTS a2a_grants (
		grant_id TEXT PRIMARY KEY,
		source_agent_id TEXT NOT NULL,
		target_agent_id TEXT NOT NULL,
		capability_glob TEXT NOT NULL,
		scope TEXT NOT NULL,
		decision TEXT NOT NULL,
		expires_at_ms INTEGER,
		issued_by TEXT NOT NULL,
		issued_at_ms INTEGER NOT NULL,
		revoked_at_ms INTEGER,
		notes TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS idx_a2a_grants_st ON a2a_grants(source_agent_id, target_agent_id)`,

	`CREATE TABLE IF NOT EXISTS a2a_pending_approvals (
		approval_id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		source_agent_id TEXT NOT NULL,
		target_agent_id TEXT NOT NULL,
		skill TEXT NOT NULL,
		required_capabilities TEXT NOT NULL,
		input_preview TEXT,
		created_at_ms INTEGER NOT NULL,
		resolved_at_ms INTEGER,
		resolved_by TEXT,
		resolution TEXT
	)`,
}

// MigrateGrants runs the DDL for governance tables on the given database.
func MigrateGrants(db *sql.DB) error {
	for _, stmt := range grantMigrations {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("governance: migrate: %w", err)
		}
	}
	return nil
}

// GrantStore provides CRUD operations for grants and pending approvals.
type GrantStore struct {
	db *sql.DB
}

// NewGrantStore creates a GrantStore backed by the given database.
// The caller must ensure MigrateGrants has been called on db.
func NewGrantStore(db *sql.DB) *GrantStore {
	return &GrantStore{db: db}
}

// CreateGrant persists a new grant.
func (s *GrantStore) CreateGrant(g *Grant) error {
	var expiresMs *int64
	if g.ExpiresAt != nil {
		ms := g.ExpiresAt.UnixMilli()
		expiresMs = &ms
	}

	_, err := s.db.Exec(
		`INSERT INTO a2a_grants
			(grant_id, source_agent_id, target_agent_id, capability_glob,
			 scope, decision, expires_at_ms, issued_by, issued_at_ms, notes)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.GrantID, g.SourceAgentID, g.TargetAgentID, g.CapabilityGlob,
		g.Scope, g.Decision, expiresMs, g.IssuedBy, g.IssuedAt.UnixMilli(), g.Notes,
	)
	if err != nil {
		return fmt.Errorf("governance: create grant: %w", err)
	}
	return nil
}

// GetGrant retrieves a grant by ID.
func (s *GrantStore) GetGrant(grantID string) (*Grant, error) {
	var (
		g         Grant
		expiresMs *int64
		revokedMs *int64
		issuedMs  int64
	)
	err := s.db.QueryRow(
		`SELECT grant_id, source_agent_id, target_agent_id, capability_glob,
		        scope, decision, expires_at_ms, issued_by, issued_at_ms,
		        revoked_at_ms, notes
		 FROM a2a_grants WHERE grant_id = ?`, grantID,
	).Scan(&g.GrantID, &g.SourceAgentID, &g.TargetAgentID, &g.CapabilityGlob,
		&g.Scope, &g.Decision, &expiresMs, &g.IssuedBy, &issuedMs,
		&revokedMs, &g.Notes)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("governance: grant %q not found", grantID)
	}
	if err != nil {
		return nil, fmt.Errorf("governance: get grant: %w", err)
	}

	g.IssuedAt = time.UnixMilli(issuedMs).UTC()
	if expiresMs != nil {
		t := time.UnixMilli(*expiresMs).UTC()
		g.ExpiresAt = &t
	}
	if revokedMs != nil {
		t := time.UnixMilli(*revokedMs).UTC()
		g.RevokedAt = &t
	}
	return &g, nil
}

// ListGrants returns all grants.
func (s *GrantStore) ListGrants() ([]*Grant, error) {
	rows, err := s.db.Query(
		`SELECT grant_id, source_agent_id, target_agent_id, capability_glob,
		        scope, decision, expires_at_ms, issued_by, issued_at_ms,
		        revoked_at_ms, notes
		 FROM a2a_grants ORDER BY issued_at_ms DESC`)
	if err != nil {
		return nil, fmt.Errorf("governance: list grants: %w", err)
	}
	defer rows.Close()

	return scanGrants(rows)
}

// RevokeGrant marks a grant as revoked at the given time.
func (s *GrantStore) RevokeGrant(grantID string, at time.Time) error {
	res, err := s.db.Exec(
		`UPDATE a2a_grants SET revoked_at_ms = ? WHERE grant_id = ? AND revoked_at_ms IS NULL`,
		at.UnixMilli(), grantID,
	)
	if err != nil {
		return fmt.Errorf("governance: revoke grant: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("governance: grant %q not found or already revoked", grantID)
	}
	return nil
}

// FindMatchingGrants returns all active (non-expired, non-revoked) grants
// for the given source and target agent pair.
func (s *GrantStore) FindMatchingGrants(source, target string) ([]*Grant, error) {
	rows, err := s.db.Query(
		`SELECT grant_id, source_agent_id, target_agent_id, capability_glob,
		        scope, decision, expires_at_ms, issued_by, issued_at_ms,
		        revoked_at_ms, notes
		 FROM a2a_grants
		 WHERE source_agent_id = ? AND target_agent_id = ?
		   AND revoked_at_ms IS NULL
		 ORDER BY issued_at_ms DESC`,
		source, target,
	)
	if err != nil {
		return nil, fmt.Errorf("governance: find grants: %w", err)
	}
	defer rows.Close()

	return scanGrants(rows)
}

// scanGrants scans rows into a slice of Grant pointers.
func scanGrants(rows *sql.Rows) ([]*Grant, error) {
	var grants []*Grant
	for rows.Next() {
		var (
			g         Grant
			expiresMs *int64
			revokedMs *int64
			issuedMs  int64
		)
		if err := rows.Scan(
			&g.GrantID, &g.SourceAgentID, &g.TargetAgentID, &g.CapabilityGlob,
			&g.Scope, &g.Decision, &expiresMs, &g.IssuedBy, &issuedMs,
			&revokedMs, &g.Notes,
		); err != nil {
			return nil, fmt.Errorf("governance: scan grant: %w", err)
		}
		g.IssuedAt = time.UnixMilli(issuedMs).UTC()
		if expiresMs != nil {
			t := time.UnixMilli(*expiresMs).UTC()
			g.ExpiresAt = &t
		}
		if revokedMs != nil {
			t := time.UnixMilli(*revokedMs).UTC()
			g.RevokedAt = &t
		}
		grants = append(grants, &g)
	}
	return grants, rows.Err()
}

// CreateApproval persists a new pending approval.
func (s *GrantStore) CreateApproval(a *PendingApproval) error {
	caps := strings.Join(a.RequiredCapabilities, ",")
	_, err := s.db.Exec(
		`INSERT INTO a2a_pending_approvals
			(approval_id, task_id, source_agent_id, target_agent_id, skill,
			 required_capabilities, input_preview, created_at_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ApprovalID, a.TaskID, a.SourceAgentID, a.TargetAgentID, a.Skill,
		caps, a.InputPreview, a.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("governance: create approval: %w", err)
	}
	return nil
}

// GetApproval retrieves a pending approval by ID.
func (s *GrantStore) GetApproval(approvalID string) (*PendingApproval, error) {
	var (
		a          PendingApproval
		caps       string
		createdMs  int64
		resolvedMs *int64
		resolvedBy *string
		resolution *string
	)
	err := s.db.QueryRow(
		`SELECT approval_id, task_id, source_agent_id, target_agent_id, skill,
		        required_capabilities, input_preview, created_at_ms,
		        resolved_at_ms, resolved_by, resolution
		 FROM a2a_pending_approvals WHERE approval_id = ?`, approvalID,
	).Scan(&a.ApprovalID, &a.TaskID, &a.SourceAgentID, &a.TargetAgentID, &a.Skill,
		&caps, &a.InputPreview, &createdMs, &resolvedMs, &resolvedBy, &resolution)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("governance: approval %q not found", approvalID)
	}
	if err != nil {
		return nil, fmt.Errorf("governance: get approval: %w", err)
	}

	a.CreatedAt = time.UnixMilli(createdMs).UTC()
	if resolvedMs != nil {
		t := time.UnixMilli(*resolvedMs).UTC()
		a.ResolvedAt = &t
	}
	if resolvedBy != nil {
		a.ResolvedBy = *resolvedBy
	}
	if resolution != nil {
		a.Resolution = *resolution
	}
	if caps != "" {
		a.RequiredCapabilities = strings.Split(caps, ",")
	}
	return &a, nil
}

// ListPendingApprovals returns all unresolved approvals.
func (s *GrantStore) ListPendingApprovals() ([]*PendingApproval, error) {
	rows, err := s.db.Query(
		`SELECT approval_id, task_id, source_agent_id, target_agent_id, skill,
		        required_capabilities, input_preview, created_at_ms,
		        resolved_at_ms, resolved_by, resolution
		 FROM a2a_pending_approvals
		 WHERE resolution IS NULL
		 ORDER BY created_at_ms DESC`)
	if err != nil {
		return nil, fmt.Errorf("governance: list approvals: %w", err)
	}
	defer rows.Close()

	return scanApprovals(rows)
}

// ResolveApproval marks an approval as resolved.
func (s *GrantStore) ResolveApproval(approvalID string, resolvedBy string, resolution string, at time.Time) error {
	res, err := s.db.Exec(
		`UPDATE a2a_pending_approvals
		 SET resolved_at_ms = ?, resolved_by = ?, resolution = ?
		 WHERE approval_id = ? AND resolution IS NULL`,
		at.UnixMilli(), resolvedBy, resolution, approvalID,
	)
	if err != nil {
		return fmt.Errorf("governance: resolve approval: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("governance: approval %q not found or already resolved", approvalID)
	}
	return nil
}

// scanApprovals scans rows into a slice of PendingApproval pointers.
func scanApprovals(rows *sql.Rows) ([]*PendingApproval, error) {
	var approvals []*PendingApproval
	for rows.Next() {
		var (
			a          PendingApproval
			caps       string
			createdMs  int64
			resolvedMs *int64
			resolvedBy *string
			resolution *string
		)
		if err := rows.Scan(
			&a.ApprovalID, &a.TaskID, &a.SourceAgentID, &a.TargetAgentID, &a.Skill,
			&caps, &a.InputPreview, &createdMs, &resolvedMs, &resolvedBy, &resolution,
		); err != nil {
			return nil, fmt.Errorf("governance: scan approval: %w", err)
		}
		a.CreatedAt = time.UnixMilli(createdMs).UTC()
		if resolvedMs != nil {
			t := time.UnixMilli(*resolvedMs).UTC()
			a.ResolvedAt = &t
		}
		if resolvedBy != nil {
			a.ResolvedBy = *resolvedBy
		}
		if resolution != nil {
			a.Resolution = *resolution
		}
		if caps != "" {
			a.RequiredCapabilities = strings.Split(caps, ",")
		}
		approvals = append(approvals, &a)
	}
	return approvals, rows.Err()
}
