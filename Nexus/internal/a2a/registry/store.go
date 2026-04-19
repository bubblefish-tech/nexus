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

package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
	"github.com/BurntSushi/toml"
	_ "modernc.org/sqlite" // SQLite driver
)

const rowInfo = "registry-row"

// SchemaSQL is the full DDL for the registry's SQLite schema, including the
// a2a_agents table (registered agents) and the MT.1 control-plane tables
// (grants, approval_requests, tasks, task_events, action_log). All statements
// use CREATE ... IF NOT EXISTS so re-running on an existing DB is a no-op.
const SchemaSQL = `
CREATE TABLE IF NOT EXISTS a2a_agents (
	agent_id                  TEXT PRIMARY KEY,
	name                      TEXT UNIQUE NOT NULL,
	display_name              TEXT,
	agent_card_json           BLOB NOT NULL,
	pinned_public_key         TEXT,
	transport_toml            TEXT NOT NULL,
	status                    TEXT NOT NULL,
	last_seen_at_ms           INTEGER,
	last_error                TEXT,
	created_at_ms             INTEGER NOT NULL,
	updated_at_ms             INTEGER NOT NULL,
	agent_card_json_encrypted BLOB,
	transport_toml_encrypted  BLOB,
	last_error_encrypted      BLOB,
	encryption_version        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS grants (
	grant_id                TEXT PRIMARY KEY,
	agent_id                TEXT NOT NULL REFERENCES a2a_agents(agent_id),
	capability              TEXT NOT NULL,
	scope_json              TEXT NOT NULL DEFAULT '{}',
	granted_by              TEXT NOT NULL,
	granted_at_ms           INTEGER NOT NULL,
	expires_at_ms           INTEGER,
	revoked_at_ms           INTEGER,
	revoke_reason           TEXT,
	scope_json_encrypted    BLOB,
	revoke_reason_encrypted BLOB,
	encryption_version      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_grants_agent_cap ON grants(agent_id, capability);
CREATE INDEX IF NOT EXISTS idx_grants_expires ON grants(expires_at_ms);

CREATE TABLE IF NOT EXISTS approval_requests (
	request_id            TEXT PRIMARY KEY,
	agent_id              TEXT NOT NULL REFERENCES a2a_agents(agent_id),
	capability            TEXT NOT NULL,
	action_json           TEXT NOT NULL,
	status                TEXT NOT NULL DEFAULT 'pending',
	requested_at_ms       INTEGER NOT NULL,
	decided_at_ms         INTEGER,
	decided_by            TEXT,
	decision              TEXT,
	reason                TEXT,
	action_json_encrypted BLOB,
	reason_encrypted      BLOB,
	encryption_version    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_approvals_status ON approval_requests(status);
CREATE INDEX IF NOT EXISTS idx_approvals_agent ON approval_requests(agent_id);

CREATE TABLE IF NOT EXISTS tasks (
	task_id               TEXT PRIMARY KEY,
	agent_id              TEXT NOT NULL REFERENCES a2a_agents(agent_id),
	parent_task_id        TEXT,
	state                 TEXT NOT NULL DEFAULT 'submitted',
	capability            TEXT,
	input_json            TEXT,
	output_json           TEXT,
	created_at_ms         INTEGER NOT NULL,
	updated_at_ms         INTEGER NOT NULL,
	completed_at_ms       INTEGER,
	input_json_encrypted  BLOB,
	output_json_encrypted BLOB,
	encryption_version    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_tasks_agent_state ON tasks(agent_id, state);
CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id);

CREATE TABLE IF NOT EXISTS task_events (
	event_id               TEXT PRIMARY KEY,
	task_id                TEXT NOT NULL REFERENCES tasks(task_id),
	event_type             TEXT NOT NULL,
	payload_json           TEXT,
	created_at_ms          INTEGER NOT NULL,
	payload_json_encrypted BLOB,
	encryption_version     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_task_events_task ON task_events(task_id, created_at_ms);

CREATE TABLE IF NOT EXISTS action_log (
	action_id               TEXT PRIMARY KEY,
	agent_id                TEXT NOT NULL,
	capability              TEXT NOT NULL,
	target                  TEXT,
	grant_id                TEXT,
	approval_id             TEXT,
	policy_decision         TEXT NOT NULL,
	policy_reason           TEXT,
	executed_at_ms          INTEGER NOT NULL,
	result                  TEXT,
	audit_hash              TEXT,
	policy_reason_encrypted BLOB,
	result_encrypted        BLOB,
	encryption_version      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_actions_agent_time ON action_log(agent_id, executed_at_ms);
CREATE INDEX IF NOT EXISTS idx_actions_capability ON action_log(capability);
`

