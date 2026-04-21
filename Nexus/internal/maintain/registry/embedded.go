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

package registry

import (
	_ "embed"
	"fmt"
)

//go:embed connectors.json
var embeddedConnectors []byte

// LoadEmbedded parses and returns the bundled registry from connectors.json.
// This is always available — no network required. Panics only if the embedded
// JSON is malformed (a build-time invariant, caught by tests).
func LoadEmbedded() (*Registry, error) {
	r, err := NewRegistry(embeddedConnectors)
	if err != nil {
		return nil, fmt.Errorf("registry: embedded load failed: %w", err)
	}
	return r, nil
}

// MustLoadEmbedded is like LoadEmbedded but panics on error.
// Use only in init() or main(); prefer LoadEmbedded elsewhere.
func MustLoadEmbedded() *Registry {
	r, err := LoadEmbedded()
	if err != nil {
		panic(err)
	}
	return r
}
