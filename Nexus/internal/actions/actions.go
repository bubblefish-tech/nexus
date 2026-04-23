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

// Package actions is the append-only audit log of every policy decision
// recorded by the control plane. Each row captures the agent, capability,
// target, the grant and approval references that authorized (or the policy
// decision that denied) the action, and an optional audit_hash linking into
// the broader Phase 4 provenance chain. MT.1 is storage-only; MT.3 populates
// the policy_decision/reason fields.
package actions

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

const rowInfo = "actions-row"

// IDPrefix is the identifier prefix for action log IDs.
const IDPrefix = "act_"

// PolicyDecision values. The full set of decisions is the responsibility of
// MT.3's policy engine; these are the canonical short-forms recorded in the
// log.
const (
	DecisionAllow = "allow"
	DecisionDeny  = "deny"
	DecisionError = "error"
)

// ErrNotFound is returned when an action_id does not exist.
var ErrNotFound = errors.New("actions: action not found")

// Action is one row of the action log. GrantID and ApprovalID are empty when
// the decision was made without a grant or approval respectively (e.g. a deny
// decision, or a built-in admin path). AuditHash is populated when the row is
// linked into the Phase 4 audit chain (MT.7).
type Action struct {
	ActionID       string
	AgentID        string
	Capability     string
	Target         string
	GrantID        string
	ApprovalID     string
	PolicyDecision string
	PolicyReason   string
	ExecutedAt     time.Time
	Result         string
	AuditHash      string
}

// Store persists Actions against a shared *sql.DB. The schema must already be
// initialized — typically by registry.InitSchema.
type Store struct {
	db  *sql.DB
	mkm *nexuscrypto.MasterKeyManager
}

// NewStore wraps db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// SetEncryption wires a MasterKeyManager for per-row AES-256-GCM encryption of
// policy_reason and result. Safe to call with a nil or disabled mkm.
func (s *Store) SetEncryption(mkm *nexuscrypto.MasterKeyManager) {
	s.mkm = mkm
}

// NewID generates a fresh action_id with the "act_" prefix.
func NewID() string {
	return IDPrefix + newULID()
}

