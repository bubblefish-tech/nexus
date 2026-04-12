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
	"encoding/base64"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

const (
	// compressPrefix marks a compressed WAL entry payload. When replay
	// encounters this prefix in the JSON field position, it strips the prefix,
	// base64-decodes, and zstd-decompresses to recover the original JSON.
	// Uncompressed entries (v0.1.2 and earlier) never start with this prefix.
	compressPrefix = "zstd:"
)

// compressor holds a pooled zstd encoder and decoder. Created once via sync.Once.
var (
	compressorOnce sync.Once
	zstdEncoder    *zstd.Encoder
	zstdDecoder    *zstd.Decoder
)

func initCompressor() {
	compressorOnce.Do(func() {
		// Speed-optimized encoder — WAL writes are latency-sensitive.
		enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
		if err != nil {
			panic("wal: create zstd encoder: " + err.Error())
		}
		zstdEncoder = enc

		dec, err := zstd.NewReader(nil)
		if err != nil {
			panic("wal: create zstd decoder: " + err.Error())
		}
		zstdDecoder = dec
	})
}

// compressPayload compresses JSON bytes with zstd and returns a string
// formatted as "zstd:<base64-encoded-compressed-bytes>".
// CRC32 is computed over this output (compressed), not the original JSON.
func compressPayload(jsonBytes []byte) string {
	initCompressor()
	compressed := zstdEncoder.EncodeAll(jsonBytes, nil)
	return compressPrefix + base64.StdEncoding.EncodeToString(compressed)
}

// decompressPayload checks if data starts with the zstd prefix, and if so
// decompresses it. Returns the original JSON bytes and true if decompressed,
// or the input unchanged and false if not compressed.
func decompressPayload(data []byte) ([]byte, bool, error) {
	s := string(data)
	if !strings.HasPrefix(s, compressPrefix) {
		return data, false, nil
	}

	initCompressor()
	b64 := s[len(compressPrefix):]
	compressed, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, false, err
	}

	decompressed, err := zstdDecoder.DecodeAll(compressed, nil)
	if err != nil {
		return nil, false, err
	}

	return decompressed, true, nil
}
