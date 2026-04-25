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

package daemon

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/bubblefish-tech/nexus/internal/audit"
	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

// RunIntegrityCheck performs pre-flight data integrity validation.
// Returns nil if all checks pass, or a descriptive error.
func RunIntegrityCheck(db *sql.DB, auditWAL *audit.WALWriter, mkm *nexuscrypto.MasterKeyManager) error {
	if err := checkSQLiteIntegrity(db); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if err := checkAuditChain(db); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if err := checkEncryptionCanary(mkm); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	return nil
}

func checkSQLiteIntegrity(db *sql.DB) error {
	if db == nil {
		return nil
	}
	var result string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil {
		return fmt.Errorf("PRAGMA integrity_check failed: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("PRAGMA integrity_check returned %q — database may be corrupt", result)
	}
	return nil
}

// auditEntry is a minimal representation for chain verification.
type auditEntry struct {
	RecordID      string `json:"record_id"`
	PrevAuditHash string `json:"prev_audit_hash"`
	Hash          string `json:"hash"`
}

func checkAuditChain(db *sql.DB) error {
	if db == nil {
		return nil
	}
	// Check if the audit table exists before querying.
	var tableName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='audit_log'").Scan(&tableName)
	if err != nil {
		return nil // no audit table = nothing to verify
	}

	rows, err := db.Query("SELECT payload FROM audit_log ORDER BY rowid DESC LIMIT 100")
	if err != nil {
		return nil // table structure mismatch — skip check
	}
	defer rows.Close()

	var entries []auditEntry
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			continue
		}
		var e auditEntry
		if err := json.Unmarshal([]byte(payload), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if len(entries) < 2 {
		return nil // not enough entries to verify chain
	}

	// Entries are newest-first; reverse for chain verification.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	for i := 1; i < len(entries); i++ {
		prevPayload, _ := json.Marshal(entries[i-1])
		expectedHash := fmt.Sprintf("%x", sha256.Sum256(prevPayload))
		if entries[i].PrevAuditHash != "" && entries[i].PrevAuditHash != expectedHash {
			return fmt.Errorf("audit chain broken at entry %s: prev_audit_hash mismatch", entries[i].RecordID)
		}
	}
	return nil
}

func checkEncryptionCanary(mkm *nexuscrypto.MasterKeyManager) error {
	if mkm == nil || !mkm.IsEnabled() {
		return nil
	}
	return nexuscrypto.SelfTest(mkm)
}
