// Copyright © 2026 Shawn Sammartano. All rights reserved.

package wal

import (
	"fmt"
	"hash/crc32"
	"strings"
)

const (
	// StartSentinel marks the beginning of a sentinel-format WAL entry.
	// 8 bytes of 0xBF hex-encoded (16 characters). The "BF" BubbleFish
	// signature pattern is unlikely to appear as the first field of a
	// valid JSON line, enabling reliable format auto-detection.
	StartSentinel = "BFBFBFBFBFBFBFBF"

	// EndSentinel marks the end of a sentinel-format WAL entry.
	// 8 bytes of 0xFB hex-encoded (16 characters). Deliberately different
	// from StartSentinel so directional corruption is detectable.
	EndSentinel = "FBFBFBFBFBFBFBFB"
)

// TODO(shawn): The build plan specifies binary 8-byte sentinels in a binary
// wire format ([START:8][LEN:4][CRC:4][PAYLOAD:N][END:8]). The actual WAL
// uses text-based JSONL, so sentinels are hex-encoded as tab-separated
// fields in the line format. This preserves all existing line-based readers
// (replay, checkpoint scan, updater, scanDelivered) and maintains the same
// backward compatibility guarantees. The torn-write detection semantics are
// identical: a missing or corrupted end sentinel triggers fail-closed rejection.

// walLine holds parsed fields from a single WAL line in either old or new format.
type walLine struct {
	JSONBytes    []byte
	StoredCRC    string
	StoredHMAC   string // empty if no HMAC field present
	HasSentinels bool   // true if start sentinel was detected
	SentinelErr  error  // non-nil if start sentinel found but end sentinel missing/corrupt
}

// parseWALLine parses a WAL line in either format and returns its components.
//
// New format: StartSentinel\tJSON\tCRC[\tHMAC]\tEndSentinel
// Old format: JSON\tCRC[\tHMAC]
//
// Returns nil if the line has fewer than 2 tab-separated fields (partial write).
func parseWALLine(line string) *walLine {
	parts := strings.SplitN(line, "\t", 6)
	if len(parts) < 2 {
		return nil
	}

	if parts[0] == StartSentinel {
		wl := &walLine{HasSentinels: true}

		// New format minimum: START, JSON, CRC, END = 4 fields.
		if len(parts) < 4 {
			wl.SentinelErr = fmt.Errorf("sentinel entry incomplete: %d fields", len(parts))
			if len(parts) >= 2 {
				wl.JSONBytes = []byte(parts[1])
			}
			if len(parts) >= 3 {
				wl.StoredCRC = parts[2]
			}
			return wl
		}

		last := parts[len(parts)-1]
		if last != EndSentinel {
			wl.SentinelErr = fmt.Errorf("end sentinel mismatch: expected %s, got %q", EndSentinel, last)
			wl.JSONBytes = []byte(parts[1])
			wl.StoredCRC = parts[2]
			return wl
		}

		wl.JSONBytes = []byte(parts[1])
		wl.StoredCRC = parts[2]
		// 5 fields: START\tJSON\tCRC\tHMAC\tEND
		if len(parts) == 5 {
			wl.StoredHMAC = parts[3]
		}
		return wl
	}

	// Old format: JSON\tCRC[\tHMAC]
	wl := &walLine{
		JSONBytes: []byte(parts[0]),
		StoredCRC: parts[1],
	}
	if len(parts) >= 3 {
		wl.StoredHMAC = parts[2]
	}
	return wl
}

// formatWALContent creates the content of a WAL line (without trailing newline)
// in the new sentinel format.
//
// Without HMAC: StartSentinel\tJSON\tCRC\tEndSentinel
// With HMAC:    StartSentinel\tJSON\tCRC\tHMAC\tEndSentinel
func formatWALContent(data []byte, integrityMode string, macKey []byte) string {
	checksum := crc32.ChecksumIEEE(data)
	if integrityMode == IntegrityModeMAC {
		mac := computeHMAC(data, macKey)
		return fmt.Sprintf("%s\t%s\t%08x\t%s\t%s", StartSentinel, data, checksum, mac, EndSentinel)
	}
	return fmt.Sprintf("%s\t%s\t%08x\t%s", StartSentinel, data, checksum, EndSentinel)
}
