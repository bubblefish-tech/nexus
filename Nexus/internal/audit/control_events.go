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

package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Control-plane audit event types.
const (
	ControlEventGrantCreated       = "grant_created"
	ControlEventGrantRevoked       = "grant_revoked"
	ControlEventApprovalRequested  = "approval_requested"
	ControlEventApprovalDecided    = "approval_decided"
	ControlEventTaskCreated        = "task_created"
	ControlEventTaskStateChanged   = "task_state_changed"
	ControlEventActionExecuted     = "action_executed"
	ControlEventActionDenied       = "action_denied"
)

// ControlEventRecord is a structured audit record for control-plane mutations.
// Every mutation of a grant, approval, task, or policy decision produces one record.
// Records are hash-chained through PrevHash into the existing Phase 4 audit log.
type ControlEventRecord struct {
	RecordID   string          `json:"record_id"`
	EventType  string          `json:"event_type"`
	Actor      string          `json:"actor"`
	ActorType  string          `json:"actor_type"`  // "admin", "agent", "system"
	TargetID   string          `json:"target_id"`
	TargetType string          `json:"target_type"` // "grant", "approval", "task", "action"
	AgentID    string          `json:"agent_id"`
	Capability string          `json:"capability"`
	EntityJSON json.RawMessage `json:"entity,omitempty"` // full snapshot of the mutated entity
	Decision   string          `json:"decision"`
	Reason     string          `json:"reason,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
	PrevHash   string          `json:"prev_hash,omitempty"` // SHA-256 of previous audit entry
	Hash       string          `json:"hash,omitempty"`      // SHA-256 of this record (set after chain)

	// CU.0.5 — Selective disclosure: when EncryptionVersion=1, PayloadEncrypted
	// holds the AES-256-GCM encrypted JSON of the sensitive payload fields
	// (Actor, ActorType, TargetID, TargetType, AgentID, Capability, EntityJSON,
	// Decision, Reason). Chain fields (RecordID, EventType, Timestamp, PrevHash,
	// Hash) remain plaintext for chain integrity verification without the key.
	PayloadEncrypted  []byte `json:"payload_encrypted,omitempty"`
	EncryptionVersion int    `json:"encryption_version,omitempty"`
}

// ComputeHash returns the SHA-256 of the record serialized with Hash set to "".
// Callers should set rec.Hash = rec.ComputeHash() before storing.
func (r ControlEventRecord) ComputeHash() string {
	r.Hash = ""
	b, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
