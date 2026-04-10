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

package wal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
)

const (
	// IntegrityModeCRC32 is the default integrity mode. Each WAL line is
	// JSON_BYTES<TAB>CRC32_HEX<NEWLINE>. No HMAC overhead.
	IntegrityModeCRC32 = "crc32"

	// IntegrityModeMAC adds HMAC-SHA256 tamper detection on top of CRC32.
	// Each WAL line is JSON_BYTES<TAB>CRC32_HEX<TAB>HMAC_HEX<NEWLINE>.
	// Reference: Tech Spec Section 4.1, Section 6.4.1.
	IntegrityModeMAC = "mac"
)

// SecurityEventFunc is a callback invoked when a security-relevant event
// occurs inside the WAL (e.g. HMAC mismatch indicating tampering).
// The attrs slice contains structured fields for the event.
type SecurityEventFunc func(eventType string, attrs ...slog.Attr)

// computeHMAC computes HMAC-SHA256 over data using key and returns the
// hex-encoded result. A new HMAC instance is created per call because
// hash.Hash is stateful and not safe for reuse across entries.
func computeHMAC(data, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// validateHMAC checks that expectedHex matches the HMAC-SHA256 of data
// using key. Returns false if expectedHex is not valid hex or the MAC
// does not match. Uses hmac.Equal for constant-time comparison.
func validateHMAC(data, key []byte, expectedHex string) bool {
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return hmac.Equal(mac.Sum(nil), expected)
}
