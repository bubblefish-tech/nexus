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
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// EncryptedEmbedding is the on-disk representation of an encrypted embedding.
// Ciphertext and nonce are stored in separate columns (embedding_ciphertext,
// embedding_nonce) on the memories table.
type EncryptedEmbedding struct {
	Ciphertext []byte // ciphertext || 16-byte GCM authentication tag
	Nonce      []byte // 12 bytes
}

// EncryptEmbedding encrypts a full-precision embedding with AES-256-GCM.
// Returns the ciphertext (with authentication tag appended) and the nonce.
//
// The caller is responsible for passing a key derived via DeriveEmbeddingKey.
// The caller is responsible for zeroizing the key buffer after this call.
//
// The nonce is 12 bytes from crypto/rand. Per-memory key derivation via HKDF
// means no two memories share a key, so nonce collisions between memories
// are impossible. Within a single memory, fresh random nonces make
// collisions cryptographically negligible (birthday bound at 2^48 per key).
//
// Note: v0.1.3 uses standard AES-256-GCM (not GCM-SIV). Per-memory key
// derivation provides equivalent security in our threat model because each
// memory has its own unique key. Release notes say "AES-256-GCM with
// per-memory key derivation" — not "AES-256-GCM-SIV".
func EncryptEmbedding(key [32]byte, plaintext []byte) (*EncryptedEmbedding, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("aes new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm new: %w", err)
	}

	nonce := make([]byte, aead.NonceSize()) // 12 bytes for GCM
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	return &EncryptedEmbedding{
		Ciphertext: ciphertext,
		Nonce:      nonce,
	}, nil
}

// DecryptEmbedding decrypts an encrypted embedding.
// Returns ErrEmbeddingUnreachable if decryption fails — this is the expected
// failure mode after a shred-seed delete (the key was derived from a shredded
// ratchet state and does not match the original encryption key).
func DecryptEmbedding(key [32]byte, enc *EncryptedEmbedding) ([]byte, error) {
	if enc == nil {
		return nil, errors.New("decrypt embedding: nil input")
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("aes new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm new: %w", err)
	}

	plaintext, err := aead.Open(nil, enc.Nonce, enc.Ciphertext, nil)
	if err != nil {
		// AEAD authentication failure. This is the expected behavior when
		// the key was derived from a shredded state and doesn't match the
		// original key.
		return nil, ErrEmbeddingUnreachable
	}
	return plaintext, nil
}

// encryptEmbeddingWithNonce is a test-only variant that accepts a fixed nonce.
// Production code must use EncryptEmbedding which generates a fresh random nonce.
func encryptEmbeddingWithNonce(key [32]byte, plaintext, nonce []byte) (*EncryptedEmbedding, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("aes new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm new: %w", err)
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("nonce size must be %d, got %d", aead.NonceSize(), len(nonce))
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	return &EncryptedEmbedding{
		Ciphertext: ciphertext,
		Nonce:      nonce,
	}, nil
}
