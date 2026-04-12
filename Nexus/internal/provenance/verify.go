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
	"runtime"
	"sync"
)

// VerifyResult holds the outcome of proof bundle verification.
type VerifyResult struct {
	Valid          bool   `json:"valid"`
	SignatureValid *bool  `json:"signature_valid,omitempty"` // nil when unsigned
	ChainValid     bool   `json:"chain_valid"`
	DaemonKnown    bool   `json:"daemon_known"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
}

// Error codes for verification failures.
const (
	ErrCodeInvalidSignature = "invalid_signature"
	ErrCodeUnknownDaemon    = "unknown_daemon"
	ErrCodeChainMismatch    = "chain_mismatch"
	ErrCodeInvalidBundle    = "invalid_bundle"
)

// VerifyProofBundle performs full cryptographic verification of a proof bundle.
// It checks:
//  1. Source signature over the signable envelope (if signed)
//  2. Audit chain integrity from genesis to tip
//  3. Daemon identity (genesis entry contains matching daemon pubkey)
//
// Chain verification is parallelized for large chains.
func VerifyProofBundle(bundle *ProofBundle) *VerifyResult {
	result := &VerifyResult{
		DaemonKnown: true,
		ChainValid:  true,
	}

	// Validate bundle version.
	if bundle.Version != 1 {
		result.Valid = false
		result.ErrorCode = ErrCodeInvalidBundle
		result.ErrorMessage = fmt.Sprintf("unsupported proof bundle version %d", bundle.Version)
		return result
	}

	// Step 0: Verify content hash matches content.
	if bundle.Memory.Content != "" && bundle.Memory.ContentHash != "" {
		computed := ContentHash(bundle.Memory.Content)
		if computed != bundle.Memory.ContentHash {
			result.Valid = false
			result.ErrorCode = ErrCodeInvalidSignature
			result.ErrorMessage = "content hash does not match content — data has been tampered"
			return result
		}
	}

	// Step 1: Verify source signature (if present).
	if bundle.Signature != "" && bundle.SourcePubKey != "" {
		sigValid := verifySourceSignature(bundle)
		result.SignatureValid = &sigValid
		if !sigValid {
			result.Valid = false
			result.ErrorCode = ErrCodeInvalidSignature
			result.ErrorMessage = "source Ed25519 signature does not match the signable envelope"
			return result
		}
	}

	// Step 2: Verify daemon identity in genesis.
	if bundle.DaemonPubKey != "" && len(bundle.GenesisEntry) > 0 {
		var genesis GenesisEntry
		if err := json.Unmarshal(bundle.GenesisEntry, &genesis); err != nil {
			result.Valid = false
			result.DaemonKnown = false
			result.ErrorCode = ErrCodeUnknownDaemon
			result.ErrorMessage = "cannot parse genesis entry"
			return result
		}
		if genesis.DaemonKey != bundle.DaemonPubKey {
			result.Valid = false
			result.DaemonKnown = false
			result.ErrorCode = ErrCodeUnknownDaemon
			result.ErrorMessage = "daemon pubkey in genesis does not match bundle daemon_pubkey"
			return result
		}
	}

	// Step 3: Verify audit chain integrity.
	if len(bundle.AuditChain) > 0 {
		chainErr := verifyChainParallel(bundle.AuditChain)
		if chainErr != nil {
			result.Valid = false
			result.ChainValid = false
			result.ErrorCode = ErrCodeChainMismatch
			result.ErrorMessage = chainErr.Error()
			return result
		}
	}

	result.Valid = true
	return result
}

// verifySourceSignature checks the Ed25519 signature in the proof bundle.
func verifySourceSignature(bundle *ProofBundle) bool {
	pubBytes, err := hex.DecodeString(bundle.SourcePubKey)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		return false
	}
	pub := ed25519.PublicKey(pubBytes)

	env := SignableEnvelope{
		SourceName:     bundle.Memory.Source,
		Timestamp:      bundle.Memory.Timestamp,
		IdempotencyKey: bundle.Memory.IdempotencyKey,
		ContentHash:    bundle.Memory.ContentHash,
	}

	valid, err := VerifyEnvelope(env, bundle.Signature, pub)
	if err != nil {
		return false
	}
	return valid
}

// verifyChainParallel verifies the audit chain using parallel workers for
// large chains. For chains under 1000 entries, it falls back to sequential.
func verifyChainParallel(entries []ChainEntry) error {
	if len(entries) == 0 {
		return nil
	}

	// Verify genesis hash.
	h := sha256.Sum256(entries[0].Payload)
	if hex.EncodeToString(h[:]) != entries[0].Hash {
		return fmt.Errorf("genesis hash mismatch")
	}

	n := len(entries)
	if n < 1000 {
		// Sequential verification for small chains.
		return verifyChainRange(entries, 1, n)
	}

	// Parallel verification: split into chunks.
	workers := runtime.NumCPU()
	if workers > 8 {
		workers = 8
	}
	chunkSize := (n - 1) / workers
	if chunkSize < 100 {
		chunkSize = 100
		workers = (n - 1) / chunkSize
		if workers < 1 {
			workers = 1
		}
	}

	var (
		wg      sync.WaitGroup
		errOnce sync.Once
		firstErr error
	)

	for w := 0; w < workers; w++ {
		start := 1 + w*chunkSize
		end := start + chunkSize
		if w == workers-1 || end > n {
			end = n
		}
		if start >= n {
			break
		}

		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			if err := verifyChainRange(entries, s, e); err != nil {
				errOnce.Do(func() { firstErr = err })
			}
		}(start, end)
	}

	wg.Wait()
	return firstErr
}

// verifyChainRange verifies entries[start:end] checking hash links and
// payload hash integrity.
func verifyChainRange(entries []ChainEntry, start, end int) error {
	for i := start; i < end; i++ {
		// Check link to previous entry.
		if entries[i].PrevHash != entries[i-1].Hash {
			return fmt.Errorf("chain break at entry %d: prev_hash %s != expected %s",
				i, entries[i].PrevHash, entries[i-1].Hash)
		}
		// Check hash matches payload.
		h := sha256.Sum256(entries[i].Payload)
		computed := hex.EncodeToString(h[:])
		if computed != entries[i].Hash {
			return fmt.Errorf("hash mismatch at entry %d: computed %s, recorded %s",
				i, computed, entries[i].Hash)
		}
	}
	return nil
}