// InitSchema applies SchemaSQL to db. Safe to call on an existing database —
// all DDL uses IF NOT EXISTS. Exported so packages that share the registry DB
// (grants, approvals, tasks, actions) can initialize a fresh in-memory DB in
// tests without importing the full Store.
func InitSchema(db *sql.DB) error {
	if _, err := db.Exec(SchemaSQL); err != nil {
		return fmt.Errorf("registry: init schema: %w", err)
	}
	return nil
}

// Store is a SQLite-backed agent registry.
type Store struct {
	db  *sql.DB
	mkm *nexuscrypto.MasterKeyManager
}

// NewStore opens (or creates) a SQLite database at path and initializes the
// a2a_agents table. It configures WAL mode and synchronous=FULL.
func NewStore(path string) (*Store, error) {
	dsn := path + "?_pragma=busy_timeout%3d5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("registry: open %q: %w", path, err)
	}

	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("registry: %s: %w", pragma, err)
		}
	}

	if _, err := db.Exec(SchemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("registry: create schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB so other subsystems (control plane,
// governance) can reuse the same connection pool and enforce foreign keys
// against the a2a_agents table. The caller MUST NOT close this DB — the
// registry Store owns its lifetime.
func (s *Store) DB() *sql.DB {
	return s.db
}

// NewStoreFromDB wraps an existing *sql.DB as a Store. The schema must already
// be initialized. Used in tests to share a single DB connection between
// multiple Store instances with different encryption keys.
func NewStoreFromDB(db *sql.DB) *Store {
	return &Store{db: db}
}

// SetEncryption wires a MasterKeyManager for per-row AES-256-GCM encryption of
// agent_card_json, transport_toml, and last_error. Safe to call with a nil or
// disabled mkm — writes and reads remain plaintext.
func (s *Store) SetEncryption(mkm *nexuscrypto.MasterKeyManager) {
	s.mkm = mkm
}

// Register inserts a new agent into the registry.
func (s *Store) Register(ctx context.Context, agent RegisteredAgent) error {
	if !ValidStatus(agent.Status) {
		return fmt.Errorf("registry: invalid status %q", agent.Status)
	}

	cardJSON, err := json.Marshal(agent.AgentCard)
	if err != nil {
		return fmt.Errorf("registry: marshal agent card: %w", err)
	}

	transportTOML, err := marshalTransportConfig(agent.TransportConfig)
	if err != nil {
		return fmt.Errorf("registry: marshal transport config: %w", err)
	}

	now := time.Now().UnixMilli()
	var lastSeenMs *int64
	if agent.LastSeenAt != nil {
		v := agent.LastSeenAt.UnixMilli()
		lastSeenMs = &v
	}

	if s.mkm != nil && s.mkm.IsEnabled() {
		subKey := s.mkm.SubKey("nexus-control-key-v1")
		rowKey, keyErr := nexuscrypto.DeriveRowKey(subKey, agent.AgentID, rowInfo)
		if keyErr != nil {
			return fmt.Errorf("registry: derive row key: %w", keyErr)
		}
		encCard, sealErr := nexuscrypto.SealAES256GCM(rowKey, cardJSON, []byte(agent.AgentID))
		if sealErr != nil {
			return fmt.Errorf("registry: encrypt agent_card_json: %w", sealErr)
		}
		encTransport, sealErr := nexuscrypto.SealAES256GCM(rowKey, []byte(transportTOML), []byte(agent.AgentID))
		if sealErr != nil {
			return fmt.Errorf("registry: encrypt transport_toml: %w", sealErr)
		}
		var encLastError []byte
		if agent.LastError != "" {
			encLastError, err = nexuscrypto.SealAES256GCM(rowKey, []byte(agent.LastError), []byte(agent.AgentID))
			if err != nil {
				return fmt.Errorf("registry: encrypt last_error: %w", err)
			}
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO a2a_agents (
				agent_id, name, display_name, agent_card_json,
				pinned_public_key, transport_toml, status,
				last_seen_at_ms, last_error, created_at_ms, updated_at_ms,
				agent_card_json_encrypted, transport_toml_encrypted, last_error_encrypted, encryption_version
			) VALUES (?, ?, ?, '{}', ?, '', ?, ?, '', ?, ?, ?, ?, ?, 1)`,
			agent.AgentID, agent.Name, agent.DisplayName,
			agent.PinnedPublicKey, agent.Status,
			lastSeenMs, now, now,
			encCard, encTransport, encLastError,
		)
	} else {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO a2a_agents (
				agent_id, name, display_name, agent_card_json,
				pinned_public_key, transport_toml, status,
				last_seen_at_ms, last_error, created_at_ms, updated_at_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			agent.AgentID, agent.Name, agent.DisplayName, cardJSON,
			agent.PinnedPublicKey, transportTOML, agent.Status,
			lastSeenMs, agent.LastError, now, now,
		)
	}
	if err != nil {
		return fmt.Errorf("registry: insert agent: %w", err)
	}
	return nil
}

// selectAgentCols includes all columns needed for full RegisteredAgent
// reconstruction, including CU.0.6 encrypted-column set.
const selectAgentCols = `SELECT agent_id, name, display_name, agent_card_json,
	pinned_public_key, transport_toml, status,
	last_seen_at_ms, last_error, created_at_ms, updated_at_ms,
	agent_card_json_encrypted, transport_toml_encrypted, last_error_encrypted, encryption_version`

// Get retrieves an agent by ID.
func (s *Store) Get(ctx context.Context, agentID string) (*RegisteredAgent, error) {
	row := s.db.QueryRowContext(ctx, selectAgentCols+` FROM a2a_agents WHERE agent_id = ?`, agentID)
	agent, err := scanAgentWith(row, s.mkm)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("registry: agent not found")
	}
	return agent, err
}

// GetByName retrieves an agent by unique name.
func (s *Store) GetByName(ctx context.Context, name string) (*RegisteredAgent, error) {
	row := s.db.QueryRowContext(ctx, selectAgentCols+` FROM a2a_agents WHERE name = ?`, name)
	agent, err := scanAgentWith(row, s.mkm)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("registry: agent not found")
	}
	return agent, err
}

// ListFilter specifies optional criteria for listing agents.
type ListFilter struct {
	Status string // if non-empty, filter by status
}

// List returns all agents matching the optional filter.
func (s *Store) List(ctx context.Context, filter ListFilter) ([]RegisteredAgent, error) {
	query := selectAgentCols + ` FROM a2a_agents`
	var args []interface{}

	if filter.Status != "" {
		query += " WHERE status = ?"
		args = append(args, filter.Status)
	}
	query += " ORDER BY name"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("registry: list agents: %w", err)
	}
	defer rows.Close()

	var agents []RegisteredAgent
	for rows.Next() {
		agent, err := scanAgentWith(rows, s.mkm)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *agent)
	}
	return agents, rows.Err()
}

// UpdateStatus changes an agent's status.
func (s *Store) UpdateStatus(ctx context.Context, agentID, status string) error {
	if !ValidStatus(status) {
		return fmt.Errorf("registry: invalid status %q", status)
	}
	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx, `
		UPDATE a2a_agents SET status = ?, updated_at_ms = ?
		WHERE agent_id = ?`, status, now, agentID)
	if err != nil {
		return fmt.Errorf("registry: update status: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("registry: agent %q not found", agentID)
	}
	return nil
}

// UpdateLastSeen updates the last_seen_at and optionally clears last_error.
func (s *Store) UpdateLastSeen(ctx context.Context, agentID string, seenAt time.Time, lastError string) error {
	now := time.Now().UnixMilli()
	seenMs := seenAt.UnixMilli()
	var (
		res sql.Result
		err error
	)
	if s.mkm != nil && s.mkm.IsEnabled() {
		subKey := s.mkm.SubKey("nexus-control-key-v1")
		rowKey, keyErr := nexuscrypto.DeriveRowKey(subKey, agentID, rowInfo)
		if keyErr != nil {
			return fmt.Errorf("registry: derive row key: %w", keyErr)
		}
		var encLastError []byte
		if lastError != "" {
			encLastError, err = nexuscrypto.SealAES256GCM(rowKey, []byte(lastError), []byte(agentID))
			if err != nil {
				return fmt.Errorf("registry: encrypt last_error: %w", err)
			}
		}
		res, err = s.db.ExecContext(ctx, `
			UPDATE a2a_agents
			SET last_seen_at_ms = ?, last_error = '', last_error_encrypted = ?,
			    encryption_version = 1, updated_at_ms = ?
			WHERE agent_id = ?`, seenMs, encLastError, now, agentID)
	} else {
		res, err = s.db.ExecContext(ctx, `
			UPDATE a2a_agents
			SET last_seen_at_ms = ?, last_error = ?, updated_at_ms = ?
			WHERE agent_id = ?`, seenMs, lastError, now, agentID)
	}
	if err != nil {
		return fmt.Errorf("registry: update last seen: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("registry: agent %q not found", agentID)
	}
	return nil
}

// Delete removes an agent from the registry.
func (s *Store) Delete(ctx context.Context, agentID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM a2a_agents WHERE agent_id = ?`, agentID)
	if err != nil {
		return fmt.Errorf("registry: delete agent: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("registry: agent %q not found", agentID)
	}
	return nil
}

// scanner is the interface shared by *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...interface{}) error
}

// scanAgentWith scans a row (from QueryRow or Rows.Next) and decrypts
// agent_card_json, transport_toml, and last_error when encryption_version=1
// and mkm is enabled.
func scanAgentWith(r scanner, mkm *nexuscrypto.MasterKeyManager) (*RegisteredAgent, error) {
	var (
		agentID, name, displayName string
		cardJSON, transportTOML    string
		pinnedKey, status          string
		lastSeenMs                 *int64
		lastError                  string
		createdMs, updatedMs       int64
		cardEncBlob                []byte
		transportEncBlob           []byte
		lastErrorEncBlob           []byte
		encVersion                 int64
	)
	err := r.Scan(
		&agentID, &name, &displayName, &cardJSON,
		&pinnedKey, &transportTOML, &status,
		&lastSeenMs, &lastError, &createdMs, &updatedMs,
		&cardEncBlob, &transportEncBlob, &lastErrorEncBlob, &encVersion,
	)
	if err != nil {
		return nil, err
	}

	if encVersion == 1 && mkm != nil && mkm.IsEnabled() {
		subKey := mkm.SubKey("nexus-control-key-v1")
		rowKey, keyErr := nexuscrypto.DeriveRowKey(subKey, agentID, rowInfo)
		if keyErr != nil {
			return nil, fmt.Errorf("registry: derive row key: %w", keyErr)
		}
		if len(cardEncBlob) > 0 {
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, cardEncBlob, []byte(agentID))
			if decErr != nil {
				return nil, fmt.Errorf("registry: decrypt agent_card_json: %w", decErr)
			}
			cardJSON = string(plain)
		}
		if len(transportEncBlob) > 0 {
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, transportEncBlob, []byte(agentID))
			if decErr != nil {
				return nil, fmt.Errorf("registry: decrypt transport_toml: %w", decErr)
			}
			transportTOML = string(plain)
		}
		if len(lastErrorEncBlob) > 0 {
			plain, decErr := nexuscrypto.OpenAES256GCM(rowKey, lastErrorEncBlob, []byte(agentID))
			if decErr != nil {
				return nil, fmt.Errorf("registry: decrypt last_error: %w", decErr)
			}
			lastError = string(plain)
		}
	}

	return buildAgent(agentID, name, displayName, cardJSON, pinnedKey,
		transportTOML, status, lastSeenMs, lastError, createdMs, updatedMs)
}

func buildAgent(agentID, name, displayName, cardJSON, pinnedKey,
	transportTOML, status string, lastSeenMs *int64, lastError string,
	createdMs, updatedMs int64) (*RegisteredAgent, error) {

	var card a2a.AgentCard
	if err := json.Unmarshal([]byte(cardJSON), &card); err != nil {
		return nil, fmt.Errorf("registry: unmarshal agent card: %w", err)
	}

	var tcfg transport.TransportConfig
	if err := unmarshalTransportConfig(transportTOML, &tcfg); err != nil {
		return nil, fmt.Errorf("registry: unmarshal transport config: %w", err)
	}

	agent := &RegisteredAgent{
		AgentID:         agentID,
		Name:            name,
		DisplayName:     displayName,
		AgentCard:       card,
		TransportConfig: tcfg,
		PinnedPublicKey: pinnedKey,
		Status:          status,
		LastError:       lastError,
		CreatedAt:       time.UnixMilli(createdMs),
		UpdatedAt:       time.UnixMilli(updatedMs),
	}
	if lastSeenMs != nil {
		t := time.UnixMilli(*lastSeenMs)
		agent.LastSeenAt = &t
	}
	return agent, nil
}

// marshalTransportConfig encodes a TransportConfig as TOML.
func marshalTransportConfig(cfg transport.TransportConfig) (string, error) {
	buf, err := toml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// unmarshalTransportConfig decodes a TOML string into a TransportConfig.
func unmarshalTransportConfig(data string, cfg *transport.TransportConfig) error {
	_, err := toml.Decode(data, cfg)
	return err
}
