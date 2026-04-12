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
	"path/filepath"
	"sync"
	"testing"
)

func TestGenesis(t *testing.T) {
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	cs := NewChainState()
	genesisJSON, err := cs.Genesis(kp)
	if err != nil {
		t.Fatalf("Genesis: %v", err)
	}

	if cs.EntryCount() != 1 {
		t.Errorf("entry count = %d, want 1", cs.EntryCount())
	}
	if cs.GenesisHash() == "" {
		t.Error("genesis hash is empty")
	}
	if cs.LastHash() != cs.GenesisHash() {
		t.Error("after genesis, last hash should equal genesis hash")
	}

	// Verify genesis JSON contains expected fields.
	var entry GenesisEntry
	if err := json.Unmarshal(genesisJSON, &entry); err != nil {
		t.Fatalf("unmarshal genesis: %v", err)
	}
	if entry.Event != "chain_genesis" {
		t.Errorf("event = %q, want %q", entry.Event, "chain_genesis")
	}
	if entry.DaemonID != kp.KeyID {
		t.Errorf("daemon_id = %q, want %q", entry.DaemonID, kp.KeyID)
	}

	// Verify hash correctness.
	h := sha256.Sum256(genesisJSON)
	expected := hex.EncodeToString(h[:])
	if cs.GenesisHash() != expected {
		t.Errorf("genesis hash mismatch: got %s, computed %s", cs.GenesisHash(), expected)
	}
}

func TestGenesis_DoubleCall(t *testing.T) {
	kp, _ := GenerateKeyPair()
	cs := NewChainState()
	if _, err := cs.Genesis(kp); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Genesis(kp); err == nil {
		t.Error("second Genesis call should fail")
	}
}

func TestExtend(t *testing.T) {
	kp, _ := GenerateKeyPair()
	cs := NewChainState()
	if _, err := cs.Genesis(kp); err != nil {
		t.Fatal(err)
	}

	genesisHash := cs.GenesisHash()

	payload := []byte(`{"record_id":"r1","operation":"write"}`)
	prevHash, currentHash := cs.Extend(payload)

	if prevHash != genesisHash {
		t.Errorf("prevHash = %s, want genesis %s", prevHash, genesisHash)
	}

	h := sha256.Sum256(payload)
	expected := hex.EncodeToString(h[:])
	if currentHash != expected {
		t.Errorf("currentHash = %s, computed %s", currentHash, expected)
	}

	if cs.EntryCount() != 2 {
		t.Errorf("entry count = %d, want 2", cs.EntryCount())
	}
	if cs.LastHash() != currentHash {
		t.Error("last hash should equal current hash after extend")
	}
}

func TestExtendChain_100Entries(t *testing.T) {
	kp, _ := GenerateKeyPair()
	cs := NewChainState()
	genesisJSON, err := cs.Genesis(kp)
	if err != nil {
		t.Fatal(err)
	}

	// Build a chain of 100 entries and verify it.
	entries := make([]ChainEntry, 0, 101)

	// Genesis entry.
	h := sha256.Sum256(genesisJSON)
	entries = append(entries, ChainEntry{
		Hash:    hex.EncodeToString(h[:]),
		Payload: genesisJSON,
	})

	for i := 0; i < 100; i++ {
		payload := []byte(fmt.Sprintf(`{"record_id":"r%d","seq":%d}`, i, i))
		prevHash, currentHash := cs.Extend(payload)
		entries = append(entries, ChainEntry{
			Hash:     currentHash,
			PrevHash: prevHash,
			Payload:  payload,
		})
	}

	if cs.EntryCount() != 101 {
		t.Errorf("entry count = %d, want 101", cs.EntryCount())
	}

	valid, err := VerifyChain(entries)
	if err != nil {
		t.Fatalf("VerifyChain: %v (valid entries: %d)", err, valid)
	}
	if valid != 101 {
		t.Errorf("valid = %d, want 101", valid)
	}
}

