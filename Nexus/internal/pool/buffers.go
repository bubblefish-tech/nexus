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

package pool

import (
	"bytes"
	"sync"
)

// JSONBuf is a sync.Pool for JSON encode buffers.
var JSONBuf = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// GetJSONBuf returns a reset buffer. Caller must defer PutJSONBuf.
func GetJSONBuf() *bytes.Buffer {
	buf := JSONBuf.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// PutJSONBuf returns a buffer to the pool. Oversized buffers (>1MiB) are discarded.
func PutJSONBuf(buf *bytes.Buffer) {
	if buf.Cap() > 1<<20 {
		return
	}
	JSONBuf.Put(buf)
}

// CopyBuf is a sync.Pool for io.CopyBuffer scratch buffers.
var CopyBuf = sync.Pool{New: func() any {
	b := make([]byte, 32*1024)
	return &b
}}

// GetCopyBuf returns a 32KB scratch buffer.
func GetCopyBuf() []byte { return *CopyBuf.Get().(*[]byte) }

// PutCopyBuf returns a scratch buffer to the pool.
func PutCopyBuf(b []byte) { CopyBuf.Put(&b) }
