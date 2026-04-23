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

package subscribe

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

const IDPrefix = "sub_"

var ErrNotFound = errors.New("subscribe: subscription not found")

const schemaSQL = `
CREATE TABLE IF NOT EXISTS subscriptions (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL,
    filter      TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    match_count INTEGER NOT NULL DEFAULT 0,
    active      INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_subscriptions_agent ON subscriptions(agent_id);
`

type Subscription struct {
	ID         string    `json:"id"`
	AgentID    string    `json:"agent_id"`
	Filter     string    `json:"filter"`
	CreatedAt  time.Time `json:"created_at"`
	MatchCount int       `json:"match_count"`
	Active     bool      `json:"active"`
}

type Store struct {
	mu sync.RWMutex
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("subscribe: init schema: %w", err)
	}
	return &Store{db: db}, nil
}

var entropyPool = sync.Pool{
	New: func() interface{} {
		return ulid.Monotonic(rand.Reader, 0)
	},
}

func newID() string {
	entropy := entropyPool.Get().(*ulid.MonotonicEntropy)
	defer entropyPool.Put(entropy)
	id, err := ulid.New(ulid.Timestamp(time.Now()), entropy)
	if err != nil {
		id = ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader)
	}
	return IDPrefix + id.String()
}

func (s *Store) Add(agentID, filter string) (*Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sub := &Subscription{
		ID:        newID(),
		AgentID:   agentID,
		Filter:    filter,
		CreatedAt: time.Now().UTC(),
		Active:    true,
	}

	_, err := s.db.Exec(
		`INSERT INTO subscriptions (id, agent_id, filter, created_at, match_count, active) VALUES (?, ?, ?, ?, 0, 1)`,
		sub.ID, sub.AgentID, sub.Filter, sub.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return nil, fmt.Errorf("subscribe: add: %w", err)
	}
	return sub, nil
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`DELETE FROM subscriptions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("subscribe: remove: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListForAgent(agentID string) []*Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, agent_id, filter, created_at, match_count, active FROM subscriptions WHERE agent_id = ? ORDER BY created_at DESC`,
		agentID,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanSubscriptions(rows)
}

func (s *Store) All() []*Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT id, agent_id, filter, created_at, match_count, active FROM subscriptions WHERE active = 1 ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanSubscriptions(rows)
}

func (s *Store) IncrementMatch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.db.Exec(`UPDATE subscriptions SET match_count = match_count + 1 WHERE id = ?`, id) //nolint:errcheck
}

func (s *Store) Get(id string) (*Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(
		`SELECT id, agent_id, filter, created_at, match_count, active FROM subscriptions WHERE id = ?`,
		id,
	)
	return scanSubscription(row)
}

func scanSubscriptions(rows *sql.Rows) []*Subscription {
	var subs []*Subscription
	for rows.Next() {
		var sub Subscription
		var createdMS int64
		var active int
		if err := rows.Scan(&sub.ID, &sub.AgentID, &sub.Filter, &createdMS, &sub.MatchCount, &active); err != nil {
			continue
		}
		sub.CreatedAt = time.UnixMilli(createdMS).UTC()
		sub.Active = active == 1
		subs = append(subs, &sub)
	}
	return subs
}

func scanSubscription(row *sql.Row) (*Subscription, error) {
	var sub Subscription
	var createdMS int64
	var active int
	if err := row.Scan(&sub.ID, &sub.AgentID, &sub.Filter, &createdMS, &sub.MatchCount, &active); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("subscribe: scan: %w", err)
	}
	sub.CreatedAt = time.UnixMilli(createdMS).UTC()
	sub.Active = active == 1
	return &sub, nil
}
