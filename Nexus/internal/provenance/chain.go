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
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChainState maintains the hash chain for the audit log. Each audit entry
// includes the SHA-256 hash of the previous entry, forming a tamper-evident
// chain from genesis. All operations are safe for concurrent use.
//
// Reference: v0.1.3 Build Plan Phase 4 Subtask 4.3.
type ChainState struct {
	mu          sync.Mutex
	lastHash    string // hex SHA-256 of previous audit entry
	genesisHash string
	entryCount  int64
}

// NewChainState creates an empty chain state. Call Genesis() to initialize
// the chain, or RestoreChainState() to resume from persisted state.
func NewChainState() *ChainState {
	return &ChainState{}
}

// GenesisEntry is the first entry in the audit hash chain. It establishes
// the daemon's cryptographic identity.
type GenesisEntry struct {
	Event      string `json:"event"`
	DaemonID   string `json:"daemon_id"`
	DaemonKey  string `json:"daemon_pubkey"`
	Timestamp  string `json:"timestamp"`
	Signature  string `json:"signature"`
}

// Genesis creates the genesis entry for a new daemon. The entry is signed
// with the daemon's Ed25519 key. Returns the genesis JSON (for WAL append)
// and the genesis hash. Calling Genesis on a non-empty chain returns an error.
func (cs *ChainState) Genesis(daemonKP *KeyPair) ([]byte, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.entryCount > 0 {
		return nil, fmt.Errorf("provenance: chain already has %d entries, cannot create genesis", cs.entryCount)
	}

	entry := GenesisEntry{
		Event:     "chain_genesis",
		DaemonID:  daemonKP.KeyID,
		DaemonKey: hex.EncodeToString(daemonKP.PublicKey),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
	}

	// Sign the entry (without the signature field).
	unsignedJSON, err := json.Marshal(struct {
		Event     string `json:"event"`
		DaemonID  string `json:"daemon_id"`
		DaemonKey string `json:"daemon_pubkey"`
		Timestamp string `json:"timestamp"`
	}{entry.Event, entry.DaemonID, entry.DaemonKey, entry.Timestamp})
	if err != nil {
		return nil, fmt.Errorf("provenance: marshal genesis for signing: %w", err)
	}
	sig := ed25519.Sign(daemonKP.PrivateKey, unsignedJSON)
	entry.Signature = hex.EncodeToString(sig)

	genesisJSON, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("provenance: marshal genesis entry: %w", err)
	}

	h := sha256.Sum256(genesisJSON)
	cs.genesisHash = hex.EncodeToString(h[:])
	cs.lastHash = cs.genesisHash
	cs.entryCount = 1

	return genesisJSON, nil
}

// Extend adds an audit entry to the chain. It sets the prevAuditHash on the
// provided payload (which the caller should include in the audit record before
// marshaling), computes the hash of the full payload, and advances the chain.
//
// Returns (prevHash, currentHash). The caller should set PrevAuditHash on
// the InteractionRecord to prevHash before marshaling.
func (cs *ChainState) Extend(auditPayload []byte) (prevHash, currentHash string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	prevHash = cs.lastHash
	h := sha256.Sum256(auditPayload)
	currentHash = hex.EncodeToString(h[:])
	cs.lastHash = currentHash
	cs.entryCount++

	return prevHash, currentHash
}

// LastHash returns the current tail hash of the chain.
func (cs *ChainState) LastHash() string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.lastHash
}

// GenesisHash returns the hash of the genesis entry.
func (cs *ChainState) GenesisHash() string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.genesisHash
}

// EntryCount returns the number of entries in the chain (including genesis).
func (cs *ChainState) EntryCount() int64 {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.entryCount
}

// ChainEntry represents one element in the audit hash chain for verification.
type ChainEntry struct {
	Hash         string          `json:"hash"`
	PrevHash     string          `json:"prev_hash"`
	Payload      json.RawMessage `json:"payload"`
}

// VerifyChain checks the integrity of a sequence of chain entries.
// The first entry must be the genesis (no PrevHash check).
// Returns the count of valid entries and an error at the first break.
func VerifyChain(entries []ChainEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	// Verify genesis hash matches payload.
	h := sha256.Sum256(entries[0].Payload)
	computed := hex.EncodeToString(h[:])
	if computed != entries[0].Hash {
		return 0, fmt.Errorf("provenance: genesis hash mismatch: computed %s, recorded %s", computed, entries[0].Hash)
	}

	for i := 1; i < len(entries); i++ {
		// Check that PrevHash links to the previous entry's Hash.
		if entries[i].PrevHash != entries[i-1].Hash {
			return i, fmt.Errorf("provenance: chain break at entry %d: prev_hash %s != expected %s",
				i, entries[i].PrevHash, entries[i-1].Hash)
		}
		// Verify this entry's hash matches its payload.
		h := sha256.Sum256(entries[i].Payload)
		computed := hex.EncodeToString(h[:])
		if computed != entries[i].Hash {
			return i, fmt.Errorf("provenance: hash mismatch at entry %d: computed %s, recorded %s",
				i, computed, entries[i].Hash)
		}
	}

	return len(entries), nil
}

// persistedChainState is the on-disk format for chain state.
type persistedChainState struct {
	GenesisHash string `json:"genesis_hash"`
	LastHash    string `json:"last_hash"`
	EntryCount  int64  `json:"entry_count"`
}

// SaveChainState persists the current chain state to a JSON file.
// The file is written atomically to prevent corruption from crashes.
func (cs *ChainState) SaveChainState(dataDir string) error {
	cs.mu.Lock()
	state := persistedChainState{
		GenesisHash: cs.genesisHash,
		LastHash:    cs.lastHash,
		EntryCount:  cs.entryCount,
	}
	cs.mu.Unlock()

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("provenance: marshal chain state: %w", err)
	}

	path := filepath.Join(dataDir, "chain-state.json")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("provenance: create data dir %s: %w", dataDir, err)
	}

	// Atomic write: temp file + rename in same directory.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".chain-state-*")
	if err != nil {
		return fmt.Errorf("provenance: create temp file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("provenance: write chain state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("provenance: sync chain state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("provenance: close chain state: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("provenance: rename chain state: %w", err)
	}

	return nil
}

// RestoreChainState loads chain state from disk. Returns false if the file
// does not exist (first startup). Returns an error for corrupt files.
func RestoreChainState(dataDir string) (*ChainState, bool, error) {
	path := filepath.Join(dataDir, "chain-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewChainState(), false, nil
		}
		return nil, false, fmt.Errorf("provenance: read chain state: %w", err)
	}

	var state persistedChainState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false, fmt.Errorf("provenance: unmarshal chain state: %w", err)
	}

	cs := &ChainState{
		lastHash:    state.LastHash,
		genesisHash: state.GenesisHash,
		entryCount:  state.EntryCount,
	}
	return cs, true, nil
}
