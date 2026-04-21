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

// Minimal in-house INI parser. Handles [section] / key = value format
// sufficient for all known AI tool config files. No external dependency.
package configio

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// parseINI parses an INI file into a map[string]any. Each [section] becomes a
// nested map[string]any. Top-level keys (before any section) are stored at the
// root. Section names are used as keypath components: "section.key".
func parseINI(raw []byte) (any, error) {
	result := map[string]any{}
	currentSection := ""

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			end := strings.Index(line, "]")
			if end < 0 {
				return nil, fmt.Errorf("configio: unclosed section header: %q", line)
			}
			currentSection = line[1:end]
			if _, ok := result[currentSection]; !ok {
				result[currentSection] = map[string]any{}
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue // non-assignment lines are skipped
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		// Strip inline ; or # comments from value
		if idx := strings.IndexAny(val, ";#"); idx >= 0 {
			val = strings.TrimSpace(val[:idx])
		}
		if currentSection == "" {
			result[key] = val
		} else {
			sec := result[currentSection].(map[string]any)
			sec[key] = val
		}
	}
	return result, scanner.Err()
}

// serializeINI writes a map[string]any back as INI. Top-level non-map values
// are written first, then each nested map becomes a [section].
func serializeINI(data any) ([]byte, error) {
	m, ok := toStringMap(data)
	if !ok {
		return nil, fmt.Errorf("configio: INI serialization requires a map at top level")
	}
	var buf bytes.Buffer

	// Top-level scalar keys first
	for k, v := range m {
		if _, isMap := toStringMap(v); !isMap {
			fmt.Fprintf(&buf, "%s = %v\n", k, v)
		}
	}
	// Sections
	for k, v := range m {
		sec, ok := toStringMap(v)
		if !ok {
			continue
		}
		fmt.Fprintf(&buf, "\n[%s]\n", k)
		for sk, sv := range sec {
			fmt.Fprintf(&buf, "%s = %v\n", sk, sv)
		}
	}
	return buf.Bytes(), nil
}
