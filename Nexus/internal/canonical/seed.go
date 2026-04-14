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

package canonical

import (
	"crypto/rand"
	"errors"
	"os"

	"github.com/BubbleFish-Nexus/internal/secrets"
)

const seedFileName = "canonical.seed"

// LoadOrCreateSeed loads the canonical seed from the secrets directory, or
// creates a new one if it does not exist. The seed is 32 bytes of
// crypto/rand entropy.
//
// The seed is permanent: once created, it must not change. Changing the seed
// invalidates all existing canonicalized vectors and sketches.
func LoadOrCreateSeed(sd *secrets.Dir) ([32]byte, error) {
	var seed [32]byte

	data, err := sd.ReadSecret(seedFileName)
	if err == nil && len(data) == 32 {
		copy(seed[:], data)
		return seed, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return seed, err
	}

	// Seed is missing or wrong size — create new one.
	if _, err := rand.Read(seed[:]); err != nil {
		return seed, err
	}
	if err := sd.WriteSecret(seedFileName, seed[:]); err != nil {
		return seed, err
	}
	return seed, nil
}
