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
	"bytes"
	"fmt"
)

// selfTestPlaintext is the known-good plaintext used in the seal/open round-trip.
var selfTestPlaintext = []byte("nexus-encryption-self-test-v1")

// selfTestAAD is the additional authenticated data bound to each self-test blob.
var selfTestAAD = []byte("nexus-selftest")

// SelfTest verifies the encryption stack is functional by performing a
// seal → open round-trip for every sub-key domain. Returns nil when mkm is
// nil or disabled. Returns a descriptive error on the first domain that fails.
//
// Call this at daemon startup after MKM is initialized. A non-nil return means
// the encryption stack is broken; the daemon must refuse to start.
func SelfTest(mkm *MasterKeyManager) error {
	if mkm == nil || !mkm.IsEnabled() {
		return nil
	}

	for _, domain := range subKeyDomains {
		subKey := mkm.SubKey(domain)

		blob, err := SealAES256GCM(subKey, selfTestPlaintext, selfTestAAD)
		if err != nil {
			return fmt.Errorf("crypto: self-test seal failed for domain %q: %w", domain, err)
		}

		got, err := OpenAES256GCM(subKey, blob, selfTestAAD)
		if err != nil {
			return fmt.Errorf("crypto: self-test open failed for domain %q: %w", domain, err)
		}

		if !bytes.Equal(got, selfTestPlaintext) {
			return fmt.Errorf("crypto: self-test round-trip mismatch for domain %q: decrypted %d bytes, want %d",
				domain, len(got), len(selfTestPlaintext))
		}
	}

	return nil
}
