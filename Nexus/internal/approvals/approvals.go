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

// Package approvals stores and transitions human-in-the-loop approval requests
// for governed capabilities. A request is created in status=pending, then
// transitioned exactly once to approved, denied, or expired. The policy engine
// (MT.3) queries by (agent_id, capability) + action payload to decide whether
// a prior approval authorizes the action; MT.1 is storage-only.
package approvals

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

const rowInfo = "approvals-row"

// IDPrefix is the identifier prefix for approval request IDs.
const IDPrefix = "apr_"

// Status values for an approval request.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusDenied   = "denied"
	StatusExpired  = "expired"
)

// Decision values recorded on a decided request.
const (
	DecisionApprove = "approve"
	DecisionDeny    = "deny"
)

// ErrNotFound is returned when a request_id does not exist.
var ErrNotFound = errors.New("approvals: request not found")

// ErrAlreadyDecided is returned by Decide if the request is not in pending
// status at the moment of decision.
var ErrAlreadyDecided = errors.New("approvals: request already decided")

// Request is a durable record of a human-approval request for a capability.
// Action is an opaque JSON payload describing the specific action being
// approved (distinct from the Capability, which names the category).
type Request struct {
	RequestID   string
	AgentID     string
	Capability  string
	Action      json.RawMessage
	Status      string
	RequestedAt time.Time
	DecidedAt   *time.Time
	DecidedBy   string
	Decision    string
	Reason      string
}

// Store persists Requests against a shared *sql.DB. The schema must already be
// initialized — typically by registry.InitSchema.
type Store struct {
	db  *sql.DB
	mkm *nexuscrypto.MasterKeyManager
}

// NewStore wraps db. It does not create tables.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// SetEncryption wires a MasterKeyManager for per-row AES-256-GCM encryption of
// action_json and reason. Safe to call with a nil or disabled mkm.
func (s *Store) SetEncryption(mkm *nexuscrypto.MasterKeyManager) {
	s.mkm = mkm
}

// NewID generates a fresh request_id with the "apr_" prefix.
func NewID() string {
	return IDPrefix + newULID()
}

// Create inserts r. AgentID, Capability, and Action are required. If r.RequestID
// is empty a fresh ID is assigned; if r.Status is empty it defaults to pending.
func (s *Store) Create(ctx context.Context, r Request) (Request, error) {
	if r.AgentID == "" {
		return Request{}, fmt.Errorf("approvals: agent_id required")
	}
	if r.Capability == "" {
		return Request{}, fmt.Errorf("approvals: capability required")
	}
	if len(r.Action) == 0 {
		return Request{}, fmt.Errorf("approvals: action required")
	}
	if !json.Valid(r.Action) {
		return Request{}, fmt.Errorf("approvals: action is not valid JSON")
	}
	if r.RequestID == "" {
		r.RequestID = NewID()
	}
	if r.Status == "" {
		r.Status = StatusPending
	}
	if r.RequestedAt.IsZero() {
		r.RequestedAt = time.Now()
	}

	if s.mkm != nil && s.mkm.IsEnabled() {
		subKey := s.mkm.SubKey("nexus-control-key-v1")
		rowKey, err := nexuscrypto.DeriveRowKey(subKey, r.RequestID, rowInfo)
		if err != nil {
			return Request{}, fmt.Errorf("approvals: derive row key: %w", err)
		}
		encAction, err := nexuscrypto.SealAES256GCM(rowKey, r.Action, []byte(r.RequestID))
		if err != nil {
			return Request{}, fmt.Errorf("approvals: encrypt action_json: %w", err)
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO approval_requests (
				request_id, agent_id, capability, action_json,
				status, requested_at_ms,
				action_json_encrypted, encryption_version
			) VALUES (?, ?, ?, '', ?, ?, ?, 1)`,
			r.RequestID, r.AgentID, r.Capability,
			r.Status, r.RequestedAt.UnixMilli(),
			encAction,
		)
		if err != nil {
			return Request{}, fmt.Errorf("approvals: insert: %w", err)
		}
	} else {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO approval_requests (
				request_id, agent_id, capability, action_json,
				status, requested_at_ms
			) VALUES (?, ?, ?, ?, ?, ?)`,
			r.RequestID, r.AgentID, r.Capability, string(r.Action),
			r.Status, r.RequestedAt.UnixMilli(),
		)
		if err != nil {
			return Request{}, fmt.Errorf("approvals: insert: %w", err)
		}
	}
	return r, nil
}

