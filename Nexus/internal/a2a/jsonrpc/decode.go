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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Decode parses raw JSON into JSON-RPC 2.0 messages. It returns a slice of
// decoded messages which will be one of: *Request, *Notification, or *Response.
// Batch messages (JSON arrays) are fully unpacked.
func Decode(data []byte) ([]interface{}, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("jsonrpc: empty input")
	}

	if data[0] == '[' {
		return decodeBatch(data)
	}
	msg, err := decodeSingle(data)
	if err != nil {
		return nil, err
	}
	return []interface{}{msg}, nil
}

// DecodeRequest decodes a single JSON-RPC 2.0 request.
func DecodeRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("jsonrpc: decode request: %w", err)
	}
	if req.JSONRPC != Version {
		return nil, fmt.Errorf("jsonrpc: expected version %q, got %q", Version, req.JSONRPC)
	}
	if req.Method == "" {
		return nil, errors.New("jsonrpc: request missing method")
	}
	return &req, nil
}

// DecodeResponse decodes a single JSON-RPC 2.0 response with strict
// unknown-field rejection.
func DecodeResponse(data []byte) (*Response, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var resp Response
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("jsonrpc: decode response: %w", err)
	}
	if resp.JSONRPC != Version {
		return nil, fmt.Errorf("jsonrpc: expected version %q, got %q", Version, resp.JSONRPC)
	}
	return &resp, nil
}

// decodeSingle decodes a single JSON object as either a Request, Notification,
// or Response.
func decodeSingle(data []byte) (interface{}, error) {
	// Probe for discriminating fields.
	var probe struct {
		JSONRPC string           `json:"jsonrpc"`
		Method  *string          `json:"method"`
		ID      *json.RawMessage `json:"id"`
		Result  *json.RawMessage `json:"result"`
		Error   *json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("jsonrpc: invalid JSON: %w", err)
	}
	if probe.JSONRPC != Version {
		return nil, fmt.Errorf("jsonrpc: expected version %q, got %q", Version, probe.JSONRPC)
	}

	// Response: has "result" or "error" field (and no "method").
	if probe.Method == nil {
		return DecodeResponse(data)
	}

	// Notification: has "method" but no "id" field.
	if probe.ID == nil {
		var n Notification
		if err := json.Unmarshal(data, &n); err != nil {
			return nil, fmt.Errorf("jsonrpc: decode notification: %w", err)
		}
		return &n, nil
	}

	// Request: has "method" and "id".
	return DecodeRequest(data)
}

// decodeBatch decodes a JSON array of JSON-RPC messages.
func decodeBatch(data []byte) ([]interface{}, error) {
	var rawMsgs []json.RawMessage
	if err := json.Unmarshal(data, &rawMsgs); err != nil {
		return nil, fmt.Errorf("jsonrpc: decode batch: %w", err)
	}
	if len(rawMsgs) == 0 {
		return nil, errors.New("jsonrpc: empty batch")
	}

	msgs := make([]interface{}, 0, len(rawMsgs))
	for i, raw := range rawMsgs {
		msg, err := decodeSingle(raw)
		if err != nil {
			return nil, fmt.Errorf("jsonrpc: batch element %d: %w", i, err)
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}
