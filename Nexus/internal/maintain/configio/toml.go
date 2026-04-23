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
	"fmt"

	"github.com/BurntSushi/toml"
)

func parseTOML(raw []byte) (any, error) {
	var data map[string]any
	if _, err := toml.NewDecoder(bytes.NewReader(raw)).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func serializeTOML(data any) ([]byte, error) {
	m, ok := toStringMap(data)
	if !ok {
		return nil, fmt.Errorf("configio: TOML serialization requires a map at top level")
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
