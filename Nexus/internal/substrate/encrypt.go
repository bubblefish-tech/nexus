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

package substrate

import (
	"fmt"
	"strconv"

	nexuscrypto "github.com/bubblefish-tech/nexus/internal/crypto"
)

// SubstrateEncryptor provides AES-256-GCM encryption for substrate state tables
// (substrate_ratchet_states and substrate_cuckoo_filter).
//
// Keys are derived via HKDF from the "nexus-substrate-key-v1" domain sub-key.
// Per-row key derivation binds each ciphertext to its specific row, preventing
// cross-row ciphertext reuse.
type SubstrateEncryptor struct {
	subKey [32]byte
}

// NewSubstrateEncryptor creates an encryptor from the master key manager.
// Returns nil when mkm is nil or encryption is not enabled — callers must
// nil-check the return value before use.
func NewSubstrateEncryptor(mkm *nexuscrypto.MasterKeyManager) *SubstrateEncryptor {
	if mkm == nil || !mkm.IsEnabled() {
		return nil
	}
	return &SubstrateEncryptor{subKey: mkm.SubKey("nexus-substrate-key-v1")}
}

// SealRatchetState encrypts the 32-byte ratchet state for the given stateID.
// Row key: HKDF(subKey, stateID-as-decimal, "substrate-ratchet-state").
// AAD: stateID bytes — binds ciphertext to this specific row.
func (e *SubstrateEncryptor) SealRatchetState(stateID uint32, stateBytes [32]byte) ([]byte, error) {
	rowKey, err := nexuscrypto.DeriveRowKey(
		e.subKey,
		strconv.FormatUint(uint64(stateID), 10),
		"substrate-ratchet-state",
	)
	if err != nil {
		return nil, fmt.Errorf("substrate: derive ratchet row key: %w", err)
	}
	aad := []byte(strconv.FormatUint(uint64(stateID), 10))
	return nexuscrypto.SealAES256GCM(rowKey, stateBytes[:], aad)
}

// OpenRatchetState decrypts the encrypted ratchet state bytes for stateID.
func (e *SubstrateEncryptor) OpenRatchetState(stateID uint32, blob []byte) ([32]byte, error) {
	rowKey, err := nexuscrypto.DeriveRowKey(
		e.subKey,
		strconv.FormatUint(uint64(stateID), 10),
		"substrate-ratchet-state",
	)
	if err != nil {
		return [32]byte{}, fmt.Errorf("substrate: derive ratchet row key: %w", err)
	}
	aad := []byte(strconv.FormatUint(uint64(stateID), 10))
	plain, err := nexuscrypto.OpenAES256GCM(rowKey, blob, aad)
	if err != nil {
		return [32]byte{}, fmt.Errorf("substrate: decrypt ratchet state %d: %w", stateID, err)
	}
	if len(plain) != 32 {
		return [32]byte{}, fmt.Errorf("substrate: decrypted ratchet state %d: unexpected length %d", stateID, len(plain))
	}
	var result [32]byte
	copy(result[:], plain)
	return result, nil
}

// SealCuckooFilter encrypts the serialized cuckoo filter bytes.
// Row key: HKDF(subKey, "1", "substrate-cuckoo-filter") — filter_id is always 1.
// AAD: "substrate-cuckoo-filter-1".
func (e *SubstrateEncryptor) SealCuckooFilter(data []byte) ([]byte, error) {
	rowKey, err := nexuscrypto.DeriveRowKey(e.subKey, "1", "substrate-cuckoo-filter")
	if err != nil {
		return nil, fmt.Errorf("substrate: derive cuckoo row key: %w", err)
	}
	return nexuscrypto.SealAES256GCM(rowKey, data, []byte("substrate-cuckoo-filter-1"))
}

// OpenCuckooFilter decrypts the encrypted cuckoo filter bytes.
func (e *SubstrateEncryptor) OpenCuckooFilter(blob []byte) ([]byte, error) {
	rowKey, err := nexuscrypto.DeriveRowKey(e.subKey, "1", "substrate-cuckoo-filter")
	if err != nil {
		return nil, fmt.Errorf("substrate: derive cuckoo row key: %w", err)
	}
	plain, err := nexuscrypto.OpenAES256GCM(rowKey, blob, []byte("substrate-cuckoo-filter-1"))
	if err != nil {
		return nil, fmt.Errorf("substrate: decrypt cuckoo filter: %w", err)
	}
	return plain, nil
}

// WithEncryptor returns an Option that injects enc into a Substrate coordinator.
// It is extracted before component initialization in New() so that the ratchet
// manager and cuckoo oracle are loaded with decryption support from the start.
func WithEncryptor(enc *SubstrateEncryptor) Option {
	return func(s *Substrate) {
		s.encryptor = enc
	}
}
