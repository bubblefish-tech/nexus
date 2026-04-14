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

package agent

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Registry manages agent registration and lifecycle backed by SQLite.
type Registry struct {
	db *sql.DB
}

// NewRegistry creates the agents table if absent and returns a ready-to-use
// registry. The db must be an open *sql.DB pointing at the daemon's SQLite.
func NewRegistry(db *sql.DB) (*Registry, error) {
	if db == nil {
		return nil, fmt.Errorf("agent: registry requires non-nil db")
	}

	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			agent_id        TEXT PRIMARY KEY,
			name            TEXT NOT NULL UNIQUE,
			description     TEXT NOT NULL DEFAULT '',
			status          TEXT NOT NULL DEFAULT 'active',
			created_at      TEXT NOT NULL,
			last_seen_at    TEXT NOT NULL DEFAULT '',
			ed25519_pubkey  BLOB,
			metadata        TEXT NOT NULL DEFAULT '{}'
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("agent: create agents table: %w", err)
	}

	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_agents_name ON agents(name)
	`)
	if err != nil {
		return nil, fmt.Errorf("agent: create name index: %w", err)
	}

	return &Registry{db: db}, nil
}

// generateID creates a random 16-byte hex agent ID.
func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("agent: generate ID: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Register creates a new agent with the given name and description.
// Returns the generated agent ID.
func (r *Registry) Register(name, description string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("agent: name is required")
	}

	id, err := generateID()
	if err != nil {
		return "", err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	meta, _ := json.Marshal(map[string]string{})

	_, err = r.db.Exec(`
		INSERT INTO agents (agent_id, name, description, status, created_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, name, description, string(StatusActive), now, string(meta))
	if err != nil {
		return "", fmt.Errorf("agent: register %q: %w", name, err)
	}

	return id, nil
}

// Get returns the agent with the given ID, or nil if not found.
func (r *Registry) Get(id string) (*Agent, error) {
	return r.scanOne(`SELECT agent_id, name, description, status, created_at, last_seen_at, ed25519_pubkey, metadata FROM agents WHERE agent_id = ?`, id)
}

// GetByName returns the agent with the given name, or nil if not found.
func (r *Registry) GetByName(name string) (*Agent, error) {
	return r.scanOne(`SELECT agent_id, name, description, status, created_at, last_seen_at, ed25519_pubkey, metadata FROM agents WHERE name = ?`, name)
}

// List returns all registered agents.
func (r *Registry) List() ([]Agent, error) {
	rows, err := r.db.Query(`SELECT agent_id, name, description, status, created_at, last_seen_at, ed25519_pubkey, metadata FROM agents ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("agent: list: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *a)
	}
	return agents, rows.Err()
}

// Suspend marks an agent as suspended. Suspended agents' requests are rejected.
func (r *Registry) Suspend(id string) error {
	return r.setStatus(id, StatusSuspended)
}

// Retire marks an agent as retired (soft delete). Audit history is preserved.
func (r *Registry) Retire(id string) error {
	return r.setStatus(id, StatusRetired)
}

// Reactivate returns a suspended or retired agent to active status.
func (r *Registry) Reactivate(id string) error {
	return r.setStatus(id, StatusActive)
}

// TouchLastSeen updates the last_seen_at timestamp for the agent.
func (r *Registry) TouchLastSeen(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := r.db.Exec(`UPDATE agents SET last_seen_at = ? WHERE agent_id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("agent: touch last seen: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent: %s not found", id)
	}
	return nil
}

func (r *Registry) setStatus(id string, status Status) error {
	result, err := r.db.Exec(`UPDATE agents SET status = ? WHERE agent_id = ?`, string(status), id)
	if err != nil {
		return fmt.Errorf("agent: set status %s for %s: %w", status, id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent: %s not found", id)
	}
	return nil
}

func (r *Registry) scanOne(query string, args ...interface{}) (*Agent, error) {
	row := r.db.QueryRow(query, args...)
	a, err := scanAgentRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanAgent(s scannable) (*Agent, error) {
	return scanAgentRow(s)
}

func scanAgentRow(s scannable) (*Agent, error) {
	var a Agent
	var statusStr, createdStr, lastSeenStr string
	var pubkey []byte
	var metaJSON string

	if err := s.Scan(&a.ID, &a.Name, &a.Description, &statusStr, &createdStr, &lastSeenStr, &pubkey, &metaJSON); err != nil {
		return nil, fmt.Errorf("agent: scan: %w", err)
	}

	a.Status = Status(statusStr)
	a.Ed25519PubKey = pubkey

	if t, err := time.Parse(time.RFC3339, createdStr); err == nil {
		a.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, lastSeenStr); err == nil {
		a.LastSeenAt = t
	}

	if metaJSON != "" && metaJSON != "{}" {
		_ = json.Unmarshal([]byte(metaJSON), &a.Metadata)
	}

	return &a, nil
}
