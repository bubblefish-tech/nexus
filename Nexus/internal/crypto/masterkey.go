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

package crypto

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
	cryptorand "crypto/rand"
)

const (
	argon2Time    uint32 = 3
	argon2Memory  uint32 = 65536
	argon2Threads uint8  = 4
	argon2KeyLen  uint32 = 32

	// EnvPassword is the environment variable consulted when no password is passed explicitly.
	EnvPassword = "NEXUS_PASSWORD"
)

// subKeyDomains are the canonical HKDF info strings for each encryption domain.
var subKeyDomains = []string{
	"nexus-config-key-v1",
	"nexus-memory-key-v1",
	"nexus-audit-key-v1",
	"nexus-control-key-v1",
	"nexus-backup-key-v1",
}

// MasterKeyManager derives encryption sub-keys from a user password using
// Argon2id key stretching and HKDF domain separation.
//
// When IsEnabled() is false all sub-keys are zero and encryption is disabled.
type MasterKeyManager struct {
	salt    [32]byte
	subKeys map[string][32]byte
	enabled bool
}

// NewMasterKeyManager creates a MasterKeyManager for the given password and
// salt file path.
//
// Password resolution order:
//  1. The password argument (if non-empty).
//  2. The NEXUS_PASSWORD environment variable (if set).
//  3. Disabled mode (IsEnabled() returns false).
//
// Salt behaviour: if saltPath does not exist a fresh 32-byte random salt is
// generated and written there (0600). If saltPath exists its contents are used.
// The parent directory is created (0700) if absent.
func NewMasterKeyManager(password string, saltPath string) (*MasterKeyManager, error) {
	if password == "" {
		password = os.Getenv(EnvPassword)
	}
	if password == "" {
		return &MasterKeyManager{enabled: false}, nil
	}

	salt, err := loadOrCreateSalt(saltPath)
	if err != nil {
		return nil, err
	}

	masterKey := argon2.IDKey([]byte(password), salt[:], argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	subKeys, err := deriveSubKeys(masterKey)
	if err != nil {
		return nil, err
	}

	return &MasterKeyManager{
		salt:    salt,
		subKeys: subKeys,
		enabled: true,
	}, nil
}

// SubKey returns the 32-byte sub-key for the named domain.
// Returns a zero key if the domain is unknown or encryption is disabled.
func (m *MasterKeyManager) SubKey(domain string) [32]byte {
	return m.subKeys[domain]
}

// IsEnabled reports whether a password was supplied and key derivation succeeded.
func (m *MasterKeyManager) IsEnabled() bool {
	return m.enabled
}

// loadOrCreateSalt reads saltPath or generates and persists a new random salt.
func loadOrCreateSalt(saltPath string) ([32]byte, error) {
	var salt [32]byte

	data, err := os.ReadFile(saltPath)
	if err == nil {
		if len(data) != 32 {
			return salt, fmt.Errorf("crypto: salt file %q has unexpected length %d (want 32)", saltPath, len(data))
		}
		copy(salt[:], data)
		return salt, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return salt, fmt.Errorf("crypto: read salt file: %w", err)
	}

	if _, err := cryptorand.Read(salt[:]); err != nil {
		return salt, fmt.Errorf("crypto: generate salt: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(saltPath), 0700); err != nil {
		return salt, fmt.Errorf("crypto: create salt directory: %w", err)
	}
	if err := os.WriteFile(saltPath, salt[:], 0600); err != nil {
		return salt, fmt.Errorf("crypto: write salt file: %w", err)
	}
	return salt, nil
}

// deriveSubKeys runs HKDF over masterKey for each canonical domain.
func deriveSubKeys(masterKey []byte) (map[string][32]byte, error) {
	prk := ActiveProfile.HKDFExtract(masterKey, nil)

	subKeys := make(map[string][32]byte, len(subKeyDomains))
	for _, domain := range subKeyDomains {
		expanded, err := ActiveProfile.HKDFExpand(prk, []byte(domain), 32)
		if err != nil {
			return nil, fmt.Errorf("crypto: derive sub-key %q: %w", domain, err)
		}
		var key [32]byte
		copy(key[:], expanded)
		subKeys[domain] = key
	}
	return subKeys, nil
}
