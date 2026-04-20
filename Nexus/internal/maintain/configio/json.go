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

package configio

import (
	"bytes"
	"encoding/json"
)

func parseJSON(raw []byte) (any, error) {
	raw = stripBOM(raw)
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return data, nil
}

func parseJSONC(raw []byte) (any, error) {
	raw = stripBOM(raw)
	raw = stripJSONCComments(raw)
	return parseJSON(raw)
}

func serializeJSON(data any) ([]byte, error) {
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// stripBOM removes a UTF-8 byte-order mark from the start of b.
func stripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// stripJSONCComments removes // line comments and /* */ block comments from
// JSON with comments (JSONC), correctly skipping content inside string literals.
// JSONC write-back: comments are NOT preserved (stripped on read, absent on write).
func stripJSONCComments(src []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(src))
	i := 0
	for i < len(src) {
		c := src[i]

		// String literal: copy verbatim including escape sequences
		if c == '"' {
			out.WriteByte(c)
			i++
			for i < len(src) {
				c2 := src[i]
				out.WriteByte(c2)
				i++
				if c2 == '\\' && i < len(src) {
					// escaped character — copy it and move on
					out.WriteByte(src[i])
					i++
				} else if c2 == '"' {
					break
				}
			}
			continue
		}

		// Line comment: // ... \n
		if c == '/' && i+1 < len(src) && src[i+1] == '/' {
			i += 2
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		}

		// Block comment: /* ... */
		if c == '/' && i+1 < len(src) && src[i+1] == '*' {
			i += 2
			for i+1 < len(src) {
				if src[i] == '*' && src[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}

		out.WriteByte(c)
		i++
	}
	return out.Bytes()
}
