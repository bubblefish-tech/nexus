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

package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
)

// ForensicReport is the result of a chain inspection.
type ForensicReport struct {
	TotalEntries     int    `json:"total_entries"`
	ValidEntries     int    `json:"valid_entries"`
	CorruptionIndex  int    `json:"corruption_index,omitempty"`  // index of the first corrupt entry
	CorruptEntryID   string `json:"corrupt_entry_id,omitempty"`
	ChainStatus      string `json:"chain_status"`      // "intact" or "broken_at_index_N"
	Recommendation   string `json:"recommendation"`
	GenesisHash      string `json:"genesis_hash,omitempty"`
	LastValidHash    string `json:"last_valid_hash,omitempty"`
}

// auditPayloadID is used to extract the record_id from audit entries.
type auditPayloadID struct {
	RecordID string `json:"record_id"`
}

// InspectChainFromEntries verifies the hash chain formed by a sequence of
// raw audit entry payloads. Each entry must have a prev_audit_hash field
// that links to the SHA-256 of the previous entry.
//
// The first entry is the genesis entry (prev_audit_hash is empty or absent).
// Returns a ForensicReport describing the chain's integrity.
func InspectChainFromEntries(entries []json.RawMessage, logger *slog.Logger) *ForensicReport {
	report := &ForensicReport{
		TotalEntries: len(entries),
	}

	if len(entries) == 0 {
		report.ChainStatus = "empty"
		report.Recommendation = "No audit entries found. No action needed."
		return report
	}

	// Verify genesis hash.
	genesisHash := hashPayload(entries[0])
	report.GenesisHash = genesisHash

	type chainField struct {
		PrevAuditHash string `json:"prev_audit_hash"`
	}

	prevHash := genesisHash

	for i := 1; i < len(entries); i++ {
		// Extract prev_audit_hash from the entry.
		var cf chainField
		if err := json.Unmarshal(entries[i], &cf); err != nil {
			report.ValidEntries = i
			report.CorruptionIndex = i
			report.ChainStatus = fmt.Sprintf("broken_at_index_%d", i)
			report.Recommendation = fmt.Sprintf("Entry at index %d cannot be parsed. Run 'nexus audit recover' to truncate corrupt entries.", i)

			var pid auditPayloadID
			_ = json.Unmarshal(entries[i], &pid)
			report.CorruptEntryID = pid.RecordID
			report.LastValidHash = prevHash
			return report
		}

		// Check chain link.
		if cf.PrevAuditHash != prevHash {
			report.ValidEntries = i
			report.CorruptionIndex = i
			report.ChainStatus = fmt.Sprintf("broken_at_index_%d", i)
			report.Recommendation = fmt.Sprintf(
				"Chain break at entry %d: prev_audit_hash=%q expected=%q. "+
					"Run 'nexus audit recover' with --truncate to remove entries from index %d onward.",
				i, cf.PrevAuditHash, prevHash, i)

			var pid auditPayloadID
			_ = json.Unmarshal(entries[i], &pid)
			report.CorruptEntryID = pid.RecordID
			report.LastValidHash = prevHash

			if logger != nil {
				logger.Warn("chain break detected",
					"index", i,
					"expected_prev_hash", prevHash,
					"actual_prev_hash", cf.PrevAuditHash,
				)
			}
			return report
		}

		// Advance chain.
		currentHash := hashPayload(entries[i])
		prevHash = currentHash
	}

	report.ValidEntries = len(entries)
	report.ChainStatus = "intact"
	report.LastValidHash = prevHash
	report.Recommendation = "Audit chain is intact. No action needed."
	return report
}

// hashPayload computes the hex SHA-256 of a raw JSON payload.
func hashPayload(payload json.RawMessage) string {
	h := sha256.Sum256(payload)
	return hex.EncodeToString(h[:])
}