// Record appends a new row to the action log. AgentID, Capability, and
// PolicyDecision are required. If ActionID is empty a fresh one is generated.
func (s *Store) Record(ctx context.Context, a Action) (Action, error) {
	if a.AgentID == "" {
		return Action{}, fmt.Errorf("actions: agent_id required")
	}
	if a.Capability == "" {
		return Action{}, fmt.Errorf("actions: capability required")
	}
	if a.PolicyDecision == "" {
		return Action{}, fmt.Errorf("actions: policy_decision required")
	}
	if a.ActionID == "" {
		a.ActionID = NewID()
	}
	if a.ExecutedAt.IsZero() {
		a.ExecutedAt = time.Now()
	}

	if s.mkm != nil && s.mkm.IsEnabled() {
		subKey := s.mkm.SubKey("nexus-control-key-v1")
		rowKey, err := nexuscrypto.DeriveRowKey(subKey, a.ActionID, rowInfo)
		if err != nil {
			return Action{}, fmt.Errorf("actions: derive row key: %w", err)
		}
		var encReason, encResult []byte
		if a.PolicyReason != "" {
			encReason, err = nexuscrypto.SealAES256GCM(rowKey, []byte(a.PolicyReason), []byte(a.ActionID))
			if err != nil {
				return Action{}, fmt.Errorf("actions: encrypt policy_reason: %w", err)
			}
		}
		if a.Result != "" {
			encResult, err = nexuscrypto.SealAES256GCM(rowKey, []byte(a.Result), []byte(a.ActionID))
			if err != nil {
				return Action{}, fmt.Errorf("actions: encrypt result: %w", err)
			}
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO action_log (
				action_id, agent_id, capability, target,
				grant_id, approval_id,
				policy_decision, policy_reason,
				executed_at_ms, result, audit_hash,
				policy_reason_encrypted, result_encrypted, encryption_version
			) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, NULL, ?, ?, ?, 1)`,
			a.ActionID, a.AgentID, a.Capability, nullIfEmpty(a.Target),
			nullIfEmpty(a.GrantID), nullIfEmpty(a.ApprovalID),
			a.PolicyDecision,
			a.ExecutedAt.UnixMilli(), nullIfEmpty(a.AuditHash),
			encReason, encResult,
		)
		if err != nil {
			return Action{}, fmt.Errorf("actions: insert: %w", err)
		}
	} else {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO action_log (
				action_id, agent_id, capability, target,
				grant_id, approval_id,
				policy_decision, policy_reason,
				executed_at_ms, result, audit_hash
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			a.ActionID, a.AgentID, a.Capability, nullIfEmpty(a.Target),
			nullIfEmpty(a.GrantID), nullIfEmpty(a.ApprovalID),
			a.PolicyDecision, nullIfEmpty(a.PolicyReason),
			a.ExecutedAt.UnixMilli(),
			nullIfEmpty(a.Result), nullIfEmpty(a.AuditHash),
		)
		if err != nil {
			return Action{}, fmt.Errorf("actions: insert: %w", err)
		}
	}
	return a, nil
}

// Get retrieves an action by ID.
func (s *Store) Get(ctx context.Context, actionID string) (*Action, error) {
	row := s.db.QueryRowContext(ctx, selectCols+` FROM action_log WHERE action_id = ?`, actionID)
	a, err := scanRow(row, s.mkm)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

// QueryFilter narrows Query results.
type QueryFilter struct {
	AgentID        string
	Capability     string
	PolicyDecision string
	Since          time.Time // executed_at_ms >= Since (zero = unbounded)
	Until          time.Time // executed_at_ms < Until (zero = unbounded)
	Limit          int       // 0 = no limit
}

// Query returns actions matching filter, ordered by executed_at_ms DESC.
func (s *Store) Query(ctx context.Context, filter QueryFilter) ([]Action, error) {
	query := selectCols + ` FROM action_log WHERE 1=1`
	var args []any
	if filter.AgentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, filter.AgentID)
	}
	if filter.Capability != "" {
		query += ` AND capability = ?`
		args = append(args, filter.Capability)
	}
	if filter.PolicyDecision != "" {
		query += ` AND policy_decision = ?`
		args = append(args, filter.PolicyDecision)
	}
	if !filter.Since.IsZero() {
		query += ` AND executed_at_ms >= ?`
		args = append(args, filter.Since.UnixMilli())
	}
	if !filter.Until.IsZero() {
		query += ` AND executed_at_ms < ?`
		args = append(args, filter.Until.UnixMilli())
	}
	query += ` ORDER BY executed_at_ms DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("actions: query: %w", err)
	}
	defer rows.Close()

	var out []Action
	for rows.Next() {
		a, err := scanRow(rows, s.mkm)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// selectCols includes all columns needed for full Action reconstruction,
// including CU.0.4 encrypted-column set.
const selectCols = `SELECT action_id, agent_id, capability, target,
	grant_id, approval_id, policy_decision, policy_reason,
	executed_at_ms, result, audit_hash,
	policy_reason_encrypted, result_encrypted, encryption_version`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(r rowScanner, mkm *nexuscrypto.MasterKeyManager) (*Action, error) {
	var (
		a                   Action
		target              sql.NullString
		grantID             sql.NullString
		approvalID          sql.NullString
		policyReason        sql.NullString
		executedMs          int64
		result              sql.NullString
		auditHash           sql.NullString
		policyReasonEncBlob []byte
		resultEncBlob       []byte
		encVersion          int64
	)
	err := r.Scan(
		&a.ActionID, &a.AgentID, &a.Capability, &target,
		&grantID, &approvalID, &a.PolicyDecision, &policyReason,
		&executedMs, &result, &auditHash,
		&policyReasonEncBlob, &resultEncBlob, &encVersion,
	)
	if err != nil {
		return nil, err
	}
	if target.Valid {
		a.Target = target.String
	}
	if grantID.Valid {
		a.GrantID = grantID.String
	}
	if approvalID.Valid {
		a.ApprovalID = approvalID.String
	}
	a.ExecutedAt = time.UnixMilli(executedMs)
	if auditHash.Valid {
		a.AuditHash = auditHash.String
	}

	if encVersion == 1 && mkm != nil && mkm.IsEnabled() {
		subKey := mkm.SubKey("nexus-control-key-v1")
		rowKey, keyErr := nexuscrypto.DeriveRowKey(subKey, a.ActionID, rowInfo)
		if keyErr != nil {
			return nil, fmt.Errorf("actions: derive row key: %w", keyErr)
		}
		if len(policyReasonEncBlob) > 0 {
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, policyReasonEncBlob, []byte(a.ActionID))
			if decErr != nil {
				return nil, fmt.Errorf("actions: decrypt policy_reason: %w", decErr)
			}
			a.PolicyReason = string(plain)
		} else if policyReason.Valid {
			a.PolicyReason = policyReason.String
		}
		if len(resultEncBlob) > 0 {
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, resultEncBlob, []byte(a.ActionID))
			if decErr != nil {
				return nil, fmt.Errorf("actions: decrypt result: %w", decErr)
			}
			a.Result = string(plain)
		} else if result.Valid {
			a.Result = result.String
		}
	} else {
		if policyReason.Valid {
			a.PolicyReason = policyReason.String
		}
		if result.Valid {
			a.Result = result.String
		}
	}
	return &a, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var entropyPool = sync.Pool{
	New: func() any { return ulid.Monotonic(rand.Reader, 0) },
}

func newULID() string {
	e := entropyPool.Get().(*ulid.MonotonicEntropy)
	defer entropyPool.Put(e)
	id, err := ulid.New(ulid.Timestamp(time.Now()), e)
	if err != nil {
		id = ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader)
	}
	return id.String()
}
