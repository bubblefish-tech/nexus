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
	"encoding/json"
	"fmt"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

// PayloadCrypto encrypts and decrypts audit event payloads using per-record
// HKDF-derived AES-256-GCM keys under the audit sub-key.
//
// Selective disclosure: chain metadata (record_id, prev_hash, timestamp,
// event_type, hash) stays plaintext so chain integrity can be verified
// without the decryption key. Sensitive operational fields are encrypted.
type PayloadCrypto struct {
	subKey [32]byte
}

// NewPayloadCrypto creates a PayloadCrypto from mkm. Returns nil when mkm is
// nil or disabled so call sites can nil-check without additional branching.
func NewPayloadCrypto(mkm *nexuscrypto.MasterKeyManager) *PayloadCrypto {
	if mkm == nil || !mkm.IsEnabled() {
		return nil
	}
	sk := mkm.SubKey("nexus-audit-key-v1")
	return &PayloadCrypto{subKey: sk}
}

// seal encrypts plaintext for recordID.
// Per-record key: HKDF(auditSubKey, recordID, "audit-payload").
// AAD = recordID — binds the ciphertext to this specific record.
// Returns nonce(12) || ciphertext || GCM-tag(16).
func (pc *PayloadCrypto) seal(plaintext []byte, recordID string) ([]byte, error) {
	rowKey, err := nexuscrypto.DeriveRowKey(pc.subKey, recordID, "audit-payload")
	if err != nil {
		return nil, fmt.Errorf("audit: derive row key: %w", err)
	}
	return nexuscrypto.SealAES256GCM(rowKey, plaintext, []byte(recordID))
}

// open decrypts a blob produced by seal for the given recordID.
func (pc *PayloadCrypto) open(blob []byte, recordID string) ([]byte, error) {
	rowKey, err := nexuscrypto.DeriveRowKey(pc.subKey, recordID, "audit-payload")
	if err != nil {
		return nil, fmt.Errorf("audit: derive row key: %w", err)
	}
	return nexuscrypto.OpenAES256GCM(rowKey, blob, []byte(recordID))
}

// interactionPayload holds the sensitive operational fields of InteractionRecord.
// When encryption is enabled these are marshaled to JSON, encrypted, and stored
// in InteractionRecord.PayloadEncrypted. The corresponding plaintext fields in
// the outer record are zeroed so only chain metadata remains readable.
type interactionPayload struct {
	Source                    string   `json:"source"`
	ActorType                 string   `json:"actor_type"`
	ActorID                   string   `json:"actor_id"`
	EffectiveIP               string   `json:"effective_ip"`
	RequestID                 string   `json:"request_id"`
	OperationType             string   `json:"operation_type"`
	Endpoint                  string   `json:"endpoint"`
	HTTPMethod                string   `json:"http_method"`
	HTTPStatusCode            int      `json:"http_status_code"`
	PayloadID                 string   `json:"payload_id,omitempty"`
	Destination               string   `json:"destination,omitempty"`
	Subject                   string   `json:"subject,omitempty"`
	IdempotencyKey            string   `json:"idempotency_key,omitempty"`
	IsDuplicate               bool     `json:"is_duplicate,omitempty"`
	SensitivityLabelsSet      []string `json:"sensitivity_labels_set,omitempty"`
	RetrievalProfile          string   `json:"retrieval_profile,omitempty"`
	StagesHit                 []string `json:"stages_hit,omitempty"`
	ResultCount               int      `json:"result_count,omitempty"`
	CacheHit                  bool     `json:"cache_hit,omitempty"`
	PolicyDecision            string   `json:"policy_decision"`
	PolicyReason              string   `json:"policy_reason,omitempty"`
	SensitivityLabelsFiltered []string `json:"sensitivity_labels_filtered,omitempty"`
	TierFiltered              bool     `json:"tier_filtered,omitempty"`
	LatencyMs                 float64  `json:"latency_ms"`
	WALAppendMs               float64  `json:"wal_append_ms,omitempty"`
	CRC32                     string   `json:"crc32"`
}

