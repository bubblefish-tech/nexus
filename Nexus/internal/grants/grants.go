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

// Package grants implements durable storage and lookup of agent capability
// grants. A grant authorizes a specific agent to exercise a named capability,
// optionally scoped by JSON-encoded constraints and bounded by an expiry. The
// policy engine (MT.3) consumes CheckGrant to decide whether an action may
// proceed; MT.1 is storage-only and performs no policy evaluation.
package grants

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

const rowInfo = "grants-row"

// IDPrefix is the identifier prefix for grant IDs.
const IDPrefix = "gnt_"

// ErrNotFound is returned when a requested grant_id does not exist.
var ErrNotFound = errors.New("grants: grant not found")

// Grant is a durable record authorizing an agent to exercise a capability.
// Scope is an opaque JSON document interpreted by the policy engine; zero-value
// scope is a valid "unconstrained" grant. ExpiresAt, RevokedAt, and
// RevokeReason are nil/empty until set.
type Grant struct {
	GrantID      string
	AgentID      string
	Capability   string
	Scope        json.RawMessage
	GrantedBy    string
	GrantedAt    time.Time
	ExpiresAt    *time.Time
	RevokedAt    *time.Time
	RevokeReason string
}

// IsActive reports whether g is currently usable: not revoked, not expired as
// of now.
func (g *Grant) IsActive(now time.Time) bool {
	if g.RevokedAt != nil {
		return false
	}
	if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
		return false
	}
	return true
}

// Store persists Grants against a shared *sql.DB. The schema must already be
// initialized — typically by registry.InitSchema.
type Store struct {
	db  *sql.DB
	mkm *nexuscrypto.MasterKeyManager
}

// NewStore wraps db. It does not create tables; callers must run
// registry.InitSchema beforehand.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// SetEncryption wires a MasterKeyManager for per-row AES-256-GCM encryption of
// scope_json and revoke_reason. Safe to call with a nil or disabled mkm —
// writes and reads remain plaintext.
func (s *Store) SetEncryption(mkm *nexuscrypto.MasterKeyManager) {
	s.mkm = mkm
}

// NewID generates a fresh grant_id with the "gnt_" prefix and a 26-char ULID
// suffix.
func NewID() string {
	return IDPrefix + newULID()
}

