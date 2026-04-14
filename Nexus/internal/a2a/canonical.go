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

package a2a

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Canonicalize implements RFC 8785 JSON Canonicalization Scheme (JCS).
//
// It takes arbitrary JSON input and produces a deterministic canonical
// representation with:
//   - Object keys sorted lexicographically by Unicode code points
//   - Numbers serialized per ES6 (IEEE 754 double-precision)
//   - No insignificant whitespace
//   - Strings escaped per RFC 8785 (minimal escaping)
//   - Recursive canonicalization of nested structures
func Canonicalize(data []byte) ([]byte, error) {
	var v interface{}
	// Use json.Number to preserve numeric precision.
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("a2a: canonicalize: invalid JSON: %w", err)
	}
	var buf strings.Builder
	if err := canonicalWrite(&buf, v); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func canonicalWrite(buf *strings.Builder, v interface{}) error {
	switch val := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		return canonicalWriteNumber(buf, val)
	case string:
		canonicalWriteString(buf, val)
	case []interface{}:
		buf.WriteByte('[')
		for i, elem := range val {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := canonicalWrite(buf, elem); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		// RFC 8785: sort by Unicode code points (Go string comparison is
		// byte-wise which matches UTF-8 code point order).
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			canonicalWriteString(buf, k)
			buf.WriteByte(':')
			if err := canonicalWrite(buf, val[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("a2a: canonicalize: unsupported type %T", v)
	}
	return nil
}

// canonicalWriteNumber serializes a number per ES6 / RFC 8785 rules.
func canonicalWriteNumber(buf *strings.Builder, n json.Number) error {
	s := n.String()
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("a2a: canonicalize: invalid number %q: %w", s, err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("a2a: canonicalize: non-finite number %q", s)
	}

	// ES6 number serialization rules:
	// - Integers in range are serialized without decimal point
	// - Zero is "0" (handles -0 -> 0)
	if f == 0 {
		buf.WriteByte('0')
		return nil
	}

	// Check if the value is an integer that can be exactly represented.
	if f == math.Trunc(f) && math.Abs(f) < 1e21 {
		buf.WriteString(strconv.FormatFloat(f, 'f', -1, 64))
		return nil
	}

	// For non-integer values, use ES6 shortest representation.
	// strconv.FormatFloat with 'G' format doesn't quite match ES6,
	// so we use a custom approach.
	buf.WriteString(es6FormatFloat(f))
	return nil
}

// es6FormatFloat formats a float64 per ES6 Number.prototype.toString().
func es6FormatFloat(f float64) string {
	// Use the shortest representation that roundtrips.
	s := strconv.FormatFloat(f, 'e', -1, 64)

	// Parse the parts: mantissa, exponent
	parts := strings.SplitN(s, "e", 2)
	mantissa := parts[0]
	exp, _ := strconv.Atoi(parts[1])

	// Remove trailing zeros from mantissa
	if strings.Contains(mantissa, ".") {
		mantissa = strings.TrimRight(mantissa, "0")
		mantissa = strings.TrimRight(mantissa, ".")
	}

	sign := ""
	m := mantissa
	if strings.HasPrefix(m, "-") {
		sign = "-"
		m = m[1:]
	}

	// Count significant digits (without the decimal point)
	digits := strings.Replace(m, ".", "", 1)
	nDigits := len(digits)

	// Determine the position of the decimal point.
	// In scientific notation, the decimal point is after the first digit.
	// So the actual number is digits * 10^(exp - (nDigits - 1))
	// The decimal point position from the left of digits is: exp + 1
	dotPos := exp + 1

	// ES6 rules for choosing format:
	// If dotPos in [1, nDigits]: plain decimal (e.g. 1.5, 12.3)
	// If dotPos in (nDigits, 21]: integer-like (e.g. 150 -> "150")
	// If dotPos in [-5, 0]: leading zeros (e.g. 0.001)
	// Otherwise: exponential notation

	if dotPos >= 1 && dotPos <= nDigits {
		// Plain decimal: put the dot after dotPos digits
		if dotPos == nDigits {
			return sign + digits
		}
		return sign + digits[:dotPos] + "." + digits[dotPos:]
	}

	if dotPos > nDigits && dotPos <= 21 {
		// Append zeros
		return sign + digits + strings.Repeat("0", dotPos-nDigits)
	}

	if dotPos <= 0 && dotPos > -6 {
		// Leading zeros: 0.000...digits
		return sign + "0." + strings.Repeat("0", -dotPos) + digits
	}

	// Exponential notation
	var result strings.Builder
	result.WriteString(sign)
	result.WriteByte(digits[0])
	if nDigits > 1 {
		result.WriteByte('.')
		result.WriteString(digits[1:])
	}
	result.WriteByte('e')
	if exp >= 0 {
		result.WriteByte('+')
	}
	result.WriteString(strconv.Itoa(exp))
	return result.String()
}

// canonicalWriteString writes a JSON string with RFC 8785 escaping.
func canonicalWriteString(buf *strings.Builder, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '"':
			buf.WriteString(`\"`)
		case r == '\\':
			buf.WriteString(`\\`)
		case r == '\b':
			buf.WriteString(`\b`)
		case r == '\f':
			buf.WriteString(`\f`)
		case r == '\n':
			buf.WriteString(`\n`)
		case r == '\r':
			buf.WriteString(`\r`)
		case r == '\t':
			buf.WriteString(`\t`)
		case r < 0x20:
			// Control characters below 0x20 that aren't handled above
			buf.WriteString(fmt.Sprintf(`\u%04x`, r))
		default:
			// RFC 8785: all other characters are written literally (UTF-8)
			buf.WriteString(s[i : i+size])
		}
		i += size
	}
	buf.WriteByte('"')
}
