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
	"fmt"

	"gopkg.in/yaml.v3"
)

func parseYAML(raw []byte) (any, error) {
	var data any
	if err := yaml.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return normalizeYAML(data), nil
}

func serializeYAML(data any) ([]byte, error) {
	return yaml.Marshal(data)
}

// normalizeYAML converts map[interface{}]interface{} (produced by yaml.v2) to
// map[string]any recursively. yaml.v3 produces map[string]interface{} directly,
// but we normalize defensively.
func normalizeYAML(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = normalizeYAML(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[fmt.Sprintf("%v", k)] = normalizeYAML(val)
		}
		return out
	case []any:
		for i, item := range t {
			t[i] = normalizeYAML(item)
		}
		return t
	}
	return v
}
