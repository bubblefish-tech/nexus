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

// Package provenance implements cryptographic provenance for BubbleFish Nexus.
//
// It provides Ed25519 key management, signed write envelopes, hash-chained
// audit logs, Merkle root computation, and proof bundle construction for
// verifiable memory integrity.
//
// Reference: v0.1.3 Build Plan Phase 4.
package provenance

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bubblefish-tech/nexus/internal/secrets"
)

const (
	// ed25519SeedSize is the size in bytes of an Ed25519 seed.
	ed25519SeedSize = ed25519.SeedSize // 32

	// fingerprintLen is the number of bytes from the SHA-256 of the public
	// key used as the key identifier. 16 bytes = 32 hex chars.
	fingerprintLen = 16

	// daemonKeyName is the secret name for the daemon's Ed25519 key.
	daemonKeyName = "daemon.ed25519"
)

// KeyPair holds an Ed25519 signing keypair and its computed fingerprint.
type KeyPair struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
	// KeyID is the hex-encoded first 16 bytes of SHA-256(PublicKey).
	KeyID string
}

// GenerateKeyPair creates a new Ed25519 keypair from cryptographic randomness.
func GenerateKeyPair() (*KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("provenance: generate ed25519 key: %w", err)
	}
	return &KeyPair{
		PublicKey:  pub,
		PrivateKey: priv,
		KeyID:      Fingerprint(pub),
	}, nil
}

// KeyPairFromSeed reconstructs an Ed25519 keypair from a 32-byte seed.
func KeyPairFromSeed(seed []byte) (*KeyPair, error) {
	if len(seed) != ed25519SeedSize {
		return nil, fmt.Errorf("provenance: invalid seed size %d (want %d)", len(seed), ed25519SeedSize)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return &KeyPair{
		PublicKey:  pub,
		PrivateKey: priv,
		KeyID:      Fingerprint(pub),
	}, nil
}

// Fingerprint computes the key identifier: hex(SHA256(pub)[:16]).
func Fingerprint(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:fingerprintLen])
}

// LoadOrGenerateSourceKey loads the Ed25519 key for the named source from
// the secrets directory. If no key exists, a new one is generated and stored.
// Keys are stored as 32-byte seeds at sources/<name>.ed25519.
func LoadOrGenerateSourceKey(sd *secrets.Dir, sourceName string) (*KeyPair, error) {
	relPath := fmt.Sprintf("sources/%s.ed25519", sourceName)
	data, err := sd.ReadSecretPath(relPath)
	if err == nil {
		return KeyPairFromSeed(data)
	}

	kp, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	if err := sd.WriteSecretPath(relPath, kp.PrivateKey.Seed()); err != nil {
		return nil, fmt.Errorf("provenance: write source key %q: %w", sourceName, err)
	}
	return kp, nil
}

// LoadSourceKey loads the Ed25519 key for the named source. Returns an error
// if the key does not exist.
func LoadSourceKey(sd *secrets.Dir, sourceName string) (*KeyPair, error) {
	relPath := fmt.Sprintf("sources/%s.ed25519", sourceName)
	data, err := sd.ReadSecretPath(relPath)
	if err != nil {
		return nil, fmt.Errorf("provenance: load source key %q: %w", sourceName, err)
	}
	return KeyPairFromSeed(data)
}

// LoadOrGenerateDaemonKey loads or generates the daemon-wide Ed25519 key.
// This key signs genesis entries, Merkle roots, and query attestations.
func LoadOrGenerateDaemonKey(sd *secrets.Dir) (*KeyPair, error) {
	data, err := sd.ReadSecret(daemonKeyName)
	if err == nil {
		return KeyPairFromSeed(data)
	}

	kp, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	if err := sd.WriteSecret(daemonKeyName, kp.PrivateKey.Seed()); err != nil {
		return nil, fmt.Errorf("provenance: write daemon key: %w", err)
	}
	return kp, nil
}

// rotationAttestation is the JSON structure signed when rotating a key.
type rotationAttestation struct {
	Event          string `json:"event"`
	OldFingerprint string `json:"old_fingerprint"`
	NewFingerprint string `json:"new_fingerprint"`
	Timestamp      string `json:"timestamp"`
}

// RotateSourceKey generates a new keypair for the source, signs a rotation
// attestation with the new key (proving possession), and writes both the
// new key and the attestation log. Returns the new keypair.
func RotateSourceKey(sd *secrets.Dir, sourceName string) (*KeyPair, error) {
	// Load old key to get its fingerprint.
	oldKP, err := LoadSourceKey(sd, sourceName)
	if err != nil {
		return nil, fmt.Errorf("provenance: cannot rotate — no existing key for %q: %w", sourceName, err)
	}

	// Generate new keypair.
	newKP, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}

	// Build and sign rotation attestation.
	att := rotationAttestation{
		Event:          "key_rotation",
		OldFingerprint: oldKP.KeyID,
		NewFingerprint: newKP.KeyID,
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	attJSON, err := json.Marshal(att)
	if err != nil {
		return nil, fmt.Errorf("provenance: marshal rotation attestation: %w", err)
	}
	sig := ed25519.Sign(newKP.PrivateKey, attJSON)

	// Rotation log entry: attestation JSON + newline + hex signature.
	logEntry := append(attJSON, '\n')
	logEntry = append(logEntry, []byte(hex.EncodeToString(sig))...)

	// Write rotation log first (evidence before key swap).
	logPath := fmt.Sprintf("sources/%s.ed25519.rotation-log", sourceName)
	if err := sd.WriteSecretPath(logPath, logEntry); err != nil {
		return nil, fmt.Errorf("provenance: write rotation log: %w", err)
	}

	// Overwrite key.
	keyPath := fmt.Sprintf("sources/%s.ed25519", sourceName)
	if err := sd.WriteSecretPath(keyPath, newKP.PrivateKey.Seed()); err != nil {
		return nil, fmt.Errorf("provenance: write rotated key: %w", err)
	}

	return newKP, nil
}
