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

// Package registry implements the A2A agent registry, providing agent
// discovery, CRUD, Ed25519 card signing, and periodic health checks.
package registry

import (
	"time"

	"github.com/bubblefish-tech/nexus/internal/a2a"
	"github.com/bubblefish-tech/nexus/internal/a2a/transport"
)

// Agent status values.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusRetired   = "retired"
)

// ValidStatus returns true if s is a valid agent status.
func ValidStatus(s string) bool {
	switch s {
	case StatusActive, StatusSuspended, StatusRetired:
		return true
	}
	return false
}

// RegisteredAgent is a fully-resolved agent entry in the registry.
type RegisteredAgent struct {
	AgentID         string
	Name            string
	DisplayName     string
	AgentCard       a2a.AgentCard
	TransportConfig transport.TransportConfig
	PinnedPublicKey string // hex-encoded Ed25519 public key, if pinned
	Status          string // "active", "suspended", "retired"
	LastSeenAt      *time.Time
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
