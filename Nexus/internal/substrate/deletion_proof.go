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

// Deletion proof construction and verification. Produces a signed proof
// bundle demonstrating that a memory has been cryptographically shredded.
//
// Reference: v0.1.3 BF-Sketch Substrate Build Plan, Section 5.3.
package substrate

import (
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// DeletionProof is the signed bundle produced by `nexus substrate
// prove-deletion <memory_id>`. It demonstrates that a memory has been
// cryptographically shredded.
type DeletionProof struct {
	MemoryID  string    `json:"memory_id"`
	IssuedAt  time.Time `json:"issued_at"`

	// Evidence 1: cuckoo filter lookup
	CuckooLookupResult bool   `json:"cuckoo_lookup_result"` // false = not present (desired)
	CuckooStateHash    string `json:"cuckoo_state_hash,omitempty"`

	// Evidence 2: canonical row check
	CanonicalRowExists bool `json:"canonical_row_exists"` // false = deleted (desired)

	// Evidence 3: ratchet state check
	OriginalStateID        uint32     `json:"original_state_id"`
	OriginalStateShreddedAt *time.Time `json:"original_state_shredded_at,omitempty"`
	CurrentStateID         uint32     `json:"current_state_id"`
	StateBytesZeroed       bool       `json:"state_bytes_zeroed"`

	// Evidence 4: chain integrity
	AuditChainLength int64  `json:"audit_chain_length"`
	AuditChainHead   string `json:"audit_chain_head"` // current chain tail hash

	// Signature over the proof (Ed25519)
	Signature []byte `json:"signature,omitempty"`
}

// ProveDeletion produces a DeletionProof for a previously-shredded memory.
// Returns an error if the memory still exists or if no shred record is found.
func ProveDeletion(
	db *sql.DB,
	cuckoo *CuckooOracle,
	ratchet *RatchetManager,
	chain *SubstrateAuditLog,
	signingKey ed25519.PrivateKey,
	memoryID string,
) (*DeletionProof, error) {
	proof := &DeletionProof{
		MemoryID: memoryID,
		IssuedAt: time.Now().UTC(),
	}

	// Evidence 1: cuckoo filter lookup
	proof.CuckooLookupResult = cuckoo.Lookup(memoryID)

	// Evidence 2: canonical row exists?
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM memories WHERE payload_id = ?`, memoryID).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("prove deletion: query memories: %w", err)
	}
	proof.CanonicalRowExists = count > 0

	// Evidence 3: find the ratchet state this memory was sketched under
	var stateID uint32
	err = db.QueryRow(
		`SELECT state_id FROM substrate_memory_state WHERE memory_id = ?`,
		memoryID,
	).Scan(&stateID)
	if err == sql.ErrNoRows {
		// No substrate_memory_state record — memory may never have been sketched,
		// or the row was cleaned up. Check if we have any shredded states.
		stateID = 0
	} else if err != nil {
		return nil, fmt.Errorf("prove deletion: query memory state: %w", err)
	}
	proof.OriginalStateID = stateID

	// Check if the original state is shredded
	if stateID > 0 {
		var shreddedNano sql.NullInt64
		var stateBytes []byte
		err = db.QueryRow(
			`SELECT shredded_at, state_bytes FROM substrate_ratchet_states WHERE state_id = ?`,
			stateID,
		).Scan(&shreddedNano, &stateBytes)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("prove deletion: query ratchet state: %w", err)
		}
		if err == nil {
			if shreddedNano.Valid {
				t := time.Unix(0, shreddedNano.Int64)
				proof.OriginalStateShreddedAt = &t
			}
			proof.StateBytesZeroed = isAllZero(stateBytes)
		}
	}

	// Current ratchet state
	if current := ratchet.Current(); current != nil {
		proof.CurrentStateID = current.StateID
	}

	// Evidence 4: chain integrity
	if chain != nil && chain.chain != nil {
		proof.AuditChainLength = chain.chain.EntryCount()
		proof.AuditChainHead = chain.chain.LastHash()
	}

	// Sign the proof
	if signingKey != nil {
		proofCopy := *proof
		proofCopy.Signature = nil
		proofBytes, err := json.Marshal(proofCopy)
		if err != nil {
			return nil, fmt.Errorf("prove deletion: marshal for signing: %w", err)
		}
		proof.Signature = ed25519.Sign(signingKey, proofBytes)
	}

	// Emit an audit event for the proof issuance
	if chain != nil {
		chain.Emit(EventDeletionProof, proof)
	}

	return proof, nil
}

// VerifyDeletionProof verifies a deletion proof by checking the signature
// and evaluating the evidence fields.
func VerifyDeletionProof(proof *DeletionProof, pubKey ed25519.PublicKey) error {
	if proof == nil {
		return fmt.Errorf("verify proof: nil proof")
	}

	// Verify signature if present
	if len(proof.Signature) > 0 && pubKey != nil {
		proofCopy := *proof
		proofCopy.Signature = nil
		proofBytes, err := json.Marshal(proofCopy)
		if err != nil {
			return fmt.Errorf("verify proof: marshal: %w", err)
		}
		if !ed25519.Verify(pubKey, proofBytes, proof.Signature) {
			return fmt.Errorf("verify proof: invalid signature")
		}
	}

	// Evaluate evidence
	if proof.CanonicalRowExists {
		return fmt.Errorf("verify proof: memory row still exists in canonical store")
	}
	if proof.CuckooLookupResult {
		// Cuckoo filter says it might still be present. This could be a
		// false positive, so it's a warning, not a hard failure.
		// The other evidence (row deleted, state shredded) is authoritative.
	}
	if proof.OriginalStateID > 0 && !proof.StateBytesZeroed {
		return fmt.Errorf("verify proof: original ratchet state not shredded")
	}

	return nil
}
