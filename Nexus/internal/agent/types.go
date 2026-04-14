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

// Package agent implements agent identity, registration, and lifecycle
// management for the BubbleFish Nexus Agent Gateway.
//
// Every agent that uses Nexus has a durable identity separate from its
// source config. Sources describe where data comes from; agents describe
// who is acting. The agent_id is carried on every WAL entry when the
// X-Agent-ID header is present.
package agent

import "time"

// Status represents the lifecycle state of a registered agent.
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusRetired   Status = "retired"
)

// Agent is a registered entity in the Agent Gateway.
type Agent struct {
	ID          string            `json:"agent_id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Status      Status            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	LastSeenAt  time.Time         `json:"last_seen_at,omitempty"`
	Ed25519PubKey []byte          `json:"ed25519_pubkey,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}