// controlEventPayload holds the sensitive fields of ControlEventRecord.
type controlEventPayload struct {
	Actor      string          `json:"actor"`
	ActorType  string          `json:"actor_type"`
	TargetID   string          `json:"target_id"`
	TargetType string          `json:"target_type"`
	AgentID    string          `json:"agent_id"`
	Capability string          `json:"capability"`
	EntityJSON json.RawMessage `json:"entity,omitempty"`
	Decision   string          `json:"decision"`
	Reason     string          `json:"reason,omitempty"`
}

// encryptInteractionPayload extracts payload fields from rec, encrypts them,
// stores the blob in rec.PayloadEncrypted, and zeros the plaintext fields.
// rec.RecordID must be set before calling.
func encryptInteractionPayload(pc *PayloadCrypto, rec *InteractionRecord) error {
	p := interactionPayload{
		Source:                    rec.Source,
		ActorType:                 rec.ActorType,
		ActorID:                   rec.ActorID,
		EffectiveIP:               rec.EffectiveIP,
		RequestID:                 rec.RequestID,
		OperationType:             rec.OperationType,
		Endpoint:                  rec.Endpoint,
		HTTPMethod:                rec.HTTPMethod,
		HTTPStatusCode:            rec.HTTPStatusCode,
		PayloadID:                 rec.PayloadID,
		Destination:               rec.Destination,
		Subject:                   rec.Subject,
		IdempotencyKey:            rec.IdempotencyKey,
		IsDuplicate:               rec.IsDuplicate,
		SensitivityLabelsSet:      rec.SensitivityLabelsSet,
		RetrievalProfile:          rec.RetrievalProfile,
		StagesHit:                 rec.StagesHit,
		ResultCount:               rec.ResultCount,
		CacheHit:                  rec.CacheHit,
		PolicyDecision:            rec.PolicyDecision,
		PolicyReason:              rec.PolicyReason,
		SensitivityLabelsFiltered: rec.SensitivityLabelsFiltered,
		TierFiltered:              rec.TierFiltered,
		LatencyMs:                 rec.LatencyMs,
		WALAppendMs:               rec.WALAppendMs,
		CRC32:                     rec.CRC32,
	}
	pt, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("audit: marshal interaction payload: %w", err)
	}
	blob, err := pc.seal(pt, rec.RecordID)
	if err != nil {
		return err
	}

	rec.Source = ""
	rec.ActorType = ""
	rec.ActorID = ""
	rec.EffectiveIP = ""
	rec.RequestID = ""
	rec.OperationType = ""
	rec.Endpoint = ""
	rec.HTTPMethod = ""
	rec.HTTPStatusCode = 0
	rec.PayloadID = ""
	rec.Destination = ""
	rec.Subject = ""
	rec.IdempotencyKey = ""
	rec.IsDuplicate = false
	rec.SensitivityLabelsSet = nil
	rec.RetrievalProfile = ""
	rec.StagesHit = nil
	rec.ResultCount = 0
	rec.CacheHit = false
	rec.PolicyDecision = ""
	rec.PolicyReason = ""
	rec.SensitivityLabelsFiltered = nil
	rec.TierFiltered = false
	rec.LatencyMs = 0
	rec.WALAppendMs = 0
	rec.CRC32 = ""

	rec.PayloadEncrypted = blob
	rec.EncryptionVersion = 1
	return nil
}

