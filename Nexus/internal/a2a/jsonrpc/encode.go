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

package jsonrpc

import (
	"encoding/json"
	"fmt"
)

// Encode marshals any JSON-RPC message (Request, Response, Notification)
// to JSON bytes.
func Encode(v interface{}) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: encode: %w", err)
	}
	return data, nil
}

// EncodeBatch marshals a batch of JSON-RPC messages to a JSON array.
func EncodeBatch(msgs []interface{}) ([]byte, error) {
	if len(msgs) == 0 {
		return nil, fmt.Errorf("jsonrpc: cannot encode empty batch")
	}
	data, err := json.Marshal(msgs)
	if err != nil {
		return nil, fmt.Errorf("jsonrpc: encode batch: %w", err)
	}
	return data, nil
}