// Get retrieves a request by ID. Returns ErrNotFound if none exists.
func (s *Store) Get(ctx context.Context, requestID string) (*Request, error) {
	row := s.db.QueryRowContext(ctx, selectCols+` FROM approval_requests WHERE request_id = ?`, requestID)
	r, err := scanRow(row, s.mkm)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// ListFilter narrows List results.
type ListFilter struct {
	AgentID    string // restrict to this agent
	Status     string // restrict to this status (e.g. "pending")
	Capability string // restrict to this capability
	Limit      int    // max rows to return; 0 = no cap
}

// List returns requests matching filter, ordered by requested_at_ms DESC.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]Request, error) {
	query := selectCols + ` FROM approval_requests WHERE 1=1`
	var args []any
	if filter.AgentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, filter.AgentID)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.Capability != "" {
		query += ` AND capability = ?`
		args = append(args, filter.Capability)
	}
	query += ` ORDER BY requested_at_ms DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("approvals: list: %w", err)
	}
	defer rows.Close()

	var out []Request
	for rows.Next() {
		r, err := scanRow(rows, s.mkm)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// ListPending returns all requests in pending status (newest first). Shorthand
// for List({Status: StatusPending}).
func (s *Store) ListPending(ctx context.Context) ([]Request, error) {
	return s.List(ctx, ListFilter{Status: StatusPending})
}

// DecideInput carries the inputs to Decide.
type DecideInput struct {
	Decision  string // DecisionApprove or DecisionDeny
	DecidedBy string // required, typically the admin actor
	Reason    string
}

// Decide transitions a pending request to approved or denied. Returns
// ErrNotFound if no such request; ErrAlreadyDecided if the request is not in
// pending status. DecidedAt is set to the current time.
func (s *Store) Decide(ctx context.Context, requestID string, in DecideInput) error {
	if in.Decision != DecisionApprove && in.Decision != DecisionDeny {
		return fmt.Errorf("approvals: decision must be %q or %q", DecisionApprove, DecisionDeny)
	}
	if in.DecidedBy == "" {
		return fmt.Errorf("approvals: decided_by required")
	}

	targetStatus := StatusApproved
	if in.Decision == DecisionDeny {
		targetStatus = StatusDenied
	}
	nowMs := time.Now().UnixMilli()

	var (
		res sql.Result
		err error
	)
	if s.mkm != nil && s.mkm.IsEnabled() {
		subKey := s.mkm.SubKey("nexus-control-key-v1")
		rowKey, keyErr := nexuscrypto.DeriveRowKey(subKey, requestID, rowInfo)
		if keyErr != nil {
			return fmt.Errorf("approvals: derive row key: %w", keyErr)
		}
		var encReason []byte
		if in.Reason != "" {
			encReason, err = nexuscrypto.SealAES256GCM(rowKey, []byte(in.Reason), []byte(requestID))
			if err != nil {
				return fmt.Errorf("approvals: encrypt reason: %w", err)
			}
		}
		res, err = s.db.ExecContext(ctx, `
			UPDATE approval_requests
			SET status = ?, decided_at_ms = ?, decided_by = ?, decision = ?,
			    reason = '', reason_encrypted = ?, encryption_version = 1
			WHERE request_id = ? AND status = ?`,
			targetStatus, nowMs, in.DecidedBy, in.Decision,
			encReason, requestID, StatusPending)
	} else {
		res, err = s.db.ExecContext(ctx, `
			UPDATE approval_requests
			SET status = ?, decided_at_ms = ?, decided_by = ?, decision = ?, reason = ?
			WHERE request_id = ? AND status = ?`,
			targetStatus, nowMs, in.DecidedBy, in.Decision, in.Reason,
			requestID, StatusPending)
	}
	if err != nil {
		return fmt.Errorf("approvals: decide: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return nil
	}

	// Zero rows updated: either not found or not pending. Distinguish.
	r, getErr := s.Get(ctx, requestID)
	if errors.Is(getErr, ErrNotFound) {
		return ErrNotFound
	}
	if getErr != nil {
		return getErr
	}
	if r.Status != StatusPending {
		return ErrAlreadyDecided
	}
	// Fallback — should be unreachable.
	return fmt.Errorf("approvals: decide: no rows updated")
}

// Expire transitions a pending request to expired status. Used by background
// sweepers (MT.3+) and by tests to simulate stale pending requests. Returns
// ErrNotFound or ErrAlreadyDecided on non-pending rows.
func (s *Store) Expire(ctx context.Context, requestID string) error {
	nowMs := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx, `
		UPDATE approval_requests
		SET status = ?, decided_at_ms = ?
		WHERE request_id = ? AND status = ?`,
		StatusExpired, nowMs, requestID, StatusPending)
	if err != nil {
		return fmt.Errorf("approvals: expire: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return nil
	}
	r, getErr := s.Get(ctx, requestID)
	if errors.Is(getErr, ErrNotFound) {
		return ErrNotFound
	}
	if getErr != nil {
		return getErr
	}
	if r.Status != StatusPending {
		return ErrAlreadyDecided
	}
	return fmt.Errorf("approvals: expire: no rows updated")
}

// selectCols includes all columns needed for full Request reconstruction,
// including CU.0.4 encrypted-column set.
const selectCols = `SELECT request_id, agent_id, capability, action_json,
	status, requested_at_ms, decided_at_ms, decided_by, decision, reason,
	action_json_encrypted, reason_encrypted, encryption_version`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(s rowScanner, mkm *nexuscrypto.MasterKeyManager) (*Request, error) {
	var (
		r               Request
		actionStr       string
		requestedMs     int64
		decidedMs       sql.NullInt64
		decidedBy       sql.NullString
		decision        sql.NullString
		reason          sql.NullString
		actionEncBlob   []byte
		reasonEncBlob   []byte
		encVersion      int64
	)
	err := s.Scan(
		&r.RequestID, &r.AgentID, &r.Capability, &actionStr,
		&r.Status, &requestedMs, &decidedMs, &decidedBy, &decision, &reason,
		&actionEncBlob, &reasonEncBlob, &encVersion,
	)
	if err != nil {
		return nil, err
	}

	if encVersion == 1 && mkm != nil && mkm.IsEnabled() {
		subKey := mkm.SubKey("nexus-control-key-v1")
		rowKey, keyErr := nexuscrypto.DeriveRowKey(subKey, r.RequestID, rowInfo)
		if keyErr != nil {
			return nil, fmt.Errorf("approvals: derive row key: %w", keyErr)
		}
		if len(actionEncBlob) > 0 {
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, actionEncBlob, []byte(r.RequestID))
			if decErr != nil {
				return nil, fmt.Errorf("approvals: decrypt action_json: %w", decErr)
			}
			actionStr = string(plain)
		}
		if len(reasonEncBlob) > 0 {
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, reasonEncBlob, []byte(r.RequestID))
			if decErr != nil {
				return nil, fmt.Errorf("approvals: decrypt reason: %w", decErr)
			}
			r.Reason = string(plain)
		} else if reason.Valid {
			r.Reason = reason.String
		}
	} else {
		if reason.Valid {
			r.Reason = reason.String
		}
	}

	r.Action = json.RawMessage(actionStr)
	r.RequestedAt = time.UnixMilli(requestedMs)
	if decidedMs.Valid {
		t := time.UnixMilli(decidedMs.Int64)
		r.DecidedAt = &t
	}
	if decidedBy.Valid {
		r.DecidedBy = decidedBy.String
	}
	if decision.Valid {
		r.Decision = decision.String
	}
	return &r, nil
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
