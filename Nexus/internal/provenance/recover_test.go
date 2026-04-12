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
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&discardWriter{}, nil))
}

type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func buildTestChain(t *testing.T, count int) []json.RawMessage {
	t.Helper()

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	cs := NewChainState()
	genesisJSON, err := cs.Genesis(kp)
	if err != nil {
		t.Fatal(err)
	}

	entries := []json.RawMessage{genesisJSON}

	for i := 1; i < count; i++ {
		prevHash := cs.LastHash()
		record := fmt.Sprintf(`{"record_id":"r%d","operation":"write","prev_audit_hash":"%s"}`, i, prevHash)
		payload := json.RawMessage(record)
		cs.Extend(payload)
		entries = append(entries, payload)
	}

	return entries
}

func TestInspectChainFromEntries_Intact(t *testing.T) {
	entries := buildTestChain(t, 20)
	report := InspectChainFromEntries(entries, testLogger())

	if report.ChainStatus != "intact" {
		t.Errorf("chain_status = %q, want %q", report.ChainStatus, "intact")
	}
	if report.ValidEntries != 20 {
		t.Errorf("valid_entries = %d, want 20", report.ValidEntries)
	}
	if report.GenesisHash == "" {
		t.Error("genesis_hash should not be empty")
	}
}

func TestInspectChainFromEntries_Empty(t *testing.T) {
	report := InspectChainFromEntries(nil, testLogger())
	if report.ChainStatus != "empty" {
		t.Errorf("chain_status = %q, want %q", report.ChainStatus, "empty")
	}
}

func TestInspectChainFromEntries_BrokenLink(t *testing.T) {
	entries := buildTestChain(t, 10)

	// Tamper: replace entry 5 with a different prev_audit_hash.
	entries[5] = json.RawMessage(`{"record_id":"r5","operation":"write","prev_audit_hash":"0000000000000000000000000000000000000000000000000000000000000000"}`)

	report := InspectChainFromEntries(entries, testLogger())

	if report.ChainStatus != "broken_at_index_5" {
		t.Errorf("chain_status = %q, want %q", report.ChainStatus, "broken_at_index_5")
	}
	if report.ValidEntries != 5 {
		t.Errorf("valid_entries = %d, want 5", report.ValidEntries)
	}
	if report.CorruptionIndex != 5 {
		t.Errorf("corruption_index = %d, want 5", report.CorruptionIndex)
	}
}

func TestInspectChainFromEntries_TamperedPayload(t *testing.T) {
	// Build a chain where we manually set prev_audit_hash correctly
	// but then tamper with a payload (changing content but not the hash).
	// The chain link will break because the computed hash of the tampered
	// payload won't match what the next entry expects.
	entries := buildTestChain(t, 5)

	// Replace entry 2's content but keep prev_audit_hash pointing to entry 1's real hash.
	// The chain breaks at entry 3 because entry 3's prev_audit_hash points to
	// the hash of the ORIGINAL entry 2, not the tampered one.
	h := sha256.Sum256(entries[1])
	prevHash := hex.EncodeToString(h[:])
	entries[2] = json.RawMessage(fmt.Sprintf(`{"record_id":"r2-tampered","operation":"read","prev_audit_hash":"%s"}`, prevHash))

	report := InspectChainFromEntries(entries, testLogger())

	// Entry 2 will verify (its prev_audit_hash links to entry 1 correctly).
	// Entry 3 will fail because its prev_audit_hash expects hash of original entry 2.
	if report.ChainStatus == "intact" {
		t.Error("tampered chain should not be intact")
	}
	if report.ValidEntries > 3 {
		t.Errorf("valid_entries = %d, expected <= 3", report.ValidEntries)
	}
}

func TestInspectChainFromEntries_SingleEntry(t *testing.T) {
	entries := buildTestChain(t, 1)
	report := InspectChainFromEntries(entries, testLogger())

	if report.ChainStatus != "intact" {
		t.Errorf("chain_status = %q, want %q", report.ChainStatus, "intact")
	}
	if report.ValidEntries != 1 {
		t.Errorf("valid_entries = %d, want 1", report.ValidEntries)
	}
}