func TestVerifyChain_TamperDetection(t *testing.T) {
	kp, _ := GenerateKeyPair()
	cs := NewChainState()
	genesisJSON, _ := cs.Genesis(kp)

	entries := make([]ChainEntry, 0, 6)
	h := sha256.Sum256(genesisJSON)
	entries = append(entries, ChainEntry{
		Hash:    hex.EncodeToString(h[:]),
		Payload: genesisJSON,
	})

	for i := 0; i < 5; i++ {
		payload := []byte(fmt.Sprintf(`{"seq":%d}`, i))
		prevHash, currentHash := cs.Extend(payload)
		entries = append(entries, ChainEntry{
			Hash:     currentHash,
			PrevHash: prevHash,
			Payload:  payload,
		})
	}

	// Tamper with entry 3's payload.
	tampered := make([]ChainEntry, len(entries))
	copy(tampered, entries)
	tampered[3].Payload = []byte(`{"seq":999}`)

	valid, err := VerifyChain(tampered)
	if err == nil {
		t.Fatal("expected error for tampered chain")
	}
	if valid != 3 {
		t.Errorf("valid = %d, want 3 (break at tampered entry)", valid)
	}
}

func TestVerifyChain_BrokenLink(t *testing.T) {
	kp, _ := GenerateKeyPair()
	cs := NewChainState()
	genesisJSON, _ := cs.Genesis(kp)

	entries := make([]ChainEntry, 0, 4)
	h := sha256.Sum256(genesisJSON)
	entries = append(entries, ChainEntry{
		Hash:    hex.EncodeToString(h[:]),
		Payload: genesisJSON,
	})

	for i := 0; i < 3; i++ {
		payload := []byte(fmt.Sprintf(`{"seq":%d}`, i))
		prevHash, currentHash := cs.Extend(payload)
		entries = append(entries, ChainEntry{
			Hash:     currentHash,
			PrevHash: prevHash,
			Payload:  payload,
		})
	}

	// Break the link at entry 2.
	broken := make([]ChainEntry, len(entries))
	copy(broken, entries)
	broken[2].PrevHash = "0000000000000000000000000000000000000000000000000000000000000000"

	valid, err := VerifyChain(broken)
	if err == nil {
		t.Fatal("expected error for broken link")
	}
	if valid != 2 {
		t.Errorf("valid = %d, want 2", valid)
	}
}

func TestVerifyChain_Empty(t *testing.T) {
	valid, err := VerifyChain(nil)
	if err != nil {
		t.Fatal(err)
	}
	if valid != 0 {
		t.Errorf("valid = %d, want 0", valid)
	}
}

func TestSaveAndRestoreChainState(t *testing.T) {
	kp, _ := GenerateKeyPair()
	cs := NewChainState()
	if _, err := cs.Genesis(kp); err != nil {
		t.Fatal(err)
	}

	payload := []byte(`{"test":"data"}`)
	cs.Extend(payload)

	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")

	if err := cs.SaveChainState(dataDir); err != nil {
		t.Fatalf("SaveChainState: %v", err)
	}

	restored, found, err := RestoreChainState(dataDir)
	if err != nil {
		t.Fatalf("RestoreChainState: %v", err)
	}
	if !found {
		t.Fatal("expected chain state to be found")
	}
	if restored.GenesisHash() != cs.GenesisHash() {
		t.Error("genesis hash mismatch after restore")
	}
	if restored.LastHash() != cs.LastHash() {
		t.Error("last hash mismatch after restore")
	}
	if restored.EntryCount() != cs.EntryCount() {
		t.Error("entry count mismatch after restore")
	}
}

func TestRestoreChainState_NotFound(t *testing.T) {
	cs, found, err := RestoreChainState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected not found on empty directory")
	}
	if cs == nil {
		t.Error("expected non-nil chain state")
	}
}

func TestExtend_ConcurrentSafety(t *testing.T) {
	kp, _ := GenerateKeyPair()
	cs := NewChainState()
	if _, err := cs.Genesis(kp); err != nil {
		t.Fatal(err)
	}

	const goroutines = 10
	const entriesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < entriesPerGoroutine; i++ {
				payload := []byte(fmt.Sprintf(`{"g":%d,"i":%d}`, gID, i))
				cs.Extend(payload)
			}
		}(g)
	}

	wg.Wait()

	expected := int64(1 + goroutines*entriesPerGoroutine)
	if cs.EntryCount() != expected {
		t.Errorf("entry count = %d, want %d", cs.EntryCount(), expected)
	}
}
