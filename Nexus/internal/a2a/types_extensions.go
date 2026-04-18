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

import "encoding/json"

// ExtensionMap is a typed map of extension URI to opaque JSON payload.
type ExtensionMap map[string]json.RawMessage

// Get retrieves the raw JSON payload for an extension URI.
// Returns nil if the key is not present.
func (em ExtensionMap) Get(uri string) json.RawMessage {
	if em == nil {
		return nil
	}
	return em[uri]
}

// Set stores a raw JSON payload for an extension URI.
func (em ExtensionMap) Set(uri string, data json.RawMessage) {
	em[uri] = data
}

// SetValue marshals v as JSON and stores it under the given URI.
func (em ExtensionMap) SetValue(uri string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	em[uri] = data
	return nil
}

// GetValue unmarshals the JSON payload for uri into v.
// Returns false if the key is not present.
func (em ExtensionMap) GetValue(uri string, v interface{}) (bool, error) {
	data := em.Get(uri)
	if data == nil {
		return false, nil
	}
	return true, json.Unmarshal(data, v)
}

// Has returns true if the extension URI is present in the map.
func (em ExtensionMap) Has(uri string) bool {
	if em == nil {
		return false
	}
	_, ok := em[uri]
	return ok
}

// Delete removes an extension URI from the map.
func (em ExtensionMap) Delete(uri string) {
	delete(em, uri)
}

// Merge copies all entries from other into em.
// Existing keys in em are overwritten by values from other.
func (em ExtensionMap) Merge(other ExtensionMap) {
	for k, v := range other {
		em[k] = v
	}
}

// Keys returns all extension URIs in the map.
func (em ExtensionMap) Keys() []string {
	keys := make([]string, 0, len(em))
	for k := range em {
		keys = append(keys, k)
	}
	return keys
}