// Create inserts g into the grants table. If g.GrantID is empty, a fresh ID is
// generated and assigned onto a copy; the returned Grant carries the final ID.
// AgentID, Capability, and GrantedBy are required.
func (s *Store) Create(ctx context.Context, g Grant) (Grant, error) {
	if g.AgentID == "" {
		return Grant{}, fmt.Errorf("grants: agent_id required")
	}
	if g.Capability == "" {
		return Grant{}, fmt.Errorf("grants: capability required")
	}
	if g.GrantedBy == "" {
		return Grant{}, fmt.Errorf("grants: granted_by required")
	}
	if g.GrantID == "" {
		g.GrantID = NewID()
	}
	if g.GrantedAt.IsZero() {
		g.GrantedAt = time.Now()
	}
	scope := g.Scope
	if len(scope) == 0 {
		scope = json.RawMessage("{}")
	}

	var expiresMs *int64
	if g.ExpiresAt != nil {
		v := g.ExpiresAt.UnixMilli()
		expiresMs = &v
	}

	if s.mkm != nil && s.mkm.IsEnabled() {
		subKey := s.mkm.SubKey("nexus-control-key-v1")
		rowKey, err := nexuscrypto.DeriveRowKey(subKey, g.GrantID, rowInfo)
		if err != nil {
			return Grant{}, fmt.Errorf("grants: derive row key: %w", err)
		}
		encScope, err := nexuscrypto.SealAES256GCM(rowKey, scope, []byte(g.GrantID))
		if err != nil {
			return Grant{}, fmt.Errorf("grants: encrypt scope_json: %w", err)
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO grants (
				grant_id, agent_id, capability, scope_json,
				granted_by, granted_at_ms, expires_at_ms,
				scope_json_encrypted, encryption_version
			) VALUES (?, ?, ?, '{}', ?, ?, ?, ?, 1)`,
			g.GrantID, g.AgentID, g.Capability,
			g.GrantedBy, g.GrantedAt.UnixMilli(), expiresMs,
			encScope,
		)
		if err != nil {
			return Grant{}, fmt.Errorf("grants: insert: %w", err)
		}
	} else {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO grants (
				grant_id, agent_id, capability, scope_json,
				granted_by, granted_at_ms, expires_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			g.GrantID, g.AgentID, g.Capability, string(scope),
			g.GrantedBy, g.GrantedAt.UnixMilli(), expiresMs,
		)
		if err != nil {
			return Grant{}, fmt.Errorf("grants: insert: %w", err)
		}
	}
	g.Scope = scope
	return g, nil
}

// Get retrieves a grant by ID. Returns ErrNotFound if no row matches.
func (s *Store) Get(ctx context.Context, grantID string) (*Grant, error) {
	row := s.db.QueryRowContext(ctx, selectCols+` FROM grants WHERE grant_id = ?`, grantID)
	g, err := scanRow(row, s.mkm)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

// ListFilter narrows the result set for List.
type ListFilter struct {
	AgentID      string // if non-empty, restrict to this agent
	Capability   string // if non-empty, restrict to this capability
	OnlyActive   bool   // if true, exclude revoked and expired-as-of-now
	IncludeScope bool   // reserved for future projection control
	Limit        int    // max rows to return; 0 = no cap
}

// List returns grants matching filter, ordered by granted_at_ms DESC.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]Grant, error) {
	query := selectCols + ` FROM grants WHERE 1=1`
	var args []any
	if filter.AgentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, filter.AgentID)
	}
	if filter.Capability != "" {
		query += ` AND capability = ?`
		args = append(args, filter.Capability)
	}
	if filter.OnlyActive {
		query += ` AND revoked_at_ms IS NULL AND (expires_at_ms IS NULL OR expires_at_ms > ?)`
		args = append(args, time.Now().UnixMilli())
	}
	query += ` ORDER BY granted_at_ms DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("grants: list: %w", err)
	}
	defer rows.Close()

	var out []Grant
	for rows.Next() {
		g, err := scanRow(rows, s.mkm)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// Revoke marks grantID as revoked with the given reason at the current time.
// Revoking an already-revoked grant is a no-op (the first revocation stands).
// Returns ErrNotFound if no row exists.
func (s *Store) Revoke(ctx context.Context, grantID, reason string) error {
	now := time.Now().UnixMilli()
	var (
		res sql.Result
		err error
	)
	if s.mkm != nil && s.mkm.IsEnabled() {
		subKey := s.mkm.SubKey("nexus-control-key-v1")
		rowKey, keyErr := nexuscrypto.DeriveRowKey(subKey, grantID, rowInfo)
		if keyErr != nil {
			return fmt.Errorf("grants: derive row key: %w", keyErr)
		}
		var encReason []byte
		if reason != "" {
			encReason, err = nexuscrypto.SealAES256GCM(rowKey, []byte(reason), []byte(grantID))
			if err != nil {
				return fmt.Errorf("grants: encrypt revoke_reason: %w", err)
			}
		}
		res, err = s.db.ExecContext(ctx, `
			UPDATE grants
			SET revoked_at_ms           = COALESCE(revoked_at_ms, ?),
			    revoke_reason_encrypted = COALESCE(revoke_reason_encrypted, ?),
			    revoke_reason           = '',
			    encryption_version      = 1
			WHERE grant_id = ?`,
			now, encReason, grantID)
	} else {
		res, err = s.db.ExecContext(ctx, `
			UPDATE grants
			SET revoked_at_ms = COALESCE(revoked_at_ms, ?),
			    revoke_reason = COALESCE(NULLIF(revoke_reason, ''), ?)
			WHERE grant_id = ?`,
			now, reason, grantID)
	}
	if err != nil {
		return fmt.Errorf("grants: revoke: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CheckGrant returns the most recently-granted active grant for (agentID,
// capability), or (nil, nil) if none exists. A DB error returns (nil, err).
func (s *Store) CheckGrant(ctx context.Context, agentID, capability string) (*Grant, error) {
	nowMs := time.Now().UnixMilli()
	row := s.db.QueryRowContext(ctx, selectCols+`
		FROM grants
		WHERE agent_id = ? AND capability = ?
		  AND revoked_at_ms IS NULL
		  AND (expires_at_ms IS NULL OR expires_at_ms > ?)
		ORDER BY granted_at_ms DESC
		LIMIT 1`, agentID, capability, nowMs)
	g, err := scanRow(row, s.mkm)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

// selectCols includes all columns needed for full Grant reconstruction,
// including CU.0.4 encrypted-column set.
const selectCols = `SELECT grant_id, agent_id, capability, scope_json,
	granted_by, granted_at_ms, expires_at_ms, revoked_at_ms, revoke_reason,
	scope_json_encrypted, revoke_reason_encrypted, encryption_version`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(r rowScanner, mkm *nexuscrypto.MasterKeyManager) (*Grant, error) {
	var (
		g                   Grant
		scopeStr            string
		grantedAtMs         int64
		expiresMs           sql.NullInt64
		revokedMs           sql.NullInt64
		revokeReason        sql.NullString
		scopeEncBlob        []byte
		revokeReasonEncBlob []byte
		encVersion          int64
	)
	err := r.Scan(
		&g.GrantID, &g.AgentID, &g.Capability, &scopeStr,
		&g.GrantedBy, &grantedAtMs, &expiresMs, &revokedMs, &revokeReason,
		&scopeEncBlob, &revokeReasonEncBlob, &encVersion,
	)
	if err != nil {
		return nil, err
	}

	if encVersion == 1 && mkm != nil && mkm.IsEnabled() {
		subKey := mkm.SubKey("nexus-control-key-v1")
		rowKey, keyErr := nexuscrypto.DeriveRowKey(subKey, g.GrantID, rowInfo)
		if keyErr != nil {
			return nil, fmt.Errorf("grants: derive row key: %w", keyErr)
		}
		if len(scopeEncBlob) > 0 {
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, scopeEncBlob, []byte(g.GrantID))
			if decErr != nil {
				return nil, fmt.Errorf("grants: decrypt scope_json: %w", decErr)
			}
			scopeStr = string(plain)
		}
		if len(revokeReasonEncBlob) > 0 {
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, revokeReasonEncBlob, []byte(g.GrantID))
			if decErr != nil {
				return nil, fmt.Errorf("grants: decrypt revoke_reason: %w", decErr)
			}
			g.RevokeReason = string(plain)
		} else if revokeReason.Valid {
			g.RevokeReason = revokeReason.String
		}
	} else {
		if revokeReason.Valid {
			g.RevokeReason = revokeReason.String
		}
	}

	g.Scope = json.RawMessage(scopeStr)
	g.GrantedAt = time.UnixMilli(grantedAtMs)
	if expiresMs.Valid {
		t := time.UnixMilli(expiresMs.Int64)
		g.ExpiresAt = &t
	}
	if revokedMs.Valid {
		t := time.UnixMilli(revokedMs.Int64)
		g.RevokedAt = &t
	}
	return &g, nil
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