// DecryptInteractionPayload decrypts rec.PayloadEncrypted and fills the
// plaintext fields. Returns nil without modifying rec when the record is
// already plaintext (EncryptionVersion != 1).
func DecryptInteractionPayload(pc *PayloadCrypto, rec *InteractionRecord) error {
	if rec.EncryptionVersion != 1 || len(rec.PayloadEncrypted) == 0 {
		return nil
	}
	pt, err := pc.open(rec.PayloadEncrypted, rec.RecordID)
	if err != nil {
		return fmt.Errorf("audit: decrypt interaction payload: %w", err)
	}
	var p interactionPayload
	if err := json.Unmarshal(pt, &p); err != nil {
		return fmt.Errorf("audit: unmarshal interaction payload: %w", err)
	}
	rec.Source = p.Source
	rec.ActorType = p.ActorType
	rec.ActorID = p.ActorID
	rec.EffectiveIP = p.EffectiveIP
	rec.RequestID = p.RequestID
	rec.OperationType = p.OperationType
	rec.Endpoint = p.Endpoint
	rec.HTTPMethod = p.HTTPMethod
	rec.HTTPStatusCode = p.HTTPStatusCode
	rec.PayloadID = p.PayloadID
	rec.Destination = p.Destination
	rec.Subject = p.Subject
	rec.IdempotencyKey = p.IdempotencyKey
	rec.IsDuplicate = p.IsDuplicate
	rec.SensitivityLabelsSet = p.SensitivityLabelsSet
	rec.RetrievalProfile = p.RetrievalProfile
	rec.StagesHit = p.StagesHit
	rec.ResultCount = p.ResultCount
	rec.CacheHit = p.CacheHit
	rec.PolicyDecision = p.PolicyDecision
	rec.PolicyReason = p.PolicyReason
	rec.SensitivityLabelsFiltered = p.SensitivityLabelsFiltered
	rec.TierFiltered = p.TierFiltered
	rec.LatencyMs = p.LatencyMs
	rec.WALAppendMs = p.WALAppendMs
	rec.CRC32 = p.CRC32
	rec.PayloadEncrypted = nil
	rec.EncryptionVersion = 0
	return nil
}

// encryptControlPayload extracts payload fields from rec, encrypts them,
// stores the blob in rec.PayloadEncrypted, and zeros the plaintext fields.
// rec.RecordID must be set before calling.
func encryptControlPayload(pc *PayloadCrypto, rec *ControlEventRecord) error {
	p := controlEventPayload{
		Actor:      rec.Actor,
		ActorType:  rec.ActorType,
		TargetID:   rec.TargetID,
		TargetType: rec.TargetType,
		AgentID:    rec.AgentID,
		Capability: rec.Capability,
		EntityJSON: rec.EntityJSON,
		Decision:   rec.Decision,
		Reason:     rec.Reason,
	}
	pt, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("audit: marshal control payload: %w", err)
	}
	blob, err := pc.seal(pt, rec.RecordID)
	if err != nil {
		return err
	}

	rec.Actor = ""
	rec.ActorType = ""
	rec.TargetID = ""
	rec.TargetType = ""
	rec.AgentID = ""
	rec.Capability = ""
	rec.EntityJSON = nil
	rec.Decision = ""
	rec.Reason = ""

	rec.PayloadEncrypted = blob
	rec.EncryptionVersion = 1
	return nil
}

// DecryptControlPayload decrypts rec.PayloadEncrypted and fills the plaintext
// fields. Returns nil without modifying rec when the record is already
// plaintext (EncryptionVersion != 1).
func DecryptControlPayload(pc *PayloadCrypto, rec *ControlEventRecord) error {
	if rec.EncryptionVersion != 1 || len(rec.PayloadEncrypted) == 0 {
		return nil
	}
	pt, err := pc.open(rec.PayloadEncrypted, rec.RecordID)
	if err != nil {
		return fmt.Errorf("audit: decrypt control payload: %w", err)
	}
	var p controlEventPayload
	if err := json.Unmarshal(pt, &p); err != nil {
		return fmt.Errorf("audit: unmarshal control payload: %w", err)
	}
	rec.Actor = p.Actor
	rec.ActorType = p.ActorType
	rec.TargetID = p.TargetID
	rec.TargetType = p.TargetType
	rec.AgentID = p.AgentID
	rec.Capability = p.Capability
	rec.EntityJSON = p.EntityJSON
	rec.Decision = p.Decision
	rec.Reason = p.Reason
	rec.PayloadEncrypted = nil
	rec.EncryptionVersion = 0
	return nil
}
